package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/store"
)

type Options struct {
	Home, AgentID, SessionID string
	TTL, PollInterval        time.Duration
}
type runtimeLease struct {
	ref, path string
	ttl       time.Duration
	holdUntil time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	status    string
}

func NewServer(opts Options) (*Server, error) {
	if strings.TrimSpace(opts.Home) == "" {
		return nil, reason.New(reason.ReasonHomeUnsafe, "authority home is required")
	}
	if opts.TTL == 0 {
		opts.TTL = config.DefaultTTL
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = config.DefaultPollInterval
	}
	return &Server{options: opts, requests: map[string]*requestState{}, seen: map[string]struct{}{}, leases: map[string]*runtimeLease{}}, nil
}

type serviceBundle struct {
	svc *lease.Service
	st  *store.Store
}

func (s *Server) open(ctx context.Context, write bool) (serviceBundle, error) {
	st, e := store.Open(ctx, s.options.Home, store.Options{ReadOnly: !write})
	if e != nil {
		return serviceBundle{}, e
	}
	return serviceBundle{lease.New(st, nil, nil, lease.Defaults{TTL: s.options.TTL, PollInterval: s.options.PollInterval}), st}, nil
}

var toolOrder = []string{"key", "acquire", "status", "list", "heartbeat", "checkpoint", "verify", "watch", "events", "release", "instructions"}

func (s *Server) handle(ctx context.Context, req rpcRequest) (any, *rpcError) {
	if v := requestVersion(req); v != "" && v != ModernVersion && v != LegacyVersion {
		return nil, unsupportedVersion(v)
	}
	if req.Method == "initialize" {
		return s.initialize(req)
	}
	if (req.Method == "tools/list" || req.Method == "tools/call") && !s.legacy && requestVersion(req) == "" {
		return nil, unsupportedVersion("")
	}
	if req.Method == "server/discover" {
		return s.discover(), nil
	}
	if req.Method == "tools/list" {
		return map[string]any{"tools": s.tools()}, nil
	}
	if req.Method != "tools/call" {
		return nil, protocolError(-32601, "method not found", nil)
	}
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
		return nil, protocolError(-32602, "invalid tools/call parameters", nil)
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}
	if !contains(toolOrder, p.Name) {
		return nil, protocolError(-32601, "unknown tool", nil)
	}
	if err := validateArgs(p.Name, p.Arguments); err != nil {
		return toolFailure(err), nil
	}
	value, err := s.callTool(ctx, p.Name, p.Arguments)
	if err != nil {
		return toolFailure(err), nil
	}
	return toolSuccess(value), nil
}
func (s *Server) initialize(req rpcRequest) (any, *rpcError) {
	var p map[string]any
	_ = json.Unmarshal(req.Params, &p)
	version, _ := p["protocolVersion"].(string)
	if version == ModernVersion || requestVersion(req) == ModernVersion {
		s.modern = true
		return map[string]any{"protocolVersion": ModernVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "worklease", "version": "dev"}, "instructions": "Worklease local lease authority; use the opaque lease returned by acquire."}, nil
	}
	if version != "" && version != LegacyVersion {
		return nil, unsupportedVersion(version)
	}
	s.mu.Lock()
	s.legacy = true
	s.legacyReady = make(chan struct{})
	s.mu.Unlock()
	return map[string]any{"protocolVersion": LegacyVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "worklease", "version": "dev"}, "instructions": "Worklease local lease authority; use the opaque lease returned by acquire."}, nil
}
func requestVersion(req rpcRequest) string {
	var m map[string]any
	if json.Unmarshal(req.Meta, &m) == nil {
		if v, _ := m["protocolVersion"].(string); v != "" {
			return v
		}
	}
	var p map[string]any
	if json.Unmarshal(req.Params, &p) == nil {
		if v, _ := p["protocolVersion"].(string); v != "" {
			return v
		}
	}
	return ""
}
func unsupportedVersion(v string) *rpcError {
	return protocolError(-32602, "unsupported protocol version", map[string]any{"protocolVersion": v, "supportedVersions": []string{ModernVersion, LegacyVersion}})
}
func (s *Server) discover() map[string]any {
	return map[string]any{"protocolVersion": ModernVersion, "supportedVersions": []string{ModernVersion, LegacyVersion}, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "worklease", "version": "dev"}}
}

