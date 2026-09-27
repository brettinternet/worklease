package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/runs"
	urfave "github.com/urfave/cli/v3"
)

// runFields is the public projection of a record with its derived health.
func runFields(record runs.Record, now time.Time) map[string]any {
	fields := map[string]any{}
	_ = remarshal(record, &fields)
	fields["health"] = record.Health(now, runs.Alive)
	if path, err := runStore().LogPath(record.ID); err == nil {
		fields["logPath"] = path
	}
	return fields
}

func writeRunResult(s *boundary, cmd *urfave.Command, operation string, record runs.Record) error {
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, operation, map[string]any{"run": runFields(record, time.Now())})
	}
	return writeRunText(s.writer, record, time.Now())
}

func writeRunText(w io.Writer, record runs.Record, now time.Time) error {
	lines := []string{fmt.Sprintf("run %s: %s", record.ID, record.Health(now, runs.Alive))}
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, fmt.Sprintf("  %s: %s", label, value))
		}
	}
	add("name", record.Name)
	add("ref", record.Ref)
	add("argv", fmt.Sprintf("%q", record.Argv))
	add("dir", record.Dir)
	add("started", relativeTime(record.StartedAt, now))
	if record.EndedAt != nil {
		add("ended", relativeTime(*record.EndedAt, now))
	} else if record.LastOutputAt != nil {
		add("lastOutput", relativeTime(*record.LastOutputAt, now))
	}
	add("outcome", runOutcomeText(record))
	if record.Result != nil {
		add("summary", record.Result.Summary)
	}
	if record.Claim != nil {
		claim := fmt.Sprintf("%s on %s", record.Claim.State, strings.Join(record.Claim.Resources, ", "))
		if record.Claim.State == runs.ClaimHeld && record.EndedAt == nil {
			claim += ", expires " + relativeTime(record.Claim.ExpiresAt, now)
		}
		add("claim", claim)
		add("claimDetail", record.Claim.Detail)
	}
	add("error", record.Error)
	if path, err := runStore().LogPath(record.ID); err == nil {
		add("log", path)
	}
	return writeLines(w, lines[0], lines[1:])
}

func runOutcomeText(record runs.Record) string {
	if record.Outcome == "" {
		return ""
	}
	switch {
	case record.Signal != "":
		return record.Outcome + " (" + record.Signal + ")"
	case record.ExitCode != nil:
		return fmt.Sprintf("%s (exit %d)", record.Outcome, *record.ExitCode)
	}
	return record.Outcome
}

