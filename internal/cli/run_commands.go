package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/runs"
	urfave "github.com/urfave/cli/v3"
)

// Worker environment set by a supervised run.
const (
	envRunID     = "WORKLEASE_RUN_ID"
	envRunResult = "WORKLEASE_RUN_RESULT"
)

const (
	// runStopGrace is how long a worker has to exit after SIGTERM.
	runStopGrace = 10 * time.Second
	// runCleanupTimeout bounds claim verification and release after the worker.
	runCleanupTimeout = 30 * time.Second
	// runStopPoll is how often the supervisor checks for a runs stop request.
	runStopPoll = time.Second
	// maxRunArgvBytes keeps the argv well inside the run record's size limit.
	maxRunArgvBytes = 64 * 1024
)

func runStore() runs.Store { return runs.Store{Dir: runs.Dir(os.Getenv)} }

// runAction supervises one worker: it acquires the claim, starts the worker,
// renews the claim while the worker lives, records the outcome, and releases.
func runAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		argv := cmd.Args().Slice()
		if len(argv) == 0 {
			return s.handle(cmd, reason.Invalid("run requires a command after --"))
		}
		if size := len(strings.Join(argv, "")); size > maxRunArgvBytes {
			return s.handle(cmd, reason.Invalid(fmt.Sprintf("run command exceeds %d bytes", maxRunArgvBytes)))
		}
		resources := cmd.StringSlice("resource")
		if len(resources) > 32 {
			return s.handle(cmd, reason.Invalid("run accepts at most 32 resources"))
		}
		if len(resources) == 0 && cmd.String("expect-authority") != "" {
			return s.handle(cmd, reason.Invalid("--expect-authority requires --resource"))
		}
		for name, value := range map[string]string{"run name": cmd.String("name"), "run ref": cmd.String("ref")} {
			if err := validatePublicCLI(name, value, 256, false); err != nil {
				return s.handle(cmd, err)
			}
		}
		if max := cmd.Duration("max-duration"); max < 0 {
			return s.handle(cmd, reason.Invalid("max-duration must not be negative"))
		}
		dir, err := os.Getwd()
		if err != nil {
			return s.handle(cmd, reason.New(reason.ReasonInvalidPath, "working directory is invalid"))
		}
		ready, assigned := runs.ReadyWriter(os.Getenv)
		if cmd.Bool("detach") && ready == nil {
			return s.handle(cmd, detachRun(s, cmd, dir))
		}
		id := assigned
		if id == "" {
			id = runs.NewID(time.Now(), randomHex(4))
		}
		sv := &runSupervisor{s: s, cmd: cmd, store: runStore(), ready: ready, foreground: ready == nil}
		return sv.run(ctx, id, dir, argv, resources)
	}
}

// detachRun re-executes this invocation as a detached supervisor and returns
// once it reports the worker running or the run failed.
func detachRun(s *boundary, cmd *urfave.Command, dir string) error {
	executable, err := os.Executable()
	if err != nil {
		return reason.New(reason.ReasonInternal, "worklease executable cannot be resolved")
	}
	s.mu.Lock()
	args := append([]string(nil), s.invocation[1:]...)
	s.mu.Unlock()
	id := runs.NewID(time.Now(), randomHex(4))
	record, _, err := runs.StartDetached(executable, args, dir, os.Environ(), id)
	if err != nil {
		return reason.New(reason.ReasonInternal, err.Error()).With("runId", id)
	}
	if record.State == runs.StateFailed {
		code := record.Reason
		if !reason.Registered(code) {
			code = reason.ReasonInternal
		}
		return reason.New(code, record.Error).With("runId", id)
	}
	return writeRunResult(s, cmd, "run", record)
}

type runSupervisor struct {
	s          *boundary
	cmd        *urfave.Command
	store      runs.Store
	ready      *os.File
	foreground bool

	mu     sync.Mutex
	record runs.Record

	// Claim lifecycle, used only when the run holds resources.
	global     []string
	handlePath string
	ttl        time.Duration
	renewed    time.Time
}

