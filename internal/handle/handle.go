// Package handle implements private, owner-only client lease handles.
package handle

import (
	"bytes"
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
	"reflect"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
	"golang.org/x/sys/unix"
)

const (
	SchemaVersion       = 1
	RemoteSchemaVersion = 2
	MaxBytes            = 64 * 1024
	RemoteMaxBytes      = 1 * 1024 * 1024
	MaxCredentialBytes  = 4096
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
	OperationID       string          `json:"operationId,omitempty"`
	Kind              string          `json:"kind"`
	AuthorityID       string          `json:"authorityId"`
	Endpoint          string          `json:"endpoint,omitempty"`
	CertificateSHA256 string          `json:"certificateSha256,omitempty"`
	ClaimID           string          `json:"claimId,omitempty"`
	RequestHash       string          `json:"requestSha256,omitempty"`
	RequestNotAfter   time.Time       `json:"requestNotAfter,omitempty"`
	ExpectedRestoreID string          `json:"expectedRestoreId,omitempty"`
	Request           []byte          `json:"request,omitempty"`
	ParentRequestID   string          `json:"parentRequestId,omitempty"`
	EffectEvidence    json.RawMessage `json:"effectEvidence,omitempty"`
	Inputs            map[string]any  `json:"inputs,omitempty"`
	SuccessorToken    string          `json:"successorToken,omitempty"`
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
	if (h.SchemaVersion != SchemaVersion && h.SchemaVersion != RemoteSchemaVersion) || (h.State != "pending" && h.State != "ready") || !validID(h.AuthorityID) || !validID(h.ClaimID) {
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
		if !validID(p.OperationID) || !validID(p.AuthorityID) || p.AuthorityID != h.AuthorityID || !validID(p.ClaimID) || p.ClaimID != h.ClaimID || (p.ExpectedRestoreID != "" && !validID(p.ExpectedRestoreID)) || p.RequestHash == "" || !validHash(p.RequestHash) || p.RequestNotAfter.IsZero() || p.Inputs == nil || len(p.Request) > MaxBytes {
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

// fileIdentity is the stable identity used to bind a pathname to an opened
// descriptor. Device and inode are sufficient for the local filesystem
// integrity boundary; this package does not attempt to fence a hostile UID.
type fileIdentity struct {
	dev uint64
	ino uint64
}

func identity(st *unix.Stat_t) fileIdentity {
	return fileIdentity{dev: uint64(st.Dev), ino: uint64(st.Ino)}
}
func sameIdentity(a, b fileIdentity) bool { return a == b }

func statFD(f *os.File) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return st, err
	}
	return st, nil
}
func statAt(dir *os.File, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return st, err
	}
	return st, nil
}
func validateParentFD(f *os.File) (unix.Stat_t, error) {
	st, err := statFD(f)
	if err != nil {
		return st, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Geteuid()) || st.Mode&0o077 != 0 || st.Nlink == 0 {
		return st, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	return st, nil
}
func validateLeafStat(st *unix.Stat_t, requirePrivate bool) error {
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || (requirePrivate && st.Mode&0o077 != 0) {
		return newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	return nil
}

// openParent walks every ancestor through an opened descriptor. No component
// is followed as a symlink, and only the final directory is subject to the
// owner/private policy (system ancestors such as /tmp cannot be private).
func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	absolute = filepath.Clean(absolute)
	// macOS exposes these system directories through symlink aliases. Resolve
	// only those fixed system aliases; user-controlled ancestors remain
	// subject to O_NOFOLLOW below.
	if runtime.GOOS == "darwin" {
		if strings.HasPrefix(absolute, "/var/") {
			absolute = "/private" + absolute
		} else if strings.HasPrefix(absolute, "/tmp/") {
			absolute = "/private" + absolute
		}
	}
	return absolute, nil
}

// EnsureOwnerPrivateDir creates a directory tree without following a
// symlink and verifies the final directory is owner-private.
func EnsureOwnerPrivateDir(path string) error {
	absolute, err := canonicalPath(path)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimPrefix(absolute, "/"), "/")
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) {
			if e = unix.Mkdirat(fd, part, 0700); e == nil {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		_ = unix.Close(fd)
		if e != nil {
			return newHandleError(reason.ReasonHandleUnsafe, "private directory is unsafe")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), absolute)
	defer f.Close()
	if err = f.Chmod(0700); err != nil {
		return newHandleError(reason.ReasonHandleUnsafe, "private directory is unsafe")
	}
	_, err = validateParentFD(f)
	return err
}

