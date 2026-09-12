package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
)

func toolError(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	if result["isError"] != true {
		t.Fatalf("expected tool failure, got %s", jsonText(result))
	}
	content := result["structuredContent"].(map[string]any)
	failure, _ := content["error"].(map[string]any)
	if failure == nil {
		t.Fatalf("tool failure without error object: %s", jsonText(result))
	}
	return failure
}

// A definitive no-commit failure must restore the ready credential. Before
// the fix an expired lease stayed pending forever and every later release or
// checkpoint failed operation-request-mismatch.
func TestDefinitiveMutationFailureClearsPendingRequest(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "expiry"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	acquired, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"expiring-resource"}, "ttl": float64(1), "maxHold": float64(60), "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	content := acquired["structuredContent"].(map[string]any)
	ref, path := content["lease"].(string), content["handlePath"].(string)
	time.Sleep(1200 * time.Millisecond)

	heartbeat, err := s.Call(context.Background(), "heartbeat", map[string]any{"lease": ref})
	if err != nil {
		t.Fatal(err)
	}
	failure := toolError(t, heartbeat)
	details := failure["details"].(map[string]any)
	if failure["reason"] != reason.ReasonClaimExpired || details["commitState"] != "not-committed" {
		t.Fatalf("expired heartbeat=%s", jsonText(heartbeat))
	}
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != "ready" || h.PendingRequest != nil {
		t.Fatalf("definitive failure left handle state=%s pending=%v", h.State, h.PendingRequest)
	}
	// The lease is still addressable: release reports the real ownership
	// failure rather than a mismatch against a stale pending heartbeat.
	released, err := s.Call(context.Background(), "release", map[string]any{"lease": ref})
	if err != nil {
		t.Fatal(err)
	}
	if got := toolError(t, released)["reason"]; got != reason.ReasonClaimExpired {
		t.Fatalf("release after cleared pending reason=%v", got)
	}
}

// A contended acquire provably did not commit: it must remove its pending
// grant instead of leaving an orphan handle, and report not-committed.
func TestContendedAcquireRemovesPendingGrant(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "contender"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if first, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"contended-resource"}, "ttl": float64(60), "maxHold": float64(120), "autoHeartbeat": false}); err != nil || first["isError"] == true {
		t.Fatalf("first acquire: %v %s", err, jsonText(first))
	}
	second, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"contended-resource"}, "ttl": float64(60), "maxHold": float64(120), "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	failure := toolError(t, second)
	details := failure["details"].(map[string]any)
	if failure["reason"] != reason.ReasonAlreadyClaimed || details["commitState"] != "not-committed" {
		t.Fatalf("contended acquire=%s", jsonText(second))
	}
	if _, present := details["lease"]; present {
		t.Fatalf("failed grant must not hand out a lease reference: %s", jsonText(second))
	}
	entries, err := os.ReadDir(filepath.Join(home, "handles"))
	if err != nil {
		t.Fatal(err)
	}
	var handles []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			handles = append(handles, entry.Name())
		}
	}
	if len(handles) != 1 {
		t.Fatalf("expected only the winning handle, found %v", handles)
	}
}

// The automatic renewer shares the definitive/uncertain classification. A
// definitive failure restores the ready handle and the runtime reports the
// renewer as stopped rather than active.
func TestAutomaticRenewalDefinitiveFailureClearsPendingAndStops(t *testing.T) {
	home, _ := testkit.Home(t)
	s, err := NewServer(Options{Home: home, AgentID: "auto-renew"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	acquired, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{"auto-renew-resource"}, "ttl": float64(1), "maxHold": float64(60)})
	if err != nil {
		t.Fatal(err)
	}
	content := acquired["structuredContent"].(map[string]any)
	ref, path := content["lease"].(string), content["handlePath"].(string)
	// Corrupt the handle revision under the handle lock so the next automatic
	// heartbeat fails stale-revision, a definitive no-commit failure.
	lk, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	h.Revision = 99
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	lk.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		s.mu.Lock()
		r := s.leases[ref]
		stopped := r == nil || r.status == "stopped"
		s.mu.Unlock()
		if stopped {
			break
		}
	}
	s.mu.Lock()
	r := s.leases[ref]
	status := "missing"
	if r != nil {
		status = r.status
	}
	s.mu.Unlock()
	if status != "stopped" {
		t.Fatalf("renewer status after definitive failure = %s", status)
	}
	after, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "ready" || after.PendingRequest != nil {
		t.Fatalf("automatic renewal left handle state=%s pending=%v", after.State, after.PendingRequest)
	}
}