func (sv *runSupervisor) update(change func(*runs.Record)) error {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	change(&sv.record)
	sv.record.UpdatedAt = time.Now().UTC()
	return sv.store.Save(sv.record)
}

func (sv *runSupervisor) snapshot() runs.Record {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	return sv.record
}

// report sends the launcher its one readiness line.
func (sv *runSupervisor) report() {
	if sv.ready == nil {
		return
	}
	data, _ := json.Marshal(sv.snapshot())
	_, _ = sv.ready.Write(data)
	_ = sv.ready.Close()
	sv.ready = nil
}

// fail records a run that ended before or instead of starting its worker.
func (sv *runSupervisor) fail(err error) error {
	_ = sv.update(func(r *runs.Record) {
		now := time.Now().UTC()
		r.State, r.Error, r.EndedAt = runs.StateFailed, output.RedactString(err.Error()), &now
		if classified := reason.As(err); classified != nil {
			r.Reason = classified.Reason
		}
	})
	sv.report()
	return sv.s.handle(sv.cmd, err)
}

func (sv *runSupervisor) run(ctx context.Context, id, dir string, argv, resources []string) error {
	// Any termination signal, including a terminal hangup, cancels the run so
	// every path after acquisition stops the worker and ends the claim.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	name := sv.cmd.String("name")
	if name == "" {
		name = filepath.Base(argv[0])
	}
	interval := 15 * time.Second
	now := time.Now().UTC()
	sv.record = runs.Record{SchemaVersion: runs.SchemaVersion, ID: id, Name: name, Ref: sv.cmd.String("ref"), Argv: argv, Dir: dir, SupervisorPID: os.Getpid(), State: runs.StateStarting, StartedAt: now, UpdatedAt: now, IntervalSeconds: int(interval / time.Second)}
	if err := sv.store.Create(sv.record); err != nil {
		return sv.fail(reason.New(reason.ReasonStorageFailure, "run record cannot be created: "+err.Error()))
	}
	logPath, _ := sv.store.LogPath(id)
	resultPath, _ := sv.store.ResultPath(id)
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return sv.fail(reason.New(reason.ReasonStorageFailure, "run log cannot be created"))
	}
	defer logFile.Close()
	env := runs.WorkerEnv(os.Environ())
	env = withoutEnv(env, envRunID, envRunResult, "WORKLEASE_SESSION_ID", "WORKLEASE_HANDLE")
	env = append(env, envRunID+"="+id, envRunResult+"="+resultPath)
	if len(resources) > 0 {
		claimEnv, err := sv.acquire(ctx, id, dir, resources)
		if err != nil {
			sv.endClaim(ctx, "run "+id+" could not start")
			return sv.fail(err)
		}
		env = append(withoutEnv(env, "WORKLEASE_HOME", "WORKLEASE_PROFILE"), claimEnv...)
	}
	if interval > sv.ttl/3 && sv.ttl > 0 {
		interval = sv.ttl / 3
		_ = sv.update(func(r *runs.Record) { r.IntervalSeconds = max(1, int(interval/time.Second)) })
	}
	log := &runLog{file: logFile, now: time.Now}
	var stream io.Writer = log
	if sv.foreground {
		stream = io.MultiWriter(log, sv.s.errWriter)
	}
	child := exec.Command(argv[0], argv[1:]...)
	child.Dir, child.Env, child.Stdout, child.Stderr = dir, env, stream, stream
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		startErr := reason.New(reason.ReasonInvalidArgument, "worker cannot start: "+err.Error())
		sv.endClaim(ctx, "run "+id+" worker failed to start")
		return sv.fail(startErr)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	if err := sv.update(func(r *runs.Record) { r.State, r.ChildPID = runs.StateRunning, child.Process.Pid }); err != nil {
		_ = stopChild(child, exited)
		sv.endClaim(ctx, "run "+id+" could not record its worker")
		return sv.fail(reason.New(reason.ReasonStorageFailure, "run record cannot be updated: "+err.Error()))
	}
	sv.report()

	outcome, waitErr := sv.supervise(ctx, child, exited, interval, log)
	return sv.finish(ctx, outcome, waitErr, resultPath, log)
}

