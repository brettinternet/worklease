package queueui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// This opt-in fixture measures renderer output and real local claim renewal
// while the GitHub adapter observes a rate limit, another adapter read is
// stalled, and a graph is recomputed alongside a full snapshot publication.
// It cannot measure physical terminal display paint or remote authority latency.
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

	// Exercise the actual HTTP/adapter diagnostic and quota gate while the
	// renderer is live. A separate request stays blocked at the transport until
	// cancellation, rather than merely parking a synthetic goroutine.
	var rateRequests, stalledRequests atomic.Int64
	stalled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(request.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if request.Variables["owner"] == "stalled" {
			stalledRequests.Add(1)
			close(stalled)
			<-r.Context().Done()
			return
		}
		rateRequests.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	binary := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'fixture-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	limited := queue.NewGitHubAdapter()
	limited.Binary, limited.APIBase = binary, server.URL
	rateSource, err := limited.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/rate", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limited.List(ctx, rateSource, queue.Query{}, ""); err == nil {
		t.Fatal("expected a real adapter rate-limit diagnostic")
	}
	stalledAdapter := queue.NewGitHubAdapter()
	stalledAdapter.Binary, stalledAdapter.APIBase = binary, server.URL
	stalledSource, err := stalledAdapter.Resolve(ctx, map[string]string{"host": "github.example.com", "repository": "stalled/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	readCtx, cancelRead := context.WithCancel(ctx)
	readDone := make(chan error, 1)
	go func() { _, err := stalledAdapter.List(readCtx, stalledSource, queue.Query{}, ""); readDone <- err }()
	select {
	case <-stalled:
	case <-time.After(5 * time.Second):
		cancelRead()
		t.Fatal("adapter read did not reach transport")
	}
	defer func() {
		cancelRead()
		select {
		case err := <-readDone:
			if err == nil {
				t.Error("stalled read unexpectedly succeeded")
			}
		case <-time.After(5 * time.Second):
			t.Error("adapter read did not stop on cancellation")
		}
	}()

	// Use the production SQLite index path concurrently with the renderer,
	// graph traversal, provider reads, and renewal. The fixture still excludes
	// the provider-to-index scheduler and any physical display paint.
	indexDir := t.TempDir()
	index, err := queueindex.Open(ctx, indexDir)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	partition := queueindex.Partition{Source: "source-0", Principal: "tester", Scope: "local", Generation: "1"}
	indexed := make([]queue.Item, 0, 10000)
	for _, item := range snapshot.Items {
		if item.Ref.SourceID == partition.Source {
			indexed = append(indexed, item)
		}
	}
	inputTimes := make([]time.Duration, 0, count)
	indexTimes := make([]time.Duration, 0, count)
	renewTimes := make([]time.Duration, 0, count)
	minimumMargin := ttl
	for i := 0; i < count; i++ {
		// The second read is refused by the real adapter's quota gate without
		// another HTTP request; render the observed retry state.
		_, rateErr := limited.List(ctx, rateSource, queue.Query{}, "")
		diagnostic, ok := rateErr.(queue.GitHubRateDiagnostic)
		if !ok || diagnostic.RetryAt.Before(time.Now()) {
			t.Fatalf("quota gate: %v", rateErr)
		}
		snapshot.Sources["source-1"] = queue.Coverage{State: queue.CoveragePartial, Reason: diagnostic.Code, RetryAt: diagnostic.RetryAt}
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
		indexDone := make(chan time.Duration, 1)
		indexErrors := make(chan error, 1)
		go func() {
			started := time.Now()
			indexErrors <- index.Replace(ctx, partition, indexed, true)
			indexDone <- time.Since(started)
		}()
		snapshot.Revision++
		selected, selectedID := "NEWONE", "000005"
		key := "j"
		if i%2 == 1 {
			selected, selectedID, key = "NEWZERO", "000000", "k"
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
		awaitTerminalRow(t, chunks, fmt.Sprintf("> %s  %s%04d", selectedID, selected, i))
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
		select {
		case err := <-indexErrors:
			if err != nil {
				t.Fatal(err)
			}
			indexTimes = append(indexTimes, <-indexDone)
		case <-time.After(30 * time.Second):
			t.Fatal("index replace hung")
		}
	}
	quantile := func(values []time.Duration, percent int) time.Duration {
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		return values[(percent*len(values)+99)/100-1]
	}
	if rateRequests.Load() != 1 || stalledRequests.Load() != 1 {
		t.Fatalf("expected one rate-limited HTTP request and one stalled transport, got %d and %d", rateRequests.Load(), stalledRequests.Load())
	}
	var indexBytes int64
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().IsRegular() {
			indexBytes += info.Size()
		}
	}
	t.Logf("combined PTY publication+input-to-output n=%d p50=%s p95=%s p99=%s; local renewal p50=%s p95=%s p99=%s, minimum previous-lease margin=%s; 10k index replace p50=%s p95=%s p99=%s, disk bytes=%d; GitHub rate HTTP requests=%d, stalled HTTP requests=%d (no physical paint or remote authority)", count,
		quantile(inputTimes, 50), quantile(inputTimes, 95), quantile(inputTimes, 99),
		quantile(renewTimes, 50), quantile(renewTimes, 95), quantile(renewTimes, 99), minimumMargin,
		quantile(indexTimes, 50), quantile(indexTimes, 95), quantile(indexTimes, 99), indexBytes, rateRequests.Load(), stalledRequests.Load())
}
