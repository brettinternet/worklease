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
	for key, field := range redactMap(e.Fields) {
		if key != "schemaVersion" && key != "operation" && key != "ok" && key != "error" {
			value[key] = field
		}
	}
	if e.Error != nil {
		value["error"] = Failure{Reason: e.Error.Reason, ExitCode: e.Error.ExitCode, Message: RedactString(e.Error.Message), Details: redactMap(e.Error.Details)}
	}
	return json.Marshal(value)
}

// WriteSuccess emits exactly one JSON document, followed by one newline.
func WriteSuccess(w io.Writer, operation string, fields map[string]any) error {
	return write(w, Envelope{SchemaVersion: SchemaVersion, Operation: operation, OK: true, Fields: fields})
}

// WriteError emits exactly one redacted failure document.
func WriteError(w io.Writer, operation string, err error) error {
	failure := Classify(err)
	return write(w, Envelope{SchemaVersion: SchemaVersion, Operation: operation, OK: false, Error: &failure})
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
	return Failure{Reason: registeredReason(classified.Reason), ExitCode: classified.Code, Message: RedactString(classified.Message), Details: redactMap(details)}
}

func registeredReason(value string) string {
	if reason.Registered(value) {
		return value
	}
	return reason.ReasonInternal
}

// WriteText writes deterministic human-readable key/value output.
func WriteText(w io.Writer, operation string, fields map[string]any) error {
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
		if _, err := fmt.Fprintf(w, "%s: %v\n", key, escapeText(fmt.Sprint(Redact(fields[key])))); err != nil {
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
			if r < 0x20 {
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
	if w == nil {
		return errors.New("nil output writer")
	}
	failure := Classify(err)
	if _, writeErr := fmt.Fprintf(w, "error: %s: %s\n", failure.Reason, RedactString(failure.Message)); writeErr != nil {
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
		value := escapeText(fmt.Sprint(Redact(failure.Details[key])))
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

// Redact recursively removes values under credential-like keys and replaces
// strings that look like bearer tokens. This is defense in depth; callers must
// still avoid putting private material in Details in the first place.
func Redact(value any) any {
	return redact(value, "")
}

func redact(value any, key string) any {
	if isSecretKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for name, child := range typed {
			result[name] = redact(child, name)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, child := range typed {
			result[i] = redact(child, key)
		}
		return result
	case string:
		if isPublicHexKey(key) {
			return typed
		}
		return RedactString(typed)
	default:
		// Typed structs are not traversed. Domain projections (receipts,
		// claim views) are token-free by construction; this pass is defense
		// in depth for loosely typed detail maps only.
		return value
	}
}

// isPublicHexKey names fields whose values legitimately contain 64 lowercase
// hex characters that are not credentials: SHA-256 request and content hashes,
// and handle paths whose contextual identity is a SHA-256 digest. Redacting
// those would destroy the recovery pointers the contract requires.
func isPublicHexKey(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, "sha256") || strings.HasSuffix(lower, "path")
}

func isSecretKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "_", ""))
	key = strings.ReplaceAll(key, "-", "")
	return key == "token" || key == "tokenhash" || key == "bearer" || key == "password" || key == "secret" || key == "credential" || key == "credentials" || key == "argv" || key == "rawrequest" || key == "rawreceipt" || key == "evidence" || key == "checkpoint" || key == "output"
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

func redactMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	redacted, _ := Redact(values).(map[string]any)
	return redacted
}
