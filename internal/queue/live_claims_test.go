package queue

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/server"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/watch"
)

type liveAuthorityFake struct {
	calls                     []string
	resource                  string
	state                     string
	expires                   time.Time
	watchCount                int
	atHead, atStatus, atWatch func()
	cancel                    context.CancelFunc
	gap                       bool
	identityChanged           bool
	failStatusOnce            bool
	failWatchOnce             bool
	expireWithoutEvent        bool
	replayEvent               bool
}

func (f *liveAuthorityFake) Events(_ context.Context, cursor string, limit int) (ledger.EventsPage, error) {
	f.calls = append(f.calls, "events")
	if cursor != "" || limit != 1 {
		panic("unexpected events read")
	}
	if f.atHead != nil {
		f.atHead()
	}
	return ledger.EventsPage{AuthorityID: "authority", NextCursor: "head"}, nil
}
func (f *liveAuthorityFake) Status(_ context.Context, selector lease.Selector) (lease.Status, error) {
	f.calls = append(f.calls, "status")
	if f.failStatusOnce {
		f.failStatusOnce = false
		return lease.Status{}, errors.New("temporary status outage")
	}
	if f.atStatus != nil {
		f.atStatus()
	}
	result := lease.Status{}
	for _, resource := range selector.Resources {
		state := lease.ResourceStatus{Resource: resource, State: f.state}
		if f.state == "active" || f.state == "expired" {
			state.Claim = &lease.ClaimView{AuthorityID: "authority", Active: f.state == "active", AgentID: "worker", ExpiresAt: f.expires}
		}
		result.Resources = append(result.Resources, state)
	}
	return result, nil
}
func (f *liveAuthorityFake) Watch(_ context.Context, request watch.Request) (watch.Result, error) {
	f.calls = append(f.calls, "watch")
	f.watchCount++
	if f.atWatch != nil {
		f.atWatch()
	}
	if f.failWatchOnce {
		f.failWatchOnce = false
		return watch.Result{}, errors.New("temporary watch outage")
	}
	if f.expireWithoutEvent && f.watchCount == 1 {
		f.state = "expired"
		return watch.Result{AuthorityID: "authority", NextCursor: request.Cursor, TimedOut: true}, nil
	}
	if request.Cursor != "head" || len(request.Resources) != 0 || request.Until != "" {
		panic("filtered or non-cursor watch")
	}
	if f.identityChanged {
		return watch.Result{}, reason.New(reason.ReasonAuthorityRestored, "authority restored")
	}
	if f.gap && f.watchCount == 1 {
		return watch.Result{AuthorityID: "authority", Gap: true}, nil
	}
	if f.watchCount == 1 || f.replayEvent && f.watchCount == 2 {
		return watch.Result{AuthorityID: "authority", NextCursor: "head", Event: &ledger.Event{Resources: []string{f.resource}}}, nil
	}
	if f.cancel != nil {
		f.cancel()
	}
	return watch.Result{}, context.Canceled
}

func TestRunClaimOverlayCursorSnapshotWatchRaces(t *testing.T) {
	for _, action := range []string{"acquire", "renew", "release", "expiry"} {
		for _, phase := range []string{"head", "status", "watch"} {
			t.Run(action+"/"+phase, func(t *testing.T) {
				items, sources := claimFixtures(1)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f := &liveAuthorityFake{state: "free", cancel: cancel, expires: time.Now().Add(time.Hour)}
				selected := ClaimAuthority{API: f, LiveAPI: f, ID: "authority", Now: func() (time.Time, error) { return time.Now(), nil }}
				mutate := func() {
					switch action {
					case "acquire", "renew":
						f.state = "active"
					case "release":
						f.state = "free"
					case "expiry":
						f.state = "expired"
					}
				}
				if action == "release" || action == "renew" || action == "expiry" {
					f.state = "active"
				}
				switch phase {
				case "head":
					f.atHead = mutate
				case "status":
					f.atStatus = mutate
				case "watch":
					f.atWatch = mutate
				}
				var last ClaimObservation
				_ = RunClaimOverlay(ctx, items, sources, selected, config.ProfilePaths{}, nil, func(observed []Item, _ bool, _ error) {
					if len(observed) != 1 {
						t.Fatal("projection lost item")
					}
					last = observed[0].Claim
					if f.resource == "" && len(observed[0].Resources) > 0 {
						f.resource = observed[0].Resources[0]
					}
				})
				if !slices.Equal(f.calls[:3], []string{"events", "status", "watch"}) || f.watchCount != 2 {
					t.Fatalf("calls: %v", f.calls)
				}
				want := "held"
				if f.state == "free" {
					want = "free"
				}
				if f.state == "expired" {
					want = "expired"
				}
				if !last.Known || last.State != want {
					t.Fatalf("projection %s != %s", last.State, want)
				}
			})
		}
	}
}

