package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/sampleadapter"
)

func TestQueueAdapterApprovalProcessHelper(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	runQueueAdapterApprovalProcessHelper(os.Args[separator+1])
}

func TestQueueAdapterProtocolReportsStaticCompatibility(t *testing.T) {
	h := newQueueQueryHarness(t)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"id":"example.adapter","version":"1.2.3","protocol":{"minMajor":1,"maxMajor":2}}`), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := h.run("queue", "adapter", "protocol", "--manifest-file", path, "--json")
	if err != nil {
		t.Fatalf("protocol: %v: %s", err, data)
	}
	var result struct {
		SchemaVersion      int    `json:"schemaVersion"`
		OK                 bool   `json:"ok"`
		Operation          string `json:"operation"`
		HostProtocolMajors []int  `json:"hostProtocolMajors"`
		Manifest           struct {
			ID       string `json:"id"`
			Protocol struct {
				MinMajor int `json:"minMajor"`
				MaxMajor int `json:"maxMajor"`
			} `json:"protocol"`
		} `json:"manifest"`
		ProtocolMajorsOverlap bool `json:"protocolMajorsOverlap"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.SchemaVersion != 2 || !result.OK || result.Operation != "queue-adapter-protocol" || len(result.HostProtocolMajors) != 1 || result.HostProtocolMajors[0] != 1 || result.Manifest.ID != "example.adapter" || result.Manifest.Protocol.MaxMajor != 2 || !result.ProtocolMajorsOverlap {
		t.Fatalf("result: %+v %v %s", result, err, data)
	}
	if host, err := h.run("queue", "adapter", "protocol", "--json"); err != nil || strings.Contains(string(host), `"manifest"`) {
		t.Fatalf("host only: %v %s", err, host)
	}
	if err := os.WriteFile(path, []byte(`{"id":"example.adapter","version":"1.2.3","protocol":{"minMajor":2,"maxMajor":3}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if incompatible, err := h.run("queue", "adapter", "protocol", "--manifest-file", path, "--json"); err != nil || !strings.Contains(string(incompatible), `"protocolMajorsOverlap":false`) {
		t.Fatalf("incompatible: %v %s", err, incompatible)
	}
	if err := os.WriteFile(path, []byte(`{"id":"example.adapter","version":"1.2.3","protocol":{"minMajor":1,"maxMajor":1},"requiredFeatures":["unknown-feature"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if overlap, err := h.run("queue", "adapter", "protocol", "--manifest-file", path, "--json"); err != nil || !strings.Contains(string(overlap), `"protocolMajorsOverlap":true`) || strings.Contains(string(overlap), `"compatible"`) {
		t.Fatalf("feature mismatch is not a negotiation verdict: %v %s", err, overlap)
	}
	if err := os.WriteFile(path, []byte(`{"id":"example.adapter","version":"1.2.3","protocol":{"minMajor":0,"maxMajor":1}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if invalid, err := h.run("queue", "adapter", "protocol", "--manifest-file", path, "--json"); err == nil || !strings.Contains(string(invalid), `"reason":"invalid-argument"`) || !strings.Contains(string(invalid), `"exitCode":64`) {
		t.Fatalf("invalid: %v %s", err, invalid)
	}
	if missing, err := h.run("queue", "adapter", "protocol", "--manifest-file", path+"-missing", "--json"); err == nil || !strings.Contains(string(missing), `"exitCode":64`) {
		t.Fatalf("missing: %v %s", err, missing)
	}
	if usage, err := h.run("queue", "adapter", "protocol", "--secret-ghp_Example123", "--json"); err == nil || !strings.Contains(string(usage), `"operation":"queue-adapter-protocol"`) || !strings.Contains(string(usage), `"reason":"invalid-argument"`) || !strings.Contains(string(usage), `"exitCode":64`) || strings.Contains(string(usage), "secret-ghp_Example123") {
		t.Fatalf("usage: %v %s", err, usage)
	}
}

func TestQueueAdapterCheckSampleWithoutApprovalOrQueueState(t *testing.T) {
	h := newQueueQueryHarness(t)
	before, err := os.ReadFile(config.QueuePath(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sample-check")
	script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestQueueAdapterCheckSampleHelper$' -- sample\n", shellQuote(binary))
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := h.run("queue", "adapter", "check", "--executable", path, "--json")
	if err != nil {
		t.Fatalf("sample conformance: %v: %s", err, data)
	}
	var result struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Operation     string               `json:"operation"`
		Verdict       string               `json:"verdict"`
		Checks        []queue.AdapterCheck `json:"checks"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.SchemaVersion != 2 || result.Operation != "queue-adapter-check" || result.Verdict != "pass" {
		t.Fatalf("check result: %+v %v: %s", result, err, data)
	}
	if len(result.Checks) < 10 {
		t.Fatalf("missing checks: %+v", result.Checks)
	}
	configPath := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	fromFile, err := h.run("queue", "adapter", "check", "--executable", path, "--adapter-config-file", configPath, "--json")
	if err != nil || !strings.Contains(string(fromFile), `"verdict":"pass"`) {
		t.Fatalf("configuration file: %v %s", err, fromFile)
	}
	if _, err := os.Stat(config.QueueAdapterApprovalPath(os.Getenv)); !os.IsNotExist(err) {
		t.Fatalf("check wrote approval: %v", err)
	}
	after, err := os.ReadFile(config.QueuePath(os.Getenv))
	if err != nil || string(after) != string(before) {
		t.Fatalf("check modified queue: %v", err)
	}
	bad, err := h.run("queue", "adapter", "check", "--executable", path, "--adapter-config", `{"wrong":"canary-credential-sentinel"}`, "--json")
	if err == nil || !strings.Contains(string(bad), `"reason":"adapter-conformance-failed"`) || !strings.Contains(string(bad), `"exitCode":65`) || !strings.Contains(string(bad), `"operation":"queue-adapter-check"`) || strings.Contains(string(bad), "canary-credential-sentinel") {
		t.Fatalf("conformance failure: %v %s", err, bad)
	}
	relative, err := filepath.Rel("/", path)
	if err != nil {
		t.Fatal(err)
	}
	relativeResult, err := h.run("queue", "adapter", "check", "--executable", relative, "--json")
	if err == nil || !strings.Contains(string(relativeResult), `"exitCode":64`) {
		t.Fatalf("relative executable: %v %s", err, relativeResult)
	}
	unknown, err := h.run("queue", "adapter", "check", "--not-a-flag", "--json")
	if err == nil || !strings.Contains(string(unknown), `"operation":"queue-adapter-check"`) || !strings.Contains(string(unknown), `"exitCode":64`) {
		t.Fatalf("unknown flag: %v %s", err, unknown)
	}
	usage, err := h.run("queue", "adapter", "check", "--executable", path, "--adapter-config", `[]`, "--json")
	if err == nil || !strings.Contains(string(usage), `"exitCode":64`) {
		t.Fatalf("usage failure: %v %s", err, usage)
	}
}

func TestQueueAdapterCheckDoesNotEchoAdapterSecrets(t *testing.T) {
	h := newQueueQueryHarness(t)
	path := filepath.Join(t.TempDir(), "leaking-adapter")
	secret := "private-canary-credential-765"
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '"+secret+"\\n' >&2\nprintf '{bad}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.run("queue", "adapter", "check", "--executable", path, "--adapter-config", `{"credential":"`+secret+`"}`, "--json")
	if err == nil || strings.Contains(string(result), secret) || !strings.Contains(string(result), `"reason":"adapter-conformance-failed"`) {
		t.Fatalf("adapter output leaked or passed: %v %s", err, result)
	}
}

func TestQueueAdapterCheckSampleHelper(t *testing.T) {
	for index, arg := range os.Args {
		if arg == "--" && index+1 < len(os.Args) {
			var err error
			switch os.Args[index+1] {
			case "sample":
				err = sampleadapter.Run(os.Stdin, os.Stdout)
			case "reference":
				err = sampleadapter.RunReference(os.Stdin, os.Stdout)
			default:
				return
			}
			if err != nil {
				os.Exit(1)
			}
			return
		}
	}
}

func TestQueueAdapterApprovalRequiresExplicitSourceAndConfirmation(t *testing.T) {
	h := newQueueQueryHarness(t)
	marker := filepath.Join(t.TempDir(), "adapter-started")
	executable := writeQueueAdapterApprovalExecutable(t, "single", marker)
	h.writeQueueConfig(queueAdapterApprovalConfig(map[string]string{"single": executable}, []string{"single"}))

	if _, err := h.run("queue", "adapter", "approve", "--json"); err == nil || !strings.Contains(err.Error(), "--source") {
		t.Fatalf("approval without explicit source: %v", err)
	}
	preview, err := h.run("queue", "adapter", "approve", "--source", "single", "--json")
	if err != nil {
		t.Fatalf("approval preview: %v: %s", err, preview)
	}
	var previewEnvelope struct {
		Approval struct {
			SourceID        string `json:"sourceId"`
			Executable      string `json:"executable"`
			ExpectedAdapter string `json:"expectedAdapterId"`
			ExpectedVersion string `json:"expectedVersion"`
			SHA256          string `json:"sha256"`
			Approved        bool   `json:"approved"`
		} `json:"approval"`
	}
	if err := json.Unmarshal(preview, &previewEnvelope); err != nil {
		t.Fatalf("decode approval preview: %v: %s", err, preview)
	}
	wantDigest := queueAdapterTestDigest(t, executable)
	approval := previewEnvelope.Approval
	if approval.SourceID != "single" || approval.Executable != executable || approval.ExpectedAdapter != "example.adapter" || approval.ExpectedVersion != "1.2.3" || approval.SHA256 != wantDigest || approval.Approved {
		t.Fatalf("approval preview did not show all approval inputs: %+v", approval)
	}
	if _, err := os.Stat(config.QueueAdapterApprovalPath(os.Getenv)); !os.IsNotExist(err) {
		t.Fatalf("preview recorded approval: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("preview executed adapter: %v", err)
	}

	approved, err := h.run("queue", "adapter", "approve", "--source", "single", "--acknowledge", "--json")
	if err != nil {
		t.Fatalf("confirm approval: %v: %s", err, approved)
	}
	var approvedEnvelope struct {
		Approval struct {
			Approved bool   `json:"approved"`
			SHA256   string `json:"sha256"`
		} `json:"approval"`
	}
	if err := json.Unmarshal(approved, &approvedEnvelope); err != nil {
		t.Fatalf("decode approved result: %v: %s", err, approved)
	}
	if !approvedEnvelope.Approval.Approved || approvedEnvelope.Approval.SHA256 != wantDigest {
		t.Fatalf("approval result did not confirm recorded digest: %+v", approvedEnvelope.Approval)
	}
	source := config.QueueSource{ID: "single", Adapter: "external", Executable: executable, ExpectedAdapterID: "example.adapter", ExpectedVersion: "1.2.3"}
	if err := config.CheckQueueAdapterApproval(os.Getenv, source); err != nil {
		t.Fatalf("CLI approval was not accepted by runtime verifier: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("approval executed adapter: %v", err)
	}
}

func TestQueueQueryIsolatesUnapprovedExternalSourceFromApprovedSource(t *testing.T) {
	h := newQueueQueryHarness(t)
	deniedMarker := filepath.Join(t.TempDir(), "denied-started")
	approvedMarker := filepath.Join(t.TempDir(), "approved-started")
	deniedExecutable := writeQueueAdapterApprovalExecutable(t, "denied", deniedMarker)
	approvedExecutable := writeQueueAdapterApprovalExecutable(t, "approved", approvedMarker)
	h.writeQueueConfig(queueAdapterApprovalConfig(map[string]string{"denied": deniedExecutable, "approved": approvedExecutable}, []string{"denied", "approved"}))

	if _, err := h.run("queue", "adapter", "approve", "--source", "approved", "--acknowledge"); err != nil {
		t.Fatalf("approve one source: %v", err)
	}
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatalf("query with one unapproved source: %v: %s", err, data)
	}
	var result struct {
		Query struct {
			Incomplete bool `json:"incomplete"`
			Sources    []struct {
				ID          string         `json:"id"`
				Coverage    queue.Coverage `json:"coverage"`
				Diagnostics []string       `json:"diagnostics"`
			} `json:"sources"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode query result: %v: %s", err, data)
	}
	if !result.Query.Incomplete || len(result.Query.Sources) != 2 {
		t.Fatalf("unapproved source did not yield incomplete per-source result: %+v", result.Query)
	}
	sourceRows := make(map[string]struct {
		coverage    queue.Coverage
		diagnostics []string
	}, len(result.Query.Sources))
	for _, source := range result.Query.Sources {
		sourceRows[source.ID] = struct {
			coverage    queue.Coverage
			diagnostics []string
		}{source.Coverage, source.Diagnostics}
	}
	denied := sourceRows["denied"]
	if denied.coverage.State != queue.CoverageUnknown || len(denied.diagnostics) == 0 || !strings.Contains(denied.diagnostics[0], "not approved") {
		t.Fatalf("unapproved source diagnostic/coverage: %+v", denied)
	}
	approved := sourceRows["approved"]
	if approved.coverage.State != queue.CoverageComplete {
		t.Fatalf("approved source did not resolve and refresh: %+v", approved)
	}
	if _, err := os.Stat(deniedMarker); !os.IsNotExist(err) {
		t.Fatalf("unapproved source executable ran: %v", err)
	}
	if _, err := os.Stat(approvedMarker); err != nil {
		t.Fatalf("approved source executable did not run: %v", err)
	}

	next, err := h.run("queue", "next", "--view", "Ready", "--json")
	if err != nil {
		t.Fatalf("next with one unapproved source: %v: %s", err, next)
	}
	var nextResult struct {
		Next struct {
			Result  string `json:"result"`
			Sources []struct {
				ID       string         `json:"id"`
				Coverage queue.Coverage `json:"coverage"`
			} `json:"sources"`
		} `json:"next"`
	}
	if err := json.Unmarshal(next, &nextResult); err != nil {
		t.Fatalf("decode next result: %v: %s", err, next)
	}
	if nextResult.Next.Result != "incomplete" || len(nextResult.Next.Sources) != 2 {
		t.Fatalf("next did not preserve isolated source incompleteness: %+v", nextResult.Next)
	}
	for _, source := range nextResult.Next.Sources {
		if source.ID == "approved" && source.Coverage.State != queue.CoverageComplete {
			t.Errorf("next lost approved source coverage: %+v", source)
		}
	}
}

func TestQueueExternalResolveSurfacesRedactedStderrPerSource(t *testing.T) {
	t.Setenv("GH_TOKEN", "canary-token")
	h := newQueueQueryHarness(t)
	executable := writeQueueAdapterApprovalExecutable(t, "broken", filepath.Join(t.TempDir(), "started"))
	configuration := queueAdapterApprovalConfig(map[string]string{"broken": executable}, []string{"broken"})
	configuration = strings.Replace(configuration, "config: {}", "config: {secret: canary-token}", 1)
	h.writeQueueConfig(configuration)
	if _, err := h.run("queue", "adapter", "approve", "--source", "broken", "--acknowledge"); err != nil {
		t.Fatal(err)
	}
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "adapter-safe-warning") || strings.Contains(string(data), "canary-token") {
		t.Fatalf("stderr diagnostic not surfaced safely: %s", data)
	}
}

func queueAdapterApprovalConfig(executables map[string]string, sourceIDs []string) string {
	var configText strings.Builder
	configText.WriteString("version: 1\nme: {}\nsources:\n")
	for _, sourceID := range sourceIDs {
		fmt.Fprintf(&configText, "  - id: %s\n    adapter: external\n    executable: %q\n    expectedAdapterId: example.adapter\n    expectedVersion: 1.2.3\n    config: {}\n", sourceID, executables[sourceID])
	}
	fmt.Fprintf(&configText, "views:\n  - name: Ready\n    authority: local\n    sources: [%s]\n    filter: {}\n", strings.Join(sourceIDs, ", "))
	return configText.String()
}

func writeQueueAdapterApprovalExecutable(t *testing.T, sourceID, marker string) string {
	t.Helper()
	binary, err := exec.LookPath(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "external-adapter-"+sourceID)
	script := fmt.Sprintf("#!/bin/sh\nprintf started > %s\nexec %s -test.run='^TestQueueAdapterApprovalProcessHelper$' -- %s\n", shellQuote(marker), shellQuote(binary), shellQuote(sourceID))
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func queueAdapterTestDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func runQueueAdapterApprovalProcessHelper(sourceID string) {
	if sourceID == "broken" {
		_, _ = fmt.Fprintln(os.Stderr, "adapter-safe-warning canary-token")
		return
	}
	reader := bufio.NewScanner(os.Stdin)
	for reader.Scan() {
		var request struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(reader.Bytes(), &request) != nil {
			return
		}
		var result json.RawMessage
		switch request.Method {
		case "initialize":
			schema := `{}`
			if sourceID == "schema" {
				schema = `{"type":"object","required":["tenant"],"properties":{"tenant":{"type":"string"}},"additionalProperties":false}`
			}
			result = json.RawMessage(fmt.Sprintf(`{"protocolVersion":1,"manifest":{"id":"example.adapter","version":"1.2.3","protocol":{"minMajor":1,"maxMajor":1},"configSchema":%s,"authentication":[],"resourcePolicy":"generic","capabilities":[],"requiredFeatures":[]}}`, schema))
		case "resolve":
			resolvedID := sourceID
			if sourceID == "schema" {
				var params struct {
					SourceID string `json:"sourceId"`
				}
				if json.Unmarshal(request.Params, &params) != nil {
					return
				}
				resolvedID = params.SourceID
			}
			result = json.RawMessage(fmt.Sprintf(`{"context":%s,"source":{"id":%q,"name":%q,"locator":%q}}`, queueAdapterTestContext(resolvedID), resolvedID, resolvedID, "memory://"+resolvedID))
		case "list":
			resolvedID := sourceID
			if sourceID == "schema" {
				var params struct {
					SourceID string `json:"sourceId"`
				}
				if json.Unmarshal(request.Params, &params) != nil {
					return
				}
				resolvedID = params.SourceID
			}
			result = json.RawMessage(fmt.Sprintf(`{"context":%s,"items":[],"nextCursor":null,"total":{"value":0,"accuracy":"exact"}}`, queueAdapterTestContext(resolvedID)))
		default:
			return
		}
		response, err := json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      string          `json:"id"`
			Result  json.RawMessage `json:"result"`
		}{JSONRPC: "2.0", ID: request.ID, Result: result})
		if err != nil {
			return
		}
		_, _ = fmt.Fprintln(os.Stdout, string(response))
	}
}

func queueAdapterTestContext(sourceID string) string {
	return fmt.Sprintf(`{"principal":null,"configurationGeneration":"test-generation","observedAt":"2025-01-01T00:00:00Z","providerVersion":null,"coverage":{"state":"complete","scope":%q}}`, sourceID)
}
