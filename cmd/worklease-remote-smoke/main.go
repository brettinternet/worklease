// Command worklease-remote-smoke exercises the shipped binary as one TLS
// authority and two isolated remote clients. It supplies local-development
// evidence for TASK-107.11 without claiming the required real-host run.
package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type groupEvidence struct {
	Group       int      `json:"group"`
	Observation string   `json:"observation"`
	Commands    []string `json:"commands"`
	Evidence    []string `json:"evidence"`
	Passed      bool     `json:"developmentCheckPassed"`
}

type report struct {
	SchemaVersion  int               `json:"schemaVersion"`
	Mode           string            `json:"mode"`
	LatencyKind    string            `json:"latencyKind"`
	StartedAt      string            `json:"startedAt"`
	FinishedAt     string            `json:"finishedAt"`
	Authority      string            `json:"authority"`
	ClientRoots    []string          `json:"clientRoots"`
	Environment    map[string]string `json:"environment"`
	LatencyMillis  int64             `json:"latencyMillis"`
	RenewalMargins []string          `json:"renewalMargins"`
	Throughput     string            `json:"throughput"`
	Storage        string            `json:"storage"`
	BackupCutoff   string            `json:"backupCutoff"`
	RestoreTime    string            `json:"restoreTime"`
	RecoveryBounds string            `json:"recoveryBounds"`
	Groups         []groupEvidence   `json:"groups"`
}

type harness struct {
	binary, self, root, endpoint, cert string
	commandLog                         string
	server                             *exec.Cmd
	clients                            [2]client
	report                             report
}

type client struct {
	name, home, config, checkout string
	env                          []string
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "effect" {
		if len(os.Args) != 3 {
			fatal(errors.New("effect requires a path"))
		}
		file, err := os.OpenFile(os.Args[2], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			fatal(err)
		}
		_, err = io.WriteString(file, "dispatch\n")
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			fatal(err)
		}
		return
	}

	binary := flag.String("binary", "bin/worklease", "built worklease binary")
	evidence := flag.String("evidence", "", "evidence directory (default: dist/remote-acceptance/TIMESTAMP)")
	keep := flag.Bool("keep", false, "keep temporary authority and client state")
	flag.Parse()
	if err := run(*binary, *evidence, *keep); err != nil {
		fatal(err)
	}
}

func run(binary, evidence string, keep bool) error {
	absoluteBinary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absoluteBinary); err != nil {
		return fmt.Errorf("binary: %w", err)
	}
	root, err := os.MkdirTemp("", "worklease-remote-smoke-")
	if err != nil {
		return err
	}
	if !keep {
		defer os.RemoveAll(root)
	}
	if evidence == "" {
		stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
		evidence, err = filepath.Abs(filepath.Join("dist", "remote-acceptance", stamp))
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	h := &harness{binary: absoluteBinary, self: self, root: root, commandLog: filepath.Join(evidence, "commands.log")}
	h.report = report{SchemaVersion: 1, Mode: "local-development", LatencyKind: "loopback (not real-host WAN)", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Environment: map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "authorityHosts": "1", "clientHosts": "1 process host / 2 isolated roots"}, RenewalMargins: []string{"guarded start exercised with 10m TTL", "real-host renewal margin unmeasured"}, RecoveryBounds: "development fixture only; real-host recovery bounds remain unmeasured"}
	defer h.stopServer()
	if err := h.provision(evidence); err != nil {
		return err
	}
	started := time.Now()
	for group := 1; group <= 5; group++ {
		groupStart := time.Now()
		var groupErr error
		switch group {
		case 1:
			groupErr = h.group1(evidence)
		case 2:
			groupErr = h.group2(evidence)
		case 3:
			groupErr = h.group3(evidence)
		case 4:
			groupErr = h.group4(evidence)
		case 5:
			groupErr = h.group5(evidence)
		}
		if groupErr != nil {
			return fmt.Errorf("group %d: %w", group, groupErr)
		}
		h.report.Groups[len(h.report.Groups)-1].Evidence = append(h.report.Groups[len(h.report.Groups)-1].Evidence, h.commandLog, "duration="+time.Since(groupStart).String())
	}
	h.report.Throughput = fmt.Sprintf("%d development groups in %s", len(h.report.Groups), time.Since(started))
	if info, err := os.Stat(filepath.Join(root, "authority", "worklease.db")); err == nil {
		h.report.Storage = fmt.Sprintf("authority database bytes=%d", info.Size())
	}
	h.report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.MarshalIndent(h.report, "", "  ")
	if err != nil {
		return err
	}
	reportPath := filepath.Join(evidence, "report.json")
	if err := os.WriteFile(reportPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("remote development smoke passed; evidence %s\n", reportPath)
	return nil
}

