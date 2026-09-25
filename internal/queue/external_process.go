package queue

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/config"
)

const (
	externalFrameLimit       = 1 << 20
	externalStderrLimit      = 64 << 10
	externalDiagnosticLimit  = 4 << 10
	externalResultLimit      = 786432
	externalMaxItems         = 100
	externalMaxInFlight      = 16
	externalRequestTimeout   = 30 * time.Second
	externalCancelWriteLimit = 100 * time.Millisecond
	externalCancelGrace      = 250 * time.Millisecond
)

var externalMethods = map[string]bool{
	"resolve": true, "capabilities": true, "list": true, "readItems": true,
	"readDependencies": true, "changes": true, "readItem": true,
	"resourcePolicy": true, "writeState": true, "recordProgress": true,
	"assign": true, "readReceipt": true, "resolveReviewBoundary": true, "archive": true,
}

var externalDiagnostics = map[string]bool{
	"parse-error": true, "invalid-request": true, "method-not-found": true,
	"invalid-params": true, "internal-error": true, "unsupported-capability": true,
	"authentication-required": true, "authentication-failed": true,
	"authorization-denied": true, "conflict": true, "rate-limited": true,
	"unavailable-source": true, "incomplete-graph": true, "unknown-outcome": true,
}

type ExternalAdapterManifest struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Protocol struct {
		MinMajor int `json:"minMajor"`
		MaxMajor int `json:"maxMajor"`
	} `json:"protocol"`
	ConfigSchema     json.RawMessage `json:"configSchema"`
	Authentication   []string        `json:"authentication"`
	ResourcePolicy   string          `json:"resourcePolicy"`
	Capabilities     []string        `json:"capabilities"`
	RequiredFeatures []string        `json:"requiredFeatures"`
	Raw              json.RawMessage `json:"-"`
}

type externalProcessRun struct {
	cmd         *exec.Cmd
	snapshot    *config.QueueAdapterLaunchSnapshot
	stdin       *os.File
	stdout      *os.File
	stderr      *os.File
	writeMu     sync.Mutex
	stderrMu    sync.Mutex
	stderrData  []byte
	stderrDone  chan struct{}
	waitDone    chan struct{}
	dead        atomic.Bool
	failOnce    sync.Once
	initialized bool
	generation  uint64
}

type externalResponse struct {
	result json.RawMessage
	err    error
	run    *externalProcessRun
}

type externalPending struct {
	id         string
	method     string
	run        *externalProcessRun
	result     chan externalResponse
	cancelled  bool
	dispatched atomic.Bool
}

// ExternalProcess supervises one approved external adapter for exactly one queue source.
// Calls are correlated by ID and bounded to 16 in-flight requests; a failed process is
// restarted only for a later call, never to replay the failed request.
type ExternalProcess struct {
	source config.QueueSource
	env    func(string) string

	mu            sync.Mutex
	run           *externalProcessRun
	closed        bool
	pending       map[string]*externalPending
	answeredIDs   map[string]bool
	nextID        uint64
	slots         chan struct{}
	restartAt     time.Time
	restartFail   int
	terminalErr   string
	manifest      *ExternalAdapterManifest
	runGeneration uint64

	initializeMu sync.Mutex
	secrets      []string
	beforeStart  func()
}

// NewExternalProcess verifies owner approval against a private executable snapshot before starting it.
// The returned client has started its process but negotiates protocol v1 on Initialize or the first Call.
func NewExternalProcess(source config.QueueSource, env func(string) string) (*ExternalProcess, error) {
	return newExternalProcess(source, env, nil)
}

func newExternalProcess(source config.QueueSource, env func(string) string, beforeStart func()) (*ExternalProcess, error) {
	if env == nil {
		env = os.Getenv
	}
	encoded, err := json.Marshal(source.Config)
	if err != nil {
		return nil, fmt.Errorf("external adapter source configuration is invalid")
	}
	if len(source.Config) != 0 {
		var cloned map[string]any
		if err := json.Unmarshal(encoded, &cloned); err != nil {
			return nil, fmt.Errorf("external adapter source configuration is invalid")
		}
		source.Config = cloned
	}
	client := &ExternalProcess{
		source:      source,
		env:         env,
		pending:     make(map[string]*externalPending),
		answeredIDs: make(map[string]bool),
		slots:       make(chan struct{}, externalMaxInFlight),
		beforeStart: beforeStart,
	}
	client.secrets = externalSecretValues(source, env)
	if _, err := client.startProcess(); err != nil {
		return nil, err
	}
	return client, nil
}

// Initialize performs v1 negotiation and returns the verified adapter manifest.
func (p *ExternalProcess) Initialize(ctx context.Context) (ExternalAdapterManifest, error) {
	ctx, cancel := boundedExternalContext(ctx)
	defer cancel()
	run, err := p.ensureInitialized(ctx)
	if err != nil {
		return ExternalAdapterManifest{}, err
	}
	p.mu.Lock()
	manifest := p.manifest
	current := p.run == run && !run.dead.Load()
	p.mu.Unlock()
	if !current || manifest == nil {
		return ExternalAdapterManifest{}, fmt.Errorf("external adapter initialization was interrupted")
	}
	return cloneExternalManifest(*manifest), nil
}

