package main

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
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
