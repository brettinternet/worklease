package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/handle"
)

// QueueSessionID returns the persisted owner-only session used only by the
// interactive queue. It is intentionally independent of WORKLEASE_SESSION_ID,
// which belongs to worker commands.
func QueueSessionID(env func(string) string) (string, error) {
	path := filepath.Join(filepath.Dir(QueuePath(env)), "queue-session-id")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		return "", fmt.Errorf("queue session directory is unsafe: %w", err)
	}
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return "", err
	}
	defer lock.Close()
	data, err := handle.ReadOwnerPrivate(path, 128)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if !validQueueSessionID(id) {
			return "", fmt.Errorf("queue session ID is malformed")
		}
		return id, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("queue session ID cannot be read safely: %w", err)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("queue session ID generation failed: %w", err)
	}
	id := hex.EncodeToString(raw[:])
	if err := handle.WriteOwnerPrivateNoReplace(path, []byte(id+"\n"), 128); err != nil {
		return "", fmt.Errorf("queue session ID cannot be persisted: %w", err)
	}
	return id, nil
}

func validQueueSessionID(id string) bool {
	if len(id) != 32 || id != strings.ToLower(id) {
		return false
	}
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16
}
