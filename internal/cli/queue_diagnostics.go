package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
)

const queueFailureLimit = 32
const queueFailureFileLimit = 16 << 10

type queueRefreshFailure struct {
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
	Operation  string    `json:"operation"`
	Code       string    `json:"code"`
	DurationMS int64     `json:"durationMs"`
}

// Only safe identifiers and diagnostic codes enter this owner-private history.
// Provider stderr, raw errors, task data, and credentials are never retained.
func queueDiagnosticToken(value string) string {
	if value == "" || len(value) > 128 {
		return "unclassified"
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return "unclassified"
		}
	}
	return value
}

func recordQueueRefreshFailure(source, code string, duration time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return writeQueueRefreshFailure(ctx, os.Getenv, queueRefreshFailure{
		At: time.Now().UTC(), Source: source, Operation: "refresh", Code: code, DurationMS: duration.Milliseconds(),
	})
}

func writeQueueRefreshFailure(ctx context.Context, env func(string) string, failure queueRefreshFailure) error {
	path := filepath.Join(filepath.Dir(config.QueueRecoveryDir(env)), "queue-refresh-failures.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	lock, err := handle.AcquireLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	var failures []queueRefreshFailure
	data, err := handle.ReadOwnerPrivate(path, queueFailureFileLimit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &failures); err != nil {
			return err
		}
	}
	failure.Source = queueDiagnosticToken(failure.Source)
	failure.Code = queueDiagnosticToken(failure.Code)
	failure.Operation = "refresh"
	if failure.DurationMS < 0 {
		failure.DurationMS = 0
	}
	failures = append(failures, failure)
	if len(failures) > queueFailureLimit {
		failures = failures[len(failures)-queueFailureLimit:]
	}
	data, err = json.Marshal(failures)
	if err != nil {
		return err
	}
	return handle.WriteOwnerPrivate(path, data, queueFailureFileLimit)
}
