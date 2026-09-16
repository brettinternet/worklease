package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	workleaseserver "github.com/brettinternet/worklease/internal/server"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

// beforeHostedReadyHook is test-only crash-boundary instrumentation after the
// database grant commits but before the finalization marker is durable.
var beforeHostedReadyHook func() error

const acceptanceCrashBeforeHostedReady = "WORKLEASE_ACCEPTANCE_CRASH_BEFORE_HOSTED_READY"

func serverCommands(s *boundary) *urfave.Command {
	secret := &urfave.StringFlag{Name: "bootstrap-invite-file", Usage: "owner-private bootstrap invite `FILE`"}
	initCommand := &urfave.Command{
		Name: "init", Usage: "initialize a server", UsageText: "worklease server init [--server-config FILE] [--bootstrap-invite-file FILE] [--guided]",
		Description: "Create the legacy local-only server configuration, or opt into guided remote setup, initialize its authority, and write a one-time bootstrap invite.\n\nExamples:\n  worklease server init\n  worklease server init --guided\n  worklease server init --guided --listen 0.0.0.0:8443 --endpoint https://worklease.example.com:8443 --transport tls --admitted-prefix task: --admitted-prefix coordination: --confirm-non-loopback",
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "server-config", Usage: "deployment server configuration `FILE` [$WORKLEASE_SERVER_CONFIG]"}, secret,
			&urfave.BoolFlag{Name: "guided", Usage: "prompt for or validate a ready-to-run remote server setup"},
			&urfave.StringFlag{Name: "listen", Usage: "guided listener `HOST:PORT`"},
			&urfave.StringFlag{Name: "endpoint", Usage: "guided client-facing origin `URL`"},
			&urfave.StringFlag{Name: "transport", Usage: "guided transport: tls or http"},
			&urfave.StringSliceFlag{Name: "admitted-prefix", Usage: "guided admitted resource `PREFIX` (repeatable)"},
			&urfave.StringFlag{Name: "tls-cert", Usage: "existing owner-private TLS certificate `FILE`"},
			&urfave.StringFlag{Name: "tls-key", Usage: "existing owner-private TLS key `FILE`"},
			&urfave.BoolFlag{Name: "confirm-non-loopback", Usage: "confirm exposure on a non-loopback listener"},
			&urfave.BoolFlag{Name: "acknowledge-cleartext-credentials", Usage: "acknowledge that HTTP exposes bearer credentials"},
		},
	}
	initCommand.Action = func(ctx context.Context, cmd *urfave.Command) error { return hostedInit(s, ctx, cmd) }
	restore := &urfave.Command{
		Name: "restore", Usage: "restore a server authority backup", UsageText: "worklease server restore --home DIR --from FILE --selected-cutoff RFC3339 --loss-interval-start RFC3339 --loss-interval-end RFC3339 --bootstrap-invite-file FILE [--cutoff-unknown]",
		Description: "Restore a hosted backup into a fresh authority incarnation and enter recovery mode.\n\nExamples:\n  worklease server restore --home DIR --from FILE --selected-cutoff 2026-09-14T00:00:00Z --loss-interval-start 2026-09-14T00:00:00Z --loss-interval-end 2026-09-14T00:05:00Z --bootstrap-invite-file FILE",
		Flags:       []urfave.Flag{&urfave.StringFlag{Name: "from", Usage: "owner-private backup `FILE`"}, &urfave.StringFlag{Name: "selected-cutoff", Usage: "durable backup cutoff `RFC3339`"}, &urfave.StringFlag{Name: "loss-interval-start", Usage: "loss interval start `RFC3339`"}, &urfave.StringFlag{Name: "loss-interval-end", Usage: "loss interval end `RFC3339`"}, &urfave.BoolFlag{Name: "cutoff-unknown", Usage: "record that the durable cutoff is unknown"}, secret},
	}
	restore.Action = func(ctx context.Context, cmd *urfave.Command) error { return hostedRestore(s, ctx, cmd) }
	reissue := &urfave.Command{
		Name: "bootstrap-reissue", Usage: "replace the server bootstrap invite", UsageText: "worklease server bootstrap-reissue --home DIR --bootstrap-invite-file FILE",
		Description: "Replace only the active bootstrap invite while preserving authority history.\n\nExamples:\n  worklease server bootstrap-reissue --home DIR --bootstrap-invite-file FILE", Flags: []urfave.Flag{secret},
	}
	reissue.Action = func(ctx context.Context, cmd *urfave.Command) error { return hostedReissue(s, ctx, cmd) }
	retire := &urfave.Command{
		Name: "retire", Usage: "retire a server authority", UsageText: "worklease server retire --home DIR [--force --unresolved-export FILE]",
		Description: "Safely retire a hosted authority; forced retirement first exports redacted unresolved recovery records.\n\nExamples:\n  worklease server retire --home DIR\n  worklease server retire --home DIR --force --unresolved-export FILE",
		Flags:       []urfave.Flag{&urfave.BoolFlag{Name: "force", Usage: "allow retirement after writing a redacted unresolved export"}, &urfave.StringFlag{Name: "unresolved-export", Usage: "external redacted recovery export `FILE`"}},
	}
	retire.Action = func(ctx context.Context, cmd *urfave.Command) error { return hostedRetire(s, ctx, cmd) }
	return &urfave.Command{Name: "server", Usage: "initialize and manage a server authority", UsageText: "worklease server <init|restore|bootstrap-reissue|retire>", Description: "Server-authority lifecycle commands. Every command takes the hosted writer lock before opening SQLite.\n\nExamples:\n  worklease server init\n  worklease server restore --home DIR --from FILE --bootstrap-invite-file FILE\n  worklease server retire --home DIR", Commands: []*urfave.Command{initCommand, restore, reissue, retire}, OnUsageError: func(_ context.Context, cmd *urfave.Command, _ error, _ bool) error {
		return s.handle(cmd, reason.Invalid("invalid server command arguments"))
	}}
}

