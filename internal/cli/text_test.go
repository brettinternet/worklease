package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
)

func TestOpaqueTextWidthAndShortening(t *testing.T) {
	if displayWidth("a界e\u0301") != 4 || displayWidth("𠀀") != 2 {
		t.Fatalf("widths=%d,%d", displayWidth("a界e\u0301"), displayWidth("𠀀"))
	}
	value := shortenOpaque("前缀-very-long-opaque-value-末尾", 12)
	if displayWidth(value) > 12 || !strings.Contains(value, "…") {
		t.Fatalf("shortened=%q width=%d", value, displayWidth(value))
	}
}

func TestReceiptTextSummarizesHeartbeatAndRelease(t *testing.T) {
	claimID := strings.Repeat("a", 32)
	cases := []struct {
		name    string
		receipt lease.Receipt
		want    string
	}{
		{name: "heartbeat", receipt: lease.Receipt{Kind: "heartbeat", ClaimID: claimID, Revision: 2, Result: map[string]any{"expiresAt": "2026-09-13T02:13:09.228762Z"}}, want: "renewed claim aaaaaaaaaaa…aaaaaaaaaaaa\nrevision: 2\nexpiresAt: 2026-09-13T02:13:09.228762Z\n"},
		{name: "release replay", receipt: lease.Receipt{Kind: "release", ClaimID: claimID, Revision: 3, Idempotent: true, Result: map[string]any{"reason": "completed"}}, want: "released claim aaaaaaaaaaa…aaaaaaaaaaaa\nrevision: 3\nreason: completed\nreplayed: true\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeReceiptText(&out, test.receipt, nil); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != test.want || strings.Contains(got, "map[") || strings.Contains(got, "receipt:") {
				t.Fatalf("output=%q want=%q", got, test.want)
			}
		})
	}
}

func TestStatusTextColorsSemanticStates(t *testing.T) {
	var out bytes.Buffer
	status := lease.Status{Resources: []lease.ResourceStatus{{Resource: "available", State: "free"}, {Resource: "held", State: "claimed"}}}
	if err := writeStatusText(&out, status, false, true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"available=\x1b[32mfree\x1b[0m", "held=\x1b[33mclaimed\x1b[0m"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("colored status missing %q: %q", want, out.String())
		}
	}
}

func TestStatusHistoryAndEventsFullTextExpandsMetadata(t *testing.T) {
	claimID, operationID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	hash := strings.Repeat("c", 64)
	now := time.Date(2026, 9, 13, 2, 13, 9, 0, time.UTC)
	claim := &lease.ClaimView{ClaimID: claimID, Resources: []string{"exact-resource"}, AgentID: "agent", SessionID: "session", WorkKey: "work", Guarantee: "local-coordination", AuthorityID: strings.Repeat("d", 32), Revision: 4, AcquiredAt: now.Add(-time.Minute), HeartbeatAt: now, ExpiresAt: now.Add(time.Minute), Active: true, CheckpointPresent: true}
	var compact, full bytes.Buffer
	if err := writeStatusText(&compact, lease.Status{Claim: claim}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := writeStatusText(&full, lease.Status{Claim: claim}, true, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), "sessionId:") || !strings.Contains(full.String(), "sessionId: session") || !strings.Contains(full.String(), "resources: exact-resource") {
		t.Fatalf("compact=%q full=%q", compact.String(), full.String())
	}

	epoch := ledger.Epoch{ClaimID: claimID, AgentID: "agent", SessionID: "session", Resources: []string{"exact-resource"}, AcquiredAt: now, Status: "open", Operations: []ledger.Operation{{OperationID: operationID, Kind: "exec", State: "started", RequestSHA256: hash}}}
	page := ledger.HistoryPage{Resource: "exact-resource", Epochs: []ledger.Epoch{epoch}, NextCursor: "cursor", Coverage: ledger.HistoryCoverage{PrunedThroughSequence: "0"}}
	compact.Reset()
	full.Reset()
	if err := writeHistoryText(&compact, page, false); err != nil {
		t.Fatal(err)
	}
	if err := writeHistoryText(&full, page, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), hash) || !strings.Contains(full.String(), "requestSha256="+hash) || !strings.Contains(full.String(), "resources=exact-resource") {
		t.Fatalf("compact=%q full=%q", compact.String(), full.String())
	}

	revision := int64(4)
	events := ledger.EventsPage{NextCursor: "cursor", Events: []ledger.Event{{Sequence: "1", At: now, Kind: "heartbeat", ClaimID: claimID, Resources: []string{"exact-resource"}, OperationID: operationID, Revision: &revision, AgentID: "agent", Detail: map[string]any{"reason": "renewed"}}}}
	compact.Reset()
	full.Reset()
	if err := writeEventsText(&compact, events, false); err != nil {
		t.Fatal(err)
	}
	if err := writeEventsText(&full, events, true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), "exact-resource") || !strings.Contains(full.String(), "resources=exact-resource") || !strings.Contains(full.String(), "detail={\"reason\":\"renewed\"}") {
		t.Fatalf("compact=%q full=%q", compact.String(), full.String())
	}
}

