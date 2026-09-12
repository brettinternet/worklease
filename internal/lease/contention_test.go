package lease

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/store"
)

func TestCrossProcessContentionHasExactlyOneWinner(t *testing.T) {
	home := t.TempDir()
	bootstrap, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	authority := bootstrap.AuthorityID()
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		id := strings.Repeat(fmt.Sprintf("%d", i+1), 32)
		token := strings.Repeat(string(rune('a'+i)), 64)
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestLeaseAcquireProcessHelper$", "--", home, authority, id, token, deadline)
	}
	for _, command := range commands {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	wins := 0
	for _, command := range commands {
		if err := command.Wait(); err == nil {
			wins++
		} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
			t.Fatalf("unexpected helper result: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
}

func TestLeaseAcquireProcessHelper(t *testing.T) {
	if len(os.Args) < 7 || os.Args[len(os.Args)-6] != "--" {
		return
	}
	args := os.Args[len(os.Args)-5:]
	deadline, err := time.Parse(time.RFC3339Nano, args[4])
	if err != nil {
		os.Exit(64)
	}
	st, err := store.Open(context.Background(), args[0], store.Options{})
	if err != nil {
		os.Exit(75)
	}
	defer st.Close()
	svc := New(st, nil, nil, Defaults{})
	_, err = svc.Acquire(context.Background(), AcquireRequest{AuthorityID: args[1], ClaimID: args[2], Token: args[3], Resources: []string{"same"}, AgentID: args[2], SessionID: args[2], TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		os.Exit(2)
	}
}
