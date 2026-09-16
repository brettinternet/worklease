package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

const insecureHTTPWarning = "warning: insecure HTTP exposes Worklease credentials and claim data to the network"

func profileCommands(s *boundary) []*urfave.Command {
	leaf := func(name, usage string, flags []urfave.Flag, action func(context.Context, *urfave.Command) error) *urfave.Command {
		return &urfave.Command{Name: name, Usage: usage, UsageText: "worklease profile " + name, Description: usage + ".\n\nExamples:\n  worklease profile " + name, Flags: flags, Action: action}
	}
	add := leaf("add", "add a trusted remote authority profile", []urfave.Flag{&urfave.StringFlag{Name: "endpoint", Usage: "remote authority `URL`"}, &urfave.StringFlag{Name: "authority-id", Usage: "expected authority `ID`"}, &urfave.StringFlag{Name: "certificate-sha256", Usage: "expected DER leaf certificate SHA-256 `HEX`"}, &urfave.BoolFlag{Name: "allow-insecure-http", Usage: "allow cleartext HTTP to the remote authority"}}, profileAddAction(s))
	list := leaf("list", "list trusted remote authority profiles", nil, profileListAction(s))
	list.Aliases = []string{"ls"}
	show := leaf("show", "show one trusted remote authority profile", nil, profileShowAction(s))
	remove := leaf("remove", "remove one trusted remote authority profile", nil, profileRemoveAction(s))
	def := leaf("default", "select the default remote authority profile", nil, profileDefaultAction(s))
	bind := leaf("bind", "bind this checkout to a remote authority profile", []urfave.Flag{&urfave.StringFlag{Name: "cwd", Usage: "checkout `DIR` to bind"}}, profileBindAction(s, false))
	unbind := leaf("unbind", "remove this checkout's remote authority binding", []urfave.Flag{&urfave.StringFlag{Name: "cwd", Usage: "checkout `DIR` to unbind"}}, profileBindAction(s, true))
	profile := &urfave.Command{Name: "profile", Usage: "manage trusted remote authority profiles", UsageText: "worklease profile <add|list|show|remove|default|bind|unbind>", Description: "Manage owner-private remote authority profiles and checkout bindings.\n\nExamples:\n  worklease profile list", Commands: []*urfave.Command{add, list, show, remove, def, bind, unbind}}
	enroll := &urfave.Command{Name: "enroll", Usage: "enroll this installation with a remote authority", UsageText: "worklease enroll [--profile NAME] [--invite-file FILE | --invite-fd N] [--label TEXT]", Description: "Redeem an invitation without exposing either bearer in argv or output. Without an invite option, an interactive terminal prompts without echo. HTTP artifacts additionally require --allow-insecure-http.\n\nExamples:\n  worklease enroll --invite-file invite.artifact", Flags: []urfave.Flag{&urfave.StringFlag{Name: "invite-file", Usage: "owner-private invite artifact or legacy secret `FILE`"}, &urfave.IntFlag{Name: "invite-fd", Usage: "inherited invite descriptor `N`", HideDefault: true}, &urfave.StringFlag{Name: "label", Usage: "installation label `TEXT`"}, &urfave.BoolFlag{Name: "allow-insecure-http", Usage: "explicitly allow an HTTP invite artifact"}}, Action: enrollAction(s)}
	return []*urfave.Command{profile, enroll}
}

func profileNameArg(cmd *urfave.Command) (string, error) {
	name := strings.TrimSpace(cmd.Args().First())
	if name == "" {
		return "", reason.Invalid("profile name is required")
	}
	return name, nil
}

func profileAddAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		name, err := profileNameArg(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		paths := config.UserProfilePaths(os.Getenv)
		profiles, defaultName, err := config.LoadProfiles(paths)
		if err != nil {
			return s.handle(cmd, reason.New(reason.ReasonConfigInvalid, err.Error()))
		}
		if _, exists := profiles[name]; exists {
			return s.handle(cmd, reason.New(reason.ReasonConfigInvalid, "profile already exists"))
		}
		profile := config.Profile{Name: name, Endpoint: strings.TrimRight(strings.TrimSpace(cmd.String("endpoint")), "/"), AuthorityID: strings.TrimSpace(cmd.String("authority-id")), CertificateSHA256: strings.TrimSpace(cmd.String("certificate-sha256")), AllowInsecureHTTP: cmd.Bool("allow-insecure-http"), Credential: config.CredentialDescriptor{Path: filepath.Join(filepath.Dir(paths.Profiles), "credentials", name)}}
		if profile.AllowInsecureHTTP && !s.jsonRequested(cmd) {
			if _, err := fmt.Fprintln(s.errWriter, insecureHTTPWarning); err != nil {
				return err
			}
		}
		client, err := authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(filepath.Dir(paths.Profiles), "pending", name)), nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		metadata, err := client.Metadata(ctx)
		if err != nil {
			return s.handle(cmd, err)
		}
		if profile.AuthorityID != "" && profile.AuthorityID != metadata.AuthorityID {
			return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "discovered authority does not match"))
		}
		profile.AuthorityID, profile.RestoreID = metadata.AuthorityID, metadata.RestoreID
		profiles[name] = profile
		if err := saveProfileMap(paths, profiles, defaultName); err != nil {
			return s.handle(cmd, reason.New(reason.ReasonStorageFailure, err.Error()))
		}
		return writeProfileResult(s, cmd, "profile-add", profile)
	}
}

