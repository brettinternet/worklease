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
		"MCP automatic heartbeat is process-scoped",
		"After every subagent run, wait, long command, human pause, resumed session, or new loop iteration",
		"before any filesystem edit, mutating command, provider write, commit, merge, or cleanup",
		"Claim expiry does not prove the prior worker stopped",
		"require explicit handoff or authoritative abandonment evidence",
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
		"obtain approval for that scope change before proceeding",
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