// Manifest returns the most recently negotiated manifest, if Initialize has completed.
func (p *ExternalProcess) Manifest() (ExternalAdapterManifest, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.manifest == nil {
		return ExternalAdapterManifest{}, false
	}
	return cloneExternalManifest(*p.manifest), true
}

// Generation identifies the currently running process for adapter-local binding state.
func (p *ExternalProcess) Generation() (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil || p.run.dead.Load() {
		return 0, false
	}
	return p.run.generation, true
}

// Call makes one source-scoped protocol request. initialize is reserved for Initialize.
func (p *ExternalProcess) Call(ctx context.Context, method string, params any, result any) error {
	if !externalMethods[method] {
		return fmt.Errorf("external adapter method is not supported")
	}
	ctx, cancel := boundedExternalContext(ctx)
	defer cancel()
	if _, err := p.ensureInitialized(ctx); err != nil {
		return externalContextError(method, err, false)
	}
	p.initializeMu.Lock()
	run, err := p.ensureInitializedLocked(ctx)
	p.initializeMu.Unlock()
	if err != nil {
		return externalContextError(method, err, false)
	}
	wireParams, requestDeadline, err := p.normalizeParams(method, params, ctx)
	if err != nil {
		return err
	}
	callCtx, callCancel := context.WithDeadline(ctx, requestDeadline)
	defer callCancel()
	response, dispatched, err := p.callRun(callCtx, run, method, wireParams)
	if err != nil {
		return externalContextError(method, err, dispatched)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(response, result); err != nil {
		p.failProcess(run, "invalid result schema", false)
		if isExternalMutation(method) && dispatched {
			return fmt.Errorf("external adapter write outcome is unknown after dispatch")
		}
		return fmt.Errorf("external adapter returned an invalid result")
	}
	return nil
}

// Close terminates the source's process and fails any outstanding calls.
func (p *ExternalProcess) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	run := p.run
	p.mu.Unlock()
	if run == nil {
		return
	}
	p.failProcess(run, "client closed", false)
	select {
	case <-run.waitDone:
	case <-time.After(time.Second):
	}
}

func (p *ExternalProcess) ensureInitialized(ctx context.Context) (*externalProcessRun, error) {
	p.initializeMu.Lock()
	defer p.initializeMu.Unlock()
	return p.ensureInitializedLocked(ctx)
}

func (p *ExternalProcess) ensureInitializedLocked(ctx context.Context) (*externalProcessRun, error) {
	run, err := p.ensureRunning(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	initialized := run.initialized
	p.mu.Unlock()
	if initialized {
		return run, nil
	}
	var raw json.RawMessage
	params := map[string]any{"protocolMajors": []int{1}, "hostFeatures": []string{}}
	_, dispatched, err := p.callRun(ctx, run, "initialize", params, &raw)
	if err != nil {
		if dispatched {
			p.failProcess(run, "adapter initialization failed", true)
		}
		return nil, err
	}
	manifest, err := validateExternalManifest(raw, p.source)
	if err != nil {
		p.failProcess(run, "invalid adapter manifest", false)
		return nil, p.errorWithStderr(run, "invalid adapter manifest")
	}
	p.mu.Lock()
	if p.closed || p.run != run || run.dead.Load() {
		p.mu.Unlock()
		return nil, fmt.Errorf("external adapter initialization was interrupted")
	}
	run.initialized = true
	copy := cloneExternalManifest(manifest)
	p.manifest = &copy
	p.mu.Unlock()
	return run, nil
}

func (p *ExternalProcess) ensureRunning(ctx context.Context) (*externalProcessRun, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("external adapter client is closed")
	}
	if p.terminalErr != "" {
		run := p.run
		message := p.terminalErr
		p.mu.Unlock()
		if run != nil {
			return nil, p.errorWithStderr(run, message)
		}
		return nil, fmt.Errorf("%s", message)
	}
	if p.run != nil && !p.run.dead.Load() {
		run := p.run
		p.mu.Unlock()
		return run, nil
	}
	waitUntil := p.restartAt
	p.mu.Unlock()
	if delay := time.Until(waitUntil); delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("external adapter client is closed")
	}
	if p.terminalErr != "" {
		run := p.run
		message := p.terminalErr
		p.mu.Unlock()
		if run != nil {
			return nil, p.errorWithStderr(run, message)
		}
		return nil, fmt.Errorf("%s", message)
	}
	if p.run != nil && !p.run.dead.Load() {
		run := p.run
		p.mu.Unlock()
		return run, nil
	}
	run, err := p.startProcessLocked()
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return run, nil
}

// startProcess is only used during construction; later starts are serialized by mu.
func (p *ExternalProcess) startProcess() (*externalProcessRun, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startProcessLocked()
}