func TestRunClaimOverlayLocalAuthorityReplaysAcquireBetweenSnapshotAndWatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{})
	api, err := authority.NewLocalAuthority(svc, st, watch.MinPoll)
	if err != nil {
		t.Fatal(err)
	}
	items, sources := claimFixtures(1)
	selected := ClaimAuthority{API: api, LiveAPI: api, ID: st.AuthorityID(), Now: func() (time.Time, error) { return svc.AuthorityNow(), nil }}
	var seen []string
	err = RunClaimOverlay(ctx, items, sources, selected, config.ProfilePaths{}, nil, func(observed []Item, _ bool, callErr error) {
		if callErr != nil {
			t.Errorf("overlay: %v", callErr)
			return
		}
		item := observed[0]
		seen = append(seen, item.Claim.State)
		if len(seen) == 1 {
			_, acquireErr := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), Resources: item.Resources, AgentID: "worker", SessionID: "session", ClaimID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Token: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
			if acquireErr != nil {
				t.Errorf("acquire: %v", acquireErr)
				cancel()
			}
		} else {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || !slices.Equal(seen, []string{"free", "held"}) {
		t.Fatalf("states=%v err=%v", seen, err)
	}
}

// Exercise the pinned HTTP profile across a real hosted-store restore and server
// restart. Rebuilding a snapshot must not silently accept the new restore ID.
func TestRunClaimOverlayRemoteRestoreAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	home := t.TempDir()
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, home, store.Options{HostedWriter: true, HostedLock: lock})
	if err != nil {
		t.Fatal(err)
	}
	invite := strings.Repeat("a", 64)
	bootstrap, err := lease.New(st, nil, nil, lease.Defaults{}).HostedInitialize(ctx, invite)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteHostedReady(lock); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := server.Config{Home: home, Listen: "127.0.0.1:8443", Prefixes: []string{"coordination:"}, MaxTTL: "1m", MaxHold: "1h", HealthRate: 100, MetadataRate: 100, EnrollmentRate: 100}
	current, err := server.New(ctx, cfg, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	var hosted atomic.Pointer[server.Server]
	hosted.Store(current)
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosted.Load().Handler().ServeHTTP(w, r)
	}))
	defer listener.Close()
	defer func() { _ = hosted.Load().Close() }()

	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	paths := config.UserProfilePaths(os.Getenv)
	profile := config.Profile{Name: "team", Endpoint: listener.URL, AuthorityID: bootstrap.AuthorityID, RestoreID: bootstrap.RestoreID, AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(configRoot, "worklease", "credentials", "team")}}
	client, err := authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(configRoot, "pending")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Enroll(ctx, invite, "overlay-test", paths); err != nil {
		t.Fatal(err)
	}
	remote, err := authority.NewRemoteAuthority(client)
	if err != nil {
		t.Fatal(err)
	}
	items, sources := claimFixtures(1)
	prefixes := []string{"coordination:"}
	selected := ClaimAuthority{API: remote, LiveAPI: remote, ID: bootstrap.AuthorityID, Profile: "team", Remote: true, AdmittedPrefixes: &prefixes}
	var baseline, stale bool
	err = RunClaimOverlay(ctx, items, sources, selected, paths, os.Getenv, func(observed []Item, rebuilding bool, callErr error) {
		if !baseline && callErr == nil && !rebuilding && observed[0].Claim.Known {
			baseline = true
			if closeErr := current.Close(); closeErr != nil {
				t.Errorf("close old server: %v", closeErr)
				cancel()
				return
			}
			lock, openErr := store.AcquireHostedLock(ctx, home)
			if openErr != nil {
				t.Errorf("lock restored store: %v", openErr)
				cancel()
				return
			}
			restored, openErr := store.Open(ctx, home, store.Options{HostedWriter: true, HostedLock: lock})
			if openErr == nil {
				_, openErr = lease.New(restored, nil, nil, lease.Defaults{}).HostedRestore(ctx, lease.HostedRestoreRequest{}, strings.Repeat("b", 64))
				_ = restored.Close()
			}
			if openErr != nil {
				t.Errorf("restore store: %v", openErr)
				cancel()
				return
			}
			restarted, restartErr := server.New(ctx, cfg, true, nil)
			if restartErr != nil {
				t.Errorf("restart server: %v", restartErr)
				cancel()
				return
			}
			hosted.Store(restarted)
		}
		if baseline && callErr != nil && observed[0].Claim.Stale && !observed[0].Claim.Known {
			stale = true
		}
	})
	if !baseline || !stale || reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAuthorityRestored {
		t.Fatalf("baseline=%t stale=%t err=%v", baseline, stale, err)
	}
}

