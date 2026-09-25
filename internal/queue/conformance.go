package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/resource"
)

// AdapterCheck is a single bounded, stable observation of the production host.
// Details deliberately contain no adapter-controlled text or provider payloads.
type AdapterCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

type AdapterCheckManifest struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	ProtocolMajor int    `json:"protocolMajor"`
}

type AdapterCheckReport struct {
	Verdict  string                `json:"verdict"`
	Manifest *AdapterCheckManifest `json:"manifest,omitempty"`
	Checks   []AdapterCheck        `json:"checks"`
}

type AdapterCheckOptions struct {
	Executable   string
	Config       map[string]any
	Target       string // explicit disposable item ID; empty means never dispatch a write
	CancelMarker string // optional fixture marker: .request on start, .done on cancellation
}

// CheckExternalAdapter runs through the same supervised process and read model as
// configured queue sources. No queue configuration, approval or authority is read
// or written. A caller-provided target is the only opt-in to mutation probes.
func CheckExternalAdapter(ctx context.Context, options AdapterCheckOptions) (AdapterCheckReport, error) {
	if options.Executable == "" {
		return AdapterCheckReport{}, fmt.Errorf("adapter executable is required")
	}
	if options.Config == nil {
		options.Config = map[string]any{}
	}
	cfg := config.QueueSource{ID: "adapter-check", Adapter: "external", Executable: options.Executable, Config: options.Config}
	process, err := NewExternalProcessForCheck(cfg)
	if err != nil {
		return AdapterCheckReport{}, err
	}
	defer process.Close()
	report := AdapterCheckReport{Verdict: "pass", Checks: make([]AdapterCheck, 0, 14)}
	add := func(id, status, reason, detail string) {
		report.Checks = append(report.Checks, AdapterCheck{id, status, reason, detail})
		if status == "fail" {
			report.Verdict = "fail"
		}
	}
	manifest, err := process.Initialize(ctx)
	if err != nil {
		add("initialize", "fail", "negotiation-failed", "Adapter did not negotiate a valid v1 manifest")
		return finishAdapterCheck(report), nil
	}
	// Never include the raw manifest: configSchema and optional fields are adapter-controlled.
	report.Manifest = &AdapterCheckManifest{ID: redactExternalText(manifest.ID, process.secretValues()), Version: redactExternalText(manifest.Version, process.secretValues()), ProtocolMajor: 1}
	add("initialize", "pass", "negotiated-v1", "Host accepted the v1 manifest")
	if err := validateExternalConfig(manifest.ConfigSchema, cfg.Config); err != nil {
		add("config", "fail", "config-invalid", "Configuration does not match the manifest schema")
		return finishAdapterCheck(report), nil
	}
	add("config", "pass", "config-valid", "Configuration matches the manifest schema")
	if externalWriteContains(manifest.Authentication, "host-credential-v1") || externalWriteContains(manifest.RequiredFeatures, "host-credential-v1") {
		checkAdapterCredentialLeak(ctx, cfg, add)
	} else {
		add("credential-leak", "skip", "feature-undeclared", "Adapter does not request the optional host credential channel")
	}
	adapter := &ExternalAdapter{source: cfg, process: process}
	source, err := adapter.Resolve(ctx, nil)
	if err != nil {
		add("resolve", "fail", "resolve-failed", "Source resolution failed or returned an invalid binding")
		return finishAdapterCheck(report), nil
	}
	add("resolve", "pass", "source-bound", "Resolved source matches the configured source")
	_, err = adapter.Capabilities(ctx, source, "", nil)
	if err != nil {
		add("capabilities", "fail", "capabilities-invalid", "Capability response failed validation")
	} else {
		add("capabilities", "pass", "capabilities-valid", "Capability response validated")
	}
	first, err := adapter.List(ctx, source, Query{Budget: 1}, "")
	if err != nil || len(first.Items) > 1 {
		add("list-budget", "fail", "list-invalid", "List failed or exceeded a one-item budget")
	} else {
		add("list-budget", "pass", "bounded-page", "List respected a one-item budget")
	}
	var ref Ref
	if err == nil {
		seen := make(map[string]bool)
		for _, item := range first.Items {
			ref, seen[item.Ref.Key()] = item.Ref, true
		}
		cursor := first.NextCursor
		valid := true
		for pages := 0; cursor != "" && pages < 100; pages++ {
			page, nextErr := adapter.List(ctx, source, Query{Budget: 1}, cursor)
			if nextErr != nil || len(page.Items) > 1 || page.NextCursor == cursor {
				valid = false
				break
			}
			for _, item := range page.Items {
				if seen[item.Ref.Key()] {
					valid = false
				}
				seen[item.Ref.Key()] = true
				if ref.ItemID == "" {
					ref = item.Ref
				}
			}
			cursor = page.NextCursor
		}
		if valid && cursor != "" {
			add("continuation", "skip", "probe-limit", "The first 101 pages were valid; the probe stops after 100 continuations")
		} else if valid && first.NextCursor != "" {
			add("continuation", "pass", "exhausted", "Continuation exhausted without duplicate items")
		} else if valid {
			add("continuation", "skip", "no-continuation", "The fixture did not return a continuation cursor")
		} else {
			add("continuation", "fail", "continuation-invalid", "Continuation failed, repeated an item, or did not terminate")
		}
	} else {
		add("continuation", "skip", "list-failed", "List did not provide a usable cursor")
	}
	if ref.ItemID != "" {
		outcomes := adapter.ReadItems(ctx, source, []Ref{ref}, nil, 1)
		if len(outcomes) == 1 && outcomes[0].Kind == "found" && outcomes[0].Item != nil {
			add("read-items", "pass", "item-found", "Listed item was read back")
		} else {
			add("read-items", "fail", "read-invalid", "Listed item was not read back")
		}
		if _, err := adapter.ReadDependencies(ctx, source, ref, "", 1); err != nil {
			add("dependencies", "fail", "dependencies-invalid", "Dependency result failed validation")
		} else {
			add("dependencies", "pass", "dependencies-valid", "Dependency result validated")
		}
		checkAdapterResource(ctx, process, source, ref, add)
	} else {
		for _, id := range []string{"read-items", "dependencies", "resource-policy"} {
			add(id, "skip", "no-fixture-item", "Provide a configuration with at least one item")
		}
	}
	checkAdapterHostBounds(ctx, process, source.ID, add)
	checkAdapterCancellation(ctx, adapter, source, options.CancelMarker, add)
	// A deliberate read-only process failure verifies supervised restart. No
	// dispatched write is involved, so the host must not replay a mutation.
	process.mu.Lock()
	current := process.run
	process.mu.Unlock()
	if current == nil {
		add("crash-recovery", "skip", "source-unavailable", "There was no live process to restart")
	} else {
		process.failProcess(current, "conformance read-only crash", true)
		if _, err := adapter.Resolve(ctx, nil); err != nil {
			add("crash-recovery", "fail", "restart-failed", "Host could not re-resolve after a process exit")
		} else {
			add("crash-recovery", "pass", "restarted", "Host re-resolved the source after a process exit")
		}
	}
	if options.Target == "" {
		add("mutation-receipt", "skip", "target-required", "Use --disposable-target to authorize a fixture write")
		add("lost-response", "skip", "target-required", "No mutation was dispatched")
		add("unknown-outcome", "skip", "target-required", "No mutation was dispatched")
	} else if err != nil || len(first.Items) > 1 {
		for _, id := range []string{"mutation-receipt", "lost-response", "unknown-outcome"} {
			add(id, "skip", "read-failed", "List must validate before any mutation probe")
		}
	} else if !slices.Contains(manifest.Capabilities, "mutation") || !slices.Contains(manifest.Capabilities, "progress") {
		for _, id := range []string{"mutation-receipt", "lost-response", "unknown-outcome"} {
			add(id, "skip", "progress-undeclared", "Manifest does not declare both mutation and progress")
		}
	} else {
		ref := Ref{SourceID: source.ID, ItemID: options.Target}
		if _, _, readErr := (&ExternalWriteAdapter{ExternalAdapter: adapter}).readAuthoritativeItem(ctx, process, source, ref); readErr != nil {
			add("mutation-receipt", "fail", "target-unreadable", "Disposable target must be read authoritatively before writing")
			add("lost-response", "skip", "mutation-not-dispatched", "Target could not be read")
			add("unknown-outcome", "skip", "mutation-not-dispatched", "Target could not be read")
			return finishAdapterCheck(report), nil
		}
		var scoped externalWriteCapabilitiesResult
		err := process.Call(ctx, "capabilities", map[string]any{"sourceId": source.ID, "principal": nullableString(first.Observation.Principal), "ref": ref, "action": "recordProgress", "budget": externalBudget(1)}, &scoped)
		observed, observedErr := scoped.Context.observation()
		if err != nil || observedErr != nil || observed.Principal != first.Observation.Principal {
			add("mutation-receipt", "fail", "action-capabilities-invalid", "Action-scoped capabilities failed validation")
			add("lost-response", "skip", "mutation-not-dispatched", "Action capabilities failed")
			add("unknown-outcome", "skip", "mutation-not-dispatched", "Action capabilities failed")
		} else {
			if scoped.Capabilities["mutation"] == nil || scoped.Capabilities["progress"] == nil {
				for _, id := range []string{"mutation-receipt", "lost-response", "unknown-outcome"} {
					add(id, "skip", "action-not-authorized", "Action-scoped mutation and progress are not both declared")
				}
				return finishAdapterCheck(report), nil
			}
			var mutation, progress Capability
			mutationErr := json.Unmarshal(scoped.Capabilities["mutation"], &mutation)
			progressErr := json.Unmarshal(scoped.Capabilities["progress"], &progress)
			if mutationErr != nil || progressErr != nil || !validCapabilityValue(mutation) || !validCapabilityValue(progress) {
				add("mutation-receipt", "fail", "action-capabilities-invalid", "Action-scoped capabilities are invalid")
				add("lost-response", "skip", "mutation-not-dispatched", "Action capabilities failed")
				add("unknown-outcome", "skip", "mutation-not-dispatched", "Action capabilities failed")
			} else if !externalCapabilityAvailable(mutation) || !externalCapabilityAvailable(progress) || mutation.Permission != Allowed || progress.Permission != Allowed {
				for _, id := range []string{"mutation-receipt", "lost-response", "unknown-outcome"} {
					add(id, "skip", "action-not-authorized", "Action-scoped mutation and progress are not both available and allowed")
				}
			} else {
				checkAdapterMutation(ctx, process, source.ID, options.Target, add)
			}
		}
	}
	return finishAdapterCheck(report), nil
}

