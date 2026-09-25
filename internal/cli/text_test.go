package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/resource"
)

func TestKeyTextUsesDeterministicSafeFieldsWithoutBanner(t *testing.T) {
	t.Parallel()
	value := resource.Key{Provider: "path", Source: "repo\x1b", Item: "TASK-99", Resource: "path:/tmp/file", Capability: "path-mutation", Scope: "host", IdentityScope: "path", LocalReplaceAllowed: true}
	var first, second bytes.Buffer
	if err := writeKeyText(&first, value); err != nil {
		t.Fatal(err)
	}
	if err := writeKeyText(&second, value); err != nil {
		t.Fatal(err)
	}
	want := "derived resource key\nprovider: path\nresource: path:/tmp/file\ncapability: path-mutation\nscope: host\nidentityScope: path\nsource: repo\\u001b\nitem: TASK-99\nlocalReplaceAllowed: true\nproviderFencing: false\ngenericExecutionGuarantee: local-coordination\n"
	if got := first.String(); got != want || got != second.String() || strings.HasPrefix(got, "key\n") || strings.ContainsRune(got, '\x1b') || strings.Contains(got, "map[") {
		t.Fatalf("key output=%q want=%q", got, want)
	}
}

func TestOpaqueTextWidthAndShortening(t *testing.T) {
	t.Parallel()
	if displayWidth("a界e\u0301") != 4 || displayWidth("𠀀") != 2 {
		t.Fatalf("widths=%d,%d", displayWidth("a界e\u0301"), displayWidth("𠀀"))
	}
	value := shortenOpaque("前缀-very-long-opaque-value-末尾", 12)
	if displayWidth(value) > 12 || !strings.Contains(value, "…") {
		t.Fatalf("shortened=%q width=%d", value, displayWidth(value))
	}
}

func TestReceiptTextSummarizesHeartbeatAndRelease(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	claimID, operationID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	hash := strings.Repeat("c", 64)
	now := time.Date(2026, 9, 13, 2, 13, 9, 0, time.UTC)
	claim := &lease.ClaimView{ClaimID: claimID, Resources: []string{"exact-resource"}, AgentID: "agent", SessionID: "session", WorkKey: "work", Guarantee: "local-coordination", AuthorityID: strings.Repeat("d", 32), Revision: 4, AcquiredAt: now.Add(-time.Minute), HeartbeatAt: now, ExpiresAt: now.Add(time.Minute), Active: true, CheckpointPresent: true}
	var compact, full bytes.Buffer
	if err := writeStatusTextAt(&compact, lease.Status{Claim: claim}, false, false, now); err != nil {
		t.Fatal(err)
	}
	if err := writeStatusTextAt(&full, lease.Status{Claim: claim}, true, false, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), "claimSessionId:") || strings.Contains(compact.String(), "expiresAt:") || !strings.Contains(compact.String(), "expires: 1m left") || !strings.Contains(full.String(), "claimSessionId: session") || !strings.Contains(full.String(), "expiresAt: 2026-09-13T02:14:09.000000Z") || !strings.Contains(full.String(), "resources: exact-resource") {
		t.Fatalf("compact=%q full=%q", compact.String(), full.String())
	}

	completedAt := now.Add(-30 * time.Second)
	epoch := ledger.Epoch{ClaimID: claimID, AgentID: "agent", SessionID: "session", WorkKey: "work", Resources: []string{"exact-resource"}, AcquiredAt: now.Add(-2 * time.Minute), Status: "open", Operations: []ledger.Operation{{OperationID: operationID, Kind: "exec", State: "completed", RequestSHA256: hash, StartedAt: now.Add(-time.Minute), CompletedAt: &completedAt}}}
	page := ledger.HistoryPage{Resource: "exact-resource", Epochs: []ledger.Epoch{epoch}, NextCursor: "cursor", Coverage: ledger.HistoryCoverage{PrunedThroughSequence: "0"}}
	compact.Reset()
	full.Reset()
	if err := writeHistoryTextAt(&compact, page, false, false, now); err != nil {
		t.Fatal(err)
	}
	if err := writeHistoryTextAt(&full, page, true, false, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), hash) || !strings.Contains(compact.String(), "agent open acquired 2m ago operations exec") || strings.Contains(compact.String(), "acquiredAt=") || strings.Contains(compact.String(), "claimId=") {
		t.Fatalf("compact=%q", compact.String())
	}
	for _, want := range []string{"claimId=" + claimID, "acquiredAt=2026-09-13T02:11:09.000000Z", "workKey=work", "resources=exact-resource", "claimSessionId=session", "startedAt=2026-09-13T02:12:09.000000Z", "completedAt=2026-09-13T02:12:39.000000Z", "requestSha256=" + hash} {
		if !strings.Contains(full.String(), want) {
			t.Fatalf("full output missing %q: %q", want, full.String())
		}
	}
	if strings.Contains(full.String(), "1: claimId=") {
		t.Fatalf("full history used synthetic numbering: %q", full.String())
	}

	revision := int64(4)
	events := ledger.EventsPage{NextCursor: "cursor", Events: []ledger.Event{{Sequence: "1", At: now.Add(-30 * time.Second), Kind: "heartbeat", ClaimID: claimID, Resources: []string{"exact-resource"}, OperationID: operationID, Revision: &revision, AgentID: "agent", Detail: map[string]any{"reason": "renewed"}}}}
	compact.Reset()
	full.Reset()
	if err := writeEventsTextAt(&compact, events, false, false, now); err != nil {
		t.Fatal(err)
	}
	if err := writeEventsTextAt(&full, events, true, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compact.String(), "heartbeat exact-resource 30s ago") || strings.Contains(compact.String(), "resources=") || strings.Contains(compact.String(), "sequence=") || strings.Contains(compact.String(), "claimId=") || strings.Contains(compact.String(), "at=") || strings.Contains(compact.String(), "nextCursor:") {
		t.Fatalf("compact=%q", compact.String())
	}
	if strings.Contains(full.String(), "nextCursor:") || strings.Contains(full.String(), "1: sequence=") || !strings.Contains(full.String(), "sequence=1 kind=heartbeat") || !strings.Contains(full.String(), "claimId="+claimID) || !strings.Contains(full.String(), "at=2026-09-13T02:12:39.000000Z") || !strings.Contains(full.String(), "resources=exact-resource") || !strings.Contains(full.String(), "detail={\"reason\":\"renewed\"}") {
		t.Fatalf("full=%q", full.String())
	}
}

