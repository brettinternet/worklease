package cli

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queueui"
)

func TestQueueClaimReacquiresAfterConfirmedBindingMigration(t *testing.T) {
	ctx := context.Background()
	item := queueClaimItem("tasks", "1")
	controller, backend, adapter := newLocalQueueClaimController(t, time.Minute, item)
	initial := controller.Preview(ctx, item)().(queueui.ClaimPreviewMsg)
	if initial.Err != nil || initial.Preview == nil {
		t.Fatalf("initial preview: %+v", initial)
	}
	grant := controller.AcquireClaim(ctx, item, *initial.Preview)().(queueui.ClaimResultMsg)
	if grant.Err != nil {
		t.Fatalf("first claim: %v", grant.Err)
	}
	path, err := queueClaimHandlePath(controller.home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
	if err != nil {
		t.Fatal(err)
	}
	old, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.API.Release(ctx, lease.Credentials{AuthorityID: old.AuthorityID, ClaimID: old.ClaimID, Token: old.Token, Revision: old.Revision}, lease.ReleaseRequest{OperationID: strings.Repeat("a", 32), Reason: "fixture claim ended", RequestNotAfter: time.Now().Add(time.Minute)}); err != nil {
		t.Fatalf("end old fixture claim: %v", err)
	}
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	identity := identities.Sources[adapter.source.ID]
	identity.Retired = []config.ClaimDomain{{Policy: identity.Policy, Source: identity.Source}}
	identity.Source = "new-portable-binding"
	identities.Sources[adapter.source.ID] = identity
	if err := config.SaveQueueIdentities(os.Getenv, identities); err != nil {
		t.Fatal(err)
	}
	source := controller.claimSources[adapter.source.ID]
	source.ClaimSource = identity.Source
	controller.claimSources[adapter.source.ID] = source
	migrated := controller.Preview(ctx, item)().(queueui.ClaimPreviewMsg)
	if migrated.Err != nil || migrated.Preview == nil || len(migrated.Preview.Resources) != 2 {
		t.Fatalf("confirmed binding migration could not preview both domains: %+v", migrated)
	}
	second := controller.AcquireClaim(ctx, item, *migrated.Preview)().(queueui.ClaimResultMsg)
	if second.Err != nil || !second.Claim.Active {
		t.Fatalf("confirmed binding migration could not reacquire: %+v", second)
	}
	updated, err := handle.Read(path)
	if err != nil || updated.ClaimID == old.ClaimID || len(updated.Resources) != 2 {
		t.Fatalf("old handle was not safely replaced: %+v, %v", updated, err)
	}
}
