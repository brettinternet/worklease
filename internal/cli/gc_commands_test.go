package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gcresult "github.com/brettinternet/worklease/internal/gc"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/store"
)

func TestGCTextColorsModeAndOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		result gcresult.Result
		want   []string
	}{
		{name: "preview", result: gcresult.Result{DryRun: true}, want: []string{"mode: \x1b[33mpreview\x1b[0m"}},
		{name: "apply", result: gcresult.Result{Collected: map[string]gcresult.Summary{"epochs": {Count: 1}}}, want: []string{"mode: \x1b[32mapply\x1b[0m", "collected: \x1b[32mepochs=1\x1b[0m"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeGCText(&out, test.result, true); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("colored gc output missing %q: %q", want, out.String())
				}
			}
		})
	}
}

func TestPublicFullHistoryAndEventsRedactCheckpointAndCredentials(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	token := strings.Repeat("a", 64)
	claim, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: token, Resources: []string{"opaque-" + token}, AgentID: "agent-" + token, SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Checkpoint(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim.ClaimID, Token: token, Revision: claim.Revision}, lease.CheckpointRequest{OperationID: strings.Repeat("2", 32), Data: []byte(`{"private":"checkpoint-value"}`), RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"worklease", "--json", "--home", home, "status", "--resource", "opaque-" + token, "--full"},
		{"worklease", "--json", "--home", home, "list", "--resource", "opaque-" + token, "--full"},
		{"worklease", "--home", home, "status", "--resource", "opaque-" + token, "--full"},
		{"worklease", "--home", home, "list", "--resource", "opaque-" + token, "--full"},
	} {
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
	for _, args := range [][]string{
		{"worklease", "--json", "--home", home, "history", "--resource", "opaque-" + token, "--full"},
		{"worklease", "--json", "--home", home, "events", "--full"},
		{"worklease", "--home", home, "history", "--resource", "opaque-" + token, "--full"},
		{"worklease", "--home", home, "events", "--full"},
	} {
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
	if !strings.HasPrefix(out.String(), "garbage collection preview\n") || strings.HasPrefix(out.String(), "gc\n") || !strings.Contains(out.String(), "mode: preview") {
		t.Fatalf("text=%q", out.String())
	}
	cutoff := ""
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "cutoff: ") {
			cutoff = strings.TrimPrefix(line, "cutoff: ")
		}
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000000Z07:00", cutoff); err != nil || strings.Contains(out.String(), " UTC") {
		t.Fatalf("cutoff is not RFC3339: %q", out.String())
	}
	if !strings.Contains(out.String(), "hint: worklease gc --apply --cutoff "+cutoff+"\n") {
		t.Fatalf("hint is not the exact follow-up command: %q", out.String())
	}
}

func TestListTextEmptyStatesAcrossAuthorityShapes(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing")
	empty := t.TempDir()
	st, err := store.Open(ctx, empty, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	populated := t.TempDir()
	st, err = store.Open(ctx, populated, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: strings.Repeat("a", 64), Resources: []string{"held"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing authority", args: []string{"--home", missing, "list"}},
		{name: "missing authority full", args: []string{"--home", missing, "list", "--full"}},
		{name: "empty authority", args: []string{"--home", empty, "list"}},
		{name: "no matching resource", args: []string{"--home", populated, "list", "--resource", "other"}},
		{name: "no matching resource full", args: []string{"--home", populated, "list", "--resource", "other", "--full"}},
	} {
		var out, errOut bytes.Buffer
		if err := Run(ctx, append([]string{"worklease"}, test.args...), "dev", "unknown", "unknown", &out, &errOut); err != nil {
			t.Fatalf("%s: %v (%q)", test.name, err, errOut.String())
		}
		if out.String() != "no current claims\n" {
			t.Fatalf("%s: output=%q", test.name, out.String())
		}
	}
	var out bytes.Buffer
	for _, args := range [][]string{{"worklease", "--home", populated, "list"}, {"worklease", "--home", populated, "list", "--resource", "held"}} {
		out.Reset()
		if err := Run(ctx, args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "STATE") || !strings.Contains(out.String(), "held") {
			t.Fatalf("%v output=%q", args, out.String())
		}
	}
	out.Reset()
	if err := Run(ctx, []string{"worklease", "--home", populated, "list", "--resource", "held", "--resource", "other"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "at most one resource") {
		t.Fatalf("multiple list filters accepted: err=%v output=%q", err, out.String())
	}
	out.Reset()
	if err := Run(ctx, []string{"worklease", "--json", "--home", empty, "list"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true || envelope["operation"] != "list" {
		t.Fatalf("JSON envelope changed: %s", out.String())
	}
}
