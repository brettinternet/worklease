package authority

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
)

func TestEnrollmentRequiresPinnedAuthority(t *testing.T) {
	var calls int
	p := config.Profile{Name: "x", Endpoint: "https://authority.example", Credential: config.CredentialDescriptor{Path: filepath.Join(t.TempDir(), "cred")}}
	c, err := NewHTTPClient(p, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Enroll(context.Background(), "1111111111111111111111111111111111111111111111111111111111111111", "x", config.ProfilePaths{Profiles: filepath.Join(t.TempDir(), "profiles")}); err == nil || calls != 0 {
		t.Fatalf("unpinned enrollment dispatched: %v calls=%d", err, calls)
	}
}

func TestEnrollmentDurabilityAndActivation(t *testing.T) {
	dir := t.TempDir()
	p := config.Profile{Name: "new", Endpoint: "https://authority.example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Credential: config.CredentialDescriptor{Path: filepath.Join(dir, "cred")}}
	paths := config.ProfilePaths{Profiles: filepath.Join(dir, "profiles.yaml")}
	store := NewFilePendingStore(filepath.Join(dir, "pending"))
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/worklease" {
			return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{"supportedProtocolVersions":["worklease-http/1"]}}`), nil
		}
		if r.URL.Path == "/v1/enroll" {
			if r.Header.Get("Authorization") != "Invite 1111111111111111111111111111111111111111111111111111111111111111" {
				t.Errorf("invite header missing")
			}
			if r.Header.Get("Worklease-New-Installation-Authorization") == "" {
				t.Errorf("installation header missing")
			}
			return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:01Z","result":{"installationId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","role":"write","label":"agent","enrolledAt":"2026-01-01T00:00:01Z"}}`), nil
		}
		return response(404, `{"ok":false,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","error":{"reason":"invalid-argument","message":"no"}}`), nil
	})
	c, err := NewHTTPClient(p, store, transport)
	if err != nil {
		t.Fatal(err)
	}
	out, enrolled, err := c.Enroll(context.Background(), "1111111111111111111111111111111111111111111111111111111111111111", "agent", paths)
	if err != nil {
		t.Fatal(err)
	}
	if out.AuthorityID == "" || enrolled.Role != "write" {
		t.Fatalf("enrollment result: %#v %#v", out, enrolled)
	}
	if _, err := handle.ReadCredential(out.Credential.Path); err != nil {
		t.Fatal(err)
	}
	profiles, _, err := config.LoadProfiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	if profiles["new"].RestoreID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatal("profile activated without trusted restore identity")
	}
	if records, err := store.List(); err != nil || len(records) != 0 {
		t.Fatalf("terminal enrollment pending retained: %d %v", len(records), err)
	}
}