func runsCommand(s *boundary) *urfave.Command {
	idArg := func(cmd *urfave.Command) (runs.Record, error) {
		id := cmd.Args().First()
		if cmd.Args().Len() != 1 || !runs.ValidID(id) {
			return runs.Record{}, reason.Invalid("expected one run ID; list runs with worklease runs")
		}
		record, err := runStore().Read(id)
		if errors.Is(err, os.ErrNotExist) {
			return runs.Record{}, reason.New(reason.ReasonOperationNotFound, "run "+id+" not found")
		}
		return record, err
	}
	show := &urfave.Command{
		Name: "show", Usage: "show one run and the tail of its log", UsageText: "worklease runs show ID [--lines N]",
		Flags: []urfave.Flag{&urfave.IntFlag{Name: "lines", Value: 20, Usage: "log tail `N` lines (0-1000)"}},
		Action: func(_ context.Context, cmd *urfave.Command) error {
			record, err := idArg(cmd)
			if err != nil {
				return s.handle(cmd, err)
			}
			lines := cmd.Int("lines")
			if lines < 0 || lines > 1000 {
				return s.handle(cmd, reason.Invalid("lines must be between 0 and 1000"))
			}
			tail := runLogTail(record.ID, lines)
			if s.jsonRequested(cmd) {
				return output.WriteSuccess(s.writer, "runs-show", map[string]any{"run": runFields(record, time.Now()), "logTail": tail})
			}
			if err := writeRunText(s.writer, record, time.Now()); err != nil || tail == "" {
				return err
			}
			_, err = fmt.Fprintf(s.writer, "--- log tail ---\n%s", strings.ToValidUTF8(tail, "\uFFFD"))
			return err
		},
	}
	wait := &urfave.Command{
		Name: "wait", Usage: "wait for a run to finish", UsageText: "worklease runs wait ID [--timeout DURATION]",
		Flags: []urfave.Flag{&urfave.DurationFlag{Name: "timeout", Value: 30 * time.Minute, Usage: "give up after `DURATION`, at most 24h"}},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			record, err := idArg(cmd)
			if err != nil {
				return s.handle(cmd, err)
			}
			timeout := cmd.Duration("timeout")
			if timeout <= 0 || timeout > 24*time.Hour {
				return s.handle(cmd, reason.Invalid("timeout must be between 1ns and 24h"))
			}
			deadline := time.Now().Add(timeout)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				health := record.Health(time.Now(), runs.Alive)
				if record.Terminal() || health == "abandoned" {
					break
				}
				if !time.Now().Before(deadline) {
					return s.handle(cmd, reason.New(reason.ReasonWaitTimeout, "run "+record.ID+" is still "+health).With("runId", record.ID))
				}
				select {
				case <-ctx.Done():
					return s.handle(cmd, reason.New(reason.ReasonInterrupted, "wait interrupted"))
				case <-ticker.C:
				}
				if record, err = runStore().Read(record.ID); err != nil {
					return s.handle(cmd, err)
				}
			}
			if err := writeRunResult(s, cmd, "runs-wait", record); err != nil {
				return err
			}
			return runExitError(record)
		},
	}
	stop := &urfave.Command{
		Name: "stop", Usage: "stop a live run; its supervisor records the outcome and releases the claim", UsageText: "worklease runs stop ID",
		Action: func(_ context.Context, cmd *urfave.Command) error {
			record, err := idArg(cmd)
			if err != nil {
				return s.handle(cmd, err)
			}
			if health := record.Health(time.Now(), runs.Alive); record.Terminal() || health == "abandoned" {
				return s.handle(cmd, reason.Invalid("run "+record.ID+" is "+health+", not live"))
			}
			// A request file, not a signal: the stored PID may have been reused.
			if err := runStore().RequestStop(record.ID); err != nil {
				return s.handle(cmd, reason.New(reason.ReasonStorageFailure, "stop request cannot be written: "+err.Error()))
			}
			if s.jsonRequested(cmd) {
				return output.WriteSuccess(s.writer, "runs-stop", map[string]any{"runId": record.ID, "requested": true})
			}
			_, err = fmt.Fprintf(s.writer, "stop requested for run %s; wait with worklease runs wait %s\n", record.ID, record.ID)
			return err
		},
	}
	ack := &urfave.Command{
		Name: "ack", Usage: "acknowledge finished runs so the default list hides them", UsageText: "worklease runs ack ID... | --finished",
		Flags: []urfave.Flag{&urfave.BoolFlag{Name: "finished", Usage: "acknowledge every finished run"}},
		Action: func(_ context.Context, cmd *urfave.Command) error {
			return s.handle(cmd, ackRuns(s, cmd))
		},
	}
	command := &urfave.Command{
		Name: "runs", Usage: "inspect supervised runs", UsageText: "worklease runs [--all]\nworklease runs show|wait|stop|ack ...",
		Description: "Inspect workers started with worklease run or a queue launch. The default list shows live runs and finished runs that are not yet acknowledged.\n\nExamples:\n  worklease runs\n  worklease runs show ID\n  worklease runs wait ID --timeout 1h\n  worklease runs ack --finished",
		Flags:       []urfave.Flag{&urfave.BoolFlag{Name: "all", Usage: "include acknowledged runs"}},
		Commands:    []*urfave.Command{show, wait, stop, ack},
		Action: func(_ context.Context, cmd *urfave.Command) error {
			if cmd.Args().Len() > 0 {
				return s.handle(cmd, reason.Invalid(fmt.Sprintf("unknown command %q", cmd.Args().First())))
			}
			return s.handle(cmd, listRuns(s, cmd))
		},
	}
	return command
}