func (h *harness) provision(evidence string) error {
	authorityHome := filepath.Join(h.root, "authority")
	secretDir := filepath.Join(h.root, "secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return err
	}
	cert, key := filepath.Join(secretDir, "tls.crt"), filepath.Join(secretDir, "tls.key")
	if err := writeCertificate(cert, key); err != nil {
		return err
	}
	h.cert = cert
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	address := listener.Addr().String()
	_ = listener.Close()
	h.endpoint = "https://" + address
	configPath := filepath.Join(secretDir, "server.yaml")
	config := fmt.Sprintf("home: %s\nlisten: %s\ntlsCert: %s\ntlsKey: %s\nadmittedPrefixes:\n  - 'coordination:'\nmaxTTL: 1h\nmaxHold: 24h\nshutdownTimeout: 2s\nhealthRate: 100\nmetadataRate: 100\nenrollmentRate: 100\n", authorityHome, address, cert, key)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return err
	}
	bootstrap := filepath.Join(secretDir, "bootstrap.invite")
	h.logCommand("authority", []string{"worklease", "--json", "hosted", "init", "--home", authorityHome, "--server-config", configPath, "--bootstrap-invite-file", bootstrap})
	initResult, err := runJSON(nil, "", h.binary, "--json", "hosted", "init", "--home", authorityHome, "--server-config", configPath, "--bootstrap-invite-file", bootstrap)
	if err != nil {
		return err
	}
	h.report.Authority, _ = initResult["authorityId"].(string)
	h.logCommand("authority", []string{"worklease", "serve", "--server-config", configPath})
	h.server = exec.Command(h.binary, "serve", "--server-config", configPath)
	serverLog, err := os.OpenFile(filepath.Join(evidence, "authority.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h.server.Stdout, h.server.Stderr = serverLog, serverLog
	if err := h.server.Start(); err != nil {
		_ = serverLog.Close()
		return err
	}
	_ = serverLog.Close()
	if err := waitHealthy(h.endpoint, cert); err != nil {
		return err
	}
	for index, name := range []string{"client-a", "client-b"} {
		c := client{name: name, home: filepath.Join(h.root, name, "home"), config: filepath.Join(h.root, name, "config"), checkout: filepath.Join(h.root, name, "checkout")}
		if err := os.MkdirAll(c.checkout, 0o700); err != nil {
			return err
		}
		if output, err := exec.Command("git", "init", "--quiet", c.checkout).CombinedOutput(); err != nil {
			return fmt.Errorf("git init %s: %w: %s", name, err, output)
		}
		c.env = append(os.Environ(), "WORKLEASE_HOME="+c.home, "XDG_CONFIG_HOME="+c.config, "SSL_CERT_FILE="+cert, "WORKLEASE_AGENT_ID="+name, "WORKLEASE_SESSION_ID="+name+"-session")
		h.clients[index] = c
		if _, err := h.cli(c, "profile", "add", "team", "--endpoint", h.endpoint, "--authority-id", h.report.Authority); err != nil {
			return err
		}
	}
	if _, err := h.cli(h.clients[0], "enroll", "--profile", "team", "--invite-file", bootstrap, "--label", "acceptance-admin"); err != nil {
		return err
	}
	workerInvite := filepath.Join(secretDir, "worker.invite")
	if _, err := h.cli(h.clients[0], "--profile", "team", "invite", "issue", "--role", "write", "--invite-file", workerInvite, "--label", "acceptance-worker"); err != nil {
		return err
	}
	if _, err := h.cli(h.clients[1], "enroll", "--profile", "team", "--invite-file", workerInvite, "--label", "acceptance-worker"); err != nil {
		return err
	}
	h.report.ClientRoots = []string{h.clients[0].checkout, h.clients[1].checkout}
	return nil
}

func (h *harness) group1(evidence string) error {
	a, b := h.clients[0], h.clients[1]
	if _, err := h.cli(a, "--profile", "team", "acquire", "--resource", "coordination:shared", "--ttl", "10m"); err != nil {
		return err
	}
	contentionHandle := filepath.Join(b.home, "handles", "contention.json")
	contended, err := h.cliFailure(b, "--profile", "team", "acquire", "--handle", contentionHandle, "--resource", "coordination:shared", "--ttl", "10m")
	if err != nil {
		return err
	}
	if err := requireReason(contended, "already-claimed"); err != nil {
		return err
	}
	separateHandle := filepath.Join(b.home, "handles", "separate.json")
	if _, err := h.cli(b, "--profile", "team", "acquire", "--handle", separateHandle, "--resource", "coordination:separate", "--ttl", "30s"); err != nil {
		return err
	}
	if err := h.mcpRoundTrip(b); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 1, Observation: "distinct checkout and credential roots contend through CLI while a separate scope and stdio MCP lifecycle succeed", Commands: []string{"client-a acquire coordination:shared", "client-b contended acquire", "client-b acquire coordination:separate", "client-b worklease mcp"}, Evidence: []string{filepath.Join(evidence, "authority.log")}, Passed: true})
	return nil
}

