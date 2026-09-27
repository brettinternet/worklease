// Package runs keeps owner-private records of supervised worker processes so
// background work stays observable after the launching terminal is gone.
//
// A record is local convenience state, not a claim or provider progress. The
// supervisor that owns a run is the only writer of its record except for the
// acknowledgement flag.
package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
)

const (
	SchemaVersion = 1
	maxRecord     = 256 * 1024
	// MaxResultBytes bounds the optional worker-written result file.
	MaxResultBytes = 8 * 1024
	// MaxLogBytes bounds one run's combined stdout and stderr log.
	MaxLogBytes = 64 << 20
	// IdleAfter is how long a live run may go without output or a checkpoint
	// before it is reported idle. Idle is displayed, never enforced.
	IdleAfter = 10 * time.Minute
)

// Stored states. "abandoned" and "idle" are derived by Health, never stored.
const (
	StateStarting = "starting"
	StateRunning  = "running"
	StateExited   = "exited"
	StateLost     = "lost"
	StateFailed   = "failed"
)

// Worker-reported and supervisor-derived outcomes.
const (
	OutcomeDone    = "done"
	OutcomeBlocked = "blocked"
	OutcomeReview  = "review"
	OutcomeFailed  = "failed"
	OutcomeStopped = "stopped"
	OutcomeTimeout = "timeout"
)

// Claim end states.
const (
	ClaimHeld     = "held"
	ClaimReleased = "released"
	ClaimKept     = "kept"
	ClaimLost     = "lost"
)

var idPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// Record is one supervised run.
type Record struct {
	SchemaVersion int        `json:"schemaVersion"`
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Ref           string     `json:"ref,omitempty"`
	Argv          []string   `json:"argv"`
	Dir           string     `json:"dir"`
	SupervisorPID int        `json:"supervisorPid"`
	ChildPID      int        `json:"childPid,omitempty"`
	State         string     `json:"state"`
	StartedAt     time.Time  `json:"startedAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastOutputAt  *time.Time `json:"lastOutputAt,omitempty"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
	// IntervalSeconds is the supervisor's record update cadence.
	IntervalSeconds int     `json:"intervalSeconds"`
	Claim           *Claim  `json:"claim,omitempty"`
	ExitCode        *int    `json:"exitCode,omitempty"`
	Signal          string  `json:"signal,omitempty"`
	Outcome         string  `json:"outcome,omitempty"`
	Result          *Result `json:"result,omitempty"`
	Error           string  `json:"error,omitempty"`
	// Reason is the stable Worklease reason for a failed run.
	Reason       string `json:"reason,omitempty"`
	LogTruncated bool   `json:"logTruncated,omitempty"`
	Acknowledged bool   `json:"acknowledged,omitempty"`
}

// Claim is the public identity of the claim the supervisor holds for a run.
type Claim struct {
	AuthorityID string    `json:"authorityId"`
	ClaimID     string    `json:"claimId"`
	SessionID   string    `json:"sessionId"`
	Resources   []string  `json:"resources"`
	ExpiresAt   time.Time `json:"expiresAt"`
	State       string    `json:"state"`
	// Detail explains a kept, lost, or unreleased claim.
	Detail string `json:"detail,omitempty"`
}

// Result is the optional structured result a worker writes to
// WORKLEASE_RUN_RESULT before exiting.
type Result struct {
	Outcome string `json:"outcome"`
	Summary string `json:"summary,omitempty"`
}

// Dir returns the owner-private run directory under the XDG state home.
func Dir(env func(string) string) string {
	if env == nil {
		env = os.Getenv
	}
	root := strings.TrimSpace(env("XDG_STATE_HOME"))
	if root == "" {
		root = filepath.Join(env("HOME"), ".local", "state")
	}
	return filepath.Join(root, "worklease", "runs")
}

// NewID returns a sortable run ID.
func NewID(now time.Time, random string) string {
	return now.UTC().Format("20060102T150405Z") + "-" + random
}

// ValidID reports whether id has the NewID shape, so it is safe in a path.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Store reads and writes run records in one directory.
type Store struct{ Dir string }

func (s Store) path(id, suffix string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid run ID %q", id)
	}
	return filepath.Join(s.Dir, id+suffix), nil
}