func defaultServerConfigPath() string {
	if value := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); value != "" {
		return filepath.Join(value, "worklease", "server.yaml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "worklease", "server.yaml")
	}
	return filepath.Join(os.TempDir(), "worklease", "server.yaml")
}

func defaultServerHome() string {
	if value := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); value != "" {
		return filepath.Join(value, "worklease", "server")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "worklease", "server")
	}
	return filepath.Join(os.TempDir(), "worklease", "server")
}

func serverConfigPath(cmd *urfave.Command) (string, error) {
	path := strings.TrimSpace(cmd.String("server-config"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("WORKLEASE_SERVER_CONFIG"))
	}
	if path == "" {
		path = defaultServerConfigPath()
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Abs(filepath.Clean(path))
}

func writeDefaultServerConfig(path string) error {
	parent := filepath.Dir(path)
	_, statErr := os.Lstat(parent)
	createdParent := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !createdParent {
		return statErr
	}
	if createdParent {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
	}
	if err := store.ValidateHostedHome(parent); err != nil {
		return reason.New(reason.ReasonHomeUnsafe, "server configuration directory must be owner-private")
	}
	contents := fmt.Sprintf("home: %q\nlisten: 127.0.0.1:8443\nallowInsecureHTTP: true\nadmittedPrefixes:\n  - \"coordination:\"\nmaxTTL: 1h\nmaxHold: 24h\nshutdownTimeout: 5s\nhealthRate: 60\nmetadataRate: 60\nenrollmentRate: 20\n", defaultServerHome())
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if _, err = io.WriteString(file, contents); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	keep = true
	return nil
}

func hostedHome(cmd *urfave.Command) (string, error) {
	home := strings.TrimSpace(cmd.String("home"))
	if home == "" {
		return "", reason.Invalid("--home is required")
	}
	return filepath.Abs(filepath.Clean(home))
}
func requiredHostedFile(cmd *urfave.Command, name string) (string, error) {
	value := strings.TrimSpace(cmd.String(name))
	if value == "" {
		return "", reason.Invalid("--" + name + " is required")
	}
	return filepath.Abs(filepath.Clean(value))
}
func validateHostedInitContents(home, invitePath string) error {
	entries, err := os.ReadDir(home)
	if err != nil {
		return err
	}
	allowed := map[string]bool{
		store.HostedMarkerFileName:      true,
		store.HostedLockFileName:        true,
		store.HostedReadyFileName:       true,
		store.DatabaseFileName:          true,
		store.DatabaseFileName + "-wal": true,
		store.DatabaseFileName + "-shm": true,
		"handles":                       true,
	}
	if relative, relErr := filepath.Rel(home, invitePath); relErr == nil && filepath.Dir(relative) == "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		allowed[relative] = true
		allowed[relative+".legacy-secret"] = true
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return reason.Invalid("hosted initialization requires an empty or recoverable authority home")
		}
	}
	return nil
}