func saveProfileMap(paths config.ProfilePaths, profiles map[string]config.Profile, defaultName string) error {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]config.Profile, 0, len(names))
	for _, name := range names {
		values = append(values, profiles[name])
	}
	return config.SaveProfiles(paths, values, defaultName)
}
func writeProfileResult(s *boundary, cmd *urfave.Command, operation string, value any) error {
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, operation, map[string]any{"profile": value})
	}
	_, err := fmt.Fprintf(s.writer, "%s completed\n", operation)
	return err
}
func profileListAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error {
		profiles, def, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
		if err != nil {
			return s.handle(cmd, err)
		}
		if s.jsonRequested(cmd) {
			return output.WriteSuccess(s.writer, "profile-list", map[string]any{"profiles": profiles, "default": def})
		}
		names := make([]string, 0, len(profiles))
		for n := range profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		_, err = fmt.Fprintln(s.writer, strings.Join(names, "\n"))
		return err
	}
}
func profileShowAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error {
		name, err := profileNameArg(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		profiles, _, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
		if err != nil {
			return s.handle(cmd, err)
		}
		p, ok := profiles[name]
		if !ok {
			return s.handle(cmd, reason.New(reason.ReasonConfigMissing, "profile is not defined"))
		}
		return writeProfileResult(s, cmd, "profile-show", p)
	}
}
func profileRemoveAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error {
		name, err := profileNameArg(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		paths := config.UserProfilePaths(os.Getenv)
		profiles, def, err := config.LoadProfiles(paths)
		if err != nil {
			return s.handle(cmd, err)
		}
		if _, ok := profiles[name]; !ok {
			return s.handle(cmd, reason.New(reason.ReasonConfigMissing, "profile is not defined"))
		}
		delete(profiles, name)
		if def == name {
			def = ""
		}
		if err := saveProfileMap(paths, profiles, def); err != nil {
			return s.handle(cmd, err)
		}
		return writeProfileResult(s, cmd, "profile-remove", name)
	}
}
func profileDefaultAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error {
		name, err := profileNameArg(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		paths := config.UserProfilePaths(os.Getenv)
		profiles, _, err := config.LoadProfiles(paths)
		if err != nil {
			return s.handle(cmd, err)
		}
		if _, ok := profiles[name]; !ok {
			return s.handle(cmd, reason.New(reason.ReasonConfigMissing, "profile is not defined"))
		}
		if err := saveProfileMap(paths, profiles, name); err != nil {
			return s.handle(cmd, err)
		}
		return writeProfileResult(s, cmd, "profile-default", name)
	}
}
func profileBindAction(s *boundary, remove bool) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error {
		paths := config.UserProfilePaths(os.Getenv)
		cwd := strings.TrimSpace(cmd.String("cwd"))
		if cwd == "" {
			cwd = mustGetwd()
		}
		root, err := handle.ContextRoot(cwd, nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		bindings := map[string]string{}
		if dataErr := func() error {
			data, e := handle.ReadOwnerPrivate(paths.Bindings, 1<<20)
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			var doc struct {
				Bindings map[string]string `yaml:"bindings"`
			}
			if e = yaml.Unmarshal(data, &doc); e != nil {
				return e
			}
			for k, v := range doc.Bindings {
				bindings[k] = v
			}
			return nil
		}(); dataErr != nil {
			return s.handle(cmd, dataErr)
		}
		op := "profile-bind"
		if remove {
			delete(bindings, root)
			op = "profile-unbind"
		} else {
			name, e := profileNameArg(cmd)
			if e != nil {
				return s.handle(cmd, e)
			}
			profiles, _, e := config.LoadProfiles(paths)
			if e != nil {
				return s.handle(cmd, e)
			}
			if _, ok := profiles[name]; !ok {
				return s.handle(cmd, reason.New(reason.ReasonConfigMissing, "profile is not defined"))
			}
			bindings[root] = name
		}
		if err := config.SaveBindings(paths, bindings); err != nil {
			return s.handle(cmd, err)
		}
		return writeProfileResult(s, cmd, op, root)
	}
}