func TestPayloadBlocksEscapeTerminalControls(t *testing.T) {
	var out bytes.Buffer
	if err := writeTextBlock(&out, "stdout", "safe\u009b2J\x1b[2J"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, `safe\u009b2J\u001b[2J`) || strings.ContainsRune(got, '\u009b') || strings.ContainsRune(got, '\x1b') {
		t.Fatalf("unsafe payload block=%q", got)
	}
}

func TestRemainingStructuredTextSummariesAvoidGoValueDumps(t *testing.T) {
	claimID, operationID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	now := time.Date(2026, 9, 13, 2, 13, 9, 0, time.UTC)
	assert := func(name string, write func(*bytes.Buffer) error, wants ...string) {
		t.Helper()
		var out bytes.Buffer
		if err := write(&out); err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("%s output missing %q: %q", name, want, out.String())
			}
		}
		if strings.Contains(out.String(), "map[") {
			t.Fatalf("%s output contains Go map dump: %q", name, out.String())
		}
	}
	assert("acquire recovery", func(out *bytes.Buffer) error {
		return writeAcquireText(out, map[string]any{
			"claimId": claimID, "resources": []string{"resource"}, "agentId": "agent", "sessionId": "session", "revision": int64(1), "expiresAt": now, "guarantee": "local-coordination",
			"unknownOperations": []string{operationID}, "recovery": []lease.Recovery{{Resource: "resource", ClaimID: strings.Repeat("c", 32), CheckpointPresent: true}},
		})
	}, "unknownOperations: [\""+operationID+"\"]", "recovery[1]: resource=resource claim="+strings.Repeat("c", 32)+" checkpointPresent=true")
	assert("transfer", func(out *bytes.Buffer) error {
		return writeTransferText(out, map[string]any{"claimId": claimID, "agentId": "next", "sessionId": "loop", "revision": int64(1), "expiresAt": now, "guarantee": "local-coordination"})
	}, "transferred ownership", "agent: next", "revision: 1")
	assert("verify", func(out *bytes.Buffer) error {
		return writeVerificationText(out, &lease.ClaimView{ClaimID: claimID, Resources: []string{"resource"}, Revision: 4, ExpiresAt: now}, []string{operationID})
	}, "verified claim", "resources: resource", "revision: 4", "unknownOperations: [\""+operationID+"\"]")
	result := map[string]any{"argv": []any{"printf", "ok"}, "executionDirectory": map[string]any{"mode": "caller"}, "stdout": "ok", "stderr": "", "stdoutBytes": 2, "stderrBytes": 0, "stdoutTruncated": false, "stderrTruncated": false, "guarantee": "local-coordination"}
	assert("exec", func(out *bytes.Buffer) error {
		return writeExecText(out, lease.Receipt{ClaimID: claimID, Revision: 5, Result: result}, 0)
	}, "completed command", `argv: ["printf","ok"]`, "directory: caller", "stdout:\n  ok")
	assert("replace", func(out *bytes.Buffer) error {
		return writeReplaceText(out, lease.Receipt{ClaimID: claimID, Revision: 6, Result: map[string]any{"path": "/tmp/file", "contentBytes": 12}})
	}, "replaced file", "path: /tmp/file", "contentBytes: 12")
	assert("inspect", func(out *bytes.Buffer) error {
		return writeInspectionText(out, ledger.Operation{ClaimID: claimID, OperationID: operationID, Kind: "exec", State: "completed", StartedAt: now, Receipt: map[string]any{"returncode": 0}})
	}, "operation bbbbbbbbbbb…bbbbbbbbbbbb is completed", "receipt:\n  {\n    \"returncode\": 0\n  }")
	assert("reconcile", func(out *bytes.Buffer) error {
		return writeReconciliationText(out, lease.ReconciliationReceipt{TargetClaimID: claimID, TargetOperationID: operationID, Outcome: "observed-success", Revision: 7, ReconciledAt: now, ExpiresAt: now.Add(time.Minute)})
	}, "reconciled operation", "outcome: observed-success", "revision: 7")
}

