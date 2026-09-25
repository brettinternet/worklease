package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/brettinternet/worklease/internal/handle"
	"golang.org/x/sys/unix"
)

const (
	maxQueueAdapterApprovalBytes  = 1 << 20
	maxQueueAdapterExecutableSize = 64 << 20
)

type queueAdapterApproval struct {
	SourceID     string            `json:"sourceId"`
	Path         string            `json:"executable"`
	SHA256       string            `json:"sha256"`
	Adapter      string            `json:"adapterId"`
	Version      string            `json:"version"`
	ConfigSHA256 string            `json:"configSha256"`
	Claims       *QueueClaims      `json:"claims"`
	Account      string            `json:"account"`
	Workflow     map[string]string `json:"workflow"`
}

type queueAdapterApprovals struct {
	Version   int                    `json:"version"`
	Approvals []queueAdapterApproval `json:"approvals"`
}

// QueueAdapterApprovalPath is the durable owner-private approval record beside queue.yaml.
func QueueAdapterApprovalPath(env func(string) string) string {
	return filepath.Join(filepath.Dir(QueuePath(env)), "queue-adapter-approvals.json")
}

// ApproveQueueAdapter records the current executable digest for one external source.
// It never invokes the executable; callers should only expose it through an explicit approval command.
func ApproveQueueAdapter(ctx context.Context, env func(string) string, source QueueSource) error {
	if _, err := queueAdapterBinding(source); err != nil {
		return err
	}
	path := QueueAdapterApprovalPath(env)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("queue adapter approval path must be absolute")
	}
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("queue adapter approval directory is unsafe: %w", err)
	}
	lock, err := handle.AcquireLock(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("queue adapter approval cannot be locked safely: %w", err)
	}
	defer lock.Close()

	entry, err := currentQueueAdapterApproval(source)
	if err != nil {
		return err
	}
	state, err := loadQueueAdapterApprovals(path)
	if err != nil {
		return err
	}
	updated := false
	for i := range state.Approvals {
		previous := &state.Approvals[i]
		if previous.SourceID == entry.SourceID && previous.Path == entry.Path && previous.Adapter == entry.Adapter && previous.Version == entry.Version {
			*previous = entry
			updated = true
			break
		}
	}
	if !updated {
		state.Approvals = append(state.Approvals, entry)
	}
	sortQueueAdapterApprovals(state.Approvals)
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("queue adapter approval cannot be encoded: %w", err)
	}
	if err := handle.WriteOwnerPrivate(path, data, maxQueueAdapterApprovalBytes); err != nil {
		return fmt.Errorf("queue adapter approval cannot be written safely: %w", err)
	}
	return nil
}

// CheckQueueAdapterApproval rehashes the configured executable and verifies its source-bound approval.
// Launchers should use PrepareQueueAdapterLaunch so the checked bytes cannot be replaced before exec.
func CheckQueueAdapterApproval(env func(string) string, source QueueSource) error {
	entry, err := currentQueueAdapterApproval(source)
	if err != nil {
		return err
	}
	state, err := loadQueueAdapterApprovals(QueueAdapterApprovalPath(env))
	if err != nil {
		return err
	}
	for _, approved := range state.Approvals {
		if sameQueueAdapterApproval(approved, entry) {
			return nil
		}
	}
	return fmt.Errorf("external adapter for source %q is not approved or its executable changed", source.ID)
}

func currentQueueAdapterApproval(source QueueSource) (queueAdapterApproval, error) {
	if _, err := queueAdapterBinding(source); err != nil {
		return queueAdapterApproval{}, err
	}
	canonical, digest, err := hashQueueAdapterExecutable(source.Executable)
	if err != nil {
		return queueAdapterApproval{}, err
	}
	return queueAdapterApprovalForDigest(source, canonical, digest)
}

func queueAdapterApprovalForDigest(source QueueSource, path, digest string) (queueAdapterApproval, error) {
	configuration := source.Config
	if configuration == nil {
		configuration = map[string]any{}
	}
	binding, err := json.Marshal(struct {
		Config        map[string]any `json:"config"`
		CredentialRef string         `json:"credentialRef"`
	}{configuration, source.CredentialRef})
	if err != nil {
		return queueAdapterApproval{}, fmt.Errorf("external adapter configuration cannot be encoded")
	}
	bindingHash := sha256.Sum256(binding)
	entry := queueAdapterApproval{
		SourceID:     source.ID,
		Path:         path,
		SHA256:       digest,
		Adapter:      source.ExpectedAdapterID,
		Version:      source.ExpectedVersion,
		ConfigSHA256: hex.EncodeToString(bindingHash[:]),
	}
	if source.Claims != nil {
		claims := *source.Claims
		entry.Claims = &claims
	}
	entry.Account = source.Account
	if source.Workflow != nil {
		entry.Workflow = make(map[string]string, len(source.Workflow))
		for intent, transition := range source.Workflow {
			entry.Workflow[intent] = transition
		}
	}
	return entry, nil
}