func TestCompactTimelineTextIsOperationallyInformative(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 13, 2, 13, 9, 0, time.UTC)
	claimID, other := strings.Repeat("a", 32), strings.Repeat("b", 32)
	long := "backlog-md:%2FUsers%2Fbrett%2Fdev%2Fworklease%2F.git:docs%2Fbacklog:TASK-101"

	var events bytes.Buffer
	page := ledger.EventsPage{NextCursor: "opaque-cursor", Gap: true, Events: []ledger.Event{
		{Sequence: "7", At: now.Add(-90 * time.Second), Kind: "acquired", ClaimID: claimID, Resources: []string{long}},
		{Sequence: "8", At: now, Kind: "released", ClaimID: other, Resources: []string{"first", "second"}},
		{Sequence: "9", At: now, Kind: "gc-applied"},
	}}
	if err := writeEventsTextAt(&events, page, false, false, now); err != nil {
		t.Fatal(err)
	}
	got := events.String()
	for _, want := range []string{"3 events\ngap: true\n", "acquired backlog-md:worklease:TASK-101 1m ago\n", "released first, second now\n", "gc-applied now\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("events missing %q: %q", want, got)
		}
	}
	for _, omitted := range []string{"opaque-cursor", "nextCursor", "sequence=", "claimId=", "resources=", "kind=", "at=", "1: ", "2: ", "3: "} {
		if strings.Contains(got, omitted) {
			t.Fatalf("compact events included %q: %q", omitted, got)
		}
	}

	ended := now.Add(-2 * time.Minute)
	final := int64(6)
	history := ledger.HistoryPage{Resource: "exact-resource", NextCursor: "opaque-cursor", Gap: true, Coverage: ledger.HistoryCoverage{PrunedThroughSequence: "0"}, Epochs: []ledger.Epoch{
		{ClaimID: claimID, AgentID: "alice", SessionID: "s1", Resources: []string{"exact-resource"}, AcquiredAt: now.Add(-10 * time.Minute), EndedAt: &ended, EndReason: "released", FinalRevision: &final, Status: "complete", Operations: []ledger.Operation{{Kind: "acquire", State: "completed"}, {Kind: "heartbeat", State: "completed"}, {Kind: "exec", State: "completed"}}},
		{ClaimID: other, AgentID: "bob", Resources: []string{"exact-resource"}, AcquiredAt: now.Add(-time.Minute), Status: "open", Operations: []ledger.Operation{{Kind: "acquire", State: "completed"}, {Kind: "exec", State: "started"}}},
	}}
	var compact bytes.Buffer
	if err := writeHistoryTextAt(&compact, history, false, false, now); err != nil {
		t.Fatal(err)
	}
	got = compact.String()
	for _, want := range []string{"2 history epochs for exact-resource\ngap: true\n", "alice complete acquired 10m ago ended released 2m ago operations acquire,heartbeat,exec\n", "bob open acquired 1m ago operations acquire,exec:started\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("history missing %q: %q", want, got)
		}
	}
	for _, omitted := range []string{"opaque-cursor", "nextCursor", "prunedThroughSequence", "s1", "claimId=", "agentId=", "status=", "acquired=", "ended=", "operations=", "1: ", "2: "} {
		if strings.Contains(got, omitted) {
			t.Fatalf("compact history included %q: %q", omitted, got)
		}
	}

	compact.Reset()
	history.Gap = false
	history.Coverage.PrunedThroughSequence = "12"
	history.Epochs = history.Epochs[:1]
	if err := writeHistoryTextAt(&compact, history, false, false, now); err != nil {
		t.Fatal(err)
	}
	if got := compact.String(); !strings.HasPrefix(got, "1 history epoch for exact-resource\npruned: events through sequence 12 were collected\n") {
		t.Fatalf("history coverage=%q", got)
	}

	var full bytes.Buffer
	if err := writeHistoryTextAt(&full, history, true, false, now); err != nil {
		t.Fatal(err)
	}
	if got := full.String(); !strings.Contains(got, "prunedThroughSequence: 12\n") || strings.Contains(got, "opaque-cursor") || !strings.Contains(got, "endReason=released") || !strings.Contains(got, "finalRevision=6") || strings.Contains(got, "operations=") {
		t.Fatalf("full history=%q", got)
	}
}