func openParent(path string) (*os.File, string, string, error) {
	absolute, err := canonicalPath(path)
	if err != nil {
		return nil, "", "", err
	}
	parent, name := filepath.Dir(absolute), filepath.Base(absolute)
	if name == "." || name == string(filepath.Separator) || parent == absolute {
		return nil, "", "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	parts := strings.Split(strings.TrimPrefix(parent, string(filepath.Separator)), string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) {
				return nil, "", "", os.ErrNotExist
			}
			return nil, "", "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), parent)
	if _, err := validateParentFD(f); err != nil {
		_ = f.Close()
		return nil, "", "", err
	}
	return f, name, absolute, nil
}
func verifyParentPath(path string, expected fileIdentity) error {
	p, _, _, err := openParent(path)
	if err != nil {
		return err
	}
	defer p.Close()
	st, err := validateParentFD(p)
	if err != nil || !sameIdentity(expected, identity(&st)) {
		return newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	return nil
}

// ValidateMetadata checks metadata without opening or reading the leaf.
func ValidateMetadata(path string) (present bool, err error) {
	parent, name, _, err := openParent(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, os.ErrNotExist
	}
	if err != nil {
		return false, err
	}
	defer parent.Close()
	st, err := statAt(parent, name)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	if err := validateLeafStat(&st, true); err != nil {
		return false, err
	}
	return true, nil
}

func readAt(parent *os.File, name string) (Handle, error) {
	fd, openErr := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if openErr != nil {
		if errors.Is(openErr, unix.ENOENT) {
			return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is missing")
		}
		return Handle{}, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	st, err := statFD(f)
	if err != nil {
		return Handle{}, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	if err := validateLeafStat(&st, true); err != nil {
		return Handle{}, err
	}
	b, err := io.ReadAll(io.LimitReader(f, RemoteMaxBytes+1))
	if err != nil || len(b) > RemoteMaxBytes {
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
	limit := MaxBytes
	if h.SchemaVersion == RemoteSchemaVersion {
		limit = RemoteMaxBytes
	}
	if len(b) > limit {
		return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is oversized")
	}
	current, err := statAt(parent, name)
	if err != nil || validateLeafStat(&current, true) != nil || !sameIdentity(identity(&st), identity(&current)) {
		return Handle{}, newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	return h, nil
}
func Read(path string) (Handle, error) {
	parent, name, absolute, err := openParent(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Handle{}, newHandleError(reason.ReasonHandleMalformed, "handle is missing")
		}
		return Handle{}, err
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return Handle{}, err
	}
	if err := verifyParentPath(absolute, identity(&st)); err != nil {
		return Handle{}, err
	}
	h, err := readAt(parent, name)
	if err != nil {
		return Handle{}, err
	}
	if err := verifyParentPath(absolute, identity(&st)); err != nil {
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
	limit := MaxBytes
	if h.SchemaVersion == RemoteSchemaVersion {
		limit = RemoteMaxBytes
	}
	if len(b) > limit {
		return nil, newHandleError(reason.ReasonHandleMalformed, "handle is oversized")
	}
	return b, nil
}
func writeAt(parent *os.File, name string, h Handle, expected ...*Handle) error {
	b, err := encoded(h)
	if err != nil {
		return err
	}
	if st, statErr := statAt(parent, name); statErr == nil {
		if err := validateLeafStat(&st, true); err != nil {
			return err
		}
		old, readErr := readAt(parent, name)
		if readErr != nil {
			return readErr
		}
		if old.AuthorityID != h.AuthorityID {
			return newHandleError(reason.ReasonAuthorityMismatch, "handle authority does not match")
		}
		if old.State == "pending" && old.ClaimID != h.ClaimID {
			return newHandleError(reason.ReasonHandleInUse, "pending handle is in use")
		}
		replacingExpectedReady := len(expected) == 1 && expected[0] != nil && old.State == "ready" && reflect.DeepEqual(old, *expected[0])
		if old.State == "ready" && old.ClaimID != h.ClaimID && old.ExpiresAt.After(time.Now()) && !replacingExpectedReady {
			return newHandleError(reason.ReasonHandleInUse, "active handle is in use")
		}
	} else if !errors.Is(statErr, unix.ENOENT) {
		return newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	tmpName := "." + name + ".tmp-" + randomName()
	fd, err := unix.Openat(int(parent.Fd()), tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	tmp := os.NewFile(uintptr(fd), tmpName)
	defer unix.Unlinkat(int(parent.Fd()), tmpName, 0)
	tmpST, statErr := statFD(tmp)
	if statErr != nil || validateLeafStat(&tmpST, true) != nil {
		_ = tmp.Close()
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	if err = unix.Renameat(int(parent.Fd()), tmpName, int(parent.Fd()), name); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	if err = parent.Sync(); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be written")
	}
	return nil
}
func Write(path string, h Handle) error {
	parent, name, absolute, err := openParent(path)
	if err != nil {
		return err
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return err
	}
	if err := verifyParentPath(absolute, identity(&st)); err != nil {
		return err
	}
	if err := writeAt(parent, name, h); err != nil {
		return err
	}
	if err := verifyParentPath(absolute, identity(&st)); err != nil {
		return err
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
func removeAt(parent *os.File, name string) error {
	st, err := statAt(parent, name)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	if err := validateLeafStat(&st, false); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be removed")
	}
	if err := parent.Sync(); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "handle cannot be removed")
	}
	return nil
}
func Remove(path string) error {
	parent, name, absolute, err := openParent(path)
	if err != nil {
		return err
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return err
	}
	if err := verifyParentPath(absolute, identity(&st)); err != nil {
		return err
	}
	if err := removeAt(parent, name); err != nil {
		return err
	}
	return verifyParentPath(absolute, identity(&st))
}

var afterLockOpenForTest = func() {}

type Lock struct {
	f          *os.File
	parent     *os.File
	path       string
	parentPath string
	parentID   fileIdentity
	lockName   string
	lockID     fileIdentity
}

func lockFromFiles(path string, parent *os.File, name string, f *os.File, lockST, parentST *unix.Stat_t) *Lock {
	return &Lock{f: f, parent: parent, path: path, parentPath: filepath.Dir(path), parentID: identity(parentST), lockName: name, lockID: identity(lockST)}
}

func AcquireExistingLock(ctx context.Context, path string) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent, name, absolute, err := openParent(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Lock{}, nil
	}
	if err != nil {
		return nil, err
	}
	parentST, err := validateParentFD(parent)
	if err != nil {
		parent.Close()
		return nil, err
	}
	lockST, err := statAt(parent, name)
	if errors.Is(err, unix.ENOENT) {
		parent.Close()
		return &Lock{}, nil
	}
	if err != nil || validateLeafStat(&lockST, true) != nil {
		parent.Close()
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		parent.Close()
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	f := os.NewFile(uintptr(fd), absolute)
	openedST, statErr := statFD(f)
	if statErr != nil || validateLeafStat(&openedST, true) != nil || !sameIdentity(identity(&lockST), identity(&openedST)) {
		f.Close()
		parent.Close()
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	afterLockOpenForTest()
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
		if err == nil {
			if err = verifyLockPath(parent, name, identity(&openedST)); err == nil {
				err = verifyParentPath(absolute, identity(&parentST))
			}
			if err != nil {
				f.Close()
				parent.Close()
				return nil, err
			}
			return lockFromFiles(absolute, parent, name, f, &openedST, &parentST), nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			f.Close()
			parent.Close()
			return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock cannot be acquired")
		}
		select {
		case <-ctx.Done():
			f.Close()
			parent.Close()
			return nil, newHandleError(reason.ReasonHandleInUse, "handle is in use")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func AcquireLock(ctx context.Context, path string) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent, name, absolute, err := openParent(path)
	if err != nil {
		return nil, err
	}
	parentST, err := validateParentFD(parent)
	if err != nil {
		parent.Close()
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		parent.Close()
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	f := os.NewFile(uintptr(fd), absolute)
	lockST, err := statFD(f)
	if err != nil || validateLeafStat(&lockST, true) != nil {
		f.Close()
		parent.Close()
		return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	afterLockOpenForTest()
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			if err = verifyLockPath(parent, name, identity(&lockST)); err == nil {
				err = verifyParentPath(absolute, identity(&parentST))
			}
			if err != nil {
				f.Close()
				parent.Close()
				return nil, err
			}
			return lockFromFiles(absolute, parent, name, f, &lockST, &parentST), nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			f.Close()
			parent.Close()
			return nil, newHandleError(reason.ReasonHandleUnsafe, "handle lock cannot be acquired")
		}
		select {
		case <-ctx.Done():
			f.Close()
			parent.Close()
			return nil, newHandleError(reason.ReasonHandleInUse, "handle is in use")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func verifyLockPath(parent *os.File, name string, expected fileIdentity) error {
	st, err := statAt(parent, name)
	if err != nil || validateLeafStat(&st, true) != nil || !sameIdentity(expected, identity(&st)) {
		return newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	return nil
}
func AcquireLocks(ctx context.Context, paths ...string) ([]*Lock, error) {
	canonical := make([]string, len(paths))
	for i, p := range paths {
		v, err := canonicalPath(p)
		if err != nil {
			return nil, err
		}
		canonical[i] = v
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
func (l *Lock) validateFor(path string) (string, error) {
	if l == nil || l.f == nil || l.parent == nil {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle lock is unavailable")
	}
	if !l.Matches(path) {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle lock does not match path")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	absolute = filepath.Clean(absolute)
	parent, name, canonical, err := openParent(absolute)
	if err != nil {
		return "", err
	}
	st, statErr := validateParentFD(parent)
	parent.Close()
	if statErr != nil || filepath.Dir(canonical) != l.parentPath || !sameIdentity(l.parentID, identity(&st)) {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	parentST, statErr := validateParentFD(l.parent)
	if statErr != nil || !sameIdentity(l.parentID, identity(&parentST)) {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle path is unsafe")
	}
	fdST, statErr := statFD(l.f)
	if statErr != nil || validateLeafStat(&fdST, true) != nil || !sameIdentity(l.lockID, identity(&fdST)) {
		return "", newHandleError(reason.ReasonHandleUnsafe, "handle lock is unsafe")
	}
	if err := verifyLockPath(l.parent, l.lockName, l.lockID); err != nil {
		return "", err
	}
	return name, nil
}
func (l *Lock) Read(path string) (Handle, error) {
	name, err := l.validateFor(path)
	if err != nil {
		return Handle{}, err
	}
	h, err := readAt(l.parent, name)
	if err != nil {
		return Handle{}, err
	}
	if _, err = l.validateFor(path); err != nil {
		return Handle{}, err
	}
	return h, nil
}
func (l *Lock) Write(path string, h Handle) error {
	name, err := l.validateFor(path)
	if err != nil {
		return err
	}
	if err := writeAt(l.parent, name, h); err != nil {
		return err
	}
	return l.validateAfter(path)
}

// ReplaceReady atomically replaces one exactly revalidated ready handle. It is
// used only after an external authority has established that the old epoch is
// inactive; ordinary writes retain the local-expiry overwrite guard.
func (l *Lock) ReplaceReady(path string, expected, next Handle) error {
	name, err := l.validateFor(path)
	if err != nil {
		return err
	}
	old, err := readAt(l.parent, name)
	if err != nil {
		return err
	}
	if old.State != "ready" || !reflect.DeepEqual(old, expected) {
		return newHandleError(reason.ReasonHandleInUse, "handle changed before replacement")
	}
	if err := writeAt(l.parent, name, next, &expected); err != nil {
		return err
	}
	return l.validateAfter(path)
}
func (l *Lock) Remove(path string) error {
	name, err := l.validateFor(path)
	if err != nil {
		return err
	}
	if err := removeAt(l.parent, name); err != nil {
		return err
	}
	return l.validateAfter(path)
}
func (l *Lock) validateAfter(path string) error {
	_, err := l.validateFor(path)
	return err
}
func (l *Lock) ClearPending(path string, h *Handle) error {
	if h == nil {
		return nil
	}
	if h.Revision > 0 && !h.ExpiresAt.IsZero() {
		h.State, h.PendingRequest = "ready", nil
		return l.Write(path, *h)
	}
	return l.Remove(path)
}

// Path returns the lock pathname bound to this descriptor.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Matches reports whether this lock protects path's sibling lock pathname.
func (l *Lock) Matches(path string) bool {
	if l == nil || l.f == nil {
		return false
	}
	canonical, err := canonicalPath(path + ".lock")
	return err == nil && canonical == l.path
}

func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	e := unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	if ce := l.f.Close(); e == nil {
		e = ce
	}
	if l.parent != nil {
		if ce := l.parent.Close(); e == nil {
			e = ce
		}
	}
	return e
}

func ReadCredential(path string) (string, error) {
	b, err := ReadOwnerPrivate(path, MaxCredentialBytes)
	if err != nil {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source cannot be read safely")
	}
	return readCredential(bytes.NewReader(b))
}
func ReadBoundedFD(fd int, max int64) ([]byte, error) {
	if fd < 0 || max < 0 {
		return nil, newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor is invalid")
	}
	dup, e := unix.Dup(fd)
	if e != nil {
		return nil, newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be read")
	}
	unix.CloseOnExec(dup)
	file := os.NewFile(uintptr(dup), "bounded-input")
	if file == nil {
		_ = unix.Close(dup)
		return nil, newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be read")
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, max+1))
	if e != nil || int64(len(data)) > max {
		return nil, newHandleError(reason.ReasonCredentialUnsafe, "credential source is oversized")
	}
	return data, nil
}

func WriteBoundedFD(fd int, data []byte, max int64) error {
	if fd < 0 || max < 0 || int64(len(data)) > max {
		return newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor is invalid")
	}
	dup, err := unix.Dup(fd)
	if err != nil {
		return newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be written")
	}
	unix.CloseOnExec(dup)
	file := os.NewFile(uintptr(dup), "bounded-output")
	if file == nil {
		_ = unix.Close(dup)
		return newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be written")
	}
	if _, err = file.Write(data); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		return newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be written")
	}
	return nil
}

func ReadCredentialFD(fd int) (string, error) {
	if fd < 0 {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor is invalid")
	}
	dup, e := unix.Dup(fd)
	if e != nil {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be read")
	}
	unix.CloseOnExec(dup)
	file := os.NewFile(uintptr(dup), "credential")
	if file == nil {
		_ = unix.Close(dup)
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential descriptor cannot be read")
	}
	defer file.Close()
	return readCredential(file)
}
func readCredential(r io.Reader) (string, error) {
	b, e := io.ReadAll(io.LimitReader(r, MaxCredentialBytes+1))
	if e != nil || len(b) > MaxCredentialBytes {
		return "", newHandleError(reason.ReasonCredentialUnsafe, "credential source is oversized")
	}
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	v := string(b)
	if len(v) != 64 {
		return "", newHandleError(reason.ReasonCredentialMalformed, "credential is malformed")
	}
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

// StoreCredential durably creates an owner-private credential file. The
// plaintext is accepted only in memory and is never part of a profile or
// request record.
func StoreCredential(path, credential string) error {
	return storeCredential(path, credential, false)
}

// StoreCredentialNoReplace creates an enrollment credential without replacing
// an existing enrolled credential.
func StoreCredentialNoReplace(path, credential string) error {
	return storeCredential(path, credential, true)
}

func storeCredential(path, credential string, noReplace bool) error {
	if err := validateToken(credential); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := EnsureOwnerPrivateDir(parent); err != nil {
		return newHandleError(reason.ReasonCredentialUnsafe, "credential source directory is unsafe")
	}
	if noReplace {
		return WriteOwnerPrivateNoReplace(path, []byte(credential+"\n"), MaxCredentialBytes+1)
	}
	return WriteOwnerPrivate(path, []byte(credential+"\n"), MaxCredentialBytes+1)
}

// ReadOwnerPrivate reads a bounded owner-private regular file without
// following a replaced leaf and while pinning the opened parent.
func ReadOwnerPrivate(path string, max int64) ([]byte, error) {
	parent, name, absolute, err := openParent(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return nil, err
	}
	if err = verifyParentPath(absolute, identity(&st)); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, newHandleError(reason.ReasonHandleUnsafe, "private file is unsafe")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	leaf, err := statFD(f)
	if err != nil || validateLeafStat(&leaf, true) != nil {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "private file is unsafe")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, newHandleError(reason.ReasonHandleMalformed, "private file is oversized")
	}
	if err = verifyParentPath(absolute, identity(&st)); err != nil {
		return nil, err
	}
	return b, nil
}

// WriteOwnerPrivate atomically writes a bounded owner-private file using the
// pinned parent descriptor. The parent must already be owner-private.
func WriteOwnerPrivate(path string, data []byte, max int64) error {
	return writeOwnerPrivate(path, data, max, false)
}

// WriteOwnerPrivateNoReplace atomically creates a private file and fails if
// another record already occupies the leaf.
func WriteOwnerPrivateNoReplace(path string, data []byte, max int64) error {
	return writeOwnerPrivate(path, data, max, true)
}
func writeOwnerPrivate(path string, data []byte, max int64, noReplace bool) error {
	if int64(len(data)) > max {
		return newHandleError(reason.ReasonHandleWriteFailed, "private file is oversized")
	}
	parent, name, absolute, err := openParent(path)
	if err != nil {
		return newHandleError(reason.ReasonHandleUnsafe, "private path is unsafe")
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return err
	}
	if err = verifyParentPath(absolute, identity(&st)); err != nil {
		return err
	}
	tmpName := "." + name + ".tmp-" + randomName()
	fd, err := unix.Openat(int(parent.Fd()), tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "private file cannot be written")
	}
	tmp := os.NewFile(uintptr(fd), tmpName)
	defer unix.Unlinkat(int(parent.Fd()), tmpName, 0)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if ce := tmp.Close(); err == nil {
		err = ce
	}
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "private file cannot be written")
	}
	if noReplace {
		err = unix.Linkat(int(parent.Fd()), tmpName, int(parent.Fd()), name, 0)
		if err == nil {
			err = unix.Unlinkat(int(parent.Fd()), tmpName, 0)
		}
	} else {
		err = unix.Renameat(int(parent.Fd()), tmpName, int(parent.Fd()), name)
	}
	if err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "private file cannot be written")
	}
	if err = parent.Sync(); err != nil {
		return newHandleError(reason.ReasonHandleWriteFailed, "private file cannot be written")
	}
	return verifyParentPath(absolute, identity(&st))
}

// RemoveOwnerPrivate removes an owner-private file through its pinned parent.
// ListOwnerPrivateNames returns the names of regular private files in an
// owner-private directory while pinning that directory's identity.
func ListOwnerPrivateNames(path string) ([]string, error) {
	if !filepath.IsAbs(path) {
		return nil, newHandleError(reason.ReasonHandleUnsafe, "private directory must be absolute")
	}
	parent, _, absolute, err := openParent(filepath.Join(path, ".list"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return nil, err
	}
	entries, err := parent.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	if err := verifyParentPath(absolute, identity(&st)); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// RemoveOwnerPrivateIfContent atomically quarantines an owner-private file,
// then removes it only when its bounded contents match the caller's prior read.
func RemoveOwnerPrivateIfContent(path string, expected []byte, max int64) error {
	if int64(len(expected)) > max {
		return newHandleError(reason.ReasonHandleMalformed, "private file is oversized")
	}
	parent, name, absolute, err := openParent(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return newHandleError(reason.ReasonHandleUnsafe, "private path is unsafe")
	}
	defer parent.Close()
	parentST, err := validateParentFD(parent)
	if err != nil {
		return err
	}
	if err = verifyParentPath(absolute, identity(&parentST)); err != nil {
		return err
	}
	quarantine := "." + name + ".remove-" + randomName()
	if err = unix.Renameat(int(parent.Fd()), name, int(parent.Fd()), quarantine); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return os.ErrNotExist
		}
		return newHandleError(reason.ReasonHandleUnsafe, "private file cannot be quarantined")
	}
	restore := true
	defer func() {
		if restore {
			if unix.Linkat(int(parent.Fd()), quarantine, int(parent.Fd()), name, 0) == nil {
				_ = unix.Unlinkat(int(parent.Fd()), quarantine, 0)
			}
		}
	}()
	fd, err := unix.Openat(int(parent.Fd()), quarantine, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return newHandleError(reason.ReasonHandleUnsafe, "private file is unsafe")
	}
	file := os.NewFile(uintptr(fd), quarantine)
	leafST, err := statFD(file)
	if err != nil || validateLeafStat(&leafST, true) != nil {
		_ = file.Close()
		return newHandleError(reason.ReasonHandleUnsafe, "private file is unsafe")
	}
	contents, err := io.ReadAll(io.LimitReader(file, max+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || int64(len(contents)) > max {
		return newHandleError(reason.ReasonHandleMalformed, "private file cannot be read safely")
	}
	current, err := statAt(parent, quarantine)
	if err != nil || validateLeafStat(&current, true) != nil || !sameIdentity(identity(&leafST), identity(&current)) || !bytes.Equal(contents, expected) {
		return newHandleError(reason.ReasonHandleUnsafe, "private file changed before removal")
	}
	if err = unix.Unlinkat(int(parent.Fd()), quarantine, 0); err != nil {
		return newHandleError(reason.ReasonHandleUnsafe, "private file cannot be removed")
	}
	restore = false
	if err = parent.Sync(); err != nil {
		return newHandleError(reason.ReasonHandleUnsafe, "private file removal cannot be synchronized")
	}
	return verifyParentPath(absolute, identity(&parentST))
}

func RemoveOwnerPrivate(path string) error {
	parent, name, absolute, err := openParent(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return newHandleError(reason.ReasonHandleUnsafe, "private path is unsafe")
	}
	defer parent.Close()
	st, err := validateParentFD(parent)
	if err != nil {
		return err
	}
	if err = verifyParentPath(absolute, identity(&st)); err != nil {
		return err
	}
	if err = removeAt(parent, name); err != nil {
		return err
	}
	return verifyParentPath(absolute, identity(&st))
}

// ClearPending restores a handle after its pending request provably did not
// commit. A usable credential returns to the ready state; a pending grant that
// never committed is removed so the slot is free again.
func ClearPending(path string, h *Handle) error {
	if h == nil {
		return nil
	}
	if h.Revision > 0 && !h.ExpiresAt.IsZero() {
		h.State, h.PendingRequest = "ready", nil
		return Write(path, *h)
	}
	return Remove(path)
}
