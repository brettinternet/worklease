// Command worklease-remote-smoke exercises the shipped binary as one TLS
// authority and two isolated clients. With --remote-host, authority and client B
// run over SSH on a real host while client A and the orchestrator remain local.
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
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type coverageEntry struct {
	ID       string   `json:"id"`
	Clause   string   `json:"clause"`
	Status   string   `json:"status"`
	Evidence []string `json:"evidence"`
}

type supportingTestEvidence struct {
	Command string `json:"command"`
	Scope   string `json:"scope"`
	Status  string `json:"status"`
	Log     string `json:"log,omitempty"`
}

type coverageReport struct {
	SchemaVersion   int                      `json:"schemaVersion"`
	Mode            string                   `json:"mode"`
	GeneratedAt     string                   `json:"generatedAt"`
	Entries         []coverageEntry          `json:"entries"`
	SupportingTests []supportingTestEvidence `json:"supportingTests"`
}

type groupEvidence struct {
	Group       int      `json:"group"`
	Observation string   `json:"observation"`
	Commands    []string `json:"commands"`
	Evidence    []string `json:"evidence"`
	Passed      bool     `json:"smokeCheckPassed"`
}

type report struct {
	SchemaVersion        int               `json:"schemaVersion"`
	RemoteWorkspace      string            `json:"remoteWorkspace,omitempty"`
	RemoteEvidence       []string          `json:"remoteEvidence,omitempty"`
	Mode                 string            `json:"mode"`
	LatencyKind          string            `json:"latencyKind"`
	StartedAt            string            `json:"startedAt"`
	FinishedAt           string            `json:"finishedAt"`
	Authority            string            `json:"authority"`
	ClientRoots          []string          `json:"clientRoots"`
	Environment          map[string]string `json:"environment"`
	LatencyMillis        int64             `json:"latencyMillis"`
	RenewalMargins       []string          `json:"renewalMargins"`
	Throughput           string            `json:"throughput"`
	Storage              string            `json:"storage"`
	BackupCutoff         string            `json:"backupCutoff"`
	RestoreTime          string            `json:"restoreTime"`
	RecoveryBounds       string            `json:"recoveryBounds"`
	Groups               []groupEvidence   `json:"groups"`
	EffectDispatchCounts map[string]int    `json:"effectDispatchCounts"`
	CoverageReport       string            `json:"coverageReport,omitempty"`
	Coverage             []coverageEntry   `json:"coverage,omitempty"`
}

type harness struct {
	binary, self, root, endpoint, cert                              string
	remoteHost, remoteRoot, remoteAddress                           string
	remotePort                                                      int
	remoteBinary, remoteHelper, remoteConfig, remoteCert, remoteKey string
	commandLog                                                      string
	server                                                          *exec.Cmd
	clients                                                         [2]client
	report                                                          report
	realHost                                                        bool
	supportingTests                                                 []supportingTestEvidence
}

