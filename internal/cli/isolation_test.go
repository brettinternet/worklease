package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

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
