package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/store"
	watchpkg "github.com/brettinternet/worklease/internal/watch"
)

func TestWatchSubprocessEventWakesFilteredWaiter(t *testing.T) {
	if os.Getenv("WORKLEASE_WATCH_HELPER") == "1" {
		home, cursor := os.Getenv("WORKLEASE_WATCH_HOME"), os.Getenv("WORKLEASE_WATCH_CURSOR")
		if err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--cursor", cursor, "--resource", "wanted", "--timeout", "2s"}, "dev", "unknown", "unknown", os.Stdout, os.Stderr); err != nil {
			t.Fatal(err)
		}
		return
	}
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"wanted"}), 0)
	cmd := exec.Command(os.Args[0], "-test.run=TestWatchSubprocessEventWakesFilteredWaiter", "--")
	cmd.Env = append(os.Environ(), "WORKLEASE_WATCH_HELPER=1", "WORKLEASE_WATCH_HOME="+home, "WORKLEASE_WATCH_CURSOR="+cursor)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "released", Resources: []string{"wanted"}, ClaimID: strings.Repeat("1", 32)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("watch subprocess: %v output=%s", err, output.String())
		}
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("watch subprocess did not wake")
	}
	if !strings.Contains(output.String(), `"kind":"released"`) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestWatchMalformedCursorDoesNotOpenStorage(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing")
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--cursor", "not-a-cursor"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(), `"reason":"cursor-invalid"`) {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	if _, statErr := os.Stat(home); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("watch opened storage: %v", statErr)
	}
}

func TestWatchTextIncludesUnresolvedPredecessorMetadata(t *testing.T) {
	claimID, operationID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	var out bytes.Buffer
	if err := writeWatchText(&out, watchpkg.Result{
		NextCursor:            "cursor",
		UnresolvedPredecessor: []watchpkg.Predecessor{{ClaimID: claimID, OperationID: operationID, Resources: []string{"first", "second"}}},
		UnresolvedOperations:  []string{operationID},
	}, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"unresolvedPredecessor:", "claimId=" + claimID, "operationId=" + operationID, "resources=first,second", "unresolvedOperations: " + operationID} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q: %q", want, text)
		}
	}
}

func TestWatchTextIncludesExpiryGuidance(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := writeWatchTextAt(&out, watchpkg.Result{Resources: []watchpkg.ResourceState{{
		Resource: "task", State: "active", ExpiresAt: now.Add(14*time.Minute + 30*time.Second),
	}}}, false, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, phrase := range []string{"task=active (expires in 14m)", "nearest expiry", "verify ownership"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("watch text missing %q: %q", phrase, text)
		}
	}
	if strings.Contains(text, "expiresAt") || strings.Contains(text, "2026-") || strings.Contains(text, "cursor") {
		t.Fatalf("watch text is not compact: %q", text)
	}
}

func TestWatchTextPresentsCursorOnlyAsResumeCommand(t *testing.T) {
	var out bytes.Buffer
	if err := writeWatchText(&out, watchpkg.Result{TimedOut: true, NextCursor: "opaque\x1bcursor"}, false); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "watch timed out\nresume: worklease watch --cursor opaque\\u001bcursor\n" {
		t.Fatalf("text=%q", got)
	}
	out.Reset()
	if err := writeWatchText(&out, watchpkg.Result{Free: true}, false); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); strings.Contains(got, "cursor") {
		t.Fatalf("empty cursor leaked: %q", got)
	}
	out.Reset()
	if err := writeWatchText(&out, watchpkg.Result{Gap: true, NextCursor: "reset"}, false); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "lifecycle state observed\ngap: earlier events were collected; treat the current resource state as a fresh snapshot before resuming\nresume: worklease watch --cursor reset\n" {
		t.Fatalf("gap text=%q", got)
	}
}

func TestWatchEmptyAuthorityUntilChangeDoesNotEmitCursor(t *testing.T) {
	home := filepath.Join(t.TempDir(), "empty")
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--resource", "r", "--until", "change", "--timeout", "60ms"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	if envelope["nextCursor"] != "" || envelope["timedOut"] != true {
		t.Fatalf("envelope=%v", envelope)
	}
}

func TestWatchInvalidResourceDoesNotOpenStorage(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing")
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--resource", "bad\x01resource", "--until", "free"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(), `"reason":"invalid-resource"`) {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	if _, statErr := os.Stat(home); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("watch opened storage: %v", statErr)
	}
}

func TestWatchRejectsResourcesWithoutUntilOrCursor(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing")
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--resource", "r"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(), `"reason":"invalid-argument"`) {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	if _, statErr := os.Stat(home); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("invalid watch opened storage: %v", statErr)
	}
}

func TestWatchJSONRedactsEventDetails(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	token := strings.Repeat("a", 64)
	revision := int64(9007199254740993)
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.AppendEvent(store.Event{At: time.Now(), Kind: "released", Resources: []string{"r"}, ClaimID: strings.Repeat("1", 32), Revision: &revision, Detail: map[string]any{"reason": token}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cursor := ledger.EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", ledger.ResourcesFilter([]string{"r"}), 0)
	var out bytes.Buffer
	if err := Run(ctx, []string{"worklease", "watch", "--json", "--home", home, "--cursor", cursor, "--resource", "r", "--timeout", "1s"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), token) || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatalf("event detail was not redacted: %s", out.String())
	}
	if !strings.Contains(out.String(), `"revision":9007199254740993`) {
		t.Fatalf("event revision lost precision: %s", out.String())
	}
}

func TestWatchTextTimeoutIncludesDurableCursor(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "watch", "--home", home, "--resource", "r", "--until", "change", "--timeout", "60ms"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "watch timed out\n") || !strings.Contains(out.String(), "resume: worklease watch --cursor eyJ") || strings.Contains(out.String(), "timedOut:") || strings.Contains(out.String(), "gap:") || strings.Contains(out.String(), "nextCursor:") {
		t.Fatalf("text=%q", out.String())
	}
}

func TestWatchEmptyAuthorityUntilFreeJSONAndTimeoutBounds(t *testing.T) {
	home := filepath.Join(t.TempDir(), "empty")
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--resource", "r", "--until", "free"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	if envelope["ok"] != true || envelope["free"] != true {
		t.Fatalf("envelope=%v", envelope)
	}
	var bounded bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "watch", "--json", "--home", home, "--resource", "r", "--until", "change", "--timeout", (time.Hour + time.Second).String()}, "dev", "unknown", "unknown", &bounded, &bytes.Buffer{})
	if err == nil || !strings.Contains(bounded.String(), `"reason":"invalid-argument"`) {
		t.Fatalf("out=%s err=%v", bounded.String(), err)
	}
}