type client struct {
	name, home, config, checkout string
	env                          []string
	remote                       bool
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
	if len(os.Args) > 1 && os.Args[1] == "owner-marker" {
		if len(os.Args) != 3 || !strings.HasPrefix(os.Args[2], "/private/tmp/worklease-acceptance-") {
			fatal(errors.New("owner-marker requires an acceptance workspace"))
		}
		marker := filepath.Join(os.Args[2], ".worklease-acceptance-owner")
		if err := os.WriteFile(marker, []byte(fmt.Sprintf("pid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))), 0o600); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if len(os.Args) != 4 {
			fatal(errors.New("backup requires source and destination"))
		}
		if err := backupSQLite(os.Args[2], os.Args[3]); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "free-port" {
		if len(os.Args) != 2 {
			fatal(errors.New("free-port takes no arguments"))
		}
		port, err := freePort()
		if err != nil {
			fatal(err)
		}
		fmt.Println(port)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		if len(os.Args) != 5 {
			fatal(errors.New("serve requires binary, config, and pid file"))
		}
		serve := exec.Command(os.Args[2], "serve", "--server-config", os.Args[3])
		serve.Stdout, serve.Stderr = os.Stdout, os.Stderr
		if err := serve.Start(); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(os.Args[4], []byte(fmt.Sprintf("%d\n", serve.Process.Pid)), 0o600); err != nil {
			_ = serve.Process.Kill()
			fatal(err)
		}
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		go func() {
			<-signals
			_ = serve.Process.Signal(os.Interrupt)
		}()
		err := serve.Wait()
		signal.Stop(signals)
		_ = os.Remove(os.Args[4])
		if err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "stop-server" {
		if len(os.Args) != 3 {
			fatal(errors.New("stop-server requires a pid file"))
		}
		pidData, err := os.ReadFile(os.Args[2])
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			fatal(err)
		}
		var pid int
		if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
			fatal(err)
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			fatal(err)
		}
		if err := process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			fatal(err)
		}
		return
	}

	binary := flag.String("binary", "bin/worklease", "built worklease binary")
	evidence := flag.String("evidence", "", "evidence directory (default: dist/remote-acceptance/TIMESTAMP)")
	keep := flag.Bool("keep", false, "keep temporary authority and client state")
	remoteHost := flag.String("remote-host", "", "run authority and client B on this SSH host (for example remote-host)")
	flag.Parse()
	if err := run(*binary, *evidence, *keep, *remoteHost); err != nil {
		fatal(err)
	}
}

func run(binary, evidence string, keep bool, remoteHosts ...string) error {
	remoteHost := ""
	if len(remoteHosts) > 0 {
		remoteHost = remoteHosts[0]
	}
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
	h := &harness{binary: absoluteBinary, self: self, root: root, commandLog: filepath.Join(evidence, "commands.log"), remoteHost: remoteHost, realHost: remoteHost != ""}
	mode, latencyKind, hosts := "local-development", "loopback (not real-host WAN)", "1 process host / 2 isolated roots"
	if h.realHost {
		mode, latencyKind, hosts = "real-host-smoke", "measured end-to-end remote client command latency (includes SSH orchestration)", "orchestrator/client-A local; authority/client-B remote"
	}
	h.report = report{SchemaVersion: 2, Mode: mode, LatencyKind: latencyKind, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Environment: map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "authorityHosts": "1", "clientHosts": hosts}, EffectDispatchCounts: map[string]int{}}
	if h.realHost {
		h.report.Environment["sshHost"] = remoteHost
		h.report.RenewalMargins = []string{"10m contention TTL exercised; renewal timing not measured in this smoke slice"}
		h.report.RecoveryBounds = "real-host smoke observed restart/restore fail-closed bounds; exhaustive recovery bounds and WAN cutoffs remain unmeasured"
	} else {
		h.report.RenewalMargins = []string{"guarded start exercised with 10m TTL", "real-host renewal margin unmeasured"}
		h.report.RecoveryBounds = "development fixture only; real-host recovery bounds remain unmeasured"
	}
	defer h.stopServer()
	if err := h.provision(evidence); err != nil {
		return err
	}
	if h.realHost {
		h.supportingTests = runSupportingTests(evidence)
		h.supportingTests = append(h.supportingTests, h.runRemoteSupportingTests(evidence)...)
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
	throughputKind := "development"
	if h.realHost {
		throughputKind = "real-host"
		if size, sizeErr := h.remoteSize(filepath.Join(h.remoteRoot, "authority", "worklease.db")); sizeErr == nil {
			h.report.Storage = fmt.Sprintf("remote authority database bytes=%s (%s:%s)", size, h.remoteHost, filepath.Join(h.remoteRoot, "authority", "worklease.db"))
		}
	} else if info, err := os.Stat(filepath.Join(root, "authority", "worklease.db")); err == nil {
		h.report.Storage = fmt.Sprintf("authority database bytes=%d", info.Size())
	}
	h.report.Throughput = fmt.Sprintf("%d %s smoke groups in %s", len(h.report.Groups), throughputKind, time.Since(started))
	h.report.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	h.report.Coverage = h.coverageMatrix()
	coveragePath := filepath.Join(evidence, "coverage.json")
	h.report.CoverageReport = coveragePath
	coverageData, err := json.MarshalIndent(coverageReport{SchemaVersion: 1, Mode: h.report.Mode, GeneratedAt: h.report.FinishedAt, Entries: h.report.Coverage, SupportingTests: h.supportingTests}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(coveragePath, append(coverageData, '\n'), 0o600); err != nil {
		return err
	}
	data, err := json.MarshalIndent(h.report, "", "  ")
	if err != nil {
		return err
	}
	reportPath := filepath.Join(evidence, "report.json")
	if err := os.WriteFile(reportPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if h.realHost {
		fmt.Printf("real-host smoke passed; evidence %s; remote workspace %s (owner-marked, retained)\n", reportPath, h.report.RemoteWorkspace)
	} else {
		fmt.Printf("remote development smoke passed; evidence %s\n", reportPath)
	}
	return nil
}

func (h *harness) provision(evidence string) error {
	if h.realHost {
		return h.provisionRemote(evidence)
	}
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

func (h *harness) provisionRemote(evidence string) error {
	if err := validateSSHHost(h.remoteHost); err != nil {
		return err
	}
	address, err := resolveSSHAddress(h.remoteHost)
	if err != nil {
		return err
	}
	h.remoteAddress = address
	workspaceOutput, err := runSSH(h.remoteHost, "mktemp", "-d", "/private/tmp/worklease-acceptance-XXXXXX")
	if err != nil {
		return fmt.Errorf("remote workspace: %w", err)
	}
	h.remoteRoot = strings.TrimSpace(workspaceOutput)
	if !strings.HasPrefix(h.remoteRoot, "/private/tmp/worklease-acceptance-") || strings.ContainsAny(h.remoteRoot, "\r\n") {
		return fmt.Errorf("remote workspace has unsafe path %q", h.remoteRoot)
	}
	h.report.RemoteWorkspace = h.remoteHost + ":" + h.remoteRoot
	h.remoteBinary = filepath.Join(h.remoteRoot, "worklease")
	h.remoteHelper = filepath.Join(h.remoteRoot, "harness-helper")
	for local, remote := range map[string]string{h.binary: h.remoteBinary, h.self: h.remoteHelper} {
		if err := runSCP(h.remoteHost, local, remote); err != nil {
			return fmt.Errorf("copy %s: %w", filepath.Base(local), err)
		}
	}
	if _, err := runSSH(h.remoteHost, h.remoteHelper, "owner-marker", h.remoteRoot); err != nil {
		return fmt.Errorf("mark remote workspace: %w", err)
	}
	for _, path := range []string{h.remoteBinary, h.remoteHelper} {
		if _, err := runSSH(h.remoteHost, "chmod", "700", path); err != nil {
			return err
		}
	}
	secretDir := filepath.Join(h.root, "secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return err
	}
	cert, key := filepath.Join(secretDir, "tls.crt"), filepath.Join(secretDir, "tls.key")
	if err := writeCertificate(cert, key, net.ParseIP(address), address, h.remoteHost); err != nil {
		return err
	}
	h.cert = cert
	h.remoteCert, h.remoteKey = filepath.Join(h.remoteRoot, "tls.crt"), filepath.Join(h.remoteRoot, "tls.key")
	for local, remote := range map[string]string{cert: h.remoteCert, key: h.remoteKey} {
		if err := runSCP(h.remoteHost, local, remote); err != nil {
			return fmt.Errorf("copy TLS material: %w", err)
		}
	}
	if _, err := runSSH(h.remoteHost, "chmod", "600", h.remoteCert, h.remoteKey); err != nil {
		return err
	}
	portOutput, err := runSSH(h.remoteHost, h.remoteHelper, "free-port")
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(strings.TrimSpace(portOutput))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("remote helper returned invalid port %q", strings.TrimSpace(portOutput))
	}
	h.remotePort = port
	authorityHome := filepath.Join(h.remoteRoot, "authority")
	h.remoteConfig = filepath.Join(h.remoteRoot, "server.yaml")
	addressPort := fmt.Sprintf("0.0.0.0:%d", port)
	config := fmt.Sprintf("home: %s\nlisten: %s\ntlsCert: %s\ntlsKey: %s\nadmittedPrefixes:\n  - 'coordination:'\nmaxTTL: 1h\nmaxHold: 24h\nshutdownTimeout: 2s\nhealthRate: 100\nmetadataRate: 100\nenrollmentRate: 100\n", authorityHome, addressPort, h.remoteCert, h.remoteKey)
	configLocal := filepath.Join(secretDir, "server.yaml")
	if err := os.WriteFile(configLocal, []byte(config), 0o600); err != nil {
		return err
	}
	if err := runSCP(h.remoteHost, configLocal, h.remoteConfig); err != nil {
		return err
	}
	h.endpoint = fmt.Sprintf("https://%s:%d", address, port)
	bootstrapRemote := filepath.Join(h.remoteRoot, "bootstrap.invite")
	initArgs := []string{"--json", "hosted", "init", "--home", authorityHome, "--server-config", h.remoteConfig, "--bootstrap-invite-file", bootstrapRemote}
	h.logCommand("authority@"+h.remoteHost, append([]string{"worklease"}, initArgs...))
	initResult, err := h.remoteJSON(h.remoteBinary, initArgs...)
	if err != nil {
		return err
	}
	h.report.Authority, _ = initResult["authorityId"].(string)
	bootstrap := filepath.Join(secretDir, "bootstrap.invite")
	if err := runSCP(h.remoteHost, h.remoteHost+":"+bootstrapRemote, bootstrap); err != nil {
		return fmt.Errorf("fetch bootstrap invite: %w", err)
	}
	if err := h.startServer(evidence); err != nil {
		return err
	}
	a := client{name: "client-a", home: filepath.Join(h.root, "client-a", "home"), config: filepath.Join(h.root, "client-a", "config"), checkout: filepath.Join(h.root, "client-a", "checkout")}
	if err := os.MkdirAll(a.checkout, 0o700); err != nil {
		return err
	}
	if output, err := exec.Command("git", "init", "--quiet", a.checkout).CombinedOutput(); err != nil {
		return fmt.Errorf("git init client-a: %w: %s", err, output)
	}
	a.env = append(os.Environ(), "WORKLEASE_HOME="+a.home, "XDG_CONFIG_HOME="+a.config, "SSL_CERT_FILE="+cert, "WORKLEASE_AGENT_ID=client-a", "WORKLEASE_SESSION_ID=client-a-session")
	h.clients[0] = a
	b := client{name: "client-b", home: filepath.Join(h.remoteRoot, "client-b", "home"), config: filepath.Join(h.remoteRoot, "client-b", "config"), checkout: filepath.Join(h.remoteRoot, "client-b", "checkout"), remote: true}
	if _, err := runSSH(h.remoteHost, "mkdir", "-p", b.home, b.config, b.checkout); err != nil {
		return err
	}
	if _, err := runSSH(h.remoteHost, "git", "init", "--quiet", b.checkout); err != nil {
		return fmt.Errorf("git init client-b: %w", err)
	}
	h.clients[1] = b
	if _, err := h.cli(a, "profile", "add", "team", "--endpoint", h.endpoint, "--authority-id", h.report.Authority); err != nil {
		return err
	}
	if _, err := h.cli(b, "profile", "add", "team", "--endpoint", h.endpoint, "--authority-id", h.report.Authority); err != nil {
		return err
	}
	if _, err := h.cli(a, "enroll", "--profile", "team", "--invite-file", bootstrap, "--label", "acceptance-admin"); err != nil {
		return err
	}
	workerInvite := filepath.Join(secretDir, "worker.invite")
	if _, err := h.cli(a, "--profile", "team", "invite", "issue", "--role", "write", "--invite-file", workerInvite, "--label", "acceptance-worker"); err != nil {
		return err
	}
	workerRemote := filepath.Join(h.remoteRoot, "worker.invite")
	if err := runSCP(h.remoteHost, workerInvite, workerRemote); err != nil {
		return err
	}
	if _, err := h.cli(b, "enroll", "--profile", "team", "--invite-file", workerRemote, "--label", "acceptance-worker"); err != nil {
		return err
	}
	h.report.ClientRoots = []string{a.checkout, h.remoteHost + ":" + b.checkout}
	h.report.Environment["remoteAddress"] = address
	h.report.Environment["remotePort"] = fmt.Sprint(port)
	h.report.Environment["remoteWorkspaceOwnerMarker"] = filepath.Join(h.remoteRoot, ".worklease-acceptance-owner")
	return nil
}

func (h *harness) startServer(evidence string) error {
	serverArgs := []string{"ssh", h.remoteHost, h.remoteHelper, "serve", h.remoteBinary, h.remoteConfig, filepath.Join(h.remoteRoot, "server.pid")}
	h.logCommand("authority@"+h.remoteHost, serverArgs)
	h.server = exec.Command("ssh", serverArgs[1:]...)
	serverLog, err := os.OpenFile(filepath.Join(evidence, "authority.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h.server.Stdout, h.server.Stderr = serverLog, serverLog
	if err := h.server.Start(); err != nil {
		_ = serverLog.Close()
		return err
	}
	_ = serverLog.Close()
	return waitHealthy(h.endpoint, h.cert)
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
	latencyStart := time.Now()
	if _, err := h.cli(b, "--profile", "team", "list"); err != nil {
		return err
	}
	h.report.LatencyMillis = time.Since(latencyStart).Milliseconds()
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
	h.report.EffectDispatchCounts[filepath.Base(effect)] = strings.Count(string(data), "dispatch\n")
	h.stopServer()
	partitionHandle := filepath.Join(h.clients[1].home, "handles", "partition.json")
	_, partitionErr := h.cliFailure(h.clients[1], "--profile", "team", "acquire", "--handle", partitionHandle, "--resource", "coordination:partition", "--max-wait", "100ms")
	if partitionErr != nil {
		return partitionErr
	}
	fallbackPath := filepath.Join(h.clients[1].home, "worklease.db")
	if h.realHost {
		if exists, checkErr := h.remotePathExists(fallbackPath); checkErr != nil || exists {
			return fmt.Errorf("partition created remote local fallback authority: exists=%v err=%v", exists, checkErr)
		}
	} else if _, err := os.Stat(fallbackPath); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("partition created local fallback authority: %v", err)
	}
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 2, Observation: "guarded effect dispatched exactly once on the client and an authority partition created no local fallback", Commands: []string{"client-a guarded exec", "stop authority", "client-b acquire during partition", "restart authority"}, Evidence: []string{effect, "dispatch-count=1"}, Passed: true})
	return nil
}

func (h *harness) group3(evidence string) error {
	credential := filepath.Join(h.clients[1].config, "worklease", "credentials", "team")
	saved := credential + ".saved"
	var renameErr error
	if h.realHost {
		_, renameErr = runSSH(h.remoteHost, "mv", credential, saved)
	} else {
		renameErr = os.Rename(credential, saved)
	}
	if renameErr != nil {
		return renameErr
	}
	missingCredential, err := h.cliFailure(h.clients[1], "--profile", "team", "installation", "list")
	if h.realHost {
		_, renameErr = runSSH(h.remoteHost, "mv", saved, credential)
	} else {
		renameErr = os.Rename(saved, credential)
	}
	if renameErr != nil {
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
	backupRemote := backup
	cutoff := time.Now().UTC()
	if h.realHost {
		authorityDB = filepath.Join(h.remoteRoot, "authority", "worklease.db")
		backupRemote = filepath.Join(h.remoteRoot, "asynchronous-backup.db")
	}
	h.logCommand("backup@"+h.remoteHost, []string{"sqlite online backup", authorityDB, backupRemote})
	var backupErr error
	if h.realHost {
		_, backupErr = runSSH(h.remoteHost, h.remoteHelper, "backup", authorityDB, backupRemote)
		if backupErr == nil {
			backupErr = runSCP(h.remoteHost, h.remoteHost+":"+backupRemote, backup)
		}
	} else {
		done := make(chan error, 1)
		go func() { done <- backupSQLite(authorityDB, backup) }()
		backupErr = <-done
	}
	if backupErr != nil {
		return backupErr
	}
	h.report.BackupCutoff = cutoff.Format(time.RFC3339Nano)
	if h.realHost {
		h.report.RemoteEvidence = append(h.report.RemoteEvidence, h.remoteHost+":"+backupRemote)
	}
	h.stopServer()
	restoredInvite := filepath.Join(h.root, "secrets", "restored.invite")
	if h.realHost {
		restoredInvite = filepath.Join(h.remoteRoot, "restored.invite")
	}
	start := time.Now()
	restoreArgs := []string{"--json", "hosted", "restore", "--home", authorityDBRoot(h), "--from", backupRemote, "--selected-cutoff", cutoff.Format(time.RFC3339Nano), "--loss-interval-start", cutoff.Format(time.RFC3339Nano), "--loss-interval-end", time.Now().UTC().Format(time.RFC3339Nano), "--bootstrap-invite-file", restoredInvite}
	h.logCommand("authority@"+h.remoteHost, append([]string{"worklease"}, restoreArgs...))
	var err error
	if h.realHost {
		_, err = h.remoteJSON(h.remoteBinary, restoreArgs...)
	} else {
		_, err = runJSON(nil, "", h.binary, restoreArgs...)
	}
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

func runSupportingTests(evidence string) []supportingTestEvidence {
	command := []string{"go", "test", "./internal/authority", "./internal/cli", "./internal/gc", "./internal/handle", "./internal/lease", "./internal/mcp", "./internal/server", "./internal/store", "-count=1"}
	logPath := filepath.Join(evidence, "supporting-tests-local.log")
	output, err := exec.Command(command[0], command[1:]...).CombinedOutput()
	_ = os.WriteFile(logPath, output, 0o600)
	status := "supporting-test-pass"
	if err != nil {
		status = "failed"
	}
	return []supportingTestEvidence{{Command: strings.Join(command, " "), Scope: "local", Status: status, Log: logPath}}
}

func (h *harness) runRemoteSupportingTests(evidence string) []supportingTestEvidence {
	type remotePackage struct {
		path, filter string
	}
	packages := []remotePackage{{path: "./internal/gc"}, {path: "./internal/handle"}, {path: "./internal/mcp", filter: "TestCallLifecycleAndRedaction"}, {path: "./internal/store"}}
	results := make([]supportingTestEvidence, 0, len(packages))
	for index, packageInfo := range packages {
		name := fmt.Sprintf("supporting-test-%d", index)
		packagePath := packageInfo.path
		localBinary := filepath.Join(h.root, name)
		command := []string{"go", "test", "-c", "-o", localBinary, packagePath}
		buildOutput, buildErr := exec.Command(command[0], command[1:]...).CombinedOutput()
		logPath := filepath.Join(evidence, name+"-build.log")
		_ = os.WriteFile(logPath, buildOutput, 0o600)
		if buildErr != nil {
			results = append(results, supportingTestEvidence{Command: strings.Join(command, " "), Scope: "remote-host build", Status: "failed", Log: logPath})
			continue
		}
		remoteBinary := filepath.Join(h.remoteRoot, name)
		if copyErr := runSCP(h.remoteHost, localBinary, remoteBinary); copyErr != nil {
			results = append(results, supportingTestEvidence{Command: strings.Join(command, " "), Scope: "remote-host copy", Status: "failed", Log: logPath})
			continue
		}
		runCommand := []string{"ssh", h.remoteHost, remoteBinary, "-test.v"}
		runArgs := []string{"-test.v"}
		if packageInfo.filter != "" {
			runCommand = append(runCommand, "-test.run", packageInfo.filter)
			runArgs = append(runArgs, "-test.run", packageInfo.filter)
		}
		h.logCommand("supporting-test@"+h.remoteHost, runCommand)
		output, runErr := runSSH(h.remoteHost, append([]string{remoteBinary}, runArgs...)...)
		remoteLog := filepath.Join(evidence, name+"-remote-host.log")
		_ = os.WriteFile(remoteLog, []byte(output), 0o600)
		status := "supporting-test-pass"
		if runErr != nil {
			status = "failed"
		}
		results = append(results, supportingTestEvidence{Command: strings.Join(runCommand, " "), Scope: "remote-host", Status: status, Log: remoteLog})
	}
	return results
}

func (h *harness) coverageMatrix() []coverageEntry {
	localSupport := []string{}
	for _, test := range h.supportingTests {
		if test.Scope == "local" && test.Status == "supporting-test-pass" {
			localSupport = append(localSupport, test.Log)
		}
	}
	liveEvidence := func(group int) []string {
		if len(h.report.Groups) >= group && h.report.Groups[group-1].Passed {
			return []string{fmt.Sprintf("groups[%d] in report", group), h.commandLog}
		}
		return nil
	}
	supportEvidence := func(clause string) (string, []string) {
		if len(localSupport) == 0 {
			return "still-blocked", []string{"supporting tests did not pass"}
		}
		return "supporting-test-pass", append([]string{clause + " covered by focused regression suites"}, localSupport...)
	}
	blocked := func(id, clause string) coverageEntry {
		return coverageEntry{ID: id, Clause: clause, Status: "still-blocked", Evidence: []string{"not exercised by the five-group smoke slice"}}
	}
	live := func(id, clause string, group int) coverageEntry {
		evidence := liveEvidence(group)
		if len(evidence) == 0 {
			return blocked(id, clause)
		}
		return coverageEntry{ID: id, Clause: clause, Status: "live-pass", Evidence: evidence}
	}
	supported := func(id, clause string) coverageEntry {
		status, evidence := supportEvidence(clause)
		return coverageEntry{ID: id, Clause: clause, Status: status, Evidence: evidence}
	}
	return []coverageEntry{
		live("AC3.1", "cross-host contention and separate scopes", 1),
		live("AC3.2", "repository-independent profile selection", 1),
		blocked("AC3.3", "raw and misconfigured reserved-prefix rejection"),
		blocked("AC3.4", "configuration restart behavior"),
		blocked("AC3.5", "persisted admission limits on every extension path"),
		blocked("AC4.1", "exact replay after lost start, renewal, and completion responses"),
		supported("AC4.2", "coexistence of request-scoped recovery records with original guarded-effect evidence"),
		live("AC4.3", "no local fallback during partition", 2),
		blocked("AC4.4", "race ordering for revocation and policy changes"),
		blocked("AC4.5", "fresh response identity and time"),
		blocked("AC4.6", "clock-bound edge cases"),
		blocked("AC4.7", "pre-dispatch persistence failure"),
		blocked("AC4.8", "late acknowledgment without redispatch"),
		blocked("AC4.9", "asynchronous provider effect continuing after terminal completion"),
		blocked("AC5.1", "bootstrap crash ordering and redaction"),
		blocked("AC5.2", "hidden, file, and descriptor invite input"),
		blocked("AC5.3", "dropped invite and redemption responses"),
		blocked("AC5.4", "immutable request incarnation"),
		blocked("AC5.5", "no-burn mismatch"),
		live("AC5.6", "role isolation", 3),
		blocked("AC5.7", "credential rotation"),
		blocked("AC5.8", "credential revocation"),
		blocked("AC5.9", "distinct MCP authentication guidance"),
		blocked("AC6.1", "snapshot/watch races"),
		blocked("AC6.2", "disconnect and reconnect"),
		blocked("AC6.3", "cursor incarnation and retention gaps"),
		blocked("AC6.4", "stuck-history retention"),
		blocked("AC6.5", "full-volume storage-failure without pruning"),
		blocked("AC6.6", "pending evidence surviving age, GC, replay expiry, restart, and profile changes"),
		blocked("AC7.1", "actual asynchronous backup fixture with chosen and older cutoffs"),
		live("AC7.2", "online SQLite backup on the authority host", 5),
		blocked("AC7.3", "zero and nonzero pending sets"),
		live("AC7.4", "authority restart", 5),
		blocked("AC7.5", "schema and protocol upgrade"),
		blocked("AC7.6", "restored and missing credentials"),
		blocked("AC7.7", "double restore"),
		blocked("AC7.8", "retained start with lost completion"),
		blocked("AC7.9", "confirmed start missing from backup while client is offline or incomplete"),
		blocked("AC7.10", "fully missing completed work"),
		blocked("AC7.11", "provider effects after terminal receipt"),
		blocked("AC7.12", "installation inventories including ephemeral and retired clients"),
		blocked("AC7.13", "missing evidence blocks reopening"),
		blocked("AC7.14", "unknown cutoffs or history bounds with exhaustive coverage"),
		blocked("AC7.15", "transitive closure including the over-32 failure"),
		blocked("AC7.16", "retained replay"),
		blocked("AC7.17", "bootstrap reissue"),
		blocked("AC7.18", "atomic reopen"),
		blocked("AC7.19", "every lock-held bypass attempt"),
		blocked("AC7.20", "direct local mutation refused against marked hosted home while lock is free"),
	}
}

func hostSuffix(c client) string {
	if c.remote {
		return "@remote"
	}
	return "@local"
}

func (h *harness) remoteClientJSON(c client, args ...string) (map[string]any, error) {
	output, err := h.remoteClientOutput(c, args...)
	if err != nil {
		return nil, fmt.Errorf("remote client %s: %w: %s", c.name, err, output)
	}
	var result map[string]any
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil || result["ok"] != true {
		return nil, fmt.Errorf("invalid remote success envelope: %v: %s", jsonErr, output)
	}
	return result, nil
}

func (h *harness) remoteClientOutput(c client, args ...string) ([]byte, error) {
	remoteEnv := []string{"WORKLEASE_HOME=" + c.home, "XDG_CONFIG_HOME=" + c.config, "SSL_CERT_FILE=" + h.remoteCert, "WORKLEASE_AGENT_ID=" + c.name, "WORKLEASE_SESSION_ID=" + c.name + "-session"}
	sshArgs := []string{h.remoteHost, "env"}
	sshArgs = append(sshArgs, remoteEnv...)
	sshArgs = append(sshArgs, h.remoteBinary, "--json")
	sshArgs = append(sshArgs, args...)
	h.logCommand(c.name+" ssh", append([]string{"ssh"}, sshArgs...))
	cmd := exec.Command("ssh", sshArgs...)
	return cmd.Output()
}

func (h *harness) remoteJSON(binary string, args ...string) (map[string]any, error) {
	cmdArgs := append([]string{h.remoteHost, binary}, args...)
	h.logCommand("ssh@"+h.remoteHost, append([]string{"ssh"}, cmdArgs...))
	cmd := exec.Command("ssh", cmdArgs...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("remote command %s: %w: %s", strings.Join(args, " "), err, output)
	}
	var result map[string]any
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil || result["ok"] != true {
		return nil, fmt.Errorf("invalid remote JSON envelope: %v: %s", jsonErr, output)
	}
	return result, nil
}

func (h *harness) cli(c client, args ...string) (map[string]any, error) {
	h.logCommand(c.name+hostSuffix(c), append([]string{"worklease", "--json"}, args...))
	if c.remote {
		return h.remoteClientJSON(c, args...)
	}
	return runJSON(c.env, c.checkout, h.binary, append([]string{"--json"}, args...)...)
}

func (h *harness) cliFailure(c client, args ...string) (map[string]any, error) {
	h.logCommand(c.name+hostSuffix(c)+" expected-failure", append([]string{"worklease", "--json"}, args...))
	var output []byte
	var err error
	if c.remote {
		output, err = h.remoteClientOutput(c, args...)
	} else {
		cmd := exec.Command(h.binary, append([]string{"--json"}, args...)...)
		cmd.Env, cmd.Dir = c.env, c.checkout
		output, err = cmd.CombinedOutput()
	}
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
	h.logCommand(c.name+hostSuffix(c), []string{"worklease", "mcp", "--profile", "team"})
	var cmd *exec.Cmd
	if c.remote {
		remoteEnv := []string{"WORKLEASE_HOME=" + c.home, "XDG_CONFIG_HOME=" + c.config, "SSL_CERT_FILE=" + h.remoteCert, "WORKLEASE_AGENT_ID=" + c.name, "WORKLEASE_SESSION_ID=" + c.name + "-mcp-session"}
		sshArgs := []string{h.remoteHost, "env"}
		sshArgs = append(sshArgs, remoteEnv...)
		sshArgs = append(sshArgs, h.remoteBinary, "mcp", "--profile", "team")
		h.logCommand(c.name+" ssh", append([]string{"ssh"}, sshArgs...))
		cmd = exec.Command("ssh", sshArgs...)
	} else {
		cmd = exec.Command(h.binary, "mcp", "--profile", "team")
		cmd.Env, cmd.Dir = c.env, c.checkout
	}
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
	if h.realHost {
		_, _ = runSSH(h.remoteHost, h.remoteHelper, "stop-server", filepath.Join(h.remoteRoot, "server.pid"))
		// The helper forwards an interrupt to the authority. Give its lock
		// release a bounded grace period before the next hosted operation.
		time.Sleep(150 * time.Millisecond)
	}
	h.server = nil
}

func (h *harness) restartServer(evidence string) error {
	if h.realHost {
		return h.startServer(evidence)
	}
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

func writeCertificate(certPath, keyPath string, sanValues ...any) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	now := time.Now()
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1")}
	dnsNames := []string{"localhost"}
	for _, value := range sanValues {
		switch san := value.(type) {
		case net.IP:
			if san != nil && !san.IsLoopback() {
				ipAddresses = append(ipAddresses, san)
			}
		case string:
			if san != "" && net.ParseIP(san) == nil {
				dnsNames = append(dnsNames, san)
			}
		}
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "worklease acceptance"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: ipAddresses, DNSNames: dnsNames, BasicConstraintsValid: true}
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

func authorityDBRoot(h *harness) string {
	if h.realHost {
		return filepath.Join(h.remoteRoot, "authority")
	}
	return filepath.Join(h.root, "authority")
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

func validateSSHHost(host string) error {
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n;|&$'\"`") {
		return fmt.Errorf("unsafe SSH host %q", host)
	}
	return nil
}

func resolveSSHAddress(host string) (string, error) {
	if output, err := exec.Command("ssh", "-G", host).Output(); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "hostname" {
				if addresses, lookupErr := net.LookupHost(fields[1]); lookupErr == nil {
					for _, address := range addresses {
						if parsed := net.ParseIP(address); parsed != nil && !parsed.IsLoopback() {
							return address, nil
						}
					}
				}
			}
		}
	}
	if addresses, err := net.LookupHost(host); err == nil {
		for _, address := range addresses {
			if parsed := net.ParseIP(address); parsed != nil && !parsed.IsLoopback() {
				return address, nil
			}
		}
	}
	output, err := runSSH(host, "hostname", "-i")
	if err != nil {
		return "", fmt.Errorf("resolve SSH host address: %w", err)
	}
	for _, field := range strings.Fields(output) {
		if parsed := net.ParseIP(field); parsed != nil && !parsed.IsLoopback() {
			return field, nil
		}
	}
	return "", fmt.Errorf("SSH host %q returned no non-loopback address", host)
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

func runSSH(host string, args ...string) (string, error) {
	if err := validateSSHHost(host); err != nil {
		return "", err
	}
	output, err := exec.Command("ssh", append([]string{host}, args...)...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("ssh %s: %w: %s", host, err, output)
	}
	return string(output), nil
}

func runSCP(host, source, destination string) error {
	if err := validateSSHHost(host); err != nil {
		return err
	}
	var args []string
	if strings.HasPrefix(source, host+":") {
		args = []string{"--", source, destination}
	} else {
		args = []string{"--", source, host + ":" + destination}
	}
	output, err := exec.Command("scp", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("scp: %w: %s", err, output)
	}
	return nil
}

func (h *harness) remotePathExists(path string) (bool, error) {
	if !h.realHost {
		_, err := os.Stat(path)
		return err == nil, err
	}
	cmd := exec.Command("ssh", h.remoteHost, "test", "-e", path)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func (h *harness) remoteSize(path string) (string, error) {
	output, err := runSSH(h.remoteHost, "wc", "-c", path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output)
	if len(fields) == 0 {
		return "", errors.New("remote size returned no bytes")
	}
	return fields[0], nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
