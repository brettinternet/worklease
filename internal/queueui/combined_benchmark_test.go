package queueui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// This opt-in fixture measures renderer output and real local claim renewal
// while a source is rate-limited, an adapter read is hung, and a graph is
// recomputed alongside a full snapshot publication. It cannot measure physical
// terminal display paint or remote authority latency.
func TestQueueCombinedFaultOutputLatency(t *testing.T) {
	count, err := strconv.Atoi(os.Getenv("QUEUE_COMBINED_SAMPLES"))
	if err != nil || count < 1 {
		t.Skip("set QUEUE_COMBINED_SAMPLES to opt in")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{})
	const ttl = 10 * time.Minute
	claimID, token := lease.RandomIDs{}.Generate(), strings.Repeat("a", 64)
	grant, err := svc.Acquire(ctx, lease.AcquireRequest{
		AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token,
		Resources: []string{"combined-benchmark"}, AgentID: "fixture", SessionID: "fixture",
		TTL: ttl, RequestNotAfter: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	creds := lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision}

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 35, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	chunks := make(chan []byte, 256)
	go func() {
		var buf [8192]byte
		for {
			n, err := master.Read(buf[:])
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()
	snapshot := benchSnapshot(50000)
	sources := []queue.Source{{ID: "source-0"}, {ID: "source-1"}, {ID: "source-2"}, {ID: "source-3"}, {ID: "source-4"}}
	for index, title := range []string{"ROWZERO", "ROWONE"} {
		ref := queue.Ref{SourceID: "source-0", ItemID: fmt.Sprintf("%06d", index*5)}.Key()
		item := snapshot.Items[ref]
		item.Title = title
		snapshot.Items[ref] = item
	}
	m := New(snapshot)
	m.Sources = sources
	program := tea.NewProgram(m, tea.WithInput(slave), tea.WithOutput(slave), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer func() {
		program.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("renderer: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("renderer did not stop")
		}
	}()
	awaitTerminalRow(t, chunks, "ROWZERO")

	// The stalled read deliberately remains unresolved while other work runs.
	hung := make(chan struct{})
	hungDone := make(chan struct{})
	go func() { <-hung; close(hungDone) }()
	defer func() { close(hung); <-hungDone }()

	inputTimes := make([]time.Duration, 0, count)
	renewTimes := make([]time.Duration, 0, count)
	minimumMargin := ttl
	for i := 0; i < count; i++ {
		// Simulate a provider retry deadline without scheduling another read.
		snapshot.Sources["source-1"] = queue.Coverage{State: queue.CoveragePartial, Reason: "rate-limited", RetryAt: time.Now().Add(time.Minute)}
		// A chain crossing the entire 50k-item snapshot exercises a large
		// prerequisite traversal off the renderer's input path.
		graph := make(map[string]queue.Item, 50000)
		var prior queue.Ref
		for n := 0; n < 50000; n++ {
			ref := queue.Ref{SourceID: "graph", ItemID: fmt.Sprintf("%06d", n)}
			item := queue.Item{Summary: queue.Summary{Ref: ref, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Closure: queue.CoverageComplete}
			if n > 0 {
				item.Dependencies = []queue.Ref{prior}
			}
			graph[ref.Key()] = item
			prior = ref
		}
		graphDone := make(chan struct{})
		go func() { _ = queue.Recompute(graph, queue.CoverageComplete); close(graphDone) }()
		snapshot.Revision++
		selected := "NEWONE"
		key := "j"
		if i%2 == 1 {
			selected, key = "NEWZERO", "k"
		}
		for index, label := range []string{"NEWZERO", "NEWONE"} {
			ref := queue.Ref{SourceID: "source-0", ItemID: fmt.Sprintf("%06d", index*5)}.Key()
			item := snapshot.Items[ref]
			item.Title = fmt.Sprintf("%s%04d", label, i)
			snapshot.Items[ref] = item
		}
		message := PrepareSnapshot(snapshot, sources...)
		type renewal struct {
			revision int64
			elapsed  time.Duration
			err      error
		}
		renewed := make(chan renewal, 1)
		go func(credentials lease.Credentials) {
			start := time.Now()
			receipt, err := svc.Heartbeat(ctx, credentials, lease.Renew{OperationID: lease.RandomIDs{}.Generate(), TTL: ttl, RequestNotAfter: time.Now().Add(time.Hour)})
			renewed <- renewal{receipt.Revision, time.Since(start), err}
		}(creds)
		start := time.Now()
		program.Send(message)
		if _, err := master.Write([]byte(key)); err != nil {
			t.Fatal(err)
		}
		awaitTerminalRow(t, chunks, fmt.Sprintf("> 0000… %s%04d", selected, i))
		inputTimes = append(inputTimes, time.Since(start))
		result := <-renewed
		if result.err != nil {
			t.Fatal(result.err)
		}
		creds.Revision = result.revision
		renewTimes = append(renewTimes, result.elapsed)
		remaining := time.Until(grant.ExpiresAt)
		if remaining < minimumMargin {
			minimumMargin = remaining
		}
		status, err := svc.Status(ctx, lease.Selector{ClaimID: claimID})
		if err != nil || status.Claim == nil {
			t.Fatalf("renewal status: %v", err)
		}
		grant.ExpiresAt = status.Claim.ExpiresAt
		select {
		case <-graphDone:
		case <-time.After(30 * time.Second):
			t.Fatal("graph recompute hung")
		}
	}
	quantile := func(values []time.Duration, percent int) time.Duration {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		return values[(percent*len(values)+99)/100-1]
	}
	t.Logf("combined PTY publication+input-to-output n=%d p50=%s p95=%s p99=%s; local renewal p50=%s p95=%s p99=%s, minimum previous-lease margin=%s (no physical paint or remote authority)", count,
		quantile(inputTimes, 50), quantile(inputTimes, 95), quantile(inputTimes, 99),
		quantile(renewTimes, 50), quantile(renewTimes, 95), quantile(renewTimes, 99), minimumMargin)
}
