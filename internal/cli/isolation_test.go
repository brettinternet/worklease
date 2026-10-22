package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

// isolateCLIProcess lets slow environment-mutating tests run alongside other
// tests without ever changing the parent test process's environment.
func isolateCLIProcess(t *testing.T) bool {
	t.Helper()
	return testkit.RunIsolatedTest(t)
}

func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "worklease" {
		if err := Run(context.Background(), os.Args, "dev", "unknown", "unknown", os.Stdout, os.Stderr); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	restore, err := testkit.IsolateProcessEnvironment()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	restore()
	os.Exit(code)
}