func schema(required []string, props map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false, "$schema": "https://json-schema.org/draft/2020-12/schema"}
}
func (s *Server) tools() []map[string]any {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	resources := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 32, "uniqueItems": true}
	defs := map[string]map[string]any{
		"key":          schema(nil, map[string]any{"provider": str(), "source": str(), "item": str(), "path": str(), "coordinationOnly": map[string]any{"type": "boolean"}}),
		"acquire":      schema(nil, map[string]any{"lease": str(), "resources": resources, "provider": str(), "source": str(), "item": str(), "path": str(), "ttl": map[string]any{"type": "number", "exclusiveMinimum": 0}, "wait": map[string]any{"type": "number", "minimum": 0, "maximum": 60}, "workKey": str(), "agentId": str(), "sessionId": str(), "coordinationOnly": map[string]any{"type": "boolean"}, "autoHeartbeat": map[string]any{"type": "boolean", "default": true}, "maxHold": map[string]any{"type": "number", "minimum": 60}}),
		"status":       schema(nil, map[string]any{"lease": str(), "resources": resources}),
		"list":         schema(nil, map[string]any{"resource": str()}),
		"heartbeat":    schema([]string{"lease"}, map[string]any{"lease": str(), "ttl": map[string]any{"type": "number", "exclusiveMinimum": 0}}),
		"checkpoint":   schema([]string{"lease", "data"}, map[string]any{"lease": str(), "data": map[string]any{"type": "object"}, "ttl": map[string]any{"type": "number", "exclusiveMinimum": 0}}),
		"verify":       schema([]string{"lease"}, map[string]any{"lease": str(), "resources": resources}),
		"watch":        schema(nil, map[string]any{"cursor": str(), "resources": resources, "until": str(), "timeout": map[string]any{"type": "number", "minimum": 0, "maximum": 60}}),
		"events":       schema(nil, map[string]any{"cursor": str(), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}}),
		"release":      schema([]string{"lease"}, map[string]any{"lease": str(), "reason": str()}),
		"instructions": schema([]string{"topic"}, map[string]any{"topic": map[string]any{"type": "string", "enum": []string{"loop", "safety"}}}),
	}
	defs["key"]["oneOf"] = []any{map[string]any{"required": []string{"path"}}, map[string]any{"required": []string{"provider", "source", "item"}}}
	defs["acquire"]["oneOf"] = []any{map[string]any{"required": []string{"lease"}}, map[string]any{"required": []string{"resources"}}, map[string]any{"required": []string{"provider", "source", "item"}}, map[string]any{"required": []string{"path"}}}
	defs["status"]["oneOf"] = []any{map[string]any{"required": []string{"lease"}}, map[string]any{"required": []string{"resources"}}}
	defs["watch"]["oneOf"] = []any{map[string]any{"required": []string{"cursor"}}, map[string]any{"required": []string{"resources", "until"}}}
	out := make([]map[string]any, 0, len(toolOrder))
	for _, name := range toolOrder {
		out = append(out, map[string]any{"name": name, "description": "Worklease " + name + " operation", "inputSchema": defs[name]})
	}
	return out
}
func toolSuccess(v any) map[string]any {
	m := map[string]any{"ok": true}
	if x, ok := v.(map[string]any); ok {
		for k, v := range x {
			m[k] = v
		}
	} else {
		m["value"] = v
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": jsonText(m)}}, "structuredContent": output.Redact(m)}
}
func toolFailure(err error) map[string]any {
	f := output.Classify(err)
	m := map[string]any{"ok": false, "error": map[string]any{"reason": f.Reason, "exitCode": f.ExitCode, "message": f.Message, "details": f.Details}}
	return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": jsonText(m)}}, "structuredContent": output.Redact(m)}
}
func jsonText(v any) string { b, _ := json.Marshal(output.Redact(v)); return string(b) }
func contains(a []string, v string) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}
func argString(a map[string]any, k string) (string, error) {
	if v, ok := a[k]; ok {
		if x, ok := v.(string); ok {
			return x, nil
		}
		return "", reason.Invalid(k + " must be a string")
	}
	return "", nil
}
func argBool(a map[string]any, k string, d bool) (bool, error) {
	if v, ok := a[k]; ok {
		if x, ok := v.(bool); ok {
			return x, nil
		}
		return false, reason.Invalid(k + " must be a boolean")
	}
	return d, nil
}
func argNumber(a map[string]any, k string, d float64) (float64, error) {
	if v, ok := a[k]; ok {
		switch x := v.(type) {
		case json.Number:
			f, e := x.Float64()
			return f, e
		case float64:
			return x, nil
		default:
			return 0, reason.Invalid(k + " must be a number")
		}
	}
	return d, nil
}
func opID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b[:])
}

