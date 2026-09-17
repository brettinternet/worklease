package authority

import (
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
)

func TestInviteArtifactRoundTripAndBounds(t *testing.T) {
	artifact := InviteArtifact{Version: 1, Endpoint: "https://authority.example:8443", AuthorityID: strings.Repeat("a", 32), CertificateSHA256: strings.Repeat("b", 64), ProfileHint: "team", Invite: strings.Repeat("c", 64)}
	token, err := EncodeInviteArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(token, "\r\n") || len(token) > MaxInviteArtifactBytes {
		t.Fatalf("unsafe token %q", token)
	}
	got, err := DecodeInviteArtifact(token)
	if err != nil {
		t.Fatal(err)
	}
	if got != artifact {
		t.Fatalf("round trip got=%+v want=%+v", got, artifact)
	}
	for _, malformed := range []string{token + "x", token + "\n", "worklease-invite-v2." + token[strings.IndexByte(token, '.')+1:], "worklease-invite-v1." + strings.Repeat("A", MaxInviteArtifactBytes)} {
		if _, err := DecodeInviteArtifact(malformed); err == nil {
			t.Fatalf("accepted malformed artifact %q", malformed[:min(len(malformed), 32)])
		}
	}
}

func TestHTTPClientRejectsReservedLocalProfile(t *testing.T) {
	profile := config.Profile{Name: config.LocalProfileName, Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(t.TempDir(), "credential")}}
	if _, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), nil); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved profile accepted: %v", err)
	}
}

func TestPinnedHTTPSRejectsCustomTransportBeforeDispatch(t *testing.T) {
	profile := config.Profile{Name: "team", Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), CertificateSHA256: strings.Repeat("b", 64), Credential: config.CredentialDescriptor{Path: filepath.Join(t.TempDir(), "credential")}}
	called := false
	_, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil }))
	if err == nil || called {
		t.Fatalf("pinned custom transport accepted or dispatched: err=%v called=%v", err, called)
	}
}

func TestPinnedHTTPSRejectsCustomTLSDialerBeforeDispatch(t *testing.T) {
	profile := config.Profile{Name: "team", Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), CertificateSHA256: strings.Repeat("b", 64), Credential: config.CredentialDescriptor{Path: filepath.Join(t.TempDir(), "credential")}}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	called := false
	legacyDialer := func(string, string) (net.Conn, error) { called = true; return nil, nil }
	reflect.ValueOf(transport).Elem().FieldByName("DialTLS").Set(reflect.ValueOf(legacyDialer))
	_, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), transport)
	if err == nil || called {
		t.Fatalf("custom TLS dialer accepted or dispatched: err=%v called=%v", err, called)
	}
}

func TestInviteArtifactAllowsHTTPButClientOptInRemainsSeparate(t *testing.T) {
	artifact := InviteArtifact{Version: 1, Endpoint: "http://127.0.0.1:8080", AuthorityID: strings.Repeat("a", 32), ProfileHint: "local", Invite: strings.Repeat("c", 64)}
	if token, err := EncodeInviteArtifact(artifact); err != nil {
		t.Fatal(err)
	} else if _, err := DecodeInviteArtifact(token); err != nil {
		t.Fatal(err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
