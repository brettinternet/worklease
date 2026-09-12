package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/store"
)

func TestPublicFullHistoryAndEventsRedactCheckpointAndCredentials(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	token := strings.Repeat("a", 64)
	claim, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: token, Resources: []string{"opaque"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Checkpoint(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim.ClaimID, Token: token, Revision: claim.Revision}, lease.CheckpointRequest{OperationID: strings.Repeat("2", 32), Data: []byte(`{"private":"checkpoint-value"}`), RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"worklease", "--json", "--home", home, "status", "--resource", "opaque", "--full"}, {"worklease", "--json", "--home", home, "list", "--resource", "opaque", "--full"}} {
		var out bytes.Buffer
		if err := Run(ctx, args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), token) || strings.Contains(out.String(), "checkpoint-value") || strings.Contains(out.String(), "private") {
			t.Fatalf("private data leaked: %s", out.String())
		}
	}
	if _, err := svc.Release(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim.ClaimID, Token: token, Revision: claim.Revision + 1}, lease.ReleaseRequest{OperationID: strings.Repeat("3", 32), RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	for _, args := range [][]string{{"worklease", "--json", "--home", home, "history", "--resource", "opaque", "--full"}, {"worklease", "--json", "--home", home, "events", "--full"}} {
		var out bytes.Buffer
		if err := Run(ctx, args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), token) || strings.Contains(out.String(), "checkpoint-value") || strings.Contains(out.String(), "private") {
			t.Fatalf("private data leaked: %s", out.String())
		}
	}
}

func TestGCEmptyPreviewJSONAndText(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--json", "--home", home, "gc"}, "dev", "unknown", "unknown", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(out.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value["operation"] != "gc" || value["ok"] != true || value["dryRun"] != true {
		t.Fatalf("output=%s", out.String())
	}
	if value["lastEventSequence"] != "0" || value["prunedThroughSequence"] != "0" {
		t.Fatalf("watermarks=%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"worklease", "--home", home, "gc"}, "dev", "unknown", "unknown", &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gc\n") || !strings.Contains(out.String(), "mode: preview") || !strings.Contains(out.String(), "hint:") {
		t.Fatalf("text=%q", out.String())
	}
}
