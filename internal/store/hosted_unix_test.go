//go:build !windows

package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestHostedLockSurvivesPausedHolder(t *testing.T) {
	if os.Getenv("WORKLEASE_HOSTED_HOLDER") == "1" {
		hostedWriterHelper(t)
		return
	}
	home := filepath.Join(t.TempDir(), "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHostedLockSurvivesPausedHolder$")
	cmd.Env = append(os.Environ(), "WORKLEASE_HOSTED_HOLDER=1", "WORKLEASE_HOSTED_HOME="+home)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("holder ready=%q err=%v", ready, err)
	}
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{HostedWriter: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedLockHeld {
		_ = cmd.Process.Signal(syscall.SIGCONT)
		t.Fatalf("competitor while paused error=%v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("holder did not exit")
	}
	takeover, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := takeover.Close(); err != nil {
		t.Fatal(err)
	}
}

func hostedWriterHelper(t *testing.T) {
	home := os.Getenv("WORKLEASE_HOSTED_HOME")
	st, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}
