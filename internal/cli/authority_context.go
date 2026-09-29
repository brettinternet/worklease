package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

type commandAuthority interface {
	authority.Authority
	DefaultTTL() time.Duration
	LocalService() *lease.Service
}

type authorityWithDefaults struct {
	authority.Authority
	ttl   time.Duration
	local *lease.Service
}

func (a authorityWithDefaults) DefaultTTL() time.Duration    { return a.ttl }
func (a authorityWithDefaults) LocalService() *lease.Service { return a.local }
func (a authorityWithDefaults) GuardDispatchAllowed(ttl time.Duration) bool {
	if remote, ok := a.Authority.(interface{ GuardDispatchAllowed(time.Duration) bool }); ok {
		return remote.GuardDispatchAllowed(ttl)
	}
	return true
}

type authorityContext struct {
	API         commandAuthority
	Local       *lease.Service
	Store       *store.Store
	Config      config.Config
	Profile     *config.Profile
	HTTP        *authority.HTTPClient
	ProfileName string
	Remote      bool
}

func (a *authorityContext) Close() error {
	if a == nil || a.Store == nil {
		return nil
	}
	return a.Store.Close()
}

func (a *authorityContext) AuthorityID() string {
	if a.Profile != nil {
		return a.Profile.AuthorityID
	}
	if a.Store != nil {
		return a.Store.AuthorityID()
	}
	return ""
}

func profileSelection(cmd *urfave.Command) (config.ProfileSelection, error) {
	paths := config.UserProfilePaths(os.Getenv)
	explicit := strings.TrimSpace(cmd.String("profile"))
	environmentProfile := strings.TrimSpace(os.Getenv("WORKLEASE_PROFILE"))
	if cmd.Bool("local") {
		if explicit != "" || environmentProfile != "" {
			return config.ProfileSelection{}, reason.New(reason.ReasonCredentialSourceConflict, "--local conflicts with remote profile selection")
		}
		return config.ProfileSelection{Source: "forced-local", Name: config.LocalProfileName}, nil
	}
	var checkoutRoot, repositoryRoot string
	if explicit == "" && environmentProfile == "" {
		var err error
		checkoutRoot, repositoryRoot, err = handle.BindingRoots(mustGetwd(), nil)
		if err != nil {
			return config.ProfileSelection{}, reason.New(reason.ReasonConfigInvalid, err.Error())
		}
	}
	selected, err := config.SelectProfileForRoots(map[string]string{"profile": explicit}, os.Getenv, checkoutRoot, repositoryRoot, paths)
	if err != nil {
		if classified := reason.As(err); classified != nil && classified.Reason == reason.ReasonProfileBindingConflict {
			return config.ProfileSelection{}, err
		}
		return config.ProfileSelection{}, reason.New(reason.ReasonConfigInvalid, err.Error())
	}
	return selected, nil
}

func authorityFor(ctx context.Context, cmd *urfave.Command, write bool) (*authorityContext, error) {
	selected, err := profileSelection(cmd)
	if err != nil {
		return nil, err
	}
	return authorityForSelection(ctx, cmd, write, selected)
}

func queueAuthorityForClaim(ctx context.Context, cmd *urfave.Command, name string) (*authorityContext, queue.ClaimAuthority, error) {
	return queueAuthorityForViewMode(ctx, cmd, name, true, true)
}

type queueAuthoritySet struct {
	Backends      map[string]*authorityContext
	Authorities   map[string]queue.ClaimAuthority
	SourceNames   map[string]string
	SourceBackend map[string]*authorityContext
}

func (set *queueAuthoritySet) Close() {
	if set == nil {
		return
	}
	closed := make(map[*authorityContext]bool)
	for _, backend := range set.Backends {
		if backend != nil && !closed[backend] {
			closed[backend] = true
			_ = backend.Close()
		}
	}
}

func queueAuthoritiesForSources(ctx context.Context, cmd *urfave.Command, cfg config.QueueConfig, sourceIDs []string, fetchMetadata, write bool) (*queueAuthoritySet, error) {
	resolved, err := config.QueueSourceAuthorities(cfg)
	if err != nil {
		return nil, err
	}
	set := &queueAuthoritySet{Backends: map[string]*authorityContext{}, Authorities: map[string]queue.ClaimAuthority{}, SourceNames: map[string]string{}, SourceBackend: map[string]*authorityContext{}}
	if len(sourceIDs) == 0 {
		backend, selected, err := queueAuthorityForViewMode(ctx, cmd, config.LocalProfileName, fetchMetadata, write)
		if err != nil {
			return nil, err
		}
		set.Backends[config.LocalProfileName], set.Authorities[config.LocalProfileName] = backend, selected
	}
	for _, sourceID := range sourceIDs {
		name := resolved[sourceID]
		if name == "" {
			name = config.LocalProfileName
		}
		backend, ok := set.Backends[name]
		if !ok {
			var selected queue.ClaimAuthority
			backend, selected, err = queueAuthorityForViewMode(ctx, cmd, name, fetchMetadata, write)
			if err != nil {
				set.Close()
				return nil, err
			}
			set.Backends[name] = backend
			set.Authorities[name] = selected
		}
		set.SourceNames[sourceID] = name
		set.SourceBackend[sourceID] = backend
	}
	return set, nil
}