// QueueAdapterLaunchSnapshot is an owner-private copy of the exact approved executable bytes.
// Keep it alive until the child process exits, then Close it to remove its temporary directory.
type QueueAdapterLaunchSnapshot struct {
	path      string
	directory string
	closeOnce sync.Once
	closeErr  error
}

// Path returns the executable path of this verified immutable launch snapshot.
func (s *QueueAdapterLaunchSnapshot) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Close removes the private executable snapshot. It is safe to call more than once.
func (s *QueueAdapterLaunchSnapshot) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = os.RemoveAll(s.directory) })
	return s.closeErr
}

// PrepareQueueAdapterLaunch copies a safely opened executable into a private snapshot,
// then verifies the copied bytes and the complete source binding against durable approval.
func PrepareQueueAdapterLaunch(env func(string) string, source QueueSource) (*QueueAdapterLaunchSnapshot, error) {
	if _, err := queueAdapterBinding(source); err != nil {
		return nil, err
	}
	snapshot, digest, err := copyQueueAdapterExecutable(source.Executable)
	if err != nil {
		return nil, err
	}
	keepSnapshot := false
	defer func() {
		if !keepSnapshot {
			_ = snapshot.Close()
		}
	}()
	entry, err := queueAdapterApprovalForDigest(source, source.Executable, digest)
	if err != nil {
		return nil, err
	}
	state, err := loadQueueAdapterApprovals(QueueAdapterApprovalPath(env))
	if err != nil {
		return nil, err
	}
	for _, approved := range state.Approvals {
		if sameQueueAdapterApproval(approved, entry) {
			keepSnapshot = true
			return snapshot, nil
		}
	}
	return nil, fmt.Errorf("external adapter for source %q is not approved or its executable changed", source.ID)
}

// PrepareQueueAdapterCheckLaunch snapshots an explicitly supplied executable without
// recording or requiring approval. Only the conformance command may use this path;
// normal source launches must continue to require PrepareQueueAdapterLaunch.
func PrepareQueueAdapterCheckLaunch(path string) (*QueueAdapterLaunchSnapshot, error) {
	snapshot, _, err := copyQueueAdapterExecutable(path)
	return snapshot, err
}

func copyQueueAdapterExecutable(path string) (*QueueAdapterLaunchSnapshot, string, error) {
	source, err := openQueueAdapterExecutableNoSymlinks(path)
	if err != nil {
		return nil, "", err
	}
	defer source.Close()
	before, err := source.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o111 == 0 {
		return nil, "", fmt.Errorf("external adapter executable must be a readable executable regular file")
	}
	if before.Size() > maxQueueAdapterExecutableSize {
		return nil, "", fmt.Errorf("external adapter executable exceeds the %d-byte size limit", maxQueueAdapterExecutableSize)
	}
	directory, err := os.MkdirTemp("", "worklease-queue-adapter-")
	if err != nil {
		return nil, "", fmt.Errorf("external adapter launch snapshot cannot be created safely: %w", err)
	}
	keepDirectory := false
	defer func() {
		if !keepDirectory {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, "", fmt.Errorf("external adapter launch snapshot directory cannot be made private: %w", err)
	}
	dirFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fmt.Errorf("external adapter launch snapshot directory cannot be opened safely: %w", err)
	}
	defer unix.Close(dirFD)
	var dirInfo unix.Stat_t
	if err := unix.Fstat(dirFD, &dirInfo); err != nil || dirInfo.Mode&unix.S_IFMT != unix.S_IFDIR || dirInfo.Uid != uint32(os.Geteuid()) || dirInfo.Mode&0o7777 != 0o700 {
		return nil, "", fmt.Errorf("external adapter launch snapshot directory is not owner-private")
	}
	const snapshotName = "executable"
	fileFD, err := unix.Openat(dirFD, snapshotName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("external adapter launch snapshot file cannot be created safely: %w", err)
	}
	snapshotPath := filepath.Join(directory, snapshotName)
	output := os.NewFile(uintptr(fileFD), snapshotPath)
	outputOpen := true
	defer func() {
		if outputOpen {
			_ = output.Close()
		}
	}()
	hash := sha256.New()
	copied, err := io.Copy(io.MultiWriter(output, hash), io.LimitReader(source, maxQueueAdapterExecutableSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("external adapter executable cannot be copied safely: %w", err)
	}
	if copied > maxQueueAdapterExecutableSize {
		return nil, "", fmt.Errorf("external adapter executable exceeds the %d-byte size limit", maxQueueAdapterExecutableSize)
	}
	after, err := source.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		return nil, "", fmt.Errorf("external adapter executable changed while it was being copied")
	}
	if err := output.Sync(); err != nil {
		return nil, "", fmt.Errorf("external adapter launch snapshot cannot be synchronized: %w", err)
	}
	if err := output.Chmod(0o500); err != nil {
		return nil, "", fmt.Errorf("external adapter launch snapshot cannot be made executable: %w", err)
	}
	var snapshotInfo unix.Stat_t
	if err := unix.Fstat(fileFD, &snapshotInfo); err != nil || snapshotInfo.Mode&unix.S_IFMT != unix.S_IFREG || snapshotInfo.Uid != uint32(os.Geteuid()) || snapshotInfo.Mode&0o7777 != 0o500 {
		return nil, "", fmt.Errorf("external adapter launch snapshot file is not owner-private")
	}
	if err := output.Close(); err != nil {
		outputOpen = false
		return nil, "", fmt.Errorf("external adapter launch snapshot cannot be closed safely: %w", err)
	}
	outputOpen = false
	keepDirectory = true
	snapshot := &QueueAdapterLaunchSnapshot{path: snapshotPath, directory: directory}
	return snapshot, hex.EncodeToString(hash.Sum(nil)), nil
}