func (p *ExternalProcess) startProcessLocked() (*externalProcessRun, error) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("external adapter process could not start")
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		return nil, fmt.Errorf("external adapter process could not start")
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdinR.Close()
		stdinW.Close()
		stdoutR.Close()
		stdoutW.Close()
		return nil, fmt.Errorf("external adapter process could not start")
	}
	snapshot, err := config.PrepareQueueAdapterLaunch(p.env, p.source)
	if err != nil {
		closeExternalFiles(stdinR, stdinW, stdoutR, stdoutW, stderrR, stderrW)
		return nil, err
	}
	cmd := exec.Command(snapshot.Path())
	prepareExternalProcess(cmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
	cmd.Env = externalAdapterEnvironment(p.env)
	if p.beforeStart != nil {
		p.beforeStart()
	}
	if err := cmd.Start(); err != nil {
		_ = snapshot.Close()
		closeExternalFiles(stdinR, stdinW, stdoutR, stdoutW, stderrR, stderrW)
		return nil, fmt.Errorf("external adapter process could not start")
	}
	_ = stdinR.Close()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	p.runGeneration++
	run := &externalProcessRun{
		cmd:        cmd,
		snapshot:   snapshot,
		stdin:      stdinW,
		stdout:     stdoutR,
		stderr:     stderrR,
		stderrDone: make(chan struct{}),
		waitDone:   make(chan struct{}),
		generation: p.runGeneration,
	}
	p.run = run
	go p.readResponses(run)
	go p.drainStderr(run)
	go func() {
		_ = cmd.Wait()
		p.failProcess(run, "adapter process exited", true)
		_ = run.snapshot.Close()
		close(run.waitDone)
	}()
	return run, nil
}

func (p *ExternalProcess) callRun(ctx context.Context, run *externalProcessRun, method string, params any, result ...any) (json.RawMessage, bool, error) {
	if err := acquireExternalSlot(ctx, p.slots); err != nil {
		return nil, false, err
	}
	if run.dead.Load() {
		<-p.slots
		return nil, false, fmt.Errorf("adapter process unavailable")
	}
	id := p.allocateID()
	paramsJSON, err := json.Marshal(params)
	if err != nil || !json.Valid(paramsJSON) {
		<-p.slots
		return nil, false, fmt.Errorf("external adapter request parameters are invalid")
	}
	request := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}{"2.0", id, method, paramsJSON}
	frame, err := json.Marshal(request)
	if err != nil || len(frame)+1 > externalFrameLimit {
		<-p.slots
		return nil, false, fmt.Errorf("external adapter request exceeds the frame limit")
	}
	frame = append(frame, '\n')
	pending := &externalPending{id: id, method: method, run: run, result: make(chan externalResponse, 1)}
	p.mu.Lock()
	if p.closed || p.run != run || run.dead.Load() {
		p.mu.Unlock()
		<-p.slots
		return nil, false, fmt.Errorf("adapter process unavailable")
	}
	p.pending[id] = pending
	p.mu.Unlock()

	writeDone := make(chan error, 1)
	go func() { writeDone <- p.writeFrame(run, frame, pending) }()
	select {
	case writeErr := <-writeDone:
		if writeErr != nil {
			p.failProcess(run, "adapter request could not be sent", true)
			return nil, pending.dispatched.Load(), p.errorWithStderr(run, "adapter request could not be sent")
		}
	case <-ctx.Done():
		writeCompleted := false
		stillPending := false
		select {
		case writeErr := <-writeDone:
			writeCompleted = writeErr == nil
			if writeErr == nil {
				p.mu.Lock()
				stillPending = p.pending[id] == pending
				if stillPending {
					pending.cancelled = true
				}
				p.mu.Unlock()
				if stillPending {
					p.cancelRequest(run, pending)
				}
			} else {
				p.failProcess(run, "adapter request could not be sent", true)
			}
		case <-time.After(time.Millisecond):
			// The frame may have been partially written. Stop the process rather than
			// leaving a truncated request in its input stream.
			p.failProcess(run, "request cancelled during dispatch", true)
		}
		if writeCompleted && !stillPending {
			select {
			case response := <-pending.result:
				if response.err != nil {
					return nil, pending.dispatched.Load(), p.errorWithStderr(run, response.err.Error())
				}
				if len(result) != 0 && json.Unmarshal(response.result, result[0]) != nil {
					p.failProcess(run, "invalid result envelope", false)
					return nil, pending.dispatched.Load(), p.errorWithStderr(run, "invalid result envelope")
				}
				return response.result, pending.dispatched.Load(), nil
			default:
			}
		}
		return nil, pending.dispatched.Load(), ctx.Err()
	}

	select {
	case response := <-pending.result:
		if response.err != nil {
			return nil, pending.dispatched.Load(), p.errorWithStderr(run, response.err.Error())
		}
		if len(result) != 0 {
			if err := json.Unmarshal(response.result, result[0]); err != nil {
				p.failProcess(run, "invalid result envelope", false)
				return nil, pending.dispatched.Load(), p.errorWithStderr(run, "invalid result envelope")
			}
		}
		if method != "initialize" {
			p.mu.Lock()
			p.restartFail = 0
			p.mu.Unlock()
		}
		return response.result, pending.dispatched.Load(), nil
	case <-ctx.Done():
		select {
		case response := <-pending.result:
			if response.err != nil {
				return nil, pending.dispatched.Load(), p.errorWithStderr(run, response.err.Error())
			}
			if len(result) != 0 {
				if err := json.Unmarshal(response.result, result[0]); err != nil {
					p.failProcess(run, "invalid result envelope", false)
					return nil, pending.dispatched.Load(), fmt.Errorf("external adapter returned an invalid result")
				}
			}
			if method != "initialize" {
				p.mu.Lock()
				p.restartFail = 0
				p.mu.Unlock()
			}
			return response.result, pending.dispatched.Load(), nil
		default:
		}
		p.mu.Lock()
		stillPending := p.pending[id] == pending
		if stillPending {
			pending.cancelled = true
		}
		p.mu.Unlock()
		if stillPending {
			p.cancelRequest(run, pending)
		}
		return nil, pending.dispatched.Load(), ctx.Err()
	}
}

