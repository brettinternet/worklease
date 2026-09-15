package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func TestStrictJSONRejectsDuplicateAndTrailingValues(t *testing.T) {
	for _, input := range []string{`{"a":1,"a":2}`, `{"a":1} {"b":2}`} {
		if err := strictJSON([]byte(input)); err == nil {
			t.Fatalf("strictJSON(%q) accepted invalid input", input)
		}
	}
	if err := strictJSON([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
}

func testConfig() Config {
	return Config{Home: "/authority", Listen: "127.0.0.1:8443", Prefixes: []string{"coordination:"}, MaxTTL: "1m", MaxHold: "1h", HealthRate: 10, MetadataRate: 10, EnrollmentRate: 10}
}

func TestValidateConfigRequiresTLSUnlessInsecureHTTPIsAllowed(t *testing.T) {
	cfg := testConfig()
	if err := validateConfig(cfg, false); err == nil {
		t.Fatal("production configuration without TLS was accepted")
	}
	if err := validateConfig(cfg, true); err != nil {
		t.Fatalf("explicit insecure HTTP configuration rejected: %v", err)
	}
	cfg.Listen = "0.0.0.0:8443"
	if err := validateConfig(cfg, true); err != nil {
		t.Fatalf("LAN insecure HTTP configuration rejected: %v", err)
	}
	cfg.HealthRate = 0
	if err := validateConfig(cfg, true); err == nil {
		t.Fatal("configuration without explicit public endpoint rate limits was accepted")
	}
}

func TestValidateConfigRejectsInvalidAdvertisedEndpointPort(t *testing.T) {
	cfg := testConfig()
	cfg.AllowInsecureHTTP = true
	cfg.AdvertisedEndpoint = "http://worklease.example:65536"
	if err := validateConfig(cfg, true); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("invalid advertised endpoint port error = %v", err)
	}
}

func TestLoadConfigAllowsExplicitInsecureHTTP(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "server.yaml")
	contents := "home: /authority\nlisten: 127.0.0.1:8443\nallowInsecureHTTP: true\nadmittedPrefixes:\n  - \"coordination:\"\nmaxTTL: 1m\nmaxHold: 1h\nhealthRate: 10\nmetadataRate: 10\nenrollmentRate: 10\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowInsecureHTTP {
		t.Fatal("stored insecure HTTP choice was not loaded")
	}
}

func TestProtocolConfigDurationMicrosAndLimiter(t *testing.T) {
	d, err := parseDuration("", 1500000, "maxTTL")
	if err != nil || d != 1500*time.Millisecond {
		t.Fatalf("duration micros = %v, %v", d, err)
	}
	l := &limiter{limits: RateConfig{Health: 1}}
	if ok, _ := l.allow("health", "127.0.0.1"); !ok {
		t.Fatal("first request was rate limited")
	}
	if ok, retry := l.allow("health", "127.0.0.1"); ok || retry < 1 {
		t.Fatal("second request was not rate limited")
	}
	if strings.Contains(routeTemplate("/v1/claims/acquire"), "coordination:") {
		t.Fatal("route template exposed request data")
	}
}

