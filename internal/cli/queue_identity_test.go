package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/store"
)

func TestQueueIdentityConfirmationAndRebind(t *testing.T) {
	h := newQueueQueryHarness(t)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	h.setTasks(`[{"id":"TASK-1","title":"Work","status":"Open","ordinal":1,"isReady":true}]`)
	checkout := strings.TrimSpace(strings.Split(strings.Split(h.queueConfig, "checkout: ")[1], "\n")[0])
	if output, err := h.run("acquire", "--provider", "backlog-md", "--source", filepath.Join(checkout, "backlog"), "--item", "TASK-1"); err != nil {
		t.Fatalf("old claim: %s %v", output, err)
	}
	if _, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err == nil || !strings.Contains(err.Error(), "old-key claim held") {
		t.Fatalf("confirmed while old key held: %v", err)
	}
	if output, err := h.run("release", "--reason", "test confirmation"); err != nil {
		t.Fatalf("old release: %s %v", output, err)
	}
	if _, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local"); err == nil {
		t.Fatal("missing operator acknowledgement was accepted")
	}
	if output, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirmation: %s %v", output, err)
	}
	identities, err := config.LoadQueueIdentities(nil)
	if err != nil || identities.Sources["local"].Policy != "generic" || len(identities.Sources["local"].Retired) != 1 {
		t.Fatalf("confirmation receipt: %+v %v", identities, err)
	}
	response, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil || bytes.Contains(response, []byte(`"reason":"binding-migration-required"`)) {
		t.Fatalf("confirmed source still gated: %v %s", err, response)
	}
	newConfig := strings.Replace(h.queueConfig, "brettinternet/worklease/backlog", "brettinternet/worklease/new", 1)
	h.writeQueueConfig(newConfig)
	response, err = h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil || !bytes.Contains(response, []byte(`binding-migration-required`)) || !bytes.Contains(response, []byte(`brettinternet/worklease/backlog`)) || !bytes.Contains(response, []byte(`brettinternet/worklease/new`)) {
		t.Fatalf("changed binding was not gated with locators: %v %s", err, response)
	}
}

func TestQueueIdentityConfirmationRejectsDuplicateIDs(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","status":"Open"},{"id":"TASK-1","status":"Open"}]`)
	if output, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err == nil || !strings.Contains(err.Error(), "duplicate-item-id") {
		t.Fatalf("duplicate confirmation: %v %s", err, output)
	}
	if identities, err := config.LoadQueueIdentities(nil); err != nil || len(identities.Sources) != 0 {
		t.Fatalf("duplicate created receipt: %+v %v", identities, err)
	}
}

func TestQueueIdentityConfirmsAuthorityChange(t *testing.T) {
	h := newQueueQueryHarness(t)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	h.setTasks(`[{"id":"TASK-1","title":"Work","status":"Open","ordinal":1,"isReady":true}]`)
	if output, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirmation: %s %v", output, err)
	}
	identities, err := config.LoadQueueIdentities(nil)
	if err != nil {
		t.Fatal(err)
	}
	current := identities.Sources["local"]
	if current.AuthorityID == "" {
		t.Fatalf("initial receipt lacks authority: %+v", current)
	}
	former := current
	former.AuthorityID = "former-authority"
	identities.Sources["local"] = former
	if err := config.SaveQueueIdentities(nil, identities); err != nil {
		t.Fatal(err)
	}
	if output, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("authority change could not be confirmed: %s %v", output, err)
	}
	if identities, err = config.LoadQueueIdentities(nil); err != nil || identities.Sources["local"].AuthorityID != current.AuthorityID {
		t.Fatalf("receipt not moved to the view authority: %+v %v", identities, err)
	}
}
