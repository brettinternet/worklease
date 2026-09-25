package sampleadapter

import (
	"os"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestMain(m *testing.M) {
	cleanup, err := testkit.IsolateProcessEnvironment()
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