func TestEnrollmentAcquireAndHeartbeatUseDistinctBearers(t *testing.T) {
	srv, invite := newHostedTestServer(t)
	metadata := request(t, srv, http.MethodGet, "/.well-known/worklease", nil, nil)
	if metadata.Code != http.StatusOK {
		t.Fatalf("metadata status = %d: %s", metadata.Code, metadata.Body.String())
	}
	var discovered envelope
	if err := json.Unmarshal(metadata.Body.Bytes(), &discovered); err != nil {
		t.Fatal(err)
	}
	installationCredential := strings.Repeat("b", 64)
	installationID := strings.Repeat("1", 32)
	deadline := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	enroll := map[string]any{"protocolVersion": ProtocolVersion, "authorityId": discovered.AuthorityID, "expectedRestoreId": discovered.RestoreID, "requestId": strings.Repeat("2", 32), "requestNotAfter": deadline, "installationId": installationID, "label": "test"}
	enrolled := request(t, srv, http.MethodPost, "/v1/enroll", enroll, map[string]string{"Authorization": "Invite " + invite, "Worklease-New-Installation-Authorization": "Bearer " + installationCredential})
	if enrolled.Code != http.StatusOK {
		t.Fatalf("enroll status = %d: %s", enrolled.Code, enrolled.Body.String())
	}
	claimToken := strings.Repeat("c", 64)
	claimID := strings.Repeat("3", 32)
	common := map[string]any{"protocolVersion": ProtocolVersion, "authorityId": discovered.AuthorityID, "expectedRestoreId": discovered.RestoreID}
	acquire := cloneMap(common)
	acquire["claimId"], acquire["resources"] = claimID, []string{"coordination:test"}
	acquire["agentId"], acquire["sessionId"], acquire["workKey"] = "agent", "session", "work"
	acquire["ttlMicros"], acquire["maxHoldMicros"], acquire["requestNotAfter"] = int64(time.Minute/time.Microsecond), int64(time.Hour/time.Microsecond), deadline
	acquired := request(t, srv, http.MethodPost, "/v1/claims/acquire", acquire, map[string]string{"Authorization": "Bearer " + installationCredential, "Worklease-New-Claim-Authorization": "Bearer " + claimToken})
	if acquired.Code != http.StatusOK {
		t.Fatalf("acquire status = %d: %s", acquired.Code, acquired.Body.String())
	}
	if strings.Contains(acquired.Body.String(), "localReplaceAllowed") || strings.Contains(acquired.Body.String(), "installationId") {
		t.Fatalf("remote grant exposed local-only fields: %s", acquired.Body.String())
	}
	var grant struct {
		Result struct {
			Revision int64 `json:"revision"`
		} `json:"result"`
	}
	if err := json.Unmarshal(acquired.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	heartbeat := cloneMap(common)
	heartbeat["claimId"], heartbeat["revision"] = claimID, grant.Result.Revision
	heartbeat["operationId"], heartbeat["ttlMicros"], heartbeat["requestNotAfter"] = strings.Repeat("4", 32), int64(time.Minute/time.Microsecond), deadline
	renewed := request(t, srv, http.MethodPost, "/v1/claims/heartbeat", heartbeat, map[string]string{"Authorization": "Bearer " + installationCredential, "Worklease-Claim-Authorization": "Bearer " + claimToken})
	if renewed.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d: %s", renewed.Code, renewed.Body.String())
	}

	watchBody := cloneMap(common)
	watchBody["resources"], watchBody["until"], watchBody["timeoutMicros"] = []string{"coordination:test"}, "change", int64(30*time.Second/time.Microsecond)
	encodedWatch, _ := json.Marshal(watchBody)
	watchContext, cancelWatch := context.WithCancel(context.Background())
	watchRequest := httptest.NewRequest(http.MethodPost, "/v1/watch", bytes.NewReader(encodedWatch)).WithContext(watchContext)
	watchRequest.Header.Set("Worklease-Protocol-Version", ProtocolVersion)
	watchRequest.Header.Set("Accept", "application/json")
	watchRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	watchRequest.Header.Set("Authorization", "Bearer "+installationCredential)
	watchResponse := httptest.NewRecorder()
	watchDone := make(chan struct{})
	go func() {
		srv.Handler().ServeHTTP(watchResponse, watchRequest)
		close(watchDone)
	}()
	time.Sleep(20 * time.Millisecond)
	cancelWatch()
	select {
	case <-watchDone:
	case <-time.After(time.Second):
		t.Fatal("cancelled watch did not stop")
	}
	if watchResponse.Code != http.StatusRequestTimeout || !strings.Contains(watchResponse.Body.String(), reason.ReasonCancelled) {
		t.Fatalf("cancelled watch response = %d: %s", watchResponse.Code, watchResponse.Body.String())
	}

	bad := cloneMap(heartbeat)
	bad["unknown"] = true
	rejected := request(t, srv, http.MethodPost, "/v1/claims/heartbeat", bad, map[string]string{"Authorization": "Bearer " + installationCredential, "Worklease-Claim-Authorization": "Bearer " + claimToken})
	if rejected.Code != http.StatusBadRequest || !strings.Contains(rejected.Body.String(), reason.ReasonInvalidArgument) {
		t.Fatalf("unknown field response = %d: %s", rejected.Code, rejected.Body.String())
	}

	gcRequest := cloneMap(common)
	gcRequest["operationId"], gcRequest["requestNotAfter"], gcRequest["retentionDays"], gcRequest["apply"] = strings.Repeat("5", 32), deadline, 30.0, true
	for attempt := 0; attempt < 2; attempt++ {
		collected := request(t, srv, http.MethodPost, "/v1/admin/gc", gcRequest, map[string]string{"Authorization": "Bearer " + installationCredential})
		if collected.Code != http.StatusOK {
			t.Fatalf("GC attempt %d status = %d: %s", attempt+1, collected.Code, collected.Body.String())
		}
	}
	gcRequest["retentionDays"] = 31.0
	mismatch := request(t, srv, http.MethodPost, "/v1/admin/gc", gcRequest, map[string]string{"Authorization": "Bearer " + installationCredential})
	if mismatch.Code != http.StatusConflict || !strings.Contains(mismatch.Body.String(), reason.ReasonOperationRequestMismatch) {
		t.Fatalf("changed GC replay response = %d: %s", mismatch.Code, mismatch.Body.String())
	}
}

