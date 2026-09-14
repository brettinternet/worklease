// Package mcp implements Worklease's stdio MCP adapter.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
)

const (
	ModernVersion   = "2026-07-28"
	LegacyVersion   = "2025-11-25"
	maxMessageBytes = 4 << 20
	maxInFlight     = 8
	maxQueued       = 64
	shutdownWait    = 5 * time.Second
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Meta    json.RawMessage `json:"_meta,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type requestState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	cancelled bool
}

func (s *requestState) cancelRequest()     { s.mu.Lock(); s.cancelled = true; s.mu.Unlock(); s.cancel() }
func (s *requestState) wasCancelled() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.cancelled }

type Server struct {
	options      Options
	mu           sync.Mutex
	requests     map[string]*requestState
	seen         map[string]struct{}
	leases       map[string]*runtimeLease
	writerMu     sync.Mutex
	active       sync.WaitGroup
	slots        chan struct{}
	done         chan struct{}
	legacy       bool
	modern       bool
	legacyReady  chan struct{}
	serveCancel  context.CancelFunc
	inputMu      sync.Mutex
	input        io.Closer
	remote       authority.Authority
	remoteClient *authority.HTTPClient
	profile      *config.Profile
}

// Serve runs the newline-delimited stdio protocol. It never writes logs to stdout.
func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if input == nil || output == nil {
		return errors.New("MCP stdio requires input and output")
	}
	s.mu.Lock()
	s.requests = map[string]*requestState{}
	s.seen = map[string]struct{}{}
	s.mu.Unlock()
	s.slots = make(chan struct{}, maxInFlight)
	s.done = make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.serveCancel = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.serveCancel = nil; s.mu.Unlock() }()
	go func() { <-ctx.Done(); s.cancelAll() }()
	if closer, ok := input.(io.Closer); ok {
		s.inputMu.Lock()
		s.input = closer
		s.inputMu.Unlock()
		defer func() { _ = closer.Close() }()
	}
	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 64*1024), maxMessageBytes+1)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		scanErr <- scanner.Err()
	}()
	readDone := false
	for !readDone {
		var line []byte
		var scanErrValue error
		select {
		case line = <-lines:
		case scanErrValue = <-scanErr:
			readDone = true
		case <-ctx.Done():
			s.inputMu.Lock()
			if s.input != nil {
				_ = s.input.Close()
			}
			s.inputMu.Unlock()
			readDone = true
		}
		if readDone {
			if scanErrValue != nil {
				s.write(output, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32600, Message: "message is too large"}})
				cancel()
			}
			break
		}
		var req rpcRequest
		if len(line) > maxMessageBytes {
			s.write(output, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32600, Message: "message is too large"}})
			continue
		}
		if err := decodeRequest(line, &req); err != nil {
			s.write(output, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "invalid JSON-RPC request"}})
			continue
		}
		if len(req.ID) > 0 && string(req.ID) != "null" {
			if _, valid := canonicalID(req.ID); !valid {
				s.write(output, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32600, Message: "request ID must be a scalar"}})
				continue
			}
		}
		if req.Method == "notifications/cancelled" {
			s.cancelID(req.Params)
			continue
		}
		if req.Method == "notifications/initialized" {
			s.mu.Lock()
			if s.legacyReady != nil {
				close(s.legacyReady)
				s.legacyReady = nil
			}
			s.mu.Unlock()
			continue
		}
		if req.Method == "initialize" && len(req.ID) > 0 && string(req.ID) != "null" {
			id, _ := canonicalID(req.ID)
			state, reserveErr := s.reserve(ctx, id)
			if reserveErr != nil {
				s.write(output, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: reserveErr})
				continue
			}
			result, handleErr := s.handle(state.ctx, req)
			s.finish(id, state)
			if handleErr != nil {
				s.write(output, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: handleErr})
			} else {
				s.write(output, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
			}
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			// Tool notifications are rejected rather than bypassing admission and tracking.
			continue
		}
		id, valid := canonicalID(req.ID)
		if !valid {
			s.write(output, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32600, Message: "request ID must be a scalar"}})
			continue
		}
		state, err := s.reserve(ctx, id)
		if err != nil {
			s.write(output, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: err})
			continue
		}
		s.active.Add(1)
		go func() {
			defer s.active.Done()
			defer s.finish(id, state)
			select {
			case s.slots <- struct{}{}:
				defer func() { <-s.slots }()
				s.dispatch(ctx, output, req, state)
			case <-state.ctx.Done():
				if ctx.Err() == nil {
					s.write(output, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32800, Message: "request cancelled"}})
				}
			}
		}()
	}
	// EOF stops renewals immediately, then gives already-dispatched calls the bounded join window.
	s.stopAllRenewals()
	wait := make(chan struct{})
	go func() { s.active.Wait(); close(wait) }()
	select {
	case <-wait:
	case <-time.After(shutdownWait):
		s.cancelAll()
	}
	// A finishing acquire may have registered renewal after the first stop.
	s.stopAllRenewals()
	close(s.done)
	return nil
}

func decodeRequest(data []byte, req *rpcRequest) error {
	if duplicateKeys(data) != nil {
		return errors.New("duplicate JSON key")
	}
	dec := json.NewDecoder(bufio.NewReaderSize(bytesReader(data), len(data)+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		return errors.New("invalid request")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func duplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytesReader(data))
	var visit func() error
	visit = func() error {
		tok, e := dec.Token()
		if e != nil {
			return e
		}
		d, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch d {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				k, e := dec.Token()
				if e != nil {
					return e
				}
				ks, ok := k.(string)
				if !ok || seen[ks] {
					return errors.New("duplicate key")
				}
				seen[ks] = true
				if e = visit(); e != nil {
					return e
				}
			}
			_, e = dec.Token()
			return e
		case '[':
			for dec.More() {
				if e := visit(); e != nil {
					return e
				}
			}
			_, e = dec.Token()
			return e
		}
		return errors.New("invalid JSON")
	}
	if e := visit(); e != nil {
		return e
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

// bytesReader avoids retaining mutable scanner storage in a decoder.
type byteReader struct {
	b []byte
	i int
}

func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }
func (r *byteReader) Read(p []byte) (int, error) {
	if r.i == len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func (s *Server) reserve(parent context.Context, id string) (*requestState, *rpcError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return nil, &rpcError{Code: -32600, Message: "duplicate request ID"}
	}
	if len(s.requests) >= maxInFlight+maxQueued {
		return nil, &rpcError{Code: -32000, Message: "server queue is full"}
	}
	c, cancel := context.WithCancel(parent)
	st := &requestState{ctx: c, cancel: cancel}
	s.requests[id] = st
	s.seen[id] = struct{}{}
	return st, nil
}
func (s *Server) finish(id string, st *requestState) {
	st.cancel()
	s.mu.Lock()
	if s.requests[id] == st {
		delete(s.requests, id)
	}
	s.mu.Unlock()
}
func canonicalID(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytesReader(raw))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return "", false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return "", false
	}
	switch value.(type) {
	case string, json.Number:
		canonical, err := json.Marshal(value)
		return string(canonical), err == nil
	default:
		return "", false
	}
}

func (s *Server) cancelID(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	id, ok := canonicalID(p.RequestID)
	if !ok {
		return
	}
	s.mu.Lock()
	st := s.requests[id]
	s.mu.Unlock()
	if st != nil {
		st.cancelRequest()
	}
}
func (s *Server) cancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.requests {
		st.cancel()
	}
}
func (s *Server) write(w io.Writer, response rpcResponse) error {
	b, err := json.Marshal(response)
	if err != nil {
		return err
	}
	s.writerMu.Lock()
	_, err = fmt.Fprintf(w, "%s\n", b)
	s.writerMu.Unlock()
	if err != nil {
		s.mu.Lock()
		cancel := s.serveCancel
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return err
}
func (s *Server) dispatch(parent context.Context, w io.Writer, req rpcRequest, state *requestState) {
	if state != nil {
		s.mu.Lock()
		ready, legacy := s.legacyReady, s.legacy
		s.mu.Unlock()
		if legacy && req.Method != "initialize" && ready != nil {
			select {
			case <-ready:
			case <-state.ctx.Done():
				s.write(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32800, Message: "request cancelled"}})
				return
			}
		}
	}
	ctx := parent
	if state != nil {
		ctx = state.ctx
	}
	result, err := s.handle(ctx, req)
	if len(req.ID) == 0 || req.ID == nil {
		return
	}
	if state != nil && state.wasCancelled() {
		s.write(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32800, Message: "request cancelled"}})
		return
	}
	if err != nil {
		s.write(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: err})
	} else {
		s.write(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
	}
}

func protocolError(code int, message string, data any) *rpcError {
	return &rpcError{Code: code, Message: message, Data: data}
}
