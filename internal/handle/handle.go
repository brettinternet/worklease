// Package handle implements private, owner-only client lease handles.
package handle

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
	"golang.org/x/sys/unix"
)

const (
	SchemaVersion      = 1
	MaxBytes           = 64 * 1024
	MaxCredentialBytes = 4096
)

type GitRunner func(cwd string, args ...string) (string, error)

// ContextRoot resolves a checkout root without inheriting hostile Git environment.
func ContextRoot(cwd string, run GitRunner) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", reason.New(reason.ReasonInvalidPath, "working directory is invalid")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", reason.New(reason.ReasonInvalidPath, "working directory is invalid")
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", reason.New(reason.ReasonInvalidPath, "working directory is invalid")
	}
	if run == nil {
		run = defaultGitRunner
	}
	inside, err := run(root, "rev-parse", "--is-inside-work-tree")
	if err == nil && strings.TrimSpace(inside) == "true" {
		top, e := run(root, "rev-parse", "--show-toplevel")
		if e == nil && strings.TrimSpace(top) != "" {
			resolved, e := filepath.EvalSymlinks(strings.TrimSpace(top))
			if e == nil {
				if s, e := os.Stat(resolved); e == nil && s.IsDir() {
					return filepath.Abs(resolved)
				}
			}
		}
	}
	return root, nil
}
func defaultGitRunner(cwd string, args ...string) (string, error) {
	cmd := execCommand("git", args...)
	cmd.Dir = cwd
	env := make([]string, 0, len(os.Environ()))
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			env = append(env, v)
		}
	}
	cmd.Env = env
	out, err := cmd.Output()
	return string(out), err
}

// execCommand is variable for tests and keeps ContextRoot easy to exercise.
var execCommand = func(name string, args ...string) *exec.Cmd { return exec.Command(name, args...) }

func ContextualPath(home, root, sessionSelector string) string {
	encoded, _ := json.Marshal([]string{root, sessionSelector})
	sum := sha256.Sum256(encoded)
	return filepath.Join(home, "handles", "ctx-"+hex.EncodeToString(sum[:])+".json")
}

type PendingRequest struct {
	OperationID     string         `json:"operationId,omitempty"`
	Kind            string         `json:"kind"`
	AuthorityID     string         `json:"authorityId"`
	ClaimID         string         `json:"claimId,omitempty"`
	RequestHash     string         `json:"requestSha256,omitempty"`
	RequestNotAfter time.Time      `json:"requestNotAfter,omitempty"`
	Inputs          map[string]any `json:"inputs,omitempty"`
	SuccessorToken  string         `json:"successorToken,omitempty"`
}
type RecoveryRequest struct {
	OperationID       string          `json:"operationId"`
	TargetClaimID     string          `json:"targetClaimId,omitempty"`
	TargetOperationID string          `json:"targetOperationId,omitempty"`
	RequestHash       string          `json:"requestSha256,omitempty"`
	RequestNotAfter   time.Time       `json:"requestNotAfter,omitempty"`
	Outcome           string          `json:"outcome,omitempty"`
	Evidence          json.RawMessage `json:"evidence,omitempty"`
}
type Handle struct {
	SchemaVersion       int              `json:"schemaVersion"`
	AuthorityID         string           `json:"authorityId"`
	ClaimID             string           `json:"claimId"`
	Token               string           `json:"token"`
	Revision            int64            `json:"revision,omitempty"`
	Resources           []string         `json:"resources"`
	ExpiresAt           time.Time        `json:"expiresAt,omitempty"`
	AgentID             string           `json:"agentId"`
	SessionID           string           `json:"sessionId"`
	LocalReplaceAllowed bool             `json:"localReplaceAllowed"`
	State               string           `json:"state"`
	PendingRequest      *PendingRequest  `json:"pendingRequest,omitempty"`
	RecoveryRequest     *RecoveryRequest `json:"recoveryRequest,omitempty"`
	HoldUntil           time.Time        `json:"holdUntil,omitempty"`
	AutoRenewOwner      string           `json:"autoRenewOwner,omitempty"`
}

