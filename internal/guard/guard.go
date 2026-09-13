// Package guard implements supervised local guarded operations.
package guard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	"golang.org/x/sys/unix"
)

const (
	MaxCaptureBytes = 1 << 20
	MaxReplaceBytes = 16 << 20
	TimeoutExitCode = 124
)

// beforeReplaceContentRead is a test seam for proving content preflight does
// not hold the authority write transaction.
var beforeReplaceContentRead func()

type ExecRequest struct {
	OperationID     string
	Argv            []string
	CWD             string
	GitPrimary      bool
	MaxDuration     time.Duration
	TTL             time.Duration
	RequestNotAfter time.Time
	Lifecycle       *OperationLifecycle
}
type ExecResult struct {
	Receipt  lease.Receipt
	ExitCode int
}

// OperationLifecycle lets an adapter persist the exact request before dispatch
// and reconcile its private handle afterwards. Failure receives started=true
// once the authority has committed the started intent: from then on the
// pending request must be retained for exact recovery even when the error
// itself would otherwise prove no commit.
type OperationLifecycle struct {
	Prepare  func(lease.OperationIntent) error
	Complete func(lease.Receipt) error
	Failure  func(err error, started bool)
}

// postStart marks an error that occurred after the started intent committed.
// The authority holds a started operation whose outcome this process could not
// record, so the commit state is unknown regardless of the error's own reason.
func postStart(err error, operationID string) error {
	if e := reason.As(err); e != nil {
		e.With("commitState", "unknown").With("operationId", operationID)
	}
	return err
}

func failLifecycle(lc *OperationLifecycle, err error, started bool) {
	if lc != nil && lc.Failure != nil {
		lc.Failure(err, started)
	}
}

type ReplaceRequest struct {
	OperationID     string
	Path            string
	ExpectedSHA256  string
	ContentFile     string
	TTL             time.Duration
	RequestNotAfter time.Time
	RequestHash     string
	Lifecycle       *OperationLifecycle
}
type ReplaceResult struct{ Receipt lease.Receipt }

func validHash(v string) bool {
	b, e := hex.DecodeString(v)
	return len(v) == 64 && e == nil && len(b) == 32
}
func validExec(argv []string) error {
	if len(argv) == 0 || argv[0] == "" {
		return reason.Invalid("a command is required after --")
	}
	for _, v := range argv {
		if !utf8.ValidString(v) {
			return reason.Invalid("command arguments must be valid UTF-8")
		}
	}
	return nil
}
func normalizeDuration(v, max time.Duration) error {
	if v <= 0 || v > max {
		return reason.Invalid("duration is out of range")
	}
	return nil
}

func cleanEnvironment(isolated bool, claim, operation string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "WORKLEASE_TOKEN") || key == "WORKLEASE_CREDENTIAL" {
			continue
		}
		if isolated && strings.HasPrefix(key, "GIT_") {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "WORKLEASE_CLAIM_ID="+claim, "WORKLEASE_OPERATION_ID="+operation)
	return env
}