func checkAdapterCredentialLeak(ctx context.Context, cfg config.QueueSource, add func(string, string, string, string)) {
	probe, err := NewExternalProcessForCheck(cfg)
	if err != nil {
		add("credential-leak", "fail", "credential-channel-failed", "Could not start a credential probe process")
		return
	}
	// The disposable sentinel is never a provider credential. An adapter may
	// reject it as authentication-failed, but cannot emit it on either pipe.
	secret := "worklease-conformance-credential-canary"
	probe.rememberSecret(secret)
	probe.probeSecret.Store(&secret)
	var response json.RawMessage
	probeErr := probe.Call(ctx, "credential", map[string]any{"sourceId": cfg.ID, "origin": "https://conformance.invalid", "credential": secret}, &response)
	probe.Close()
	probe.mu.Lock()
	probeRun := probe.run
	probe.mu.Unlock()
	leaked := probe.probeLeak.Load() || strings.Contains(string(response), secret)
	if probeRun != nil {
		<-probeRun.stderrDone
		probeRun.stderrMu.Lock()
		leaked = leaked || strings.Contains(string(probeRun.stderrData), secret)
		probeRun.stderrMu.Unlock()
	}
	if leaked || strings.Contains(fmt.Sprint(probeErr), secret) {
		add("credential-leak", "fail", "credential-leaked", "Adapter emitted the credential on stdout, stderr or in a diagnostic")
	} else if probeErr != nil && !strings.Contains(probeErr.Error(), "authentication-failed") && !strings.Contains(probeErr.Error(), "credential-scope-mismatch") {
		add("credential-leak", "fail", "credential-channel-failed", "Adapter did not accept the credential protocol method")
	} else {
		add("credential-leak", "pass", "credential-contained", "Credential channel did not expose the sentinel in adapter output or host diagnostics")
	}
}

