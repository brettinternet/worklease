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
	root, err := handle.ContextRoot(mustGetwd(), nil)
	if err != nil {
		root = filepath.Clean(mustGetwd())
	}
	explicit := strings.TrimSpace(cmd.String("profile"))
	if cmd.Bool("local") {
		if explicit != "" || strings.TrimSpace(os.Getenv("WORKLEASE_PROFILE")) != "" {
			return config.ProfileSelection{}, reason.New(reason.ReasonCredentialSourceConflict, "--local conflicts with remote profile selection")
		}
		return config.ProfileSelection{Source: "forced-local", Name: config.LocalProfileName}, nil
	}
	selected, err := config.SelectProfile(map[string]string{"profile": explicit}, os.Getenv, root, paths)
	if err != nil {
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

// queueAuthorityForView resolves a view's named authority, independently of
// the invoking checkout's profile flags. The caller closes the returned context.
func queueAuthorityForView(ctx context.Context, cmd *urfave.Command, name string) (*authorityContext, queue.ClaimAuthority, error) {
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
	backend, err := authorityForSelection(ctx, cmd, false, selected)
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
	if backend.HTTP != nil {
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
