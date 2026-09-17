package instructions

import (
	"strings"
	"testing"
)

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