func TestOperationRenewAndCompleteWireFieldsAreUnambiguous(t *testing.T) {
	base := `"protocolVersion":"worklease-http/1","authorityId":"a","expectedRestoreId":"b","claimId":"c","revision":2,"operationId":"d","requestNotAfter":"2026-09-14T00:00:00Z"`
	var renew opRenewWire
	if err := decode([]byte(`{`+base+`,"renewalId":"e","ttlMicros":1000000}`), &renew); err != nil {
		t.Fatal(err)
	}
	if renew.OperationID != "d" || renew.RenewalID != "e" {
		t.Fatalf("renew fields decoded ambiguously: %+v", renew)
	}
	var complete completeWire
	if err := decode([]byte(`{`+base+`,"receipt":{"exitCode":0}}`), &complete); err != nil {
		t.Fatal(err)
	}
	if complete.OperationID != "d" || complete.Receipt["exitCode"] == nil {
		t.Fatalf("completion fields decoded ambiguously: %+v", complete)
	}
}

func TestServeReportsBoundAddressTransportAndAdvertisedEndpoint(t *testing.T) {
	srv, _ := newHostedTestServer(t)
	srv.http.Addr = "127.0.0.1:0"
	srv.cfg.AdvertisedEndpoint = "http://worklease.example:8443"
	var logs bytes.Buffer
	srv.logger = log.New(&logs, "", 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, true) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "transport=http advertisedEndpoint=http://worklease.example:8443") || strings.Contains(logs.String(), "address=127.0.0.1:0 ") {
		t.Fatalf("startup diagnostics = %q", logs.String())
	}
}

func TestServeTLSFailureDoesNotReportReadiness(t *testing.T) {
	srv, _ := newHostedTestServer(t)
	srv.http.Addr = "127.0.0.1:0"
	root := t.TempDir()
	srv.cfg.TLSCert = filepath.Join(root, "server.crt")
	srv.cfg.TLSKey = filepath.Join(root, "server.key")
	for _, path := range []string{srv.cfg.TLSCert, srv.cfg.TLSKey} {
		if err := os.WriteFile(path, []byte("not TLS material\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var logs bytes.Buffer
	srv.logger = log.New(&logs, "", 0)
	if err := srv.Serve(context.Background(), false); err == nil {
		t.Fatal("invalid TLS material unexpectedly served")
	}
	if strings.Contains(logs.String(), "listening address=") {
		t.Fatalf("TLS failure implied readiness: %s", logs.String())
	}
}

func TestServeCancellationReleasesHostedLock(t *testing.T) {
	srv, _ := newHostedTestServer(t)
	srv.http.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, true) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not shut down within its bound")
	}
	lock, err := store.AcquireHostedLock(context.Background(), srv.cfg.Home)
	if err != nil {
		t.Fatalf("server did not release hosted lock: %v", err)
	}
	_ = lock.Close()
}

func TestHostedLockIsHeldForServerLifetime(t *testing.T) {
	srv, _ := newHostedTestServer(t)
	if lock, err := store.AcquireHostedLock(context.Background(), srv.cfg.Home); err == nil {
		_ = lock.Close()
		t.Fatal("second hosted writer acquired the server lock")
	} else if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonHostedLockHeld {
		t.Fatalf("second lock error = %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(context.Background(), srv.cfg.Home)
	if err != nil {
		t.Fatalf("lock was not released after server close: %v", err)
	}
	_ = lock.Close()
}

func newHostedTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), home, store.Options{HostedWriter: true, HostedLock: lock})
	if err != nil {
		t.Fatal(err)
	}
	invite := strings.Repeat("a", 64)
	if _, err := lease.New(st, nil, nil, lease.Defaults{}).HostedInitialize(context.Background(), invite); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteHostedReady(lock); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Home = home
	srv, err := New(context.Background(), cfg, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, invite
}

func request(t *testing.T, srv *Server, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Worklease-Protocol-Version", ProtocolVersion)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, req)
	return response
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
