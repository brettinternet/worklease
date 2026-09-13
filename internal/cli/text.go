package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/resource"
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
	return r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe10 && r <= 0xfe19 || r >= 0xfe30 && r <= 0xfe6f || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6 || r >= 0x1f300 && r <= 0x1faff || r >= 0x20000 && r <= 0x3fffd)
}

func shortenOpaque(value string, maxCells int) string {
	value = output.RedactString(value)
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

func writeStatusText(w io.Writer, value lease.Status, color bool) error {
	if value.Claim != nil {
		state := stateForClaim(*value.Claim)
		return writeLines(w, "status", []string{
			"claim: " + shortenOpaque(value.Claim.ClaimID, 40),
			"state: " + styledState(state, color),
			"agent: " + shortenOpaque(value.Claim.AgentID, 40),
			"expiresAt: " + value.Claim.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		})
	}
	parts := make([]string, 0, len(value.Resources))
	for _, item := range value.Resources {
		parts = append(parts, shortenOpaque(item.Resource, 48)+"="+styledState(item.State, color))
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

func styledState(state string, color bool) string {
	code := output.Yellow
	if state == "active" || state == "free" || state == "ok" {
		code = output.Green
	} else if state == "fail" || state == "error" {
		code = output.Red
	}
	return output.Style(color, code, state)
}

func writeReceiptText(w io.Writer, receipt lease.Receipt) error {
	action := receipt.Kind
	lines := []string{fmt.Sprintf("revision: %d", receipt.Revision)}
	switch receipt.Kind {
	case "heartbeat":
		action = "renewed"
		lines = appendResultLine(lines, receipt.Result, "expiresAt")
	case "checkpoint":
		action = "checkpointed"
		lines = appendResultLine(lines, receipt.Result, "expiresAt")
	case "release":
		action = "released"
		lines = appendResultLine(lines, receipt.Result, "reason")
	}
	if receipt.Idempotent {
		lines = append(lines, "replayed: true")
	}
	return writeLines(w, action+" claim "+shortenOpaque(receipt.ClaimID, 24), lines)
}

func writeAcquireText(w io.Writer, fields map[string]any) error {
	claimID, _ := fields["claimId"].(string)
	resources, _ := fields["resources"].([]string)
	resourceLabel := "resources"
	if len(resources) == 1 {
		resourceLabel = "resource"
	}
	lines := []string{"resources: " + summarizeResources(resources)}
	lines = appendFieldLine(lines, fields, "agentId", "agentId")
	lines = appendFieldLine(lines, fields, "sessionId", "sessionId")
	lines = appendFieldLine(lines, fields, "revision", "revision")
	lines = appendFieldLine(lines, fields, "expiresAt", "expiresAt")
	lines = appendFieldLine(lines, fields, "guarantee", "guarantee")
	if receipt, ok := fields["receipt"].(lease.Receipt); ok && receipt.Idempotent {
		lines = append(lines, "replayed: true")
	}
	if values, ok := fields["unknownOperations"].([]string); ok && len(values) > 0 {
		lines = append(lines, "unknownOperations: "+textValue(output.Redact(values)))
	}
	if values, ok := fields["recovery"].([]lease.Recovery); ok && len(values) > 0 {
		lines = append(lines, fmt.Sprintf("recovery: %d", len(values)))
		for i, value := range values {
			lines = append(lines, fmt.Sprintf("recovery[%d]: resource=%s claim=%s checkpointPresent=%t", i+1, resourceText(value.Resource), value.ClaimID, value.CheckpointPresent))
		}
	}
	return writeLines(w, fmt.Sprintf("acquired %d %s as claim %s", len(resources), resourceLabel, shortenOpaque(claimID, 24)), lines)
}

func writeTransferText(w io.Writer, fields map[string]any) error {
	claimID, _ := fields["claimId"].(string)
	lines := []string{}
	lines = appendFieldLine(lines, fields, "agentId", "agent")
	lines = appendFieldLine(lines, fields, "sessionId", "session")
	lines = appendFieldLine(lines, fields, "revision", "revision")
	lines = appendFieldLine(lines, fields, "expiresAt", "expiresAt")
	lines = appendFieldLine(lines, fields, "guarantee", "guarantee")
	return writeLines(w, "transferred ownership to claim "+shortenOpaque(claimID, 24), lines)
}

func writeVerificationText(w io.Writer, claim *lease.ClaimView, unknown []string) error {
	lines := []string{
		"resources: " + summarizeResources(claim.Resources),
		fmt.Sprintf("revision: %d", claim.Revision),
		"expiresAt: " + claim.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
	}
	if len(unknown) > 0 {
		lines = append(lines, fmt.Sprintf("unknownOperations: %d", len(unknown)))
	}
	return writeLines(w, "verified claim "+shortenOpaque(claim.ClaimID, 24), lines)
}

func writeExecText(w io.Writer, receipt lease.Receipt, exitCode int) error {
	result, _ := output.Redact(receipt.Result).(map[string]any)
	lines := []string{fmt.Sprintf("exitCode: %d", exitCode), fmt.Sprintf("revision: %d", receipt.Revision)}
	for _, key := range []string{"argv", "executionDirectory", "stdout", "stderr", "stdoutBytes", "stderrBytes", "stdoutTruncated", "stderrTruncated", "guarantee"} {
		lines = appendResultLine(lines, result, key)
	}
	if receipt.Idempotent {
		lines = append(lines, "replayed: true")
	}
	action := "completed command for claim "
	if exitCode != 0 {
		action = "command failed for claim "
	}
	return writeLines(w, action+shortenOpaque(receipt.ClaimID, 24), lines)
}

func writeReplaceText(w io.Writer, receipt lease.Receipt) error {
	result, _ := output.Redact(receipt.Result).(map[string]any)
	lines := []string{fmt.Sprintf("revision: %d", receipt.Revision)}
	for _, key := range []string{"path", "previousSha256", "sha256", "contentBytes", "mutationProtection", "providerMutationFenced"} {
		lines = appendResultLine(lines, result, key)
	}
	if receipt.Idempotent {
		lines = append(lines, "replayed: true")
	}
	return writeLines(w, "replaced file for claim "+shortenOpaque(receipt.ClaimID, 24), lines)
}

func writeInspectionText(w io.Writer, operation ledger.Operation) error {
	lines := []string{
		"claim: " + shortenOpaque(operation.ClaimID, 32),
		"kind: " + operation.Kind,
		"state: " + operation.State,
		"startedAt: " + operation.StartedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
	}
	if operation.CompletedAt != nil {
		lines = append(lines, "completedAt: "+operation.CompletedAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"))
	}
	if operation.RequestSHA256 != "" {
		lines = append(lines, "requestSha256: "+operation.RequestSHA256)
	}
	if operation.RequestNotAfter != nil {
		lines = append(lines, "requestNotAfter: "+operation.RequestNotAfter.UTC().Format("2006-01-02T15:04:05.000000Z07:00"))
	}
	if operation.ExpectedRevision != 0 {
		lines = append(lines, fmt.Sprintf("expectedRevision: %d", operation.ExpectedRevision))
	}
	if operation.Outcome != "" {
		lines = append(lines, "outcome: "+operation.Outcome)
	}
	if len(operation.Receipt) > 0 {
		lines = append(lines, "receipt: "+inlineJSON(output.Redact(operation.Receipt)))
	}
	if operation.Evidence != nil {
		lines = append(lines, "evidence: "+inlineJSON(output.Redact(operation.Evidence)))
	}
	return writeLines(w, "operation "+shortenOpaque(operation.OperationID, 24)+" is "+operation.State, lines)
}

func writeReconciliationText(w io.Writer, receipt lease.ReconciliationReceipt) error {
	lines := []string{
		"targetClaim: " + shortenOpaque(receipt.TargetClaimID, 32),
		"targetOperation: " + shortenOpaque(receipt.TargetOperationID, 32),
		"outcome: " + receipt.Outcome,
		fmt.Sprintf("revision: %d", receipt.Revision),
		"reconciledAt: " + receipt.ReconciledAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
		"expiresAt: " + receipt.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
	}
	if receipt.Idempotent {
		lines = append(lines, "replayed: true")
	}
	return writeLines(w, "reconciled operation "+shortenOpaque(receipt.TargetOperationID, 24), lines)
}

func appendResultLine(lines []string, values map[string]any, key string) []string {
	return appendFieldLine(lines, values, key, key)
}

func appendFieldLine(lines []string, values map[string]any, key, label string) []string {
	if _, ok := values[key]; !ok {
		return lines
	}
	safe, _ := output.Redact(map[string]any{key: values[key]}).(map[string]any)
	return append(lines, label+": "+textValue(safe[key]))
}

func textValue(value any) string {
	switch typed := value.(type) {
	case string:
		return escapeTerminalCell(typed)
	case time.Time:
		return typed.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
	case map[string]any, []any, []string:
		return inlineJSON(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func inlineJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[unavailable]"
	}
	return string(encoded)
}

func summarizeResources(resources []string) string {
	if len(resources) == 0 {
		return "none"
	}
	values := make([]string, len(resources))
	for i, resource := range resources {
		values[i] = summarizeResource(resource)
	}
	return strings.Join(values, ", ")
}

func writePolicyListText(w io.Writer, values []resource.Descriptor, full, color bool) error {
	headers := []string{"PROVIDER", "SCOPE", "CAPABILITY", "IDENTITY"}
	if full {
		headers = append(headers, "LOCAL_REPLACE", "PROVIDER_FENCING")
	}
	rows := make([][]string, 0, len(values))
	for _, value := range values {
		row := []string{value.Name, value.Scope, value.Capability, value.IdentityScope}
		if full {
			row = append(row, fmt.Sprint(value.LocalReplaceAllowed), fmt.Sprint(value.ProviderFencing))
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = displayWidth(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}
	if _, err := fmt.Fprintln(w, renderTableRow(headers, widths, color, true)); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(w, renderTableRow(row, widths, color, false)); err != nil {
			return err
		}
	}
	return nil
}

func writeListText(w io.Writer, values []lease.ClaimView, full, color bool) error {
	return writeListTextAt(w, values, full, color, time.Now())
}

func writeListTextAt(w io.Writer, values []lease.ClaimView, full, color bool, now time.Time) error {
	headers := []string{"STATE", "RESOURCE", "LEASE"}
	rows := make([][]string, 0, len(values))
	if full {
		headers = []string{"STATE", "RESOURCES", "CLAIM_ID", "AGENT_ID", "EXPIRES_AT"}
	}
	for _, value := range values {
		resources := make([]string, len(value.Resources))
		for i, resource := range value.Resources {
			if full {
				resources[i] = resourceText(resource)
			} else {
				resources[i] = summarizeResource(resource)
			}
		}
		state := stateForClaim(value)
		if full {
			rows = append(rows, []string{state, strings.Join(resources, ","), output.RedactString(value.ClaimID), output.RedactString(value.AgentID), value.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z07:00")})
		} else {
			rows = append(rows, []string{state, shortenOpaque(strings.Join(resources, ","), 52), compactExpiry(value, now)})
		}
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = displayWidth(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}
	if _, err := fmt.Fprintln(w, renderTableRow(headers, widths, color, true)); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(w, renderTableRow(row, widths, color, false)); err != nil {
			return err
		}
	}
	return nil
}

func renderTableRow(row []string, widths []int, color, header bool) string {
	cells := make([]string, len(row))
	for i, cell := range row {
		if i < len(row)-1 {
			cell = padCells(cell, widths[i])
		}
		if header {
			cell = output.Style(color, output.Bold, cell)
		} else if i == 0 && (row[0] == "active" || row[0] == "expired" || row[0] == "free" || row[0] == "fail" || row[0] == "error") {
			cell = styledState(row[0], color) + strings.TrimPrefix(cell, row[0])
		}
		cells[i] = cell
	}
	return strings.Join(cells, "  ")
}

func resourceText(value string) string {
	redacted, _ := output.Redact(map[string]any{"resource": value}).(map[string]any)
	if safe, ok := redacted["resource"].(string); ok {
		return safe
	}
	return "[REDACTED]"
}

func summarizeResource(value string) string {
	parts := strings.Split(value, ":")
	if len(parts) == 3 && parts[0] == "coordination" && lowerHexDigest(parts[2]) {
		sum := sha256.Sum256([]byte(value))
		return parts[1] + ":#" + hex.EncodeToString(sum[:4])
	}
	if len(parts) == 4 && (parts[0] == "backlog-md" || parts[0] == "markdown") {
		common, commonErr := url.PathUnescape(parts[1])
		locator, locatorErr := url.PathUnescape(parts[2])
		item, itemErr := url.PathUnescape(parts[3])
		if commonErr == nil && locatorErr == nil && itemErr == nil {
			if windowsPath(common) {
				common = strings.ReplaceAll(common, `\`, "/")
			}
			repository := path.Base(path.Dir(common))
			if path.Base(common) != ".git" {
				repository = path.Base(common)
			}
			if parts[0] == "markdown" {
				item = locator
			}
			return shortenOpaque(strings.Join([]string{parts[0], escapeTerminalCell(repository), escapeTerminalCell(item)}, ":"), 52)
		}
	}
	return shortenOpaque(resourceText(value), 52)
}

func windowsPath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/') || strings.HasPrefix(value, `\\`)
}

func escapeTerminalCell(value string) string {
	var escaped strings.Builder
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			fmt.Fprintf(&escaped, `\u%04x`, r)
		} else {
			escaped.WriteRune(r)
		}
	}
	return escaped.String()
}

func lowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range []byte(value) {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func compactExpiry(value lease.ClaimView, now time.Time) string {
	delta := value.ExpiresAt.Sub(now)
	if delta < 0 {
		return relativeDuration(-delta) + " ago"
	}
	if !value.Active {
		return "expired"
	}
	return relativeDuration(delta) + " left"
}

func relativeDuration(value time.Duration) string {
	seconds := int(value / time.Second)
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	days := hours / 24
	if remainder := hours % 24; remainder != 0 {
		return fmt.Sprintf("%dd %dh", days, remainder)
	}
	return fmt.Sprintf("%dd", days)
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
