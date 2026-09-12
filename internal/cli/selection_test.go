package cli

import (
	"errors"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestSelectExclusiveModes(t *testing.T) {
	t.Parallel()
	revision, fd := int64(2), 3
	for _, test := range []struct {
		name                 string
		input                SelectionInput
		mutation             bool
		wantMode, wantReason string
	}{
		{"context", SelectionInput{Session: "loop"}, false, "context", ""},
		{"handle", SelectionInput{Handle: "claim.json"}, true, "handle", ""},
		{"lease", SelectionInput{Lease: "private-ref"}, true, "lease", ""},
		{"explicit read", SelectionInput{ClaimID: "claim", TokenFD: &fd}, false, "explicit", ""},
		{"explicit mutation", SelectionInput{ClaimID: "claim", TokenFile: "token", Revision: &revision}, true, "explicit", ""},
		{"mixed", SelectionInput{Handle: "claim.json", ClaimID: "claim", TokenFD: &fd}, false, "", reason.ReasonCredentialSourceConflict},
		{"incomplete", SelectionInput{ClaimID: "claim"}, false, "", reason.ReasonCredentialSourceConflict},
		{"missing revision", SelectionInput{ClaimID: "claim", TokenFD: &fd}, true, "", reason.ReasonInvalidArgument},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Select(test.input, test.mutation)
			if test.wantReason == "" {
				if err != nil || got.Mode != test.wantMode {
					t.Fatalf("selection=%+v err=%v", got, err)
				}
				return
			}
			var classified *reason.Error
			if !errors.As(err, &classified) || classified.Reason != test.wantReason {
				t.Fatalf("err=%#v", err)
			}
		})
	}
}