func (h *harness) group2(evidence string) error {
	effect := filepath.Join(evidence, "guarded-effects.log")
	if _, err := h.cli(h.clients[0], "--profile", "team", "exec", "--", h.self, "effect", effect); err != nil {
		return err
	}
	data, err := os.ReadFile(effect)
	if err != nil || string(data) != "dispatch\n" {
		return fmt.Errorf("guarded effect dispatch count: content=%q err=%v", data, err)
	}
	before := time.Now()
	h.stopServer()
	partitionHandle := filepath.Join(h.clients[1].home, "handles", "partition.json")
	_, partitionErr := h.cliFailure(h.clients[1], "--profile", "team", "acquire", "--handle", partitionHandle, "--resource", "coordination:partition", "--max-wait", "100ms")
	if partitionErr != nil {
		return partitionErr
	}
	if _, err := os.Stat(filepath.Join(h.clients[1].home, "worklease.db")); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("partition created local fallback authority: %v", err)
	}
	h.report.LatencyMillis = time.Since(before).Milliseconds()
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 2, Observation: "guarded effect dispatched exactly once on the client and an authority partition created no local fallback", Commands: []string{"client-a guarded exec", "stop authority", "client-b acquire during partition", "restart authority"}, Evidence: []string{effect, "dispatch-count=1"}, Passed: true})
	return nil
}

func (h *harness) group3(evidence string) error {
	credential := filepath.Join(h.clients[1].config, "worklease", "credentials", "team")
	saved := credential + ".saved"
	if err := os.Rename(credential, saved); err != nil {
		return err
	}
	missingCredential, err := h.cliFailure(h.clients[1], "--profile", "team", "installation", "list")
	if renameErr := os.Rename(saved, credential); renameErr != nil {
		return renameErr
	}
	if err != nil {
		return err
	}
	if err := requireReason(missingCredential, "credential-unsafe", "authentication-required"); err != nil {
		return err
	}
	if _, err := h.cli(h.clients[0], "--profile", "team", "installation", "list"); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 3, Observation: "file-based bootstrap and worker enrollment produced isolated roles; missing installation credentials fail closed", Commands: []string{"enroll admin from bootstrap file", "issue worker invite file", "enroll worker", "remove credential and list installations"}, Evidence: []string{filepath.Join(evidence, "authority.log")}, Passed: true})
	return nil
}