func resolveCWD(cwd string, primary bool) (string, bool, error) {
	if cwd != "" && primary {
		return "", false, reason.Invalid("--cwd and --git-primary are exclusive")
	}
	if cwd != "" {
		p, e := filepath.Abs(cwd)
		if e != nil {
			return "", false, reason.New(reason.ReasonInvalidPath, "working directory is invalid")
		}
		p, e = filepath.EvalSymlinks(p)
		if e != nil {
			return "", false, reason.New(reason.ReasonInvalidPath, "working directory is invalid")
		}
		st, e := os.Stat(p)
		if e != nil || !st.IsDir() {
			return "", false, reason.New(reason.ReasonInvalidPath, "working directory is invalid")
		}
		return p, true, nil
	}
	if !primary {
		p, e := os.Getwd()
		if e != nil {
			return "", false, reason.New(reason.ReasonInvalidPath, "working directory is invalid")
		}
		return p, false, nil
	}
	p, e := os.Getwd()
	if e != nil {
		return "", false, reason.New(reason.ReasonInvalidPath, "working directory is invalid")
	}
	return gitPrimary(p)
}
func gitRun(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	cmd.Env = cleanEnvironment(true, "", "")
	out, e := cmd.Output()
	if e != nil {
		return "", e
	}
	return strings.TrimSpace(string(out)), nil
}
func gitPrimary(cwd string) (string, bool, error) {
	p, e := filepath.EvalSymlinks(cwd)
	if e != nil {
		return "", true, reason.New(reason.ReasonInvalidPath, "working directory is invalid")
	}
	bare, e := gitRun(p, "rev-parse", "--is-bare-repository")
	if e != nil {
		return "", true, reason.New(reason.ReasonInvalidPath, "caller is not in a Git repository")
	}
	if bare == "true" {
		return "", true, reason.New(reason.ReasonInvalidPath, "bare repositories have no primary worktree")
	}
	if _, e = gitRun(p, "rev-parse", "--show-toplevel"); e != nil {
		return "", true, reason.New(reason.ReasonInvalidPath, "caller is not in a Git repository")
	}
	common, e := gitRun(p, "rev-parse", "--git-common-dir")
	if e != nil {
		return "", true, reason.New(reason.ReasonInvalidPath, "primary worktree is unavailable")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(p, common)
	}
	common, e = filepath.EvalSymlinks(common)
	if e != nil {
		return "", true, reason.New(reason.ReasonInvalidPath, "primary worktree is unavailable")
	}
	list, e := gitRun(p, "worktree", "list", "--porcelain")
	if e != nil {
		return "", true, reason.New(reason.ReasonInvalidPath, "primary worktree is unavailable")
	}
	var candidate string
	lines := strings.Split(list, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "worktree ") {
			continue
		}
		w := strings.TrimSpace(strings.TrimPrefix(lines[i], "worktree "))
		resolved, er := filepath.EvalSymlinks(w)
		if er != nil {
			continue
		}
		gd, er := gitRun(resolved, "rev-parse", "--git-dir")
		if er != nil {
			continue
		}
		if !filepath.IsAbs(gd) {
			gd = filepath.Join(resolved, gd)
		}
		gd, er = filepath.EvalSymlinks(gd)
		if er != nil || gd != common {
			continue
		}
		if candidate != "" {
			return "", true, reason.New(reason.ReasonInvalidPath, "primary worktree is ambiguous")
		}
		candidate = resolved
	}
	if candidate == "" {
		return "", true, reason.New(reason.ReasonInvalidPath, "primary worktree is unavailable")
	}
	return candidate, true, nil
}

type capture struct {
	data      []byte
	total     int
	truncated bool
}

func readBounded(r io.Reader) capture {
	var out capture
	buf := make([]byte, 64*1024)
	for {
		n, e := r.Read(buf)
		if n > 0 {
			out.total += n
			if len(out.data) < MaxCaptureBytes {
				take := n
				if take > MaxCaptureBytes-len(out.data) {
					take = MaxCaptureBytes - len(out.data)
				}
				out.data = append(out.data, buf[:take]...)
			}
			if out.total > MaxCaptureBytes {
				out.truncated = true
			}
		}
		if e != nil {
			return out
		}
	}
}
func text(c capture) string { return strings.ToValidUTF8(string(c.data), "\uFFFD") }
func reapProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	// The leader has already exited. Do not spend the guard budget waiting for
	// descendants that inherited a pipe; terminate the whole local group and
	// let bounded capture draining decide whether to wait for readers.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
func terminateAndWait(cmd *exec.Cmd, exited <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case err := <-exited:
		reapProcessGroup(cmd.Process.Pid)
		return err
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		err := <-exited
		reapProcessGroup(cmd.Process.Pid)
		return err
	}
}

