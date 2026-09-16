// Package output writes the versioned CLI result envelopes and redacts
// credential-bearing values before they reach human or machine output.
package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
)

const SchemaVersion = 2

type Envelope struct {
	SchemaVersion int            `json:"schemaVersion"`
	Operation     string         `json:"operation"`
	OK            bool           `json:"ok"`
	Fields        map[string]any `json:"-"`
	Error         *Failure       `json:"error,omitempty"`
	public        bool
}

type Failure struct {
	Reason   string         `json:"reason"`
	ExitCode int            `json:"exitCode"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	value := make(map[string]any, len(e.Fields)+4)
	value["schemaVersion"], value["operation"], value["ok"] = e.SchemaVersion, e.Operation, e.OK
	for key, field := range redactMap(e.Fields, e.public) {
		if key != "schemaVersion" && key != "operation" && key != "ok" && key != "error" {
			value[key] = field
		}
	}
	if e.Error != nil {
		value["error"] = Failure{Reason: e.Error.Reason, ExitCode: e.Error.ExitCode, Message: RedactString(e.Error.Message), Details: redactMap(e.Error.Details, true)}
	}
	return json.Marshal(value)
}

// WriteSuccess emits exactly one JSON document, followed by one newline.
func WriteSuccess(w io.Writer, operation string, fields map[string]any) error {
	return write(w, Envelope{SchemaVersion: SchemaVersion, Operation: operation, OK: true, Fields: fields})
}

// WritePublicSuccess emits a public projection with private operation payloads
// removed in addition to the bearer-material redaction applied to all output.
func WritePublicSuccess(w io.Writer, operation string, fields map[string]any) error {
	return write(w, Envelope{SchemaVersion: SchemaVersion, Operation: operation, OK: true, Fields: fields, public: true})
}

// WriteError emits exactly one redacted failure document.
func WriteError(w io.Writer, operation string, err error) error {
	failure := Classify(err)
	return write(w, Envelope{SchemaVersion: SchemaVersion, Operation: operation, OK: false, Error: &failure, public: true})
}

func write(w io.Writer, envelope Envelope) error {
	if w == nil {
		return errors.New("nil output writer")
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// Classify converts any error into the public, complete error envelope.
func Classify(err error) Failure {
	if err == nil {
		return Failure{Reason: reason.ReasonInternal, ExitCode: reason.ExitInternal, Message: "unknown error"}
	}
	classified := reason.As(err)
	if classified == nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			classified = reason.New(reason.ReasonInterrupted, "operation interrupted")
		} else {
			classified = reason.New(reason.ReasonInternal, "unexpected internal failure")
		}
	}
	details := copyMap(classified.Details)
	if details == nil {
		details = map[string]any{}
	}
	if _, ok := details["commitState"]; !ok {
		details["commitState"] = "not-committed"
	}
	if classified.Reason == reason.ReasonHomeUnsafe {
		if _, ok := details["recoveryHint"]; !ok {
			details["recoveryHint"] = "run worklease doctor"
		}
	}
	return Failure{Reason: registeredReason(classified.Reason), ExitCode: classified.Code, Message: RedactString(classified.Message), Details: redactMap(details, true)}
}

func registeredReason(value string) string {
	if reason.Registered(value) {
		return value
	}
	return reason.ReasonInternal
}

// WriteText writes deterministic human-readable key/value output.
func WriteText(w io.Writer, operation string, fields map[string]any) error {
	return writeText(w, operation, fields, false)
}

// WritePublicText is the text counterpart to WritePublicSuccess.
func WritePublicText(w io.Writer, operation string, fields map[string]any) error {
	return writeText(w, operation, fields, true)
}

func writeText(w io.Writer, operation string, fields map[string]any, public bool) error {
	if w == nil {
		return errors.New("nil output writer")
	}
	if _, err := fmt.Fprintln(w, operation); err != nil {
		return err
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(w, "%s: %v\n", key, escapeText(fmt.Sprint(redact(fields[key], key, public)))); err != nil {
			return err
		}
	}
	return nil
}
func escapeText(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			b.WriteString(`\\n`)
		case '\r':
			b.WriteString(`\\r`)
		case '\t':
			b.WriteString(`\\t`)
		default:
			if r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f {
				fmt.Fprintf(&b, `\\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// WriteTextError writes the human error format to stderr. It never includes
// the rejected credential itself.
func WriteTextError(w io.Writer, err error) error {
	return writeTextError(w, err, ColorEnabled(w))
}

func writeTextError(w io.Writer, err error, color bool) error {
	if w == nil {
		return errors.New("nil output writer")
	}
	failure := Classify(err)
	label := Style(color, Red, "error: "+failure.Reason+":")
	if _, writeErr := fmt.Fprintf(w, "%s %s\n", label, RedactString(failure.Message)); writeErr != nil {
		return writeErr
	}
	keys := make([]string, 0, len(failure.Details))
	for key := range failure.Details {
		if safeTextDetail(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := escapeText(fmt.Sprint(redact(failure.Details[key], key, true)))
		if key == "recoveryHint" {
			value = Style(color, Yellow, value)
		}
		if _, writeErr := fmt.Fprintf(w, "%s: %s\n", key, value); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func safeTextDetail(key string) bool {
	switch key {
	case "claimId", "commitState", "expiresAt", "holder", "operationId", "pendingPath", "resource", "requestNotAfter", "recoveryHint":
		return true
	default:
		return false
	}
}

// Redact applies the policy shared by invoking commands and authenticated
// private inspection: bearer material is always removed, while argv,
// checkpoints, evidence, and command output are allowed.
func Redact(value any) any {
	return redact(value, "", false)
}

// RedactPublic additionally removes operation-private payloads. Public CLI
// views, errors, and MCP projections use this policy.
func RedactPublic(value any) any {
	return redact(value, "", true)
}

func redact(value any, key string, public bool) any {
	if isSecretKey(key) || public && isPrivatePayloadKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		names := make([]string, 0, len(typed))
		for name := range typed {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			publicName := uniqueKey(RedactString(name), result)
			result[publicName] = redact(typed[name], name, public)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, child := range typed {
			result[i] = redact(child, key, public)
		}
		return result
	case string:
		if isPublicHexKey(key) {
			return typed
		}
		return RedactString(typed)
	case nil, bool, json.Number, float32, float64,
		int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return value
	default:
		// JSON normalization makes the policy independent of Go's concrete
		// map, slice, and struct types. UseNumber preserves exact integers.
		encoded, err := json.Marshal(value)
		if err != nil {
			return "[REDACTED]"
		}
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.UseNumber()
		var projected any
		if err := decoder.Decode(&projected); err != nil {
			return "[REDACTED]"
		}
		return redact(projected, key, public)
	}
}

// isPublicHexKey names fields whose values legitimately contain 64 lowercase
// hex characters that are not credentials: SHA-256 request and content hashes,
// and handle paths whose contextual identity is a SHA-256 digest. Redacting
// those would destroy the recovery pointers the contract requires.
func isPublicHexKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, "sha256") || strings.HasSuffix(lower, "path") || lower == "successorhandle"
}

func normalizedKey(key string) string {
	key = strings.ToLower(strings.ReplaceAll(key, "_", ""))
	return strings.ReplaceAll(key, "-", "")
}

func isSecretKey(key string) bool {
	switch normalizedKey(key) {
	case "token", "tokenhash", "bearer", "password", "secret", "credential", "credentials":
		return true
	default:
		return false
	}
}

func isPrivatePayloadKey(key string) bool {
	switch normalizedKey(key) {
	case "argv", "rawrequest", "rawreceipt", "evidence", "checkpoint", "output":
		return true
	default:
		return false
	}
}

func uniqueKey(key string, values map[string]any) string {
	if _, exists := values[key]; !exists {
		return key
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", key, suffix)
		if _, exists := values[candidate]; !exists {
			return candidate
		}
	}
}

func RedactString(value string) string {
	if !utf8.ValidString(value) {
		return "[REDACTED]"
	}
	// Tokens are exactly 64 lowercase hex characters. Redact them even when a
	// parser embeds a rejected value in a larger diagnostic.
	var result strings.Builder
	for start := 0; start < len(value); {
		if tokenAt(value, start) {
			result.WriteString("[REDACTED]")
			start += 64
			continue
		}
		result.WriteByte(value[start])
		start++
	}
	return result.String()
}

func tokenAt(value string, start int) bool {
	if start+64 > len(value) || start > 0 && isLowerHex(value[start-1]) || start+64 < len(value) && isLowerHex(value[start+64]) {
		return false
	}
	for _, char := range value[start : start+64] {
		if !isLowerHex(byte(char)) {
			return false
		}
	}
	return true
}

func isLowerHex(char byte) bool {
	return char >= '0' && char <= '9' || char >= 'a' && char <= 'f'
}

func copyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func redactMap(values map[string]any, public bool) map[string]any {
	if values == nil {
		return nil
	}
	redacted, _ := redact(values, "", public).(map[string]any)
	return redacted
}