func listRuns(s *boundary, cmd *urfave.Command) error {
	records, err := runStore().List()
	if err != nil {
		return err
	}
	now := time.Now()
	shown := make([]runs.Record, 0, len(records))
	for _, record := range records {
		if cmd.Bool("all") || !record.Acknowledged || !record.Terminal() {
			shown = append(shown, record)
		}
	}
	if s.jsonRequested(cmd) {
		values := make([]map[string]any, 0, len(shown))
		for _, record := range shown {
			values = append(values, runFields(record, now))
		}
		return output.WriteSuccess(s.writer, "runs", map[string]any{"runs": values})
	}
	if len(shown) == 0 {
		_, err := fmt.Fprintln(s.writer, "no runs to show")
		return err
	}
	headers := []string{"ID", "STATE", "NAME", "REF", "STARTED", "OUTCOME"}
	rows := make([][]string, 0, len(shown))
	for _, record := range shown {
		rows = append(rows, []string{record.ID, record.Health(now, runs.Alive), shortenOpaque(record.Name, 24), shortenOpaque(record.Ref, 32), relativeTime(record.StartedAt, now), runOutcomeText(record)})
	}
	return writeTable(s.writer, headers, rows, output.ColorEnabled(s.writer))
}

func ackRuns(s *boundary, cmd *urfave.Command) error {
	store := runStore()
	ids := cmd.Args().Slice()
	if cmd.Bool("finished") == (len(ids) > 0) {
		return reason.Invalid("pass run IDs or --finished")
	}
	var targets []runs.Record
	if cmd.Bool("finished") {
		records, err := store.List()
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.Terminal() && !record.Acknowledged {
				targets = append(targets, record)
			}
		}
	}
	for _, id := range ids {
		if !runs.ValidID(id) {
			return reason.Invalid("invalid run ID " + id)
		}
		record, err := store.Read(id)
		if errors.Is(err, os.ErrNotExist) {
			return reason.New(reason.ReasonOperationNotFound, "run "+id+" not found")
		}
		if err != nil {
			return err
		}
		if !record.Terminal() {
			return reason.Invalid("run " + id + " has not finished; stop it first")
		}
		targets = append(targets, record)
	}
	acked := make([]string, 0, len(targets))
	for _, record := range targets {
		record.Acknowledged = true
		if err := store.Save(record); err != nil {
			return err
		}
		acked = append(acked, record.ID)
	}
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, "runs-ack", map[string]any{"acknowledged": acked})
	}
	_, err := fmt.Fprintf(s.writer, "acknowledged %s\n", countText(len(acked), "run"))
	return err
}

// runExitError maps a finished run to the waiter's exit status.
func runExitError(record runs.Record) error {
	switch {
	case record.State == runs.StateLost:
		return &handledError{cause: reason.New(reason.ReasonOwnershipLost, "run "+record.ID+" lost its claim")}
	case record.State == runs.StateFailed:
		return &handledError{cause: reason.New(reason.ReasonInternal, "run "+record.ID+" failed: "+record.Error)}
	case !record.Terminal():
		return &handledError{cause: reason.New(reason.ReasonInterrupted, "run "+record.ID+" was abandoned by its supervisor")}
	case record.ExitCode != nil && *record.ExitCode != 0:
		return &handledError{cause: urfave.Exit("", *record.ExitCode)}
	}
	return nil
}

// runLogTail returns up to lines trailing lines from at most the last 64 KiB.
func runLogTail(id string, lines int) string {
	path, err := runStore().LogPath(id)
	if err != nil || lines == 0 {
		return ""
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	const window = 64 * 1024
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	offset := max(0, st.Size()-window)
	data := make([]byte, st.Size()-offset)
	n, _ := f.ReadAt(data, offset)
	data = data[:n]
	if offset > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	trimmed := bytes.TrimRight(data, "\n")
	parts := bytes.Split(trimmed, []byte("\n"))
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	if len(trimmed) == 0 {
		return ""
	}
	return string(bytes.Join(parts, []byte("\n"))) + "\n"
}
