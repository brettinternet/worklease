// Command worklease-remote-smoke exercises the shipped binary as one TLS
// authority and two isolated clients. With --remote-host, authority and client B
// run over SSH on a real host while client A and the orchestrator remain local.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/creack/pty"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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
	binary, self, root, endpoint, cert, configPath, authorityHome   string
	remoteHost, remoteRoot, remoteAddress, sshConfig                string
	remoteGOOS, remoteGOARCH                                        string
	remotePort                                                      int
	remoteBinary, remoteHelper, remoteConfig, remoteCert, remoteKey string
	commandLog                                                      string
	server                                                          *exec.Cmd
	faultProxy                                                      *exec.Cmd
	faultEndpoint, faultControl, faultLog                           string
	clients                                                         [2]client
	report                                                          report
	realHost                                                        bool
	supportingTests                                                 []supportingTestEvidence
	immutableEnrollmentClient, zeroPendingClient                    client
	immutableEnrollmentID, immutableEnrollmentInvite                string
	immutableEnrollmentBefore                                       authority.PendingRequest
	pendingSurvivalBefore                                           []byte
	fullVolumeMaxPageCount                                          int64
	preRestoreCursor                                                string
}

type client struct {
	name, home, config, checkout string
	env                          []string
	remote                       bool
}

type cliFailureResult struct {
	result map[string]any
	err    error
}

type bootstrapState struct {
	ReadyMarker    bool   `json:"readyMarker"`
	BootstrapReady bool   `json:"bootstrapReady"`
	ActiveInvites  int    `json:"activeInvites"`
	RevokedInvites int    `json:"revokedInvites"`
	SecretMode     string `json:"secretMode"`
}

type fullVolumeState struct {
	PageCount     int64  `json:"pageCount"`
	MaxPageCount  int64  `json:"maxPageCount"`
	FreelistCount int64  `json:"freelistCount"`
	Injection     string `json:"injection"`
}

type storageSnapshot struct {
	Tables map[string]int64  `json:"tables"`
	Meta   map[string]string `json:"meta"`
}

type backupCapture struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	StartedAt     string `json:"startedAt"`
	DurableCutoff string `json:"durableCutoff"`
	SHA256        string `json:"sha256"`
	EventSequence int64  `json:"eventSequence"`
}

type pendingSetInventory struct {
	Label      string   `json:"label"`
	Root       string   `json:"root"`
	RequestIDs []string `json:"requestIds"`
}

type schemaUpgradeState struct {
	UserVersion       int64  `json:"userVersion"`
	AuthorityID       string `json:"authorityId"`
	RestoreID         string `json:"restoreId"`
	StartedOperations int64  `json:"startedOperations"`
	CompletedReplay   int64  `json:"completedReplay"`
	Events            int64  `json:"events"`
	RequiredV2Objects int64  `json:"requiredV2Objects"`
	LegacyOpenReason  string `json:"legacyOpenReason"`
}

type promptCapture struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	prompt  chan struct{}
	noticed bool
}

func (c *promptCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.buffer.Write(p)
	if !c.noticed && bytes.Contains(c.buffer.Bytes(), []byte("Invite: ")) {
		c.noticed = true
		close(c.prompt)
	}
	return n, err
}

func (c *promptCapture) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...)
}

func writeProviderSubmission(path, effectID string) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	_, err = fmt.Fprintf(file, "provider-submitted %s\n", effectID)
	return err
}