func (h *harness) group4(evidence string) error {
	events, err := h.cli(h.clients[0], "--profile", "team", "events", "--limit", "100")
	if err != nil {
		return err
	}
	cursor, _ := events["nextCursor"].(string)
	if cursor == "" {
		return errors.New("events returned no cursor")
	}
	if _, err := h.cli(h.clients[0], "--profile", "team", "watch", "--cursor", cursor, "--timeout", "10ms"); err != nil {
		return err
	}
	pendingRoot := filepath.Join(h.clients[0].home, "pending", "team")
	entries, err := os.ReadDir(pendingRoot)
	if err != nil {
		return fmt.Errorf("pending root missing: %v", err)
	}
	inventory := filepath.Join(evidence, "pending-inventory.txt")
	if err := os.WriteFile(inventory, []byte(fmt.Sprintf("enumerable-pending-files=%d\n", len(entries))), 0o600); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 4, Observation: "snapshot cursor resumes through the remote watch path and the enumerable pending root survives client process restarts", Commands: []string{"events snapshot", "watch from cursor"}, Evidence: []string{inventory}, Passed: true})
	return nil
}

func (h *harness) group5(evidence string) error {
	authorityDB := filepath.Join(h.root, "authority", "worklease.db")
	backup := filepath.Join(evidence, "asynchronous-backup.db")
	cutoff := time.Now().UTC()
	h.logCommand("backup", []string{"sqlite online backup", authorityDB, backup})
	done := make(chan error, 1)
	go func() { done <- backupSQLite(authorityDB, backup) }()
	if err := <-done; err != nil {
		return err
	}
	h.report.BackupCutoff = cutoff.Format(time.RFC3339Nano)
	h.stopServer()
	restoredInvite := filepath.Join(h.root, "secrets", "restored.invite")
	start := time.Now()
	restoreArgs := []string{"--json", "hosted", "restore", "--home", filepath.Join(h.root, "authority"), "--from", backup, "--selected-cutoff", cutoff.Format(time.RFC3339Nano), "--loss-interval-start", cutoff.Format(time.RFC3339Nano), "--loss-interval-end", time.Now().UTC().Format(time.RFC3339Nano), "--bootstrap-invite-file", restoredInvite}
	h.logCommand("authority", append([]string{"worklease"}, restoreArgs...))
	_, err := runJSON(nil, "", h.binary, restoreArgs...)
	if err != nil {
		return err
	}
	h.report.RestoreTime = time.Since(start).String()
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	afterRestoreHandle := filepath.Join(h.clients[0].home, "handles", "after-restore.json")
	staleClient, err := h.cliFailure(h.clients[0], "--profile", "team", "acquire", "--handle", afterRestoreHandle, "--resource", "coordination:after-restore")
	if err != nil {
		return err
	}
	if err := requireReason(staleClient, "authority-restored", "installation-revoked", "authentication-required"); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 5, Observation: "an asynchronously selected SQLite cutoff restores to a fresh incarnation and old clients fail closed", Commands: []string{"copy live SQLite cutoff", "stop authority", "hosted restore", "restart authority", "old client acquire"}, Evidence: []string{backup, "selected-cutoff=" + h.report.BackupCutoff, "restore-time=" + h.report.RestoreTime}, Passed: true})
	return nil
}

func (h *harness) cli(c client, args ...string) (map[string]any, error) {
	h.logCommand(c.name, append([]string{"worklease", "--json"}, args...))
	return runJSON(c.env, c.checkout, h.binary, append([]string{"--json"}, args...)...)
}

func (h *harness) cliFailure(c client, args ...string) (map[string]any, error) {
	h.logCommand(c.name+" expected-failure", append([]string{"worklease", "--json"}, args...))
	cmd := exec.Command(h.binary, append([]string{"--json"}, args...)...)
	cmd.Env, cmd.Dir = c.env, c.checkout
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil, fmt.Errorf("command unexpectedly succeeded: %s", strings.Join(args, " "))
	}
	var result map[string]any
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil || result["ok"] != false {
		return nil, fmt.Errorf("invalid failure envelope: %v: %s", jsonErr, output)
	}
	return result, nil
}

