package queue

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CredentialHelper resolves a configured command's token in memory. A resolver
// should be shared by adapters in one client so refreshes of the same command
// cannot overlap. Provider adapters must verify the principal before using the
// token; in particular, call Resolve again immediately before each write.
type CredentialHelper struct {
	mu    sync.Mutex
	locks map[[32]byte]chan struct{}
}

// Resolve returns a token only after the provider has verified its principal.
// verify must use the supplied token to query the provider's authenticated
// identity, not an ambient credential. No helper or provider error is echoed:
// these may contain the token, command arguments or response body.
func (h *CredentialHelper) Resolve(ctx context.Context, argv []string, expected string, verify func(context.Context, string) (string, error)) (string, error) {
	if len(argv) == 0 || argv[0] == "" || expected == "" || verify == nil {
		return "", fmt.Errorf("credential helper: command, account and principal verifier required")
	}
	key := sha256.Sum256([]byte(strings.Join(argv, "\x00")))
	h.mu.Lock()
	if h.locks == nil {
		h.locks = make(map[[32]byte]chan struct{})
	}
	lock := h.locks[key]
	if lock == nil {
		lock = make(chan struct{}, 1)
		h.locks[key] = lock
	}
	h.mu.Unlock()
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return "", fmt.Errorf("credential helper: canceled waiting for credential refresh")
	}
	defer func() { <-lock }()

	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, argv[0], argv[1:]...)
	prepareExternalProcess(command)
	command.Cancel = func() error { terminateExternalProcess(command); return nil }
	defer terminateExternalProcess(command) // also stop descendants when the helper exits successfully
	command.Env = credentialHelperEnvironment(os.Getenv)
	command.WaitDelay = time.Second
	var output, stderr limitedBuffer
	output.limit, stderr.limit = 4096, 4096
	output.onLimit, stderr.onLimit = cancel, cancel
	command.Stdout, command.Stderr = &output, &stderr
	if err := command.Run(); err != nil || output.exceeded || stderr.exceeded {
		return "", fmt.Errorf("credential helper: failed or exceeded 15s/4KiB; check the configured helper and account")
	}
	token := strings.TrimSpace(output.data.String())
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return "", fmt.Errorf("credential helper: expected one non-empty token line")
	}
	principal, err := verify(bounded, token)
	if err != nil {
		return "", fmt.Errorf("credential helper: cannot verify authenticated principal; check provider access and account")
	}
	if principal != expected {
		return "", fmt.Errorf("credential helper: authenticated principal differs from configured account; writes disabled")
	}
	return token, nil
}

// Only process plumbing and the user's private configuration location are
// inherited. Never pass arbitrary ambient provider/session credentials through.
func credentialHelperEnvironment(env func(string) string) []string {
	keys := []string{"PATH", "HOME", "USER", "LOGNAME", "LANG", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := env(key); value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}