// Call invokes a tool without the transport, useful for embedders and lifecycle tests.
func (s *Server) Call(ctx context.Context, name string, a map[string]any) (map[string]any, error) {
	if a == nil {
		a = map[string]any{}
	}
	if err := validateArgs(name, a); err != nil {
		return toolFailure(err), nil
	}
	v, err := s.callTool(ctx, name, a)
	if err != nil {
		return toolFailure(err), nil
	}
	return toolSuccess(v), nil
}
func (s *Server) Close() { s.stopAllRenewals() }

func (s *Server) callTool(ctx context.Context, name string, a map[string]any) (any, error) {
	switch name {
	case "key":
		return s.key(a)
	case "acquire":
		return s.acquire(ctx, a)
	case "status":
		return s.status(ctx, a)
	case "list":
		return s.list(ctx, a)
	case "heartbeat":
		return s.mutation(ctx, a, "heartbeat")
	case "checkpoint":
		return s.mutation(ctx, a, "checkpoint")
	case "release":
		return s.mutation(ctx, a, "release")
	case "verify":
		return s.verify(ctx, a)
	case "events":
		return s.events(ctx, a)
	case "watch":
		return s.watch(ctx, a)
	case "instructions":
		return s.instructions(a)
	}
	return nil, reason.Invalid("unknown tool")
}

func validateArgs(name string, a map[string]any) error {
	allowed := map[string]map[string]bool{
		"key":     {"provider": true, "source": true, "item": true, "path": true, "coordinationOnly": true},
		"acquire": {"lease": true, "resources": true, "provider": true, "source": true, "item": true, "path": true, "ttl": true, "wait": true, "workKey": true, "agentId": true, "sessionId": true, "coordinationOnly": true, "autoHeartbeat": true, "maxHold": true},
		"status":  {"lease": true, "resources": true}, "list": {"resource": true},
		"heartbeat": {"lease": true, "ttl": true}, "checkpoint": {"lease": true, "data": true, "ttl": true}, "verify": {"lease": true, "resources": true},
		"watch": {"cursor": true, "resources": true, "until": true, "timeout": true}, "events": {"cursor": true, "limit": true}, "release": {"lease": true, "reason": true}, "instructions": {"topic": true},
	}
	if allowed[name] == nil {
		return reason.Invalid("unknown tool")
	}
	for k := range a {
		if !allowed[name][k] {
			return reason.Invalid("unknown argument: " + k)
		}
	}
	strings := []string{"provider", "source", "item", "path", "lease", "cursor", "until", "workKey", "agentId", "sessionId", "reason", "topic", "resource"}
	for _, k := range strings {
		if _, ok := a[k]; ok {
			if _, ok := a[k].(string); !ok {
				return reason.Invalid(k + " must be a string")
			}
		}
	}
	for _, k := range []string{"coordinationOnly", "autoHeartbeat"} {
		if _, ok := a[k]; ok {
			if _, ok := a[k].(bool); !ok {
				return reason.Invalid(k + " must be a boolean")
			}
		}
	}
	for _, k := range []string{"ttl", "wait", "timeout", "maxHold", "limit"} {
		if v, ok := a[k]; ok {
			if !validNumber(v, k == "limit") {
				return reason.Invalid(k + " must be a number")
			}
		}
	}
	for _, k := range []string{"resources"} {
		if v, ok := a[k]; ok {
			if _, err := stringList(v); err != nil {
				return err
			}
		}
	}
	if v, ok := a["data"]; ok {
		if _, ok := v.(map[string]any); !ok {
			return reason.Invalid("data must be an object")
		}
	}
	if name == "heartbeat" || name == "checkpoint" || name == "verify" || name == "release" {
		if _, ok := a["lease"].(string); !ok || a["lease"] == "" {
			return reason.Invalid("lease is required")
		}
	}
	if name == "instructions" {
		topic, _ := a["topic"].(string)
		if topic != "loop" && topic != "safety" {
			return reason.Invalid("topic must be loop or safety")
		}
	}
	if name == "status" {
		_, leaseOK := a["lease"]
		_, resourcesOK := a["resources"]
		if leaseOK == resourcesOK {
			return reason.Invalid("status requires exactly one of lease or resources")
		}
	}
	if name == "key" {
		if err := exclusiveResourceMode(a, false); err != nil {
			return err
		}
	}
	if name == "acquire" {
		if ref, hasLease := a["lease"]; hasLease && ref != "" {
			for _, k := range []string{"resources", "path", "provider", "source", "item"} {
				if _, ok := a[k]; ok {
					return reason.New(reason.ReasonResourceInputConflict, "pending acquire replay cannot change resource input")
				}
			}
		} else if err := exclusiveResourceMode(a, true); err != nil {
			return err
		}
	}
	if name == "watch" {
		_, cursorOK := a["cursor"]
		_, resourcesOK := a["resources"]
		_, untilOK := a["until"]
		if cursorOK == (resourcesOK || untilOK) || (!cursorOK && (!resourcesOK || !untilOK)) {
			return reason.Invalid("watch requires cursor or resources and until")
		}
	}
	if name == "events" {
		if v, ok := a["limit"]; ok && !validNumber(v, true) {
			return reason.Invalid("limit must be an integer")
		}
	}
	return nil
}

