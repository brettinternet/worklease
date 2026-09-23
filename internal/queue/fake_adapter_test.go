package queue

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// fakeAdapter exercises pagination, per-operation failures, delays, capability denials, and typed dependency edges.
type fakeAdapter struct {
	mu       sync.Mutex
	pages    map[string][]SummaryPage
	errors   map[string]error
	delays   map[string]time.Duration
	denied   map[string]bool
	outcomes map[string][]ItemOutcome
	edges    map[string][]DependencyPage
}

func newFake() *fakeAdapter {
	return &fakeAdapter{pages: map[string][]SummaryPage{}, errors: map[string]error{}, delays: map[string]time.Duration{}, denied: map[string]bool{}, outcomes: map[string][]ItemOutcome{}, edges: map[string][]DependencyPage{}}
}
func (f *fakeAdapter) pause(ctx context.Context, key string) error {
	if d := f.delays[key]; d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (f *fakeAdapter) Resolve(ctx context.Context, args map[string]string) (Source, error) {
	if err := ctx.Err(); err != nil {
		return Source{}, err
	}
	return Source{ID: args["id"], Adapter: "fake"}, nil
}
func (f *fakeAdapter) Capabilities(ctx context.Context, s Source, principal string, ref *Ref) (CapabilitySet, error) {
	if err := f.pause(ctx, s.ID); err != nil {
		return nil, err
	}
	if f.denied[s.ID] {
		return CapabilitySet{"list": {Support: Supported, Permission: Denied, Availability: Available, Reason: "test-denied"}}, nil
	}
	return CapabilitySet{"list": {Support: Supported, Permission: Allowed, Availability: Available}}, nil
}
func (f *fakeAdapter) List(ctx context.Context, s Source, q Query, cursor string) (SummaryPage, error) {
	if err := f.pause(ctx, s.ID); err != nil {
		return SummaryPage{}, err
	}
	if err := f.errors[s.ID]; err != nil {
		return SummaryPage{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pages := f.pages[s.ID]
	index := 0
	if cursor != "" {
		if len(cursor) > 5 {
			index, _ = strconv.Atoi(cursor[5:])
		}
	}
	if index >= len(pages) {
		return SummaryPage{Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}, nil
	}
	page := pages[index]
	if index+1 < len(pages) {
		page.NextCursor = "page-" + itoa(index+1)
		page.Coverage.State = CoveragePartial
	}
	return page, nil
}
func (f *fakeAdapter) ReadItems(ctx context.Context, s Source, refs []Ref, fields []string, budget int) []ItemOutcome {
	if f.pause(ctx, s.ID) != nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ItemOutcome, 0, len(refs))
	for _, r := range refs {
		found := false
		for _, o := range f.outcomes[r.Key()] {
			out = append(out, o)
			found = true
		}
		if !found {
			out = append(out, ItemOutcome{Ref: r, Kind: "missing"})
		}
	}
	return out
}
func (f *fakeAdapter) ReadDependencies(ctx context.Context, s Source, r Ref, cursor string, budget int) (DependencyPage, error) {
	if err := f.pause(ctx, s.ID); err != nil {
		return DependencyPage{}, err
	}
	if err := f.errors["dependencies:"+r.Key()]; err != nil {
		return DependencyPage{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pages := f.edges[r.Key()]
	if len(pages) == 0 {
		return DependencyPage{Completeness: CoverageComplete}, nil
	}
	index := 0
	if cursor != "" {
		if len(cursor) > 5 {
			index, _ = strconv.Atoi(cursor[5:])
		}
	}
	if index >= len(pages) {
		return DependencyPage{Completeness: CoverageComplete}, nil
	}
	return pages[index], nil
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var bytes [20]byte
	i := len(bytes)
	for n > 0 {
		i--
		bytes[i] = byte('0' + n%10)
		n /= 10
	}
	return string(bytes[i:])
}
