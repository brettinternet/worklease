package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	urfave "github.com/urfave/cli/v3"
)

func TestLinearQueueStartPreviewsAndAppliesConfiguredState(t *testing.T) {
	h, _ := linearQueueFixture(t)
	queueConfig := "version: 1\nme: {}\nsources:\n  - id: linear-test\n    adapter: linear\n    organization: " + linearQueueOrg + "\n    team: " + linearQueueTeam + "\n    account: " + linearQueueAccount + "\n    credentialHelper: [/bin/echo, fixture-token]\n    workflow: {start: " + linearQueueStart + "}\nviews:\n  - name: Ready\n    authority: local\n    sources: [linear-test]\n    filter: {readiness: ready}\n"
	h.writeQueueConfig(queueConfig)
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	registry := queue.NewRegistry()
	read, ok := registry.Get("linear")
	if !ok {
		t.Fatal("Linear adapter unavailable")
	}
	source, err := read.Resolve(context.Background(), queueSourceOptions(cfg.Sources[0]))
	if err != nil {
		t.Fatal(err)
	}
	authorityBackend, authority, err := queueAuthorityForClaim(context.Background(), &urfave.Command{}, config.LocalProfileName)
	if err != nil {
		t.Fatal(err)
	}
	defer authorityBackend.Close()
	queueSession := strings.Repeat("e", 32)
	sources := map[string]queue.Source{source.ID: source}
	claimSources := queue.ClaimSources(cfg, []queue.Source{source})
	claimController := &queueClaimController{
		backend: authorityBackend, registry: registry, sources: sources, claimSources: claimSources,
		queueSession: queueSession, paths: config.UserProfilePaths(os.Getenv),
		current:     func() (queue.ClaimAuthority, uint64) { return authority, 1 },
		profileName: config.LocalProfileName, home: authorityBackend.Config.Home,
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	writeController := queueWriteController{
		backend: authorityBackend, registry: registry,
		current: func() (queue.ClaimAuthority, uint64) { return authority, 1 },
		journal: journal, sources: sources, configured: map[string]config.QueueSource{source.ID: cfg.Sources[0]},
		me: map[string][]string{source.ID: {linearQueueAccount}}, session: queueSession, profile: config.LocalProfileName,
	}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: source.ID, ItemID: linearQueueFirst}, Fresh: true}}
	startController := queueStartController{claim: claimController, write: writeController}
	preview := startController.Preview(context.Background(), item)().(queueui.StartPreviewMsg)
	if preview.Err != nil || preview.Preview == nil || preview.Preview.TransitionValue != linearQueueStart || !strings.Contains(preview.Preview.Effect, linearQueueStart) {
		t.Fatalf("Linear Start preview: %+v", preview)
	}
	result := startController.Start(context.Background(), item, *preview.Preview)().(queueui.StartResultMsg)
	if result.Err != nil || result.ClaimStep != "applied" || result.TransitionStep != "applied" || result.Write == nil || result.Write.Result.Outcome != queue.WriteVerified {
		t.Fatalf("Linear Start did not report separate verified claim and transition outcomes: %+v", result)
	}
	assignmentPreview := writeController.Preview(context.Background(), item, queue.ActionAssignToMe, "", "")().(queueui.WritePreviewMsg)
	if assignmentPreview.Err != nil || assignmentPreview.Preview == nil {
		t.Fatalf("Linear assignment preview: %+v", assignmentPreview)
	}
	progressPreview := writeController.Preview(context.Background(), item, queue.ActionRecordProgress, "", "work done")().(queueui.WritePreviewMsg)
	if progressPreview.Err != nil || progressPreview.Preview == nil || progressPreview.Preview.Intent.Patch["append"] != "comment" {
		t.Fatalf("Linear progress preview did not prepare a marked comment: %+v", progressPreview)
	}
	if _, err := queueBuiltinRecoveryAdapter(context.Background(), progressPreview.Preview.Intent); err != nil {
		t.Fatalf("unchanged Linear recovery source rejected: %v", err)
	}
	h.writeQueueConfig(strings.Replace(queueConfig, "fixture-token", "changed-token", 1))
	if _, err := queueBuiltinRecoveryAdapter(context.Background(), progressPreview.Preview.Intent); err == nil {
		t.Fatal("Linear recovery accepted changed helper binding for a pending checkpoint")
	}
	stale := writeController.Confirm(context.Background(), *assignmentPreview.Preview)().(queueui.WriteResultMsg)
	if stale.Err == nil || !strings.Contains(stale.Err.Error(), "queue source binding changed") {
		t.Fatalf("changed credential-helper argv did not invalidate the preview: %+v", stale)
	}
}