func finishAdapterCheck(report AdapterCheckReport) AdapterCheckReport {
	seen := make(map[string]bool, len(report.Checks))
	for _, check := range report.Checks {
		seen[check.ID] = true
	}
	for _, id := range []string{"initialize", "config", "resolve", "capabilities", "list-budget", "continuation", "read-items", "dependencies", "resource-policy", "host-output-guards", "host-input-guards", "deadline", "cancel-notification", "crash-recovery", "secret-redaction", "credential-leak", "mutation-receipt", "lost-response", "unknown-outcome"} {
		if !seen[id] {
			report.Checks = append(report.Checks, AdapterCheck{ID: id, Status: "skip", Reason: "prior-check-failed", Detail: "A prerequisite check failed"})
		}
	}
	return report
}

func checkAdapterResource(ctx context.Context, process *ExternalProcess, source Source, ref Ref, add func(string, string, string, string)) {
	var result struct {
		Policy string `json:"policy"`
		Source string `json:"source"`
		Item   string `json:"item"`
		Scope  string `json:"scope"`
	}
	err := process.Call(ctx, "resourcePolicy", map[string]any{"sourceId": source.ID, "ref": ref, "workKey": "adapter-check"}, &result)
	if err != nil {
		if strings.Contains(err.Error(), "adapter returned diagnostic unsupported-capability") {
			add("resource-policy", "skip", "method-unsupported", "Adapter did not provide optional resourcePolicy inputs")
		} else {
			add("resource-policy", "fail", "policy-invalid", "Resource policy response failed validation")
		}
		return
	}
	if result.Policy != "generic" || result.Source == "" || result.Item != ref.ItemID || result.Scope != "item" {
		add("resource-policy", "fail", "policy-invalid", "Policy inputs differed from the requested item")
		return
	}
	if _, err = resource.Resolve(resource.Input{Provider: result.Policy, Source: result.Source, Item: result.Item}); err != nil {
		add("resource-policy", "fail", "policy-invalid", "Static policy rejected the inputs")
		return
	}
	add("resource-policy", "pass", "static-inputs", "Host resolved the static resource policy inputs")
}

