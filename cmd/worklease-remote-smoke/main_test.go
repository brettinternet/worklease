package main

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateSSHHostRejectsShellSyntax(t *testing.T) {
	for _, host := range []string{"", "-oProxyCommand=bad", "remote-host;touch /tmp/pwned", "remote-host\nexample"} {
		if err := validateSSHHost(host); err == nil {
			t.Errorf("validateSSHHost(%q) accepted unsafe host", host)
		}
	}
	if err := validateSSHHost("remote-host"); err != nil {
		t.Fatalf("validateSSHHost(remote-host): %v", err)
	}
}

func TestVerifyFaultReplaysRequiresMatchingBody(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	log := "path=/v1/operations/begin status=200 requestSha256=aaa dropped=true at=now\n" +
		"path=/v1/operations/begin status=422 requestSha256=aaa dropped=false at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultReplays(logPath, []string{"/v1/operations/begin"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultReplays(logPath, []string{"/v1/operations/complete"}); err == nil {
		t.Fatal("missing replay accepted")
	}
}

func TestVerifyFreshReplayEnvelopeRequiresStableResultAndNewerTime(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	log := "path=/v1/operations/complete status=200 requestSha256=aaa dropped=true authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:00Z historicalResultSha256=result at=now\n" +
		"path=/v1/operations/complete status=200 requestSha256=aaa dropped=false authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:01Z historicalResultSha256=result at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFreshReplayEnvelope(logPath, "/v1/operations/complete", "authority"); err != nil {
		t.Fatal(err)
	}
	if err := verifyFreshReplayEnvelope(logPath, "/v1/operations/complete", "other"); err == nil {
		t.Fatal("wrong authority identity accepted")
	}
}

func TestHistoricalResultHashIgnoresReplayMarker(t *testing.T) {
	first := historicalResultHash([]byte(`{"operationId":"abc","idempotent":false,"result":{"exitStatus":0}}`))
	replay := historicalResultHash([]byte(`{"result":{"exitStatus":0},"idempotent":true,"operationId":"abc"}`))
	if first == "" || first != replay {
		t.Fatalf("historical hashes differ: first=%q replay=%q", first, replay)
	}
}

func TestVerifyClockBoundEvidenceRequiresLowerBoundAndNoExpiredDispatch(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	generated, expired, late := strings.Repeat("4", 32), strings.Repeat("5", 32), strings.Repeat("6", 32)
	log := "path=/.well-known/worklease status=200 requestSha256=aaa dropped=false responseDelay=1.2s authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:00Z historicalResultSha256=result at=2026-09-14T13:00:01.2Z\n" +
		"path=/v1/claims/heartbeat status=200 requestSha256=bbb dropped=false responseDelay=0s requestId=" + generated + " requestNotAfter=2026-09-15T12:00:00.1Z authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:01Z historicalResultSha256=result at=now\n" +
		"path=/v1/operations/begin status=200 requestSha256=ccc dropped=false responseDelay=2.5s requestId=" + late + " requestNotAfter=2026-09-15T12:00:02Z authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:02Z historicalResultSha256=result at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyClockBoundEvidence(logPath, generated, expired, late, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(log+"path=/v1/claims/heartbeat requestId="+expired+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyClockBoundEvidence(logPath, generated, expired, late, 2*time.Second); err == nil {
		t.Fatal("expired request dispatch accepted")
	}
}

func TestVerifyNoFaultDispatchRejectsMatchingPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	if err := os.WriteFile(logPath, []byte("path=/v1/admin/gc status=200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoFaultDispatch(logPath, "/v1/claims/acquire"); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoFaultDispatch(logPath, "/v1/admin/gc"); err == nil {
		t.Fatal("matching authority dispatch accepted")
	}
}

func TestWithBlockedPendingRootRestoresDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pending")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "retained")
	if err := os.WriteFile(marker, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := withBlockedPendingRoot(root, func() error {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("pending root was not blocked by a regular file")
		}
		return errors.New("injected callback failure")
	}); err == nil || err.Error() != "injected callback failure" {
		t.Fatalf("callback error not preserved: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "ok" {
		t.Fatalf("pending root was not restored: data=%q err=%v", data, err)
	}
}

func TestVerifyFaultGatesRequiresMatchedRequestHashes(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	log := "path=/v1/claims/acquire phase=held requestSha256=aaa at=now\n" +
		"path=/v1/claims/acquire phase=released requestSha256=aaa at=now\n" +
		"path=/v1/claims/acquire phase=held requestSha256=bbb at=now\n" +
		"path=/v1/claims/acquire phase=released requestSha256=bbb at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultGates(logPath, "/v1/claims/acquire", 2); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultGates(logPath, "/v1/claims/acquire", 1); err == nil {
		t.Fatal("wrong gate count accepted")
	}
}

func TestFaultProxyConsumesBoundedResponseFault(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	handler := &faultProxyHandler{control: control, log: filepath.Join(dir, "fault.log")}
	if err := os.WriteFile(control, []byte("delay-response /v1/test 25ms -1h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	delay, offset := handler.consumeResponseFault("/v1/test", "request-hash")
	if delay != 25*time.Millisecond || offset != -time.Hour {
		t.Fatalf("delay=%s offset=%s", delay, offset)
	}
	delay, offset = handler.consumeResponseFault("/v1/test", "request-hash")
	if delay != 0 || offset != 0 {
		t.Fatalf("response fault was not one-shot: delay=%s offset=%s", delay, offset)
	}
}

func TestFaultProxyHoldsBeforeForwardingUntilReleased(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	handler := &faultProxyHandler{control: control, log: filepath.Join(dir, "fault.log")}
	if err := os.WriteFile(control, []byte("hold /v1/claims/acquire\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- handler.waitIfHeld("/v1/claims/acquire", "request-hash") }()
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(control)
		if err == nil && string(data) == "held /v1/claims/acquire\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("proxy did not report held request")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("held request returned before release: %v", err)
	default:
	}
	if err := os.WriteFile(control, []byte("release /v1/claims/acquire\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not release held request")
	}
}

func TestWriteCertificateIncludesRemoteAddressSAN(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if err := writeCertificate(certPath, keyPath, net.ParseIP("192.0.2.10"), "remote-host"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("certificate file contained no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("192.0.2.10"); err != nil {
		t.Fatalf("certificate SANs do not include remote address: %v", cert.IPAddresses)
	}
}