func TestRunClaimOverlayRetriesFailedEventStatusWithoutAnotherEvent(t *testing.T) {
	items, sources := claimFixtures(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &liveAuthorityFake{state: "free", replayEvent: true, cancel: cancel, expires: time.Now().Add(time.Hour)}
	f.atWatch = func() {
		if f.watchCount == 1 {
			f.state = "active"
			f.failStatusOnce = true
		}
	}
	var states []string
	err := RunClaimOverlay(ctx, items, sources, ClaimAuthority{API: f, LiveAPI: f, ID: "authority", Now: func() (time.Time, error) { return time.Now(), nil }}, config.ProfilePaths{}, nil, func(observed []Item, _ bool, _ error) {
		states = append(states, observed[0].Claim.State)
		if observed[0].Claim.State == "held" {
			cancel()
		}
		if f.resource == "" {
			f.resource = observed[0].Resources[0]
		}
	})
	if !errors.Is(err, context.Canceled) || !slices.Equal(states, []string{"free", "unknown", "held"}) {
		t.Fatalf("states=%v calls=%v err=%v", states, f.calls, err)
	}
}

func TestRunClaimOverlayRetriesTransientWatchFailure(t *testing.T) {
	items, sources := claimFixtures(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &liveAuthorityFake{state: "free", failWatchOnce: true, cancel: cancel}
	var stale, recovered bool
	err := RunClaimOverlay(ctx, items, sources, ClaimAuthority{API: f, LiveAPI: f, ID: "authority"}, config.ProfilePaths{}, nil, func(observed []Item, _ bool, callErr error) {
		if callErr != nil && observed[0].Claim.Stale {
			stale = true
		}
		if stale && observed[0].Claim.Known {
			recovered = true
			cancel()
		}
		if f.resource == "" {
			f.resource = observed[0].Resources[0]
		}
	})
	if !errors.Is(err, context.Canceled) || !stale || !recovered {
		t.Fatalf("stale=%t recovered=%t calls=%v err=%v", stale, recovered, f.calls, err)
	}
}

func TestRunClaimOverlayExpiresWithoutEventAtAuthorityTime(t *testing.T) {
	items, sources := claimFixtures(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Unix(100, 0)
	f := &liveAuthorityFake{state: "active", expires: now.Add(time.Second), expireWithoutEvent: true, cancel: cancel}
	f.atWatch = func() { now = f.expires }
	var states []string
	err := RunClaimOverlay(ctx, items, sources, ClaimAuthority{API: f, LiveAPI: f, ID: "authority", Now: func() (time.Time, error) { return now, nil }}, config.ProfilePaths{}, nil, func(observed []Item, _ bool, _ error) {
		states = append(states, observed[0].Claim.State)
		if observed[0].Claim.State == "expired" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || !slices.Equal(states, []string{"held", "expired"}) {
		t.Fatalf("states=%v calls=%v err=%v", states, f.calls, err)
	}
}

func TestHistoryGapInvalidatesObservedClaim(t *testing.T) {
	items, _ := claimFixtures(1)
	prior := time.Unix(100, 0)
	items[0].Claim = ClaimObservation{Known: true, Active: true, State: "held", ObservedAt: prior}
	observed := unknownClaims(items, "authority", "history-gap")
	if observed[0].Claim.Known || observed[0].Claim.Active || !observed[0].Claim.Stale || !observed[0].Claim.ObservedAt.Equal(prior) {
		t.Fatalf("gap must invalidate prior observation without ordering it behind the claim: %+v", observed[0].Claim)
	}
}

func TestRunClaimOverlayGapAndRestoreFailClosed(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "gap", true: "restore"}[restored], func(t *testing.T) {
			items, sources := claimFixtures(1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &liveAuthorityFake{state: "free", cancel: cancel, gap: !restored, identityChanged: restored}
			selected := ClaimAuthority{API: f, LiveAPI: f, ID: "authority", Now: func() (time.Time, error) { return time.Now(), nil }}
			var rebuild, stale bool
			_ = RunClaimOverlay(ctx, items, sources, selected, config.ProfilePaths{}, nil, func(observed []Item, rebuilding bool, err error) {
				if rebuilding {
					rebuild = true
					stale = observed[0].Claim.Stale && !observed[0].Claim.Known
				}
				if restored && err != nil {
					stale = observed[0].Claim.Stale && !observed[0].Claim.Known
				}
			})
			if !stale || rebuild == restored {
				t.Fatalf("rebuild=%t stale=%t", rebuild, stale)
			}
		})
	}
}