func checkAdapterHostBounds(ctx context.Context, process *ExternalProcess, sourceID string, add func(string, string, string, string)) {
	// These are host enforcement vectors, not requests for a well-behaved adapter
	// to intentionally produce malformed or oversized output.
	malformed := []byte("{bad}\n")
	oversized := append([]byte(`{"jsonrpc":"2.0","id":"1","result":{"items":[`), []byte(strings.Repeat(`"x",`, externalMaxItems+1))...)
	oversized = append(oversized, []byte(`"x"]}}`+"\n")...)
	_, _, _, bad := parseExternalResponse(malformed)
	_, _, _, large := parseExternalResponse(oversized)
	if bad != nil && large != nil && !validExternalCollections([]byte(strings.Repeat("x", externalResultLimit+1))) {
		add("host-output-guards", "pass", "rejected", "Host rejected malformed and oversized protocol vectors")
	} else {
		add("host-output-guards", "fail", "host-guard-failed", "Host accepted an invalid protocol vector")
	}
	var ignored map[string]any
	if err := process.Call(ctx, "list", map[string]any{"sourceId": sourceID, "budget": map[string]int{"maxItems": externalMaxItems + 1, "maxBytes": externalResultLimit}}, &ignored); err != nil {
		add("host-input-guards", "pass", "rejected", "Host rejected an oversized request budget")
	} else {
		add("host-input-guards", "fail", "host-guard-failed", "Host accepted an oversized request budget")
	}
	// An already-expired deadline must be refused without sending a request.
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	if err := process.Call(expired, "list", map[string]any{"sourceId": sourceID}, &ignored); err != nil {
		add("deadline", "pass", "expired-rejected", "Expired request was not accepted")
	} else {
		add("deadline", "fail", "deadline-ignored", "Host accepted an expired request")
	}
	if redacted := redactExternalText("canary-secret-value", []string{"canary-secret-value"}); strings.Contains(redacted, "canary-secret-value") {
		add("secret-redaction", "fail", "redaction-failed", "Host retained a secret sentinel in diagnostic output")
	} else {
		add("secret-redaction", "pass", "redacted", "Host redacted a sentinel from diagnostic output")
	}
}

