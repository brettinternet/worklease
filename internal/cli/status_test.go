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

func TestStatusMixedResourcesPreservesOrderInJSONAndText(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	svc := lease.New(st, nil, nil, lease.Defaults{})
	_, err = svc.Acquire(context.Background(), lease.AcquireRequest{
		AuthorityID:     st.AuthorityID(),
		ClaimID:         strings.Repeat("1", 32),
		Token:           token,
		Resources:       []string{"claimed-one", "claimed-two"},
		AgentID:         "agent",
		SessionID:       "session",
		TTL:             time.Minute,
		RequestNotAfter: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Acquire(context.Background(), lease.AcquireRequest{
		AuthorityID:     st.AuthorityID(),
		ClaimID:         strings.Repeat("2", 32),
		Token:           strings.Repeat("b", 64),
		Resources:       []string{"other-claim"},
		AgentID:         "other-agent",
		SessionID:       "other-session",
		TTL:             time.Minute,
		RequestNotAfter: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"worklease", "--home", home, "status", "--resource", "claimed-two", "--resource", "free", "--resource", "other-claim", "--resource", "claimed-one"}
	var stdout, stderr bytes.Buffer
	jsonArgs := append(append([]string(nil), args...), "--json")
	if err := Run(context.Background(), jsonArgs, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatalf("json status: %v stderr=%q", err, stderr.String())
	}
	var result struct {
		Resources []struct {
			Resource string `json:"resource"`
			State    string `json:"state"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) != 4 || result.Resources[0].Resource != "claimed-two" || result.Resources[1].Resource != "free" || result.Resources[1].State != "free" || result.Resources[2].Resource != "other-claim" || result.Resources[3].Resource != "claimed-one" {
		t.Fatalf("resources=%+v output=%s", result.Resources, stdout.String())
	}
	if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
		t.Fatal("JSON status leaked credential")
	}

	stdout.Reset()
	stderr.Reset()
	if err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatalf("text status: %v stderr=%q", err, stderr.String())
	}
	text := stdout.String()
	marker := strings.Index(text, "resources: ")
	if marker < 0 {
		t.Fatalf("text status omitted resources: %q", text)
	}
	resourceLine := text[marker:]
	first := strings.Index(resourceLine, "claimed-two")
	free := strings.Index(resourceLine, "free")
	other := strings.Index(resourceLine, "other-claim")
	last := strings.LastIndex(resourceLine, "claimed-one")
	if first < 0 || free <= first || other <= free || last <= other || strings.Contains(text, token) {
		t.Fatalf("unordered or unsafe text status: %q", text)
	}
}