func (p *ExternalProcess) writeFrame(run *externalProcessRun, frame []byte, pending *externalPending) error {
	run.writeMu.Lock()
	defer run.writeMu.Unlock()
	if run.dead.Load() {
		return io.ErrClosedPipe
	}
	for len(frame) > 0 {
		n, err := run.stdin.Write(frame)
		if n > 0 {
			if pending != nil {
				pending.dispatched.Store(true)
			}
			frame = frame[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func (p *ExternalProcess) cancelRequest(run *externalProcessRun, pending *externalPending) {
	if !pending.dispatched.Load() || run.dead.Load() {
		return
	}
	frame, _ := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  struct {
			ID string `json:"id"`
		} `json:"params"`
	}{JSONRPC: "2.0", Method: "$/cancelRequest", Params: struct {
		ID string `json:"id"`
	}{ID: pending.id}})
	frame = append(frame, '\n')
	written := make(chan error, 1)
	go func() { written <- p.writeFrame(run, frame, nil) }()
	select {
	case err := <-written:
		if err != nil {
			p.failProcess(run, "cancel notification could not be sent", true)
			return
		}
	case <-time.After(externalCancelWriteLimit):
		p.failProcess(run, "cancel notification could not be sent", true)
		return
	}
	time.AfterFunc(externalCancelGrace, func() {
		p.mu.Lock()
		stillPending := p.pending[pending.id] == pending
		p.mu.Unlock()
		if stillPending {
			p.failProcess(run, "adapter did not stop cancelled request", true)
		}
	})
}

func (p *ExternalProcess) readResponses(run *externalProcessRun) {
	reader := bufio.NewReaderSize(run.stdout, externalFrameLimit+1)
	for {
		frame, err := readExternalFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				p.failProcess(run, "adapter process exited", true)
			} else {
				p.failProcess(run, "adapter protocol framing failed", false)
			}
			return
		}
		id, result, diagnostic, err := parseExternalResponse(frame)
		if err != nil {
			p.failProcess(run, "adapter protocol response is invalid", false)
			return
		}
		p.mu.Lock()
		pending := p.pending[id]
		if pending == nil {
			duplicate := p.answeredIDs[id]
			p.mu.Unlock()
			if duplicate {
				p.failProcess(run, "adapter sent a duplicate response", false)
			} else {
				p.failProcess(run, "adapter sent an unsolicited response", false)
			}
			return
		}
		if pending.run != run {
			p.mu.Unlock()
			p.failProcess(run, "adapter response ID is invalid", false)
			return
		}
		if diagnostic == "" && pending.method != "initialize" && !externalResultSourceScoped(result, pending.method, p.source.ID) {
			p.mu.Unlock()
			p.failProcess(run, "adapter result escaped its configured source", false)
			return
		}
		delete(p.pending, id)
		p.answeredIDs[id] = true
		cancelled := pending.cancelled
		p.mu.Unlock()
		<-p.slots
		if cancelled {
			continue
		}
		if diagnostic != "" {
			pending.result <- externalResponse{err: fmt.Errorf("adapter returned diagnostic %s", diagnostic), run: run}
		} else {
			pending.result <- externalResponse{result: result, run: run}
		}
	}
}

