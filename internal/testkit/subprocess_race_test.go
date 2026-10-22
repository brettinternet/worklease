//go:build race

package testkit

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A zero exit delay must not hide a race in a re-executed test binary.
func TestRunTestBinaryPropagatesRaceFailure(t *testing.T) {
	if os.Getenv("TESTKIT_RACE_HELPER") == "1" {
		var value int
		var workers sync.WaitGroup
		start := make(chan struct{})
		for range 2 {
			workers.Go(func() {
				<-start
				value++ // Intentional race: both writers are unordered.
			})
		}
		close(start)
		workers.Wait()
		os.Exit(0) // The race runtime must override even an explicit success exit.
	}
	t.Parallel()
	result, err := runTestBinary(ProcessResult{Helper: t.Name(), ExitCode: -1}, 10*time.Second,
		[]string{"-test.run=^" + t.Name() + "$"}, map[string]string{
			"TESTKIT_RACE_HELPER": "1",
			"GORACE":              "atexit_sleep_ms=0 exitcode=77",
		})
	if err == nil || result.ExitCode != 77 || !strings.Contains(string(result.Stderr), "WARNING: DATA RACE") {
		t.Fatalf("race failure not propagated: exit=%d err=%v stdout=%s stderr=%s", result.ExitCode, err, result.Stdout, result.Stderr)
	}
}