// acquire claims the resources for this run's own session and returns the
// worker environment that selects that claim.
func (sv *runSupervisor) acquire(ctx context.Context, id, dir string, resources []string) ([]string, error) {
	backend, err := authorityFor(ctx, sv.cmd, true)
	if err != nil {
		return nil, err
	}
	authorityID, profile, home, cfg := backend.AuthorityID(), backend.Profile, backend.Config.Home, backend.Config
	backend.Close()
	if expected := sv.cmd.String("expect-authority"); expected != "" && expected != authorityID {
		return nil, reason.New(reason.ReasonAuthorityMismatch, "selected authority differs from the expected authority")
	}
	sv.ttl = sv.cmd.Duration("ttl")
	if sv.ttl == 0 {
		sv.ttl = cfg.TTL
	}
	if sv.ttl < time.Second || sv.ttl > time.Hour {
		return nil, reason.Invalid("ttl must be between 1s and 1h")
	}
	sv.global = []string{"worklease", "--json", "--home", home, "--profile", backend.ProfileName}
	if sv.cmd.IsSet("config") {
		sv.global = append(sv.global, "--config", sv.cmd.String("config"))
	}
	session := "run-" + id
	root, err := handle.ContextRoot(dir, nil)
	if err != nil {
		root = filepath.Clean(dir)
	}
	// Name the handle explicitly so an inherited WORKLEASE_HANDLE cannot
	// redirect the run's claim away from the path the worker receives.
	sv.handlePath = handle.ContextualPath(home, root, session, authorityID)
	args := []string{"acquire", "--session", session, "--handle", sv.handlePath, "--ttl", sv.ttl.String(), "--coordination-only", "--work-key", "run " + id}
	for _, key := range resources {
		args = append(args, "--resource", key)
	}
	pinned := context.WithValue(ctx, queueAcquirePinKey{}, queueAcquirePin{authorityID: authorityID, profile: profile})
	grant, err := sv.lifecycle(pinned, args...)
	if err != nil {
		return nil, err
	}
	var claim runs.Claim
	if err := remarshal(grant, &claim); err != nil || claim.ClaimID == "" || claim.AuthorityID != authorityID {
		return nil, reason.New(reason.ReasonUnknownOutcome, "acquire receipt could not be decoded; inspect the run's handle before retrying").With("commitState", "unknown")
	}
	claim.State = runs.ClaimHeld
	sv.renewed = time.Now()
	if err := sv.update(func(r *runs.Record) { r.Claim = &claim }); err != nil {
		return nil, err
	}
	return []string{"WORKLEASE_SESSION_ID=" + session, "WORKLEASE_HANDLE=" + sv.handlePath, "WORKLEASE_HOME=" + home, "WORKLEASE_PROFILE=" + backend.ProfileName}, nil
}

// lifecycle runs one ordinary lifecycle command in-process so the supervisor
// shares the CLI's handle, replay, and recovery behavior.
func (sv *runSupervisor) lifecycle(ctx context.Context, args ...string) (map[string]any, error) {
	var out bytes.Buffer
	err := Run(ctx, append(append([]string(nil), sv.global...), args...), "", "", "", &out, &out)
	var fields map[string]any
	_ = json.Unmarshal(out.Bytes(), &fields)
	var rendered *handledError
	if errors.As(err, &rendered) {
		err = rendered.cause // rendered only into the discarded buffer
	}
	return fields, err
}

