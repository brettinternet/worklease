package authority

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
)

type enrollmentTimeoutError struct{}

func (enrollmentTimeoutError) Error() string {
	return "timeout https://user:secret@example.invalid/?invite=redacted"
}
func (enrollmentTimeoutError) Timeout() bool   { return true }
func (enrollmentTimeoutError) Temporary() bool { return true }

func TestEnrollmentMetadataTransportFailuresAreStableAndRedacted(t *testing.T) {
	cases := []struct {
		name string
		err  error
		kind string
	}{
		{name: "dns", err: &url.Error{Op: "Get", URL: "https://user:secret@example.invalid/?invite=secret", Err: &net.DNSError{Err: "no such host", Name: "example.invalid"}}, kind: "dns"},
		{name: "refused", err: &url.Error{Op: "Get", URL: "https://user:secret@example.invalid/?invite=secret", Err: syscall.ECONNREFUSED}, kind: "refused"},
		{name: "timeout", err: &url.Error{Op: "Get", URL: "https://user:secret@example.invalid/?invite=secret", Err: enrollmentTimeoutError{}}, kind: "timeout"},
		{name: "client-timeout", err: &url.Error{Op: "Get", URL: "https://user:secret@example.invalid/?invite=secret", Err: context.DeadlineExceeded}, kind: "timeout"},
		{name: "tls-pin", err: &url.Error{Op: "Get", URL: "https://user:secret@example.invalid/?invite=secret", Err: reason.New(reason.ReasonAuthorityMismatch, "remote certificate does not match pinned certificate")}, kind: "tls"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			profile := config.Profile{Name: "new", Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(root, "credential")}}
			pending := NewFilePendingStore(filepath.Join(root, "pending"))
			client, err := NewHTTPClient(profile, pending, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, test.err }))
			if err != nil {
				t.Fatal(err)
			}
			profilesPath := config.ProfilePaths{Profiles: filepath.Join(root, "profiles.yaml")}
			_, _, err = client.Enroll(context.Background(), strings.Repeat("b", 64), "agent", profilesPath)
			classified := reason.As(err)
			if classified == nil || classified.Reason != reason.ReasonRemoteTransportFailure || classified.Details["transport"] != test.kind {
				t.Fatalf("metadata transport classification=%#v", err)
			}
			for _, secret := range []string{"user:secret", "invite=secret", "example.invalid"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("raw transport data leaked: %q in %q", secret, err.Error())
				}
			}
			if _, statErr := os.Stat(profile.Credential.Path); !os.IsNotExist(statErr) {
				t.Fatalf("failed enrollment created credential: %v", statErr)
			}
			if _, statErr := os.Stat(profilesPath.Profiles); !os.IsNotExist(statErr) {
				t.Fatalf("failed enrollment created profile: %v", statErr)
			}
			records, listErr := pending.List()
			if listErr != nil || len(records) != 0 {
				t.Fatalf("failed metadata enrollment retained pending request: %d %v", len(records), listErr)
			}

			var text bytes.Buffer
			if writeErr := output.WriteTextError(&text, err); writeErr != nil {
				t.Fatal(writeErr)
			}
			if strings.Contains(text.String(), "https://") || strings.Contains(text.String(), "example.invalid") || strings.Contains(text.String(), "secret") || !strings.Contains(text.String(), "run worklease doctor") {
				t.Fatalf("unsafe or non-actionable text envelope: %s", text.String())
			}
			var jsonOut bytes.Buffer
			if writeErr := output.WriteError(&jsonOut, "enroll", err); writeErr != nil {
				t.Fatal(writeErr)
			}
			var envelope struct {
				Error *output.Failure `json:"error"`
			}
			if decodeErr := json.Unmarshal(jsonOut.Bytes(), &envelope); decodeErr != nil || envelope.Error == nil || envelope.Error.Reason != reason.ReasonRemoteTransportFailure || envelope.Error.ExitCode != reason.ExitAuthority || envelope.Error.Details["commitState"] != "not-committed" || envelope.Error.Details["transport"] != test.kind {
				t.Fatalf("JSON envelope=%s decode=%v", jsonOut.String(), decodeErr)
			}
		})
	}
}

func TestClientAllowsExplicitInsecureLANEndpoint(t *testing.T) {
	dir := t.TempDir()
	profile := config.Profile{Name: "lan", Endpoint: "http://192.168.1.20:8080", AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(dir, "cred")}}
	if _, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(dir, "pending")), nil); err != nil {
		t.Fatalf("explicit insecure LAN endpoint rejected: %v", err)
	}
	profile.AllowInsecureHTTP = false
	if _, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(dir, "pending-secure")), nil); err == nil {
		t.Fatal("insecure LAN endpoint accepted without explicit opt-in")
	}
}

func TestPinnedTLSAppliesBeforeEnrollmentAndLaterBearerRequests(t *testing.T) {
	const authorityID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const restoreID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/.well-known/worklease" && r.Header.Get("Authorization") != "Bearer "+strings.Repeat("d", 64) {
			t.Errorf("later request bearer=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"%s","restoreId":"%s","authorityTime":%q,"result":{}}`, authorityID, restoreID, time.Now().UTC().Format(time.RFC3339Nano))
	}))
	defer server.Close()

	root := t.TempDir()
	credentialPath := filepath.Join(root, "credential")
	if err := handle.StoreCredential(credentialPath, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	pin := sha256.Sum256(server.Certificate().Raw)
	profile := config.Profile{Name: "pinned", Endpoint: server.URL, AuthorityID: authorityID, RestoreID: restoreID, CertificateSHA256: hex.EncodeToString(pin[:]), Credential: config.CredentialDescriptor{Path: credentialPath}}
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Metadata(context.Background()); err != nil {
		t.Fatalf("pinned metadata: %v", err)
	}
	if _, err := client.Call(context.Background(), RequestSpec{Path: "/v1/claims/list"}); err != nil {
		t.Fatalf("pinned later request: %v", err)
	}
	before := calls
	profile.CertificateSHA256 = strings.Repeat("0", 64)
	wrong, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "wrong-pending")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Call(context.Background(), RequestSpec{Path: "/v1/claims/list"}); err == nil {
		t.Fatal("wrong certificate pin was accepted")
	}
	if calls != before {
		t.Fatal("bearer request reached handler before wrong pin rejection")
	}
}

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