var readHiddenInvite = func() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", reason.New(reason.ReasonCredentialSourceConflict, "invite source is required when stdin is not a terminal")
	}
	if _, err := fmt.Fprint(os.Stderr, "Invite: "); err != nil {
		return "", reason.New(reason.ReasonCredentialUnsafe, "invite prompt is unavailable")
	}
	value, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", reason.New(reason.ReasonCredentialUnsafe, "invite prompt could not be read")
	}
	return strings.TrimSpace(string(value)), nil
}

func inviteInputFromCommand(cmd *urfave.Command) (string, *authority.InviteArtifact, error) {
	var raw []byte
	path := strings.TrimSpace(cmd.String("invite-file"))
	fdSet := cmd.IsSet("invite-fd")
	if (path != "") == fdSet {
		if path == "" && !fdSet {
			value, err := readHiddenInvite()
			if err != nil {
				return "", nil, err
			}
			raw = []byte(value)
		} else {
			return "", nil, reason.New(reason.ReasonCredentialSourceConflict, "exactly one invite source is required")
		}
	} else if path != "" {
		var err error
		raw, err = handle.ReadOwnerPrivate(path, authority.MaxInviteArtifactBytes+1)
		if err != nil {
			return "", nil, reason.New(reason.ReasonCredentialUnsafe, "invite source cannot be read safely")
		}
	} else {
		fd := cmd.Int("invite-fd")
		if fd < 0 {
			return "", nil, reason.New(reason.ReasonCredentialUnsafe, "invite descriptor is invalid")
		}
		var err error
		raw, err = handle.ReadBoundedFD(fd, authority.MaxInviteArtifactBytes+1)
		if err != nil {
			return "", nil, err
		}
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	if len(raw) > authority.MaxInviteArtifactBytes {
		return "", nil, reason.New(reason.ReasonCredentialUnsafe, "invite source is oversized")
	}
	value := string(raw)
	if artifact, err := authority.DecodeInviteArtifact(value); err == nil {
		return artifact.Invite, &artifact, nil
	}
	if err := validateTokenCLI(value); err != nil {
		return "", nil, err
	}
	return value, nil, nil
}

// Kept for focused CLI tests and legacy callers.
func inviteFromCommand(cmd *urfave.Command) (string, error) {
	value, _, err := inviteInputFromCommand(cmd)
	return value, err
}

func enrollAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if cmd.Bool("local") {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "--local cannot be used with enrollment"))
		}
		invite, artifact, err := inviteInputFromCommand(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		paths := config.UserProfilePaths(os.Getenv)
		profiles, _, err := config.LoadProfiles(paths)
		if err != nil {
			return s.handle(cmd, err)
		}
		var selected config.ProfileSelection
		if artifact != nil {
			name := strings.TrimSpace(cmd.String("profile"))
			if name == "" {
				name = artifact.ProfileHint
			}
			if name == "" {
				name = "remote"
			}
			if strings.HasPrefix(strings.ToLower(artifact.Endpoint), "http://") && !cmd.Bool("allow-insecure-http") {
				return s.handle(cmd, reason.New(reason.ReasonConfigInvalid, "HTTP invite artifacts require --allow-insecure-http"))
			}
			if err := config.ValidateProfileName(name); err != nil {
				return s.handle(cmd, reason.New(reason.ReasonConfigInvalid, err.Error()))
			}
			profile, exists := profiles[name]
			if exists {
				if profile.Endpoint != artifact.Endpoint || profile.AuthorityID != artifact.AuthorityID || profile.CertificateSHA256 != artifact.CertificateSHA256 {
					return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "invite artifact conflicts with the established profile trust"))
				}
			} else {
				profile = config.Profile{Name: name, Endpoint: artifact.Endpoint, AuthorityID: artifact.AuthorityID, CertificateSHA256: artifact.CertificateSHA256, AllowInsecureHTTP: cmd.Bool("allow-insecure-http"), Credential: config.CredentialDescriptor{Path: filepath.Join(filepath.Dir(paths.Profiles), "credentials", name)}}
			}
			selected = config.ProfileSelection{Profile: &profile, Name: name, Source: "artifact"}
		} else {
			selected, err = profileSelection(cmd)
			if err != nil {
				return s.handle(cmd, err)
			}
			if selected.Profile == nil {
				return s.handle(cmd, reason.New(reason.ReasonConfigMissing, "remote profile is required"))
			}
		}
		cfg, err := configForCommand(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		client, err := authority.NewHTTPClient(*selected.Profile, authority.NewFilePendingStore(filepath.Join(cfg.Home, "pending", selected.Name)), nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		label := strings.TrimSpace(cmd.String("label"))
		if label == "" {
			label = selected.Name
		}
		profile, result, err := client.Enroll(ctx, invite, label, paths)
		if err != nil {
			return s.handle(cmd, err)
		}
		if s.jsonRequested(cmd) {
			return output.WriteSuccess(s.writer, "enroll", map[string]any{"profile": profile.Name, "installation": result})
		}
		_, err = fmt.Fprintf(s.writer, "enrolled profile %s\n", profile.Name)
		return err
	}
}
