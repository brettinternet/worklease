package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// External init deliberately negotiates only the manifest. It does not resolve
// a source, deliver credentials, approve the executable, or probe provider data.
func prepareQueueInitExternal(ctx context.Context, cmd *urfave.Command, result queueInitResult) (queueInitResult, error) {
	if cmd.String("executable") == "" || cmd.IsSet("checkout") || cmd.IsSet("me") || cmd.Bool("allow-git-network") || cmd.IsSet("adapter-config") && cmd.IsSet("adapter-config-file") {
		return result, reason.Invalid("external init requires --executable; --checkout, --me, --allow-git-network and combined config flags are not supported")
	}
	path := cmd.String("executable")
	digest, err := queueAdapterExecutableDigest(path)
	if err != nil {
		return result, err
	}
	result.ExecutableSHA256 = digest
	configuration := map[string]any{}
	if cmd.IsSet("adapter-config") || cmd.IsSet("adapter-config-file") {
		data := []byte(cmd.String("adapter-config"))
		if file := cmd.String("adapter-config-file"); file != "" {
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return result, reason.New(reason.ReasonAdapterConfigInvalid, "adapter configuration file must be a readable JSON file under 1 MiB")
			}
			data, err = os.ReadFile(file)
			if err != nil {
				return result, reason.New(reason.ReasonAdapterConfigInvalid, "adapter configuration file cannot be read")
			}
		}
		if len(data) > 1<<20 || json.Unmarshal(data, &configuration) != nil || configuration == nil {
			return result, reason.New(reason.ReasonAdapterConfigInvalid, "adapter configuration must be a JSON object under 1 MiB")
		}
	}
	profiles, _, err := config.LoadProfiles(config.ProfilePaths{Profiles: filepath.Join(filepath.Dir(result.Path), "profiles.yaml")})
	if err != nil {
		return result, err
	}
	authority := cmd.String("authority")
	if authority != "local" {
		if _, ok := profiles[authority]; !ok {
			return result, reason.Invalid("unknown authority profile: " + authority)
		}
	}
	data, err := handle.ReadOwnerPrivate(result.Path, 1<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, reason.New(reason.ReasonConfigInvalid, "queue.yaml cannot be read safely: "+err.Error())
	}
	existing := err == nil
	var cfg config.QueueConfig
	var doc yaml.Node
	if existing {
		cfg, err = config.LoadQueue(os.Getenv)
		if err != nil {
			return result, err
		}
		err = yaml.Unmarshal(data, &doc)
	} else {
		err = yaml.Unmarshal([]byte("version: 1\nme:\n  {}\nsources:\n  []\nviews:\n  []\n"), &doc)
	}
	if err != nil {
		return result, reason.New(reason.ReasonConfigInvalid, err.Error())
	}
	mapping := doc.Content[0]
	if !existing {
		initBlockStyle(mapping)
	}
	for _, source := range cfg.Sources {
		if source.Adapter == "external" && source.Executable == path {
			return result, reason.New(reason.ReasonSourceAlreadyConfigured, "external executable already configured as source "+source.ID).With("sourceId", source.ID)
		}
	}
	process, err := queue.NewExternalProcessForCheck(config.QueueSource{ID: "init", Adapter: "external", Executable: path})
	if err != nil {
		return result, reason.New(reason.ReasonAdapterManifestInvalid, "external adapter could not start for manifest negotiation")
	}
	defer process.Close()
	manifest, err := process.Initialize(ctx)
	if err != nil {
		return result, reason.New(reason.ReasonAdapterManifestInvalid, "external adapter manifest negotiation failed")
	}
	currentDigest, err := queueAdapterExecutableDigest(path)
	if err != nil || currentDigest != digest {
		return result, reason.New(reason.ReasonAdapterManifestInvalid, "external adapter executable changed during manifest negotiation")
	}
	if err := queue.ValidateExternalAdapterConfig(manifest, configuration); err != nil {
		return result, reason.New(reason.ReasonAdapterConfigInvalid, err.Error())
	}
	id := cmd.String("source-id")
	if id == "" {
		id = manifest.ID
	}
	for _, source := range cfg.Sources {
		if source.ID == id {
			return result, reason.New(reason.ReasonSourceAlreadyConfigured, "source ID already configured: "+id).With("sourceId", id)
		}
	}
	view := cmd.String("view")
	if view == "" {
		view = "Ready"
	}
	result.DefaultView = len(cfg.Views) == 0 || cfg.Views[0].Name == view
	result.Adapter, result.SourceID = "external", id
	result.Facts = append(result.Facts, queueInitFact{"executable", path, "--executable"}, queueInitFact{"adapterId", manifest.ID, "adapter manifest"}, queueInitFact{"adapterVersion", manifest.Version, "adapter manifest"}, queueInitFact{"executableSHA256", digest, "executable bytes"}, queueInitFact{"sourceId", id, "adapter manifest"})
	result.Outcome = "created"
	if existing {
		result.Outcome = "merged"
	}
	var selected *yaml.Node
	for i, configuredView := range cfg.Views {
		if configuredView.Name == view {
			if configuredView.Authority != authority {
				return result, reason.Invalid("view authority differs from --authority")
			}
			selected = initField(initField(mapping, "views").Content[i], "sources")
			break
		}
	}
	if selected == nil {
		viewNode, err := initNode(config.QueueView{Name: view, Authority: authority, Sources: []string{id}, Filter: config.QueueFilter{Readiness: "ready", Claim: "free", Assigned: []string{"me", "nobody"}}})
		if err != nil {
			return result, err
		}
		views := initField(mapping, "views")
		views.Content = append(views.Content, viewNode)
		views.Style &^= yaml.FlowStyle
	} else {
		item, _ := initNode(id)
		selected.Content = append(selected.Content, item)
		selected.Style &^= yaml.FlowStyle
	}
	source := config.QueueSource{ID: id, Adapter: "external", Executable: path, ExpectedAdapterID: manifest.ID, ExpectedVersion: manifest.Version, Config: configuration}
	if claim := cmd.String("portable-claims"); claim != "" {
		source.Claims = &config.QueueClaims{Policy: "generic", Source: claim}
	}
	sourceNode, _ := initNode(map[string]any{})
	for _, field := range []struct {
		key   string
		value any
	}{
		{"id", id}, {"adapter", "external"}, {"executable", path}, {"expectedAdapterId", manifest.ID}, {"expectedVersion", manifest.Version}, {"config", configuration},
	} {
		node, err := initNode(field.value)
		if err != nil {
			return result, err
		}
		initSet(sourceNode, field.key, node)
	}
	if source.Claims != nil {
		node, _ := initNode(source.Claims)
		initSet(sourceNode, "claims", node)
	}
	sources := initField(mapping, "sources")
	sources.Content = append(sources.Content, sourceNode)
	sources.Style &^= yaml.FlowStyle
	result.YAML, err = queueInitRender(&doc)
	if err != nil {
		return result, err
	}
	if _, err := config.ValidateQueue([]byte(result.YAML), os.Getenv); err != nil {
		return result, reason.New(reason.ReasonConfigInvalid, err.Error())
	}
	result.Identity = "confirmation-required"
	result.Checklist = queue.MigrationChecklist
	if cmd.Bool("dry-run") {
		result.Identity = "pending"
		apply := "worklease queue"
		if cmd.String("view") != "" && view != "Ready" {
			apply += " --view " + queueInitQuote(view)
		}
		apply += " init --adapter external --executable " + queueInitQuote(path)
		if cmd.String("source-id") != "" {
			apply += " --source-id " + queueInitQuote(id)
		}
		if cmd.IsSet("adapter-config") {
			apply += " --adapter-config " + queueInitQuote(cmd.String("adapter-config"))
		}
		if cmd.IsSet("adapter-config-file") {
			apply += " --adapter-config-file " + queueInitQuote(cmd.String("adapter-config-file"))
		}
		if authority != "local" {
			apply += " --authority " + queueInitQuote(authority)
		}
		if source.Claims != nil {
			apply += " --portable-claims " + queueInitQuote(source.Claims.Source)
		}
		result.NextCommands = append(result.NextCommands, apply)
	} else {
		result.NextCommands = append(result.NextCommands, "worklease queue adapter approve --source "+queueInitQuote(id), fmt.Sprintf("worklease queue --view %s identity confirm --source %s --acknowledge", queueInitQuote(view), queueInitQuote(id)))
	}
	return result, nil
}