func Exec(ctx context.Context, svc *lease.Service, creds lease.Credentials, req ExecRequest) (ExecResult, error) {
	if e := validExec(req.Argv); e != nil {
		return ExecResult{}, e
	}
	if req.MaxDuration == 0 {
		req.MaxDuration = time.Hour
	}
	if e := normalizeDuration(req.MaxDuration, 24*time.Hour); e != nil {
		return ExecResult{}, e
	}
	cwd, isolated, e := resolveCWD(req.CWD, req.GitPrimary)
	if e != nil {
		return ExecResult{}, e
	}
	if req.TTL == 0 {
		req.TTL = svc.DefaultTTL()
	}
	intent := map[string]any{"argv": req.Argv, "cwd": cwd, "gitPrimary": req.GitPrimary, "maxDuration": req.MaxDuration.Microseconds()}
	operation := lease.OperationIntent{OperationID: req.OperationID, Kind: "exec", Request: intent, RequestNotAfter: req.RequestNotAfter, TTL: req.TTL}
	if req.Lifecycle != nil && req.Lifecycle.Prepare != nil {
		if e = req.Lifecycle.Prepare(operation); e != nil {
			return ExecResult{}, e
		}
	}
	started, e := svc.BeginOperation(ctx, creds, operation)
	if e != nil {
		if !started.Completed {
			failLifecycle(req.Lifecycle, e, false)
		}
		if started.Completed && started.Receipt != nil {
			return ExecResult{Receipt: *started.Receipt, ExitCode: receiptExit(*started.Receipt)}, nil
		}
		return ExecResult{}, e
	}
	if started.Completed && started.Receipt != nil {
		if req.Lifecycle != nil && req.Lifecycle.Complete != nil {
			if e := req.Lifecycle.Complete(*started.Receipt); e != nil {
				return ExecResult{}, e
			}
		}
		return ExecResult{Receipt: *started.Receipt, ExitCode: receiptExit(*started.Receipt)}, nil
	}
	current := creds
	current.Revision = started.Revision
	preSpawnFailure := func(cause error) (ExecResult, error) {
		completed, completeErr := svc.CompleteOperation(ctx, current, req.OperationID, map[string]any{"argv": req.Argv, "returncode": 127, "stdout": "", "stderr": "", "stdoutBytes": 0, "stderrBytes": 0, "stdoutTruncated": false, "stderrTruncated": false, "error": cause.Error(), "executionDirectory": map[string]any{"mode": "caller", "path": cwd}})
		if completeErr != nil {
			completeErr = postStart(completeErr, req.OperationID)
			failLifecycle(req.Lifecycle, completeErr, true)
			return ExecResult{}, completeErr
		}
		if req.Lifecycle != nil && req.Lifecycle.Complete != nil {
			if e := req.Lifecycle.Complete(completed); e != nil {
				return ExecResult{}, e
			}
		}
		return ExecResult{}, reason.New(reason.ReasonInvalidArgument, "guarded command could not be started")
	}
	cmd := exec.Command(req.Argv[0], req.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = cleanEnvironment(isolated, creds.ClaimID, req.OperationID)
	stdinFile, e := os.Open(os.DevNull)
	if e != nil {
		return preSpawnFailure(e)
	}
	defer stdinFile.Close()
	cmd.Stdin = stdinFile
	stdoutPipe, stdoutChild, e := os.Pipe()
	if e != nil {
		return preSpawnFailure(e)
	}
	defer stdoutPipe.Close()
	defer stdoutChild.Close()
	stderrPipe, stderrChild, e := os.Pipe()
	if e != nil {
		return preSpawnFailure(e)
	}
	defer stderrPipe.Close()
	defer stderrChild.Close()
	cmd.Stdout = stdoutChild
	cmd.Stderr = stderrChild
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	deadline := time.Now().Add(req.MaxDuration)
	if e = cmd.Start(); e != nil {
		return preSpawnFailure(e)
	}
	_ = stdoutChild.Close()
	_ = stderrChild.Close()
	stdoutCh, stderrCh := make(chan capture, 1), make(chan capture, 1)
	go func() { stdoutCh <- readBounded(stdoutPipe) }()
	go func() { stderrCh <- readBounded(stderrPipe) }()
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	ticker := time.NewTicker(req.TTL / 2)
	if req.TTL/2 <= 0 {
		ticker.Stop()
	}
	defer ticker.Stop()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	timedOut := false
	ownershipLost := false
	var waitErr error
	lastLeaseDeadline := time.Now().Add(req.TTL)
	leaseTimer := time.NewTimer(req.TTL)
	defer leaseTimer.Stop()
	renewResults := make(chan struct {
		receipt lease.Receipt
		err     error
	}, 1)
	renewing := false
	startRenew := func() {
		if renewing {
			return
		}
		renewing = true
		copyCreds := current
		go func() {
			r, er := svc.RenewOperation(ctx, copyCreds, req.OperationID, req.TTL)
			renewResults <- struct {
				receipt lease.Receipt
				err     error
			}{r, er}
		}()
	}
	for {
		select {
		case waitErr = <-exited:
			reapProcessGroup(cmd.Process.Pid)
			goto done
		case <-timer.C:
			timedOut = true
			waitErr = terminateAndWait(cmd, exited)
			_ = stdoutPipe.Close()
			_ = stderrPipe.Close()
			goto done
		case <-leaseTimer.C:
			if time.Now().After(lastLeaseDeadline) {
				ownershipLost = true
				waitErr = terminateAndWait(cmd, exited)
				_ = stdoutPipe.Close()
				_ = stderrPipe.Close()
				goto done
			}
			leaseTimer.Reset(time.Until(lastLeaseDeadline))
		case <-ticker.C:
			startRenew()
		case result := <-renewResults:
			renewing = false
			if result.err != nil {
				// Only an ownership or clock failure proves the lease is gone.
				// A transient storage error leaves the last confirmed deadline
				// in force; the lease timer terminates the child if it passes.
				if !reason.OwnershipRenewalFailure(result.err) {
					continue
				}
				ownershipLost = true
				waitErr = terminateAndWait(cmd, exited)
				_ = stdoutPipe.Close()
				_ = stderrPipe.Close()
				goto done
			}
			current.Revision = result.receipt.Revision
			lastLeaseDeadline = time.Now().Add(req.TTL)
			if raw, ok := result.receipt.Result["expiresAt"].(string); ok {
				if parsed, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
					lastLeaseDeadline = parsed
				}
			}
			leaseTimer.Reset(time.Until(lastLeaseDeadline))
		case <-ctx.Done():
			_ = terminateAndWait(cmd, exited)
			_ = stdoutPipe.Close()
			_ = stderrPipe.Close()
			err := postStart(reason.New(reason.ReasonInterrupted, "guarded command interrupted"), req.OperationID)
			failLifecycle(req.Lifecycle, err, true)
			return ExecResult{}, err
		}
	}
done:
	if renewing {
		// The child finished while a renewal was in flight. Completion must
		// use the revision that renewal committed, otherwise the authority
		// rejects the completion as stale and strands a finished child as an
		// unresolved operation.
		result := <-renewResults
		if result.err == nil {
			current.Revision = result.receipt.Revision
		}
	}
	var stdout, stderr capture
	captureDeadline := time.NewTimer(time.Until(deadline))
	defer captureDeadline.Stop()
	stdoutReady, stderrReady := false, false
	for !stdoutReady || !stderrReady {
		select {
		case stdout = <-stdoutCh:
			stdoutReady = true
		case stderr = <-stderrCh:
			stderrReady = true
		case <-captureDeadline.C:
			if !stdoutReady {
				stdout = capture{}
			}
			if !stderrReady {
				stderr = capture{}
			}
			stdoutReady, stderrReady = true, true
		}
	}
	if timedOut {
		err := postStart(reason.New(reason.ReasonChildTimeout, "guarded child exceeded max-duration").With("timedOut", true), req.OperationID)
		failLifecycle(req.Lifecycle, err, true)
		return ExecResult{}, err
	}
	if ownershipLost {
		err := postStart(reason.New(reason.ReasonOwnershipLost, "claim ownership was lost while command was running"), req.OperationID)
		failLifecycle(req.Lifecycle, err, true)
		return ExecResult{}, err
	}
	code := 0
	if waitErr != nil {
		if x, ok := waitErr.(*exec.ExitError); ok {
			if status, ok := x.Sys().(syscall.WaitStatus); ok {
				if status.Signaled() {
					code = 128 + int(status.Signal())
				} else {
					code = status.ExitStatus()
				}
			} else {
				code = 1
			}
		} else {
			code = 1
		}
	}
	directory := map[string]any{"mode": "caller"}
	if req.GitPrimary {
		directory = map[string]any{"mode": "git-primary", "path": cwd}
	} else if req.CWD != "" {
		directory = map[string]any{"mode": "provider-directory", "path": cwd}
	}
	receiptMap := map[string]any{"argv": req.Argv, "returncode": code, "stdout": text(stdout), "stderr": text(stderr), "stdoutBytes": stdout.total, "stderrBytes": stderr.total, "stdoutTruncated": stdout.truncated, "stderrTruncated": stderr.truncated, "executionDirectory": directory, "timedOut": false, "guarantee": "local-coordination", "providerFencing": false}
	receipt, e := svc.CompleteOperation(ctx, current, req.OperationID, receiptMap)
	if e != nil {
		e = postStart(e, req.OperationID)
		failLifecycle(req.Lifecycle, e, true)
		return ExecResult{}, e
	}
	if req.Lifecycle != nil && req.Lifecycle.Complete != nil {
		if e := req.Lifecycle.Complete(receipt); e != nil {
			return ExecResult{}, e
		}
	}
	return ExecResult{Receipt: receipt, ExitCode: code}, nil
}
func receiptExit(r lease.Receipt) int {
	if v, ok := r.Result["returncode"].(float64); ok {
		return int(v)
	}
	if v, ok := r.Result["returncode"].(int); ok {
		return v
	}
	return 1
}

