package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
)

// displayWidth counts terminal cells without depending on a layout package.
// Opaque identifiers are shown in full in JSON and shortened only for people.
func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		if r == '\u200d' || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		if wideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func wideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe10 && r <= 0xfe19 || r >= 0xfe30 && r <= 0xfe6f || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6 || r >= 0x1f300 && r <= 0x1faff)
}

func shortenOpaque(value string, maxCells int) string {
	if maxCells < 4 || displayWidth(value) <= maxCells {
		return value
	}
	const ellipsis = "…"
	budget := maxCells - displayWidth(ellipsis)
	left := budget / 2
	right := budget - left
	prefix := takeCells(value, left, false)
	suffix := takeCells(value, right, true)
	return prefix + ellipsis + suffix
}

func takeCells(value string, cells int, fromEnd bool) string {
	if cells <= 0 {
		return ""
	}
	runes := []rune(value)
	if fromEnd {
		var out []rune
		used := 0
		for i := len(runes) - 1; i >= 0; i-- {
			w := runeWidth(runes[i])
			if used+w > cells {
				break
			}
			out = append(out, runes[i])
			used += w
		}
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return string(out)
	}
	var out []rune
	used := 0
	for _, r := range runes {
		w := runeWidth(r)
		if used+w > cells {
			break
		}
		out = append(out, r)
		used += w
	}
	return string(out)
}
func runeWidth(r rune) int {
	if r < 0x20 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if wideRune(r) {
		return 2
	}
	return 1
}

func writeStatusText(w io.Writer, value lease.Status) error {
	if value.Claim != nil {
		return writeLines(w, "status", []string{
			"claim: " + shortenOpaque(value.Claim.ClaimID, 40),
			"state: " + stateForClaim(*value.Claim),
			"agent: " + shortenOpaque(value.Claim.AgentID, 40),
			"expiresAt: " + value.Claim.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		})
	}
	parts := make([]string, 0, len(value.Resources))
	for _, item := range value.Resources {
		parts = append(parts, shortenOpaque(item.Resource, 48)+"="+item.State)
	}
	if len(parts) == 0 {
		parts = append(parts, "none")
	}
	return writeLines(w, "status", []string{"resources: " + strings.Join(parts, ", ")})
}
func stateForClaim(value lease.ClaimView) string {
	if value.Active {
		return "active"
	}
	return "expired"
}

func writeListText(w io.Writer, values []lease.ClaimView) error {
	if len(values) == 0 {
		return writeLines(w, "list", []string{"claims: none"})
	}
	rows := make([][4]string, len(values))
	for i, value := range values {
		resources := make([]string, len(value.Resources))
		for j, resource := range value.Resources {
			resources[j] = shortenOpaque(resource, 30)
		}
		rows[i] = [4]string{strings.Join(resources, ","), shortenOpaque(value.ClaimID, 32), shortenOpaque(value.AgentID, 24), stateForClaim(value)}
	}
	widths := [4]int{displayWidth("resources"), displayWidth("claim"), displayWidth("agent"), displayWidth("state")}
	for _, row := range rows {
		for i, cell := range row {
			if displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}
	lines := []string{padCells("resources", widths[0]) + "  " + padCells("claim", widths[1]) + "  " + padCells("agent", widths[2]) + "  state"}
	for _, row := range rows {
		lines = append(lines, padCells(row[0], widths[0])+"  "+padCells(row[1], widths[1])+"  "+padCells(row[2], widths[2])+"  "+row[3])
	}
	return writeLines(w, "list", lines)
}

func writeHistoryText(w io.Writer, page ledger.HistoryPage) error {
	lines := []string{"resource: " + shortenOpaque(page.Resource, 48), fmt.Sprintf("epochs: %d", len(page.Epochs)), "prunedThroughSequence: " + page.Coverage.PrunedThroughSequence}
	for _, epoch := range page.Epochs {
		lines = append(lines, fmt.Sprintf("%s  %s  %s", shortenOpaque(epoch.ClaimID, 32), epoch.Status, epoch.AcquiredAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00")))
	}
	return writeLines(w, "history", lines)
}
func padCells(value string, width int) string {
	return value + strings.Repeat(" ", width-displayWidth(value))
}

func writeEventsText(w io.Writer, page ledger.EventsPage) error {
	lines := []string{fmt.Sprintf("events: %d", len(page.Events)), "gap: " + fmt.Sprint(page.Gap), "nextCursor: " + page.NextCursor}
	for _, event := range page.Events {
		lines = append(lines, fmt.Sprintf("%s  %s  %s", event.Sequence, event.Kind, shortenOpaque(event.ClaimID, 32)))
	}
	return writeLines(w, "events", lines)
}
func writeLines(w io.Writer, title string, lines []string) error {
	if _, err := fmt.Fprintln(w, title); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
func sortedStrings(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