func checkAdapterCancellation(ctx context.Context, adapter *ExternalAdapter, source Source, marker string, add func(string, string, string, string)) {
	if marker == "" {
		add("cancel-notification", "skip", "fixture-required", "Provide --cancel-marker with an adapter fixture that blocks on the cancellation query")
		return
	}
	if _, err := os.Stat(marker + ".request"); err == nil {
		add("cancel-notification", "fail", "stale-marker", "Remove the prior fixture observation files before checking")
		return
	}
	if _, err := os.Stat(marker + ".done"); err == nil {
		add("cancel-notification", "fail", "stale-marker", "Remove the prior fixture observation files before checking")
		return
	}
	probe, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() {
		_, err := adapter.List(probe, source, Query{Filters: Filters{Text: "__worklease_conformance_cancel__"}, Budget: 1}, "")
		result <- err
	}()
	if !waitAdapterMarker(ctx, marker+".request") {
		cancel()
		add("cancel-notification", "fail", "fixture-not-blocking", "The adapter fixture did not start a blocking cancellation request")
		return
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			add("cancel-notification", "fail", "cancel-ignored", "Cancelled list returned success")
			return
		}
	case <-time.After(3 * time.Second):
		add("cancel-notification", "fail", "cancel-timeout", "Host did not finish the cancelled call")
		return
	}
	if !waitAdapterMarker(ctx, marker+".done") {
		add("cancel-notification", "fail", "cancel-not-observed", "Fixture did not observe $/cancelRequest")
		return
	}
	add("cancel-notification", "pass", "cancel-observed", "Fixture observed cancellation of the in-flight request")
}

func waitAdapterMarker(ctx context.Context, path string) bool {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func checkAdapterMutation(ctx context.Context, process *ExternalProcess, sourceID, itemID string, add func(string, string, string, string)) {
	ref := Ref{SourceID: sourceID, ItemID: itemID}
	operationID := fmt.Sprintf("adapter-check-%d", time.Now().UnixNano())
	patch := map[string]string{"append": "comment", "content": "Adapter conformance fixture", "marker": "worklease-op:" + operationID}
	intent := map[string]any{"ref": ref, "operationId": operationID, "patch": patch, "expectedVersion": nil, "authority": map[string]string{"authorizationRef": "explicit-disposable-target", "scope": sourceID}}
	var response externalMutationResult
	if err := process.Call(ctx, "recordProgress", intent, &response); err != nil || response.Receipt.SourceID != sourceID || response.Receipt.Ref == nil || *response.Receipt.Ref != ref || response.Receipt.Operation != "recordProgress" {
		add("mutation-receipt", "fail", "receipt-missing", "Authorized fixture append did not return a receipt")
		add("lost-response", "skip", "write-failed", "No receipt can be checked")
		add("unknown-outcome", "skip", "write-failed", "No completed write to compare")
		return
	}
	add("mutation-receipt", "pass", "receipt-returned", "Fixture write returned a receipt")
	readback := map[string]any{"sourceId": sourceID, "operation": "recordProgress", "operationId": operationID, "target": ref, "intent": map[string]any{"payload": patch, "expectedVersion": nil}, "receipt": nil}
	var verification externalReadReceiptResult
	var evidence externalWriteEvidence
	readErr := process.Call(ctx, "readReceipt", readback, &verification)
	if readErr == nil {
		readErr = json.Unmarshal(verification.Evidence, &evidence)
	}
	if readErr != nil || verification.Verification != "verified" || evidence.OperationID != operationID || evidence.SourceID != sourceID || evidence.ItemID != itemID || evidence.MarkerCount != 1 || !evidence.AppendProof || evidence.AppendContent != patch["content"] {
		add("lost-response", "fail", "recovery-invalid", "Read-back could not verify the write without its receipt and marker")
	} else {
		add("lost-response", "pass", "verified-without-receipt", "Read-back verified the operation without redispatch")
	}
	readback["operationId"] = operationID + "-absent"
	readback["intent"] = map[string]any{"payload": map[string]string{"append": "comment", "content": "Adapter conformance fixture", "marker": "worklease-op:" + operationID + "-absent"}, "expectedVersion": nil}
	verification = externalReadReceiptResult{}
	if err := process.Call(ctx, "readReceipt", readback, &verification); err != nil || verification.Verification != "unknown" {
		add("unknown-outcome", "fail", "unknown-not-reported", "Unobserved operation did not return unknown")
	} else {
		add("unknown-outcome", "pass", "unknown-preserved", "Unobserved operation remains unknown")
	}
}
