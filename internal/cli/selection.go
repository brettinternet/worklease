package cli

import (
	"os"
	"strings"

	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

// SelectionInput describes the mutually exclusive ways to select a claim.
type SelectionInput struct {
	Handle, Lease, ClaimID, TokenFile string
	TokenFD                           *int
	Revision                          *int64
	Session                           string
}

// Selection is the selected mode. It deliberately does not contain bearer data.
type Selection struct {
	Mode                              string
	Handle, Lease, ClaimID, TokenFile string
	TokenFD                           *int
	Revision                          *int64
	Session                           string
}

// Select enforces the section 4 boundary before any contextual discovery.
func Select(in SelectionInput, mutation bool) (Selection, error) {
	handle, lease := strings.TrimSpace(in.Handle), strings.TrimSpace(in.Lease)
	claim, tokenFile := strings.TrimSpace(in.ClaimID), strings.TrimSpace(in.TokenFile)
	tokens := 0
	if tokenFile != "" {
		tokens++
	}
	if in.TokenFD != nil {
		tokens++
	}
	explicit := claim != "" || tokens > 0 || in.Revision != nil
	if handle != "" && lease != "" {
		return Selection{}, reason.New("credential-source-conflict", "handle and lease selectors are exclusive")
	}
	if (handle != "" || lease != "") && explicit {
		return Selection{}, reason.New("credential-source-conflict", "explicit credentials cannot be mixed with a handle")
	}
	if tokens > 1 {
		return Selection{}, reason.New("credential-source-conflict", "exactly one token source may be selected")
	}
	if explicit {
		if claim == "" || tokens != 1 {
			return Selection{}, reason.New("credential-source-conflict", "explicit credentials require claim-id and one token source")
		}
		if mutation && in.Revision == nil {
			return Selection{}, reason.New("invalid-argument", "mutations require revision")
		}
		return Selection{Mode: "explicit", ClaimID: claim, TokenFile: tokenFile, TokenFD: in.TokenFD, Revision: in.Revision, Session: in.Session}, nil
	}
	if handle != "" {
		return Selection{Mode: "handle", Handle: handle, Session: in.Session}, nil
	}
	if lease != "" {
		return Selection{Mode: "lease", Lease: lease, Session: in.Session}, nil
	}
	return Selection{Mode: "context", Session: in.Session}, nil
}

// ValidateSelection adapts command flags to Select, and honors the explicit
// handle environment variable without reading or validating a handle file.
func ValidateSelection(cmd *urfave.Command, mutation bool) error {
	fd := cmd.Int("token-fd")
	var fdp *int
	if cmd.IsSet("token-fd") {
		fdp = &fd
	}
	var rev *int64
	if cmd.IsSet("revision") {
		value := cmd.Int64("revision")
		rev = &value
	}
	_, err := Select(SelectionInput{Handle: first(cmd.String("handle"), os.Getenv("WORKLEASE_HANDLE")), Lease: cmd.String("lease"), ClaimID: cmd.String("claim-id"), TokenFile: cmd.String("token-file"), TokenFD: fdp, Revision: rev, Session: first(cmd.String("session"), os.Getenv("WORKLEASE_SESSION_ID"))}, mutation)
	return err
}

func first(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}
