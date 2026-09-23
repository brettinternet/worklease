package queue

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// sandbox-exec denies network to the test process AND its Backlog/Git children.
// A Go-only dialer hook cannot observe connections from provider subprocesses.
func TestLocalQueueSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec is macOS-only")
	}
	if os.Getenv("QUEUE_NETWORK_SANDBOX_CHILD") != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			t.Skip("sandbox-exec unavailable")
		}
		cmd := exec.Command("sandbox-exec", "-p", "(version 1) (allow default) (deny network*)", binary, "-test.run=^TestLocalQueueSandbox$", "-test.v")
		cmd.Env = append(os.Environ(), "QUEUE_NETWORK_SANDBOX_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("local view failed with subprocess network denied: %v\n%s", err, out)
		}
		return
	}
	// A negative control proves that the sandbox covers child processes.
	probe := exec.Command("/usr/bin/python3", "-c", "import socket; socket.create_connection(('127.0.0.1', 1), timeout=1)")
	if output, err := probe.CombinedOutput(); err == nil || !strings.Contains(string(output), "Operation not permitted") {
		t.Fatalf("subprocess network denial was not active: %v %s", err, output)
	}
	root, binary := fakeBacklog(t)
	adapter := NewBacklogAdapter()
	adapter.Binary = binary
	source, err := adapter.Resolve(context.Background(), map[string]string{"id": "local", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	page, err := adapter.List(context.Background(), source, Query{}, "")
	if err != nil || page.Coverage.State != CoverageComplete || len(page.Items) != 2 {
		t.Fatalf("local source attempted network or lost coverage: %v %+v", err, page.Coverage)
	}
}