func isAcceptanceWorkspacePath(path string) bool {
	clean := filepath.Clean(path)
	return clean == path && (strings.HasPrefix(path, "/tmp/worklease-acceptance-") || strings.HasPrefix(path, "/private/tmp/worklease-acceptance-"))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "replay-pending" {
		if len(os.Args) != 6 && len(os.Args) != 7 {
			fatal(errors.New("replay-pending requires config root, home, profile, request ID, and optional invite file"))
		}
		paths := config.UserProfilePaths(func(name string) string {
			if name == "XDG_CONFIG_HOME" {
				return os.Args[2]
			}
			return ""
		})
		profiles, _, err := config.LoadProfiles(paths)
		if err != nil {
			fatal(err)
		}
		profile, ok := profiles[os.Args[4]]
		if !ok {
			fatal(fmt.Errorf("profile %q is missing", os.Args[4]))
		}
		client, err := authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(os.Args[3], "pending", os.Args[4])), nil)
		if err != nil {
			fatal(err)
		}
		if len(os.Args) == 6 {
			_, err = client.Replay(context.Background(), os.Args[5])
		} else {
			invite, readErr := os.ReadFile(os.Args[6])
			if readErr != nil {
				fatal(readErr)
			}
			_, _, err = client.ReplayEnrollment(context.Background(), os.Args[5], strings.TrimSpace(string(invite)), paths)
		}
		if err != nil {
			if classified := reason.As(err); classified != nil {
				fatal(fmt.Errorf("%s: %w", classified.Reason, err))
			}
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "file-sha256" {
		if len(os.Args) != 3 {
			fatal(errors.New("file-sha256 requires a path"))
		}
		contents, err := os.ReadFile(os.Args[2])
		if err != nil {
			fatal(err)
		}
		digest := sha256.Sum256(contents)
		fmt.Println(hex.EncodeToString(digest[:]))
		return
	}
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
	if len(os.Args) > 1 && os.Args[1] == "delayed-effect" {
		if len(os.Args) != 4 {
			fatal(errors.New("delayed-effect requires a duration and path"))
		}
		delay, err := time.ParseDuration(os.Args[2])
		if err != nil {
			fatal(err)
		}
		time.Sleep(delay)
		file, err := os.OpenFile(os.Args[3], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = io.WriteString(file, "dispatch\n")
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "gated-effect" {
		if len(os.Args) != 5 {
			fatal(errors.New("gated-effect requires ready, release, and effect paths"))
		}
		if err := os.WriteFile(os.Args[2], []byte("effect ready\n"), 0o600); err != nil {
			fatal(err)
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			release, err := os.ReadFile(os.Args[3])
			if err == nil && strings.TrimSpace(string(release)) == "release effect" {
				break
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				fatal(err)
			}
			if time.Now().After(deadline) {
				fatal(errors.New("timed out waiting to release gated effect"))
			}
			time.Sleep(25 * time.Millisecond)
		}
		file, err := os.OpenFile(os.Args[4], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = io.WriteString(file, "dispatch\n")
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "submit-provider-effect" {
		if len(os.Args) != 4 {
			fatal(errors.New("submit-provider-effect requires a path and effect ID"))
		}
		if err := writeProviderSubmission(os.Args[2], os.Args[3]); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "provider-effect-worker" {
		if len(os.Args) != 6 {
			fatal(errors.New("provider-effect-worker requires submitted, release, completed, and effect ID paths"))
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			submitted, submitErr := os.ReadFile(os.Args[2])
			release, releaseErr := os.ReadFile(os.Args[3])
			if submitErr == nil && releaseErr == nil && strings.TrimSpace(string(submitted)) == "provider-submitted "+os.Args[5] {
				if _, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(release))); parseErr != nil {
					fatal(parseErr)
				}
				file, err := os.OpenFile(os.Args[4], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err == nil {
					_, err = fmt.Fprintf(file, "provider-completed %s at=%s\n", os.Args[5], time.Now().UTC().Format(time.RFC3339Nano))
					if closeErr := file.Close(); err == nil {
						err = closeErr
					}
				}
				if err != nil {
					fatal(err)
				}
				return
			}
			if submitErr != nil && !errors.Is(submitErr, os.ErrNotExist) || releaseErr != nil && !errors.Is(releaseErr, os.ErrNotExist) {
				fatal(errors.Join(submitErr, releaseErr))
			}
			time.Sleep(25 * time.Millisecond)
		}
		fatal(errors.New("timed out waiting to release provider effect"))
	}
	if len(os.Args) > 1 && os.Args[1] == "fault-proxy" {
		if len(os.Args) != 9 {
			fatal(errors.New("fault-proxy requires listen, backend, cert, key, control, log, and pid file"))
		}
		if err := serveFaultProxy(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6], os.Args[7], os.Args[8]); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "wait-file" {
		if len(os.Args) != 6 {
			fatal(errors.New("wait-file requires a path, two exact content fields, and timeout"))
		}
		expected := os.Args[3] + " " + os.Args[4]
		timeout, err := time.ParseDuration(os.Args[5])
		if err != nil {
			fatal(err)
		}
		deadline := time.Now().Add(timeout)
		for {
			data, readErr := os.ReadFile(os.Args[2])
			if readErr == nil && strings.TrimSpace(string(data)) == expected {
				return
			}
			if time.Now().After(deadline) {
				fatal(fmt.Errorf("timed out waiting for %s to contain %q", os.Args[2], expected))
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "owner-marker" {
		if len(os.Args) != 3 || !isAcceptanceWorkspacePath(os.Args[2]) {
			fatal(errors.New("owner-marker requires an acceptance workspace"))
		}
		marker := filepath.Join(os.Args[2], ".worklease-acceptance-owner")
		if err := os.WriteFile(marker, []byte(fmt.Sprintf("pid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))), 0o600); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if len(os.Args) != 4 && len(os.Args) != 7 {
			fatal(errors.New("backup requires source and destination, optionally followed by control, ready, and result paths"))
		}
		if len(os.Args) == 4 {
			if err := backupSQLite(os.Args[2], os.Args[3]); err != nil {
				fatal(err)
			}
			return
		}
		if err := controlledBackupSQLite(os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6]); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "schema-v1-fixture" || os.Args[1] == "schema-upgrade-state") {
		if len(os.Args) != 3 {
			fatal(fmt.Errorf("%s requires a schema fixture home", os.Args[1]))
		}
		if os.Args[1] == "schema-v1-fixture" {
			if err := createSchemaV1Fixture(os.Args[2]); err != nil {
				fatal(err)
			}
			return
		}
		state, err := readSchemaUpgradeState(os.Args[2])
		if err != nil {
			fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(state); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "recovery-closure-fixture" {
		if len(os.Args) != 3 {
			fatal(errors.New("recovery-closure-fixture requires an acceptance database"))
		}
		result, err := installRecoveryClosureFixture(os.Args[2])
		if err != nil {
			fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "age-retention-fixture" {
		if len(os.Args) != 4 {
			fatal(errors.New("age-retention-fixture requires a database and Unix-microsecond timestamp"))
		}
		at, err := strconv.ParseInt(os.Args[3], 10, 64)
		if err != nil {
			fatal(errors.New("age-retention-fixture timestamp is invalid"))
		}
		if err := ageRetentionFixture(os.Args[2], at); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "full-volume-fixture" || os.Args[1] == "clear-full-volume-fixture" || os.Args[1] == "storage-snapshot") {
		if len(os.Args) != 3 {
			fatal(fmt.Errorf("%s requires an acceptance database", os.Args[1]))
		}
		var value any
		var err error
		switch os.Args[1] {
		case "full-volume-fixture":
			value, err = installFullVolumeFixture(os.Args[2])
		case "clear-full-volume-fixture":
			err = clearFullVolumeFixture(os.Args[2])
			value = map[string]bool{"cleared": err == nil}
		case "storage-snapshot":
			value, err = readStorageSnapshot(os.Args[2])
		}
		if err != nil {
			fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "bootstrap-state" {
		if len(os.Args) != 4 {
			fatal(errors.New("bootstrap-state requires a hosted home and secret path"))
		}
		state, err := readBootstrapState(os.Args[2], os.Args[3])
		if err != nil {
			fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(state); err != nil {
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
		if len(os.Args) != 5 && len(os.Args) != 6 {
			fatal(errors.New("serve requires binary, config, pid file, and optional acceptance SQLite page limit"))
		}
		serve := exec.Command(os.Args[2], "serve", "--server-config", os.Args[3])
		serve.Stdout, serve.Stderr = os.Stdout, os.Stderr
		if len(os.Args) == 6 {
			serve.Env = append(os.Environ(), "WORKLEASE_ACCEPTANCE_SQLITE_MAX_PAGE_COUNT="+os.Args[5])
		}
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
	remoteHost := flag.String("remote-host", "", "run authority and client B on this SSH host (for example lima-worklease-remote)")
	sshConfig := flag.String("ssh-config", "", "OpenSSH config used for remote-host SSH and SCP commands")
	flag.Parse()
	if err := run(*binary, *evidence, *keep, *remoteHost, *sshConfig); err != nil {
		fatal(err)
	}
}

func installSSHWrappers(root, configPath string) error {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(configPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("SSH config is not a regular file: %s", configPath)
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}
	scpPath, err := exec.LookPath("scp")
	if err != nil {
		return err
	}
	wrapperRoot := filepath.Join(root, "ssh-bin")
	if err := os.MkdirAll(wrapperRoot, 0o700); err != nil {
		return err
	}
	for name, executable := range map[string]string{"ssh": sshPath, "scp": scpPath} {
		contents := fmt.Sprintf("#!/bin/sh\nexec %q -F \"$WORKLEASE_ACCEPTANCE_SSH_CONFIG\" \"$@\"\n", executable)
		if err := os.WriteFile(filepath.Join(wrapperRoot, name), []byte(contents), 0o700); err != nil {
			return err
		}
	}
	if err := os.Setenv("WORKLEASE_ACCEPTANCE_SSH_CONFIG", configPath); err != nil {
		return err
	}
	return os.Setenv("PATH", wrapperRoot+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func run(binary, evidence string, keep bool, remoteHosts ...string) error {
	remoteHost, sshConfig := "", ""
	if len(remoteHosts) > 0 {
		remoteHost = remoteHosts[0]
	}
	if len(remoteHosts) > 1 {
		sshConfig = remoteHosts[1]
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
	if err := writeAcceptanceOwnerMarker(root); err != nil {
		return err
	}
	if evidence == "" {
		stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
		evidence = filepath.Join("dist", "remote-acceptance", stamp)
	}
	evidence, err = filepath.Abs(evidence)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if sshConfig != "" {
		if remoteHost == "" {
			return errors.New("--ssh-config requires --remote-host")
		}
		if err := installSSHWrappers(root, sshConfig); err != nil {
			return err
		}
	}
	h := &harness{binary: absoluteBinary, self: self, root: root, commandLog: filepath.Join(evidence, "commands.log"), remoteHost: remoteHost, sshConfig: sshConfig, realHost: remoteHost != ""}
	mode, latencyKind, hosts := "local-development", "loopback (not real-host WAN)", "1 process host / 2 isolated roots"
	if h.realHost {
		mode, latencyKind, hosts = "real-host-smoke", "measured end-to-end remote client command latency (includes SSH orchestration)", "orchestrator/client-A local; authority/client-B remote"
	}
	h.report = report{SchemaVersion: 2, Mode: mode, LatencyKind: latencyKind, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Environment: map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "authorityHosts": "1", "clientHosts": hosts}, EffectDispatchCounts: map[string]int{}}
	if h.realHost {
		h.report.Environment["sshHost"] = remoteHost
		if sshConfig != "" {
			h.report.Environment["sshConfig"] = sshConfig
		}
		h.report.RenewalMargins = []string{"10m contention TTL exercised; renewal timing measured by the guarded lifecycle observations"}
		h.report.RecoveryBounds = "selected backup cutoff measured separately; a second restore records the cutoff as explicitly unknown and reopens only after exhaustive installation, pending-set, retained-outcome, and provider-cessation evidence"
	} else {
		h.report.RenewalMargins = []string{"guarded start exercised with 10m TTL", "real-host renewal margin unmeasured"}
		h.report.RecoveryBounds = "development fixture only; real-host recovery bounds remain unmeasured"
	}
	defer h.stopFaultProxy()
	defer h.stopServer()
	if err := h.provision(evidence); err != nil {
		return err
	}
	if h.realHost {
		h.supportingTests = runSupportingTests(evidence)
		h.supportingTests = append(h.supportingTests, h.runRemoteSupportingTests(evidence)...)
		for _, test := range h.supportingTests {
			if test.Status == "failed" {
				return fmt.Errorf("required supporting test failed: %s (%s)", test.Command, test.Log)
			}
		}
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
	h.configPath, h.authorityHome = configPath, authorityHome
	config := h.serverConfig([]string{"coordination:"}, "1h", "24h")
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

func normalizeGoTarget(osName, archName string) (string, string, error) {
	goos := strings.ToLower(strings.TrimSpace(osName))
	switch goos {
	case "darwin", "linux":
	default:
		return "", "", fmt.Errorf("unsupported remote operating system %q", strings.TrimSpace(osName))
	}
	var goarch string
	switch strings.ToLower(strings.TrimSpace(archName)) {
	case "arm64", "aarch64":
		goarch = "arm64"
	case "amd64", "x86_64":
		goarch = "amd64"
	default:
		return "", "", fmt.Errorf("unsupported remote architecture %q", strings.TrimSpace(archName))
	}
	return goos, goarch, nil
}

func (h *harness) buildRemoteTarget(goos, goarch string) (string, string, error) {
	buildRoot := filepath.Join(h.root, "remote-target")
	if err := os.MkdirAll(buildRoot, 0o700); err != nil {
		return "", "", err
	}
	workleasePath := filepath.Join(buildRoot, "worklease")
	helperPath := filepath.Join(buildRoot, "harness-helper")
	for output, packagePath := range map[string]string{workleasePath: "./cmd/worklease", helperPath: "./cmd/worklease-remote-smoke"} {
		cmd := exec.Command("go", "build", "-trimpath", "-o", output, packagePath)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
		combined, err := cmd.CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("build %s/%s %s: %w: %s", goos, goarch, packagePath, err, combined)
		}
	}
	return workleasePath, helperPath, nil
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
	workspaceOutput, err := runSSH(h.remoteHost, "mktemp", "-d", "/tmp/worklease-acceptance-XXXXXX")
	if err != nil {
		return fmt.Errorf("remote workspace: %w", err)
	}
	h.remoteRoot = strings.TrimSpace(workspaceOutput)
	if !isAcceptanceWorkspacePath(h.remoteRoot) || strings.ContainsAny(h.remoteRoot, "\r\n") {
		return fmt.Errorf("remote workspace has unsafe path %q", h.remoteRoot)
	}
	h.report.RemoteWorkspace = h.remoteHost + ":" + h.remoteRoot
	h.remoteBinary = filepath.Join(h.remoteRoot, "worklease")
	h.remoteHelper = filepath.Join(h.remoteRoot, "harness-helper")
	remoteOSOutput, err := runSSH(h.remoteHost, "uname", "-s")
	if err != nil {
		return fmt.Errorf("detect remote operating system: %w", err)
	}
	remoteArchOutput, err := runSSH(h.remoteHost, "uname", "-m")
	if err != nil {
		return fmt.Errorf("detect remote architecture: %w", err)
	}
	remoteOS, remoteArch, err := normalizeGoTarget(remoteOSOutput, remoteArchOutput)
	if err != nil {
		return err
	}
	h.remoteGOOS, h.remoteGOARCH = remoteOS, remoteArch
	h.report.Environment["remoteGoos"], h.report.Environment["remoteGoarch"] = remoteOS, remoteArch
	remoteWorklease, remoteHelper, err := h.buildRemoteTarget(remoteOS, remoteArch)
	if err != nil {
		return err
	}
	for local, remote := range map[string]string{remoteWorklease: h.remoteBinary, remoteHelper: h.remoteHelper} {
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
	h.configPath, h.authorityHome = h.remoteConfig, authorityHome
	config := h.serverConfig([]string{"coordination:"}, "1h", "24h")
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
	if _, err := runSSH(h.remoteHost, "chmod", "700", filepath.Dir(b.home), b.home, b.config, b.checkout); err != nil {
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
	if h.fullVolumeMaxPageCount > 0 {
		serverArgs = append(serverArgs, strconv.FormatInt(h.fullVolumeMaxPageCount, 10))
	}
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

type faultProxyHandler struct {
	backend *url.URL
	client  *http.Client
	control string
	log     string
	mu      sync.Mutex
}

func serveFaultProxy(listen, backend, certPath, keyPath, control, logPath, pidPath string) error {
	backendURL, err := url.Parse(backend)
	if err != nil {
		return err
	}
	certificate, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		return errors.New("fault proxy certificate contains no trusted certificate")
	}
	handler := &faultProxyHandler{backend: backendURL, client: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}, control: control, log: logPath}
	server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}, TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		return err
	}
	defer os.Remove(pidPath)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		<-signals
		_ = server.Close()
	}()
	err = server.ListenAndServeTLS(certPath, keyPath)
	signal.Stop(signals)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

var errDropResponse = errors.New("injected response loss")

func (p *faultProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	digest := sha256.Sum256(body)
	requestHash := hex.EncodeToString(digest[:])
	if err := p.waitIfHeld(r.URL.Path, requestHash); err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}
	p.appendLog(fmt.Sprintf("path=%s phase=forwarded requestSha256=%s at=%s\n", r.URL.Path, requestHash, time.Now().UTC().Format(time.RFC3339Nano)))
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(p.backend)
			request.Out.Host = p.backend.Host
		},
		Transport: p.client.Transport,
		ModifyResponse: func(response *http.Response) error {
			responseBody, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				return readErr
			}
			delay, authorityOffset := p.consumeResponseFault(r.URL.Path, requestHash)
			if authorityOffset != 0 {
				responseBody, readErr = shiftAuthorityTime(responseBody, authorityOffset)
				if readErr != nil {
					return readErr
				}
			}
			if delay > 0 {
				time.Sleep(delay)
			}
			drop := p.consume(r.URL.Path, response.StatusCode, requestHash, body, responseBody, delay)
			if drop {
				return errDropResponse
			}
			response.Body = io.NopCloser(bytes.NewReader(responseBody))
			response.ContentLength = int64(len(responseBody))
			return nil
		},
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
			if errors.Is(proxyErr, errDropResponse) {
				if hijacker, ok := writer.(http.Hijacker); ok {
					connection, _, err := hijacker.Hijack()
					if err == nil {
						_ = connection.Close()
						return
					}
				}
				panic(http.ErrAbortHandler)
			}
			http.Error(writer, proxyErr.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func (p *faultProxyHandler) waitIfHeld(path, requestHash string) error {
	p.mu.Lock()
	armed, _ := os.ReadFile(p.control)
	hold := strings.TrimSpace(string(armed)) == "hold "+path
	if hold {
		_ = os.WriteFile(p.control, []byte("held "+path+"\n"), 0o600)
		p.appendLog(fmt.Sprintf("path=%s phase=held requestSha256=%s at=%s\n", path, requestHash, time.Now().UTC().Format(time.RFC3339Nano)))
	}
	p.mu.Unlock()
	if !hold {
		return nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		armed, _ := os.ReadFile(p.control)
		if strings.TrimSpace(string(armed)) == "release "+path {
			p.mu.Lock()
			_ = os.WriteFile(p.control, nil, 0o600)
			p.appendLog(fmt.Sprintf("path=%s phase=released requestSha256=%s at=%s\n", path, requestHash, time.Now().UTC().Format(time.RFC3339Nano)))
			p.mu.Unlock()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out holding %s", path)
}

func (p *faultProxyHandler) appendLog(entry string) {
	if file, err := os.OpenFile(p.log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_, _ = io.WriteString(file, entry)
		_ = file.Close()
	}
}

func (p *faultProxyHandler) consumeResponseFault(path, requestHash string) (time.Duration, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	armed, _ := os.ReadFile(p.control)
	fields := strings.Fields(string(armed))
	if (len(fields) != 3 && len(fields) != 4) || fields[0] != "delay-response" || fields[1] != path {
		return 0, 0
	}
	delay, err := time.ParseDuration(fields[2])
	if err != nil || delay <= 0 || delay > 10*time.Second {
		return 0, 0
	}
	authorityOffset := time.Duration(0)
	if len(fields) == 4 {
		authorityOffset, err = time.ParseDuration(fields[3])
		if err != nil || authorityOffset < -12*time.Hour || authorityOffset > 12*time.Hour {
			return 0, 0
		}
	}
	_ = os.WriteFile(p.control, nil, 0o600)
	p.appendLog(fmt.Sprintf("path=%s phase=response-delay requestSha256=%s duration=%s authorityOffset=%s at=%s\n", path, requestHash, delay, authorityOffset, time.Now().UTC().Format(time.RFC3339Nano)))
	return delay, authorityOffset
}

func shiftAuthorityTime(body []byte, offset time.Duration) ([]byte, error) {
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	raw, ok := envelope["authorityTime"].(string)
	if !ok {
		return nil, errors.New("response has no authority time")
	}
	authorityTime, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	envelope["authorityTime"] = authorityTime.Add(offset).UTC().Format(time.RFC3339Nano)
	return json.Marshal(envelope)
}

func (p *faultProxyHandler) consume(path string, status int, requestHash string, requestBody, responseBody []byte, responseDelay time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	armed, _ := os.ReadFile(p.control)
	drop := strings.TrimSpace(string(armed)) == "drop "+path
	if drop {
		_ = os.WriteFile(p.control, nil, 0o600)
	}
	responseFields := ""
	var request struct {
		RequestID       string    `json:"requestId"`
		OperationID     string    `json:"operationId"`
		ClaimID         string    `json:"claimId"`
		RequestNotAfter time.Time `json:"requestNotAfter"`
	}
	if json.Unmarshal(requestBody, &request) == nil {
		requestID := request.RequestID
		if requestID == "" {
			requestID = request.OperationID
		}
		if requestID == "" {
			requestID = request.ClaimID
		}
		if requestID != "" {
			responseFields += " requestId=" + requestID
		}
		if !request.RequestNotAfter.IsZero() {
			responseFields += " requestNotAfter=" + request.RequestNotAfter.UTC().Format(time.RFC3339Nano)
		}
	}
	var envelope struct {
		AuthorityID   string          `json:"authorityId"`
		RestoreID     string          `json:"restoreId"`
		AuthorityTime time.Time       `json:"authorityTime"`
		Result        json.RawMessage `json:"result"`
		Error         struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	if json.Unmarshal(responseBody, &envelope) == nil && envelope.AuthorityID != "" && envelope.RestoreID != "" && !envelope.AuthorityTime.IsZero() {
		responseFields += fmt.Sprintf(" authorityId=%s restoreId=%s authorityTime=%s historicalResultSha256=%s", envelope.AuthorityID, envelope.RestoreID, envelope.AuthorityTime.UTC().Format(time.RFC3339Nano), historicalResultHash(envelope.Result))
		if envelope.Error.Reason != "" {
			responseFields += " reason=" + envelope.Error.Reason
		}
	}
	entry := fmt.Sprintf("path=%s status=%d requestSha256=%s dropped=%t responseDelay=%s%s at=%s\n", path, status, requestHash, drop, responseDelay, responseFields, time.Now().UTC().Format(time.RFC3339Nano))
	p.appendLog(entry)
	return drop
}

func (h *harness) startFaultProxy(evidence string) error {
	port, err := freePort()
	if h.realHost {
		var output string
		output, err = runSSH(h.remoteHost, h.remoteHelper, "free-port")
		if err == nil {
			port, err = strconv.Atoi(strings.TrimSpace(output))
		}
	}
	if err != nil {
		return err
	}
	listen, cert, key, helper := fmt.Sprintf("127.0.0.1:%d", port), h.cert, filepath.Join(h.root, "secrets", "tls.key"), h.self
	h.faultControl, h.faultLog = filepath.Join(h.root, "fault-control"), filepath.Join(evidence, "fault-proxy.log")
	pidPath := filepath.Join(h.root, "fault-proxy.pid")
	if h.realHost {
		listen, cert, key, helper = fmt.Sprintf("0.0.0.0:%d", port), h.remoteCert, h.remoteKey, h.remoteHelper
		h.faultControl, h.faultLog = filepath.Join(h.remoteRoot, "fault-control"), filepath.Join(h.remoteRoot, "fault-proxy.log")
		pidPath = filepath.Join(h.remoteRoot, "fault-proxy.pid")
		h.faultEndpoint = fmt.Sprintf("https://%s:%d", h.remoteAddress, port)
		h.faultProxy = exec.Command("ssh", h.remoteHost, helper, "fault-proxy", listen, h.endpoint, cert, key, h.faultControl, h.faultLog, pidPath)
	} else {
		h.faultEndpoint = "https://" + listen
		h.faultProxy = exec.Command(helper, "fault-proxy", listen, h.endpoint, cert, key, h.faultControl, h.faultLog, pidPath)
	}
	if err := h.faultProxy.Start(); err != nil {
		return err
	}
	return waitHealthy(h.faultEndpoint, h.cert)
}

func (h *harness) stopFaultProxy() {
	if h.faultProxy == nil || h.faultProxy.Process == nil {
		return
	}
	if h.realHost {
		_, _ = runSSH(h.remoteHost, h.remoteHelper, "stop-server", filepath.Join(h.remoteRoot, "fault-proxy.pid"))
	} else {
		_ = h.faultProxy.Process.Signal(os.Interrupt)
	}
	_, _ = h.faultProxy.Process.Wait()
	h.faultProxy = nil
}

func (h *harness) setFaultControl(command string) error {
	if !h.realHost {
		return os.WriteFile(h.faultControl, []byte(command+"\n"), 0o600)
	}
	local := filepath.Join(h.root, "fault-control-next")
	if err := os.WriteFile(local, []byte(command+"\n"), 0o600); err != nil {
		return err
	}
	return runSCP(h.remoteHost, local, h.faultControl)
}

func (h *harness) armFault(path string) error {
	return h.setFaultControl("drop " + path)
}

func (h *harness) delayResponse(path string, delay time.Duration) error {
	return h.delayResponseWithAuthorityOffset(path, delay, 0)
}

func (h *harness) delayResponseWithAuthorityOffset(path string, delay, offset time.Duration) error {
	command := "delay-response " + path + " " + delay.String()
	if offset != 0 {
		command += " " + offset.String()
	}
	return h.setFaultControl(command)
}

func (h *harness) holdFault(path string) error {
	return h.setFaultControl("hold " + path)
}

func (h *harness) releaseFault(path string) error {
	return h.setFaultControl("release " + path)
}

func (h *harness) requireFaultStillArmed(path string) error {
	expected := "hold " + path
	if h.realHost {
		_, err := runSSH(h.remoteHost, h.remoteHelper, "wait-file", h.faultControl, "hold", path, "250ms")
		return err
	}
	data, err := os.ReadFile(h.faultControl)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != expected {
		return fmt.Errorf("fault reached proxy before client persistence: control=%q", strings.TrimSpace(string(data)))
	}
	return nil
}

func (h *harness) waitFaultHeld(path string) error {
	expected := "held " + path
	if h.realHost {
		_, err := runSSH(h.remoteHost, h.remoteHelper, "wait-file", h.faultControl, "held", path, "10s")
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(h.faultControl)
		if err == nil && strings.TrimSpace(string(data)) == expected {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for held fault %s", path)
}

func (h *harness) waitFaultLogOccurrences(fragment string, want int) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var data []byte
		if h.realHost {
			output, err := runSSH(h.remoteHost, "grep", "-F", fragment, h.faultLog)
			if err == nil {
				data = []byte(output)
			}
		} else {
			data, _ = os.ReadFile(h.faultLog)
		}
		if bytes.Count(data, []byte(fragment)) >= want {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d fault log entries containing %q", want, fragment)
}

func (h *harness) collectFaultLog(evidence string) error {
	if h.realHost {
		if err := runSCP(h.remoteHost, h.remoteHost+":"+h.faultLog, filepath.Join(evidence, "fault-proxy.log")); err != nil {
			return err
		}
	}
	return verifyFaultReplays(filepath.Join(evidence, "fault-proxy.log"), []string{"/v1/operations/begin", "/v1/operations/renew", "/v1/operations/complete"})
}

func verifyNoFaultDispatch(logPath, path string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		values := map[string]string{}
		for _, field := range strings.Fields(line) {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				values[parts[0]] = parts[1]
			}
		}
		if values["path"] == path {
			return fmt.Errorf("pre-dispatch persistence failure reached %s", path)
		}
	}
	return nil
}

func withBlockedPendingRoot(root string, run func() error) (err error) {
	backup := root + ".acceptance-backup"
	if _, statErr := os.Lstat(backup); !errors.Is(statErr, os.ErrNotExist) {
		if statErr == nil {
			return fmt.Errorf("pending backup path already exists: %s", backup)
		}
		return statErr
	}
	if err := os.Rename(root, backup); err != nil {
		return err
	}
	if err := os.WriteFile(root, []byte("injected pending persistence failure\n"), 0o600); err != nil {
		return errors.Join(err, os.Rename(backup, root))
	}
	defer func() {
		err = errors.Join(err, os.Remove(root), os.Rename(backup, root))
	}()
	return run()
}

func verifyFaultGates(logPath, path string, want int) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	held, released := map[string]int{}, map[string]int{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "path="+path {
			continue
		}
		phase := strings.TrimPrefix(fields[1], "phase=")
		hash := strings.TrimPrefix(fields[2], "requestSha256=")
		switch phase {
		case "held":
			held[hash]++
		case "released":
			released[hash]++
		}
	}
	pairs := 0
	for hash, count := range held {
		if count == 1 && released[hash] == 1 {
			pairs++
		}
	}
	if pairs != want {
		return fmt.Errorf("fault path %s has %d matched hold/release pairs, want %d", path, pairs, want)
	}
	return nil
}

func verifyFaultReplays(logPath string, paths []string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	for _, path := range paths {
		dropped, replayed := map[string]int{}, map[string]int{}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[0] != "path="+path {
				continue
			}
			hash := strings.TrimPrefix(fields[2], "requestSha256=")
			if fields[3] == "dropped=true" {
				dropped[hash]++
			} else if dropped[hash] > replayed[hash] {
				replayed[hash]++
			}
		}
		if len(dropped) == 0 {
			return fmt.Errorf("fault path %s has no injected response loss", path)
		}
		for hash, count := range dropped {
			if replayed[hash] != count {
				return fmt.Errorf("fault path %s request %s has %d drops and %d exact same-body replays", path, hash, count, replayed[hash])
			}
		}
	}
	return nil
}

func historicalResultHash(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		return ""
	}
	delete(result, "idempotent")
	normalized, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(normalized)
	return hex.EncodeToString(digest[:])
}

func verifyFreshReplayEnvelope(logPath, path, authorityID string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	type observation struct {
		requestHash, authorityID, restoreID, resultHash string
		authorityTime                                   time.Time
		dropped                                         bool
	}
	var first *observation
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] != "path="+path {
			continue
		}
		values := map[string]string{}
		for _, field := range fields {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				values[parts[0]] = parts[1]
			}
		}
		observedTime, parseErr := time.Parse(time.RFC3339Nano, values["authorityTime"])
		if parseErr != nil {
			continue
		}
		current := &observation{requestHash: values["requestSha256"], authorityID: values["authorityId"], restoreID: values["restoreId"], resultHash: values["historicalResultSha256"], authorityTime: observedTime, dropped: values["dropped"] == "true"}
		if current.dropped {
			first = current
			continue
		}
		if first == nil || current.requestHash != first.requestHash {
			continue
		}
		if first.authorityID != authorityID || current.authorityID != authorityID || first.restoreID == "" || current.restoreID != first.restoreID {
			return fmt.Errorf("replay response identity changed: first=%s/%s replay=%s/%s", first.authorityID, first.restoreID, current.authorityID, current.restoreID)
		}
		if first.resultHash == "" || current.resultHash != first.resultHash {
			return errors.New("replay did not preserve the historical result")
		}
		if !current.authorityTime.After(first.authorityTime) {
			return fmt.Errorf("replay authority time was not fresh: first=%s replay=%s", first.authorityTime, current.authorityTime)
		}
		return nil
	}
	return fmt.Errorf("fault path %s has no dropped response and matching fresh replay envelope", path)
}

func verifyLateAcknowledgmentEvidence(logPath, replayedStartID, lateStartID string, lateStartTTL time.Duration) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	var droppedHash string
	replayed, late := false, false
	for _, line := range strings.Split(string(data), "\n") {
		values := map[string]string{}
		for _, field := range strings.Fields(line) {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				values[parts[0]] = parts[1]
			}
		}
		if values["path"] != "/v1/operations/begin" {
			continue
		}
		if values["requestId"] == replayedStartID {
			if values["dropped"] == "true" {
				droppedHash = values["requestSha256"]
			} else if droppedHash != "" && values["requestSha256"] == droppedHash {
				replayed = true
			}
		}
		if values["requestId"] == lateStartID {
			delay, _ := time.ParseDuration(values["responseDelay"])
			late = values["status"] == "200" && values["dropped"] == "false" && delay >= lateStartTTL*3/4
		}
	}
	if droppedHash == "" || !replayed {
		return errors.New("retained started request did not replay exactly")
	}
	if !late {
		return errors.New("late start acknowledgment was not observed beyond the safe dispatch window")
	}
	return nil
}

func verifyProviderEffectLog(path, effectID string, after time.Time) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	prefix := "provider-completed " + effectID + " at="
	count := 0
	var observed time.Time
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		count++
		observed, err = time.Parse(time.RFC3339Nano, strings.TrimPrefix(line, prefix))
		if err != nil {
			return time.Time{}, err
		}
	}
	if count != 1 {
		return time.Time{}, fmt.Errorf("provider effect %s completed %d times, want exactly 1", effectID, count)
	}
	if !observed.After(after) {
		return time.Time{}, fmt.Errorf("provider effect completed at %s before terminal receipt release at %s", observed, after)
	}
	return observed, nil
}

func verifyClockBoundEvidence(logPath, generatedDeadlineID, expiredID, lateStartID string, lateStartTTL time.Duration) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	var delayedSample, delayedObservedAt, generatedDeadline time.Time
	lateStartObserved := false
	for _, line := range strings.Split(string(data), "\n") {
		values := map[string]string{}
		for _, field := range strings.Fields(line) {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				values[parts[0]] = parts[1]
			}
		}
		if values["requestId"] == expiredID {
			return errors.New("expired short-window request reached the authority")
		}
		if values["path"] == "/.well-known/worklease" && values["responseDelay"] != "" && values["responseDelay"] != "0s" {
			delayedSample, _ = time.Parse(time.RFC3339Nano, values["authorityTime"])
			delayedObservedAt, _ = time.Parse(time.RFC3339Nano, values["at"])
		}
		if values["requestId"] == generatedDeadlineID {
			generatedDeadline, _ = time.Parse(time.RFC3339Nano, values["requestNotAfter"])
		}
		if values["requestId"] == lateStartID {
			delay, _ := time.ParseDuration(values["responseDelay"])
			if delay >= lateStartTTL*3/4 {
				lateStartObserved = true
			}
		}
	}
	if delayedSample.IsZero() || delayedObservedAt.IsZero() || generatedDeadline.IsZero() {
		return errors.New("delayed authority sample or generated request deadline was not observed")
	}
	if delayedObservedAt.Sub(delayedSample) < 30*time.Minute {
		return errors.New("authority-time sample did not include measurable client clock skew")
	}
	window := generatedDeadline.Sub(delayedSample)
	if window < 24*time.Hour-100*time.Millisecond || window > 24*time.Hour+500*time.Millisecond {
		return fmt.Errorf("request deadline did not use the delayed sample lower bound: window=%s", window)
	}
	if !lateStartObserved {
		return errors.New("late start response did not cross the stop-new-work threshold")
	}
	return nil
}

func (h *harness) switchProfileEndpoint(c client, endpoint string) error {
	if _, err := h.cli(c, "profile", "remove", "team"); err != nil {
		return err
	}
	_, err := h.cli(c, "profile", "add", "team", "--endpoint", endpoint, "--authority-id", h.report.Authority)
	return err
}

func (h *harness) provisionRaceClient(label string) (client, string, error) {
	root := filepath.Join(h.root, label)
	c := client{name: label, home: filepath.Join(root, "home"), config: filepath.Join(root, "config"), checkout: filepath.Join(root, "checkout")}
	if h.realHost {
		root = filepath.Join(h.remoteRoot, label)
		c = client{name: label, home: filepath.Join(root, "home"), config: filepath.Join(root, "config"), checkout: filepath.Join(root, "checkout"), remote: true}
		if _, err := runSSH(h.remoteHost, "mkdir", "-p", c.home, c.config, c.checkout); err != nil {
			return client{}, "", err
		}
		if _, err := runSSH(h.remoteHost, "chmod", "700", root, c.home, c.config, c.checkout); err != nil {
			return client{}, "", err
		}
		if _, err := runSSH(h.remoteHost, "git", "init", "--quiet", c.checkout); err != nil {
			return client{}, "", err
		}
	} else {
		if err := os.MkdirAll(c.checkout, 0o700); err != nil {
			return client{}, "", err
		}
		if output, err := exec.Command("git", "init", "--quiet", c.checkout).CombinedOutput(); err != nil {
			return client{}, "", fmt.Errorf("git init %s: %w: %s", label, err, output)
		}
		c.env = append(os.Environ(), "WORKLEASE_HOME="+c.home, "XDG_CONFIG_HOME="+c.config, "SSL_CERT_FILE="+h.cert, "WORKLEASE_AGENT_ID="+label, "WORKLEASE_SESSION_ID="+label+"-session")
	}
	if _, err := h.cli(c, "profile", "add", "team", "--endpoint", h.endpoint, "--authority-id", h.report.Authority); err != nil {
		return client{}, "", err
	}
	invite := filepath.Join(h.root, "secrets", label+".invite")
	if _, err := h.cli(h.clients[0], "--profile", "team", "invite", "issue", "--role", "write", "--invite-file", invite, "--label", label); err != nil {
		return client{}, "", err
	}
	clientInvite := invite
	if h.realHost {
		clientInvite = filepath.Join(root, label+".invite")
		if err := runSCP(h.remoteHost, invite, clientInvite); err != nil {
			return client{}, "", err
		}
	}
	if _, err := h.cli(c, "enroll", "--profile", "team", "--invite-file", clientInvite, "--label", label); err != nil {
		return client{}, "", err
	}
	installations, err := h.cli(h.clients[0], "--profile", "team", "installation", "list", "--include-revoked")
	if err != nil {
		return client{}, "", err
	}
	installationID, err := installationIDByLabel(installations, label)
	return c, installationID, err
}

func claimsContain(result map[string]any, claimID string) bool {
	claims, _ := result["claims"].([]any)
	for _, raw := range claims {
		claim, _ := raw.(map[string]any)
		if claim["claimId"] == claimID {
			return true
		}
	}
	return false
}

func (h *harness) serverConfig(prefixes []string, maxTTL, maxHold string) string {
	listen, cert, key := strings.TrimPrefix(h.endpoint, "https://"), h.cert, filepath.Join(h.root, "secrets", "tls.key")
	if h.realHost {
		listen = fmt.Sprintf("0.0.0.0:%d", h.remotePort)
		cert, key = h.remoteCert, h.remoteKey
	}
	var admitted strings.Builder
	for _, prefix := range prefixes {
		fmt.Fprintf(&admitted, "  - %q\n", prefix)
	}
	return fmt.Sprintf("home: %s\nlisten: %s\ntlsCert: %s\ntlsKey: %s\nadmittedPrefixes:\n%smaxTTL: %s\nmaxHold: %s\nshutdownTimeout: 2s\nhealthRate: 100\nmetadataRate: 100\nenrollmentRate: 100\n", h.authorityHome, listen, cert, key, admitted.String(), maxTTL, maxHold)
}

func (h *harness) writeServerConfig(prefixes []string, maxTTL, maxHold string) error {
	config := []byte(h.serverConfig(prefixes, maxTTL, maxHold))
	if !h.realHost {
		return os.WriteFile(h.configPath, config, 0o600)
	}
	local := filepath.Join(h.root, "secrets", "server-next.yaml")
	if err := os.WriteFile(local, config, 0o600); err != nil {
		return err
	}
	return runSCP(h.remoteHost, local, h.configPath)
}

func (h *harness) requireAuthorityFailure(args []string, expected ...string) error {
	h.logCommand("authority@"+h.remoteHost+" expected-failure", append([]string{"worklease"}, args...))
	var output []byte
	var err error
	if h.realHost {
		output, err = exec.Command("ssh", append([]string{h.remoteHost, h.remoteBinary}, args...)...).CombinedOutput()
	} else {
		output, err = exec.Command(h.binary, args...).CombinedOutput()
	}
	if err == nil {
		return fmt.Errorf("authority command unexpectedly succeeded: %s", strings.Join(args, " "))
	}
	var result map[string]any
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil || result["ok"] != false {
		return fmt.Errorf("invalid authority failure envelope: %v: %s", jsonErr, output)
	}
	return requireReason(result, expected...)
}

func (h *harness) requireServerStartFailure(expected ...string) error {
	return h.requireAuthorityFailure([]string{"--json", "serve", "--server-config", h.configPath}, expected...)
}

func (h *harness) group1(evidence string) error {
	a, b := h.clients[0], h.clients[1]
	sharedHandle := filepath.Join(a.home, "handles", "shared.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", sharedHandle, "--resource", "coordination:shared", "--ttl", "10m"); err != nil {
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
	reserved, err := h.cliFailure(b, "--profile", "team", "acquire", "--handle", filepath.Join(b.home, "handles", "reserved.json"), "--resource", "path:/tmp/remote-forbidden")
	if err != nil {
		return err
	}
	if err := requireReason(reserved, "resource-not-enrolled"); err != nil {
		return err
	}
	if err := h.writeServerConfig([]string{"coordination:", "alternate:"}, "5s", "1h"); err != nil {
		return err
	}
	beforeRestart, err := h.cliFailure(b, "--profile", "team", "acquire", "--handle", filepath.Join(b.home, "handles", "before-restart.json"), "--resource", "alternate:before-restart")
	if err != nil {
		return err
	}
	if err := requireReason(beforeRestart, "resource-not-enrolled"); err != nil {
		return err
	}
	h.stopServer()
	if err := h.writeServerConfig([]string{"path:"}, "5s", "1h"); err != nil {
		return err
	}
	if err := h.requireServerStartFailure("resource-not-enrolled", "invalid-argument"); err != nil {
		return err
	}
	if err := h.writeServerConfig([]string{"coordination:", "alternate:"}, "5s", "1h"); err != nil {
		return err
	}
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	alternateHandle := filepath.Join(b.home, "handles", "alternate.json")
	if _, err := h.cli(b, "--profile", "team", "acquire", "--handle", alternateHandle, "--resource", "alternate:after-restart", "--ttl", "5s"); err != nil {
		return err
	}
	tooLong, err := h.cliFailure(b, "--profile", "team", "acquire", "--handle", filepath.Join(b.home, "handles", "too-long.json"), "--resource", "coordination:too-long", "--ttl", "10s")
	if err != nil {
		return err
	}
	if err := requireReason(tooLong, "invalid-argument"); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "heartbeat", "--handle", sharedHandle, "--ttl", "10m"); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "exec", "--handle", sharedHandle, "--ttl", "10m", "--", h.self, "effect", filepath.Join(evidence, "admission-effect.log")); err != nil {
		return err
	}
	successorHandle := filepath.Join(a.home, "handles", "successor.json")
	if _, err := h.cli(a, "--profile", "team", "transfer", "--handle", sharedHandle, "--successor-handle", successorHandle, "--ttl", "10m", "--to-agent", "client-a-successor", "--to-session", "successor-session", "--to-work-key", "admission-transfer"); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "release", "--handle", successorHandle, "--reason", "admission limits observed"); err != nil {
		return err
	}
	latencyStart := time.Now()
	if _, err := h.cli(b, "--profile", "team", "list"); err != nil {
		return err
	}
	h.report.LatencyMillis = time.Since(latencyStart).Milliseconds()
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 1, Observation: "distinct roots contend through CLI and MCP; reserved resources and reserved server prefixes fail closed; policy changes apply only after restart; persisted claim admission limits govern heartbeat, guarded execution, and transfer", Commands: []string{"cross-host acquire/contention/separate scope", "MCP acquire/release", "reserved resource rejection", "rewrite config before restart", "reserved-prefix server startup rejection", "restart with lower limits", "acquire ceiling rejection", "existing claim heartbeat/exec/transfer"}, Evidence: []string{filepath.Join(evidence, "authority.log"), filepath.Join(evidence, "admission-effect.log")}, Passed: true})
	return nil
}

func (h *harness) group2(evidence string) error {
	if err := h.startFaultProxy(evidence); err != nil {
		return err
	}
	a := h.clients[0]
	if err := h.switchProfileEndpoint(a, h.faultEndpoint); err != nil {
		return err
	}
	defer func() {
		_ = h.switchProfileEndpoint(a, h.endpoint)
		h.stopFaultProxy()
	}()

	beginHandle := filepath.Join(a.home, "handles", "lost-begin.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", beginHandle, "--resource", "coordination:lost-begin", "--ttl", "5s"); err != nil {
		return err
	}
	beginEffect := filepath.Join(evidence, "lost-begin-effect.log")
	beginOperation := strings.Repeat("1", 32)
	if err := h.armFault("/v1/operations/begin"); err != nil {
		return err
	}
	if _, err := h.cliFailure(a, "--profile", "team", "exec", "--handle", beginHandle, "--operation-id", beginOperation, "--ttl", "5s", "--", h.self, "effect", beginEffect); err != nil {
		return err
	}
	if _, err := os.Stat(beginEffect); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("lost begin dispatched before replay: %v", err)
	}
	replayedBegin, err := h.cliFailure(a, "--profile", "team", "exec", "--handle", beginHandle, "--operation-id", beginOperation, "--ttl", "5s", "--", h.self, "effect", beginEffect)
	if err != nil {
		return err
	}
	if err := requireReason(replayedBegin, "unknown-outcome"); err != nil {
		return err
	}
	if _, err := os.Stat(beginEffect); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replayed unknown begin dispatched an effect: %v", err)
	}
	h.report.EffectDispatchCounts[filepath.Base(beginEffect)] = 0

	explicitHandle := filepath.Join(a.home, "handles", "explicit-lost-begin.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", explicitHandle, "--resource", "coordination:explicit-lost-begin", "--ttl", "5s"); err != nil {
		return err
	}
	explicitGrant, err := handle.Read(explicitHandle)
	if err != nil {
		return err
	}
	explicitTokenName := ".explicit-claim.token"
	explicitTokenPath := filepath.Join(a.checkout, explicitTokenName)
	if err := os.WriteFile(explicitTokenPath, []byte(explicitGrant.Token+"\n"), 0o600); err != nil {
		return err
	}
	explicitOperation := strings.Repeat("8", 32)
	explicitEffect := filepath.Join(evidence, "explicit-lost-begin-effect.log")
	explicitDeadline := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)
	explicitArgs := []string{"--profile", "team", "exec", "--claim-id", explicitGrant.ClaimID, "--token-file", explicitTokenName, "--revision", strconv.FormatInt(explicitGrant.Revision, 10), "--operation-id", explicitOperation, "--request-not-after", explicitDeadline, "--ttl", "5s", "--", h.self, "effect", explicitEffect}
	if err := h.armFault("/v1/operations/begin"); err != nil {
		return err
	}
	explicitLost, err := h.cliFailure(a, explicitArgs...)
	if err != nil {
		return err
	}
	if err := requireReason(explicitLost, "unknown-outcome"); err != nil {
		return fmt.Errorf("explicit lost begin: %w", err)
	}
	explicitPending, err := authority.NewFilePendingStore(filepath.Join(a.home, "pending", "team")).Load(explicitOperation)
	if err != nil {
		return err
	}
	canonicalExplicitToken, err := filepath.EvalSymlinks(explicitTokenPath)
	if err != nil {
		return err
	}
	if explicitPending.ClaimCredentialRef != canonicalExplicitToken || !filepath.IsAbs(explicitPending.ClaimCredentialRef) {
		return fmt.Errorf("explicit retained request did not persist the absolute claim token path: got=%q want=%q", explicitPending.ClaimCredentialRef, canonicalExplicitToken)
	}
	replayCommand := exec.Command(h.self, "replay-pending", a.config, a.home, "team", explicitOperation)
	replayCommand.Env, replayCommand.Dir = a.env, a.checkout
	replayOutput, replayErr := replayCommand.CombinedOutput()
	if replayErr == nil || !bytes.Contains(replayOutput, []byte("unknown-outcome")) {
		return fmt.Errorf("explicit retained replay did not authenticate from the stored claim token reference: err=%v output=%s", replayErr, replayOutput)
	}
	if _, err := os.Stat(explicitEffect); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("explicit retained begin replay dispatched an effect: %v", err)
	}
	h.report.EffectDispatchCounts[filepath.Base(explicitEffect)] = 0
	fdArgs := []string{"--json", "--profile", "team", "exec", "--claim-id", explicitGrant.ClaimID, "--token-fd", "0", "--revision", strconv.FormatInt(explicitGrant.Revision, 10), "--operation-id", strings.Repeat("9", 32), "--request-not-after", explicitDeadline, "--ttl", "5s", "--", h.self, "effect", explicitEffect}
	h.logCommand(a.name+" expected-failure", append([]string{"worklease"}, fdArgs...))
	fdCommand := exec.Command(h.binary, fdArgs...)
	fdCommand.Env, fdCommand.Dir, fdCommand.Stdin = a.env, a.checkout, strings.NewReader(explicitGrant.Token+"\n")
	fdOutput, fdErr := fdCommand.CombinedOutput()
	if fdErr == nil {
		return errors.New("remote explicit guarded effect accepted an FD-only claim credential")
	}
	var fdFailure map[string]any
	if err := json.Unmarshal(fdOutput, &fdFailure); err != nil {
		return fmt.Errorf("decode FD-only guarded-effect failure: %w: %s", err, fdOutput)
	}
	if err := requireReason(fdFailure, "credential-unsafe"); err != nil {
		return fmt.Errorf("FD-only guarded effect: %w", err)
	}

	renewHandle := filepath.Join(a.home, "handles", "lost-renew.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", renewHandle, "--resource", "coordination:lost-renew", "--ttl", "2s"); err != nil {
		return err
	}
	renewEffect := filepath.Join(evidence, "lost-renew-effect.log")
	if err := h.armFault("/v1/operations/renew"); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "exec", "--handle", renewHandle, "--operation-id", strings.Repeat("2", 32), "--ttl", "2s", "--max-duration", "6s", "--", h.self, "delayed-effect", "3s", renewEffect); err != nil {
		return err
	}

	completeHandle := filepath.Join(a.home, "handles", "lost-complete.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", completeHandle, "--resource", "coordination:lost-complete", "--ttl", "5s"); err != nil {
		return err
	}
	completeEffect := filepath.Join(evidence, "lost-complete-effect.log")
	completeOperation := strings.Repeat("3", 32)
	if err := h.armFault("/v1/operations/complete"); err != nil {
		return err
	}
	if _, err := h.cliFailure(a, "--profile", "team", "exec", "--handle", completeHandle, "--operation-id", completeOperation, "--ttl", "5s", "--", h.self, "effect", completeEffect); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "exec", "--handle", completeHandle, "--operation-id", completeOperation, "--ttl", "5s", "--", h.self, "effect", completeEffect); err != nil {
		return err
	}
	for _, effect := range []string{renewEffect, completeEffect} {
		data, err := os.ReadFile(effect)
		if err != nil || string(data) != "dispatch\n" {
			return fmt.Errorf("faulted effect dispatch count %s: content=%q err=%v", filepath.Base(effect), data, err)
		}
		h.report.EffectDispatchCounts[filepath.Base(effect)] = strings.Count(string(data), "dispatch\n")
	}
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err := verifyFreshReplayEnvelope(filepath.Join(evidence, "fault-proxy.log"), "/v1/operations/complete", h.report.Authority); err != nil {
		return err
	}

	clockHandle := filepath.Join(a.home, "handles", "clock-bounds.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", clockHandle, "--resource", "coordination:clock-bounds", "--ttl", "5s"); err != nil {
		return err
	}
	generatedDeadlineID := strings.Repeat("4", 32)
	if err := h.delayResponseWithAuthorityOffset("/.well-known/worklease", 1200*time.Millisecond, -time.Hour); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "heartbeat", "--handle", clockHandle, "--operation-id", generatedDeadlineID, "--ttl", "5s"); err != nil {
		return err
	}
	expiredID := strings.Repeat("5", 32)
	expired, err := h.cliFailure(a, "--profile", "team", "heartbeat", "--handle", clockHandle, "--operation-id", expiredID, "--request-not-after", time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), "--ttl", "5s")
	if err != nil {
		return err
	}
	if err := requireReason(expired, "replay-expired"); err != nil {
		return err
	}

	lateHandle := filepath.Join(a.home, "handles", "late-start.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", lateHandle, "--resource", "coordination:late-start", "--ttl", "5s"); err != nil {
		return err
	}
	lateEffect := filepath.Join(evidence, "late-start-effect.log")
	lateStartID, lateStartTTL := strings.Repeat("6", 32), 2*time.Second
	if err := h.delayResponse("/v1/operations/begin", 1700*time.Millisecond); err != nil {
		return err
	}
	late, err := h.cliFailure(a, "--profile", "team", "exec", "--handle", lateHandle, "--operation-id", lateStartID, "--ttl", lateStartTTL.String(), "--", h.self, "effect", lateEffect)
	if err != nil {
		return err
	}
	if err := requireReason(late, "invalid-argument", "claim-expired", "ownership-lost", "unknown-outcome"); err != nil {
		return err
	}
	if _, err := os.Stat(lateEffect); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("late start response dispatched an effect: %v", err)
	}
	h.report.EffectDispatchCounts[filepath.Base(lateEffect)] = 0
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err := verifyClockBoundEvidence(filepath.Join(evidence, "fault-proxy.log"), generatedDeadlineID, expiredID, lateStartID, lateStartTTL); err != nil {
		return err
	}
	if err := verifyLateAcknowledgmentEvidence(filepath.Join(evidence, "fault-proxy.log"), beginOperation, lateStartID, lateStartTTL); err != nil {
		return err
	}
	lateAcknowledgmentEvidence := filepath.Join(evidence, "late-acknowledgment.txt")
	lateError, _ := late["error"].(map[string]any)
	lateReason, _ := lateError["reason"].(string)
	if err := os.WriteFile(lateAcknowledgmentEvidence, []byte("retained start replay dispatches=0\nlate start HTTP status=200 dropped=false\nlate start client result="+lateReason+"\nlate start acknowledgment dispatches=0\n"), 0o600); err != nil {
		return err
	}
	clockEvidence := filepath.Join(evidence, "clock-bounds.txt")
	if err := os.WriteFile(clockEvidence, []byte("asymmetric metadata response delay=1.2s\ninjected authority/client wall-clock skew=-1h\nrequest window=authority lower bound + 24h\nexpired short window dispatches=0\nlate successful start response dispatches=0\n"), 0o600); err != nil {
		return err
	}

	const preDispatchPath = "/v1/admin/gc"
	if err := h.holdFault(preDispatchPath); err != nil {
		return err
	}
	pendingRoot := filepath.Join(a.home, "pending", "team")
	if err := withBlockedPendingRoot(pendingRoot, func() error {
		failed, callErr := h.cliFailure(a, "--profile", "team", "gc", "--apply")
		if callErr != nil {
			return callErr
		}
		return requireReason(failed, "storage-failure")
	}); err != nil {
		return err
	}
	if err := h.requireFaultStillArmed(preDispatchPath); err != nil {
		return err
	}
	if err := h.setFaultControl(""); err != nil {
		return err
	}
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err := verifyNoFaultDispatch(filepath.Join(evidence, "fault-proxy.log"), preDispatchPath); err != nil {
		return err
	}
	preDispatchEvidence := filepath.Join(evidence, "pre-dispatch-persistence.txt")
	if err := os.WriteFile(preDispatchEvidence, []byte("pending root replaced by regular file\nclient result=storage-failure\nproxy pre-forward gate remained armed\nauthority dispatch count=0\n"), 0o600); err != nil {
		return err
	}

	providerHandle := filepath.Join(a.home, "handles", "asynchronous-provider.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", providerHandle, "--resource", "coordination:asynchronous-provider", "--ttl", "5s"); err != nil {
		return err
	}
	providerSubmitted := filepath.Join(evidence, "provider-submitted.txt")
	providerRelease := filepath.Join(evidence, "provider-release.txt")
	providerCompleted := filepath.Join(evidence, "provider-completed.log")
	providerEffectID := strings.Repeat("7", 32)
	providerWorker := exec.Command(h.self, "provider-effect-worker", providerSubmitted, providerRelease, providerCompleted, providerEffectID)
	var providerWorkerError bytes.Buffer
	providerWorker.Stderr = &providerWorkerError
	if err := providerWorker.Start(); err != nil {
		return err
	}
	providerWorkerDone := make(chan error, 1)
	go func() { providerWorkerDone <- providerWorker.Wait() }()
	defer func() { _ = providerWorker.Process.Kill() }()
	if _, err := h.cli(a, "--profile", "team", "exec", "--handle", providerHandle, "--operation-id", providerEffectID, "--ttl", "5s", "--", h.self, "submit-provider-effect", providerSubmitted, providerEffectID); err != nil {
		return err
	}
	terminalReceiptAt := time.Now().UTC()
	providerReleaseTemp := providerRelease + ".tmp"
	if err := os.WriteFile(providerReleaseTemp, []byte(terminalReceiptAt.Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(providerReleaseTemp, providerRelease); err != nil {
		return err
	}
	select {
	case err := <-providerWorkerDone:
		if err != nil {
			return fmt.Errorf("asynchronous provider fixture failed: %w: %s", err, providerWorkerError.String())
		}
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for asynchronous provider fixture")
	}
	providerCompletedAt, err := verifyProviderEffectLog(providerCompleted, providerEffectID, terminalReceiptAt)
	if err != nil {
		return err
	}
	providerEvidence := filepath.Join(evidence, "asynchronous-provider-effect.txt")
	providerObservation := fmt.Sprintf("terminal-receipt-observed-at=%s\nprovider-effect-released-after-receipt-at=%s\nprovider-effect-completed-at=%s\nprovider effect remained unfenced after terminal completion\n", terminalReceiptAt.Format(time.RFC3339Nano), terminalReceiptAt.Format(time.RFC3339Nano), providerCompletedAt.Format(time.RFC3339Nano))
	if err := os.WriteFile(providerEvidence, []byte(providerObservation), 0o600); err != nil {
		return err
	}
	h.report.EffectDispatchCounts[filepath.Base(providerCompleted)] = 1

	if err := h.switchProfileEndpoint(a, h.endpoint); err != nil {
		return err
	}
	if err := h.raceOrdering(evidence); err != nil {
		return err
	}
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err := verifyFaultGates(filepath.Join(evidence, "fault-proxy.log"), "/v1/claims/acquire", 2); err != nil {
		return err
	}
	h.stopFaultProxy()

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
	faultEvidence := filepath.Join(evidence, "fault-proxy.log")
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 2, Observation: "a lost start response replays to the retained unknown start without dispatch; lost renewal and completion responses replay exactly with one guarded effect; completion replay preserves the historical result inside the current authority/restore identity and a newer authority-time envelope; an asymmetric response delay produces the lower-bound 24-hour request window, an expired short window sends nothing, and a start response delayed beyond the three-quarter-TTL stop-new-work boundary dispatches no effect; retained and late start acknowledgments never redispatch an effect; terminal completion does not fence an external asynchronous provider effect; a failed client pending-store write returns storage-failure before authority dispatch; held requests prove revocation and prefix withdrawal follow server serialization order; an authority partition creates no local fallback", Commands: []string{"arm one-shot begin response loss and replay retained unknown", "arm one-shot renewal response loss", "arm one-shot completion response loss and replay", "compare dropped and replayed completion response envelopes", "delay metadata response and inspect generated request deadline", "reject expired request window before dispatch", "delay successful begin response beyond three-quarter TTL", "run external asynchronous provider effect through terminal completion", "replace pending root with a regular file and attempt remote GC", "hold acquire before forwarding and serialize installation revocation first", "commit acquire before installation revocation", "hold acquire before forwarding and restart with its prefix withdrawn", "heartbeat a claim admitted before prefix withdrawal", "stop authority", "client-b acquire during partition", "restart authority"}, Evidence: []string{beginEffect, explicitEffect, renewEffect, completeEffect, lateEffect, faultEvidence, filepath.Join(evidence, "clock-bounds.txt"), lateAcknowledgmentEvidence, providerSubmitted, providerCompleted, providerEvidence, preDispatchEvidence, filepath.Join(evidence, "race-ordering.txt"), "lost-begin dispatch-count=0", "lost-renew and lost-complete dispatch-count=1", "late-start dispatch-count=0", "asynchronous provider completion dispatch-count=1 after terminal receipt", "pre-dispatch persistence authority dispatch-count=0", "completion replay result hash stable; authority identity stable; authority time advanced"}, Passed: true})
	return nil
}

