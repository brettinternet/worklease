package authority

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
)

func executorForTest(t *testing.T, status int, bodyTemplate string) (*RemoteAuthority, *FilePendingStore) {
	t.Helper()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	pending := NewFilePendingStore(filepath.Join(t.TempDir(), "pending"))
	client, err := NewHTTPClient(profile, pending, roundTripFunc(func(*http.Request) (*http.Response, error) {
		now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z07:00")
		return response(status, strings.ReplaceAll(bodyTemplate, "AUTHORITY_TIME", now)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Clock().Sample(time.Now().UTC(), time.Now().Add(-time.Millisecond), time.Now()); err != nil {
		t.Fatal(err)
	}
	return &RemoteAuthority{Client: client}, pending
}

func adminSpecForTest(id string) RequestSpec {
	body, _ := json.Marshal(map[string]any{"protocolVersion": protocolVersion, "authorityId": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "expectedRestoreId": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "operationId": id, "requestNotAfter": time.Now().Add(time.Hour).UTC()})
	return RequestSpec{Path: "/v1/admin/invites/issue", Kind: "invite-issue", RequestID: id, Body: body, Mutating: true, Terminal: true}
}

// A definitively rejected mutation can never commit, so its bounded recovery
// record must be cleared; an uncertain outcome must retain it for exact replay.
func TestExecuteClearsDefinitiveRejectionAndRetainsUncertainty(t *testing.T) {
	rejected := "{\"ok\":false,\"protocolVersion\":\"worklease-http/1\",\"authorityId\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"restoreId\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"authorityTime\":\"AUTHORITY_TIME\",\"error\":{\"reason\":\"invalid-argument\",\"message\":\"role is invalid\"}}"
	rejecting, rejectedStore := executorForTest(t, 400, rejected)
	for _, id := range []string{strings.Repeat("1", 32), strings.Repeat("2", 32)} {
		_, err := rejecting.Execute(context.Background(), adminSpecForTest(id))
		if err == nil || !reason.DefinitiveNoCommit(err) {
			t.Fatalf("definitive rejection not classified: %v", err)
		}
	}
	if records, err := rejectedStore.List(); err != nil || len(records) != 0 {
		t.Fatalf("definitive rejections retained %d records (err=%v)", len(records), err)
	}

	unavailable := "{\"ok\":false,\"protocolVersion\":\"worklease-http/1\",\"authorityTime\":\"AUTHORITY_TIME\",\"error\":{\"reason\":\"storage-failure\",\"message\":\"unavailable\"}}"
	lost, lostStore := executorForTest(t, 503, unavailable)
	uncertainID := strings.Repeat("3", 32)
	if _, err := lost.Execute(context.Background(), adminSpecForTest(uncertainID)); err == nil {
		t.Fatal("uncertain outcome reported success")
	}
	if _, err := lostStore.Load(uncertainID); err != nil {
		t.Fatalf("uncertain outcome lost its recovery record: %v", err)
	}
}