func TestListTextCompactAndFull(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	resource := "backlog-md:%2FUsers%2Fbrett%2Fdev%2Fworklease%2F.git:docs%2Fbacklog:TASK-68"
	values := []lease.ClaimView{{ClaimID: strings.Repeat("a", 32), Resources: []string{resource}, AgentID: "agent", Active: true, ExpiresAt: now.Add(62 * time.Minute)}}

	var compact bytes.Buffer
	if err := writeListTextAt(&compact, values, false, false, now); err != nil {
		t.Fatal(err)
	}
	if got := compact.String(); !strings.HasPrefix(got, "STATE") || strings.Contains(got, "\nlist\n") || strings.Contains(got, values[0].ClaimID) || strings.Contains(got, resource) || !strings.Contains(got, "backlog-md:worklease:TASK-68") || !strings.Contains(got, "1h 2m left") {
		t.Fatalf("compact output=%q", got)
	}

	var full bytes.Buffer
	if err := writeListTextAt(&full, values, true, false, now); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CLAIM_ID", "AGENT_ID", "EXPIRES_AT", resource, values[0].ClaimID, "agent", "2026-09-12T13:02:00.000000Z"} {
		if !strings.Contains(full.String(), want) {
			t.Fatalf("full output missing %q: %q", want, full.String())
		}
	}
}

func TestListTextAlignsByTerminalCellsAndColorsState(t *testing.T) {
	now := time.Now()
	values := []lease.ClaimView{{Resources: []string{"短"}, Active: true, ExpiresAt: now.Add(time.Minute)}, {Resources: []string{"long-resource"}, Active: false, ExpiresAt: now.Add(-time.Minute)}}
	var out bytes.Buffer
	if err := writeListTextAt(&out, values, false, false, now); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("output=%q", out.String())
	}
	resourceColumn := func(line, marker string) int { return displayWidth(line[:strings.Index(line, marker)]) }
	if resourceColumn(lines[1], "短") != resourceColumn(lines[2], "long-resource") {
		t.Fatalf("unaligned output=%q", out.String())
	}

	out.Reset()
	if err := writeListTextAt(&out, values, false, true, now); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []string{"\x1b[1mSTATE", "\x1b[32mactive", "\x1b[33mexpired"} {
		if !strings.Contains(out.String(), sequence) {
			t.Fatalf("colored output missing %q: %q", sequence, out.String())
		}
	}
}

func TestSummarizeResource(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	coordination := summarizeResource("coordination:generic:" + digest)
	if !strings.HasPrefix(coordination, "generic:#") || len(coordination) != len("generic:#")+8 || strings.Contains(coordination, digest[:12]) {
		t.Fatalf("unsafe or unreadable coordination summary=%q", coordination)
	}
	for _, test := range []struct{ resource, want string }{
		{"backlog-md:C%3A%5CUsers%5Cbrett%5Cdev%5Chum%5C.git:backlog:HUM-103", "backlog-md:hum:HUM-103"},
		{"backlog-md:%2Ftmp%2Fteam%5Cblue%2F.git:backlog:TASK-1", `backlog-md:team\blue:TASK-1`},
		{"backlog-md:%2Ftmp%2F%1B%5B2J%2F.git:backlog:TASK-1", `backlog-md:\u001b[2J:TASK-1`},
	} {
		got := summarizeResource(test.resource)
		if got != test.want || strings.ContainsAny(got, "\x1b\n\r\t") {
			t.Fatalf("summary=%q want=%q", got, test.want)
		}
	}
}