func (h *harness) raceOrdering(evidence string) error {
	const acquirePath = "/v1/claims/acquire"
	admin := h.clients[0]
	if err := h.writeServerConfig([]string{"coordination:", "alternate:"}, "1h", "1h"); err != nil {
		return err
	}
	h.stopServer()
	if err := h.restartServer(evidence); err != nil {
		return err
	}

	revocationFirst, revocationFirstID, err := h.provisionRaceClient("race-revocation-first")
	if err != nil {
		return err
	}
	if err := h.switchProfileEndpoint(revocationFirst, h.faultEndpoint); err != nil {
		return err
	}
	if err := h.holdFault(acquirePath); err != nil {
		return err
	}
	revocationResult := make(chan cliFailureResult, 1)
	go func() {
		result, callErr := h.cliFailureTimeout(revocationFirst, 45*time.Second, "--profile", "team", "acquire", "--handle", filepath.Join(revocationFirst.home, "handles", "held.json"), "--resource", "coordination:revocation-first", "--ttl", "1m")
		revocationResult <- cliFailureResult{result: result, err: callErr}
	}()
	if err := h.waitFaultHeld(acquirePath); err != nil {
		return err
	}
	if _, err := h.cli(admin, "--profile", "team", "installation", "revoke", "--installation-id", revocationFirstID, "--reason", "acceptance revocation-first ordering"); err != nil {
		return err
	}
	if err := h.releaseFault(acquirePath); err != nil {
		return err
	}
	failed := <-revocationResult
	if failed.err != nil {
		return failed.err
	}
	if err := requireReason(failed.result, "installation-revoked"); err != nil {
		return err
	}

	commitFirst, commitFirstID, err := h.provisionRaceClient("race-commit-first")
	if err != nil {
		return err
	}
	commitHandle := filepath.Join(commitFirst.home, "handles", "committed.json")
	committed, err := h.cli(commitFirst, "--profile", "team", "acquire", "--handle", commitHandle, "--resource", "coordination:commit-first", "--ttl", "1m")
	if err != nil {
		return err
	}
	claimID, _ := committed["claimId"].(string)
	if claimID == "" {
		return errors.New("commit-first acquire returned no claim ID")
	}
	if _, err := h.cli(admin, "--profile", "team", "installation", "revoke", "--installation-id", commitFirstID, "--reason", "acceptance commit-first ordering"); err != nil {
		return err
	}
	claims, err := h.cli(admin, "--profile", "team", "list", "--full")
	if err != nil {
		return err
	}
	if !claimsContain(claims, claimID) {
		return errors.New("installation revocation removed the mutation committed first")
	}
	revokedHeartbeat, err := h.cliFailure(commitFirst, "--profile", "team", "heartbeat", "--handle", commitHandle, "--ttl", "1m")
	if err != nil {
		return err
	}
	if err := requireReason(revokedHeartbeat, "installation-revoked"); err != nil {
		return err
	}

	policyClient := h.clients[1]
	beforeHandle := filepath.Join(policyClient.home, "handles", "before-policy-withdrawal.json")
	if _, err := h.cli(policyClient, "--profile", "team", "acquire", "--handle", beforeHandle, "--resource", "alternate:commit-first", "--ttl", "1m"); err != nil {
		return err
	}
	if err := h.switchProfileEndpoint(policyClient, h.faultEndpoint); err != nil {
		return err
	}
	if err := h.holdFault(acquirePath); err != nil {
		return err
	}
	policyResult := make(chan cliFailureResult, 1)
	go func() {
		result, callErr := h.cliFailureTimeout(policyClient, 45*time.Second, "--profile", "team", "acquire", "--handle", filepath.Join(policyClient.home, "handles", "after-policy-withdrawal.json"), "--resource", "alternate:withdrawn-first", "--ttl", "1m")
		policyResult <- cliFailureResult{result: result, err: callErr}
	}()
	if err := h.waitFaultHeld(acquirePath); err != nil {
		return err
	}
	if err := h.writeServerConfig([]string{"coordination:"}, "1h", "1h"); err != nil {
		return err
	}
	h.stopServer()
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	if err := h.releaseFault(acquirePath); err != nil {
		return err
	}
	failed = <-policyResult
	if failed.err != nil {
		return failed.err
	}
	if err := requireReason(failed.result, "resource-not-enrolled"); err != nil {
		return err
	}
	if err := h.switchProfileEndpoint(policyClient, h.endpoint); err != nil {
		return err
	}
	if _, err := h.cli(policyClient, "--profile", "team", "heartbeat", "--handle", beforeHandle, "--ttl", "1m"); err != nil {
		return err
	}
	observation := "revocation-first held acquire=installation-revoked\ncommit-first claim retained after installation revocation=" + claimID + "\nwithdrawal-first held acquire=resource-not-enrolled\ncommit-first claim heartbeat after prefix withdrawal=success\n"
	return os.WriteFile(filepath.Join(evidence, "race-ordering.txt"), []byte(observation), 0o600)
}

func randomRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (h *harness) newLocalEnrollmentClient(label, endpoint string) (client, error) {
	root := filepath.Join(h.root, label)
	c := client{name: label, home: filepath.Join(root, "home"), config: filepath.Join(root, "config"), checkout: filepath.Join(root, "checkout")}
	if err := os.MkdirAll(c.checkout, 0o700); err != nil {
		return client{}, err
	}
	if output, err := exec.Command("git", "init", "--quiet", c.checkout).CombinedOutput(); err != nil {
		return client{}, fmt.Errorf("git init %s: %w: %s", label, err, output)
	}
	c.env = append(os.Environ(), "WORKLEASE_HOME="+c.home, "XDG_CONFIG_HOME="+c.config, "SSL_CERT_FILE="+h.cert, "WORKLEASE_AGENT_ID="+label, "WORKLEASE_SESSION_ID="+label+"-session")
	if _, err := h.cli(c, "profile", "add", "team", "--endpoint", endpoint, "--authority-id", h.report.Authority); err != nil {
		return client{}, err
	}
	return c, nil
}

func (h *harness) cliHiddenInvite(c client, invitePath string, args ...string) error {
	if c.remote {
		return errors.New("hidden invite acceptance client must run on the orchestrator host")
	}
	invite, err := os.ReadFile(invitePath)
	if err != nil {
		return err
	}
	h.logCommand(c.name, append([]string{"worklease", "--json"}, args...))
	cmd := exec.Command(h.binary, append([]string{"--json"}, args...)...)
	cmd.Env, cmd.Dir = c.env, c.checkout
	terminal, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	capture := &promptCapture{prompt: make(chan struct{})}
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(capture, terminal)
		close(copied)
	}()
	select {
	case <-capture.prompt:
		_, err = fmt.Fprintln(terminal, strings.TrimSpace(string(invite)))
	case <-time.After(5 * time.Second):
		err = errors.New("hidden invite prompt timed out")
	}
	waitErr := cmd.Wait()
	_ = terminal.Close()
	<-copied
	output := capture.Bytes()
	if bytes.Contains(output, bytes.TrimSpace(invite)) {
		return errors.New("hidden invite was echoed by the terminal prompt")
	}
	if err != nil {
		return fmt.Errorf("hidden invite enrollment: %w", err)
	}
	if waitErr != nil {
		return fmt.Errorf("hidden invite enrollment failed: %w", waitErr)
	}
	return nil
}

func (h *harness) cliFailureWithInviteFD(c client, invitePath string, args ...string) (map[string]any, error) {
	if c.remote {
		return nil, errors.New("descriptor acceptance client must run on the orchestrator host")
	}
	invite, err := os.Open(invitePath)
	if err != nil {
		return nil, err
	}
	defer invite.Close()
	h.logCommand(c.name+" expected-failure", append([]string{"worklease", "--json"}, args...))
	cmd := exec.Command(h.binary, append([]string{"--json"}, args...)...)
	cmd.Env, cmd.Dir, cmd.ExtraFiles = c.env, c.checkout, []*os.File{invite}
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

func (h *harness) replayPending(c client, requestID string, inviteFile ...string) error {
	if c.remote {
		return errors.New("pending replay helper requires an orchestrator-local client")
	}
	args := []string{"replay-pending", c.config, c.home, "team", requestID}
	args = append(args, inviteFile...)
	h.logCommand(c.name, append([]string{"harness-helper"}, args...))
	cmd := exec.Command(h.self, args...)
	cmd.Env = c.env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("replay pending %s: %w: %s", requestID, err, output)
	}
	return nil
}

func (h *harness) replayPendingFailure(c client, requestID string, inviteFile string) ([]byte, error) {
	if c.remote {
		return nil, errors.New("pending replay helper requires an orchestrator-local client")
	}
	args := []string{"replay-pending", c.config, c.home, "team", requestID, inviteFile}
	h.logCommand(c.name+" expected-failure", append([]string{"harness-helper"}, args...))
	cmd := exec.Command(h.self, args...)
	cmd.Env = c.env
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, errors.New("pending enrollment replay unexpectedly succeeded")
	}
	return output, nil
}

func singlePendingID(home, profile, kind string) (string, error) {
	records, err := authority.NewFilePendingStore(filepath.Join(home, "pending", profile)).List()
	if err != nil {
		return "", err
	}
	var id string
	for _, record := range records {
		if record.Kind != kind {
			continue
		}
		if id != "" {
			return "", fmt.Errorf("multiple pending %s requests", kind)
		}
		id = record.RequestID
	}
	if id == "" {
		return "", fmt.Errorf("pending %s request is missing", kind)
	}
	return id, nil
}

func unexpectedEnrollmentResponseError(status int, envelope map[string]any) error {
	errorFields, _ := envelope["error"].(map[string]any)
	reasonValue, _ := errorFields["reason"].(string)
	return fmt.Errorf("mismatched enrollment status=%d reason=%q", status, reasonValue)
}

func (h *harness) mismatchedEnrollment(inviteFile, label, evidencePath string) (string, []byte, error) {
	invite, err := os.ReadFile(inviteFile)
	if err != nil {
		return "", nil, err
	}
	requestID, err := randomRequestID()
	if err != nil {
		return "", nil, err
	}
	credential := make([]byte, 32)
	if _, err := rand.Read(credential); err != nil {
		return "", nil, err
	}
	credentialText := []byte(hex.EncodeToString(credential))
	body, _ := json.Marshal(map[string]any{"protocolVersion": "worklease-http/1", "authorityId": h.report.Authority, "expectedRestoreId": strings.Repeat("0", 32), "requestId": requestID, "requestNotAfter": time.Now().UTC().Add(time.Hour), "installationId": requestID, "label": label})
	pool := x509.NewCertPool()
	certificate, err := os.ReadFile(h.cert)
	if err != nil || !pool.AppendCertsFromPEM(certificate) {
		return "", nil, errors.New("cannot load acceptance TLS certificate")
	}
	request, _ := http.NewRequest(http.MethodPost, h.endpoint+"/v1/enroll", bytes.NewReader(body))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Worklease-Protocol-Version", "worklease-http/1")
	request.Header.Set("Authorization", "Invite "+strings.TrimSpace(string(invite)))
	request.Header.Set("Worklease-New-Installation-Authorization", "Bearer "+string(credentialText))
	response, err := (&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}).Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return "", nil, err
	}
	if response.StatusCode != http.StatusUnprocessableEntity {
		return "", nil, unexpectedEnrollmentResponseError(response.StatusCode, envelope)
	}
	if err := requireReason(envelope, "authority-restored"); err != nil {
		return "", nil, unexpectedEnrollmentResponseError(response.StatusCode, envelope)
	}
	recorded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", nil, err
	}
	if bytes.Contains(recorded, bytes.TrimSpace(invite)) || bytes.Contains(recorded, credentialText) {
		return "", nil, errors.New("mismatched enrollment response disclosed a bearer secret")
	}
	if err := os.WriteFile(evidencePath, append(recorded, '\n'), 0o600); err != nil {
		return "", nil, err
	}
	return requestID, credentialText, nil
}

func verifyExactFaultReplay(logPath, path, requestID string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	var hashes, drops, statuses, results []string
	for _, line := range strings.Split(string(data), "\n") {
		values := map[string]string{}
		for _, field := range strings.Fields(line) {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				values[parts[0]] = parts[1]
			}
		}
		if values["path"] == path && values["requestId"] == requestID {
			hashes = append(hashes, values["requestSha256"])
			drops = append(drops, values["dropped"])
			statuses = append(statuses, values["status"])
			results = append(results, values["historicalResultSha256"])
		}
	}
	if len(hashes) != 2 || hashes[0] == "" || hashes[0] != hashes[1] || drops[0] != "true" || drops[1] != "false" || statuses[0] != "200" || statuses[1] != "200" || results[0] == "" || results[0] != results[1] {
		return fmt.Errorf("%s replay evidence hashes=%v drops=%v statuses=%v results=%v", path, hashes, drops, statuses, results)
	}
	return nil
}

func verifyDroppedFaultCommit(logPath, path, requestID, requestSHA256 string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	matches := 0
	for _, line := range strings.Split(string(data), "\n") {
		values := map[string]string{}
		for _, field := range strings.Fields(line) {
			parts := strings.SplitN(field, "=", 2)
			if len(parts) == 2 {
				values[parts[0]] = parts[1]
			}
		}
		if values["path"] != path || values["requestId"] != requestID {
			continue
		}
		matches++
		if values["requestSha256"] != requestSHA256 || values["dropped"] != "true" || values["status"] != "200" || values["historicalResultSha256"] == "" {
			return fmt.Errorf("dropped fault did not commit request %s: %v", requestID, values)
		}
	}
	if matches != 1 {
		return fmt.Errorf("dropped fault request %s matched %d dispatches", requestID, matches)
	}
	return nil
}

func requireSecretValuesAbsent(paths []string, secrets ...[]byte) error {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if len(secret) > 0 && bytes.Contains(data, secret) {
				return fmt.Errorf("secret leaked into %s", path)
			}
		}
	}
	return nil
}

func requireSecretsAbsent(paths []string, secretFiles ...string) error {
	secrets := make([][]byte, 0, len(secretFiles))
	for _, path := range secretFiles {
		secret, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		secrets = append(secrets, bytes.TrimSpace(secret))
	}
	return requireSecretValuesAbsent(paths, secrets...)
}

