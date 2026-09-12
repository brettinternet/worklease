package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeyAndPolicyCommandsReportContractMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "key", "--json", "--provider", "generic", "--source", "source", "--item", "item"}, "dev", "unknown", "unknown", &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &key); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resource", "provider", "capability", "scope", "identityScope", "localReplaceAllowed", "providerFencing"} {
		if _, ok := key[field]; !ok {
			t.Errorf("key missing %s", field)
		}
	}
	if key["providerFencing"] != false || key["localReplaceAllowed"] != false {
		t.Fatalf("metadata = %#v", key)
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "key", "--json", "-r", "opaque"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var direct map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &direct); err != nil {
		t.Fatal(err)
	}
	if direct["localReplaceAllowed"] != true || direct["identityScope"] != "portable" {
		t.Fatalf("direct metadata = %#v", direct)
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "key", "--json", "-r", "opaque", "--coordination-only"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var coordination map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &coordination); err != nil {
		t.Fatal(err)
	}
	if coordination["localReplaceAllowed"] != false || coordination["capability"] != "local-coordination" || coordination["providerFencing"] != false {
		t.Fatalf("coordination metadata = %#v", coordination)
	}
	stdout.Reset()
	err = Run(context.Background(), []string{"worklease", "policy", "list", "--json"}, "dev", "unknown", "unknown", &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var policies map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &policies); err != nil {
		t.Fatal(err)
	}
	if len(policies["policies"].([]any)) != 6 {
		t.Fatalf("policies = %#v", policies)
	}
	for _, name := range []string{"backlog-md", "markdown", "github", "linear", "generic", "path"} {
		if !strings.Contains(stdout.String(), name) {
			t.Errorf("missing policy %s", name)
		}
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "list"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resource", "provider", "capability", "scope", "identityScope", "localReplaceAllowed", "providerFencing"} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("text policy list missing %s", field)
		}
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "describe", "path", "--json"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var description map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &description); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resource", "provider", "capability", "scope", "identityScope", "localReplaceAllowed", "providerFencing"} {
		if _, ok := description[field]; !ok {
			t.Errorf("JSON policy description missing %s", field)
		}
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "describe", "path"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resource:", "provider:", "identityScope:", "localReplaceAllowed:", "providerFencing:"} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("text description missing %s: %q", field, stdout.String())
		}
	}
}

func TestResourceInputResolverRejectsDuplicateAndMixedModesBeforeAcquire(t *testing.T) {
	cases := [][]string{
		{"worklease", "acquire", "--json", "-r", "one", "-r", "one"},
		{"worklease", "acquire", "--json", "-r", "one", "--provider", "generic", "--source", "s", "--item", "i"},
		{"worklease", "acquire", "--json", "--provider", "generic", "--source", "s"},
	}
	for _, args := range cases {
		var stdout bytes.Buffer
		err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
		if err == nil {
			t.Fatalf("%v unexpectedly succeeded", args)
		}
		var envelope map[string]any
		if e := json.Unmarshal(stdout.Bytes(), &envelope); e != nil {
			t.Fatalf("%v output: %v", args, e)
		}
		failure := envelope["error"].(map[string]any)
		if failure["reason"] != "resource-input-conflict" && failure["reason"] != "invalid-resource" {
			t.Fatalf("%v reason=%v", args, failure["reason"])
		}
	}
}

func TestLifecycleRejectsMixedCredentialSelection(t *testing.T) {
	home, tokenPath := t.TempDir(), filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	args := []string{"worklease", "heartbeat", "--json", "--home", home, "--handle", "lease.json", "--claim-id", strings.Repeat("1", 32), "--token-file", tokenPath, "--revision", "1", "--operation-id", strings.Repeat("2", 32), "--request-not-after", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
	if err == nil || !strings.Contains(stdout.String(), `"reason":"credential-source-conflict"`) {
		t.Fatalf("mixed selection err=%v output=%q", err, stdout.String())
	}
}

func TestStatusRequiresSelectionAndStatelessTransferSucceeds(t *testing.T) {
	home, credentialDir := t.TempDir(), t.TempDir()
	currentTokenPath, successorTokenPath := filepath.Join(credentialDir, "current"), filepath.Join(credentialDir, "successor")
	if err := os.WriteFile(currentTokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(successorTokenPath, []byte(strings.Repeat("b", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	claimID, successorID := strings.Repeat("1", 32), strings.Repeat("2", 32)
	acquire := []string{"worklease", "acquire", "--json", "--home", home, "--resource", "r", "--no-handle", "--claim-id", claimID, "--token-file", currentTokenPath, "--request-not-after", deadline}
	if err := Run(context.Background(), acquire, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	transfer := []string{"worklease", "transfer", "--json", "--home", home, "--claim-id", claimID, "--token-file", currentTokenPath, "--revision", "1", "--operation-id", strings.Repeat("3", 32), "--request-not-after", deadline, "--successor-claim-id", successorID, "--successor-token-file", successorTokenPath, "--to-agent", "next", "--to-session", "next-session"}
	var transferOut bytes.Buffer
	if err := Run(context.Background(), transfer, "dev", "unknown", "unknown", &transferOut, &bytes.Buffer{}); err != nil || !strings.Contains(transferOut.String(), successorID) {
		t.Fatalf("transfer err=%v output=%q", err, transferOut.String())
	}
	var publicOut bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "status", "--json", "--home", home, "--claim-id", successorID}, "dev", "unknown", "unknown", &publicOut, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(publicOut.String(), `"AgentID"`) || !strings.Contains(publicOut.String(), `"agentId":"next"`) {
		t.Fatalf("status fields are not contract-cased: %q", publicOut.String())
	}
	var statusOut bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "status", "--json", "--home", home}, "dev", "unknown", "unknown", &statusOut, &bytes.Buffer{})
	if err == nil || !strings.Contains(statusOut.String(), `"reason":"claim-selection-missing"`) {
		t.Fatalf("empty status err=%v output=%q", err, statusOut.String())
	}
}

func TestAcquireDerivesInputBeforeDispatch(t *testing.T) {
	var stdout bytes.Buffer
	home, tokenPath := t.TempDir(), filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"worklease", "acquire", "--json", "--home", home, "--provider", "generic", "--source", "s", "--item", "i", "--no-handle", "--claim-id", strings.Repeat("1", 32), "--token-file", tokenPath, "--request-not-after", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire failed: %v output=%q", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"claimId"`) || strings.Contains(stdout.String(), `"token"`) {
		t.Fatalf("unexpected output=%q", stdout.String())
	}
}
