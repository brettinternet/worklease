package queue_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/cli"
)

// The in-repo fixture suite reexecutes this helper to exercise the public CLI
// rather than bypassing the command through queue.CheckExternalAdapter.
func TestProcessHelper(t *testing.T) {
	if os.Getenv("WORKLEASE_TEST_HELPER") != "adapter-cli" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+2 >= len(os.Args) {
		t.Fatal("adapter CLI fixture needs an executable and JSON configuration")
	}
	var stdout, stderr bytes.Buffer
	args := []string{"worklease", "queue", "adapter", "check", "--executable", os.Args[separator+1], "--adapter-config", os.Args[separator+2], "--json"}
	if err := cli.Run(context.Background(), args, "test", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatalf("adapter CLI fixture: %v: %s %s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"verdict":"pass"`) || !strings.Contains(stdout.String(), `"operation":"queue-adapter-check"`) {
		t.Fatal("adapter CLI fixture did not pass conformance")
	}
	fmt.Fprint(os.Stdout, stdout.String())
}
