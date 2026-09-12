package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
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

func TestAcquireDerivesInputBeforeLaterPlaceholder(t *testing.T) {
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "acquire", "--json", "--provider", "generic", "--source", "s", "--item", "i"}, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("placeholder should return an error")
	}
	if !strings.Contains(stdout.String(), `"reason":"internal"`) {
		t.Fatalf("output=%q", stdout.String())
	}
}
