package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/lease"
)

func TestOpaqueTextWidthAndShortening(t *testing.T) {
	if displayWidth("a界e\u0301") != 4 {
		t.Fatalf("width=%d", displayWidth("a界e\u0301"))
	}
	value := shortenOpaque("前缀-very-long-opaque-value-末尾", 12)
	if displayWidth(value) > 12 || !strings.Contains(value, "…") {
		t.Fatalf("shortened=%q width=%d", value, displayWidth(value))
	}
}

func TestListTextAlignsByTerminalCells(t *testing.T) {
	values := []lease.ClaimView{{ClaimID: strings.Repeat("a", 32), Resources: []string{"短"}, AgentID: "agent", Active: true}, {ClaimID: strings.Repeat("b", 32), Resources: []string{"long-resource"}, AgentID: "β", Active: false}}
	var out bytes.Buffer
	if err := writeListText(&out, values); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("output=%q", out.String())
	}
	// The state column starts at one terminal column for both rows.
	stateColumn := func(line, marker string) int { return displayWidth(line[:strings.Index(line, marker)]) }
	if stateColumn(lines[2], "active") != stateColumn(lines[3], "expired") {
		t.Fatalf("unaligned output=%q", out.String())
	}
}