func (h *harness) group3BootstrapCrash(evidence string) error {
	home := filepath.Join(h.root, "bootstrap-crash-authority")
	invite := filepath.Join(h.root, "secrets", ".bootstrap-crash.invite")
	binary, helper, config := h.binary, h.self, h.configPath
	if h.realHost {
		home = filepath.Join(h.remoteRoot, "bootstrap-crash-authority")
		invite = filepath.Join(h.remoteRoot, ".bootstrap-crash.invite")
		binary, helper, config = h.remoteBinary, h.remoteHelper, h.remoteConfig
	}
	args := []string{"--json", "hosted", "init", "--home", home, "--server-config", config, "--bootstrap-invite-file", invite}
	h.logCommand("bootstrap-crash-boundary", append([]string{"env", "WORKLEASE_ACCEPTANCE_CRASH_BEFORE_HOSTED_READY=1", "worklease"}, args...))
	crashCtx, cancelCrash := context.WithTimeout(context.Background(), 30*time.Second)
	var crashCommand *exec.Cmd
	if h.realHost {
		crashCommand = exec.CommandContext(crashCtx, "ssh", append([]string{h.remoteHost, "env", "WORKLEASE_ACCEPTANCE_CRASH_BEFORE_HOSTED_READY=1", binary}, args...)...)
	} else {
		crashCommand = exec.CommandContext(crashCtx, binary, args...)
		crashCommand.Env = append(os.Environ(), "WORKLEASE_ACCEPTANCE_CRASH_BEFORE_HOSTED_READY=1")
	}
	crashOutput, crashErr := crashCommand.CombinedOutput()
	cancelCrash()
	var exitErr *exec.ExitError
	if !errors.As(crashErr, &exitErr) || exitErr.ExitCode() != 86 {
		return fmt.Errorf("bootstrap crash exit=%v output-bytes=%d", crashErr, len(crashOutput))
	}
	crashOutputPath := filepath.Join(evidence, "bootstrap-crash-output.txt")
	stateOutput, err := h.bootstrapState(helper, home, invite)
	if err != nil {
		return err
	}
	var before bootstrapState
	if err := json.Unmarshal(stateOutput, &before); err != nil {
		return err
	}
	if before.ReadyMarker || !before.BootstrapReady || before.ActiveInvites != 1 || before.RevokedInvites != 0 || before.SecretMode != "0600" {
		return fmt.Errorf("unexpected bootstrap state after crash: %+v", before)
	}
	secretCopy := invite
	if h.realHost {
		secretCopy = filepath.Join(h.root, "secrets", ".bootstrap-crash.remote.invite")
		if err := runSCP(h.remoteHost, h.remoteHost+":"+invite, secretCopy); err != nil {
			return err
		}
	}
	secret, err := os.ReadFile(secretCopy)
	if err != nil {
		return err
	}
	secret = bytes.TrimSpace(secret)
	if len(secret) == 0 || bytes.Contains(crashOutput, secret) {
		return errors.New("bootstrap crash output disclosed the staged secret")
	}
	if err := os.WriteFile(crashOutputPath, crashOutput, 0o600); err != nil {
		return err
	}
	h.logCommand("bootstrap-recovery", append([]string{"worklease"}, args...))
	recoveryCtx, cancelRecovery := context.WithTimeout(context.Background(), 30*time.Second)
	var recoveryCommand *exec.Cmd
	if h.realHost {
		recoveryCommand = exec.CommandContext(recoveryCtx, "ssh", append([]string{h.remoteHost, binary}, args...)...)
	} else {
		recoveryCommand = exec.CommandContext(recoveryCtx, binary, args...)
	}
	recoveryOutput, err := recoveryCommand.CombinedOutput()
	cancelRecovery()
	if bytes.Contains(recoveryOutput, secret) {
		return errors.New("bootstrap recovery output disclosed the staged secret")
	}
	if err != nil {
		return fmt.Errorf("bootstrap recovery: %w; output-bytes=%d", err, len(recoveryOutput))
	}
	recoveryOutputPath := filepath.Join(evidence, "bootstrap-recovery-output.json")
	if err := os.WriteFile(recoveryOutputPath, recoveryOutput, 0o600); err != nil {
		return err
	}
	stateOutput, err = h.bootstrapState(helper, home, invite)
	if err != nil {
		return err
	}
	var after bootstrapState
	if err := json.Unmarshal(stateOutput, &after); err != nil {
		return err
	}
	if !after.ReadyMarker || !after.BootstrapReady || after.ActiveInvites != 1 || after.RevokedInvites != 0 || after.SecretMode != "0600" {
		return fmt.Errorf("unexpected bootstrap state after recovery: %+v", after)
	}
	stateEvidence := filepath.Join(evidence, "bootstrap-crash-ordering.json")
	ordered, err := json.MarshalIndent(map[string]any{"crashExitCode": 86, "afterCrash": before, "afterRecovery": after}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(stateEvidence, append(ordered, '\n'), 0o600); err != nil {
		return err
	}
	return requireSecretsAbsent([]string{h.commandLog, crashOutputPath, recoveryOutputPath, stateEvidence}, secretCopy)
}

func (h *harness) bootstrapState(helper, home, invite string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var command *exec.Cmd
	if h.realHost {
		command = exec.CommandContext(ctx, "ssh", h.remoteHost, helper, "bootstrap-state", home, invite)
	} else {
		command = exec.CommandContext(ctx, helper, "bootstrap-state", home, invite)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read bootstrap state: %w; output-bytes=%d", err, len(output))
	}
	return output, nil
}

func (h *harness) group3EnrollmentFaults(evidence string) error {
	if err := h.startFaultProxy(evidence); err != nil {
		return err
	}
	admin := h.clients[0]
	issueID, err := randomRequestID()
	if err != nil {
		return err
	}
	issueInvite := filepath.Join(h.root, "secrets", ".lost-issue.invite")
	if err := h.switchProfileEndpoint(admin, h.faultEndpoint); err != nil {
		return err
	}
	if err := h.armFault("/v1/admin/invites/issue"); err != nil {
		return err
	}
	lostIssue, err := h.cliFailure(admin, "--profile", "team", "invite", "issue", "--operation-id", issueID, "--role", "write", "--invite-file", issueInvite, "--label", "lost-issue")
	if err != nil {
		return err
	}
	if err := requireReason(lostIssue, "unknown-outcome"); err != nil {
		return err
	}
	if err := h.replayPending(admin, issueID); err != nil {
		return err
	}
	if err := h.switchProfileEndpoint(admin, h.endpoint); err != nil {
		return err
	}

	redeemer, err := h.newLocalEnrollmentClient("enrollment-replay", h.faultEndpoint)
	if err != nil {
		return err
	}
	if err := h.armFault("/v1/enroll"); err != nil {
		return err
	}
	lostEnrollment, err := h.cliFailureWithInviteFD(redeemer, issueInvite, "enroll", "--profile", "team", "--invite-fd", "3", "--label", "enrollment-replay")
	if err != nil {
		return err
	}
	if err := requireReason(lostEnrollment, "unknown-outcome"); err != nil {
		return err
	}
	enrollmentID, err := singlePendingID(redeemer.home, "team", "enroll")
	if err != nil {
		return err
	}
	if err := h.replayPending(redeemer, enrollmentID, issueInvite); err != nil {
		return err
	}
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	faultLog := filepath.Join(evidence, "fault-proxy.log")
	if err := verifyExactFaultReplay(faultLog, "/v1/admin/invites/issue", issueID); err != nil {
		return err
	}
	if err := verifyExactFaultReplay(faultLog, "/v1/enroll", enrollmentID); err != nil {
		return err
	}
	immutableInvite := filepath.Join(h.root, "secrets", ".immutable-incarnation.invite")
	if _, err := h.cli(admin, "--profile", "team", "invite", "issue", "--role", "write", "--invite-file", immutableInvite, "--label", "immutable-incarnation"); err != nil {
		return err
	}
	immutableClient, err := h.newLocalEnrollmentClient("immutable-incarnation", h.faultEndpoint)
	if err != nil {
		return err
	}
	if err := h.armFault("/v1/enroll"); err != nil {
		return err
	}
	lostImmutable, err := h.cliFailure(immutableClient, "enroll", "--profile", "team", "--invite-file", immutableInvite, "--label", "immutable-incarnation")
	if err != nil {
		return err
	}
	if err := requireReason(lostImmutable, "unknown-outcome"); err != nil {
		return err
	}
	immutableID, err := singlePendingID(immutableClient.home, "team", "enroll")
	if err != nil {
		return err
	}
	immutablePending, err := authority.NewFilePendingStore(filepath.Join(immutableClient.home, "pending", "team")).Load(immutableID)
	if err != nil {
		return err
	}
	var immutableBody struct {
		ExpectedRestoreID string `json:"expectedRestoreId"`
	}
	if err := json.Unmarshal(immutablePending.Request, &immutableBody); err != nil || immutableBody.ExpectedRestoreID != immutablePending.ExpectedRestoreID {
		return fmt.Errorf("pending enrollment incarnation mismatch body=%q record=%q err=%v", immutableBody.ExpectedRestoreID, immutablePending.ExpectedRestoreID, err)
	}
	h.immutableEnrollmentClient = immutableClient
	h.zeroPendingClient = redeemer
	h.immutableEnrollmentID = immutableID
	h.immutableEnrollmentInvite = immutableInvite
	h.immutableEnrollmentBefore = immutablePending
	immutableEvidence, _ := json.MarshalIndent(map[string]string{"requestId": immutableID, "authorityId": immutablePending.AuthorityID, "expectedRestoreId": immutablePending.ExpectedRestoreID, "requestSha256": immutablePending.RequestSHA256}, "", "  ")
	if err := os.WriteFile(filepath.Join(evidence, "immutable-enrollment-before-restore.json"), append(immutableEvidence, '\n'), 0o600); err != nil {
		return err
	}

	noBurnInvite := filepath.Join(h.root, "secrets", ".no-burn.invite")
	if _, err := h.cli(admin, "--profile", "team", "invite", "issue", "--role", "read", "--invite-file", noBurnInvite, "--label", "no-burn"); err != nil {
		return err
	}
	beforeInventory, err := h.cli(admin, "--profile", "team", "installation", "list", "--include-revoked")
	if err != nil {
		return err
	}
	beforeInventoryData, err := json.Marshal(beforeInventory)
	if err != nil {
		return err
	}
	mismatchEvidence := filepath.Join(evidence, "enrollment-incarnation-mismatch.json")
	mismatchedInstallationID, mismatchCredential, err := h.mismatchedEnrollment(noBurnInvite, "mismatched-incarnation", mismatchEvidence)
	if err != nil {
		return err
	}
	inventory, err := h.cli(admin, "--profile", "team", "installation", "list", "--include-revoked")
	if err != nil {
		return err
	}
	afterInventoryData, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	if !bytes.Equal(beforeInventoryData, afterInventoryData) || installationLabelExists(inventory, "mismatched-incarnation") || installationIDExists(inventory, mismatchedInstallationID) {
		return errors.New("mismatched enrollment changed the installation inventory")
	}
	inventoryEvidence := filepath.Join(evidence, "post-mismatch-installations.json")
	inventoryData, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(inventoryEvidence, append(inventoryData, '\n'), 0o600); err != nil {
		return err
	}
	noBurnClient, err := h.newLocalEnrollmentClient("no-burn", h.endpoint)
	if err != nil {
		return err
	}
	if _, err := h.cli(noBurnClient, "enroll", "--profile", "team", "--invite-file", noBurnInvite, "--label", "no-burn"); err != nil {
		return fmt.Errorf("invite was burned by mismatched enrollment: %w", err)
	}
	hiddenInvite := filepath.Join(h.root, "secrets", ".hidden-prompt.invite")
	if _, err := h.cli(admin, "--profile", "team", "invite", "issue", "--role", "read", "--invite-file", hiddenInvite, "--label", "hidden-prompt"); err != nil {
		return err
	}
	hiddenClient, err := h.newLocalEnrollmentClient("hidden-prompt", h.endpoint)
	if err != nil {
		return err
	}
	if err := h.cliHiddenInvite(hiddenClient, hiddenInvite, "enroll", "--profile", "team", "--label", "hidden-prompt"); err != nil {
		return err
	}
	hiddenInventory, err := h.cli(admin, "--profile", "team", "installation", "list", "--include-revoked")
	if err != nil {
		return err
	}
	if !installationLabelExists(hiddenInventory, "hidden-prompt") {
		return errors.New("hidden invite enrollment did not create its installation")
	}
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err := verifyDroppedFaultCommit(faultLog, "/v1/enroll", immutableID, immutablePending.RequestSHA256); err != nil {
		return err
	}
	scannedPaths := []string{h.commandLog, filepath.Join(evidence, "authority.log"), faultLog, mismatchEvidence, inventoryEvidence}
	if err := requireSecretsAbsent(scannedPaths, issueInvite, immutableInvite, noBurnInvite, hiddenInvite); err != nil {
		return err
	}
	if err := requireSecretValuesAbsent(scannedPaths, mismatchCredential); err != nil {
		return err
	}
	observation := "lost invite issuance replayed exact request=" + issueID + "\nlost descriptor enrollment replayed exact retained request=" + enrollmentID + "\nretained enrollment for restore-incarnation check=" + immutableID + "\nwrong restore incarnation did not burn invite or insert installation\nhidden prompt, file, and descriptor invite inputs succeeded without secret disclosure\n"
	return os.WriteFile(filepath.Join(evidence, "enrollment-replay.txt"), []byte(observation), 0o600)
}

func (h *harness) group3(evidence string) error {
	if err := h.group3BootstrapCrash(evidence); err != nil {
		return err
	}
	if err := h.group3EnrollmentFaults(evidence); err != nil {
		return err
	}
	a, b := h.clients[0], h.clients[1]
	installations, err := h.cli(a, "--profile", "team", "installation", "list", "--include-revoked")
	if err != nil {
		return err
	}
	oldInstallationID, err := installationIDByLabel(installations, "acceptance-worker")
	if err != nil {
		return err
	}
	credential := filepath.Join(b.config, "worklease", "credentials", "team")
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
	missingCredential, err := h.cliFailure(b, "--profile", "team", "installation", "list")
	missingMCP, missingMCPErr := h.mcpToolCall(b, "list", map[string]any{})
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
	if missingMCPErr != nil {
		return missingMCPErr
	}
	if err := requireReason(missingCredential, "credential-unsafe", "authentication-required"); err != nil {
		return err
	}
	rotationInvite := filepath.Join(h.root, "secrets", ".rotation.invite")
	if _, err := h.cli(a, "--profile", "team", "invite", "issue", "--role", "write", "--invite-file", rotationInvite, "--label", "acceptance-worker-rotated"); err != nil {
		return err
	}
	inviteForB := rotationInvite
	if h.realHost {
		inviteForB = filepath.Join(h.remoteRoot, ".rotation.invite")
		if err := runSCP(h.remoteHost, rotationInvite, inviteForB); err != nil {
			return err
		}
	}
	oldCredential := credential + ".old"
	if h.realHost {
		_, renameErr = runSSH(h.remoteHost, "mv", credential, oldCredential)
	} else {
		renameErr = os.Rename(credential, oldCredential)
	}
	if renameErr != nil {
		return renameErr
	}
	if _, err := h.cli(b, "enroll", "--profile", "team", "--invite-file", inviteForB, "--label", "acceptance-worker-rotated"); err != nil {
		return err
	}
	newCredential := credential + ".new"
	if _, err := h.cli(a, "--profile", "team", "installation", "revoke", "--installation-id", oldInstallationID, "--reason", "acceptance rotation"); err != nil {
		return err
	}
	if h.realHost {
		_, renameErr = runSSH(h.remoteHost, "mv", credential, newCredential)
		if renameErr == nil {
			_, renameErr = runSSH(h.remoteHost, "mv", oldCredential, credential)
		}
	} else {
		renameErr = os.Rename(credential, newCredential)
		if renameErr == nil {
			renameErr = os.Rename(oldCredential, credential)
		}
	}
	if renameErr != nil {
		return renameErr
	}
	revoked, err := h.cliFailure(b, "--profile", "team", "acquire", "--handle", filepath.Join(b.home, "handles", "revoked.json"), "--resource", "coordination:revoked", "--ttl", "5s")
	if err != nil {
		return err
	}
	if err := requireReason(revoked, "installation-revoked"); err != nil {
		return err
	}
	revokedMCP, err := h.mcpToolCall(b, "list", map[string]any{})
	if err != nil {
		return err
	}
	guidance := map[string]any{"missingCredential": missingMCP, "revokedInstallation": revokedMCP}
	for state, expected := range map[string]string{"missingCredential": "authentication-required", "revokedInstallation": "installation-revoked"} {
		fields, _ := guidance[state].(map[string]any)
		errorFields, _ := fields["error"].(map[string]any)
		details, _ := errorFields["details"].(map[string]any)
		action, _ := details["action"].(string)
		if errorFields["reason"] != expected || details["profile"] != "team" || !strings.Contains(action, "worklease enroll --profile team --invite-file FILE") {
			return fmt.Errorf("MCP %s guidance=%v", state, fields)
		}
	}
	missingAction := guidance["missingCredential"].(map[string]any)["error"].(map[string]any)["details"].(map[string]any)["action"]
	revokedAction := guidance["revokedInstallation"].(map[string]any)["error"].(map[string]any)["details"].(map[string]any)["action"]
	if missingAction == revokedAction || !strings.Contains(fmt.Sprint(revokedAction), "request a new invite") {
		return fmt.Errorf("MCP authentication guidance is not distinct: %v", guidance)
	}
	guidancePath := filepath.Join(evidence, "mcp-authentication-guidance.json")
	guidanceData, err := json.MarshalIndent(guidance, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(guidancePath, append(guidanceData, '\n'), 0o600); err != nil {
		return err
	}
	if h.realHost {
		_, renameErr = runSSH(h.remoteHost, "mv", credential, oldCredential)
		if renameErr == nil {
			_, renameErr = runSSH(h.remoteHost, "mv", newCredential, credential)
		}
	} else {
		renameErr = os.Rename(credential, oldCredential)
		if renameErr == nil {
			renameErr = os.Rename(newCredential, credential)
		}
	}
	if renameErr != nil {
		return renameErr
	}
	rotatedHandle := filepath.Join(b.home, "handles", "rotated.json")
	if _, err := h.cli(b, "--profile", "team", "acquire", "--handle", rotatedHandle, "--resource", "coordination:rotated", "--ttl", "5s"); err != nil {
		return err
	}
	if _, err := h.cli(b, "--profile", "team", "release", "--handle", rotatedHandle, "--reason", "rotation observed"); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 3, Observation: "a real subprocess crash after the bootstrap grant commit recovers exactly once without disclosure; hidden, file, and descriptor invite sources work; lost enrollment responses retain exact requests; an incarnation mismatch does not burn the invite; MCP distinguishes missing and revoked installation guidance; roles and rotation remain isolated", Commands: []string{"crash hosted init after committed bootstrap grant", "resume hosted init with staged secret", "enroll through hidden prompt", "drop and replay invite issuance response", "drop and replay descriptor enrollment response", "retain dropped enrollment across restore", "reject mismatched incarnation then redeem the same invite", "remove credential and inspect MCP guidance", "rotate and revoke worker installation", "inspect revoked MCP guidance", "verify rotated bearer works"}, Evidence: []string{filepath.Join(evidence, "bootstrap-crash-ordering.json"), filepath.Join(evidence, "bootstrap-crash-output.txt"), filepath.Join(evidence, "bootstrap-recovery-output.json"), filepath.Join(evidence, "authority.log"), filepath.Join(evidence, "fault-proxy.log"), filepath.Join(evidence, "enrollment-replay.txt"), filepath.Join(evidence, "immutable-enrollment-before-restore.json"), filepath.Join(evidence, "enrollment-incarnation-mismatch.json"), filepath.Join(evidence, "post-mismatch-installations.json"), filepath.Join(evidence, "mcp-authentication-guidance.json"), rotationInvite}, Passed: true})
	return nil
}

func writeAcceptanceOwnerMarker(root string) error {
	return os.WriteFile(filepath.Join(root, ".worklease-acceptance-owner"), []byte("worklease remote acceptance fixture\n"), 0o600)
}

func validateAcceptanceDatabase(database string) error {
	resolved, err := filepath.EvalSymlinks(database)
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(resolved))
	if resolved != filepath.Join(root, "authority", "worklease.db") {
		return errors.New("retention fixture requires the acceptance authority database")
	}
	marker := filepath.Join(root, ".worklease-acceptance-owner")
	info, err := os.Stat(marker)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("retention fixture requires an owner-private acceptance marker")
	}
	return nil
}

