//go:build darwin || linux

package queue

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCredentialHelperStopsDescendants(t *testing.T) {
	t.Parallel()
	argv := credentialFixture(t, "sleep 60 </dev/null >/dev/null 2>&1 &\nprintf '%s' \"$!\" > \"$1\"\nprintf 'test-token\\n'\n")
	pidFile := filepath.Join(t.TempDir(), "pid")
	argv = append(argv, pidFile)
	_, err := new(CredentialHelper).Resolve(context.Background(), argv, "alice", func(context.Context, string) (string, error) { return "alice", nil })
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	pulse := time.NewTicker(10 * time.Millisecond)
	defer pulse.Stop()
	for {
		if exec.Command("kill", "-0", strconv.Itoa(pid)).Run() != nil {
			return
		}
		select {
		case <-pulse.C:
		case <-deadline:
			t.Fatalf("credential helper child %d survived successful resolution", pid)
		}
	}
}