func TestFullResourceStatusOmitsSyntheticRowNumbers(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	status := lease.Status{Resources: []lease.ResourceStatus{{Resource: "first", State: "free"}, {Resource: "second", State: "claimed"}}}
	if err := writeStatusText(&out, status, true, false); err != nil {
		t.Fatal(err)
	}
	want := "checked 2 resources\nfirst state=free\nsecond state=claimed\n"
	if got := out.String(); got != want {
		t.Fatalf("output=%q want=%q", got, want)
	}
}

func TestListTextExplicitEmptyState(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		var out bytes.Buffer
		if err := writeListTextAt(&out, nil, full, true, time.Now()); err != nil {
			t.Fatal(err)
		}
		if out.String() != "no current claims\n" {
			t.Fatalf("full=%t output=%q", full, out.String())
		}
	}
}

func TestHistoryAndEventsColorOnlySemanticValues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 13, 2, 13, 9, 0, time.UTC)
	claimID := strings.Repeat("a", 32)
	var out bytes.Buffer
	page := ledger.HistoryPage{Resource: "resource", Epochs: []ledger.Epoch{{ClaimID: claimID, AcquiredAt: now, Status: "open"}}}
	if err := writeHistoryTextAt(&out, page, true, true, now); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "status=\x1b[32mopen\x1b[0m") || !strings.Contains(got, "claimId="+claimID) || strings.Contains(got, "claimId=\x1b") || strings.Contains(got, "at=\x1b") {
		t.Fatalf("history color scope=%q", got)
	}
	out.Reset()
	events := ledger.EventsPage{Events: []ledger.Event{{Sequence: "1", At: now, Kind: "heartbeat", ClaimID: claimID}}}
	if err := writeEventsTextAt(&out, events, true, true, now); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "kind=\x1b[32mheartbeat\x1b[0m") || !strings.Contains(got, "claimId="+claimID) || strings.Contains(got, "claimId=\x1b") || strings.Contains(got, "at=\x1b") {
		t.Fatalf("event color scope=%q", got)
	}
}

func TestPayloadBlocksEscapeTerminalControls(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := writeTextBlock(&out, "stdout", "safe\u009b2J\x1b[2J"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, `safe\u009b2J\u001b[2J`) || strings.ContainsRune(got, '\u009b') || strings.ContainsRune(got, '\x1b') {
		t.Fatalf("unsafe payload block=%q", got)
	}
}

func TestRemainingStructuredTextSummariesAvoidGoValueDumps(t *testing.T) {
	t.Parallel()
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
	}, "unknownOperations: [\""+operationID+"\"]", "recovery: 1\nresource=resource claimId="+strings.Repeat("c", 32)+" checkpointPresent=true")
	assert("transfer", func(out *bytes.Buffer) error {
		return writeTransferText(out, map[string]any{"claimId": claimID, "agentId": "next", "sessionId": "loop", "revision": int64(1), "expiresAt": now, "guarantee": "local-coordination"})
	}, "transferred ownership", "agentId: next", "revision: 1")
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