// LogPath is the combined stdout and stderr log for a run.
func (s Store) LogPath(id string) (string, error) { return s.path(id, ".log") }

// ResultPath is where the worker may write its structured result.
func (s Store) ResultPath(id string) (string, error) { return s.path(id, ".result.json") }

// RequestStop asks a run's supervisor to stop its worker.
func (s Store) RequestStop(id string) error {
	path, err := s.path(id, ".stop")
	if err != nil {
		return err
	}
	return handle.WriteOwnerPrivate(path, []byte("stop\n"), 16)
}

// StopRequested reports whether runs stop asked this run to stop.
func (s Store) StopRequested(id string) bool {
	path, err := s.path(id, ".stop")
	if err != nil {
		return false
	}
	_, err = os.Lstat(path)
	return err == nil
}

// Create writes a new record and fails if the ID is already used.
func (s Store) Create(r Record) error {
	if err := handle.EnsureOwnerPrivateDir(s.Dir); err != nil {
		return err
	}
	return s.write(r, true)
}

// Save replaces an existing record.
func (s Store) Save(r Record) error { return s.write(r, false) }

func (s Store) write(r Record, create bool) error {
	path, err := s.path(r.ID, ".json")
	if err != nil {
		return err
	}
	r.SchemaVersion = SchemaVersion
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if create {
		return handle.WriteOwnerPrivateNoReplace(path, data, maxRecord)
	}
	return handle.WriteOwnerPrivate(path, data, maxRecord)
}

// Read returns one record.
func (s Store) Read(id string) (Record, error) {
	path, err := s.path(id, ".json")
	if err != nil {
		return Record{}, err
	}
	data, err := handle.ReadOwnerPrivate(path, maxRecord)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil || r.ID != id {
		return Record{}, fmt.Errorf("run record %s is malformed", id)
	}
	return r, nil
}

// List returns every record, newest first. A missing directory is empty.
func (s Store) List() ([]Record, error) {
	names, err := handle.ListOwnerPrivateNames(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, name := range names {
		id, ok := strings.CutSuffix(name, ".json")
		if !ok || !ValidID(id) {
			continue // logs, results, and temporary files
		}
		r, err := s.Read(id)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	sort.Slice(records, func(a, b int) bool {
		if !records[a].StartedAt.Equal(records[b].StartedAt) {
			return records[a].StartedAt.After(records[b].StartedAt)
		}
		return records[a].ID > records[b].ID
	})
	return records, nil
}

// Terminal reports whether the supervisor has finished with the run.
func (r Record) Terminal() bool {
	return r.State == StateExited || r.State == StateLost || r.State == StateFailed
}

// Health is the displayed state: the stored terminal state, or for a live
// record "abandoned" when its supervisor is gone, "idle" when the worker has
// been quiet for IdleAfter, and otherwise the stored state.
func (r Record) Health(now time.Time, alive func(pid int) bool) string {
	if r.Terminal() {
		return r.State
	}
	interval := time.Duration(r.IntervalSeconds) * time.Second
	if !alive(r.SupervisorPID) || interval > 0 && now.Sub(r.UpdatedAt) > 4*interval {
		return "abandoned"
	}
	last := r.StartedAt
	if r.LastOutputAt != nil && r.LastOutputAt.After(last) {
		last = *r.LastOutputAt
	}
	if r.State == StateRunning && now.Sub(last) > IdleAfter {
		return "idle"
	}
	return r.State
}

// Alive reports whether a local process exists. It cannot detect PID reuse;
// Health also checks the record's update cadence.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ReadResult reads and validates the worker's optional result file. A missing
// file is not an error; an invalid one is reported so the run shows it.
func ReadResult(path string) (*Result, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("result file cannot be opened")
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("result file is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxResultBytes+1))
	if err != nil || len(data) > MaxResultBytes {
		return nil, fmt.Errorf("result file exceeds %d bytes", MaxResultBytes)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("result file is not JSON")
	}
	switch result.Outcome {
	case OutcomeDone, OutcomeBlocked, OutcomeReview, OutcomeFailed:
	default:
		return nil, fmt.Errorf("result outcome must be done, blocked, review, or failed")
	}
	if len(result.Summary) > 1024 {
		result.Summary = result.Summary[:1024]
	}
	result.Summary = strings.ToValidUTF8(result.Summary, "")
	return &result, nil
}
