package queueui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// TestQueueTerminalOutputLatency measures keyboard input through Bubble Tea's
// renderer to PTY output. It deliberately does not claim physical screen paint.
// Opt in because it starts a real renderer and builds a large fixture.
func TestQueueTerminalOutputLatency(t *testing.T) {
	count, err := strconv.Atoi(os.Getenv("QUEUE_TERMINAL_SAMPLES"))
	if err != nil || count < 1 {
		t.Skip("set QUEUE_TERMINAL_SAMPLES to opt in")
	}
	for _, size := range []int{10000, 50000, 100000} {
		t.Run(fmt.Sprintf("items-%d", size), func(t *testing.T) {
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
			snapshot := benchSnapshot(size)
			for index, title := range []string{"ROWZERO", "ROWONE"} {
				key := queue.Ref{SourceID: "source-0", ItemID: fmt.Sprintf("%06d", index*5)}.Key()
				item := snapshot.Items[key]
				item.Title = title
				snapshot.Items[key] = item
			}
			m := New(snapshot)
			m.Sources = []queue.Source{{ID: "source-0"}, {ID: "source-1"}, {ID: "source-2"}, {ID: "source-3"}, {ID: "source-4"}}
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
			durations := make([]time.Duration, 0, count)
			for i := 0; i < count; i++ {
				key, row := "j", "> 000005  ROWONE"
				if i%2 == 1 {
					key, row = "k", "> 000000  ROWZERO"
				}
				start := time.Now()
				if _, err := master.Write([]byte(key)); err != nil {
					t.Fatal(err)
				}
				awaitTerminalRow(t, chunks, row)
				durations = append(durations, time.Since(start))
			}
			if count%2 != 0 {
				if _, err := master.Write([]byte("k")); err != nil {
					t.Fatal(err)
				}
				awaitTerminalRow(t, chunks, "> 000000  ROWZERO")
			}
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			quantile := func(percent int) time.Duration {
				index := (percent*len(durations)+99)/100 - 1
				return durations[index]
			}
			t.Logf("PTY input-to-output n=%d p50=%s p95=%s p99=%s (excludes terminal display paint)", count, quantile(50), quantile(95), quantile(99))
			// Publish a worker-prepared full snapshot immediately before each
			// keypress. The expected marker belongs to the new snapshot, so a
			// keypress rendered against old rows cannot satisfy the observation.
			refreshDurations := make([]time.Duration, 0, count)
			for i := 0; i < count; i++ {
				snapshot.Revision++
				key, row := "j", fmt.Sprintf("> 000005  NEWONE%04d", i)
				if i%2 == 1 {
					key, row = "k", fmt.Sprintf("> 000000  NEWZERO%04d", i)
				}
				for index, title := range []string{fmt.Sprintf("NEWZERO%04d", i), fmt.Sprintf("NEWONE%04d", i)} {
					ref := queue.Ref{SourceID: "source-0", ItemID: fmt.Sprintf("%06d", index*5)}.Key()
					item := snapshot.Items[ref]
					item.Title = title
					snapshot.Items[ref] = item
				}
				message := PrepareSnapshot(snapshot, m.Sources...)
				start := time.Now()
				program.Send(message)
				if _, err := master.Write([]byte(key)); err != nil {
					t.Fatal(err)
				}
				awaitTerminalRow(t, chunks, row)
				refreshDurations = append(refreshDurations, time.Since(start))
			}
			sort.Slice(refreshDurations, func(i, j int) bool { return refreshDurations[i] < refreshDurations[j] })
			durations = refreshDurations
			t.Logf("PTY prepared-refresh-to-key-output n=%d p50=%s p95=%s p99=%s (worker preparation and terminal display paint excluded)", count, quantile(50), quantile(95), quantile(99))
		})
	}
}

func awaitTerminalRow(t *testing.T, chunks <-chan []byte, row string) {
	t.Helper()
	var output strings.Builder
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatalf("PTY closed waiting for %q (last output: %q)", row, output.String())
			}
			output.Write(chunk)
			if strings.Contains(output.String(), row) {
				return
			}
		case <-deadline.C:
			t.Fatalf("timeout waiting for %q (last output: %q)", row, output.String())
		}
	}
}
