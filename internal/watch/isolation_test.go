package watch

import (
	"os"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestMain(m *testing.M) {
	restore, err := testkit.IsolateProcessEnvironment()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	restore()
	os.Exit(code)
}
