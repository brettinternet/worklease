package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Event is the public, typed lifecycle event boundary. Private credentials and
// payloads are intentionally not representable in its top-level fields.
type Event struct {
	At             time.Time
	Kind           string
	ClaimID        string
	Resources      []string
	OperationID    string
	Revision       *int64
	AgentID        string
	InstallationID string
	RestoreID      string
	Remote         bool
	Detail         map[string]any
}

var eventKinds = map[string]bool{
	"acquired": true, "renewed": true, "checkpointed": true,
	"exec-started": true, "exec-completed": true, "replace-started": true,
	"replace-completed": true, "reconciled": true, "transferred": true,
	"released": true, "expired-replaced": true, "expired-retired": true,
	"restored": true, "revoked": true, "gc-applied": true,
}

// These are intentionally non-secret scalar fields suitable for public feeds.
var eventDetailKeys = map[string]bool{
	"reason": true, "exitStatus": true, "timedOut": true,
	"stdoutBytes": true, "stderrBytes": true, "stdoutTruncated": true,
	"stderrTruncated": true, "mutationProtection": true,
	"providerMutationFenced": true, "count": true, "outcome": true,
	"checkpointPresent": true, "requiredResources": true,
}

func (t *Tx) AppendEvent(ev Event) (int64, error) {
	if t == nil || t.tx == nil || !t.write {
		return 0, errors.New("events require a write transaction")
	}
	if !eventKinds[ev.Kind] {
		return 0, fmt.Errorf("unsupported event kind %q", ev.Kind)
	}
	if ev.At.IsZero() {
		return 0, errors.New("event time is required")
	}
	if ev.ClaimID == "" && ev.OperationID != "" {
		return 0, errors.New("operation event requires claim ID")
	}
	for _, resource := range ev.Resources {
		if !utf8.ValidString(resource) || resource == "" || strings.ContainsAny(resource, "\x00\r\n") {
			return 0, errors.New("event resource is invalid")
		}
	}
	if err := validateEventDetail(ev.Detail); err != nil {
		return 0, err
	}
	resources, err := json.Marshal(nonNilStrings(ev.Resources))
	if err != nil {
		return 0, err
	}
	detail, err := json.Marshal(nonNilMap(ev.Detail))
	if err != nil {
		return 0, err
	}
	var operation, claim, agent any
	if ev.OperationID != "" {
		operation = ev.OperationID
	}
	if ev.ClaimID != "" {
		claim = ev.ClaimID
	}
	if ev.AgentID != "" {
		agent = ev.AgentID
	}
	var revision any
	if ev.Revision != nil {
		revision = *ev.Revision
	}
	var installation, restore any
	if ev.InstallationID != "" {
		installation = ev.InstallationID
	}
	if ev.RestoreID != "" {
		restore = ev.RestoreID
	}
	remote := 0
	if ev.Remote {
		remote = 1
	}
	result, err := t.tx.Exec(`INSERT INTO events(at,kind,claim_id,resources,operation_id,revision,agent_id,detail,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, ev.At.UnixMicro(), ev.Kind, claim, string(resources), operation, revision, agent, string(detail), installation, restore, remote)
	if err != nil {
		return 0, err
	}
	seq, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	var current string
	if err := t.tx.QueryRow(`SELECT value FROM meta WHERE key='last_event_seq'`).Scan(&current); err != nil {
		return 0, err
	}
	currentSeq, err := parseDecimal(current)
	if err != nil {
		return 0, fmt.Errorf("invalid last_event_seq watermark: %w", err)
	}
	if seq > currentSeq {
		if _, err := t.tx.Exec(`UPDATE meta SET value=? WHERE key='last_event_seq'`, fmt.Sprintf("%d", seq)); err != nil {
			return 0, err
		}
	}
	return seq, nil
}

func validateEventDetail(detail map[string]any) error {
	for key, value := range detail {
		if !eventDetailKeys[key] {
			return fmt.Errorf("event detail field %q is not public", key)
		}
		if err := validatePublicValue(value); err != nil {
			return fmt.Errorf("event detail field %q: %w", key, err)
		}
	}
	return nil
}
func validatePublicValue(value any) error {
	switch typed := value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return nil
	case []string:
		return nil
	case []any:
		for _, item := range typed {
			if err := validatePublicValue(item); err != nil {
				return errors.New("nested private payload is not allowed")
			}
		}
		return nil
	default:
		return errors.New("nested private payload is not allowed")
	}
}
func nonNilStrings(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}
func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
func parseDecimal(value string) (int64, error) {
	var result int64
	if value == "" {
		return 0, errors.New("empty decimal")
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errors.New("not a decimal")
		}
		result = result*10 + int64(r-'0')
	}
	return result, nil
}