func (set *queueAuthoritySet) ForSource(sourceID string) (*authorityContext, queue.ClaimAuthority, bool) {
	if set == nil {
		return nil, queue.ClaimAuthority{}, false
	}
	name := set.SourceNames[sourceID]
	backend, backendOK := set.SourceBackend[sourceID]
	authority, authorityOK := set.Authorities[name]
	return backend, authority, backendOK && authorityOK
}

func (set *queueAuthoritySet) Primary(sourceIDs []string) (*authorityContext, queue.ClaimAuthority, bool) {
	for _, id := range sourceIDs {
		if backend, authority, ok := set.ForSource(id); ok {
			return backend, authority, true
		}
	}
	backend, backendOK := set.Backends[config.LocalProfileName]
	authority, authorityOK := set.Authorities[config.LocalProfileName]
	return backend, authority, backendOK && authorityOK
}

func queueAuthorityForViewMode(ctx context.Context, cmd *urfave.Command, name string, fetchMetadata, write bool) (*authorityContext, queue.ClaimAuthority, error) {
	paths := config.UserProfilePaths(os.Getenv)
	profiles, _, err := config.LoadProfiles(paths)
	if err != nil {
		return nil, queue.ClaimAuthority{}, err
	}
	selected := config.ProfileSelection{Name: name, Source: "queue-view"}
	if name != config.LocalProfileName {
		profile, ok := profiles[name]
		if !ok {
			return nil, queue.ClaimAuthority{}, reason.New(reason.ReasonConfigInvalid, "queue authority profile is not trusted")
		}
		selected.Profile = &profile
	}
	backend, err := authorityForSelection(ctx, cmd, write, selected)
	if err != nil {
		return nil, queue.ClaimAuthority{}, err
	}
	overlay := queue.ClaimAuthority{API: backend.API, LiveAPI: backend.API, ID: backend.AuthorityID(), Profile: backend.ProfileName, Remote: backend.Remote}
	if backend.HTTP != nil {
		overlay.Now = backend.HTTP.Clock().UpperBound
	} else if backend.Local != nil {
		overlay.Now = func() (time.Time, error) { return backend.Local.AuthorityNow(), nil }
	}
	if !backend.Remote {
		workerConfig, err := config.Load(config.Input{})
		if err != nil {
			backend.Close()
			return nil, queue.ClaimAuthority{}, err
		}
		workerStore, err := store.Open(ctx, workerConfig.Home, store.Options{ReadOnly: true})
		if err != nil {
			backend.Close()
			return nil, queue.ClaimAuthority{}, err
		}
		overlay.LocalDefaultAuthorityID = workerStore.AuthorityID()
		if err := workerStore.Close(); err != nil {
			backend.Close()
			return nil, queue.ClaimAuthority{}, err
		}
	}
	if fetchMetadata && backend.HTTP != nil {
		// An outage or an old server with no admission metadata leaves claims
		// unknown; it never authorizes a fallback to local.
		if response, err := backend.HTTP.Metadata(ctx); err == nil && response.Metadata != nil {
			overlay.AdmittedPrefixes = response.Metadata.AdmittedPrefixes
		}
	}
	return backend, overlay, nil
}

// authorityForSelection constructs the same client used by regular commands,
// without replacing a view's authority with the invoking checkout's profile.
func authorityForSelection(ctx context.Context, cmd *urfave.Command, write bool, selected config.ProfileSelection) (*authorityContext, error) {
	cfg, err := configForCommand(cmd)
	if err != nil {
		return nil, err
	}
	if selected.Profile == nil {
		st, err := storeForCommand(ctx, cfg, write)
		if err != nil {
			return nil, err
		}
		svc := lease.New(st, nil, nil, lease.Defaults{TTL: cfg.TTL, PollInterval: cfg.PollInterval})
		local, err := authority.NewLocalAuthority(svc, st, cfg.PollInterval)
		if err != nil {
			st.Close()
			return nil, err
		}
		return &authorityContext{API: authorityWithDefaults{Authority: local, ttl: cfg.TTL, local: svc}, Local: svc, Store: st, Config: cfg, ProfileName: config.LocalProfileName}, nil
	}
	pending := authority.NewFilePendingStore(filepath.Join(cfg.Home, "pending", selected.Name))
	client, err := authority.NewHTTPClient(*selected.Profile, pending, nil)
	if err != nil {
		return nil, err
	}
	remote, err := authority.NewRemoteAuthority(client)
	if err != nil {
		return nil, err
	}
	profile := *selected.Profile
	return &authorityContext{API: authorityWithDefaults{Authority: remote, ttl: cfg.TTL}, Config: cfg, Profile: &profile, HTTP: client, ProfileName: selected.Name, Remote: true}, nil
}
