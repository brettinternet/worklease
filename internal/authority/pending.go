package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
)

const maxPendingBytes = 1 << 20

var errPendingRequestMismatch = errors.New("pending request already records a different effect")

type PendingRequest struct {
	RequestID             string          `json:"requestId"`
	OperationID           string          `json:"operationId,omitempty"`
	Kind                  string          `json:"kind"`
	AuthorityID           string          `json:"authorityId"`
	Endpoint              string          `json:"endpoint"`
	CertificateSHA256     string          `json:"certificateSha256,omitempty"`
	ExpectedRestoreID     string          `json:"expectedRestoreId"`
	RequestNotAfter       time.Time       `json:"requestNotAfter"`
	Request               []byte          `json:"request"`
	RequestSHA256         string          `json:"requestSha256"`
	ParentRequestID       string          `json:"parentRequestId,omitempty"`
	EffectEvidence        json.RawMessage `json:"effectEvidence,omitempty"`
	State                 string          `json:"state"` // pending, uncertain, terminal
	Route                 string          `json:"route"`
	TargetOperationID     string          `json:"targetOperationId,omitempty"`
	TargetHandleRef       string          `json:"targetHandleRef,omitempty"`
	CredentialRef         string          `json:"credentialRef,omitempty"`
	ClaimHandleRef        string          `json:"claimHandleRef,omitempty"`
	ClaimCredentialRef    string          `json:"claimCredentialRef,omitempty"`
	NewClaimHandleRef     string          `json:"newClaimHandleRef,omitempty"`
	NewClaimCredentialRef string          `json:"newClaimCredentialRef,omitempty"`
}

type PendingStore interface {
	Save(PendingRequest) error
	BindTrust(string, string, string) error
	Load(string) (PendingRequest, error)
	Clear(string) error
	List() ([]PendingRequest, error)
}

type FilePendingStore struct {
	Dir        string
	MaxRecords int
}

func NewFilePendingStore(dir string) *FilePendingStore {
	return &FilePendingStore{Dir: dir, MaxRecords: 256}
}
func (s *FilePendingStore) validateDir() error {
	if !filepath.IsAbs(s.Dir) || filepath.Clean(s.Dir) == string(filepath.Separator) {
		return fmt.Errorf("pending recovery directory must be an absolute private path")
	}
	return nil
}
func (s *FilePendingStore) Save(p PendingRequest) error {
	if err := s.validateDir(); err != nil {
		return err
	}
	if err := validatePending(p); err != nil {
		return err
	}
	if s.MaxRecords <= 0 {
		s.MaxRecords = 256
	}
	if err := handle.EnsureOwnerPrivateDir(s.Dir); err != nil {
		return err
	}
	path := s.path(p.RequestID)
	if old, loadErr := s.Load(p.RequestID); loadErr == nil {
		if old.RequestSHA256 != p.RequestSHA256 || !bytes.Equal(old.Request, p.Request) {
			return errPendingRequestMismatch
		}
		return nil
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	if records, err := s.List(); err == nil && len(records) >= s.MaxRecords {
		return fmt.Errorf("pending request recovery limit reached")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(data) > maxPendingBytes {
		return fmt.Errorf("pending request is too large")
	}
	if err := handle.WriteOwnerPrivateNoReplace(path, data, maxPendingBytes); err != nil {
		return err
	}
	return syncDir(s.Dir)
}
func (s *FilePendingStore) BindTrust(id, endpoint, certificateSHA256 string) error {
	p, err := s.Load(id)
	if err != nil {
		return err
	}
	if p.Endpoint != "" {
		if p.Endpoint == endpoint && p.CertificateSHA256 == certificateSHA256 {
			return nil
		}
		return fmt.Errorf("pending request trust is already bound differently")
	}
	p.Endpoint, p.CertificateSHA256 = endpoint, certificateSHA256
	if err := validatePending(p); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > maxPendingBytes {
		return fmt.Errorf("pending request trust update is invalid")
	}
	if err := handle.WriteOwnerPrivate(s.path(id), data, maxPendingBytes); err != nil {
		return err
	}
	return syncDir(s.Dir)
}

func (s *FilePendingStore) Load(id string) (PendingRequest, error) {
	var p PendingRequest
	if err := s.validateDir(); err != nil {
		return p, err
	}
	b, err := s.read(id)
	if err != nil {
		return p, err
	}
	if err = decodeStrict(b, &p); err != nil {
		return p, fmt.Errorf("pending record is malformed")
	}
	if err = validatePending(p); err != nil {
		return p, err
	}
	return p, nil
}
func (s *FilePendingStore) Clear(id string) error {
	if err := s.validateDir(); err != nil {
		return err
	}
	if !validRequestID(id) {
		return fmt.Errorf("request ID is invalid")
	}
	if err := handle.RemoveOwnerPrivate(s.path(id)); err != nil {
		return err
	}
	// Clearing a request that was never recorded is already the desired state.
	if err := syncDir(s.Dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (s *FilePendingStore) List() ([]PendingRequest, error) {
	if err := s.validateDir(); err != nil {
		return nil, err
	}
	names, err := handle.ListOwnerPrivateNames(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []PendingRequest{}
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		p, er := s.Load(strings.TrimSuffix(name, ".json"))
		if er != nil {
			return nil, er
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *FilePendingStore) path(id string) string { return filepath.Join(s.Dir, id+".json") }
func (s *FilePendingStore) read(id string) ([]byte, error) {
	if !validRequestID(id) {
		return nil, fmt.Errorf("request ID is invalid")
	}
	return handle.ReadOwnerPrivate(s.path(id), maxPendingBytes)
}
func validatePending(p PendingRequest) error {
	if !validRequestID(p.RequestID) || p.Kind == "" || !strings.HasPrefix(p.Route, "/v1/") || !validID(p.AuthorityID) || (p.Endpoint != "" && !validEndpoint(p.Endpoint)) || (p.CertificateSHA256 != "" && (!hex64(p.CertificateSHA256) || !strings.HasPrefix(p.Endpoint, "https://"))) || !validID(p.ExpectedRestoreID) || p.RequestNotAfter.IsZero() || len(p.Request) == 0 || len(p.Request) > maxPendingBytes {
		return fmt.Errorf("pending request is malformed")
	}
	for _, ref := range []string{p.CredentialRef, p.ClaimHandleRef, p.ClaimCredentialRef, p.NewClaimHandleRef, p.NewClaimCredentialRef, p.TargetHandleRef} {
		if ref != "" && !filepath.IsAbs(ref) {
			return fmt.Errorf("pending credential reference is not absolute")
		}
	}
	sum := sha256.Sum256(p.Request)
	expected := hex.EncodeToString(sum[:])
	if p.RequestSHA256 != expected {
		return fmt.Errorf("pending request hash is malformed")
	}
	return nil
}
func validEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme != "" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") && (u.Scheme == "http" || u.Scheme == "https")
}
func validRequestID(s string) bool { return validID(s) }
func validID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
