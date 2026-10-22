package testkit

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	restore, err := IsolateProcessEnvironment()
	if err != nil {
		panic(err)
	}
	code := m.Run()
	restore()
	os.Exit(code)
}