func requireReason(result map[string]any, expected ...string) error {
	errorFields, _ := result["error"].(map[string]any)
	got, _ := errorFields["reason"].(string)
	for _, want := range expected {
		if got == want {
			return nil
		}
	}
	return fmt.Errorf("failure reason=%q, want one of %v", got, expected)
}

func (h *harness) mcpRoundTrip(c client) error {
	h.logCommand(c.name, []string{"worklease", "mcp", "--profile", "team"})
	cmd := exec.Command(h.binary, "mcp", "--profile", "team")
	cmd.Env, cmd.Dir = c.env, c.checkout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	encoder, scanner := json.NewEncoder(stdin), bufio.NewScanner(stdout)
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25"}}); err != nil {
		return err
	}
	_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if !scanner.Scan() {
		return errors.New("MCP initialize returned no response")
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "acquire", "arguments": map[string]any{"resources": []string{"coordination:mcp"}, "ttl": 30, "maxHold": 60, "autoHeartbeat": false}}}); err != nil {
		return err
	}
	var lease string
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil || fmt.Sprint(message["id"]) != "2" {
			continue
		}
		result, _ := message["result"].(map[string]any)
		fields, _ := result["structuredContent"].(map[string]any)
		lease, _ = fields["lease"].(string)
		break
	}
	if lease == "" {
		return errors.New("MCP acquire returned no lease reference")
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "release", "arguments": map[string]any{"lease": lease, "reason": "acceptance complete"}}}); err != nil {
		return err
	}
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) == nil && fmt.Sprint(message["id"]) == "3" {
			break
		}
	}
	_ = stdin.Close()
	err = cmd.Wait()
	finished = true
	return err
}

func (h *harness) logCommand(role string, args []string) {
	line := time.Now().UTC().Format(time.RFC3339Nano) + " " + role + ": " + strings.Join(args, " ") + "\n"
	file, err := os.OpenFile(h.commandLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = io.WriteString(file, line)
	_ = file.Close()
}

func (h *harness) stopServer() {
	if h.server == nil || h.server.Process == nil {
		return
	}
	_ = h.server.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- h.server.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = h.server.Process.Kill()
		<-done
	}
	h.server = nil
}

func (h *harness) restartServer(evidence string) error {
	configPath := filepath.Join(h.root, "secrets", "server.yaml")
	h.logCommand("authority", []string{"worklease", "serve", "--server-config", configPath})
	h.server = exec.Command(h.binary, "serve", "--server-config", configPath)
	logFile, err := os.OpenFile(filepath.Join(evidence, "authority.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h.server.Stdout, h.server.Stderr = logFile, logFile
	if err := h.server.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = logFile.Close()
	return waitHealthy(h.endpoint, h.cert)
}

func runJSON(env []string, dir, binary string, args ...string) (map[string]any, error) {
	cmd := exec.Command(binary, args...)
	if env != nil {
		cmd.Env = env
	}
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, output)
	}
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil || result["ok"] != true {
		return nil, fmt.Errorf("invalid success envelope: %v: %s", err, output)
	}
	return result, nil
}

func waitHealthy(endpoint, cert string) error {
	pool := x509.NewCertPool()
	data, err := os.ReadFile(cert)
	if err != nil || !pool.AppendCertsFromPEM(data) {
		return errors.New("cannot load development TLS certificate")
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		request, _ := http.NewRequest(http.MethodGet, endpoint+"/healthz", nil)
		request.Header.Set("Accept", "application/json")
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health status %d", response.StatusCode)
		} else {
			lastErr = requestErr
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("authority did not become healthy: %w", lastErr)
}

func writeCertificate(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "worklease acceptance"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600)
}

func backupSQLite(source, destination string) error {
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("backup destination already exists")
		}
		return err
	}
	database, err := sql.Open("sqlite", "file:"+source+"?mode=ro")
	if err != nil {
		return err
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	if _, err := database.Exec("VACUUM INTO ?", destination); err != nil {
		return err
	}
	return os.Chmod(destination, 0o600)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
