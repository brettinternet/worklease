package authority

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/reason"
)

// InviteArtifact is the compact, owner-private handoff token used to enroll a
// client. Its authenticity comes from the transfer channel, not this encoding.
type InviteArtifact struct {
	Version           int    `json:"v"`
	Endpoint          string `json:"endpoint"`
	AuthorityID       string `json:"authorityId"`
	CertificateSHA256 string `json:"certificateSha256,omitempty"`
	ProfileHint       string `json:"profileHint,omitempty"`
	Invite            string `json:"invite"`
}

const (
	inviteArtifactPrefix   = "worklease-invite-v1."
	MaxInviteArtifactBytes = 4096
)

func EncodeInviteArtifact(a InviteArtifact) (string, error) {
	a.Version = 1
	if err := validateInviteArtifact(a); err != nil {
		return "", err
	}
	body, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	token := inviteArtifactPrefix + base64.RawURLEncoding.EncodeToString(body)
	if len(token) > MaxInviteArtifactBytes {
		return "", reason.New(reason.ReasonCredentialUnsafe, "invite artifact is oversized")
	}
	return token, nil
}

func DecodeInviteArtifact(input string) (InviteArtifact, error) {
	if len(input) == 0 || len(input) > MaxInviteArtifactBytes || strings.ContainsAny(input, "\r\n") || !strings.HasPrefix(input, inviteArtifactPrefix) {
		return InviteArtifact{}, reason.New(reason.ReasonCredentialMalformed, "invite artifact is malformed")
	}
	encoded := strings.TrimPrefix(input, inviteArtifactPrefix)
	if encoded == "" {
		return InviteArtifact{}, reason.New(reason.ReasonCredentialMalformed, "invite artifact is malformed")
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(body) > MaxInviteArtifactBytes {
		return InviteArtifact{}, reason.New(reason.ReasonCredentialMalformed, "invite artifact is malformed")
	}
	var a InviteArtifact
	if err := decodeStrict(body, &a); err != nil {
		return InviteArtifact{}, reason.New(reason.ReasonCredentialMalformed, "invite artifact is malformed")
	}
	if a.Version != 1 {
		return InviteArtifact{}, reason.New(reason.ReasonCredentialMalformed, "invite artifact version is unsupported")
	}
	if err := validateInviteArtifact(a); err != nil {
		return InviteArtifact{}, err
	}
	return a, nil
}

func validateInviteArtifact(a InviteArtifact) error {
	if a.Version != 1 || strings.TrimSpace(a.Endpoint) != a.Endpoint || strings.TrimSpace(a.AuthorityID) != a.AuthorityID || !validID(a.AuthorityID) {
		return reason.New(reason.ReasonCredentialMalformed, "invite artifact identity is invalid")
	}
	u, err := url.Parse(a.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return reason.New(reason.ReasonCredentialMalformed, "invite artifact endpoint is invalid")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return reason.New(reason.ReasonCredentialMalformed, "invite artifact endpoint must use HTTP or HTTPS")
	}
	if a.CertificateSHA256 != "" && (!hex64(a.CertificateSHA256) || u.Scheme != "https") {
		return reason.New(reason.ReasonCredentialMalformed, "invite artifact certificate pin requires HTTPS and 64 lowercase hexadecimal characters")
	}
	if a.ProfileHint != "" && config.ValidateProfileName(a.ProfileHint) != nil {
		return reason.New(reason.ReasonCredentialMalformed, "invite artifact profile hint is invalid")
	}
	if len(a.Invite) != 64 || !hex64(a.Invite) {
		return reason.New(reason.ReasonCredentialMalformed, "invite artifact invite is invalid")
	}
	return nil
}

func hex64(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