func validNumber(v any, integer bool) bool {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return false
		}
		n = f
	default:
		return false
	}
	return !math.IsNaN(n) && !math.IsInf(n, 0) && (!integer || math.Trunc(n) == n)
}

func exclusiveResourceMode(a map[string]any, acquire bool) error {
	_, direct := a["resources"]
	_, path := a["path"]
	_, p := a["provider"]
	_, src := a["source"]
	_, item := a["item"]
	triple := p || src || item
	modes := 0
	if direct {
		modes++
	}
	if path {
		modes++
	}
	if triple {
		modes++
	}
	if modes != 1 {
		return reason.Invalid("resource input modes are exclusive")
	}
	if path && triple {
		return reason.New(reason.ReasonResourceInputConflict, "resource input modes are exclusive")
	}
	if triple && (!p || !src || !item) {
		return reason.Invalid("provider input requires provider, source, and item")
	}
	_ = acquire
	return nil
}
func resourceInputs(a map[string]any) ([]string, error) {
	coordination, _ := a["coordinationOnly"].(bool)
	if v, ok := a["resources"]; ok {
		var arr []any
		switch x := v.(type) {
		case []any:
			arr = x
		case []string:
			for _, item := range x {
				arr = append(arr, item)
			}
		default:
		}
		if len(arr) == 0 || len(arr) > 32 {
			return nil, reason.Invalid("resources must contain 1 to 32 strings")
		}
		out := make([]string, len(arr))
		for i, x := range arr {
			var ok bool
			out[i], ok = x.(string)
			if !ok {
				return nil, reason.Invalid("resources must contain strings")
			}
			if _, e := resource.Direct(out[i], coordination); e != nil {
				return nil, e
			}
		}
		return out, nil
	}
	path, _ := a["path"].(string)
	p, _ := a["provider"].(string)
	src, _ := a["source"].(string)
	item, _ := a["item"].(string)
	if path != "" && (p != "" || src != "" || item != "") {
		return nil, reason.New(reason.ReasonResourceInputConflict, "resource input modes are exclusive")
	}
	if path == "" && (p == "" || src == "" || item == "") {
		return nil, reason.Invalid("resource input is required")
	}
	co, _ := a["coordinationOnly"].(bool)
	k, e := resource.Resolve(resource.Input{Provider: p, Source: src, Item: item, Path: path, CoordinationOnly: co})
	if e != nil {
		return nil, e
	}
	return []string{k.Resource}, nil
}
func (s *Server) key(a map[string]any) (any, error) {
	co, e := argBool(a, "coordinationOnly", false)
	if e != nil {
		return nil, e
	}
	var k resource.Key
	if raw, ok := a["path"]; ok {
		path, _ := raw.(string)
		k, e = resource.Resolve(resource.Input{Path: path, CoordinationOnly: co})
	} else if _, ok := a["resources"]; ok {
		return nil, reason.Invalid("resources are not valid for key")
	} else {
		k, e = resource.Resolve(resource.Input{Provider: valueString(a, "provider"), Source: valueString(a, "source"), Item: valueString(a, "item"), CoordinationOnly: co})
	}
	if e != nil {
		return nil, e
	}
	return map[string]any{"provider": k.Provider, "source": k.Source, "item": k.Item, "resource": k.Resource, "capability": k.Capability, "scope": k.Scope, "identityScope": k.IdentityScope, "localReplaceAllowed": k.LocalReplaceAllowed, "providerFencing": k.ProviderFencing, "genericExecutionGuarantee": "local-coordination"}, nil
}
func valueString(a map[string]any, key string) string { v, _ := a[key].(string); return v }
func (s *Server) handlePath(ref string) string {
	return filepath.Join(s.options.Home, "handles", "mcp-"+ref+".json")
}
func (s *Server) readLease(ctx context.Context, ref string, write bool) (handle.Handle, string, *handle.Lock, error) {
	if len(ref) != 32 {
		return handle.Handle{}, "", nil, reason.Invalid("lease reference is invalid")
	}
	if _, e := hex.DecodeString(ref); e != nil {
		return handle.Handle{}, "", nil, reason.Invalid("lease reference is invalid")
	}
	p := s.handlePath(ref)
	var lk *handle.Lock
	var e error
	if write {
		lk, e = handle.AcquireLock(ctx, p+".lock")
	} else {
		lk, e = handle.AcquireExistingLock(ctx, p+".lock")
	}
	if e != nil {
		return handle.Handle{}, p, nil, e
	}
	h, e := handle.Read(p)
	if e != nil {
		lk.Close()
		return handle.Handle{}, p, nil, reason.New(reason.ReasonInvalidToken, "lease reference is unavailable")
	}
	if h.AuthorityID != "" {
		return h, p, lk, nil
	}
	lk.Close()
	return handle.Handle{}, p, nil, reason.New(reason.ReasonHandleMalformed, "lease handle is malformed")
}
func (s *Server) deadline() time.Time { return time.Now().UTC().Add(24 * time.Hour) }
func (s *Server) acquire(ctx context.Context, a map[string]any) (any, error) {
	if ref, _ := argString(a, "lease"); ref != "" {
		return s.recoverAcquire(ctx, ref)
	}
	resources, e := resourceInputs(a)
	if e != nil {
		return nil, e
	}
	b, e := s.open(ctx, true)
	if e != nil {
		return nil, e
	}
	defer b.st.Close()
	ttlv, e := argNumber(a, "ttl", s.options.TTL.Seconds())
	if e != nil || ttlv <= 0 || ttlv > 3600 {
		return nil, reason.Invalid("ttl must be between 1s and 1h")
	}
	wait, e := argNumber(a, "wait", 0)
	if e != nil || wait < 0 || wait > 60 {
		return nil, reason.Invalid("wait must be between 0 and 60s")
	}
	hold, e := argNumber(a, "maxHold", 4*3600)
	if e != nil || hold < 60 || hold > 24*3600 {
		return nil, reason.Invalid("maxHold must be between 1m and 24h")
	}
	if ttlv > hold {
		ttlv = hold
	}
	if ttlv < 1 {
		return nil, reason.Invalid("ttl must be at least 1s")
	}
	co, e := argBool(a, "coordinationOnly", false)
	if e != nil {
		return nil, e
	}
	auto, e := argBool(a, "autoHeartbeat", true)
	if e != nil {
		return nil, e
	}
	agent, _ := argString(a, "agentId")
	if agent == "" {
		agent = s.options.AgentID
	}
	if agent == "" {
		agent = "worklease-mcp"
	}
	session, e := argString(a, "sessionId")
	if e != nil {
		return nil, e
	}
	if session == "" {
		session = s.options.SessionID
	}
	if session == "" {
		session = opID()
	}
	work, _ := argString(a, "workKey")
	if work == "" {
		work = strings.Join(resources, ",")
	}
	ref := opID()
	claim := opID()
	path := s.handlePath(ref)
	// The reference is private until returned, but the handle lock is still
	// the contract boundary from pending write through dispatch and update.
	lk, e := handle.AcquireLock(ctx, path+".lock")
	if e != nil {
		return nil, e
	}
	defer lk.Close()
	deadline := s.deadline()
	inputs := map[string]any{"kind": "acquire", "authorityId": b.st.AuthorityID(), "claimId": claim, "resources": resources, "agentId": agent, "sessionId": session, "workKey": work, "ttl": time.Duration(ttlv * float64(time.Second)).Microseconds(), "wait": time.Duration(wait * float64(time.Second)).Microseconds(), "requestNotAfter": deadline.UnixMicro(), "coordinationOnly": co, "localReplaceAllowed": !co}
	hash := hashValue(map[string]any{"kind": "acquire", "authorityId": b.st.AuthorityID(), "claimId": claim, "resources": resources, "agentId": agent, "sessionId": session, "workKey": work, "ttl": time.Duration(ttlv * float64(time.Second)).Microseconds(), "requestNotAfter": deadline.UnixMicro(), "localReplaceAllowed": !co, "coordinationOnly": co})
	h := handle.Handle{SchemaVersion: 1, AuthorityID: b.st.AuthorityID(), ClaimID: claim, Token: randomToken(), Resources: resources, AgentID: agent, SessionID: session, LocalReplaceAllowed: !co, HoldUntil: time.Now().UTC().Add(time.Duration(hold * float64(time.Second))), AutoRenewOwner: func() string {
		if auto {
			return opID()
		}
		return ""
	}(), State: "pending", PendingRequest: &handle.PendingRequest{OperationID: claim, Kind: "acquire", AuthorityID: b.st.AuthorityID(), ClaimID: claim, RequestHash: hash, RequestNotAfter: deadline, Inputs: inputs}}
	if e = handle.Write(path, h); e != nil {
		return nil, e
	}
	g, e := b.svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: b.st.AuthorityID(), ClaimID: claim, Token: h.Token, Resources: resources, AgentID: agent, SessionID: session, WorkKey: work, TTL: time.Duration(ttlv * float64(time.Second)), Wait: time.Duration(wait * float64(time.Second)), CoordinationOnly: co, LocalReplaceAllowed: !co, RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
	if e != nil {
		if reason.DefinitiveNoCommit(e) {
			// A grant that provably never committed leaves no recoverable
			// state; retaining it would only accumulate orphan pending handles.
			_ = handle.Remove(path)
			return nil, mutationError(e, claim, claim, path)
		}
		if x := reason.As(e); x != nil {
			x.With("lease", ref)
		}
		return nil, mutationError(e, claim, claim, path)
	}
	h.State = "ready"
	h.Revision = g.Revision
	h.ExpiresAt = g.ExpiresAt
	h.HoldUntil = g.AcquiredAt.Add(time.Duration(hold * float64(time.Second)))
	if auto {
		h.AutoRenewOwner = opID()
	}
	if h.ExpiresAt.After(h.HoldUntil) {
		h.ExpiresAt = h.HoldUntil
	}
	h.PendingRequest = nil
	if e = handle.Write(path, h); e != nil {
		return nil, reason.New(reason.ReasonHandleWriteFailed, "lease handle could not be updated").With("claimId", h.ClaimID).With("operationId", claim).With("pendingPath", path).With("commitState", "committed")
	}
	status := "disabled"
	if auto {
		status = "active"
		s.startRenewal(ref, ttlDuration(ttlv), h.HoldUntil)
	} else {
		status = "disabled"
	}
	return map[string]any{"lease": ref, "authorityId": g.AuthorityID, "handlePath": path, "claim": gClaim(g), "autoHeartbeat": status, "holdUntil": h.HoldUntil}, nil
}
func (s *Server) recoverAcquire(ctx context.Context, ref string) (any, error) {
	h, path, lk, err := s.readLease(ctx, ref, true)
	if err != nil {
		return nil, err
	}
	defer lk.Close()
	if h.State != "pending" || h.PendingRequest == nil || h.PendingRequest.Kind != "acquire" {
		return nil, reason.New(reason.ReasonOperationRequestMismatch, "pending acquire request differs")
	}
	b, err := s.open(ctx, true)
	if err != nil {
		return nil, err
	}
	defer b.st.Close()
	p := h.PendingRequest
	resources, ok := p.Inputs["resources"].([]any)
	var rs []string
	if ok {
		for _, v := range resources {
			x, good := v.(string)
			if !good {
				return nil, reason.Invalid("pending acquire resources are malformed")
			}
			rs = append(rs, x)
		}
	} else if raw, ok := p.Inputs["resources"].([]string); ok {
		rs = raw
	} else {
		return nil, reason.Invalid("pending acquire resources are malformed")
	}
	co, _ := p.Inputs["coordinationOnly"].(bool)
	local := !co
	g, err := b.svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Resources: rs, AgentID: h.AgentID, SessionID: h.SessionID, WorkKey: pendingString(p.Inputs, "workKey"), TTL: time.Duration(pendingInt(p.Inputs, "ttl")) * time.Microsecond, Wait: time.Duration(pendingInt(p.Inputs, "wait")) * time.Microsecond, CoordinationOnly: co, LocalReplaceAllowed: local, RequestNotAfter: p.RequestNotAfter, HoldUntil: h.HoldUntil})
	if err != nil {
		if reason.DefinitiveNoCommit(err) {
			_ = handle.ClearPending(path, &h)
		}
		return nil, mutationError(err, h.ClaimID, p.OperationID, path)
	}
	h.State, h.PendingRequest = "ready", nil
	h.Revision, h.ExpiresAt = g.Revision, g.ExpiresAt
	if h.HoldUntil.IsZero() {
		h.HoldUntil = g.AcquiredAt.Add(4 * time.Hour)
	}
	if h.ExpiresAt.After(h.HoldUntil) {
		h.ExpiresAt = h.HoldUntil
	}
	if err := handle.Write(path, h); err != nil {
		return nil, reason.New(reason.ReasonHandleWriteFailed, "lease handle could not be updated").With("claimId", h.ClaimID).With("operationId", p.OperationID).With("commitState", "committed")
	}
	if h.AutoRenewOwner != "" {
		s.startRenewal(ref, time.Duration(pendingInt(p.Inputs, "ttl"))*time.Microsecond, h.HoldUntil)
	}
	status := "disabled"
	if h.AutoRenewOwner != "" {
		status = "active"
	}
	return map[string]any{"lease": ref, "authorityId": g.AuthorityID, "handlePath": path, "claim": gClaim(g), "autoHeartbeat": status, "holdUntil": h.HoldUntil}, nil
}

func randomToken() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b[:])
}
func ttlDuration(v float64) time.Duration { return time.Duration(v * float64(time.Second)) }
func gClaim(g lease.Grant) map[string]any {
	return map[string]any{"claimId": g.ClaimID, "resources": g.Resources, "agentId": g.AgentID, "sessionId": g.SessionID, "workKey": g.WorkKey, "revision": g.Revision, "acquiredAt": g.AcquiredAt, "expiresAt": g.ExpiresAt, "authorityId": g.AuthorityID, "guarantee": g.Guarantee, "localReplaceAllowed": g.LocalReplaceAllowed, "active": g.Active, "unknownOperations": g.UnknownOperations}
}
func hashValue(v any) string {
	b, _ := json.Marshal(v)
	out := sha256.Sum256(b)
	return hex.EncodeToString(out[:])
}