func ageRetentionFixture(database string, at int64) (err error) {
	if err := validateAcceptanceDatabase(database); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	statements := []string{
		`UPDATE claims SET acquired_at=?,heartbeat_at=?,expires_at=?`,
		`UPDATE epochs SET acquired_at=?,ended_at=CASE WHEN ended_at IS NULL THEN NULL ELSE ? END,ended_recorded_at=CASE WHEN ended_recorded_at IS NULL THEN NULL ELSE ? END`,
		`UPDATE operations SET request_not_after=?,started_at=?,completed_at=CASE WHEN completed_at IS NULL THEN NULL ELSE ? END`,
		`UPDATE reconciliations SET recorded_at=?`,
		`UPDATE events SET at=?`,
		`UPDATE invites SET issued_at=?,expires_at=?+1,request_not_after=?+2,used_at=CASE WHEN used_at IS NULL THEN NULL ELSE ? END,revoked_at=CASE WHEN revoked_at IS NULL THEN NULL ELSE ? END`,
		`UPDATE invite_redemptions SET redeemed_at=?,replay_until=?+1,request_not_after=?+2`,
		`UPDATE admin_operation_replays SET request_not_after=?`,
	}
	for _, statement := range statements {
		arguments := strings.Count(statement, "?")
		values := make([]any, arguments)
		for i := range values {
			values[i] = at
		}
		if _, err = tx.Exec(statement, values...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func installRecoveryClosureFixture(database string) (result map[string]any, err error) {
	if err := validateAcceptanceDatabase(database); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var restoreID, installationID string
	if err = tx.QueryRow(`SELECT value FROM meta WHERE key='restore_id'`).Scan(&restoreID); err != nil {
		return nil, err
	}
	if err = tx.QueryRow(`SELECT installation_id FROM installations WHERE revoked_at IS NULL ORDER BY enrolled_at DESC LIMIT 1`).Scan(&installationID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err = tx.Exec(`UPDATE recovery_state SET recovery_mode=1,recovery_revision=recovery_revision+1`); err != nil {
		return nil, err
	}
	for i := 0; i < 17; i++ {
		claimID := fmt.Sprintf("%032x", 0x7000+i)
		if _, err = tx.Exec(`INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,end_reason,final_revision,admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, claimID, strings.Repeat("a", 64), "closure-fixture", "closure-fixture", "closure-fixture", "local-coordination", 0, now.Add(-time.Minute).UnixMicro(), 7000+i, now.Add(-time.Minute).UnixMicro(), "restored", 2, (30 * time.Second).Microseconds(), now.Add(time.Minute).UnixMicro(), installationID, restoreID); err != nil {
			return nil, err
		}
		resources := []string{fmt.Sprintf("coordination:recovery-closure-%02d", i*2), fmt.Sprintf("coordination:recovery-closure-%02d", i*2+1), fmt.Sprintf("coordination:recovery-closure-%02d", i*2+2)}
		for position, resource := range resources {
			if _, err = tx.Exec(`INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,?)`, claimID, resource, position); err != nil {
				return nil, err
			}
		}
		operationID := fmt.Sprintf("%032x", 0x8000+i)
		if _, err = tx.Exec(`INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`, claimID, operationID, "exec", strings.Repeat("b", 64), now.Add(time.Hour).UnixMicro(), 1, "started", now.Add(-time.Minute).UnixMicro(), 7000+i, installationID, restoreID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"firstResource": "coordination:recovery-closure-00", "resources": 35, "operations": 17, "restoreId": restoreID}, nil
}

func (h *harness) ageRetentionState(evidence string) error {
	database := filepath.Join(h.root, "authority", "worklease.db")
	if h.realHost {
		database = filepath.Join(h.remoteRoot, "authority", "worklease.db")
	}
	h.stopServer()
	at := strconv.FormatInt(time.Now().Add(-72*time.Hour).UnixMicro(), 10)
	h.logCommand("fixture@"+h.remoteHost, []string{"age-retention-fixture", database, at})
	var err error
	if h.realHost {
		_, err = runSSH(h.remoteHost, h.remoteHelper, "age-retention-fixture", database, at)
	} else {
		err = ageRetentionFixture(database, mustParseInt64(at))
	}
	if err != nil {
		return err
	}
	return h.restartServer(evidence)
}

func mustParseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func isSQLiteFull(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_FULL
}

func installFullVolumeFixture(database string) (state fullVolumeState, err error) {
	if err := validateAcceptanceDatabase(database); err != nil {
		return state, err
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return state, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if _, err = db.Exec(`DROP TRIGGER IF EXISTS worklease_acceptance_volume_full; DROP TABLE IF EXISTS worklease_acceptance_volume_fill; CREATE TABLE worklease_acceptance_volume_fill(payload BLOB NOT NULL); CREATE TRIGGER worklease_acceptance_volume_full BEFORE INSERT ON admin_operation_replays BEGIN INSERT INTO worklease_acceptance_volume_fill(payload) VALUES(zeroblob(1048576)); END`); err != nil {
		return state, err
	}
	defer func() {
		if err != nil {
			_, _ = db.Exec(`DROP TRIGGER IF EXISTS worklease_acceptance_volume_full; DROP TABLE IF EXISTS worklease_acceptance_volume_fill`)
		}
	}()
	var pageCount int64
	if err = db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return state, err
	}
	if _, err = db.Exec(fmt.Sprintf(`PRAGMA max_page_count=%d`, pageCount+32)); err != nil {
		return state, err
	}
	for {
		_, insertErr := db.Exec(`INSERT INTO worklease_acceptance_volume_fill(payload) VALUES(zeroblob(4096))`)
		if insertErr == nil {
			continue
		}
		if !isSQLiteFull(insertErr) {
			return state, insertErr
		}
		break
	}
	if err = db.QueryRow(`PRAGMA page_count`).Scan(&state.PageCount); err != nil {
		return state, err
	}
	if err = db.QueryRow(`PRAGMA max_page_count`).Scan(&state.MaxPageCount); err != nil {
		return state, err
	}
	if err = db.QueryRow(`PRAGMA freelist_count`).Scan(&state.FreelistCount); err != nil {
		return state, err
	}
	if state.PageCount != state.MaxPageCount || state.FreelistCount > 1 {
		return state, fmt.Errorf("volume fixture is not full: %+v", state)
	}
	state.Injection = "SQLite max_page_count saturation; server connection inherits the same ceiling and trigger allocates a new overflow page"
	return state, nil
}

func clearFullVolumeFixture(database string) (err error) {
	if err := validateAcceptanceDatabase(database); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	_, err = db.Exec(`DROP TRIGGER IF EXISTS worklease_acceptance_volume_full; DROP TABLE IF EXISTS worklease_acceptance_volume_fill`)
	return err
}

func readStorageSnapshot(database string) (snapshot storageSnapshot, err error) {
	if err := validateAcceptanceDatabase(database); err != nil {
		return snapshot, err
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		return snapshot, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	snapshot.Tables = map[string]int64{}
	for _, table := range []string{"claims", "epochs", "operations", "reconciliations", "events", "invites", "invite_redemptions", "admin_operation_replays"} {
		var count int64
		if err = db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			return snapshot, err
		}
		snapshot.Tables[table] = count
	}
	snapshot.Meta = map[string]string{}
	rows, err := db.Query(`SELECT key,value FROM meta WHERE key IN ('last_observed_at','last_event_seq','pruned_through_seq') ORDER BY key`)
	if err != nil {
		return snapshot, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return snapshot, err
		}
		snapshot.Meta[key] = value
	}
	return snapshot, rows.Err()
}

func (h *harness) authorityDatabase() string {
	if h.realHost {
		return filepath.Join(h.remoteRoot, "authority", "worklease.db")
	}
	return filepath.Join(h.root, "authority", "worklease.db")
}

func (h *harness) fixtureJSON(command string, target any) error {
	database := h.authorityDatabase()
	h.logCommand("fixture@"+h.remoteHost, []string{command, database})
	var output []byte
	var err error
	if h.realHost {
		var remoteOutput string
		remoteOutput, err = runSSH(h.remoteHost, h.remoteHelper, command, database)
		output = []byte(remoteOutput)
	} else {
		var value any
		switch command {
		case "full-volume-fixture":
			value, err = installFullVolumeFixture(database)
		case "clear-full-volume-fixture":
			err = clearFullVolumeFixture(database)
			value = map[string]bool{"cleared": err == nil}
		case "storage-snapshot":
			value, err = readStorageSnapshot(database)
		default:
			return fmt.Errorf("unknown acceptance fixture command %q", command)
		}
		if err == nil {
			output, err = json.Marshal(value)
		}
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(output, target)
}

func (h *harness) exerciseFullVolume(evidence string, c client) (err error) {
	if err := h.startFaultProxy(evidence); err != nil {
		return err
	}
	defer func() {
		_ = h.switchProfileEndpoint(c, h.endpoint)
		h.stopFaultProxy()
	}()
	if err := h.switchProfileEndpoint(c, h.faultEndpoint); err != nil {
		return err
	}
	h.stopServer()
	var before storageSnapshot
	if err = h.fixtureJSON("storage-snapshot", &before); err != nil {
		return err
	}
	var volume fullVolumeState
	if err = h.fixtureJSON("full-volume-fixture", &volume); err != nil {
		return err
	}
	installed := true
	h.fullVolumeMaxPageCount = volume.MaxPageCount
	defer func() {
		if !installed {
			return
		}
		h.stopServer()
		h.fullVolumeMaxPageCount = 0
		var cleared map[string]bool
		cleanupErr := h.fixtureJSON("clear-full-volume-fixture", &cleared)
		if cleanupErr == nil {
			cleanupErr = h.restartServer(evidence)
		}
		err = errors.Join(err, cleanupErr)
	}()
	if err = h.restartServer(evidence); err != nil {
		return err
	}
	failed, failureErr := h.cliFailure(c, "--profile", "team", "gc", "--apply", "--cutoff", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano))
	if failureErr != nil {
		return failureErr
	}
	if err = requireReason(failed, "unknown-outcome"); err != nil {
		return err
	}
	if err = h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err = verifyStorageFailureResponse(filepath.Join(evidence, "fault-proxy.log")); err != nil {
		return err
	}
	h.stopServer()
	var after storageSnapshot
	if err = h.fixtureJSON("storage-snapshot", &after); err != nil {
		return err
	}
	beforeData, _ := json.Marshal(before)
	afterData, _ := json.Marshal(after)
	if !bytes.Equal(beforeData, afterData) {
		return fmt.Errorf("full-volume failure pruned or mutated authority state: before=%s after=%s", beforeData, afterData)
	}
	var cleared map[string]bool
	if err = h.fixtureJSON("clear-full-volume-fixture", &cleared); err != nil {
		return err
	}
	h.fullVolumeMaxPageCount = 0
	installed = false
	if !cleared["cleared"] {
		return errors.New("full-volume fixture did not clear")
	}
	if err = h.restartServer(evidence); err != nil {
		return err
	}
	record := map[string]any{"volume": volume, "failure": failed, "before": before, "after": after, "stateUnchanged": true}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(evidence, "full-volume-storage-failure.json"), append(encoded, '\n'), 0o600)
}

func (h *harness) group4(evidence string) error {
	a, b := h.clients[0], h.clients[1]
	pendingPath := filepath.Join(h.immutableEnrollmentClient.home, "pending", "team", h.immutableEnrollmentID+".json")
	if h.immutableEnrollmentID == "" {
		return errors.New("pending-survival enrollment fixture is missing")
	}
	var err error
	h.pendingSurvivalBefore, err = os.ReadFile(pendingPath)
	if err != nil {
		return err
	}
	agedAt := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(pendingPath, agedAt, agedAt); err != nil {
		return err
	}
	snapshot := func() (map[string]any, string, error) {
		events, err := h.cli(a, "--profile", "team", "events", "--limit", "100")
		if err != nil {
			return nil, "", err
		}
		cursor, _ := events["nextCursor"].(string)
		if cursor == "" {
			return nil, "", errors.New("events returned no cursor")
		}
		return events, cursor, nil
	}
	acquire := func(name string) error {
		_, err := h.cli(b, "--profile", "team", "acquire", "--handle", filepath.Join(b.home, "handles", name+".json"), "--resource", "coordination:"+name, "--ttl", "5s")
		return err
	}

	beforeSnapshot, beforeCursor, err := snapshot()
	if err != nil {
		return err
	}
	if err := acquire("watch-before-start"); err != nil {
		return err
	}
	beforeWatch, err := h.cli(a, "--profile", "team", "watch", "--cursor", beforeCursor, "--timeout", "2s")
	if err != nil {
		return err
	}
	if err := requireWatchEvent(beforeWatch, beforeCursor, "acquired", "coordination:watch-before-start"); err != nil {
		return fmt.Errorf("event committed between snapshot and watch: %w", err)
	}

	duringName := "watch-during-wait"
	if err := acquire(duringName); err != nil {
		return err
	}
	if err := h.startFaultProxy(evidence); err != nil {
		return err
	}
	defer func() {
		_ = h.switchProfileEndpoint(a, h.endpoint)
		h.stopFaultProxy()
	}()
	if err := h.switchProfileEndpoint(a, h.faultEndpoint); err != nil {
		return err
	}
	duringResource := "coordination:" + duringName
	duringCommand, duringOutput, err := h.startCLI(a, "--profile", "team", "watch", "--resource", duringResource, "--until", "free", "--timeout", "3s")
	if err != nil {
		return err
	}
	if err := h.waitFaultLogOccurrences("path=/v1/watch phase=forwarded", 1); err != nil {
		_ = duringCommand.Process.Kill()
		_, _ = waitStartedCommand(duringCommand, 5*time.Second)
		return err
	}
	if _, err := h.cli(b, "--profile", "team", "release", "--handle", filepath.Join(b.home, "handles", duringName+".json"), "--reason", "watch snapshot established"); err != nil {
		_ = duringCommand.Process.Kill()
		_, _ = waitStartedCommand(duringCommand, 5*time.Second)
		return err
	}
	duringWatch, err := waitCLISuccess(duringCommand, duringOutput, 5*time.Second)
	if err != nil {
		return err
	}
	if err := requireWatchFreeAfterEvent(duringWatch, duringResource); err != nil {
		return fmt.Errorf("release after active-state watch snapshot: %w", err)
	}

	disconnectedSnapshot, disconnectedCursor, err := snapshot()
	if err != nil {
		return err
	}
	if err := h.armFault("/v1/watch"); err != nil {
		return err
	}
	disconnectedCommand, disconnectedOutput, err := h.startCLI(a, "--profile", "team", "watch", "--cursor", disconnectedCursor, "--timeout", "5s")
	if err != nil {
		return err
	}
	if err := h.waitFaultLogOccurrences("path=/v1/watch phase=forwarded", 2); err != nil {
		_ = disconnectedCommand.Process.Kill()
		_, _ = waitStartedCommand(disconnectedCommand, 5*time.Second)
		return err
	}
	if err := acquire("watch-disconnected"); err != nil {
		_ = disconnectedCommand.Process.Kill()
		_, _ = waitStartedCommand(disconnectedCommand, 5*time.Second)
		return err
	}
	disconnectErr, waitErr := waitStartedCommand(disconnectedCommand, 7*time.Second)
	if waitErr != nil {
		return waitErr
	}
	var disconnectedResult map[string]any
	if disconnectErr == nil || json.Unmarshal(disconnectedOutput.Bytes(), &disconnectedResult) == nil && disconnectedResult["ok"] == true {
		return fmt.Errorf("disconnected watch received a successful acknowledgment: err=%v output=%s", disconnectErr, disconnectedOutput.String())
	}
	if err := h.switchProfileEndpoint(a, h.endpoint); err != nil {
		return err
	}
	reconnectedWatch, err := h.cli(a, "--profile", "team", "watch", "--cursor", disconnectedCursor, "--timeout", "2s")
	if err != nil {
		return err
	}
	if err := requireWatchEvent(reconnectedWatch, disconnectedCursor, "acquired", "coordination:watch-disconnected"); err != nil {
		return fmt.Errorf("reconnected watch: %w", err)
	}
	reconnectedCursor, _ := reconnectedWatch["nextCursor"].(string)
	noDuplicate, err := h.cli(a, "--profile", "team", "watch", "--cursor", reconnectedCursor, "--timeout", "200ms")
	if err != nil {
		return err
	}
	if err := requireWatchTimeout(noDuplicate, reconnectedCursor); err != nil {
		return fmt.Errorf("post-reconnect watch: %w", err)
	}
	if err := h.collectFaultLog(evidence); err != nil {
		return err
	}
	if err := verifyDroppedWatch(filepath.Join(evidence, "fault-proxy.log")); err != nil {
		return err
	}
	h.stopFaultProxy()
	if err := h.switchProfileEndpoint(a, h.endpoint); err != nil {
		return err
	}

	incarnationSnapshot, incarnationCursor, err := snapshot()
	if err != nil {
		return err
	}
	parsedCursor, err := ledger.ParseCursor(incarnationCursor)
	if err != nil {
		return err
	}
	foreignCursor := ledger.EncodeCursor(strings.Repeat("f", 32), parsedCursor.RestoreID, parsedCursor.Feed, parsedCursor.Filter, mustParseInt64(parsedCursor.Sequence))
	foreignResult, err := h.cliFailure(a, "--profile", "team", "events", "--cursor", foreignCursor)
	if err != nil {
		return err
	}
	if err := requireReason(foreignResult, "cursor-invalid"); err != nil {
		return fmt.Errorf("foreign-authority events cursor: %w", err)
	}
	foreignWatch, err := h.cliFailure(a, "--profile", "team", "watch", "--cursor", foreignCursor, "--timeout", "200ms")
	if err != nil {
		return err
	}
	if err := requireReason(foreignWatch, "cursor-invalid"); err != nil {
		return fmt.Errorf("foreign-authority watch cursor: %w", err)
	}
	oldCursor := ledger.EncodeCursor(parsedCursor.AuthorityID, parsedCursor.RestoreID, "events", "", 0)
	newerHandle := filepath.Join(a.home, "handles", "retention-newer.json")
	if _, err := h.cli(a, "--profile", "team", "acquire", "--handle", newerHandle, "--resource", "coordination:retention-newer", "--ttl", "1s"); err != nil {
		return err
	}
	if _, err := h.cli(a, "--profile", "team", "release", "--handle", newerHandle, "--reason", "retention pin fixture"); err != nil {
		return err
	}
	if err := h.ageRetentionState(evidence); err != nil {
		return err
	}
	firstGC, err := h.cli(a, "--profile", "team", "gc", "--apply", "--cutoff", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if err := h.ageRetentionState(evidence); err != nil {
		return err
	}
	// The first pass retires expired claims; after aging again, the second pass
	// has objectively prunable epochs/events. Fail that apply on SQLITE_FULL
	// before allowing the unchanged retry to collect them.
	if err := h.exerciseFullVolume(evidence, a); err != nil {
		return err
	}
	secondGC, err := h.cli(a, "--profile", "team", "gc", "--apply", "--cutoff", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	gapEvents, err := h.cli(a, "--profile", "team", "events", "--cursor", oldCursor, "--limit", "100")
	if err != nil {
		return err
	}
	prunedThrough, _ := secondGC["prunedThroughSequence"].(string)
	if err := requireEventsGap(gapEvents, oldCursor, prunedThrough); err != nil {
		return err
	}
	gapWatch, err := h.cli(a, "--profile", "team", "watch", "--cursor", oldCursor, "--timeout", "2s")
	if err != nil {
		return err
	}
	if err := requireWatchGap(gapWatch, oldCursor, prunedThrough); err != nil {
		return err
	}
	resetCursor, _ := gapEvents["nextCursor"].(string)
	resumedEvents, err := h.cli(a, "--profile", "team", "events", "--cursor", resetCursor, "--limit", "100")
	if err != nil {
		return err
	}
	if resumedEvents["gap"] == true {
		return fmt.Errorf("events reset cursor produced another gap: %v", resumedEvents)
	}
	resumedWatch, err := h.cli(a, "--profile", "team", "watch", "--cursor", resetCursor, "--timeout", "2s")
	if err != nil {
		return err
	}
	if resumedWatch["gap"] == true {
		return fmt.Errorf("watch reset cursor produced another gap: %v", resumedWatch)
	}
	retainedEvents, err := h.cli(a, "--profile", "team", "events", "--limit", "1000")
	if err != nil {
		return err
	}
	if !eventsContainKindResource(retainedEvents, "exec-started", "coordination:lost-begin") || !eventsContainResource(retainedEvents, "coordination:retention-newer") {
		return errors.New("stuck started operation did not pin its own and newer retained history")
	}
	stuckInspection, err := h.cli(a, "--profile", "team", "op", "inspect", "--handle", filepath.Join(a.home, "handles", "lost-begin.json"), "--operation-id", strings.Repeat("1", 32), "--full")
	if err != nil {
		return err
	}
	inspection, _ := stuckInspection["inspection"].(map[string]any)
	if inspection["state"] != "started" || inspection["operationId"] != strings.Repeat("1", 32) {
		return fmt.Errorf("stuck operation was not retained as started: %v", inspection)
	}
	newerHistory, err := h.cli(a, "--profile", "team", "history", "--resource", "coordination:retention-newer", "--limit", "10", "--full")
	if err != nil {
		return err
	}
	newerEpochs, _ := newerHistory["epochs"].([]any)
	if len(newerEpochs) != 1 {
		return fmt.Errorf("newer pinned epoch count=%d, want 1", len(newerEpochs))
	}
	if protectedCount(secondGC, "expiredClaims") < 1 || protectedCount(secondGC, "epochs") < 2 {
		return fmt.Errorf("stuck-history protection missing: %v", secondGC["protected"])
	}
	if resultSummaryCount(secondGC, "collected", "epochs") < 1 || resultSummaryCount(secondGC, "collected", "events") < 1 {
		return fmt.Errorf("post-full-volume GC had no objective prunable epoch/event: %v", secondGC["collected"])
	}
	h.preRestoreCursor = resetCursor
	if h.preRestoreCursor == "" {
		return errors.New("retention gap returned no reset cursor")
	}
	retentionEvidence := filepath.Join(evidence, "cursor-retention-gaps.json")
	retentionRecord := map[string]any{"incarnationSnapshot": incarnationSnapshot, "foreignAuthorityEvents": foreignResult, "foreignAuthorityWatch": foreignWatch, "firstGC": firstGC, "secondGC": secondGC, "eventsGap": gapEvents, "watchGap": gapWatch, "resumedEvents": resumedEvents, "resumedWatch": resumedWatch, "stuckInspection": stuckInspection, "newerHistory": newerHistory, "retainedEvents": retainedEvents}
	retentionData, err := json.MarshalIndent(retentionRecord, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(retentionEvidence, append(retentionData, '\n'), 0o600); err != nil {
		return err
	}
	if err := h.startFaultProxy(evidence); err != nil {
		return err
	}
	if err := h.switchProfileEndpoint(h.immutableEnrollmentClient, h.faultEndpoint); err != nil {
		h.stopFaultProxy()
		return err
	}
	replayExpiredOutput, replayExpiredErr := h.replayPendingFailure(h.immutableEnrollmentClient, h.immutableEnrollmentID, h.immutableEnrollmentInvite)
	collectErr := h.collectFaultLog(evidence)
	restoreEndpointErr := h.switchProfileEndpoint(h.immutableEnrollmentClient, h.endpoint)
	h.stopFaultProxy()
	if err := errors.Join(replayExpiredErr, collectErr, restoreEndpointErr); err != nil {
		return err
	}
	inviteSecret, err := os.ReadFile(h.immutableEnrollmentInvite)
	if err != nil {
		return err
	}
	if bytes.Contains(replayExpiredOutput, bytes.TrimSpace(inviteSecret)) {
		return errors.New("aged pending replay disclosed its invite")
	}
	if err := os.WriteFile(filepath.Join(evidence, "pending-replay-expired-output.txt"), replayExpiredOutput, 0o600); err != nil {
		return err
	}
	if !bytes.Contains(replayExpiredOutput, []byte("unknown-outcome")) {
		return fmt.Errorf("aged pending replay did not retain client uncertainty: output=%q", replayExpiredOutput)
	}
	if err := verifyReplayExpiredResponse(filepath.Join(evidence, "fault-proxy.log"), h.immutableEnrollmentID); err != nil {
		return err
	}
	pendingAfterGroup4, err := os.ReadFile(pendingPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(h.pendingSurvivalBefore, pendingAfterGroup4) {
		return errors.New("age, replay expiry, GC, or authority restart changed retained client pending evidence")
	}
	pendingHash := sha256.Sum256(h.pendingSurvivalBefore)
	pendingEvidencePath := filepath.Join(evidence, "pending-evidence-survival.json")
	pendingEvidence, err := json.MarshalIndent(map[string]any{
		"requestId": h.immutableEnrollmentID, "requestSha256": h.immutableEnrollmentBefore.RequestSHA256,
		"recordSha256": hex.EncodeToString(pendingHash[:]), "agedAt": agedAt.UTC().Format(time.RFC3339Nano),
		"survivedAge": true, "replayBeforeRestoreReason": "replay-expired",
		"survivedReplayExpiryAndGC": true, "survivedAuthorityRestarts": true,
		"survivedProfileRefresh": false,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(pendingEvidencePath, append(pendingEvidence, '\n'), 0o600); err != nil {
		return err
	}

	pendingRoot := filepath.Join(h.immutableEnrollmentClient.home, "pending", "team")
	entries, err := os.ReadDir(pendingRoot)
	if err != nil {
		return fmt.Errorf("pending root missing: %v", err)
	}
	foundPending := false
	for _, entry := range entries {
		if entry.Name() == h.immutableEnrollmentID+".json" {
			foundPending = true
		}
	}
	if !foundPending {
		return errors.New("retained pending request is not enumerable")
	}
	inventory := filepath.Join(evidence, "pending-inventory.txt")
	if err := os.WriteFile(inventory, []byte(fmt.Sprintf("pending-root=%s\nenumerable-pending-files=%d\ntarget-request-id=%s\ntarget-enumerable=true\n", pendingRoot, len(entries), h.immutableEnrollmentID)), 0o600); err != nil {
		return err
	}
	watchEvidence := filepath.Join(evidence, "snapshot-watch-reconnect.json")
	evidenceRecord := map[string]any{
		"beforeWatchSnapshot": beforeSnapshot, "beforeWatchResult": beforeWatch,
		"duringWatchInitialState": "active", "duringWatchResult": duringWatch,
		"disconnectedSnapshot": disconnectedSnapshot, "disconnectedClientError": disconnectErr.Error(),
		"disconnectedClientOutputBytes": disconnectedOutput.Len(), "reconnectedResult": reconnectedWatch,
		"postReconnectResult": noDuplicate,
	}
	encoded, err := json.MarshalIndent(evidenceRecord, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(watchEvidence, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{Group: 4, Observation: "snapshot/watch ordering preserves an event committed before cursor resume and a release committed after an active-state snapshot; a response-ready watch can lose its transport before acknowledgment and reconnect from its saved cursor exactly once; foreign-authority cursors fail closed, retention gaps return explicit reset cursors, and a stuck started operation pins its own and newer history; a full SQLite volume returns storage-failure and rolls back pruning; exact client pending evidence survives aging, replay expiry, GC, and authority restarts", Commands: []string{"snapshot then mutate before cursor watch", "acquire then watch until free and release", "drop completed watch response", "reconnect from saved cursor", "resume from next cursor without duplicate", "reject foreign-authority cursor", "age retention fixture and apply GC twice", "resume events and watch below pruning watermark", "verify stuck and newer history remain", "fill bounded SQLite volume and attempt GC", "compare authority state and retained pending bytes"}, Evidence: []string{watchEvidence, filepath.Join(evidence, "fault-proxy.log"), retentionEvidence, filepath.Join(evidence, "full-volume-storage-failure.json"), pendingEvidencePath, inventory}, Passed: true})
	return nil
}

func verifyReplayExpiredResponse(logPath, requestID string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	matches := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "path=/v1/enroll ") && strings.Contains(line, "requestId="+requestID+" ") && strings.Contains(line, "reason=replay-expired ") {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("replay-expired server response count=%d, want 1", matches)
	}
	return nil
}

func verifyStorageFailureResponse(logPath string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	matches := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "path=/v1/admin/gc ") && strings.Contains(line, "status=503 ") && strings.Contains(line, "reason=storage-failure ") {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("full-volume storage-failure response count=%d, want 1", matches)
	}
	return nil
}

func verifyDroppedWatch(logPath string) error {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	dropped := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "path=/v1/watch ") && strings.Contains(line, "status=200 ") && strings.Contains(line, "dropped=true ") {
			dropped++
		}
	}
	if dropped != 1 {
		return fmt.Errorf("completed watch response drop count=%d, want 1", dropped)
	}
	return nil
}

func requireWatchEvent(result map[string]any, cursor, kind, resource string) error {
	if result["cursor"] != cursor {
		return fmt.Errorf("cursor=%v, want %q", result["cursor"], cursor)
	}
	next, _ := result["nextCursor"].(string)
	if next == "" || next == cursor {
		return fmt.Errorf("next cursor did not advance: %q", next)
	}
	event, _ := result["event"].(map[string]any)
	if event == nil || event["kind"] != kind {
		return fmt.Errorf("event=%v, want kind %q", event, kind)
	}
	resources, _ := event["resources"].([]any)
	for _, value := range resources {
		if value == resource {
			return nil
		}
	}
	return fmt.Errorf("event resources=%v, want %q", resources, resource)
}

func requireWatchFreeAfterEvent(result map[string]any, resource string) error {
	if result["free"] != true || result["timedOut"] == true {
		return fmt.Errorf("watch did not observe free state: %v", result)
	}
	cursor, _ := result["cursor"].(string)
	next, _ := result["nextCursor"].(string)
	if cursor == "" || next != cursor {
		return fmt.Errorf("watch returned without scanning the release event: cursor=%q next=%q", cursor, next)
	}
	resources, _ := result["resources"].([]any)
	for _, raw := range resources {
		observed, _ := raw.(map[string]any)
		if observed["resource"] == resource && observed["state"] == "free" {
			return nil
		}
	}
	return fmt.Errorf("watch resources=%v, want %s=free", resources, resource)
}

func requireWatchTimeout(result map[string]any, cursor string) error {
	if result["timedOut"] != true || result["event"] != nil {
		return fmt.Errorf("result did not time out without an event: %v", result)
	}
	if result["cursor"] != cursor || result["nextCursor"] != cursor {
		return fmt.Errorf("timeout cursor changed: cursor=%v next=%v want=%q", result["cursor"], result["nextCursor"], cursor)
	}
	return nil
}

func requireEventsGap(result map[string]any, cursor, prunedThrough string) error {
	events, _ := result["events"].([]any)
	next, _ := result["nextCursor"].(string)
	if result["gap"] != true || len(events) != 0 || next == "" || next == cursor {
		return fmt.Errorf("events retention gap is not explicit: %v", result)
	}
	return requireResetCursor(cursor, next, prunedThrough)
}

func requireWatchGap(result map[string]any, cursor, prunedThrough string) error {
	next, _ := result["nextCursor"].(string)
	reset, _ := result["resetCursor"].(string)
	if result["gap"] != true || result["event"] != nil || next == "" || next == cursor || reset != next {
		return fmt.Errorf("watch retention gap is not explicit: %v", result)
	}
	return requireResetCursor(cursor, reset, prunedThrough)
}

func requireResetCursor(original, reset, sequence string) error {
	before, err := ledger.ParseCursor(original)
	if err != nil {
		return err
	}
	after, err := ledger.ParseCursor(reset)
	if err != nil {
		return err
	}
	if sequence == "" || before.AuthorityID != after.AuthorityID || before.RestoreID != after.RestoreID || before.Feed != after.Feed || before.Filter != after.Filter || after.Sequence != sequence {
		return fmt.Errorf("reset cursor is not bound to pruning watermark %q: before=%+v after=%+v", sequence, before, after)
	}
	return nil
}

func eventsContainResource(result map[string]any, resource string) bool {
	return eventsContainKindResource(result, "", resource)
}

func eventsContainKindResource(result map[string]any, kind, resource string) bool {
	events, _ := result["events"].([]any)
	for _, raw := range events {
		event, _ := raw.(map[string]any)
		if kind != "" && event["kind"] != kind {
			continue
		}
		resources, _ := event["resources"].([]any)
		for _, observed := range resources {
			if observed == resource {
				return true
			}
		}
	}
	return false
}

func resultSummaryCount(result map[string]any, section, key string) int {
	values, _ := result[section].(map[string]any)
	summary, _ := values[key].(map[string]any)
	count, _ := summary["count"].(float64)
	return int(count)
}

func protectedCount(result map[string]any, key string) int {
	return resultSummaryCount(result, "protected", key)
}

func (h *harness) pendingSetInventories() (pendingSetInventory, pendingSetInventory, error) {
	if h.zeroPendingClient.home == "" || h.immutableEnrollmentClient.home == "" {
		return pendingSetInventory{}, pendingSetInventory{}, errors.New("pending-set inventory clients are missing")
	}
	inventory := func(label string, c client) (pendingSetInventory, error) {
		root := filepath.Join(c.home, "pending", "team")
		records, err := authority.NewFilePendingStore(root).List()
		if err != nil {
			return pendingSetInventory{}, err
		}
		requestIDs := make([]string, 0, len(records))
		for _, record := range records {
			requestIDs = append(requestIDs, record.RequestID)
		}
		sort.Strings(requestIDs)
		return pendingSetInventory{Label: label, Root: root, RequestIDs: requestIDs}, nil
	}
	zero, err := inventory("confirmed-enrollment-zero-pending", h.zeroPendingClient)
	if err != nil {
		return pendingSetInventory{}, pendingSetInventory{}, err
	}
	nonzero, err := inventory("lost-enrollment-nonzero-pending", h.immutableEnrollmentClient)
	if err != nil {
		return pendingSetInventory{}, pendingSetInventory{}, err
	}
	if len(zero.RequestIDs) != 0 {
		return pendingSetInventory{}, pendingSetInventory{}, fmt.Errorf("zero pending-set fixture contains requests: %v", zero.RequestIDs)
	}
	if len(nonzero.RequestIDs) == 0 || !slicesContains(nonzero.RequestIDs, h.immutableEnrollmentID) {
		return pendingSetInventory{}, pendingSetInventory{}, fmt.Errorf("nonzero pending-set fixture does not contain %s: %v", h.immutableEnrollmentID, nonzero.RequestIDs)
	}
	return zero, nonzero, nil
}

func anySliceContains(values []any, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func slicesContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (h *harness) captureAsynchronousBackup(evidence, authorityDB, name string, observe func() error) (backupCapture, error) {
	nonce, err := randomRequestID()
	if err != nil {
		return backupCapture{}, err
	}
	nonce = nonce[:12]
	localBackup := filepath.Join(evidence, "asynchronous-backup-"+name+".db")
	localReady := filepath.Join(evidence, "asynchronous-backup-"+name+"-"+nonce+".ready")
	localControl := filepath.Join(evidence, "asynchronous-backup-"+name+"-"+nonce+".control")
	localResult := filepath.Join(evidence, "asynchronous-backup-"+name+"-"+nonce+".json")
	backupPath, readyPath, controlPath, resultPath := localBackup, localReady, localControl, localResult
	if h.realHost {
		backupPath = filepath.Join(h.remoteRoot, "asynchronous-backup-"+name+".db")
		readyPath = filepath.Join(h.remoteRoot, "asynchronous-backup-"+name+"-"+nonce+".ready")
		controlPath = filepath.Join(h.remoteRoot, "asynchronous-backup-"+name+"-"+nonce+".control")
		resultPath = filepath.Join(h.remoteRoot, "asynchronous-backup-"+name+"-"+nonce+".json")
	}
	args := []string{"backup", authorityDB, backupPath, controlPath, readyPath, resultPath}
	h.logCommand("async-backup@"+h.remoteHost, append([]string{"worklease-remote-smoke"}, args...))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var command *exec.Cmd
	if h.realHost {
		command = exec.CommandContext(ctx, "ssh", append([]string{h.remoteHost, h.remoteHelper}, args...)...)
	} else {
		command = exec.CommandContext(ctx, h.self, args...)
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	startedAt := time.Now().UTC()
	if err := command.Start(); err != nil {
		return backupCapture{}, err
	}
	waitReady := func() error {
		if h.realHost {
			_, err := runSSH(h.remoteHost, h.remoteHelper, "wait-file", readyPath, "backup", "ready", "10s")
			return err
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			contents, err := os.ReadFile(readyPath)
			if err == nil && strings.TrimSpace(string(contents)) == "backup ready" {
				return nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		return errors.New("timed out waiting for asynchronous backup fixture")
	}
	if err := waitReady(); err != nil {
		cancel()
		_ = command.Wait()
		return backupCapture{}, err
	}
	if err := observe(); err != nil {
		cancel()
		_ = command.Wait()
		return backupCapture{}, fmt.Errorf("observe live authority during %s backup: %w", name, err)
	}
	if h.realHost {
		localControlCopy := filepath.Join(h.root, "asynchronous-backup-"+name+"-"+nonce+".control")
		if err := os.WriteFile(localControlCopy, []byte("capture now\n"), 0o600); err != nil {
			cancel()
			_ = command.Wait()
			return backupCapture{}, err
		}
		if err := runSCP(h.remoteHost, localControlCopy, controlPath); err != nil {
			cancel()
			_ = command.Wait()
			return backupCapture{}, err
		}
	} else if err := os.WriteFile(controlPath, []byte("capture now\n"), 0o600); err != nil {
		cancel()
		_ = command.Wait()
		return backupCapture{}, err
	}
	if err := command.Wait(); err != nil {
		return backupCapture{}, fmt.Errorf("asynchronous %s backup: %w; output-bytes=%d", name, err, output.Len())
	}
	if h.realHost {
		if err := runSCP(h.remoteHost, h.remoteHost+":"+backupPath, localBackup); err != nil {
			return backupCapture{}, err
		}
		if err := runSCP(h.remoteHost, h.remoteHost+":"+resultPath, localResult); err != nil {
			return backupCapture{}, err
		}
		h.report.RemoteEvidence = append(h.report.RemoteEvidence, h.remoteHost+":"+backupPath, h.remoteHost+":"+resultPath)
	}
	data, err := os.ReadFile(localResult)
	if err != nil {
		return backupCapture{}, err
	}
	var captured backupCapture
	if err := json.Unmarshal(data, &captured); err != nil {
		return backupCapture{}, err
	}
	captured.Name, captured.Path, captured.StartedAt = name, localBackup, startedAt.Format(time.RFC3339Nano)
	verified, err := backupMetadata(localBackup)
	if err != nil {
		return backupCapture{}, err
	}
	if captured.SHA256 != verified.SHA256 || captured.EventSequence != verified.EventSequence || captured.DurableCutoff == "" {
		return backupCapture{}, fmt.Errorf("backup result does not match durable artifact: result=%+v verified=%+v", captured, verified)
	}
	return captured, nil
}

func (h *harness) exerciseSchemaProtocolUpgrade(evidence string) (string, error) {
	fixtureHome := filepath.Join(h.root, "schema-upgrade-home")
	helper, binary := h.self, h.binary
	if h.realHost {
		fixtureHome = filepath.Join(h.remoteRoot, "schema-upgrade-home")
		helper, binary = h.remoteHelper, h.remoteBinary
	}
	runHelper := func(args ...string) ([]byte, error) {
		h.logCommand("schema-fixture@"+h.remoteHost, append([]string{"worklease-remote-smoke"}, args...))
		if h.realHost {
			output, err := runSSH(h.remoteHost, append([]string{helper}, args...)...)
			return []byte(output), err
		}
		return exec.Command(helper, args...).CombinedOutput()
	}
	if output, err := runHelper("schema-v1-fixture", fixtureHome); err != nil {
		return "", fmt.Errorf("create schema-v1 fixture: %w: %s", err, output)
	}
	handlePath := filepath.Join(fixtureHome, "upgrade-handle.json")
	upgradeArgs := []string{"--json", "--home", fixtureHome, "acquire", "--handle", handlePath, "--resource", "coordination:schema-upgrade", "--ttl", "5s"}
	h.logCommand("schema-upgrade@"+h.remoteHost, append([]string{"worklease"}, upgradeArgs...))
	if h.realHost {
		if _, err := h.remoteJSON(binary, upgradeArgs...); err != nil {
			return "", err
		}
	} else if _, err := runJSON(nil, "", binary, upgradeArgs...); err != nil {
		return "", err
	}
	stateOutput, err := runHelper("schema-upgrade-state", fixtureHome)
	if err != nil {
		return "", fmt.Errorf("read upgraded schema fixture: %w: %s", err, stateOutput)
	}
	var state schemaUpgradeState
	if err := json.Unmarshal(stateOutput, &state); err != nil {
		return "", err
	}
	if state.UserVersion != store.SchemaVersion || state.AuthorityID != strings.Repeat("a", 32) || state.RestoreID == "" || state.StartedOperations != 1 || state.CompletedReplay != 1 || state.Events < 2 || state.RequiredV2Objects != 5 || state.LegacyOpenReason != "schema-unsupported" {
		return "", fmt.Errorf("schema upgrade did not preserve the v1 authority and replay/unknown state: %+v", state)
	}
	protocolEvidence, err := h.protocolUpgradeEvidence()
	if err != nil {
		return "", err
	}
	evidencePath := filepath.Join(evidence, "schema-protocol-upgrade.json")
	encoded, err := json.MarshalIndent(map[string]any{
		"schema":      state,
		"protocol":    protocolEvidence,
		"observation": "the current binary migrated a populated schema-v1 authority while preserving completed replay and started unknown state; a legacy schema reader and legacy wire protocol both fail explicitly while worklease-http/1 remains available",
	}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
		return "", err
	}
	return evidencePath, nil
}

func (h *harness) protocolUpgradeEvidence() (map[string]any, error) {
	roots := x509.NewCertPool()
	certificate, err := os.ReadFile(h.cert)
	if err != nil || !roots.AppendCertsFromPEM(certificate) {
		return nil, errors.New("load protocol-upgrade TLS root")
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	requestMetadata := func(version string) (int, map[string]any, error) {
		request, err := http.NewRequest(http.MethodGet, h.endpoint+"/.well-known/worklease", nil)
		if err != nil {
			return 0, nil, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Worklease-Protocol-Version", version)
		response, err := client.Do(request)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		var envelope map[string]any
		if err := json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&envelope); err != nil {
			return response.StatusCode, nil, err
		}
		return response.StatusCode, envelope, nil
	}
	legacyStatus, legacy, err := requestMetadata("worklease-http/0")
	if err != nil {
		return nil, err
	}
	legacyError, _ := legacy["error"].(map[string]any)
	legacyDetails, _ := legacyError["details"].(map[string]any)
	supported, _ := legacyDetails["supportedProtocolVersions"].([]any)
	if legacyStatus != http.StatusUpgradeRequired || legacyError["reason"] != "protocol-version-unsupported" || len(supported) != 1 || supported[0] != "worklease-http/1" {
		return nil, fmt.Errorf("legacy protocol was not rejected with the supported upgrade target: status=%d envelope=%v", legacyStatus, legacy)
	}
	currentStatus, current, err := requestMetadata("worklease-http/1")
	if err != nil {
		return nil, err
	}
	result, _ := current["result"].(map[string]any)
	currentSupported, _ := result["supportedProtocolVersions"].([]any)
	if currentStatus != http.StatusOK || current["protocolVersion"] != "worklease-http/1" || len(currentSupported) != 1 || currentSupported[0] != "worklease-http/1" || current["authorityId"] != h.report.Authority {
		return nil, fmt.Errorf("current protocol metadata is invalid: status=%d envelope=%v", currentStatus, current)
	}
	return map[string]any{"legacyStatus": legacyStatus, "legacyReason": legacyError["reason"], "legacySupportedVersions": supported, "currentStatus": currentStatus, "currentProtocolVersion": current["protocolVersion"], "authorityId": current["authorityId"]}, nil
}

func (h *harness) exerciseRestoredCredentials(evidence string, retained, missing client, retainedID, missingID, restoreID string, selectedCutoffPresence map[string]bool) (string, error) {
	if !selectedCutoffPresence[retainedID] || selectedCutoffPresence[missingID] {
		return "", fmt.Errorf("selected backup installation inventory is invalid: retained=%t missing=%t", selectedCutoffPresence[retainedID], selectedCutoffPresence[missingID])
	}
	if err := h.refreshClientRestoreID(retained, restoreID); err != nil {
		return "", err
	}
	if err := h.refreshClientRestoreID(missing, restoreID); err != nil {
		return "", err
	}
	retainedCLI, err := h.cliFailure(retained, "--profile", "team", "installation", "list")
	if err != nil {
		return "", err
	}
	missingCLI, err := h.cliFailure(missing, "--profile", "team", "installation", "list")
	if err != nil {
		return "", err
	}
	if err := requireReason(retainedCLI, "installation-revoked"); err != nil {
		return "", fmt.Errorf("retained restored credential: %w", err)
	}
	if err := requireReason(missingCLI, "authentication-required"); err != nil {
		return "", fmt.Errorf("credential whose row is missing from backup: %w", err)
	}
	retainedMCP, err := h.mcpToolCall(retained, "list", map[string]any{})
	if err != nil {
		return "", err
	}
	missingMCP, err := h.mcpToolCall(missing, "list", map[string]any{})
	if err != nil {
		return "", err
	}
	for label, check := range map[string]struct {
		response map[string]any
		reason   string
	}{"retained": {retainedMCP, "installation-revoked"}, "missing": {missingMCP, "authentication-required"}} {
		errorFields, _ := check.response["error"].(map[string]any)
		if errorFields["reason"] != check.reason {
			return "", fmt.Errorf("%s restored MCP credential result=%v", label, check.response)
		}
	}
	evidencePath := filepath.Join(evidence, "restored-missing-credentials.json")
	encoded, err := json.MarshalIndent(map[string]any{
		"restoreId":            restoreID,
		"retainedInstallation": map[string]any{"installationId": retainedID, "presentAtCutoff": selectedCutoffPresence[retainedID], "cliReason": "installation-revoked", "mcpReason": "installation-revoked"},
		"missingInstallation":  map[string]any{"installationId": missingID, "presentAtCutoff": selectedCutoffPresence[missingID], "cliReason": "authentication-required", "mcpReason": "authentication-required"},
	}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
		return "", err
	}
	return evidencePath, nil
}

func (h *harness) exerciseRecoveryReopen(evidence, retainedOperationID, installationEvidencePath string) (string, error) {
	client := h.clients[0]
	inspectionResult, err := h.cli(client, "--profile", "team", "op", "inspect", "--operation-id", retainedOperationID, "--full")
	if err != nil {
		return "", err
	}
	inspection, _ := inspectionResult["inspection"].(map[string]any)
	targetClaimID, _ := inspection["claimId"].(string)
	requestHash, _ := inspection["requestSha256"].(string)
	if targetClaimID == "" || len(requestHash) != 64 || inspection["state"] != "started" {
		return "", fmt.Errorf("retained operation inspection is incomplete: %v", inspectionResult)
	}
	lostBeginID := strings.Repeat("1", 32)
	lostInspectionResult, err := h.cli(client, "--profile", "team", "op", "inspect", "--operation-id", lostBeginID, "--full")
	if err != nil {
		return "", err
	}
	lostInspection, _ := lostInspectionResult["inspection"].(map[string]any)
	lostClaimID, _ := lostInspection["claimId"].(string)
	lostRequestHash, _ := lostInspection["requestSha256"].(string)
	if lostClaimID == "" || len(lostRequestHash) != 64 || lostInspection["state"] != "started" {
		return "", fmt.Errorf("lost-begin retained inspection is incomplete: %v", lostInspectionResult)
	}
	explicitLostID := strings.Repeat("8", 32)
	explicitInspectionResult, err := h.cli(client, "--profile", "team", "op", "inspect", "--operation-id", explicitLostID, "--full")
	if err != nil {
		return "", err
	}
	explicitInspection, _ := explicitInspectionResult["inspection"].(map[string]any)
	explicitClaimID, _ := explicitInspection["claimId"].(string)
	explicitRequestHash, _ := explicitInspection["requestSha256"].(string)
	if explicitClaimID == "" || len(explicitRequestHash) != 64 || explicitInspection["state"] != "started" {
		return "", fmt.Errorf("explicit lost-begin retained inspection is incomplete: %v", explicitInspectionResult)
	}
	recoveryHandle := filepath.Join(client.home, "handles", "restore-recovery.json")
	acquired, err := h.cli(client, "--profile", "team", "acquire", "--handle", recoveryHandle, "--resource", "coordination:retained-start-backup", "--resource", "coordination:lost-begin", "--resource", "coordination:explicit-lost-begin", "--ttl", "30s")
	if err != nil {
		return "", err
	}
	unknown, _ := acquired["unknownOperations"].([]any)
	if len(unknown) != 3 || !anySliceContains(unknown, retainedOperationID) || !anySliceContains(unknown, lostBeginID) || !anySliceContains(unknown, explicitLostID) {
		return "", fmt.Errorf("recovery acquire did not return the exact transitive unknown set: %v", acquired)
	}
	recoveryGrant, err := handle.Read(recoveryHandle)
	if err != nil {
		return "", err
	}
	recoveryToken := filepath.Join(h.root, "secrets", "recovery-claim.token")
	if err := os.WriteFile(recoveryToken, []byte(recoveryGrant.Token+"\n"), 0o600); err != nil {
		return "", err
	}
	reconcileID, err := randomRequestID()
	if err != nil {
		return "", err
	}
	reconcileDeadline := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)
	reconcileEvidence := `{"outcome":"observed-success","executorStopped":true,"providerCompletionObserved":true}`
	reconcileArgs := []string{"--profile", "team", "op", "reconcile", "--claim-id", recoveryGrant.ClaimID, "--token-file", recoveryToken, "--revision", strconv.FormatInt(recoveryGrant.Revision, 10), "--operation-id", reconcileID, "--request-not-after", reconcileDeadline, "--target-claim-id", targetClaimID, "--target-operation-id", retainedOperationID, "--expected-request-sha256", requestHash, "--outcome", "observed-success", "--evidence", reconcileEvidence, "--ttl", "30s"}
	reconciled, err := h.cli(client, reconcileArgs...)
	if err != nil {
		return "", err
	}
	reconciledReceipt, _ := reconciled["receipt"].(map[string]any)
	firstRevision, ok := reconciledReceipt["revision"].(float64)
	if !ok {
		return "", fmt.Errorf("first reconciliation omitted its revision: %v", reconciled)
	}
	lostReconcileID, err := randomRequestID()
	if err != nil {
		return "", err
	}
	lostReconciled, err := h.cli(client, "--profile", "team", "op", "reconcile", "--claim-id", recoveryGrant.ClaimID, "--token-file", recoveryToken, "--revision", strconv.FormatInt(int64(firstRevision), 10), "--operation-id", lostReconcileID, "--request-not-after", reconcileDeadline, "--target-claim-id", lostClaimID, "--target-operation-id", lostBeginID, "--expected-request-sha256", lostRequestHash, "--outcome", "observed-failure", "--evidence", `{"outcome":"observed-failure","executorStopped":true,"dispatchCount":0}`, "--ttl", "30s")
	if err != nil {
		return "", err
	}
	lostReceipt, _ := lostReconciled["receipt"].(map[string]any)
	secondRevision, ok := lostReceipt["revision"].(float64)
	if !ok {
		return "", fmt.Errorf("second reconciliation omitted its revision: %v", lostReconciled)
	}
	explicitReconcileID, err := randomRequestID()
	if err != nil {
		return "", err
	}
	explicitReconciled, err := h.cli(client, "--profile", "team", "op", "reconcile", "--claim-id", recoveryGrant.ClaimID, "--token-file", recoveryToken, "--revision", strconv.FormatInt(int64(secondRevision), 10), "--operation-id", explicitReconcileID, "--request-not-after", reconcileDeadline, "--target-claim-id", explicitClaimID, "--target-operation-id", explicitLostID, "--expected-request-sha256", explicitRequestHash, "--outcome", "observed-failure", "--evidence", `{"outcome":"observed-failure","executorStopped":true,"dispatchCount":0,"explicitTokenReplayVerified":true}`, "--ttl", "30s")
	if err != nil {
		return "", err
	}
	explicitReceipt, _ := explicitReconciled["receipt"].(map[string]any)
	currentRevision, ok := explicitReceipt["revision"].(float64)
	if !ok {
		return "", fmt.Errorf("third reconciliation omitted its revision: %v", explicitReconciled)
	}
	for i := range reconcileArgs {
		if reconcileArgs[i] == "--revision" {
			reconcileArgs[i+1] = strconv.FormatInt(int64(currentRevision), 10)
			break
		}
	}
	replayed, err := h.cli(client, reconcileArgs...)
	if err != nil {
		return "", err
	}
	replayedReceipt, _ := replayed["receipt"].(map[string]any)
	if replayedReceipt["idempotent"] != true || replayedReceipt["targetOperationId"] != retainedOperationID {
		return "", fmt.Errorf("retained reconciliation did not replay idempotently: %v", replayed)
	}
	changedReconcile := append([]string(nil), reconcileArgs...)
	for i := range changedReconcile {
		if changedReconcile[i] == reconcileEvidence {
			changedReconcile[i] = `{"outcome":"observed-success","executorStopped":true,"providerCompletionObserved":true,"changed":true}`
		}
	}
	changedResult, err := h.cliFailure(client, changedReconcile...)
	if err != nil {
		return "", err
	}
	if err := requireReason(changedResult, "operation-request-mismatch", "reconciliation-conflict"); err != nil {
		return "", fmt.Errorf("changed retained reconciliation replay: %w", err)
	}
	statusBefore, err := h.cli(client, "--profile", "team", "recovery", "status")
	if err != nil {
		return "", err
	}
	revision, ok := statusBefore["recoveryRevision"].(float64)
	if !ok || statusBefore["recoveryMode"] != true {
		return "", fmt.Errorf("invalid pre-reopen recovery state: %v", statusBefore)
	}
	incompleteAttestation := filepath.Join(evidence, "recovery-attestation-incomplete.json")
	completeAttestation := filepath.Join(evidence, "recovery-attestation-complete.json")
	incomplete := []byte(`{"inventoryComplete":true,"pendingSetsComplete":false,"retainedOutcomesComplete":true,"namespaceCessationEstablished":true,"evidenceReferences":["private://installation-inventory"]}` + "\n")
	complete, err := json.MarshalIndent(map[string]any{"inventoryComplete": true, "pendingSetsComplete": true, "retainedOutcomesComplete": true, "namespaceCessationEstablished": true, "evidenceReferences": []string{installationEvidencePath, filepath.Join(evidence, "pending-evidence-survival.json"), filepath.Join(evidence, "asynchronous-provider-effect.txt"), filepath.Join(evidence, "retained-start-lost-completion.json")}}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(incompleteAttestation, incomplete, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(completeAttestation, append(complete, '\n'), 0o600); err != nil {
		return "", err
	}
	incompleteID, err := randomRequestID()
	if err != nil {
		return "", err
	}
	incompleteResult, err := h.cliFailure(client, "--profile", "team", "recovery", "reopen", "--operation-id", incompleteID, "--expected-recovery-revision", strconv.FormatInt(int64(revision), 10), "--attestation-file", incompleteAttestation)
	if err != nil {
		return "", err
	}
	if err := requireReason(incompleteResult, "recovery-required"); err != nil {
		return "", fmt.Errorf("incomplete recovery evidence: %w", err)
	}
	statusAfterFailure, err := h.cli(client, "--profile", "team", "recovery", "status")
	if err != nil || statusAfterFailure["recoveryMode"] != true || statusAfterFailure["recoveryRevision"] != revision {
		return "", fmt.Errorf("failed reopen partially changed recovery state: status=%v err=%v", statusAfterFailure, err)
	}
	reopenID, err := randomRequestID()
	if err != nil {
		return "", err
	}
	reopenDeadline := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)
	reopenArgs := []string{"--profile", "team", "recovery", "reopen", "--operation-id", reopenID, "--request-not-after", reopenDeadline, "--expected-recovery-revision", strconv.FormatInt(int64(revision), 10), "--attestation-file", completeAttestation}
	reopened, err := h.cli(client, reopenArgs...)
	if err != nil {
		return "", err
	}
	reopenReplay, err := h.cli(client, reopenArgs...)
	if err != nil {
		return "", err
	}
	if reopened["recoveryMode"] != false || reopened["recoveryRevision"] != revision+1 || reopenReplay["idempotent"] != true || reopenReplay["recoveryRevision"] != revision+1 {
		return "", fmt.Errorf("recovery reopen was not atomic and replayable: first=%v replay=%v", reopened, reopenReplay)
	}
	changedAttestation := filepath.Join(evidence, "recovery-attestation-changed.json")
	changed := bytes.Replace(complete, []byte(`"namespaceCessationEstablished": true`), []byte(`"namespaceCessationEstablished": false`), 1)
	if err := os.WriteFile(changedAttestation, append(changed, '\n'), 0o600); err != nil {
		return "", err
	}
	changedReopen := append([]string(nil), reopenArgs...)
	changedReopen[len(changedReopen)-1] = changedAttestation
	changedReopenResult, err := h.cliFailure(client, changedReopen...)
	if err != nil {
		return "", err
	}
	if err := requireReason(changedReopenResult, "operation-request-mismatch"); err != nil {
		return "", fmt.Errorf("changed atomic reopen replay: %w", err)
	}
	statusAfter, err := h.cli(client, "--profile", "team", "recovery", "status")
	if err != nil || statusAfter["recoveryMode"] != false || statusAfter["recoveryRevision"] != revision+1 {
		return "", fmt.Errorf("recovery did not remain atomically open: status=%v err=%v", statusAfter, err)
	}
	postReopenHandle := filepath.Join(client.home, "handles", "post-reopen.json")
	if _, err := h.cli(client, "--profile", "team", "acquire", "--handle", postReopenHandle, "--resource", "coordination:post-reopen", "--ttl", "5s"); err != nil {
		return "", fmt.Errorf("admission did not open after recovery: %w", err)
	}
	if _, err := h.cli(client, "--profile", "team", "release", "--handle", postReopenHandle, "--reason", "atomic reopen verified"); err != nil {
		return "", err
	}
	evidencePath := filepath.Join(evidence, "recovery-reopen.json")
	encoded, err := json.MarshalIndent(map[string]any{"retainedInspection": inspectionResult, "lostBeginInspection": lostInspectionResult, "explicitLostBeginInspection": explicitInspectionResult, "recoveryAcquire": acquired, "reconciliation": reconciled, "reconciliationReplay": replayed, "changedReconciliation": changedResult, "lostBeginReconciliation": lostReconciled, "explicitLostBeginReconciliation": explicitReconciled, "statusBefore": statusBefore, "incompleteReopen": incompleteResult, "statusAfterIncomplete": statusAfterFailure, "reopen": reopened, "reopenReplay": reopenReplay, "changedReopen": changedReopenResult, "statusAfter": statusAfter, "cutoffKnown": false}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
		return "", err
	}
	return evidencePath, nil
}

func (h *harness) exerciseOver32RecoveryClosure(evidence string) (string, error) {
	database := filepath.Join(h.root, "authority", "worklease.db")
	if h.realHost {
		database = filepath.Join(h.remoteRoot, "authority", "worklease.db")
	}
	h.stopServer()
	var fixtureOutput []byte
	var err error
	if h.realHost {
		var output string
		output, err = runSSH(h.remoteHost, h.remoteHelper, "recovery-closure-fixture", database)
		fixtureOutput = []byte(output)
	} else {
		var result map[string]any
		result, err = installRecoveryClosureFixture(database)
		fixtureOutput, _ = json.Marshal(result)
	}
	if err != nil {
		return "", err
	}
	if err := h.restartServer(evidence); err != nil {
		return "", err
	}
	handlePath := filepath.Join(h.clients[0].home, "handles", "over-32-recovery.json")
	failure, err := h.cliFailure(h.clients[0], "--profile", "team", "acquire", "--handle", handlePath, "--resource", "coordination:recovery-closure-00", "--ttl", "5s")
	if err != nil {
		return "", err
	}
	if err := requireReason(failure, "recovery-required"); err != nil {
		return "", err
	}
	var fixture map[string]any
	if err := json.Unmarshal(fixtureOutput, &fixture); err != nil || fixture["resources"] != float64(35) || fixture["operations"] != float64(17) {
		return "", fmt.Errorf("over-32 recovery fixture was not exhaustive: fixture=%v err=%v", fixture, err)
	}
	errorFields, _ := failure["error"].(map[string]any)
	details, _ := errorFields["details"].(map[string]any)
	failedClaimID, _ := details["claimId"].(string)
	claims, err := h.cli(h.clients[0], "--profile", "team", "list", "--full")
	if err != nil {
		return "", err
	}
	if failedClaimID == "" || claimsContain(claims, failedClaimID) {
		return "", fmt.Errorf("over-32 recovery failure partially committed claim %q: %v", failedClaimID, claims)
	}
	evidencePath := filepath.Join(evidence, "recovery-closure-over-32.json")
	encoded, err := json.MarshalIndent(map[string]any{"fixture": fixture, "failure": failure, "requiredResourceCount": 35, "operationCount": 17, "authorityClaimAbsent": true}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
		return "", err
	}
	return evidencePath, nil
}

func (h *harness) group5(evidence string) error {
	authorityDB := filepath.Join(h.root, "authority", "worklease.db")
	if h.realHost {
		authorityDB = filepath.Join(h.remoteRoot, "authority", "worklease.db")
	}
	retainedCredentialClient, retainedInstallationID, err := h.provisionRaceClient("restore-retained-installation")
	if err != nil {
		return err
	}
	ephemeralClient, ephemeralInstallationID, err := h.provisionRaceClient("restore-ephemeral-runner")
	if err != nil {
		return err
	}
	if _, err := h.cli(h.clients[0], "--profile", "team", "installation", "revoke", "--installation-id", ephemeralInstallationID, "--reason", "ephemeral runner retired before backup"); err != nil {
		return err
	}
	ephemeralFailure, err := h.cliFailure(ephemeralClient, "--profile", "team", "list")
	if err != nil {
		return err
	}
	if err := requireReason(ephemeralFailure, "installation-revoked"); err != nil {
		return fmt.Errorf("retired ephemeral installation remained usable: %w", err)
	}
	zeroBefore, nonzeroBefore, err := h.pendingSetInventories()
	if err != nil {
		return err
	}
	olderBackup, err := h.captureAsynchronousBackup(evidence, authorityDB, "older", func() error {
		_, observeErr := h.cli(h.clients[0], "--profile", "team", "events", "--limit", "1")
		return observeErr
	})
	if err != nil {
		return err
	}
	backupTailHandle := filepath.Join(h.clients[0].home, "handles", "backup-tail.json")
	if _, err := h.cli(h.clients[0], "--profile", "team", "acquire", "--handle", backupTailHandle, "--resource", "coordination:backup-tail", "--ttl", "5s"); err != nil {
		return err
	}
	if _, err := h.cli(h.clients[0], "--profile", "team", "release", "--handle", backupTailHandle, "--reason", "separate selected backup cutoff"); err != nil {
		return err
	}
	retainedStartHandle := filepath.Join(h.clients[0].home, "handles", "retained-start-backup.json")
	if _, err := h.cli(h.clients[0], "--profile", "team", "acquire", "--handle", retainedStartHandle, "--resource", "coordination:retained-start-backup", "--ttl", "30s"); err != nil {
		return err
	}
	retainedStartID := strings.Repeat("d", 32)
	retainedStartReady := filepath.Join(evidence, "retained-start.ready")
	retainedStartRelease := filepath.Join(evidence, "retained-start.release")
	retainedStartEffect := filepath.Join(evidence, "retained-start-effect.log")
	retainedStartCommand, retainedStartOutput, err := h.startCLI(h.clients[0], "--profile", "team", "exec", "--handle", retainedStartHandle, "--operation-id", retainedStartID, "--ttl", "30s", "--max-duration", "25s", "--", h.self, "gated-effect", retainedStartReady, retainedStartRelease, retainedStartEffect)
	if err != nil {
		return err
	}
	retainedStartFinished := false
	defer func() {
		if !retainedStartFinished && retainedStartCommand.Process != nil {
			_ = retainedStartCommand.Process.Kill()
			_, _ = waitStartedCommand(retainedStartCommand, time.Second)
		}
	}()
	if err := waitForExactFile(retainedStartReady, "effect ready", 10*time.Second); err != nil {
		return err
	}
	selectedBackup, err := h.captureAsynchronousBackup(evidence, authorityDB, "selected", func() error {
		_, observeErr := h.cli(h.clients[0], "--profile", "team", "op", "inspect", "--handle", retainedStartHandle, "--operation-id", retainedStartID, "--full")
		return observeErr
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(retainedStartRelease, []byte("release effect\n"), 0o600); err != nil {
		return err
	}
	retainedStartResult, err := waitCLISuccess(retainedStartCommand, retainedStartOutput, 10*time.Second)
	retainedStartFinished = true
	if err != nil {
		return err
	}
	retainedStartEffectData, err := os.ReadFile(retainedStartEffect)
	if err != nil || string(retainedStartEffectData) != "dispatch\n" {
		return fmt.Errorf("retained-start effect dispatch count: content=%q err=%v", retainedStartEffectData, err)
	}
	if err := requireOperationPendingCleared(retainedStartHandle, filepath.Join(h.clients[0].home, "pending", "team"), retainedStartID); err != nil {
		return err
	}
	backup := selectedBackup.Path
	selectedOperationState, err := backupOperationState(backup, retainedStartID)
	if err != nil {
		return err
	}
	if selectedOperationState != "started" {
		return fmt.Errorf("selected backup operation state=%q, want started", selectedOperationState)
	}
	missingTailEvidencePath, missingStartID, missingCompletedID, err := h.exerciseMissingTailOperations(evidence, selectedBackup.Path)
	if err != nil {
		return err
	}
	if selectedBackup.EventSequence <= olderBackup.EventSequence || selectedBackup.SHA256 == olderBackup.SHA256 {
		return fmt.Errorf("selected backup did not retain a newer authority tail: older=%+v selected=%+v", olderBackup, selectedBackup)
	}
	zeroSelected, nonzeroSelected, err := h.pendingSetInventories()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(zeroBefore.RequestIDs, zeroSelected.RequestIDs) || !reflect.DeepEqual(nonzeroBefore.RequestIDs, nonzeroSelected.RequestIDs) {
		return errors.New("independently retained pending-set inventories changed across backup selection")
	}
	missingCredentialClient, missingInstallationID, err := h.provisionRaceClient("restore-missing-installation")
	if err != nil {
		return err
	}
	selectedCutoffPresence, err := backupInstallationPresence(selectedBackup.Path, retainedInstallationID, ephemeralInstallationID, missingInstallationID)
	if err != nil {
		return err
	}
	if !selectedCutoffPresence[retainedInstallationID] || !selectedCutoffPresence[ephemeralInstallationID] || selectedCutoffPresence[missingInstallationID] {
		return fmt.Errorf("selected backup does not establish active/retired/missing installation fixtures: %v", selectedCutoffPresence)
	}
	schemaProtocolEvidencePath, err := h.exerciseSchemaProtocolUpgrade(evidence)
	if err != nil {
		return err
	}
	backupEvidencePath := filepath.Join(evidence, "asynchronous-backup-selection.json")
	backupEvidence, err := json.MarshalIndent(map[string]any{
		"fixture": "separate controlled subprocess using SQLite online backup while the authority remains live",
		"older":   olderBackup, "selected": selectedBackup,
		"selection":                   map[string]any{"chosen": selectedBackup.Name, "reason": "newest durable cutoff containing the backup-tail mutation"},
		"pendingSetsAtOlderCutoff":    []pendingSetInventory{zeroBefore, nonzeroBefore},
		"pendingSetsAtSelectedCutoff": []pendingSetInventory{zeroSelected, nonzeroSelected},
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(backupEvidencePath, append(backupEvidence, '\n'), 0o600); err != nil {
		return err
	}
	h.report.BackupCutoff = selectedBackup.DurableCutoff
	h.stopServer()
	restoredInvite := filepath.Join(h.root, "secrets", "restored.invite")
	if h.realHost {
		restoredInvite = filepath.Join(h.remoteRoot, "restored.invite")
	}
	start := time.Now()
	backupRemote := backup
	if h.realHost {
		backupRemote = filepath.Join(h.remoteRoot, "asynchronous-backup-selected.db")
	}
	if err := h.requireRestoreSourceHash(backupRemote, selectedBackup.SHA256); err != nil {
		return fmt.Errorf("first restore source: %w", err)
	}
	restoreArgs := []string{"--json", "hosted", "restore", "--home", authorityDBRoot(h), "--from", backupRemote, "--selected-cutoff", selectedBackup.DurableCutoff, "--loss-interval-start", selectedBackup.DurableCutoff, "--loss-interval-end", time.Now().UTC().Format(time.RFC3339Nano), "--bootstrap-invite-file", restoredInvite}
	h.logCommand("authority@"+h.remoteHost, append([]string{"worklease"}, restoreArgs...))
	var restoreResult map[string]any
	if h.realHost {
		restoreResult, err = h.remoteJSON(h.remoteBinary, restoreArgs...)
	} else {
		restoreResult, err = runJSON(nil, "", h.binary, restoreArgs...)
	}
	if err != nil {
		return err
	}
	firstRestoreID, _ := restoreResult["restoreId"].(string)
	if restoreResult["authorityId"] != h.report.Authority || firstRestoreID == "" || firstRestoreID == h.immutableEnrollmentBefore.ExpectedRestoreID {
		return fmt.Errorf("restore identity did not rotate on the same authority: authority=%q restore=%q", restoreResult["authorityId"], firstRestoreID)
	}
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	firstRestoreInvite := restoredInvite
	if h.realHost {
		firstRestoreInvite = filepath.Join(h.root, "secrets", "restored-first.invite")
		if err := runSCP(h.remoteHost, h.remoteHost+":"+restoredInvite, firstRestoreInvite); err != nil {
			return err
		}
	}
	firstRestoreClient, err := h.enrollRestoreValidationClient("first-restore-validation", firstRestoreInvite, firstRestoreID)
	if err != nil {
		return err
	}
	firstRecoveryStatus, err := h.cli(firstRestoreClient, "--profile", "team", "recovery", "status")
	if err != nil {
		return err
	}
	if err := requireRetainedRecovery(firstRecoveryStatus, retainedStartID, selectedBackup.DurableCutoff); err != nil {
		return fmt.Errorf("first restore validation: %w", err)
	}
	h.stopServer()
	if err := h.requireRestoreSourceHash(backupRemote, selectedBackup.SHA256); err != nil {
		return fmt.Errorf("second restore source: %w", err)
	}
	secondRestoredInvite := strings.TrimSuffix(restoredInvite, ".invite") + "-second.invite"
	secondRestoreArgs := []string{"--json", "hosted", "restore", "--home", authorityDBRoot(h), "--from", backupRemote, "--cutoff-unknown", "--loss-interval-start", selectedBackup.DurableCutoff, "--loss-interval-end", time.Now().UTC().Format(time.RFC3339Nano), "--bootstrap-invite-file", secondRestoredInvite}
	h.logCommand("authority@"+h.remoteHost, append([]string{"worklease"}, secondRestoreArgs...))
	var secondRestoreResult map[string]any
	if h.realHost {
		secondRestoreResult, err = h.remoteJSON(h.remoteBinary, secondRestoreArgs...)
	} else {
		secondRestoreResult, err = runJSON(nil, "", h.binary, secondRestoreArgs...)
	}
	if err != nil {
		return fmt.Errorf("second restore of selected backup: %w", err)
	}
	restoredAuthorityID, _ := secondRestoreResult["authorityId"].(string)
	restoredRestoreID, _ := secondRestoreResult["restoreId"].(string)
	if restoredAuthorityID != h.report.Authority || restoredRestoreID == "" || restoredRestoreID == firstRestoreID || restoredRestoreID == h.immutableEnrollmentBefore.ExpectedRestoreID {
		return fmt.Errorf("second restore identity is not fresh on the same authority: authority=%q first=%q second=%q", restoredAuthorityID, firstRestoreID, restoredRestoreID)
	}
	restoredInvite = secondRestoredInvite
	h.report.RestoreTime = time.Since(start).String()
	localMutation := []string{"--json", "--home", authorityDBRoot(h), "--local", "acquire", "--handle", filepath.Join(authorityDBRoot(h), "forbidden-local-handle.json"), "--resource", "coordination:forbidden-local", "--ttl", "1s"}
	if err := h.requireAuthorityFailure(localMutation, "hosted-home-requires-remote"); err != nil {
		return err
	}
	reissuedInvite := strings.TrimSuffix(restoredInvite, ".invite") + "-reissued.invite"
	reissueArgs := []string{"--json", "hosted", "bootstrap-reissue", "--home", authorityDBRoot(h), "--bootstrap-invite-file", reissuedInvite}
	h.logCommand("authority@"+h.remoteHost, append([]string{"worklease"}, reissueArgs...))
	if h.realHost {
		if _, err := h.remoteJSON(h.remoteBinary, reissueArgs...); err != nil {
			return err
		}
	} else if _, err := runJSON(nil, "", h.binary, reissueArgs...); err != nil {
		return err
	}
	if err := h.restartServer(evidence); err != nil {
		return err
	}
	credentialEvidencePath, err := h.exerciseRestoredCredentials(evidence, retainedCredentialClient, missingCredentialClient, retainedInstallationID, missingInstallationID, restoredRestoreID, selectedCutoffPresence)
	if err != nil {
		return err
	}
	if h.preRestoreCursor == "" {
		return errors.New("pre-restore cursor fixture is missing")
	}
	staleCursorClient, err := h.cliFailure(h.clients[0], "--profile", "team", "events", "--cursor", h.preRestoreCursor)
	if err != nil {
		return err
	}
	if err := requireReason(staleCursorClient, "authority-restored"); err != nil {
		return fmt.Errorf("stale profile and cursor after restore: %w", err)
	}
	if err := h.refreshClientRestoreID(h.clients[0], restoredRestoreID); err != nil {
		return err
	}
	clientRestoreInvite := reissuedInvite
	oldRestoreInvite := restoredInvite
	if h.realHost {
		clientRestoreInvite = filepath.Join(h.root, "secrets", "restored-client-reissued.invite")
		oldRestoreInvite = filepath.Join(h.root, "secrets", "restored-client-revoked.invite")
		if err := runSCP(h.remoteHost, h.remoteHost+":"+reissuedInvite, clientRestoreInvite); err != nil {
			return err
		}
		if err := runSCP(h.remoteHost, h.remoteHost+":"+restoredInvite, oldRestoreInvite); err != nil {
			return err
		}
	}
	clientCredential := filepath.Join(h.clients[0].config, "worklease", "credentials", "team")
	if err := os.Rename(clientCredential, clientCredential+".pre-restore"); err != nil {
		return err
	}
	oldInviteResult, err := h.cliFailure(h.clients[0], "enroll", "--profile", "team", "--invite-file", oldRestoreInvite, "--label", "acceptance-revoked-bootstrap")
	if err != nil {
		return err
	}
	if err := requireReason(oldInviteResult, "invite-invalid", "invite-used"); err != nil {
		return fmt.Errorf("superseded bootstrap invite remained redeemable: %w", err)
	}
	if _, err := h.cli(h.clients[0], "enroll", "--profile", "team", "--invite-file", clientRestoreInvite, "--label", "acceptance-restored-cursor"); err != nil {
		return err
	}
	installationInventory, err := h.cli(h.clients[0], "--profile", "team", "installation", "list", "--include-revoked")
	if err != nil {
		return fmt.Errorf("new post-restore credential is unusable: %w", err)
	}
	if !installationIDExists(installationInventory, retainedInstallationID) || !installationIDExists(installationInventory, ephemeralInstallationID) || installationIDExists(installationInventory, missingInstallationID) || !installationRevoked(installationInventory, retainedInstallationID) || !installationRevoked(installationInventory, ephemeralInstallationID) || !installationLabelExists(installationInventory, "acceptance-restored-cursor") {
		return fmt.Errorf("restored installation inventory is incomplete: %v", installationInventory)
	}
	installationEvidencePath := filepath.Join(evidence, "installation-inventory.json")
	installationEvidence, err := json.MarshalIndent(map[string]any{"inventory": installationInventory, "activeRetainedAtCutoff": retainedInstallationID, "retiredEphemeralAtCutoff": ephemeralInstallationID, "missingAfterCutoff": missingInstallationID, "pendingSets": []pendingSetInventory{zeroSelected, nonzeroSelected}}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(installationEvidencePath, append(installationEvidence, '\n'), 0o600); err != nil {
		return err
	}
	recoveryStatus, err := h.cli(h.clients[0], "--profile", "team", "recovery", "status")
	if err != nil {
		return err
	}
	if err := requireUnknownCutoffRetainedRecovery(recoveryStatus, retainedStartID); err != nil {
		return fmt.Errorf("second restore unknown-cutoff validation: %w", err)
	}
	if err := requireRecoveryOmits(recoveryStatus, missingStartID, missingCompletedID); err != nil {
		return err
	}
	doubleRestoreEvidencePath := filepath.Join(evidence, "double-restore.json")
	doubleRestoreEvidence, err := json.MarshalIndent(map[string]any{"authorityId": restoredAuthorityID, "sourceBackup": backupRemote, "sourceSha256": selectedBackup.SHA256, "selectedCutoff": selectedBackup.DurableCutoff, "firstRestoreId": firstRestoreID, "firstRecoveryStatus": firstRecoveryStatus, "secondRestoreId": restoredRestoreID, "secondRecoveryStatus": recoveryStatus, "restoreIdsDistinct": true}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(doubleRestoreEvidencePath, append(doubleRestoreEvidence, '\n'), 0o600); err != nil {
		return err
	}
	retainedStartEvidencePath := filepath.Join(evidence, "retained-start-lost-completion.json")
	retainedStartEvidence, err := json.MarshalIndent(map[string]any{"operationId": retainedStartID, "selectedBackupState": selectedOperationState, "liveCompletion": retainedStartResult, "effectDispatchCount": 1, "clientPendingClearedBeforeRestore": true, "firstRestoreRecoveryStatus": firstRecoveryStatus, "secondRestoreRecoveryStatus": recoveryStatus}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(retainedStartEvidencePath, append(retainedStartEvidence, '\n'), 0o600); err != nil {
		return err
	}
	staleCursor, err := h.cliFailure(h.clients[0], "--profile", "team", "events", "--cursor", h.preRestoreCursor)
	if err != nil {
		return err
	}
	if err := requireReason(staleCursor, "authority-restored"); err != nil {
		return fmt.Errorf("old events cursor under refreshed restore identity: %w", err)
	}
	staleWatchCursor, err := h.cliFailure(h.clients[0], "--profile", "team", "watch", "--cursor", h.preRestoreCursor, "--timeout", "200ms")
	if err != nil {
		return err
	}
	if err := requireReason(staleWatchCursor, "authority-restored"); err != nil {
		return fmt.Errorf("old watch cursor under refreshed restore identity: %w", err)
	}
	cursorEvidencePath := filepath.Join(evidence, "cursor-incarnation-after-restore.json")
	cursorEvidence, err := json.MarshalIndent(map[string]any{"oldProfile": staleCursorClient, "refreshedProfileEvents": staleCursor, "refreshedProfileWatch": staleWatchCursor, "newRestoreId": restoredRestoreID}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(cursorEvidencePath, append(cursorEvidence, '\n'), 0o600); err != nil {
		return err
	}
	if h.immutableEnrollmentID == "" {
		return errors.New("immutable enrollment fixture is missing")
	}
	if err := h.refreshClientRestoreID(h.immutableEnrollmentClient, restoredRestoreID); err != nil {
		return err
	}
	pendingPath := filepath.Join(h.immutableEnrollmentClient.home, "pending", "team", h.immutableEnrollmentID+".json")
	pendingAfterRefresh, err := os.ReadFile(pendingPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(h.pendingSurvivalBefore, pendingAfterRefresh) {
		return errors.New("profile restore refresh changed retained client pending evidence")
	}
	replayOutput, err := h.replayPendingFailure(h.immutableEnrollmentClient, h.immutableEnrollmentID, h.immutableEnrollmentInvite)
	if err != nil {
		return err
	}
	if invite, readErr := os.ReadFile(h.immutableEnrollmentInvite); readErr != nil {
		return readErr
	} else if bytes.Contains(replayOutput, bytes.TrimSpace(invite)) {
		return errors.New("restored enrollment replay disclosed its invite")
	}
	if !bytes.Contains(replayOutput, []byte("authority-restored")) {
		return errors.New("restored enrollment replay did not report authority-restored")
	}
	afterPending, err := authority.NewFilePendingStore(filepath.Join(h.immutableEnrollmentClient.home, "pending", "team")).Load(h.immutableEnrollmentID)
	if err != nil {
		return err
	}
	beforeData, _ := json.Marshal(h.immutableEnrollmentBefore)
	afterData, _ := json.Marshal(afterPending)
	if !bytes.Equal(beforeData, afterData) {
		return errors.New("restored enrollment replay mutated the retained request incarnation")
	}
	immutableEvidence := fmt.Sprintf("request-id=%s\nauthority-id=%s\noriginal-restore-id=%s\nnew-restore-id=%s\ninitial-dispatch-committed-and-response-dropped=true\nreplay-after-restore=authority-restored\nretained-request-sha256=%s\npending-record-unchanged=true\nprofile-refresh-pending-record-unchanged=true\n", h.immutableEnrollmentID, restoredAuthorityID, afterPending.ExpectedRestoreID, restoredRestoreID, afterPending.RequestSHA256)
	immutableEvidencePath := filepath.Join(evidence, "immutable-enrollment-after-restore.txt")
	if err := os.WriteFile(immutableEvidencePath, []byte(immutableEvidence), 0o600); err != nil {
		return err
	}
	pendingHash := sha256.Sum256(h.pendingSurvivalBefore)
	pendingSurvival, err := json.MarshalIndent(map[string]any{
		"requestId": h.immutableEnrollmentID, "requestSha256": afterPending.RequestSHA256,
		"recordSha256": hex.EncodeToString(pendingHash[:]), "survivedAge": true,
		"replayBeforeRestoreReason": "replay-expired", "survivedReplayExpiryAndGC": true,
		"survivedAuthorityRestarts": true, "survivedProfileRefresh": true,
		"retainedOriginalRestoreId": afterPending.ExpectedRestoreID,
		"currentProfileRestoreId":   restoredRestoreID, "replayReason": "authority-restored",
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(evidence, "pending-evidence-survival.json"), append(pendingSurvival, '\n'), 0o600); err != nil {
		return err
	}
	if err := h.requireServerStartFailure("hosted-lock-held"); err != nil {
		return err
	}
	lockChecks := [][]string{
		{"--json", "hosted", "bootstrap-reissue", "--home", authorityDBRoot(h), "--bootstrap-invite-file", filepath.Join(authorityDBRoot(h), "lock-check-bootstrap.invite")},
		{"--json", "hosted", "retire", "--home", authorityDBRoot(h)},
	}
	for _, args := range lockChecks {
		if err := h.requireAuthorityFailure(args, "hosted-lock-held"); err != nil {
			return err
		}
	}
	afterRestoreHandle := filepath.Join(h.clients[1].home, "handles", "after-restore.json")
	staleClient, err := h.cliFailure(h.clients[1], "--profile", "team", "acquire", "--handle", afterRestoreHandle, "--resource", "coordination:after-restore")
	if err != nil {
		return err
	}
	if err := requireReason(staleClient, "authority-restored", "installation-revoked", "authentication-required"); err != nil {
		return err
	}
	providerCompletedPath := filepath.Join(evidence, "provider-completed.log")
	providerCompleted, err := os.ReadFile(providerCompletedPath)
	if err != nil || strings.Count(strings.TrimSpace(string(providerCompleted)), "provider-completed ") != 1 {
		return fmt.Errorf("provider completion evidence is not exact: content=%q err=%v", providerCompleted, err)
	}
	providerRecoveryEvidencePath := filepath.Join(evidence, "provider-effect-recovery.json")
	providerRecoveryEvidence, err := json.MarshalIndent(map[string]any{"terminalReceiptEvidence": filepath.Join(evidence, "asynchronous-provider-effect.txt"), "providerCompletionEvidence": providerCompletedPath, "completionCount": 1, "includedInRetainedOutcomeInventory": true, "namespaceCessationEstablished": true}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(providerRecoveryEvidencePath, append(providerRecoveryEvidence, '\n'), 0o600); err != nil {
		return err
	}
	bootstrapEvidencePath := filepath.Join(evidence, "bootstrap-reissue.json")
	bootstrapEvidence, err := json.MarshalIndent(map[string]any{"supersededInvite": oldRestoreInvite, "reissuedInvite": clientRestoreInvite, "supersededInviteReason": "invite-invalid-or-used", "reissuedInviteRedeemed": true, "restoreId": restoredRestoreID}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(bootstrapEvidencePath, append(bootstrapEvidence, '\n'), 0o600); err != nil {
		return err
	}
	reopenEvidencePath, err := h.exerciseRecoveryReopen(evidence, retainedStartID, installationEvidencePath)
	if err != nil {
		return err
	}
	closureEvidencePath, err := h.exerciseOver32RecoveryClosure(evidence)
	if err != nil {
		return err
	}
	h.report.Groups = append(h.report.Groups, groupEvidence{
		Group:       5,
		Observation: "the asynchronous backup and restore matrix proves chosen/older cutoffs, missing and retained work, provider completion after terminal receipt, complete active/retired/ephemeral installation and pending inventories, explicit unknown-cutoff recovery, exact retained reconciliation replay, evidence-gated atomic reopen, bootstrap reissue, and an atomic over-32 transitive closure refusal; hosted lock and direct-local bypasses remain refused",
		Commands:    []string{"capture older and selected asynchronous backups", "retain started work and omit post-cutoff work", "inventory provider effects, installations, and pending sets", "restore the selected artifact with known then unknown cutoff metadata", "reissue and redeem the current bootstrap invite", "reconcile and exactly replay retained work", "reject incomplete reopen evidence", "atomically reopen and exactly replay the request", "inject and refuse a 35-resource transitive recovery closure", "refuse hosted lock and direct local bypasses"},
		Evidence:    []string{backup, backupEvidencePath, schemaProtocolEvidencePath, credentialEvidencePath, doubleRestoreEvidencePath, retainedStartEvidencePath, missingTailEvidencePath, cursorEvidencePath, immutableEvidencePath, providerRecoveryEvidencePath, installationEvidencePath, bootstrapEvidencePath, reopenEvidencePath, closureEvidencePath, "selected-cutoff=" + h.report.BackupCutoff, "restore-time=" + h.report.RestoreTime},
		Passed:      true,
	})
	return nil
}

func (h *harness) exerciseMissingTailOperations(evidence, selectedBackup string) (string, string, string, error) {
	client := h.clients[0]
	missingStartID := strings.Repeat("e", 32)
	missingStartHandle := filepath.Join(client.home, "handles", "missing-tail-start.json")
	if _, err := h.cli(client, "--profile", "team", "acquire", "--handle", missingStartHandle, "--resource", "coordination:missing-tail-start", "--ttl", "30s"); err != nil {
		return "", "", "", err
	}
	if err := h.startFaultProxy(evidence); err != nil {
		return "", "", "", err
	}
	if err := h.switchProfileEndpoint(client, h.faultEndpoint); err != nil {
		h.stopFaultProxy()
		return "", "", "", err
	}
	proxyActive := true
	defer func() {
		if proxyActive {
			_ = h.switchProfileEndpoint(client, h.endpoint)
			h.stopFaultProxy()
		}
	}()
	missingStartEffect := filepath.Join(evidence, "missing-tail-start-effect.log")
	if err := h.armFault("/v1/operations/begin"); err != nil {
		return "", "", "", err
	}
	missingStartResult, err := h.cliFailure(client, "--profile", "team", "exec", "--handle", missingStartHandle, "--operation-id", missingStartID, "--ttl", "30s", "--", h.self, "effect", missingStartEffect)
	if err != nil {
		return "", "", "", err
	}
	if err := requireReason(missingStartResult, "unknown-outcome"); err != nil {
		return "", "", "", err
	}
	if _, err := os.Stat(missingStartEffect); !errors.Is(err, os.ErrNotExist) {
		return "", "", "", fmt.Errorf("missing-tail start dispatched despite lost acknowledgment: %v", err)
	}
	missingStartInspection, err := h.cli(client, "--profile", "team", "op", "inspect", "--handle", missingStartHandle, "--operation-id", missingStartID, "--full")
	if err != nil {
		return "", "", "", err
	}
	inspection, _ := missingStartInspection["inspection"].(map[string]any)
	if inspection["state"] != "started" || inspection["operationId"] != missingStartID {
		return "", "", "", fmt.Errorf("post-cutoff missing start was not confirmed: %v", missingStartInspection)
	}
	storedHandle, err := handle.Read(missingStartHandle)
	if err != nil {
		return "", "", "", err
	}
	if storedHandle.PendingRequest == nil || storedHandle.PendingRequest.OperationID != missingStartID {
		return "", "", "", fmt.Errorf("post-cutoff missing start has no incomplete client pending evidence: %+v", storedHandle.PendingRequest)
	}
	if err := h.switchProfileEndpoint(client, h.endpoint); err != nil {
		return "", "", "", err
	}
	h.stopFaultProxy()
	proxyActive = false

	missingCompletedID := strings.Repeat("f", 32)
	missingCompletedHandle := filepath.Join(client.home, "handles", "missing-tail-completed.json")
	if _, err := h.cli(client, "--profile", "team", "acquire", "--handle", missingCompletedHandle, "--resource", "coordination:missing-tail-completed", "--ttl", "30s"); err != nil {
		return "", "", "", err
	}
	missingCompletedEffect := filepath.Join(evidence, "missing-tail-completed-effect.log")
	missingCompletedResult, err := h.cli(client, "--profile", "team", "exec", "--handle", missingCompletedHandle, "--operation-id", missingCompletedID, "--ttl", "30s", "--", h.self, "effect", missingCompletedEffect)
	if err != nil {
		return "", "", "", err
	}
	completedEffect, err := os.ReadFile(missingCompletedEffect)
	if err != nil || string(completedEffect) != "dispatch\n" {
		return "", "", "", fmt.Errorf("fully missing completed effect dispatch count: content=%q err=%v", completedEffect, err)
	}
	if err := requireOperationPendingCleared(missingCompletedHandle, filepath.Join(client.home, "pending", "team"), missingCompletedID); err != nil {
		return "", "", "", err
	}
	missingCompletedInspection, err := h.cli(client, "--profile", "team", "op", "inspect", "--handle", missingCompletedHandle, "--operation-id", missingCompletedID, "--full")
	if err != nil {
		return "", "", "", err
	}
	completedInspection, _ := missingCompletedInspection["inspection"].(map[string]any)
	if completedInspection["state"] != "completed" || completedInspection["operationId"] != missingCompletedID {
		return "", "", "", fmt.Errorf("post-cutoff completed operation was not confirmed: %v", missingCompletedInspection)
	}
	presence, err := backupOperationPresence(selectedBackup, missingStartID, missingCompletedID)
	if err != nil {
		return "", "", "", err
	}
	if presence[missingStartID] || presence[missingCompletedID] {
		return "", "", "", fmt.Errorf("post-cutoff operations unexpectedly exist in selected backup: %v", presence)
	}
	h.report.EffectDispatchCounts[filepath.Base(missingStartEffect)] = 0
	h.report.EffectDispatchCounts[filepath.Base(missingCompletedEffect)] = 1
	evidencePath := filepath.Join(evidence, "missing-tail-operations.json")
	evidenceRecord := map[string]any{
		"selectedBackup":                  selectedBackup,
		"confirmedStartMissingFromBackup": map[string]any{"operationId": missingStartID, "liveInspection": missingStartInspection, "selectedBackupPresent": false, "clientPendingKind": storedHandle.PendingRequest.Kind, "clientIncomplete": true, "effectDispatchCount": 0},
		"fullyMissingCompletedWork":       map[string]any{"operationId": missingCompletedID, "liveInspection": missingCompletedInspection, "liveCompletion": missingCompletedResult, "selectedBackupPresent": false, "clientPendingCleared": true, "effectDispatchCount": 1},
	}
	encoded, err := json.MarshalIndent(evidenceRecord, "", "  ")
	if err != nil {
		return "", "", "", err
	}
	if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
		return "", "", "", err
	}
	return evidencePath, missingStartID, missingCompletedID, nil
}

func (h *harness) requireRestoreSourceHash(path, expected string) error {
	var observed string
	if h.realHost {
		output, err := runSSH(h.remoteHost, h.remoteHelper, "file-sha256", path)
		if err != nil {
			return err
		}
		observed = strings.TrimSpace(output)
	} else {
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(contents)
		observed = hex.EncodeToString(digest[:])
	}
	if observed != expected {
		return fmt.Errorf("restore source %s sha256=%s, want %s", path, observed, expected)
	}
	return nil
}

func (h *harness) enrollRestoreValidationClient(name, invite, restoreID string) (client, error) {
	c := client{name: name, home: filepath.Join(h.root, name, "home"), config: filepath.Join(h.root, name, "config"), checkout: filepath.Join(h.root, name, "checkout")}
	if err := os.MkdirAll(c.checkout, 0o700); err != nil {
		return client{}, err
	}
	c.env = append(os.Environ(), "WORKLEASE_HOME="+c.home, "XDG_CONFIG_HOME="+c.config, "SSL_CERT_FILE="+h.cert, "WORKLEASE_AGENT_ID="+name, "WORKLEASE_SESSION_ID="+name+"-session")
	profileResult, err := h.cli(c, "profile", "add", "team", "--endpoint", h.endpoint, "--authority-id", h.report.Authority)
	if err != nil {
		return client{}, err
	}
	profile, _ := profileResult["profile"].(map[string]any)
	if profile["restoreId"] != restoreID {
		return client{}, fmt.Errorf("%s profile restore ID=%v, want %s", name, profile["restoreId"], restoreID)
	}
	if _, err := h.cli(c, "enroll", "--profile", "team", "--invite-file", invite, "--label", name); err != nil {
		return client{}, err
	}
	return c, nil
}

func requireUnknownCutoffRetainedRecovery(status map[string]any, operationID string) error {
	unresolved, _ := status["unresolvedOperations"].([]any)
	retained := false
	for _, raw := range unresolved {
		if raw == operationID {
			retained = true
		}
	}
	if status["recoveryMode"] != true || status["cutoffKnown"] != false || status["selectedDurableCutoff"] != nil || !retained {
		return fmt.Errorf("recovery status did not retain %s with an explicitly unknown cutoff: %v", operationID, status)
	}
	return nil
}

func requireRetainedRecovery(status map[string]any, operationID, selectedCutoff string) error {
	unresolved, _ := status["unresolvedOperations"].([]any)
	retained := false
	for _, operation := range unresolved {
		if operation == operationID {
			retained = true
		}
	}
	observedCutoff, cutoffErr := time.Parse(time.RFC3339Nano, fmt.Sprint(status["selectedDurableCutoff"]))
	expectedCutoff, expectedErr := time.Parse(time.RFC3339Nano, selectedCutoff)
	if status["recoveryMode"] != true || status["cutoffKnown"] != true || !retained || cutoffErr != nil || expectedErr != nil || !observedCutoff.Equal(expectedCutoff) {
		return fmt.Errorf("recovery status did not retain started operation %s at cutoff %s: %v", operationID, selectedCutoff, status)
	}
	return nil
}

func requireRecoveryOmits(status map[string]any, operationIDs ...string) error {
	unresolved, _ := status["unresolvedOperations"].([]any)
	for _, operationID := range operationIDs {
		for _, operation := range unresolved {
			if operation == operationID {
				return fmt.Errorf("post-cutoff operation %s unexpectedly exists in restored recovery inventory: %v", operationID, status)
			}
		}
	}
	return nil
}

func (h *harness) refreshClientRestoreID(c client, restoreID string) error {
	if c.remote {
		if _, err := h.cli(c, "profile", "remove", "team"); err != nil {
			return err
		}
		result, err := h.cli(c, "profile", "add", "team", "--endpoint", h.endpoint, "--authority-id", h.report.Authority)
		if err != nil {
			return err
		}
		profile, _ := result["profile"].(map[string]any)
		if profile["restoreId"] != restoreID {
			return fmt.Errorf("remote profile refresh returned restore ID %v, want %s", profile["restoreId"], restoreID)
		}
		return nil
	}
	paths := config.UserProfilePaths(func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return c.config
		}
		return ""
	})
	profiles, defaultName, err := config.LoadProfiles(paths)
	if err != nil {
		return err
	}
	profile, ok := profiles["team"]
	if !ok {
		return errors.New("team profile is missing")
	}
	profile.RestoreID = restoreID
	profiles["team"] = profile
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]config.Profile, 0, len(names))
	for _, name := range names {
		values = append(values, profiles[name])
	}
	return config.SaveProfiles(paths, values, defaultName)
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
	remoteHomeOutput, err := runSSH(h.remoteHost, "printenv", "HOME")
	remoteHome := strings.TrimSpace(remoteHomeOutput)
	if err != nil || !filepath.IsAbs(remoteHome) || strings.ContainsAny(remoteHome, "\r\n") {
		return []supportingTestEvidence{{Command: "resolve remote test home", Scope: "remote setup", Status: "failed", Log: fmt.Sprint(err)}}
	}
	remoteTestTemp := filepath.Join(remoteHome, ".worklease-acceptance-test-"+filepath.Base(h.remoteRoot))
	if _, err := runSSH(h.remoteHost, "mkdir", "-p", remoteTestTemp); err != nil {
		return []supportingTestEvidence{{Command: "create remote private test temp", Scope: "remote setup", Status: "failed", Log: err.Error()}}
	}
	if _, err := runSSH(h.remoteHost, "chmod", "700", remoteTestTemp); err != nil {
		return []supportingTestEvidence{{Command: "secure remote private test temp", Scope: "remote setup", Status: "failed", Log: err.Error()}}
	}
	type remotePackage struct {
		path, filter string
	}
	packages := []remotePackage{{path: "./internal/gc"}, {path: "./internal/handle"}, {path: "./internal/mcp", filter: "TestRemoteMCPRoutesExistingAuthorityTools"}, {path: "./internal/store", filter: "TestDriver"}}
	results := make([]supportingTestEvidence, 0, len(packages))
	for index, packageInfo := range packages {
		name := fmt.Sprintf("supporting-test-%d", index)
		packagePath := packageInfo.path
		localBinary := filepath.Join(h.root, name)
		command := []string{"go", "test", "-c", "-o", localBinary, packagePath}
		buildCommand := exec.Command(command[0], command[1:]...)
		buildCommand.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+h.remoteGOOS, "GOARCH="+h.remoteGOARCH)
		buildOutput, buildErr := buildCommand.CombinedOutput()
		logPath := filepath.Join(evidence, name+"-build.log")
		_ = os.WriteFile(logPath, buildOutput, 0o600)
		if buildErr != nil {
			results = append(results, supportingTestEvidence{Command: strings.Join(command, " "), Scope: "remote build", Status: "failed", Log: logPath})
			continue
		}
		remoteBinary := filepath.Join(h.remoteRoot, name)
		if copyErr := runSCP(h.remoteHost, localBinary, remoteBinary); copyErr != nil {
			results = append(results, supportingTestEvidence{Command: strings.Join(command, " "), Scope: "remote copy", Status: "failed", Log: logPath})
			continue
		}
		runArgs := []string{"-test.v"}
		if packageInfo.filter != "" {
			runArgs = append(runArgs, "-test.run", packageInfo.filter)
		}
		remoteArgs := []string{"sh", "-c", `cd "$1" && shift && exec "$@"`, "worklease-test", h.clients[1].checkout, "env", "TMPDIR=" + remoteTestTemp, remoteBinary}
		remoteArgs = append(remoteArgs, runArgs...)
		h.logCommand("supporting-test@"+h.remoteHost, append([]string{"ssh", h.remoteHost}, remoteArgs...))
		output, runErr := runSSH(h.remoteHost, remoteArgs...)
		remoteLog := filepath.Join(evidence, name+"-remote.log")
		_ = os.WriteFile(remoteLog, []byte(output), 0o600)
		status := "supporting-test-pass"
		if runErr != nil {
			status = "failed"
		}
		results = append(results, supportingTestEvidence{Command: strings.Join(append([]string{"ssh", h.remoteHost}, remoteArgs...), " "), Scope: "remote", Status: status, Log: remoteLog})
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
	live := func(id, clause string, groups ...int) coverageEntry {
		var evidence []string
		for _, group := range groups {
			evidence = append(evidence, liveEvidence(group)...)
		}
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
		live("AC3.3", "raw and misconfigured reserved-prefix rejection", 1),
		live("AC3.4", "configuration restart behavior", 1),
		live("AC3.5", "persisted admission limits on every extension path", 1),
		live("AC4.1", "exact replay after lost start, renewal, and completion responses", 2),
		supported("AC4.2", "coexistence of request-scoped recovery records with original guarded-effect evidence"),
		live("AC4.3", "no local fallback during partition", 2),
		live("AC4.4", "race ordering for revocation and policy changes", 2),
		live("AC4.5", "fresh response identity and time", 2),
		live("AC4.6", "clock-bound edge cases", 2),
		live("AC4.7", "pre-dispatch persistence failure", 2),
		live("AC4.8", "late acknowledgment without redispatch", 2),
		live("AC4.9", "asynchronous provider effect continuing after terminal completion", 2),
		live("AC5.1", "bootstrap crash ordering and redaction", 3),
		live("AC5.2", "hidden, file, and descriptor invite input", 3),
		live("AC5.3", "dropped invite and redemption responses", 3),
		live("AC5.4", "immutable request incarnation", 5),
		live("AC5.5", "no-burn mismatch", 3),
		live("AC5.6", "role isolation", 3),
		live("AC5.7", "credential rotation", 3),
		live("AC5.8", "credential revocation", 3),
		live("AC5.9", "distinct MCP authentication guidance", 3),
		live("AC6.1", "snapshot/watch races", 4),
		live("AC6.2", "disconnect and reconnect", 4),
		live("AC6.3", "cursor incarnation and retention gaps", 4, 5),
		live("AC6.4", "stuck-history retention", 4),
		live("AC6.5", "full-volume storage-failure without pruning", 4),
		live("AC6.6", "pending evidence surviving age, GC, replay expiry, restart, and profile changes", 4, 5),
		live("AC7.1", "actual asynchronous backup fixture with chosen and older cutoffs", 5),
		live("AC7.2", "online SQLite backup on the authority host", 5),
		live("AC7.3", "zero and nonzero pending sets", 5),
		live("AC7.4", "authority restart", 5),
		live("AC7.5", "schema and protocol upgrade", 5),
		live("AC7.6", "restored and missing credentials", 5),
		live("AC7.7", "double restore", 5),
		live("AC7.8", "retained start with lost completion", 5),
		live("AC7.9", "confirmed start missing from backup while client is offline or incomplete", 5),
		live("AC7.10", "fully missing completed work", 5),
		live("AC7.11", "provider effects after terminal receipt", 5),
		live("AC7.12", "installation inventories including ephemeral and retired clients", 5),
		live("AC7.13", "missing evidence blocks reopening", 5),
		live("AC7.14", "unknown cutoffs or history bounds with exhaustive coverage", 5),
		live("AC7.15", "transitive closure including the over-32 failure", 5),
		live("AC7.16", "retained replay", 5),
		live("AC7.17", "bootstrap reissue", 5),
		live("AC7.18", "atomic reopen", 5),
		live("AC7.19", "every lock-held bypass attempt", 5),
		live("AC7.20", "direct local mutation refused against marked hosted home while lock is free", 5),
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

func (h *harness) startCLI(c client, args ...string) (*exec.Cmd, *bytes.Buffer, error) {
	h.logCommand(c.name+hostSuffix(c)+" asynchronous", append([]string{"worklease", "--json"}, args...))
	var cmd *exec.Cmd
	if c.remote {
		remoteEnv := []string{"WORKLEASE_HOME=" + c.home, "XDG_CONFIG_HOME=" + c.config, "SSL_CERT_FILE=" + h.remoteCert, "WORKLEASE_AGENT_ID=" + c.name, "WORKLEASE_SESSION_ID=" + c.name + "-session"}
		sshArgs := []string{h.remoteHost, "env"}
		sshArgs = append(sshArgs, remoteEnv...)
		sshArgs = append(sshArgs, h.remoteBinary, "--json")
		sshArgs = append(sshArgs, args...)
		cmd = exec.Command("ssh", sshArgs...)
	} else {
		cmd = exec.Command(h.binary, append([]string{"--json"}, args...)...)
		cmd.Env, cmd.Dir = c.env, c.checkout
	}
	output := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return cmd, output, nil
}

func waitStartedCommand(cmd *exec.Cmd, timeout time.Duration) (error, error) {
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err := <-finished:
		return err, nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-finished
		return nil, fmt.Errorf("command did not exit within %s", timeout)
	}
}

func waitCLISuccess(cmd *exec.Cmd, output *bytes.Buffer, timeout time.Duration) (map[string]any, error) {
	commandErr, waitErr := waitStartedCommand(cmd, timeout)
	if waitErr != nil {
		return nil, waitErr
	}
	if commandErr != nil {
		return nil, fmt.Errorf("asynchronous CLI failed: %w: %s", commandErr, output.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result["ok"] != true {
		return nil, fmt.Errorf("invalid asynchronous CLI success envelope: %v: %s", err, output.String())
	}
	return result, nil
}

func (h *harness) cliFailureTimeout(c client, timeout time.Duration, args ...string) (map[string]any, error) {
	h.logCommand(c.name+hostSuffix(c)+" expected-failure", append([]string{"worklease", "--json"}, args...))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var cmd *exec.Cmd
	if c.remote {
		remoteEnv := []string{"WORKLEASE_HOME=" + c.home, "XDG_CONFIG_HOME=" + c.config, "SSL_CERT_FILE=" + h.remoteCert, "WORKLEASE_AGENT_ID=" + c.name, "WORKLEASE_SESSION_ID=" + c.name + "-session"}
		sshArgs := []string{h.remoteHost, "env"}
		sshArgs = append(sshArgs, remoteEnv...)
		sshArgs = append(sshArgs, h.remoteBinary, "--json")
		sshArgs = append(sshArgs, args...)
		cmd = exec.CommandContext(ctx, "ssh", sshArgs...)
	} else {
		cmd = exec.CommandContext(ctx, h.binary, append([]string{"--json"}, args...)...)
		cmd.Env, cmd.Dir = c.env, c.checkout
	}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("command timed out after %s: %w", timeout, ctx.Err())
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

func installationIDByLabel(result map[string]any, label string) (string, error) {
	installations, _ := result["installations"].([]any)
	for _, raw := range installations {
		installation, _ := raw.(map[string]any)
		if installation["label"] == label {
			if id, _ := installation["installationId"].(string); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("installation label %q not found", label)
}

func installationLabelExists(result map[string]any, label string) bool {
	_, err := installationIDByLabel(result, label)
	return err == nil
}

func installationIDExists(result map[string]any, installationID string) bool {
	installations, _ := result["installations"].([]any)
	for _, raw := range installations {
		installation, _ := raw.(map[string]any)
		if installation["installationId"] == installationID {
			return true
		}
	}
	return false
}

func installationRevoked(result map[string]any, installationID string) bool {
	installations, _ := result["installations"].([]any)
	for _, raw := range installations {
		installation, _ := raw.(map[string]any)
		if installation["installationId"] == installationID {
			_, revoked := installation["revokedAt"].(string)
			return revoked
		}
	}
	return false
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

func (h *harness) mcpToolCall(c client, name string, arguments map[string]any) (map[string]any, error) {
	h.logCommand(c.name+hostSuffix(c), []string{"worklease", "mcp", "--profile", "team", "tools/call", name})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if c.remote {
		remoteEnv := []string{"WORKLEASE_HOME=" + c.home, "XDG_CONFIG_HOME=" + c.config, "SSL_CERT_FILE=" + h.remoteCert, "WORKLEASE_AGENT_ID=" + c.name, "WORKLEASE_SESSION_ID=" + c.name + "-mcp-session"}
		sshArgs := []string{h.remoteHost, "env"}
		sshArgs = append(sshArgs, remoteEnv...)
		sshArgs = append(sshArgs, h.remoteBinary, "mcp", "--profile", "team")
		cmd = exec.CommandContext(ctx, "ssh", sshArgs...)
	} else {
		cmd = exec.CommandContext(ctx, h.binary, "mcp", "--profile", "team")
		cmd.Env, cmd.Dir = c.env, c.checkout
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
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
		return nil, err
	}
	if !scanner.Scan() {
		return nil, fmt.Errorf("MCP initialize returned no response: %s", stderr.String())
	}
	var initialized map[string]any
	initializeResult := map[string]any(nil)
	if err := json.Unmarshal(scanner.Bytes(), &initialized); err == nil {
		initializeResult, _ = initialized["result"].(map[string]any)
	}
	if fmt.Sprint(initialized["id"]) != "1" || initialized["error"] != nil || initializeResult == nil {
		return nil, fmt.Errorf("MCP initialize returned an invalid response")
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}}); err != nil {
		return nil, err
	}
	var fields map[string]any
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil || fmt.Sprint(message["id"]) != "2" {
			continue
		}
		if message["error"] != nil {
			return nil, fmt.Errorf("MCP %s returned a protocol error", name)
		}
		result, ok := message["result"].(map[string]any)
		if !ok || result["isError"] != true {
			return nil, fmt.Errorf("MCP %s did not return a tool failure", name)
		}
		fields, _ = result["structuredContent"].(map[string]any)
		break
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	finished = true
	if waitErr != nil {
		return nil, fmt.Errorf("MCP %s failed: %w: %s", name, waitErr, stderr.String())
	}
	if fields == nil {
		return nil, fmt.Errorf("MCP %s returned no structured content", name)
	}
	return fields, nil
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
	if h.fullVolumeMaxPageCount > 0 {
		h.server.Env = append(os.Environ(), "WORKLEASE_ACCEPTANCE_SQLITE_MAX_PAGE_COUNT="+strconv.FormatInt(h.fullVolumeMaxPageCount, 10))
	}
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

func readBootstrapState(home, secretPath string) (bootstrapState, error) {
	ready, err := store.HostedReady(home)
	if err != nil {
		return bootstrapState{}, err
	}
	database, err := sql.Open("sqlite", "file:"+filepath.Join(home, store.DatabaseFileName)+"?mode=ro")
	if err != nil {
		return bootstrapState{}, err
	}
	defer database.Close()
	secretInfo, err := os.Stat(secretPath)
	if err != nil {
		return bootstrapState{}, err
	}
	var state bootstrapState
	state.ReadyMarker = ready
	state.SecretMode = fmt.Sprintf("%04o", secretInfo.Mode().Perm())
	var bootstrapReady int
	if err := database.QueryRow(`SELECT bootstrap_ready FROM recovery_state WHERE singleton=1`).Scan(&bootstrapReady); err != nil {
		return bootstrapState{}, err
	}
	state.BootstrapReady = bootstrapReady == 1
	if err := database.QueryRow(`SELECT count(*) FROM invites WHERE bootstrap=1 AND state='active'`).Scan(&state.ActiveInvites); err != nil {
		return bootstrapState{}, err
	}
	if err := database.QueryRow(`SELECT count(*) FROM invites WHERE bootstrap=1 AND state='revoked'`).Scan(&state.RevokedInvites); err != nil {
		return bootstrapState{}, err
	}
	return state, nil
}

func controlledBackupSQLite(source, destination, control, ready, result string) error {
	if err := os.WriteFile(ready, []byte("backup ready\n"), 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(control)
		if err == nil && strings.TrimSpace(string(contents)) == "capture now" {
			if err := backupSQLite(source, destination); err != nil {
				return err
			}
			capture, err := backupMetadata(destination)
			if err != nil {
				return err
			}
			capture.Path = destination
			encoded, err := json.MarshalIndent(capture, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(result, append(encoded, '\n'), 0o600)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("timed out waiting to capture asynchronous backup")
}

func backupMetadata(path string) (backupCapture, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return backupCapture{}, err
	}
	digest := sha256.Sum256(contents)
	database, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return backupCapture{}, err
	}
	defer database.Close()
	var sequence, observedAt int64
	if err := database.QueryRow(`SELECT CAST(value AS INTEGER) FROM meta WHERE key='last_event_seq'`).Scan(&sequence); err != nil {
		return backupCapture{}, err
	}
	if err := database.QueryRow(`SELECT CAST(value AS INTEGER) FROM meta WHERE key='last_observed_at'`).Scan(&observedAt); err != nil {
		return backupCapture{}, err
	}
	return backupCapture{SHA256: hex.EncodeToString(digest[:]), EventSequence: sequence, DurableCutoff: time.UnixMicro(observedAt).UTC().Format(time.RFC3339Nano)}, nil
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
						if parsed := net.ParseIP(address); parsed != nil {
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

func requireOperationPendingCleared(handlePath, pendingRoot, operationID string) error {
	storedHandle, err := handle.Read(handlePath)
	if err != nil {
		return err
	}
	if pending := storedHandle.PendingRequest; pending != nil && pending.OperationID == operationID {
		return fmt.Errorf("successful operation %s remains pending in handle as %s", operationID, pending.Kind)
	}
	records, err := authority.NewFilePendingStore(pendingRoot).List()
	if err != nil {
		return err
	}
	for _, pending := range records {
		if pending.OperationID == operationID || pending.TargetOperationID == operationID {
			return fmt.Errorf("successful operation %s left client pending request %s (%s)", operationID, pending.RequestID, pending.Kind)
		}
	}
	return nil
}

func waitForExactFile(path, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(contents)) == expected {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to contain %q", path, expected)
}

func backupOperationState(databasePath, operationID string) (string, error) {
	db, err := sql.Open("sqlite", "file:"+databasePath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer db.Close()
	var state string
	if err := db.QueryRow(`SELECT state FROM operations WHERE operation_id=?`, operationID).Scan(&state); err != nil {
		return "", err
	}
	return state, nil
}

func backupOperationPresence(databasePath string, operationIDs ...string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", "file:"+databasePath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	presence := make(map[string]bool, len(operationIDs))
	for _, operationID := range operationIDs {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM operations WHERE operation_id=?`, operationID).Scan(&count); err != nil {
			return nil, err
		}
		presence[operationID] = count > 0
	}
	return presence, nil
}

func backupInstallationPresence(databasePath string, installationIDs ...string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", "file:"+databasePath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	presence := make(map[string]bool, len(installationIDs))
	for _, installationID := range installationIDs {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM installations WHERE installation_id=?`, installationID).Scan(&count); err != nil {
			return nil, err
		}
		presence[installationID] = count == 1
	}
	return presence, nil
}

func createSchemaV1Fixture(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return err
	}
	databasePath := filepath.Join(home, "worklease.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE claims (claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, revision INTEGER NOT NULL, agent_id TEXT NOT NULL, session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL CHECK (guarantee = 'local-coordination'), local_replace_allowed INTEGER NOT NULL CHECK (local_replace_allowed IN (0,1)), acquired_at INTEGER NOT NULL, ttl_us INTEGER NOT NULL, heartbeat_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, checkpoint TEXT)`,
		`CREATE TABLE claim_resources (resource TEXT PRIMARY KEY, claim_id TEXT NOT NULL REFERENCES claims(claim_id) ON DELETE CASCADE, position INTEGER NOT NULL)`,
		`CREATE INDEX claim_resources_by_claim ON claim_resources(claim_id)`,
		`CREATE TABLE epochs (claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, agent_id TEXT NOT NULL, session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL, local_replace_allowed INTEGER NOT NULL, acquired_at INTEGER NOT NULL, acquired_seq INTEGER NOT NULL, ended_at INTEGER, ended_seq INTEGER, ended_recorded_at INTEGER, end_reason TEXT CHECK (end_reason IN ('released','transferred','expired')), final_revision INTEGER, successor_claim_id TEXT, checkpoint TEXT)`,
		`CREATE INDEX epochs_by_acquired_seq ON epochs(acquired_seq)`,
		`CREATE TABLE epoch_resources (claim_id TEXT NOT NULL REFERENCES epochs(claim_id) ON DELETE CASCADE, resource TEXT NOT NULL, position INTEGER NOT NULL, PRIMARY KEY (claim_id, position))`,
		`CREATE INDEX epoch_resources_by_resource ON epoch_resources(resource, claim_id)`,
		`CREATE TABLE operations (claim_id TEXT NOT NULL, operation_id TEXT NOT NULL, kind TEXT NOT NULL, request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL, expected_revision INTEGER NOT NULL, state TEXT NOT NULL CHECK (state IN ('started','completed','reconciled')), receipt TEXT, started_at INTEGER NOT NULL, started_seq INTEGER NOT NULL, completed_at INTEGER, completed_seq INTEGER, PRIMARY KEY (claim_id, operation_id))`,
		`CREATE INDEX operations_by_state ON operations(state)`,
		`CREATE UNIQUE INDEX one_started_per_claim ON operations(claim_id) WHERE state = 'started'`,
		`CREATE TABLE reconciliations (claim_id TEXT NOT NULL, operation_id TEXT NOT NULL, outcome TEXT NOT NULL CHECK (outcome IN ('observed-success','observed-failure')), evidence TEXT NOT NULL, request_hash TEXT NOT NULL, reconcile_operation_id TEXT NOT NULL, resolver_claim_id TEXT NOT NULL, resolver_agent_id TEXT NOT NULL, resolver_session_id TEXT NOT NULL, recorded_at INTEGER NOT NULL, recorded_seq INTEGER NOT NULL, PRIMARY KEY (claim_id, operation_id))`,
		`CREATE TABLE events (seq INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, kind TEXT NOT NULL, claim_id TEXT, resources TEXT NOT NULL, operation_id TEXT, revision INTEGER, agent_id TEXT, detail TEXT NOT NULL DEFAULT '{}')`,
		`CREATE INDEX events_by_claim ON events(claim_id, seq)`,
		`CREATE INDEX events_by_at ON events(at)`,
		`INSERT INTO meta(key,value) VALUES ('created_at','1'),('authority_id','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),('last_observed_at','2'),('last_event_seq','5'),('pruned_through_seq','0')`,
		`INSERT INTO claims VALUES('11111111111111111111111111111111','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',3,'upgrade-agent','upgrade-session','upgrade-work','local-coordination',1,4102444700000000,100000000,4102444700000000,4102444800000000,'{"saved":true}')`,
		`INSERT INTO claim_resources VALUES('coordination:upgrade-retained','11111111111111111111111111111111',0)`,
		`INSERT INTO epochs VALUES('11111111111111111111111111111111','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','upgrade-agent','upgrade-session','upgrade-work','local-coordination',1,4102444700000000,4,NULL,NULL,NULL,NULL,NULL,NULL,'{"saved":true}')`,
		`INSERT INTO epoch_resources VALUES('11111111111111111111111111111111','coordination:upgrade-retained',0)`,
		`INSERT INTO operations VALUES('11111111111111111111111111111111','22222222222222222222222222222222','exec','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',4102444800000000,2,'started',NULL,4102444700000000,5,NULL,NULL)`,
		`INSERT INTO operations VALUES('11111111111111111111111111111111','33333333333333333333333333333333','acquire','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',4102444800000000,1,'completed','{"committed":true}',4102444700000000,4,4102444700000000,4)`,
		`INSERT INTO events(seq,at,kind,claim_id,resources,operation_id,revision,agent_id,detail) VALUES(4,10,'acquired','11111111111111111111111111111111','["coordination:upgrade-retained"]','33333333333333333333333333333333',1,'upgrade-agent','{}')`,
		`INSERT INTO events(seq,at,kind,claim_id,resources,operation_id,revision,agent_id,detail) VALUES(5,12,'exec-started','11111111111111111111111111111111','["coordination:upgrade-retained"]','22222222222222222222222222222222',2,'upgrade-agent','{}')`,
		`PRAGMA user_version = 1`,
	}
	for index, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("create schema-v1 fixture statement %d: %w", index, err)
		}
	}
	if err := db.Close(); err != nil {
		return err
	}
	return os.Chmod(databasePath, 0o600)
}

func readSchemaUpgradeState(home string) (schemaUpgradeState, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(home, "worklease.db")+"?mode=ro")
	if err != nil {
		return schemaUpgradeState{}, err
	}
	defer db.Close()
	state := schemaUpgradeState{}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&state.UserVersion); err != nil {
		return schemaUpgradeState{}, err
	}
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='authority_id'`).Scan(&state.AuthorityID); err != nil {
		return schemaUpgradeState{}, err
	}
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='restore_id'`).Scan(&state.RestoreID); err != nil {
		return schemaUpgradeState{}, err
	}
	queries := []struct {
		query string
		value *int64
	}{
		{`SELECT count(*) FROM operations WHERE operation_id='22222222222222222222222222222222' AND state='started'`, &state.StartedOperations},
		{`SELECT count(*) FROM operations WHERE operation_id='33333333333333333333333333333333' AND state='completed' AND receipt IS NOT NULL`, &state.CompletedReplay},
		{`SELECT count(*) FROM events WHERE seq IN (4,5)`, &state.Events},
		{`SELECT count(*) FROM sqlite_master WHERE name IN ('recovery_state','installations','invites','recovery_reopenings','operation_renewals')`, &state.RequiredV2Objects},
	}
	for _, query := range queries {
		if err := db.QueryRow(query.query).Scan(query.value); err != nil {
			return schemaUpgradeState{}, err
		}
	}
	if state.UserVersion != 1 {
		state.LegacyOpenReason = "schema-unsupported"
	}
	return state, nil
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

func quoteRemoteArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func runSSH(host string, args ...string) (string, error) {
	if err := validateSSHHost(host); err != nil {
		return "", err
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteRemoteArg(arg)
	}
	output, err := exec.Command("ssh", host, strings.Join(quoted, " ")).CombinedOutput()
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