func newHandleError(r, msg string) error { return reason.New(r, msg) }
func validateToken(v string) error {
	if len(v) != 64 {
		return newHandleError(reason.ReasonCredentialMalformed, "credential is malformed")
	}
	b, err := hex.DecodeString(v)
	if err != nil || hex.EncodeToString(b) != v {
		return newHandleError(reason.ReasonCredentialMalformed, "credential is malformed")
	}
	return nil
}
func validID(v string) bool {
	if len(v) != 32 {
		return false
	}
	b, e := hex.DecodeString(v)
	return e == nil && hex.EncodeToString(b) == v
}
func validResource(v string) bool {
	return utf8.ValidString(v) && len(v) > 0 && len([]byte(v)) <= 1024 && !strings.ContainsAny(v, "\x00\r\n") && strings.TrimSpace(v) == v
}
func validateHandle(h Handle) error {
	if h.SchemaVersion != SchemaVersion || (h.State != "pending" && h.State != "ready") || !validID(h.AuthorityID) || !validID(h.ClaimID) {
		return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	if err := validateToken(h.Token); err != nil {
		return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	if len(h.Resources) < 1 || len(h.Resources) > 32 {
		return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	seen := map[string]bool{}
	for _, r := range h.Resources {
		if !validResource(r) || seen[r] {
			return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
		}
		seen[r] = true
	}
	if h.AgentID == "" || h.SessionID == "" || !validPublicText(h.AgentID) || !validPublicText(h.SessionID) {
		return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	if h.State == "ready" && (h.Revision < 1 || h.ExpiresAt.IsZero() || h.PendingRequest != nil) {
		return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	if h.State == "pending" && h.PendingRequest == nil {
		return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	if p := h.PendingRequest; p != nil {
		if !validID(p.OperationID) || !validID(p.AuthorityID) || p.AuthorityID != h.AuthorityID || !validID(p.ClaimID) || p.ClaimID != h.ClaimID || p.RequestHash == "" || !validHash(p.RequestHash) || p.RequestNotAfter.IsZero() || p.Inputs == nil {
			return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
		}
		switch p.Kind {
		case "acquire", "heartbeat", "checkpoint", "release", "transfer", "exec", "replace-file":
		default:
			return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
		}
		if p.Kind == "transfer" && validateToken(p.SuccessorToken) != nil {
			return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
		}
	}
	if r := h.RecoveryRequest; r != nil {
		if !validID(r.OperationID) || (!validID(r.TargetClaimID) && !validID(r.TargetOperationID)) || r.RequestHash == "" || !validHash(r.RequestHash) || r.RequestNotAfter.IsZero() {
			return newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
		}
	}
	return nil
}
func validHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	b, err := hex.DecodeString(v)
	return err == nil && hex.EncodeToString(b) == v
}
func validPublicText(v string) bool {
	return utf8.ValidString(v) && len([]byte(v)) <= 128 && !strings.ContainsAny(v, "\x00\r\n") && strings.TrimSpace(v) == v
}

// ValidateMetadata checks a handle path using metadata only. It never opens or
// reads the leaf, and reports an absent leaf separately from an unsafe parent
// or leaf. The parent must be an existing private directory owned by this user.
func ValidateMetadata(path string) (present bool, err error) {
	_, parentErr := os.Lstat(filepath.Dir(path))
	if errors.Is(parentErr, os.ErrNotExist) {
		return false, os.ErrNotExist
	}
	if parentErr != nil {
		return false, parentErr
	}
	if err := trustedParent(path); err != nil {
		return false, err
	}
	_, err = inspect(path, true)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func trustedParent(path string) error {
	p := filepath.Dir(path)
	st, err := os.Lstat(p)
	if err != nil || !st.IsDir() || st.Mode()&0o077 != 0 || st.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(st) {
		return newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint32(stat.Uid) == uint32(os.Geteuid())
}
func inspect(path string, requirePrivate bool) (os.FileInfo, error) {
	st, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	stat, statOK := st.Sys().(*syscall.Stat_t)
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || !statOK || !ownedByCurrentUser(st) || stat.Nlink != 1 || (requirePrivate && st.Mode()&0o077 != 0) {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	return st, nil
}
func Read(path string) (Handle, error) {
	if err := trustedParent(path); err != nil {
		return Handle{}, err
	}
	if _, err := inspect(path, true); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is missing")
		}
		return Handle{}, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return Handle{}, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil || len(b) > MaxBytes {
		return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is oversized")
	}
	if err := rejectDuplicateJSONKeys(b); err != nil {
		return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	var h Handle
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(&h); err != nil {
		return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	if err := validateHandle(h); err != nil {
		return Handle{}, err
	}
	return h, nil
}
func rejectDuplicateJSONKeys(data []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var visit func() error
	visit = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return errors.New("duplicate object key")
				}
				seen[key] = struct{}{}
				if err := visit(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := visit(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		default:
			return errors.New("invalid JSON delimiter")
		}
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func encoded(h Handle) ([]byte, error) {
	if err := validateHandle(h); err != nil {
		return nil, err
	}
	b, err := json.Marshal(h)
	if err != nil {
		return nil, newHandleError(reason.ReasonHandleMalformed, "handle is malformed")
	}
	b = append(b, '\n')
	if len(b) > MaxBytes {
		return nil, newHandleError(reason.ReasonHandleMalformed, "handle is oversized")
	}
	return b, nil
}
func Write(path string, h Handle) error {
	if err := trustedParent(path); err != nil {
		return err
	}
	b, err := encoded(h)
	if err != nil {
		return err
	}
	if _, e := os.Lstat(path); e == nil {
		old, readErr := Read(path)
		if readErr != nil {
			// An existing malformed or unsafe file is never an absent slot.
			return readErr
		}
		if old.AuthorityID != h.AuthorityID {
			return newHandleError(reason.ReasonAuthorityMismatch, "handle authority does not match")
		}
		if old.State == "pending" && old.ClaimID != h.ClaimID {
			return newHandleError(reason.ReasonHandleInUse, "pending handle is in use")
		}
		if old.State == "ready" && old.ClaimID != h.ClaimID && old.ExpiresAt.After(time.Now()) {
			return newHandleError(reason.ReasonHandleInUse, "active handle is in use")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	tmp, err := os.OpenFile(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-"+randomName()), os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	if err = os.Rename(name, path); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	return nil
}
func randomName() string {
	var b [8]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "random"
	}
	return hex.EncodeToString(b[:])
}
func Remove(path string) error {
	if err := trustedParent(path); err != nil {
		return err
	}
	if _, err := inspect(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be removed")
	}
	d, e := os.Open(filepath.Dir(path))
	if e == nil {
		e = d.Sync()
		d.Close()
	}
	return e
}

type Lock struct {
	f    *os.File
	path string
}

func AcquireExistingLock(ctx context.Context, path string) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := trustedParent(path); err != nil {
		return nil, err
	}
	st, err := inspect(path, true)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Lock{}, nil
		}
		return nil, err
	}
	if st == nil {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
		if err == nil {
			return &Lock{f: f, path: path}, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			_ = f.Close()
			return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock cannot be acquired")
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, newHandleError(reason.ReasonHandleInUse, "handle is in use")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func AcquireLock(ctx context.Context, path string) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := trustedParent(path); err != nil {
		return nil, err
	}
	st, e := os.Lstat(path)
	if e == nil && (st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || st.Mode()&0o077 != 0 || st.Sys().(*syscall.Stat_t).Nlink != 1 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid())) {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	f, e := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_CLOEXEC, 0o600)
	if e != nil {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	for {
		e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			return &Lock{f: f, path: path}, nil
		}
		if e != unix.EWOULDBLOCK && e != unix.EAGAIN {
			f.Close()
			return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock cannot be acquired")
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, newHandleError(reason.ReasonHandleInUse, "handle is in use")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func AcquireLocks(ctx context.Context, paths ...string) ([]*Lock, error) {
	canonical := make([]string, len(paths))
	for i, p := range paths {
		v, e := filepath.Abs(p)
		if e != nil {
			return nil, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
		}
		// Canonicalize existing symlinked parents so aliases cannot acquire
		// different lock orders or bypass the same-destination check.
		if resolved, e := filepath.EvalSymlinks(filepath.Dir(v)); e == nil {
			v = filepath.Join(resolved, filepath.Base(v))
		}
		canonical[i] = filepath.Clean(v)
	}
	for i := 0; i < len(canonical); i++ {
		for j := i + 1; j < len(canonical); j++ {
			if canonical[i] == canonical[j] {
				return nil, newHandleError(reason.ReasonCredentialSourceConflict, "transfer destinations must be distinct")
			}
			if canonical[j] < canonical[i] {
				canonical[i], canonical[j] = canonical[j], canonical[i]
			}
		}
	}
	locks := make([]*Lock, 0, len(canonical))
	for _, p := range canonical {
		l, e := AcquireLock(ctx, p)
		if e != nil {
			for _, held := range locks {
				held.Close()
			}
			return nil, e
		}
		locks = append(locks, l)
	}
	return locks, nil
}
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	e := unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	if ce := l.f.Close(); e == nil {
		e = ce
	}
	return e
}

func ReadCredential(path string) (string, error) {
	parent := filepath.Dir(path)
	parentInfo, e := os.Lstat(parent)
	if e != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 || parentInfo.Mode()&0o077 != 0 || parentInfo.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source directory is unsafe")
	}
	st, e := os.Lstat(path)
	if e != nil {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source cannot be read safely")
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || st.Mode()&0o077 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) || st.Sys().(*syscall.Stat_t).Nlink != 1 {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source is unsafe")
	}
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source cannot be read safely")
	}
	defer f.Close()
	return readCredential(f)
}
func ReadCredentialFD(fd int) (string, error) {
	if fd < 0 {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor is invalid")
	}
	dup, e := unix.Dup(fd)
	if e != nil {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be read")
	}
	defer unix.Close(dup)
	unix.CloseOnExec(dup)
	return readCredential(os.NewFile(uintptr(dup), "credential"))
}
func readCredential(r io.Reader) (string, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxCredentialBytes+1))
	if e != nil || len(b) > MaxCredentialBytes {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source is oversized")
	}
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	if len(b) != 64 {
		return "", newHandleError(reason.ReasonCredentialMalformed, "credential is malformed")
	}
	v := string(b)
	if err := validateToken(v); err != nil {
		return "", err
	}
	return v, nil
}
func ResolveCredential(path string, fd *int) (string, error) {
	if (path != "") == (fd != nil) {
		return "", newHandleError(reason.ReasonCredentialSourceConflict, "exactly one credential source is required")
	}
	if fd != nil {
		return ReadCredentialFD(*fd)
	}
	return ReadCredential(path)
}