func queueAdapterBinding(source QueueSource) (queueAdapterApproval, error) {
	if source.Adapter != "external" {
		return queueAdapterApproval{}, fmt.Errorf("queue source %q is not an external adapter", source.ID)
	}
	if !validExternalAdapterID(source.ID) || !validExternalAdapterID(source.ExpectedAdapterID) || !validExternalAdapterVersion(source.ExpectedVersion) {
		return queueAdapterApproval{}, fmt.Errorf("external adapter source has an invalid source ID, expected adapter ID, or version")
	}
	if !filepath.IsAbs(source.Executable) || filepath.Clean(source.Executable) != source.Executable || strings.ContainsRune(source.Executable, '\x00') {
		return queueAdapterApproval{}, fmt.Errorf("external adapter executable path must be absolute and canonical")
	}
	if source.Claims != nil {
		if source.Claims.Policy != "generic" {
			return queueAdapterApproval{}, fmt.Errorf("external adapter claims policy must be generic")
		}
		if err := validateQueueClaimSource(source.Claims.Source); err != nil {
			return queueAdapterApproval{}, fmt.Errorf("external adapter claims source is invalid: %w", err)
		}
	}
	if source.Account != "" && !validExternalQueueText(source.Account) {
		return queueAdapterApproval{}, fmt.Errorf("external adapter account must be a non-empty safe principal")
	}
	for intent, transition := range source.Workflow {
		if !map[string]bool{"start": true, "blocked": true, "review": true, "complete": true, "reopen": true}[intent] || !validExternalQueueText(transition) {
			return queueAdapterApproval{}, fmt.Errorf("external adapter workflow mapping is invalid")
		}
	}
	return queueAdapterApproval{}, nil
}

func sameQueueAdapterApproval(a, b queueAdapterApproval) bool {
	if a.SourceID != b.SourceID || a.Path != b.Path || a.SHA256 != b.SHA256 || a.Adapter != b.Adapter || a.Version != b.Version || a.ConfigSHA256 != b.ConfigSHA256 || a.Account != b.Account || !reflect.DeepEqual(a.Workflow, b.Workflow) {
		return false
	}
	if a.Claims == nil || b.Claims == nil {
		return a.Claims == nil && b.Claims == nil
	}
	return *a.Claims == *b.Claims
}

