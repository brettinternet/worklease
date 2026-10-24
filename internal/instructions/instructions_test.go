package instructions

import (
	"strings"
	"testing"
)

func TestLoopInstructionsRequirePostWaitVerificationAndExplicitHandoff(t *testing.T) {
	loop, err := For("loop")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(loop, "\n")
	for _, want := range []string{
		"Use the harness's own loop-run ID unchanged; otherwise its own session ID unchanged",
		"CLI: set WORKLEASE_SESSION_ID",
		"MCP: pass it as sessionId to acquire, persist the returned lease before work",
		"Never truncate the ID or derive it",
		"MCP automatic heartbeat lasts only while the server process runs",
		"After any subagent run, wait, long command, human pause, resumed session, or new loop iteration",
		"before any file edit, mutating command, provider write, commit, merge, or cleanup",
		"Expiry does not prove the previous worker stopped",
		"require an explicit handoff or authoritative evidence of abandonment",
		"Claim as soon as you choose a candidate, before reading its full intent",
		"on conflict do not wait: pick the next ready candidate",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing loop ownership guardrail %q", want)
		}
	}
}

func TestRemoteSetupPreservesAuthoritySelection(t *testing.T) {
	remote, err := For("remote")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"worklease profile default before enrollment",
		"get approval for that scope change before proceeding",
		"preserves an existing default",
		"worklease profile show without a name",
	} {
		if !strings.Contains(strings.Join(remote, "\n"), want) {
			t.Fatalf("missing remote selection guardrail %q", want)
		}
	}
	server, err := For("server")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"worklease enroll --profile NAME --invite-file PATH_PRINTED_BY_INIT",
		"worklease invite issue --profile NAME",
	} {
		if !strings.Contains(strings.Join(server, "\n"), want) {
			t.Fatalf("server instructions must pin enrollment and invitations to the same authority: missing %q", want)
		}
	}
}