func stageHostedSecret(path string) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return store.ReadHostedSecret(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := lease.GenerateInviteCode()
	if err != nil {
		return "", err
	}
	if err := store.WriteHostedSecret(path, value); err != nil {
		return "", err
	}
	return value, nil
}

// Init keeps a durable legacy secret beside the artifact until initialization
// is ready. This preserves crash recovery while making the transferred file a
// single self-contained artifact.
func stageHostedInitSecret(path string, allowArtifact bool) (string, error) {
	if data, err := handle.ReadOwnerPrivate(path, authority.MaxInviteArtifactBytes+1); err == nil {
		if artifact, decodeErr := authority.DecodeInviteArtifact(strings.TrimSuffix(string(data), "\n")); decodeErr == nil {
			if !allowArtifact {
				return "", reason.New(reason.ReasonCredentialUnsafe, "preexisting invite artifact requires a recoverable hosted authority")
			}
			return artifact.Invite, nil
		}
		return store.ReadHostedSecret(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return stageHostedSecret(path + ".legacy-secret")
}

// rejectStagedBootstrapSecret refuses a fresh initialization that would adopt
// a bootstrap secret staged by an unrelated or interrupted run. That bearer may
// already be known to a previous holder, and HostedInitialize would grant it
// the new authority's admin role.
func rejectStagedBootstrapSecret(invitePath string) error {
	legacyPath := invitePath + ".legacy-secret"
	if _, err := os.Lstat(legacyPath); err == nil {
		return reason.New(reason.ReasonCredentialUnsafe, "staged bootstrap secret "+legacyPath+" belongs to another initialization; remove it or resume that authority")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeHostedInviteArtifact(path string, result lease.BootstrapResult, setup *guidedSetupResult, secret string, advertisedEndpoint string, listen string, insecure bool) error {
	endpoint, pin := "", ""
	if setup != nil {
		endpoint, pin = setup.Endpoint, setup.Fingerprint
	}
	if endpoint == "" {
		endpoint = strings.TrimSpace(advertisedEndpoint)
	}
	if endpoint == "" {
		scheme := "https"
		if insecure {
			scheme = "http"
		}
		endpoint = scheme + "://" + listen
	}
	desired := authority.InviteArtifact{Version: 1, Endpoint: endpoint, AuthorityID: result.AuthorityID, CertificateSHA256: pin, ProfileHint: "admin", Invite: secret}
	encoded, err := authority.EncodeInviteArtifact(desired)
	if err != nil {
		return err
	}
	data, readErr := handle.ReadOwnerPrivate(path, authority.MaxInviteArtifactBytes+1)
	if readErr == nil {
		value := strings.TrimSuffix(string(data), "\n")
		if existing, decodeErr := authority.DecodeInviteArtifact(value); decodeErr == nil {
			if existing != desired {
				return reason.New(reason.ReasonAuthorityMismatch, "existing invite artifact conflicts with hosted authority")
			}
		} else {
			legacy, legacyErr := store.ReadHostedSecret(path)
			if legacyErr != nil || legacy != secret {
				return reason.New(reason.ReasonCredentialUnsafe, "existing bootstrap invite conflicts with hosted authority")
			}
			if err := handle.WriteOwnerPrivate(path, []byte(encoded+"\n"), authority.MaxInviteArtifactBytes+1); err != nil {
				return err
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	} else if err := handle.WriteOwnerPrivateNoReplace(path, []byte(encoded+"\n"), authority.MaxInviteArtifactBytes+1); err != nil {
		return err
	}
	legacyPath := path + ".legacy-secret"
	if err := handle.RemoveOwnerPrivate(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func parseHostedTime(value, name string, required bool) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		if required {
			return time.Time{}, reason.Invalid("--" + name + " is required")
		}
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, reason.Invalid("--" + name + " must be RFC3339")
	}
	return parsed.UTC(), nil
}
func hostedOpen(ctx context.Context, home string) (*store.Store, *store.HostedLock, error) {
	lock, err := store.AcquireHostedLock(ctx, home)
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(ctx, home, store.Options{HostedWriter: true, HostedLock: lock, RequireDatabaseIfHostedReady: true})
	if err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	return st, lock, nil
}
func hostedResult(s *boundary, cmd *urfave.Command, op string, value any) error {
	if s.jsonRequested(cmd) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			return err
		}
		return output.WriteSuccess(s.writer, op, fields)
	}
	_, err := fmt.Fprintf(s.writer, "%s completed\n", op)
	return err
}
func hostedError(s *boundary, cmd *urfave.Command, err error) error { return s.handle(cmd, err) }

func finalizeHostedReady(lock *store.HostedLock) error {
	if beforeHostedReadyHook != nil {
		if err := beforeHostedReadyHook(); err != nil {
			return err
		}
	}
	return store.WriteHostedReady(lock)
}

func hostedResultPath(s *boundary, cmd *urfave.Command, op string, value any, path string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var fields map[string]any
	if err = json.Unmarshal(encoded, &fields); err != nil {
		return err
	}
	fields["bootstrapInviteFile"] = path
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, op, fields)
	}
	_, err = fmt.Fprintf(s.writer, "%s completed; bootstrap invite file %s\n", op, path)
	return err
}

func hostedGuidedResult(s *boundary, cmd *urfave.Command, bootstrap lease.BootstrapResult, setup *guidedSetupResult, invitePath string) error {
	fields := map[string]any{
		"authorityId":           bootstrap.AuthorityID,
		"restoreId":             bootstrap.RestoreID,
		"inviteId":              bootstrap.InviteID,
		"expiresAt":             bootstrap.ExpiresAt,
		"serverConfig":          setup.ConfigPath,
		"bootstrapInviteFile":   invitePath,
		"advertisedEndpoint":    setup.Endpoint,
		"certificateFile":       setup.CertificatePath,
		"keyFile":               setup.KeyPath,
		"certificateSha256":     setup.Fingerprint,
		"certificateValidUntil": setup.CertificateEnd,
		"startCommand":          "worklease serve --server-config " + shellQuote(setup.ConfigPath),
		"enrollCommand":         "worklease enroll --invite-file " + shellQuote(invitePath),
	}
	if setup.Warning != "" {
		if _, err := fmt.Fprintln(s.errWriter, setup.Warning); err != nil {
			return err
		}
	}
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, "server-init", fields)
	}
	_, err := fmt.Fprintf(s.writer, "server-init completed\nserver config: %s\nbootstrap invite file: %s\nauthority ID: %s\nadvertised endpoint: %s\n", setup.ConfigPath, invitePath, bootstrap.AuthorityID, setup.Endpoint)
	if err != nil {
		return err
	}
	if setup.CertificatePath != "" {
		if _, err = fmt.Fprintf(s.writer, "TLS certificate: %s\nTLS key: %s\ncertificate SHA-256: %s\ncertificate valid until: %s\n", setup.CertificatePath, setup.KeyPath, setup.Fingerprint, setup.CertificateEnd.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(s.writer, "start: worklease serve --server-config %s\nenroll: worklease enroll --invite-file %s\n", shellQuote(setup.ConfigPath), shellQuote(invitePath))
	return err
}

func hostedInit(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	if !cmd.Bool("guided") {
		for _, name := range []string{"listen", "endpoint", "transport", "admitted-prefix", "tls-cert", "tls-key", "confirm-non-loopback", "acknowledge-cleartext-credentials"} {
			if cmd.IsSet(name) {
				return hostedError(s, cmd, reason.Invalid("--"+name+" requires --guided"))
			}
		}
	}
	configPath, err := serverConfigPath(cmd)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	invitePath := strings.TrimSpace(cmd.String("bootstrap-invite-file"))
	if invitePath == "" {
		invitePath = filepath.Join(filepath.Dir(configPath), "bootstrap.invite")
	}
	invitePath, err = filepath.Abs(filepath.Clean(invitePath))
	if err != nil {
		return hostedError(s, cmd, err)
	}
	guided, err := prepareGuidedSetup(s.errWriter, cmd, configPath, invitePath)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	guidedCommitted := false
	defer func() {
		if guided != nil && !guidedCommitted {
			rollbackGuided(guided.created)
		}
	}()
	if guided != nil {
		defer guided.release()
	} else {
		initLock, lockErr := acquireGuidedSetupLock(configPath)
		if lockErr != nil {
			return hostedError(s, cmd, lockErr)
		}
		defer releaseGuidedSetupLock(initLock)
	}
	if guided == nil {
		if _, statErr := os.Lstat(configPath); errors.Is(statErr, os.ErrNotExist) {
			if err = writeDefaultServerConfig(configPath); err != nil {
				return hostedError(s, cmd, err)
			}
		} else if statErr != nil {
			return hostedError(s, cmd, statErr)
		}
	}
	config, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		if guided != nil {
			rollbackGuided(guided.created)
		}
		return hostedError(s, cmd, err)
	}
	home, err := filepath.Abs(filepath.Clean(config.Home))
	if err != nil {
		return hostedError(s, cmd, err)
	}
	if explicitHome := strings.TrimSpace(cmd.String("home")); explicitHome != "" {
		resolved, resolveErr := filepath.Abs(filepath.Clean(explicitHome))
		if resolveErr != nil {
			return hostedError(s, cmd, resolveErr)
		}
		if resolved != home {
			return hostedError(s, cmd, reason.Invalid("--home must match the server configuration home"))
		}
	}
	if err = store.ValidateHostedHome(home); err != nil {
		if guided != nil {
			rollbackGuided(guided.created)
		}
		return hostedError(s, cmd, err)
	}
	marked, markErr := os.Lstat(filepath.Join(home, store.HostedMarkerFileName))
	resuming := markErr == nil
	if errors.Is(markErr, os.ErrNotExist) {
		if err := rejectStagedBootstrapSecret(invitePath); err != nil {
			if guided != nil {
				rollbackGuided(guided.created)
			}
			return hostedError(s, cmd, err)
		}
		if err := store.MarkHosted(home); err != nil {
			return hostedError(s, cmd, err)
		}
	} else if markErr != nil {
		return hostedError(s, cmd, markErr)
	} else if marked.Mode()&os.ModeSymlink != 0 {
		return hostedError(s, cmd, reason.New(reason.ReasonHomeUnsafe, "hosted marker is unsafe"))
	}
	guidedCommitted = true
	allowArtifact := false
	if resuming {
		if err := validateHostedInitContents(home, invitePath); err != nil {
			return hostedError(s, cmd, err)
		}
		ready, readyErr := store.HostedReady(home)
		if readyErr != nil {
			return hostedError(s, cmd, readyErr)
		}
		allowArtifact = ready
		if ready {
			if _, databaseErr := os.Lstat(filepath.Join(home, store.DatabaseFileName)); errors.Is(databaseErr, os.ErrNotExist) {
				return hostedError(s, cmd, reason.New(reason.ReasonStorageFailure, "ready server authority database is missing"))
			} else if databaseErr != nil {
				return hostedError(s, cmd, databaseErr)
			}
		}
		if _, secretErr := os.Lstat(invitePath); errors.Is(secretErr, os.ErrNotExist) {
			if _, legacyErr := os.Lstat(invitePath + ".legacy-secret"); errors.Is(legacyErr, os.ErrNotExist) {
				if _, databaseErr := os.Lstat(filepath.Join(home, store.DatabaseFileName)); !errors.Is(databaseErr, os.ErrNotExist) {
					return hostedError(s, cmd, reason.Invalid("incomplete hosted home requires its staged bootstrap secret"))
				}
			} else if legacyErr != nil {
				return hostedError(s, cmd, legacyErr)
			}
		} else if secretErr != nil {
			return hostedError(s, cmd, secretErr)
		}
	}
	secret, err := stageHostedInitSecret(invitePath, allowArtifact)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	st, lock, err := hostedOpen(ctx, home)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	defer lock.Close()
	result, err := lease.New(st, nil, nil, lease.Defaults{}).HostedInitialize(ctx, secret)
	if err != nil {
		_ = st.Close()
		return hostedError(s, cmd, err)
	}
	// The acceptance harness uses a real subprocess exit to prove recovery at
	// the committed-grant/before-ready durability boundary.
	if os.Getenv(acceptanceCrashBeforeHostedReady) == "1" {
		os.Exit(86)
	}
	if err := finalizeHostedReady(lock); err != nil {
		_ = st.Close()
		return hostedError(s, cmd, err)
	}
	if err := st.Close(); err != nil {
		return hostedError(s, cmd, err)
	}
	artifactSetup := guided
	if artifactSetup == nil && config.AdvertisedEndpoint != "" && config.TLSCert != "" {
		endpoint, parseErr := url.Parse(config.AdvertisedEndpoint)
		if parseErr != nil {
			return hostedError(s, cmd, reason.New(reason.ReasonConfigInvalid, "advertised endpoint is invalid"))
		}
		fingerprint, _, _, inspectErr := inspectGuidedCertificate(config.TLSCert, config.TLSKey, endpoint.Hostname())
		if inspectErr != nil {
			return hostedError(s, cmd, inspectErr)
		}
		artifactSetup = &guidedSetupResult{Endpoint: config.AdvertisedEndpoint, Fingerprint: fingerprint}
	}
	if err := writeHostedInviteArtifact(invitePath, result, artifactSetup, secret, config.AdvertisedEndpoint, config.Listen, config.AllowInsecureHTTP); err != nil {
		return hostedError(s, cmd, err)
	}
	if guided != nil {
		if err := completeGuidedSetup(guided); err != nil {
			return hostedError(s, cmd, err)
		}
		return hostedGuidedResult(s, cmd, result, guided, invitePath)
	}
	if err := completeRecoveredGuidedSetup(configPath, home); err != nil {
		return hostedError(s, cmd, err)
	}
	return hostedResultPath(s, cmd, "server-init", result, invitePath)
}

func hostedRestore(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	home, err := hostedHome(cmd)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	from, err := requiredHostedFile(cmd, "from")
	if err != nil {
		return hostedError(s, cmd, err)
	}
	secretPath, err := requiredHostedFile(cmd, "bootstrap-invite-file")
	if err != nil {
		return hostedError(s, cmd, err)
	}
	secret, err := stageHostedSecret(secretPath)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	cutoff, err := parseHostedTime(cmd.String("selected-cutoff"), "selected-cutoff", !cmd.Bool("cutoff-unknown"))
	if err != nil {
		return hostedError(s, cmd, err)
	}
	start, err := parseHostedTime(cmd.String("loss-interval-start"), "loss-interval-start", true)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	end, err := parseHostedTime(cmd.String("loss-interval-end"), "loss-interval-end", true)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	if end.Before(start) {
		return hostedError(s, cmd, reason.Invalid("loss interval end must not precede its start"))
	}
	if _, err = os.Stat(from); err != nil {
		return hostedError(s, cmd, reason.New(reason.ReasonHomeUnsafe, "restore backup is unavailable"))
	}
	lock, err := store.AcquireHostedLock(ctx, home)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	defer lock.Close()
	if err := store.ClearHostedReady(lock); err != nil {
		return hostedError(s, cmd, err)
	}
	if err := store.ReplaceHostedDatabase(lock, from); err != nil {
		return hostedError(s, cmd, err)
	}
	st, err := store.Open(ctx, home, store.Options{HostedWriter: true, HostedLock: lock})
	if err != nil {
		return hostedError(s, cmd, err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{})
	result, err := svc.HostedRestore(ctx, lease.HostedRestoreRequest{SelectedCutoff: cutoff, LossStart: start, LossEnd: end, CutoffUnknown: cmd.Bool("cutoff-unknown")}, secret)
	if err != nil {
		_ = st.Close()
		return hostedError(s, cmd, err)
	}
	if err := finalizeHostedReady(lock); err != nil {
		_ = st.Close()
		return hostedError(s, cmd, err)
	}
	if err := st.Close(); err != nil {
		return hostedError(s, cmd, err)
	}
	return hostedResultPath(s, cmd, "server-restore", result, secretPath)
}

func hostedReissue(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	home, err := hostedHome(cmd)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	path, err := requiredHostedFile(cmd, "bootstrap-invite-file")
	if err != nil {
		return hostedError(s, cmd, err)
	}
	secret, err := stageHostedSecret(path)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	st, lock, err := hostedOpen(ctx, home)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	defer lock.Close()
	result, err := lease.New(st, nil, nil, lease.Defaults{}).HostedBootstrapReissue(ctx, secret)
	if err != nil {
		_ = st.Close()
		return hostedError(s, cmd, err)
	}
	if err := finalizeHostedReady(lock); err != nil {
		_ = st.Close()
		return hostedError(s, cmd, err)
	}
	if err := st.Close(); err != nil {
		return hostedError(s, cmd, err)
	}
	return hostedResultPath(s, cmd, "server-bootstrap-reissue", result, path)
}

func hostedRetire(s *boundary, ctx context.Context, cmd *urfave.Command) error {
	home, err := hostedHome(cmd)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	lock, err := store.AcquireHostedLock(ctx, home)
	if err != nil {
		return hostedError(s, cmd, err)
	}
	st, err := store.Open(ctx, home, store.Options{HostedWriter: true, RequireHostedReady: true, HostedLock: lock})
	if err != nil {
		_ = lock.Close()
		return hostedError(s, cmd, err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{})
	status, err := svc.HostedRetirementStatus(ctx)
	if err != nil {
		_ = st.Close()
		lock.Close()
		return hostedError(s, cmd, err)
	}
	if !cmd.Bool("force") && (status.ActiveClaims > 0 || status.Unresolved > 0) {
		_ = st.Close()
		lock.Close()
		return hostedError(s, cmd, reason.Invalid("hosted authority has active claims or unresolved operations"))
	}
	exportPath := strings.TrimSpace(cmd.String("unresolved-export"))
	if cmd.Bool("force") {
		if exportPath == "" {
			_ = st.Close()
			lock.Close()
			return hostedError(s, cmd, reason.Invalid("--force requires --unresolved-export"))
		}
		exportPath, err = filepath.Abs(filepath.Clean(exportPath))
		if err != nil {
			_ = st.Close()
			_ = lock.Close()
			return hostedError(s, cmd, err)
		}
		rel, relErr := filepath.Rel(home, exportPath)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			_ = st.Close()
			lock.Close()
			return hostedError(s, cmd, reason.Invalid("unresolved export must be outside hosted home"))
		}
		if err := store.WriteHostedExport(exportPath, func(writer io.Writer) error {
			_, exportErr := svc.WriteHostedRetirementExport(ctx, writer)
			return exportErr
		}); err != nil {
			_ = st.Close()
			lock.Close()
			return hostedError(s, cmd, err)
		}
	}
	if err := st.CloseDatabase(); err != nil {
		lock.Close()
		return hostedError(s, cmd, err)
	}
	if err := store.RemoveHostedFiles(lock); err != nil {
		lock.Close()
		return hostedError(s, cmd, err)
	}
	if err := lock.Close(); err != nil {
		return hostedError(s, cmd, err)
	}
	return hostedResult(s, cmd, "server-retire", map[string]any{"exported": cmd.Bool("force"), "unresolved": status.Unresolved})
}