func (p *ExternalProcess) drainStderr(run *externalProcessRun) {
	defer close(run.stderrDone)
	buffer := make([]byte, 4096)
	for {
		n, err := run.stderr.Read(buffer)
		if n > 0 {
			run.stderrMu.Lock()
			remaining := externalStderrLimit - len(run.stderrData)
			if remaining > 0 {
				if n > remaining {
					n = remaining
				}
				run.stderrData = append(run.stderrData, buffer[:n]...)
			}
			run.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (p *ExternalProcess) failProcess(run *externalProcessRun, message string, restartable bool) {
	run.failOnce.Do(func() {
		run.dead.Store(true)
		p.mu.Lock()
		if p.run == run && !p.closed {
			if restartable {
				p.restartFail++
				if p.restartFail > 6 {
					p.restartFail = 6
				}
				backoff := 100 * time.Millisecond * time.Duration(1<<(p.restartFail-1))
				p.restartAt = time.Now().Add(backoff)
			} else {
				p.terminalErr = message
			}
		}
		pending := make([]*externalPending, 0)
		for id, call := range p.pending {
			if call.run == run {
				delete(p.pending, id)
				pending = append(pending, call)
			}
		}
		p.mu.Unlock()

		_ = run.stdin.Close()
		_ = run.stdout.Close()
		_ = run.stderr.Close()
		terminateExternalProcess(run.cmd)
		for _, call := range pending {
			<-p.slots
			call.result <- externalResponse{err: fmt.Errorf("%s", message), run: run}
		}
	})
}

func (p *ExternalProcess) errorWithStderr(run *externalProcessRun, message string) error {
	select {
	case <-run.stderrDone:
	case <-time.After(50 * time.Millisecond):
	}
	run.stderrMu.Lock()
	stderr := append([]byte(nil), run.stderrData...)
	run.stderrMu.Unlock()
	diagnostic := redactExternalText(string(stderr), p.secrets)
	for _, secret := range p.secrets {
		if len(secret) > externalDiagnosticLimit {
			diagnostic = ""
			break
		}
	}
	if diagnostic == "" {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s (adapter stderr: %s)", message, diagnostic)
}

func (p *ExternalProcess) allocateID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	return strconv.FormatUint(p.nextID, 10)
}

func (p *ExternalProcess) normalizeParams(method string, params any, ctx context.Context) (map[string]json.RawMessage, time.Time, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("external adapter request parameters are invalid")
	}
	if rejectExternalDuplicateKeys(encoded) != nil {
		return nil, time.Time{}, fmt.Errorf("external adapter request parameters are invalid")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		return nil, time.Time{}, fmt.Errorf("external adapter request parameters must be an object")
	}
	if method == "resolve" {
		if _, exists := object["credentialRef"]; !exists && p.source.CredentialRef != "" {
			object["credentialRef"], _ = json.Marshal(p.source.CredentialRef)
		}
	}
	if err := validateExternalScope(object, p.source.ID, method, p.source.CredentialRef); err != nil {
		return nil, time.Time{}, err
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, time.Time{}, fmt.Errorf("external adapter request deadline is missing")
	}
	if raw, exists := object["deadline"]; exists {
		var provided string
		if json.Unmarshal(raw, &provided) != nil || !strings.HasSuffix(provided, "Z") {
			return nil, time.Time{}, fmt.Errorf("external adapter request deadline is invalid")
		}
		parsed, err := time.Parse(time.RFC3339Nano, provided)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("external adapter request deadline is invalid")
		}
		if parsed.Before(deadline) {
			deadline = parsed
		}
	}
	if !deadline.After(time.Now()) {
		return nil, time.Time{}, context.DeadlineExceeded
	}
	object["deadline"], _ = json.Marshal(deadline.UTC().Format(time.RFC3339Nano))
	if _, exists := object["budget"]; !exists {
		object["budget"] = json.RawMessage(`{"maxItems":100,"maxBytes":786432}`)
	}
	if err := validateExternalBudget(object["budget"]); err != nil {
		return nil, time.Time{}, err
	}
	encoded, err = json.Marshal(object)
	if err != nil || !validExternalCollections(encoded) {
		return nil, time.Time{}, fmt.Errorf("external adapter request collection exceeds protocol limits")
	}
	return object, deadline, nil
}

func validateExternalScope(params map[string]json.RawMessage, sourceID, method, credentialRef string) error {
	foundSource := false
	var walk func(json.RawMessage, bool) error
	walk = func(raw json.RawMessage, root bool) error {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil && object != nil {
			for childKey, child := range object {
				if childKey == "sourceId" {
					var value string
					if json.Unmarshal(child, &value) != nil || value != sourceID {
						return fmt.Errorf("external adapter request escaped its configured source")
					}
					foundSource = true
				}
				if childKey == "credentialRef" {
					var value string
					if method != "resolve" || !root || credentialRef == "" || json.Unmarshal(child, &value) != nil || value != credentialRef {
						return fmt.Errorf("credential references are restricted to their configured resolve request")
					}
				}
				if isExternalCredentialField(childKey) {
					return fmt.Errorf("raw credentials are not permitted in external adapter parameters")
				}
				if err := walk(child, false); err != nil {
					return err
				}
			}
			return nil
		}
		var children []json.RawMessage
		if json.Unmarshal(raw, &children) == nil {
			for _, child := range children {
				if err := walk(child, false); err != nil {
					return err
				}
			}
			return nil
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			parsed, err := url.Parse(text)
			if err == nil {
				if parsed.User != nil {
					return fmt.Errorf("raw credentials are not permitted in external adapter parameters")
				}
				for key := range parsed.Query() {
					if isExternalCredentialField(key) {
						return fmt.Errorf("raw credentials are not permitted in external adapter parameters")
					}
				}
			}
		}
		return nil
	}
	if err := walk(mustMarshalRawObject(params), true); err != nil {
		return err
	}
	if !foundSource {
		return fmt.Errorf("external adapter request is not source-scoped")
	}
	switch method {
	case "readDependencies", "readItem", "resourcePolicy", "writeState", "recordProgress", "assign":
		if !externalRefIsScoped(params["ref"], sourceID) {
			return fmt.Errorf("external adapter request is not source-scoped")
		}
	case "readItems":
		var refs []json.RawMessage
		if json.Unmarshal(params["refs"], &refs) != nil || len(refs) == 0 || len(refs) > externalMaxItems {
			return fmt.Errorf("external adapter request references are invalid")
		}
		for _, ref := range refs {
			if !externalRefIsScoped(ref, sourceID) {
				return fmt.Errorf("external adapter request escaped its configured source")
			}
		}
	default:
		var value string
		if json.Unmarshal(params["sourceId"], &value) != nil || value != sourceID {
			return fmt.Errorf("external adapter request is not source-scoped")
		}
	}
	return nil
}

func externalResultSourceScoped(raw json.RawMessage, method, sourceID string) bool {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	valid := true
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				if key == "sourceId" {
					id, ok := child.(string)
					if !ok || id != sourceID {
						valid = false
					}
				}
				if key == "itemId" || key == "cursor" || key == "title" || key == "diagnostic" {
					text, ok := child.(string)
					if ok && (len(text) > 4096 || !utf8.ValidString(text)) {
						valid = false
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	if !valid {
		return false
	}
	if method == "resolve" {
		var result struct {
			Source struct {
				ID string `json:"id"`
			} `json:"source"`
		}
		return json.Unmarshal(raw, &result) == nil && result.Source.ID == sourceID
	}
	return true
}

func externalRefIsScoped(raw json.RawMessage, sourceID string) bool {
	var ref struct {
		SourceID string `json:"sourceId"`
		ItemID   string `json:"itemId"`
	}
	return json.Unmarshal(raw, &ref) == nil && ref.SourceID == sourceID && ref.ItemID != "" && utf8.ValidString(ref.ItemID) && len(ref.ItemID) <= 4096
}

func isExternalCredentialField(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(key))
	if key == "credentialref" || strings.HasSuffix(key, "authorizationref") {
		return false
	}
	for _, marker := range []string{"password", "passwd", "secret", "token", "apikey", "accesskey", "privatekey", "credential", "authorization"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return key == "auth" || key == "credentials"
}

func mustMarshalRawObject(value map[string]json.RawMessage) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func validateExternalBudget(raw json.RawMessage) error {
	var budget struct {
		MaxItems int `json:"maxItems"`
		MaxBytes int `json:"maxBytes"`
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil || json.Unmarshal(raw, &budget) != nil ||
		budget.MaxItems < 1 || budget.MaxItems > externalMaxItems || budget.MaxBytes < 1 || budget.MaxBytes > externalResultLimit ||
		values["maxItems"] == nil || values["maxBytes"] == nil || len(values) != 2 {
		return fmt.Errorf("external adapter request budget is invalid or exceeds protocol limits")
	}
	return nil
}

func validateExternalManifest(raw json.RawMessage, source config.QueueSource) (ExternalAdapterManifest, error) {
	if len(raw) == 0 || len(raw) > externalResultLimit {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	for _, field := range []string{"protocolVersion", "manifest"} {
		if fields[field] == nil {
			return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
		}
	}
	var protocolVersion int
	if json.Unmarshal(fields["protocolVersion"], &protocolVersion) != nil || protocolVersion != 1 {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	var manifestFields map[string]json.RawMessage
	if json.Unmarshal(fields["manifest"], &manifestFields) != nil || manifestFields == nil {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	for _, field := range []string{"id", "version", "protocol", "configSchema", "authentication", "resourcePolicy", "capabilities", "requiredFeatures"} {
		if manifestFields[field] == nil {
			return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
		}
	}
	var manifest ExternalAdapterManifest
	if json.Unmarshal(fields["manifest"], &manifest) != nil ||
		manifest.ID != source.ExpectedAdapterID || manifest.Version != source.ExpectedVersion ||
		manifest.Protocol.MinMajor > 1 || manifest.Protocol.MaxMajor < 1 ||
		manifest.Protocol.MinMajor < 1 || manifest.Protocol.MaxMajor < manifest.Protocol.MinMajor ||
		manifest.ResourcePolicy != "generic" || len(manifest.RequiredFeatures) != 0 {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	var schema map[string]json.RawMessage
	if json.Unmarshal(manifest.ConfigSchema, &schema) != nil || schema == nil {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	for key, target := range map[string]any{
		"authentication":   &manifest.Authentication,
		"capabilities":     &manifest.Capabilities,
		"requiredFeatures": &manifest.RequiredFeatures,
	} {
		if len(manifestFields[key]) == 0 || manifestFields[key][0] != '[' || json.Unmarshal(manifestFields[key], target) != nil {
			return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
		}
	}
	if len(manifest.RequiredFeatures) != 0 || hasDuplicateStrings(manifest.Capabilities) || hasDuplicateStrings(manifest.RequiredFeatures) || !validExternalObjectKeys(manifestFields["protocol"], "minMajor", "maxMajor") {
		return ExternalAdapterManifest{}, fmt.Errorf("invalid manifest")
	}
	manifest.Raw = append(json.RawMessage(nil), fields["manifest"]...)
	return manifest, nil
}

func parseExternalResponse(frame []byte) (string, json.RawMessage, string, error) {
	if len(frame) < 2 || frame[len(frame)-1] != '\n' {
		return "", nil, "", fmt.Errorf("invalid frame")
	}
	data := frame[:len(frame)-1]
	if len(data) == 0 || bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) || !utf8.Valid(data) {
		return "", nil, "", fmt.Errorf("invalid frame")
	}
	var compact bytes.Buffer
	if json.Compact(&compact, data) != nil || !bytes.Equal(compact.Bytes(), data) || rejectExternalDuplicateKeys(data) != nil {
		return "", nil, "", fmt.Errorf("invalid JSON")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return "", nil, "", fmt.Errorf("response must be an object")
	}
	for key := range object {
		if key != "jsonrpc" && key != "id" && key != "result" && key != "error" {
			return "", nil, "", fmt.Errorf("unknown response envelope field")
		}
	}
	var version, id string
	if json.Unmarshal(object["jsonrpc"], &version) != nil || version != "2.0" ||
		json.Unmarshal(object["id"], &id) != nil || id == "" || len(id) > 128 {
		return "", nil, "", fmt.Errorf("invalid response envelope")
	}
	result, hasResult := object["result"]
	errorRaw, hasError := object["error"]
	if hasResult == hasError {
		return "", nil, "", fmt.Errorf("invalid response envelope")
	}
	if hasResult {
		if len(result) == 0 || bytes.Equal(result, []byte("null")) || len(result) > externalResultLimit || !validExternalCollections(result) {
			return "", nil, "", fmt.Errorf("invalid response result")
		}
		var resultObject map[string]json.RawMessage
		if json.Unmarshal(result, &resultObject) != nil || resultObject == nil {
			return "", nil, "", fmt.Errorf("result must be an object")
		}
		return id, append(json.RawMessage(nil), result...), "", nil
	}
	diagnostic, err := parseExternalDiagnostic(errorRaw)
	if err != nil {
		return "", nil, "", err
	}
	return id, nil, diagnostic, nil
}

func parseExternalDiagnostic(raw json.RawMessage) (string, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil || envelope == nil || envelope["code"] == nil || envelope["message"] == nil || envelope["data"] == nil {
		return "", fmt.Errorf("invalid error response")
	}
	for key := range envelope {
		if key != "code" && key != "message" && key != "data" {
			return "", fmt.Errorf("invalid error response")
		}
	}
	var code int
	var message string
	if json.Unmarshal(envelope["code"], &code) != nil || json.Unmarshal(envelope["message"], &message) != nil || len(message) > 4096 || !utf8.ValidString(message) {
		return "", fmt.Errorf("invalid error response")
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(envelope["data"], &data) != nil || data == nil || data["diagnostic"] == nil {
		return "", fmt.Errorf("invalid error response")
	}
	for key := range data {
		if key != "diagnostic" && key != "retryAt" && key != "providerCode" {
			return "", fmt.Errorf("invalid error response")
		}
	}
	var diagnostic string
	if json.Unmarshal(data["diagnostic"], &diagnostic) != nil || !externalDiagnostics[diagnostic] || externalDiagnosticCode(diagnostic) != code {
		return "", fmt.Errorf("invalid error diagnostic")
	}
	if rawRetry, ok := data["retryAt"]; ok {
		var retryAt string
		if json.Unmarshal(rawRetry, &retryAt) != nil || !strings.HasSuffix(retryAt, "Z") {
			return "", fmt.Errorf("invalid retryAt")
		}
		if _, err := time.Parse(time.RFC3339Nano, retryAt); err != nil {
			return "", fmt.Errorf("invalid retryAt")
		}
	}
	if rawCode, ok := data["providerCode"]; ok {
		var providerCode string
		if json.Unmarshal(rawCode, &providerCode) != nil || len(providerCode) > 4096 || !utf8.ValidString(providerCode) {
			return "", fmt.Errorf("invalid provider code")
		}
	}
	return diagnostic, nil
}

func externalDiagnosticCode(diagnostic string) int {
	codes := map[string]int{
		"parse-error": -32700, "invalid-request": -32600, "method-not-found": -32601,
		"invalid-params": -32602, "internal-error": -32603, "unsupported-capability": -32001,
		"authentication-required": -32002, "authentication-failed": -32003,
		"authorization-denied": -32004, "conflict": -32005, "rate-limited": -32006,
		"unavailable-source": -32007, "incomplete-graph": -32008, "unknown-outcome": -32009,
	}
	return codes[diagnostic]
}

func validExternalObjectKeys(raw json.RawMessage, allowed ...string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return false
	}
	for key := range object {
		known := false
		for _, candidate := range allowed {
			if key == candidate {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return object["minMajor"] != nil && object["maxMajor"] != nil
}

func hasDuplicateStrings(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func validExternalCollections(raw []byte) bool {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	var valid func(any) bool
	valid = func(value any) bool {
		switch typed := value.(type) {
		case []any:
			if len(typed) > externalMaxItems {
				return false
			}
			for _, child := range typed {
				if !valid(child) {
					return false
				}
			}
		case map[string]any:
			if len(typed) > externalMaxItems {
				return false
			}
			for _, child := range typed {
				if !valid(child) {
					return false
				}
			}
		}
		return true
	}
	return valid(value)
}

func readExternalFrame(reader *bufio.Reader) ([]byte, error) {
	frame, err := reader.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && len(frame) != 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	if len(frame) > externalFrameLimit {
		return nil, fmt.Errorf("frame too large")
	}
	return append([]byte(nil), frame...), nil
}

func rejectExternalDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeExternalJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func consumeExternalJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[key] = true
			if err := consumeExternalJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeExternalJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("invalid JSON array")
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}

func boundedExternalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(externalRequestTimeout)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	return context.WithDeadline(ctx, deadline)
}

func acquireExternalSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func externalContextError(method string, err error, dispatched bool) error {
	if err == nil {
		return nil
	}
	if isExternalMutation(method) && dispatched {
		return fmt.Errorf("external adapter write outcome is unknown after dispatch")
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("external adapter request cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("external adapter request deadline exceeded")
	}
	return err
}

func isExternalMutation(method string) bool {
	return method == "writeState" || method == "recordProgress" || method == "assign" || method == "archive"
}

func externalAdapterEnvironment(env func(string) string) []string {
	if env == nil {
		env = os.Getenv
	}
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "LANG": true, "TMPDIR": true,
		"LC_ALL": true, "LC_COLLATE": true, "LC_CTYPE": true, "LC_MESSAGES": true,
		"LC_MONETARY": true, "LC_NUMERIC": true, "LC_TIME": true,
		"XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	}
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowedExternalEnvironmentName(name) {
			allowed[name] = true
		}
	}
	values := make(map[string]string)
	for name := range allowed {
		if strings.HasPrefix(name, "PI_") || strings.HasPrefix(name, "WORKLEASE_") || name == "GH_TOKEN" || name == "GITHUB_TOKEN" {
			continue
		}
		if value := env(name); value != "" {
			values[name] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func allowedExternalEnvironmentName(name string) bool {
	return name == "PATH" || name == "HOME" || name == "USER" || name == "LOGNAME" || name == "LANG" || name == "TMPDIR" ||
		strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "XDG_") && strings.HasSuffix(name, "_HOME")
}

func closeExternalFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}

func cloneExternalManifest(manifest ExternalAdapterManifest) ExternalAdapterManifest {
	manifest.ConfigSchema = append(json.RawMessage(nil), manifest.ConfigSchema...)
	manifest.Raw = append(json.RawMessage(nil), manifest.Raw...)
	manifest.Authentication = append([]string(nil), manifest.Authentication...)
	manifest.Capabilities = append([]string(nil), manifest.Capabilities...)
	manifest.RequiredFeatures = append([]string(nil), manifest.RequiredFeatures...)
	return manifest
}

func externalSecretValues(source config.QueueSource, env func(string) string) []string {
	values := make(map[string]bool)
	add := func(value string) {
		remember := func(candidate string) {
			if candidate == "" {
				return
			}
			values[candidate] = true
			if encoded, err := json.Marshal(candidate); err == nil && len(encoded) > 2 {
				values[string(encoded[1:len(encoded)-1])] = true
			}
			values[url.QueryEscape(candidate)] = true
			values[url.PathEscape(candidate)] = true
		}
		if value == "" {
			return
		}
		remember(value)
		parsed, err := url.Parse(value)
		if err != nil {
			return
		}
		if parsed.User != nil {
			username := parsed.User.Username()
			remember(username)
			if password, ok := parsed.User.Password(); ok {
				remember(password)
			}
		}
		for _, queryValues := range parsed.Query() {
			for _, queryValue := range queryValues {
				remember(queryValue)
			}
		}
	}
	add(source.CredentialRef)
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			add(typed)
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(source.Config)
	secretName := func(name string) bool {
		name = strings.ToUpper(name)
		for _, part := range []string{"TOKEN", "SECRET", "CREDENTIAL", "PASSWORD", "BEARER", "AUTH", "API_KEY", "APIKEY", "PRIVATE_KEY"} {
			if strings.Contains(name, part) {
				return true
			}
		}
		return false
	}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok && secretName(name) {
			add(value)
		}
	}
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "BITBUCKET_TOKEN", "GIT_TOKEN", "API_TOKEN", "ACCESS_TOKEN", "BEARER_TOKEN", "AUTH_TOKEN"} {
		add(env(name))
	}
	result := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result
}

func redactExternalText(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	value = strings.ToValidUTF8(value, "�")
	var clean strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\t' || unicode.IsPrint(r) {
			clean.WriteRune(r)
		} else if r == '\r' {
			clean.WriteRune('\n')
		} else {
			clean.WriteRune(' ')
		}
	}
	value = strings.TrimSpace(clean.String())
	if len(value) > externalDiagnosticLimit {
		value = value[:externalDiagnosticLimit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
