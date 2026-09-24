package cli

import (
	"os/exec"
	"testing"
	"time"
)

func TestDetachedLaunchReapsShortLivedChildren(t *testing.T) {
	for i := 0; i < 10; i++ {
		cmd := exec.Command("sh", "-c", "exit 0")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-detachLaunch(cmd):
		case <-time.After(2 * time.Second):
			t.Fatalf("child %d was not reaped before deadline", i)
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
			t.Fatalf("child %d was not reaped", i)
		}
	}
}
