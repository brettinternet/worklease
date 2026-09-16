package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/guard"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func execAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		argv := cmd.Args().Slice()
		if len(argv) == 0 {
			return s.handle(cmd, reason.Invalid("exec requires a command after --"))
		}
		creds, svc, st, lock, h, hp, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		if lock != nil {
			defer lock.Close()
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if h != nil && h.PendingRequest != nil && !cmd.IsSet("request-not-after") {
			deadline = h.PendingRequest.RequestNotAfter
		}
		var pending *handle.PendingRequest
		if h != nil {
			pending = h.PendingRequest
		}
		op, err := operationID(cmd, pending)
		if err != nil {
			return s.handle(cmd, err)
		}
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = svc.DefaultTTL()
		}
		max := cmd.Duration("max-duration")
		if max == 0 {
			max = time.Hour
		}
		leaseExpiresAt := time.Time{}
		if h != nil && h.SchemaVersion == handle.RemoteSchemaVersion {
			leaseExpiresAt = h.ExpiresAt
		}
		result, err := guard.Exec(ctx, svc, creds, guard.ExecRequest{OperationID: op, Argv: argv, CWD: cmd.String("cwd"), GitPrimary: cmd.Bool("git-primary"), MaxDuration: max, TTL: ttl, RequestNotAfter: deadline, LeaseExpiresAt: leaseExpiresAt, Lifecycle: guardLifecycle(hp, h, lock)})
		if err != nil {
			return s.handle(cmd, mutationFailure(err, creds.ClaimID, op, hp))
		}
		fields := map[string]any{"receipt": result.Receipt, "exitCode": result.ExitCode, "operationId": result.Receipt.OperationID, "revision": result.Receipt.Revision}
		if s.jsonRequested(cmd) {
			if e := output.WriteSuccess(s.writer, "exec", fields); e != nil {
				return e
			}
		} else if e := writeExecText(s.writer, result.Receipt, result.ExitCode); e != nil {
			return e
		}
		if result.ExitCode != 0 {
			return urfave.Exit("", result.ExitCode)
		}
		return nil
	}
}

func replaceFileAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		path, content := strings.TrimSpace(cmd.String("path")), strings.TrimSpace(cmd.String("content-file"))
		if path == "" || content == "" {
			return s.handle(cmd, reason.Invalid("replace-file requires --path and --content-file"))
		}
		selected, err := profileSelection(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if selected.Profile != nil {
			return s.handle(cmd, reason.New(reason.ReasonOperationKindUnsupported, "replace-file is unavailable for remote authorities"))
		}
		creds, svc, st, lock, h, hp, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		if lock != nil {
			defer lock.Close()
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if h != nil && h.PendingRequest != nil && !cmd.IsSet("request-not-after") {
			deadline = h.PendingRequest.RequestNotAfter
		}
		var pending *handle.PendingRequest
		if h != nil {
			pending = h.PendingRequest
		}
		op, err := operationID(cmd, pending)
		if err != nil {
			return s.handle(cmd, err)
		}
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = svc.DefaultTTL()
		}
		requestHash := ""
		if pending != nil {
			requestHash = pending.RequestHash
		}
		local, ok := svc.(interface{ LocalService() *lease.Service })
		if !ok {
			return s.handle(cmd, reason.New(reason.ReasonOperationKindUnsupported, "replace-file requires the local authority"))
		}
		result, err := guard.ReplaceFile(ctx, local.LocalService(), creds, guard.ReplaceRequest{OperationID: op, Path: path, ExpectedSHA256: strings.ToLower(strings.TrimSpace(cmd.String("expected-sha256"))), ContentFile: content, TTL: ttl, RequestNotAfter: deadline, RequestHash: requestHash, Lifecycle: guardLifecycle(hp, h, lock)})
		if err != nil {
			return s.handle(cmd, mutationFailure(err, creds.ClaimID, op, hp))
		}
		fields := map[string]any{"receipt": result.Receipt, "operationId": result.Receipt.OperationID, "revision": result.Receipt.Revision}
		if s.jsonRequested(cmd) {
			return output.WriteSuccess(s.writer, "replace-file", fields)
		}
		return writeReplaceText(s.writer, result.Receipt)
	}
}

type hookEvent struct {
	CWD       string                     `json:"cwd"`
	ToolName  string                     `json:"tool_name"`
	ToolInput map[string]json.RawMessage `json:"tool_input"`
}

type lockAndStore struct {
	Closer *handle.Lock
	Store  *store.Store
}

func (c lockAndStore) Close() error {
	if c.Closer != nil {
		_ = c.Closer.Close()
	}
	if c.Store != nil {
		return c.Store.Close()
	}
	return nil
}