// supervise waits for the worker while renewing the claim. It returns the
// supervisor-imposed outcome (stopped, timeout, or lost), or "" when the
// worker exited on its own.
func (sv *runSupervisor) supervise(ctx context.Context, child *exec.Cmd, exited <-chan error, interval time.Duration, log *runLog) (string, error) {
	var deadline <-chan time.Time
	if max := sv.cmd.Duration("max-duration"); max > 0 {
		timer := time.NewTimer(max)
		defer timer.Stop()
		deadline = timer.C
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	poll := time.NewTicker(runStopPoll)
	defer poll.Stop()
	id := sv.snapshot().ID
	for {
		select {
		case err := <-exited:
			killGroup(child.Process.Pid) // background members must not outlive the claim
			return "", err
		case <-ctx.Done():
			return runs.OutcomeStopped, stopChild(child, exited)
		case <-poll.C:
			if sv.store.StopRequested(id) {
				return runs.OutcomeStopped, stopChild(child, exited)
			}
		case <-deadline:
			return runs.OutcomeTimeout, stopChild(child, exited)
		case <-ticker.C:
			if lost := sv.renew(ctx); lost != "" {
				err := stopChild(child, exited)
				_ = sv.update(func(r *runs.Record) { r.Claim.State, r.Claim.Detail = runs.ClaimLost, lost })
				return runs.StateLost, err
			}
			_ = sv.update(func(r *runs.Record) { r.LastOutputAt, r.LogTruncated = log.last(), log.truncated() })
		}
	}
}

// renew heartbeats the claim when a third of its TTL has passed. It returns a
// reason when ownership is lost; transient failures are retried until expiry.
func (sv *runSupervisor) renew(ctx context.Context) string {
	record := sv.snapshot()
	if record.Claim == nil || time.Since(sv.renewed) < sv.ttl/3 {
		return ""
	}
	fields, err := sv.lifecycle(ctx, "heartbeat", "--handle", sv.handlePath, "--ttl", sv.ttl.String())
	if err != nil {
		classified := reason.As(err)
		if reason.OwnershipRenewalFailure(err) && (classified == nil || classified.Reason != reason.ReasonStaleRevision) {
			return classified.Reason + ": " + classified.Message
		}
		if !time.Now().Before(record.Claim.ExpiresAt) {
			return "claim expired while renewal failed: " + err.Error()
		}
		return ""
	}
	sv.renewed = time.Now()
	var receipt struct {
		Receipt struct {
			Result struct {
				ExpiresAt time.Time `json:"expiresAt"`
			} `json:"result"`
		} `json:"receipt"`
	}
	if remarshal(fields, &receipt) == nil && !receipt.Receipt.Result.ExpiresAt.IsZero() {
		_ = sv.update(func(r *runs.Record) { r.Claim.ExpiresAt = receipt.Receipt.Result.ExpiresAt })
	}
	return ""
}

// stopChild terminates the worker's process group and waits for it.
func stopChild(child *exec.Cmd, exited <-chan error) error {
	pgid := child.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case err := <-exited:
		killGroup(pgid)
		return err
	case <-time.After(runStopGrace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return <-exited
	}
}

// killGroup ends any process left in the worker's group after the worker
// itself exited: SIGTERM, then SIGKILL once runStopGrace passes.
func killGroup(pgid int) {
	if syscall.Kill(-pgid, syscall.SIGTERM) != nil {
		return // the group is already empty
	}
	for deadline := time.Now().Add(runStopGrace); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(-pgid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// finish records the worker's exit and releases the claim unless it must be
// kept for recovery or was lost.
func (sv *runSupervisor) finish(ctx context.Context, imposed string, waitErr error, resultPath string, log *runLog) error {
	code, signalName := exitStatus(waitErr)
	result, resultErr := runs.ReadResult(resultPath)
	outcome := imposed
	switch {
	case imposed == runs.StateLost:
		outcome = runs.OutcomeFailed
	case imposed != "":
	case result != nil:
		outcome = result.Outcome
	case code == 0:
		outcome = runs.OutcomeDone
	default:
		outcome = runs.OutcomeFailed
	}
	state := runs.StateExited
	if imposed == runs.StateLost {
		state = runs.StateLost
	}
	release := fmt.Sprintf("run %s %s (exit %d)", sv.snapshot().ID, outcome, code)
	if signalName != "" {
		release = fmt.Sprintf("run %s %s (%s)", sv.snapshot().ID, outcome, signalName)
	}
	if state != runs.StateLost {
		sv.endClaim(ctx, release)
	}
	saveErr := sv.update(func(r *runs.Record) {
		now := time.Now().UTC()
		r.State, r.EndedAt, r.ExitCode, r.Signal, r.Outcome, r.Result = state, &now, &code, signalName, outcome, result
		r.LastOutputAt, r.LogTruncated = log.last(), log.truncated()
		if resultErr != nil {
			r.Error = resultErr.Error()
		}
	})
	record := sv.snapshot()
	if saveErr != nil {
		return sv.s.handle(sv.cmd, reason.New(reason.ReasonStorageFailure, "run "+record.ID+" ended but its record cannot be saved: "+saveErr.Error()))
	}
	if err := writeRunResult(sv.s, sv.cmd, "run", record); err != nil {
		return err
	}
	switch {
	case state == runs.StateLost:
		return &handledError{cause: reason.New(reason.ReasonOwnershipLost, "run "+record.ID+" lost its claim; the worker was stopped")}
	case code != 0:
		return &handledError{cause: urfave.Exit("", code)}
	}
	return nil
}

// endClaim releases the run's claim, or keeps it when the claim has
// unresolved guarded operations or queue provider writes.
func (sv *runSupervisor) endClaim(ctx context.Context, releaseReason string) {
	record := sv.snapshot()
	if record.Claim == nil || record.Claim.State != runs.ClaimHeld {
		return
	}
	// A cancelled supervisor context must not strand the claim, and a hung
	// authority must not hang the supervisor.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runCleanupTimeout)
	defer cancel()
	keep := ""
	if fields, err := sv.lifecycle(ctx, "verify", "--handle", sv.handlePath); err != nil {
		keep = "verify failed: " + err.Error()
	} else if unknown, _ := fields["unknownOperations"].([]any); len(unknown) > 0 {
		keep = fmt.Sprintf("%d unresolved guarded operation(s); inspect with worklease op inspect", len(unknown))
	}
	if keep == "" {
		if journal, err := queueRecoveryJournal(); err == nil {
			entries, err := journal.Recovery(time.Now())
			for _, entry := range entries {
				if entry.ClaimID == record.Claim.ClaimID {
					keep = "unresolved queue provider write " + entry.OperationID + "; see worklease queue recovery"
					break
				}
			}
			if err != nil {
				keep = "queue recovery state is unreadable: " + err.Error()
			}
		}
	}
	if keep != "" {
		_ = sv.update(func(r *runs.Record) { r.Claim.State, r.Claim.Detail = runs.ClaimKept, keep })
		return
	}
	if _, err := sv.lifecycle(ctx, "release", "--handle", sv.handlePath, "--reason", releaseReason); err != nil {
		_ = sv.update(func(r *runs.Record) {
			r.Claim.State, r.Claim.Detail = runs.ClaimKept, "release failed; the claim expires at its TTL: "+err.Error()
		})
		return
	}
	_ = sv.update(func(r *runs.Record) { r.Claim.State, r.Claim.Detail = runs.ClaimReleased, releaseReason })
}

func exitStatus(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), status.Signal().String()
		}
		return exit.ExitCode(), ""
	}
	return 1, ""
}

// runLog appends worker output to the run log up to runs.MaxLogBytes and
// remembers when the worker last wrote.
type runLog struct {
	mu      sync.Mutex
	file    *os.File
	now     func() time.Time
	written int64
	lastAt  time.Time
	cut     bool
}

func (l *runLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastAt = l.now().UTC()
	room := runs.MaxLogBytes - l.written
	if room <= 0 {
		l.cut = true
		return len(p), nil
	}
	chunk := p
	if int64(len(chunk)) > room {
		chunk, l.cut = chunk[:room], true
	}
	n, err := l.file.Write(chunk)
	l.written += int64(n)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

func (l *runLog) last() *time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastAt.IsZero() {
		return nil
	}
	value := l.lastAt
	return &value
}

func (l *runLog) truncated() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cut
}

func withoutEnv(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		drop := false
		for _, candidate := range names {
			drop = drop || name == candidate
		}
		if !drop {
			out = append(out, entry)
		}
	}
	return out
}

func remarshal(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