func hashQueueAdapterExecutable(path string) (string, string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("external adapter executable cannot be resolved safely: %w", err)
	}
	if canonical != path {
		return "", "", fmt.Errorf("external adapter executable path cannot contain symlinks")
	}
	file, err := openQueueAdapterExecutableNoSymlinks(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o111 == 0 {
		return "", "", fmt.Errorf("external adapter executable must be a readable executable regular file")
	}
	if err := unix.Access(path, unix.X_OK); err != nil {
		return "", "", fmt.Errorf("external adapter executable is not executable: %w", err)
	}
	if before.Size() > maxQueueAdapterExecutableSize {
		return "", "", fmt.Errorf("external adapter executable exceeds the %d-byte size limit", maxQueueAdapterExecutableSize)
	}
	hash := sha256.New()
	copied, err := io.Copy(hash, io.LimitReader(file, maxQueueAdapterExecutableSize+1))
	if err != nil {
		return "", "", fmt.Errorf("external adapter executable cannot be read safely: %w", err)
	}
	if copied > maxQueueAdapterExecutableSize {
		return "", "", fmt.Errorf("external adapter executable exceeds the %d-byte size limit", maxQueueAdapterExecutableSize)
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
		return "", "", fmt.Errorf("external adapter executable changed while it was being checked")
	}
	resolvedAfter, err := filepath.EvalSymlinks(path)
	if err != nil || resolvedAfter != path {
		return "", "", fmt.Errorf("external adapter executable path changed while it was being checked")
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(after, current) {
		return "", "", fmt.Errorf("external adapter executable was redirected while it was being checked")
	}
	return path, hex.EncodeToString(hash.Sum(nil)), nil
}

func openQueueAdapterExecutableNoSymlinks(path string) (*os.File, error) {
	parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return nil, fmt.Errorf("external adapter executable path must name a file")
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("external adapter executable parent cannot be opened safely: %w", err)
	}
	for _, component := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, fmt.Errorf("external adapter executable path contains an unsafe directory: %w", openErr)
		}
		fd = next
	}
	fileFD, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	_ = unix.Close(fd)
	if err != nil {
		return nil, fmt.Errorf("external adapter executable cannot be opened safely: %w", err)
	}
	return os.NewFile(uintptr(fileFD), path), nil
}

func loadQueueAdapterApprovals(path string) (queueAdapterApprovals, error) {
	state := queueAdapterApprovals{Version: 1, Approvals: []queueAdapterApproval{}}
	data, err := handle.ReadOwnerPrivate(path, maxQueueAdapterApprovalBytes)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return queueAdapterApprovals{}, fmt.Errorf("queue adapter approvals cannot be read safely: %w", err)
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return queueAdapterApprovals{}, fmt.Errorf("queue adapter approval record is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil || decoder.Decode(&struct{}{}) != io.EOF || state.Version != 1 || state.Approvals == nil {
		return queueAdapterApprovals{}, fmt.Errorf("queue adapter approval record is invalid")
	}
	seen := map[string]bool{}
	for _, approval := range state.Approvals {
		if err := validateQueueAdapterApproval(approval); err != nil {
			return queueAdapterApprovals{}, fmt.Errorf("queue adapter approval record is invalid")
		}
		key := strings.Join([]string{approval.SourceID, approval.Path, approval.Adapter, approval.Version}, "\x00")
		if seen[key] {
			return queueAdapterApprovals{}, fmt.Errorf("queue adapter approval record is invalid")
		}
		seen[key] = true
	}
	return state, nil
}

func validateQueueAdapterApproval(approval queueAdapterApproval) error {
	if !validExternalAdapterID(approval.SourceID) || !validExternalAdapterID(approval.Adapter) || !validExternalAdapterVersion(approval.Version) || !filepath.IsAbs(approval.Path) || filepath.Clean(approval.Path) != approval.Path || len(approval.Path) > 4096 {
		return fmt.Errorf("invalid approval binding")
	}
	if approval.Claims != nil {
		if approval.Claims.Policy != "generic" {
			return fmt.Errorf("invalid approval claims policy")
		}
		if err := validateQueueClaimSource(approval.Claims.Source); err != nil {
			return fmt.Errorf("invalid approval claims source")
		}
	}
	if approval.Account != "" && !validExternalQueueText(approval.Account) {
		return fmt.Errorf("invalid approval account")
	}
	for intent, transition := range approval.Workflow {
		if !map[string]bool{"start": true, "blocked": true, "review": true, "complete": true, "reopen": true}[intent] || !validExternalQueueText(transition) {
			return fmt.Errorf("invalid approval workflow")
		}
	}
	for _, digest := range []string{approval.SHA256, approval.ConfigSHA256} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest {
			return fmt.Errorf("invalid approval digest")
		}
	}
	return nil
}

func sortQueueAdapterApprovals(approvals []queueAdapterApproval) {
	sort.Slice(approvals, func(i, j int) bool {
		a, b := approvals[i], approvals[j]
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Adapter != b.Adapter {
			return a.Adapter < b.Adapter
		}
		return a.Version < b.Version
	})
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	opening, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch opening {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid JSON key")
			}
			seen[key] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
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