func sha256File(path string) (string, []byte, os.FileInfo, error) {
	st, e := os.Lstat(path)
	if e != nil {
		return "", nil, nil, e
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return "", nil, nil, errors.New("target is not a regular file")
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return "", nil, nil, errors.New("hard-linked file is not replaceable")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return "", nil, nil, e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, st, nil
}

func readReplaceContent(path string) (string, []byte, error) {
	parent := filepath.Dir(path)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil || resolvedParent != parent {
		return "", nil, reason.New(reason.ReasonInvalidPath, "content parent is not canonical")
	}
	dir, err := openPinnedDir(parent)
	if err != nil {
		return "", nil, reason.New(reason.ReasonInvalidPath, "content parent is not canonical")
	}
	defer dir.Close()
	f, opened, err := openRegularAt(dir, filepath.Base(path))
	if err != nil {
		return "", nil, reason.New(reason.ReasonInvalidPath, "content file is not a safe regular file")
	}
	defer f.Close()
	if opened.Size() > MaxReplaceBytes {
		return "", nil, reason.Invalid("content file exceeds the 16 MiB replacement limit")
	}
	if beforeReplaceContentRead != nil {
		beforeReplaceContentRead()
	}
	content, err := io.ReadAll(io.LimitReader(f, MaxReplaceBytes+1))
	if err != nil {
		return "", nil, reason.New(reason.ReasonInvalidPath, "content file cannot be read")
	}
	if len(content) > MaxReplaceBytes {
		return "", nil, reason.Invalid("content file exceeds the 16 MiB replacement limit")
	}
	after, err := f.Stat()
	if err != nil || !sameFile(opened, after) || after.Size() != int64(len(content)) {
		return "", nil, reason.New(reason.ReasonInvalidPath, "content file changed while reading")
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), content, nil
}
func sameFile(a, b os.FileInfo) bool {
	as, aok := a.Sys().(*syscall.Stat_t)
	bs, bok := b.Sys().(*syscall.Stat_t)
	return aok && bok && as.Dev == bs.Dev && as.Ino == bs.Ino && as.Nlink == bs.Nlink
}
func openPinnedDir(path string) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("directory is not canonical")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	opened, err := f.Stat()
	if err != nil || !sameFile(st, opened) {
		f.Close()
		return nil, errors.New("directory changed while opening")
	}
	return f, nil
}
func openRegularAt(dir *os.File, name string) (*os.File, os.FileInfo, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	st, err := f.Stat()
	stat, ok := st.Sys().(*syscall.Stat_t)
	if err != nil || !ok || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 || stat.Nlink != 1 {
		f.Close()
		return nil, nil, errors.New("file is not a safe regular file")
	}
	return f, st, nil
}
func digestOpenFile(f *os.File) (string, []byte, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return "", nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, nil
}
func newTempAt(dir *os.File, mode os.FileMode) (*os.File, string, error) {
	for i := 0; i < 10; i++ {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, "", err
		}
		name := ".worklease-" + hex.EncodeToString(raw[:])
		fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
		if err == nil {
			return os.NewFile(uintptr(fd), name), name, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("temporary file name collision")
}
func ReplaceFile(ctx context.Context, svc *lease.Service, creds lease.Credentials, req ReplaceRequest) (ReplaceResult, error) {
	if !validHash(req.ExpectedSHA256) {
		return ReplaceResult{}, reason.Invalid("expected-sha256 must be 64 hexadecimal characters")
	}
	req.ExpectedSHA256 = strings.ToLower(req.ExpectedSHA256)
	if replay, found, err := svc.ReplayOperation(ctx, creds, req.OperationID, "replace-file", req.RequestHash); found || err != nil {
		if err != nil {
			return ReplaceResult{}, err
		}
		if req.Lifecycle != nil && req.Lifecycle.Complete != nil {
			if err := req.Lifecycle.Complete(replay); err != nil {
				return ReplaceResult{}, err
			}
		}
		return ReplaceResult{Receipt: replay}, nil
	}
	target, err := filepath.Abs(req.Path)
	if err != nil {
		return ReplaceResult{}, reason.New(reason.ReasonInvalidPath, "target path is invalid")
	}
	target = filepath.Clean(target)
	content, err := filepath.Abs(req.ContentFile)
	if err != nil {
		return ReplaceResult{}, reason.New(reason.ReasonInvalidPath, "content path is invalid")
	}
	content = filepath.Clean(content)
	if parent, resolveErr := filepath.EvalSymlinks(filepath.Dir(content)); resolveErr == nil {
		content = filepath.Join(parent, filepath.Base(content))
	}
	// Reject an unsafe leaf before resource canonicalization can resolve a
	// symlink to its referent.
	actual, _, oldInfo, err := sha256File(target)
	if err != nil {
		return ReplaceResult{}, reason.New(reason.ReasonInvalidPath, "target file is not a safe regular file")
	}
	key, err := resource.Resolve(resource.Input{Path: target})
	if err != nil {
		return ReplaceResult{}, err
	}
	target = key.Source
	contentHash, contentBytes, err := readReplaceContent(content)
	if err != nil {
		return ReplaceResult{}, err
	}
	intent := map[string]any{"path": target, "expectedSha256": req.ExpectedSHA256, "contentSha256": contentHash, "resource": key.Resource}
	operation := lease.OperationIntent{OperationID: req.OperationID, Kind: "replace-file", Request: intent, RequestHash: req.RequestHash, RequestNotAfter: req.RequestNotAfter, TTL: req.TTL}
	if req.Lifecycle != nil && req.Lifecycle.Prepare != nil {
		if err := req.Lifecycle.Prepare(operation); err != nil {
			return ReplaceResult{}, err
		}
	}
	startedCommitted := false
	receipt, err := svc.RunGuardedOperation(ctx, creds, operation, func() { startedCommitted = true }, func(claim lease.ClaimView) (map[string]any, error) {
		if !claim.LocalReplaceAllowed || !containsResource(claim.Resources, key.Resource) {
			return nil, reason.New(reason.ReasonUnsupportedCoordinationReplace, "claim does not permit local replacement")
		}
		parent := filepath.Dir(target)
		resolvedParent, e := filepath.EvalSymlinks(parent)
		if e != nil || resolvedParent != parent {
			return nil, reason.New(reason.ReasonInvalidPath, "target parent is not canonical")
		}
		dir, e := openPinnedDir(parent)
		if e != nil {
			return nil, reason.New(reason.ReasonInvalidPath, "target parent is not canonical")
		}
		defer dir.Close()
		targetFile, targetInfo, e := openRegularAt(dir, filepath.Base(target))
		if e != nil {
			return nil, reason.New(reason.ReasonInvalidPath, "target file is not a safe regular file")
		}
		defer targetFile.Close()
		nowHash, _, e := digestOpenFile(targetFile)
		if e != nil {
			return nil, reason.New(reason.ReasonInvalidPath, "target file cannot be read")
		}
		if !sameFile(oldInfo, targetInfo) || nowHash != req.ExpectedSHA256 {
			return map[string]any{"path": target, "expectedSha256": req.ExpectedSHA256, "actualSha256": nowHash}, reason.New(reason.ReasonExpectedHashMismatch, "target hash does not match expected hash")
		}
		contentNow := sha256.Sum256(contentBytes)
		if hex.EncodeToString(contentNow[:]) != contentHash {
			return nil, reason.New(reason.ReasonInvalidPath, "prepared content digest changed before replacement")
		}
		tmp, tmpName, e := newTempAt(dir, oldInfo.Mode().Perm())
		if e != nil {
			// Nothing was created: this is a proven no-effect failure and must
			// free the started slot rather than remain unresolved.
			return nil, reason.New(reason.ReasonInvalidPath, "temporary file cannot be created in target directory")
		}
		committed := false
		defer func() {
			if !committed {
				_ = unix.Unlinkat(int(dir.Fd()), tmpName, 0)
			}
		}()
		if _, e = tmp.Write(contentBytes); e == nil {
			e = tmp.Sync()
		}
		if closeErr := tmp.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			// The temporary file is unlinked by the deferred cleanup and the
			// target was never renamed over, so no effect occurred.
			return nil, reason.New(reason.ReasonInvalidPath, "temporary file cannot be written")
		}
		if !req.RequestNotAfter.After(time.Now()) {
			return nil, reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
		}
		checkFile, checkInfo, e := openRegularAt(dir, filepath.Base(target))
		if e != nil {
			return nil, reason.New(reason.ReasonUnknownOutcome, "replacement outcome is unresolved")
		}
		checkHash, _, checkErr := digestOpenFile(checkFile)
		_ = checkFile.Close()
		if checkErr != nil || !sameFile(targetInfo, checkInfo) || checkHash != req.ExpectedSHA256 {
			return nil, reason.New(reason.ReasonUnknownOutcome, "replacement target changed before commit")
		}
		if e = unix.Renameat(int(dir.Fd()), tmpName, int(dir.Fd()), filepath.Base(target)); e != nil {
			return nil, reason.New(reason.ReasonUnknownOutcome, "replacement outcome is unresolved")
		}
		committed = true
		if e = dir.Sync(); e != nil {
			return nil, reason.New(reason.ReasonUnknownOutcome, "replacement outcome is unresolved")
		}
		return map[string]any{"ok": true, "path": target, "previousSha256": actual, "sha256": contentHash, "contentBytes": len(contentBytes), "mutationProtection": "local-serialized-replace", "providerMutationFenced": false}, nil
	})
	if err != nil {
		if receipt.Committed {
			if req.Lifecycle != nil && req.Lifecycle.Complete != nil {
				if completeErr := req.Lifecycle.Complete(receipt); completeErr != nil {
					return ReplaceResult{}, completeErr
				}
			}
		} else {
			if startedCommitted {
				err = postStart(err, req.OperationID)
			}
			failLifecycle(req.Lifecycle, err, startedCommitted)
		}
		return ReplaceResult{Receipt: receipt}, err
	}
	if req.Lifecycle != nil && req.Lifecycle.Complete != nil {
		if err := req.Lifecycle.Complete(receipt); err != nil {
			return ReplaceResult{}, err
		}
	}
	return ReplaceResult{Receipt: receipt}, nil
}

func containsResource(resources []string, want string) bool {
	for _, resource := range resources {
		if resource == want {
			return true
		}
	}
	return false
}