func guardLifecycle(path string, h *handle.Handle, locks ...*handle.Lock) *guard.OperationLifecycle {
	if h == nil {
		return nil
	}
	if h.SchemaVersion == handle.RemoteSchemaVersion {
		return &guard.OperationLifecycle{
			Prepare: func(intent lease.OperationIntent) error {
				if h.PendingRequest != nil && h.PendingRequest.OperationID != intent.OperationID {
					return reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
				}
				return nil
			},
		}
	}
	return &guard.OperationLifecycle{
		Prepare: func(intent lease.OperationIntent) error {
			hash, err := lease.OperationRequestHash(intent.Kind, h.AuthorityID, h.ClaimID, intent.Request, intent.TTL, intent.RequestNotAfter)
			if err != nil {
				return err
			}
			if h.RecoveryRequest != nil {
				return reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation")
			}
			if p := h.PendingRequest; p != nil {
				if p.OperationID != intent.OperationID || p.Kind != intent.Kind || p.RequestHash != hash {
					return reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
				}
				return nil
			}
			h.State = "pending"
			h.PendingRequest = &handle.PendingRequest{OperationID: intent.OperationID, Kind: intent.Kind, AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hash, RequestNotAfter: intent.RequestNotAfter, Inputs: intent.Request}
			if len(locks) > 0 && locks[0] != nil {
				return locks[0].Write(path, *h)
			}
			return handle.Write(path, *h)
		},
		Complete: func(receipt lease.Receipt) error { return finishHandleMutation(path, h, receipt, locks...) },
		Failure: func(err error, started bool) {
			// Once the started intent is committed the pending request is the
			// only exact record of that operation; keep it for recovery.
			if !started && isDefinitiveNoCommit(err) {
				clearPending(path, h, locks...)
			}
		},
	}
}

func hookTargets(ev hookEvent) ([]string, error) {
	names := map[string]string{"Edit": "file_path", "Write": "file_path", "MultiEdit": "file_path", "NotebookEdit": "notebook_path"}
	field, ok := names[ev.ToolName]
	if !ok {
		return nil, reason.New(reason.ReasonHookInputInvalid, "unsupported hook tool")
	}
	raw, ok := ev.ToolInput[field]
	if !ok {
		return nil, reason.New(reason.ReasonHookInputInvalid, "hook tool input has no target")
	}
	var p string
	if json.Unmarshal(raw, &p) != nil || strings.TrimSpace(p) == "" {
		return nil, reason.New(reason.ReasonHookInputInvalid, "hook target is invalid")
	}
	return []string{p}, nil
}
func verifyCreds(ctx context.Context, cmd *urfave.Command) (lease.Credentials, commandAuthority, io.Closer, *handle.Handle, error) {
	return verifyCredsAt(ctx, cmd, "")
}
func verifyCredsAt(ctx context.Context, cmd *urfave.Command, contextualCWD string) (lease.Credentials, commandAuthority, io.Closer, *handle.Handle, error) {
	if err := ValidateSelection(cmd, false); err != nil {
		return lease.Credentials{}, nil, nil, nil, err
	}
	backend, err := authorityFor(ctx, cmd, false)
	if err != nil {
		return lease.Credentials{}, nil, nil, nil, err
	}
	cfg := backend.Config
	explicit := strings.TrimSpace(cmd.String("claim-id")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision")
	if explicit {
		tok, e := tokenFromCommand(cmd, "token-file", "token-fd")
		if e != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, e
		}
		credentialPath := strings.TrimSpace(cmd.String("token-file"))
		if backend.Remote && credentialPath == "" {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, reason.New(reason.ReasonCredentialUnsafe, "remote guarded effects require a durable token file")
		}
		if credentialPath != "" {
			credentialPath, e = filepath.Abs(credentialPath)
			if e != nil {
				backend.Close()
				return lease.Credentials{}, nil, nil, nil, reason.New(reason.ReasonCredentialUnsafe, "credential path cannot be resolved")
			}
		}
		return lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: cmd.String("claim-id"), Token: tok, Revision: cmd.Int64("revision"), CredentialPath: credentialPath}, backend.API, backend, nil, nil
	}
	path := strings.TrimSpace(cmd.String("handle"))
	if ref := strings.TrimSpace(cmd.String("lease")); ref != "" {
		if len(ref) != 32 || strings.Trim(ref, "0123456789abcdef") != "" {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, reason.Invalid("lease reference must be 32 lowercase hex characters")
		}
		path = filepath.Join(cfg.Home, "handles", "mcp-"+ref+".json")
	}
	if path == "" && strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")) == "" && contextualCWD != "" {
		root, rootErr := handle.ContextRoot(contextualCWD, nil)
		if rootErr != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, rootErr
		}
		path = handle.ContextualPath(cfg.Home, root, cfg.SessionID)
	}
	if path == "" {
		path, err = acquireHandlePath(cmd, cfg)
		if err != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, err
		}
	}
	if backend.Remote {
		h, e := handle.Read(path)
		if e != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, reason.New(reason.ReasonVerifyFailed, "no contextual claim is available; run worklease acquire --path FILE").With("cause", "missing-handle")
		}
		if h.AuthorityID != backend.AuthorityID() {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match")
		}
		return lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: path, CredentialPath: backend.Profile.Credential.Path}, backend.API, backend, &h, nil
	}
	lock, e := handle.AcquireExistingLock(ctx, path+".lock")
	if e != nil {
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, e
	}
	h, e := lock.Read(path)
	if e != nil {
		lock.Close()
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, reason.New(reason.ReasonVerifyFailed, "no contextual claim is available; run worklease acquire --path FILE").With("cause", "missing-handle")
	}
	if h.AuthorityID != backend.AuthorityID() {
		lock.Close()
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match")
	}
	return lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: path}, backend.API, lockAndStore{Closer: lock, Store: backend.Store}, &h, nil
}
func configForCommand(cmd *urfave.Command) (config.Config, error) {
	return config.Load(config.Input{Flags: map[string]string{"home": cmd.String("home"), "agent": cmd.String("agent"), "session": cmd.String("session"), "ttl": cmd.String("ttl"), "poll_interval": cmd.String("poll-interval"), "config": cmd.String("config")}})
}
func storeForCommand(ctx context.Context, cfg config.Config, write bool) (*store.Store, error) {
	return store.Open(ctx, cfg.Home, store.Options{ReadOnly: !write})
}

func verifyAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if cmd.String("hook") != "" {
			return verifyHook(s, ctx, cmd)
		}
		creds, svc, closer, h, err := verifyCreds(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer closer.Close()
		if h != nil && h.PendingRequest != nil {
			return s.handle(cmd, reason.New(reason.ReasonVerifyFailed, "pending request is not ready").With("cause", "unknown-outcome-pending"))
		}
		v, err := svc.Verify(ctx, creds, cmd.StringSlice("resource"))
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "verify", map[string]any{"claim": v.Claim, "unknownOperations": v.UnknownOperations})
	}
}
func verifyHook(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	if strings.TrimSpace(cmd.String("hook")) != "claude-code" {
		return reasonOrHook(s, cmd, reason.New(reason.ReasonHookInputInvalid, "unsupported hook"))
	}
	data, e := io.ReadAll(io.LimitReader(os.Stdin, 1<<20+1))
	if e != nil || len(data) > 1<<20 {
		return reasonOrHook(s, cmd, reason.New(reason.ReasonHookInputInvalid, "hook input is invalid"))
	}
	var ev hookEvent
	dec := json.Unmarshal(data, &ev)
	if dec != nil || ev.ToolName == "" || ev.ToolInput == nil {
		return reasonOrHook(s, cmd, reason.New(reason.ReasonHookInputInvalid, "hook input is invalid"))
	}
	targets, e := hookTargets(ev)
	if e != nil {
		return reasonOrHook(s, cmd, e)
	}
	cwd := strings.TrimSpace(ev.CWD)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	creds, svc, closer, h, e := verifyCredsAt(ctx, cmd, cwd)
	if e != nil {
		return reasonOrHook(s, cmd, e)
	}
	defer closer.Close()
	if h != nil && h.PendingRequest != nil {
		return reasonOrHook(s, cmd, reason.New(reason.ReasonHookInputInvalid, "selected claim has a pending request"))
	}
	expected := []string{}
	if strings.ToLower(strings.TrimSpace(cmd.String("coverage"))) == "path" {
		for _, target := range targets {
			k, e := resource.Resolve(resource.Input{Path: target, WorkingDir: cwd})
			if e != nil {
				return reasonOrHook(s, cmd, reason.New(reason.ReasonHookInputInvalid, "hook target path is invalid"))
			}
			expected = append(expected, k.Resource)
		}
	} else if strings.TrimSpace(cmd.String("coverage")) != "" && strings.ToLower(cmd.String("coverage")) != "claim" {
		return reasonOrHook(s, cmd, reason.New(reason.ReasonHookInputInvalid, "coverage must be claim or path"))
	}
	if _, e = svc.Verify(ctx, creds, expected); e != nil {
		return reasonOrHook(s, cmd, e)
	}
	return nil
}
func reasonOrHook(s *boundary, cmd *urfave.Command, e error) error {
	if s.jsonRequested(cmd) {
		// Hook integrations are policy gates: malformed input, unavailable
		// authority, and ownership loss all block the edit with exit 2 even
		// though the JSON payload retains the precise diagnostic reason.
		_ = s.handle(cmd, e)
		return &handledError{cause: reason.New(reason.ReasonVerifyFailed, "hook verification blocked the edit")}
	}
	if s.errWriter != nil {
		_, _ = io.WriteString(s.errWriter, "blocked: "+output.Classify(e).Reason+"\n")
	}
	return urfave.Exit("", reason.ExitOwnership)
}
