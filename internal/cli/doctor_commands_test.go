package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/doctor"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/store"

	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
)

func TestDoctorTextColorsStatuses(t *testing.T) {
	checks := []doctor.Check{{ID: "good", Status: "ok", Detail: "ready"}, {ID: "caution", Status: "warn", Detail: "check"}, {ID: "bad", Status: "fail", Detail: "broken"}}
	var out bytes.Buffer
	if err := writeDoctorText(&out, checks, true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\x1b[32mok\x1b[0m", "\x1b[33mwarn\x1b[0m", "\x1b[31mfail\x1b[0m"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("colored doctor output missing %q: %q", want, out.String())
		}
	}
}

func TestSetupFreePathLifecycleTextAndJSON(t *testing.T) {
	clearWorkleaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	for _, jsonMode := range []bool{false, true} {
		home := t.TempDir()
		args := []string{"worklease", "acquire", "--home", home, "--path", "README.md"}
		if jsonMode {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("acquire json=%t: %v (%s)", jsonMode, err, out.String())
		}
		if jsonMode {
			var envelope map[string]any
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope["ok"] != true || envelope["agentId"] == "" {
				t.Fatalf("acquire envelope=%s err=%v", out.String(), err)
			}
		} else if !strings.Contains(out.String(), "agentId:") {
			t.Fatalf("acquire did not report default agent: %s", out.String())
		}
		for _, command := range [][]string{{"status"}, {"exec", "--", "printf", "ok"}, {"release"}} {
			commandArgs := append([]string{"worklease"}, command...)
			var stderr bytes.Buffer
			if command[0] == "exec" {
				commandArgs = []string{"worklease", "exec", "--home", home}
			} else {
				commandArgs = append(commandArgs, "--home", home)
			}
			if jsonMode {
				commandArgs = append(commandArgs, "--json")
			}
			if command[0] == "exec" {
				commandArgs = append(commandArgs, "--", "printf", "ok")
			}
			out.Reset()
			if err := Run(context.Background(), commandArgs, "dev", "unknown", "unknown", &out, &stderr); err != nil {
				t.Fatalf("%v json=%t: %v (%s)", command, jsonMode, err, out.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("%v json=%t wrote stderr=%q", command, jsonMode, stderr.String())
			}
			if jsonMode {
				if strings.Count(out.String(), "\n") != 1 {
					t.Fatalf("%v emitted multiple JSON documents: %q", command, out.String())
				}
				var envelope map[string]any
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope["ok"] != true {
					t.Fatalf("%v envelope=%s err=%v", command, out.String(), err)
				}
				if command[0] == "release" && !strings.Contains(out.String(), "released") {
					t.Fatalf("release did not report default reason: %s", out.String())
				}
			} else if command[0] == "release" && !strings.Contains(out.String(), "released") {
				t.Fatalf("release did not report default reason: %s", out.String())
			}
		}
	}
}

func TestGroupedCommandWithoutSubcommandJSONIsOneErrorEnvelope(t *testing.T) {
	var out, stderr bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "instructions", "--json"}, "dev", "unknown", "unknown", &out, &stderr)
	if err == nil || stderr.Len() != 0 || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, out.String(), stderr.String())
	}
	var envelope map[string]any
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil || envelope["ok"] != false || envelope["operation"] != "instructions" {
		t.Fatalf("envelope=%s err=%v", out.String(), decodeErr)
	}
}

func TestContextualLoopsUseSessionEnvironmentOnly(t *testing.T) {
	clearWorkleaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKLEASE_HOME", home)
	for _, session := range []string{"loop-one", "loop-two"} {
		t.Setenv("WORKLEASE_SESSION_ID", session)
		var out bytes.Buffer
		for _, command := range [][]string{{"acquire", "--resource", "loop-" + session}, {"status"}, {"release"}} {
			args := append([]string{"worklease"}, command...)
			if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
				t.Fatalf("session=%s command=%v: %v output=%s", session, command, err, out.String())
			}
			out.Reset()
		}
	}
	entries, err := os.ReadDir(filepath.Join(home, "handles"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("session-isolated handles=%d want 2", len(entries))
	}
}

func TestUnscopedContextualSelectorIsDistinctFromClaimSessionMetadata(t *testing.T) {
	clearWorkleaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	run := func(args ...string) (string, error) {
		t.Helper()
		var out bytes.Buffer
		base := []string{"worklease", "--local", "--home", home}
		err := Run(context.Background(), append(base, args...), "dev", "unknown", "unknown", &out, &bytes.Buffer{})
		return out.String(), err
	}

	acquired, err := run("--json", "acquire", "--resource", "coordination:shared")
	if err != nil {
		t.Fatalf("unscoped acquire: %v output=%s", err, acquired)
	}
	var acquireEnvelope map[string]any
	if err := json.Unmarshal([]byte(acquired), &acquireEnvelope); err != nil {
		t.Fatal(err)
	}
	claimSession, _ := acquireEnvelope["sessionId"].(string)
	if claimSession == "" || acquireEnvelope["claimSessionId"] != nil {
		t.Fatalf("acquire JSON changed stable sessionId fields: %s", acquired)
	}

	inspected, err := run("--json", "handle", "inspect")
	if err != nil {
		t.Fatalf("inspect unscoped handle: %v output=%s", err, inspected)
	}
	var inspectEnvelope map[string]any
	if err := json.Unmarshal([]byte(inspected), &inspectEnvelope); err != nil {
		t.Fatal(err)
	}
	if inspectEnvelope["selectorSession"] != "" || inspectEnvelope["claimSessionId"] != claimSession {
		t.Fatalf("selector and claim metadata were conflated: %s", inspected)
	}
	inspectText, err := run("handle", "inspect")
	if err != nil || !strings.Contains(inspectText, `contextualHandleSelector: "" (unscoped)`) || strings.Contains(inspectText, "selectorSession:") {
		t.Fatalf("unscoped inspection text err=%v output=%s", err, inspectText)
	}

	if selected, selectErr := run("--json", "status", "--session", claimSession); selectErr == nil || strings.Contains(selected, `"sessionId"`) {
		t.Fatalf("generated claim session selected the unscoped handle: err=%v output=%s", selectErr, selected)
	}
	contended, contentionErr := run("--json", "acquire", "--session", "other", "--resource", "coordination:shared")
	if contentionErr == nil || !strings.Contains(contended, `"reason":"already-claimed"`) || strings.Contains(contended, `"sessionId"`) {
		t.Fatalf("same resource did not contend independently of selector: err=%v output=%s", contentionErr, contended)
	}

	diagnosed, err := run("--json", "doctor")
	if err != nil || !strings.Contains(diagnosed, `contextual handle selector: \"\" (unscoped)`) {
		t.Fatalf("unscoped doctor err=%v output=%s", err, diagnosed)
	}
	t.Setenv("WORKLEASE_SESSION_ID", "environment-loop")
	diagnosed, err = run("--json", "doctor")
	if err != nil || !strings.Contains(diagnosed, `contextual handle selector: \"environment-loop\"`) || !strings.Contains(diagnosed, "session=env") {
		t.Fatalf("environment doctor selector err=%v output=%s", err, diagnosed)
	}
	diagnosed, err = run("--json", "doctor", "--session", "flag-loop")
	if err != nil || !strings.Contains(diagnosed, `contextual handle selector: \"flag-loop\"`) || !strings.Contains(diagnosed, "session=flag") {
		t.Fatalf("doctor selector precedence err=%v output=%s", err, diagnosed)
	}
	t.Setenv("WORKLEASE_SESSION_ID", "")

	if released, releaseErr := run("release"); releaseErr != nil {
		t.Fatalf("release unscoped handle: %v output=%s", releaseErr, released)
	}
	textAcquire, err := run("acquire", "--resource", "coordination:next")
	if err != nil || !strings.Contains(textAcquire, "claimSessionId:") || strings.Contains(textAcquire, "\nsessionId:") {
		t.Fatalf("acquire text did not distinguish claim metadata: err=%v output=%s", err, textAcquire)
	}
	status, err := run("status", "--full")
	if err != nil || !strings.Contains(status, "claimSessionId:") || strings.Contains(status, "\nsessionId:") {
		t.Fatalf("status text did not distinguish claim metadata: err=%v output=%s", err, status)
	}
	if released, releaseErr := run("release"); releaseErr != nil {
		t.Fatalf("release second unscoped claim: %v output=%s", releaseErr, released)
	}

	for _, command := range [][]string{
		{"acquire", "--session", "loop-a", "--resource", "coordination:explicit"},
		{"heartbeat", "--session", "loop-a"},
		{"status", "--session", "loop-a", "--full"},
		{"release", "--session", "loop-a"},
	} {
		output, commandErr := run(command...)
		if commandErr != nil {
			t.Fatalf("explicit selector lifecycle %v: %v output=%s", command, commandErr, output)
		}
		if command[0] == "acquire" && !strings.Contains(output, "claimSessionId: loop-a") {
			t.Fatalf("explicit selector acquire metadata=%s", output)
		}
	}

	literal := "unscoped (empty resolved selector)"
	if output, acquireErr := run("acquire", "--session", literal, "--resource", "coordination:literal"); acquireErr != nil {
		t.Fatalf("literal selector acquire: %v output=%s", acquireErr, output)
	}
	literalInspect, err := run("handle", "inspect", "--session", literal)
	if err != nil || !strings.Contains(literalInspect, `contextualHandleSelector: "unscoped (empty resolved selector)"`) || strings.Contains(literalInspect, `contextualHandleSelector: "" (unscoped)`) {
		t.Fatalf("literal selector inspection err=%v output=%s", err, literalInspect)
	}
	literalDoctor, err := run("--json", "doctor", "--session", literal)
	if err != nil || !strings.Contains(literalDoctor, `contextual handle selector: \"unscoped (empty resolved selector)\"`) || strings.Contains(literalDoctor, `contextual handle selector: \"\" (unscoped)`) {
		t.Fatalf("literal selector doctor err=%v output=%s", err, literalDoctor)
	}
}

func TestInstructionsAndDoctorAreReadOnly(t *testing.T) {
	clearWorkleaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	var text, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "instructions", "loop"}, "dev", "unknown", "unknown", &text, &stderr); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 || !strings.Contains(text.String(), "same exact canonical resource") || !strings.Contains(text.String(), "--handle PATH") {
		t.Fatalf("instructions text=%q stderr=%q", text.String(), stderr.String())
	}

	text.Reset()
	if err := Run(context.Background(), []string{"worklease", "--json", "instructions", "safety"}, "dev", "unknown", "unknown", &text, &stderr); err != nil {
		t.Fatal(err)
	}
	var instructionEnvelope map[string]any
	if err := json.Unmarshal(text.Bytes(), &instructionEnvelope); err != nil {
		t.Fatal(err)
	}
	if instructionEnvelope["operation"] != "instructions" || instructionEnvelope["topic"] != "safety" || instructionEnvelope["ok"] != true {
		t.Fatalf("instruction envelope=%v", instructionEnvelope)
	}

	text.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "doctor", "--json", "--home", home}, "dev", "unknown", "unknown", &text, &stderr); err != nil {
		t.Fatalf("doctor err=%v output=%s stderr=%s", err, text.String(), stderr.String())
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Checks []struct {
			ID string `json:"id"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(text.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || len(envelope.Checks) != 16 {
		t.Fatalf("doctor envelope=%s", text.String())
	}
	want := []string{"config.sources", "home.path", "home.permissions", "db.open", "db.schema", "context.root", "handle.present", "handle.permissions", "agent.identity", "git.available", "clock.monotonic", "clock.authority", "authority.identity", "restore.identity", "mcp.available", "state.python-era"}
	for i, id := range want {
		if envelope.Checks[i].ID != id {
			t.Fatalf("check %d=%q want %q", i, envelope.Checks[i].ID, id)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("doctor stderr=%q", stderr.String())
	}
	if entries, err := os.ReadDir(home); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("doctor created state: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(home, "handles")); !os.IsNotExist(err) {
		t.Fatalf("doctor created handles directory: %v", err)
	}
}

func TestRunAcquireCommittedHandleWriteFailureIsRedacted(t *testing.T) {
	clearWorkleaseEnvironment(t)
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(home, "handle.json")
	previousWriter := writeHandleFile
	defer func() { writeHandleFile = previousWriter }()
	calls := 0
	var committedToken string
	writeHandleFile = func(path string, value handle.Handle) error {
		calls++
		if calls == 2 {
			committedToken = value.Token
			return errors.New("injected handle write failure")
		}
		return handle.Write(path, value)
	}
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "acquire", "--json", "--home", home, "--handle", handlePath, "--path", "README.md", "--ttl", "1s"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || calls != 2 || committedToken == "" || strings.Contains(out.String(), committedToken) {
		t.Fatalf("acquire failure err=%v calls=%d token=%q output=%s", err, calls, committedToken, out.String())
	}
	exitErr, ok := err.(interface{ ExitCode() int })
	if !ok || exitErr.ExitCode() != reason.ExitAuthority {
		t.Fatalf("exit=%v want %d", err, reason.ExitAuthority)
	}
	var envelope struct {
		Operation string `json:"operation"`
		Error     struct {
			Reason  string         `json:"reason"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if envelope.Operation != "acquire" || envelope.Error.Reason != reason.ReasonHandleWriteFailed || envelope.Error.Details["commitState"] != "committed" || envelope.Error.Details["pendingPath"] != handlePath {
		t.Fatalf("envelope=%s", out.String())
	}
}

func TestRunStartedOperationRetryReportsUnknownOutcomeRedacted(t *testing.T) {
	clearWorkleaseEnvironment(t)
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{})
	claimID, operationID, token := strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("e", 64)
	deadline := time.Now().UTC().Add(time.Hour)
	grant, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, AgentID: "agent", SessionID: "session", Resources: []string{"path:fixture"}, WorkKey: "path:fixture", TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	secret := "started-operation-secret"
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginOperation(context.Background(), lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision}, lease.OperationIntent{OperationID: operationID, Kind: "exec", Request: map[string]any{"argv": []string{"true"}, "cwd": cwd, "gitPrimary": false, "maxDuration": int64(time.Second / time.Microsecond)}, TTL: time.Minute, RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(home, "token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = Run(context.Background(), []string{"worklease", "exec", "--json", "--home", home, "--claim-id", claimID, "--token-file", tokenPath, "--revision", strconv.FormatInt(grant.Revision, 10), "--operation-id", operationID, "--request-not-after", deadline.Format(time.RFC3339Nano), "--ttl", "1m", "--max-duration", "1s", "--", "true"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || strings.Contains(out.String(), token) || strings.Contains(out.String(), secret) {
		t.Fatalf("unknown outcome err=%v output=%s", err, out.String())
	}
	exitErr, ok := err.(interface{ ExitCode() int })
	if !ok || exitErr.ExitCode() != reason.ExitLedger {
		t.Fatalf("exit=%v want %d", err, reason.ExitLedger)
	}
	var envelope struct {
		Operation string `json:"operation"`
		Error     struct {
			Reason  string         `json:"reason"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if envelope.Operation != "exec" || envelope.Error.Reason != reason.ReasonUnknownOutcome || envelope.Error.Details["commitState"] != "unknown" || envelope.Error.Details["operationId"] != operationID {
		t.Fatalf("envelope=%s", out.String())
	}
}

func TestCLICommittedAndUnknownFailuresAreRedacted(t *testing.T) {
	token := strings.Repeat("d", 64)
	for _, test := range []struct {
		name, failure, state string
		exit                 int
	}{
		{name: "committed", failure: reason.ReasonHandleWriteFailed, state: "committed", exit: reason.ExitAuthority},
		{name: "unknown", failure: reason.ReasonUnknownOutcome, state: "unknown", exit: reason.ExitLedger},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			root := NewRootCommand("dev", "unknown", "unknown", &out, &bytes.Buffer{})
			SetInvocationArgs(root, []string{"worklease", "--json", "acquire"})
			command := root.Command("acquire")
			err := reason.New(test.failure, "mutation result is uncertain").With("claimId", "claim").With("operationId", "operation").With("commitState", test.state).With("pendingPath", "/private/pending").With("recoveryHint", "inspect pending operation").With("receipt", map[string]any{"token": token})
			handled := root.Metadata[jsonStateKey].(*boundary).handle(command, err)
			exitErr, hasExitCode := handled.(interface{ ExitCode() int })
			if handled == nil || !JSONErrorHandled(handled) || !hasExitCode || exitErr.ExitCode() != test.exit {
				t.Fatalf("handled error=%v", handled)
			}
			if strings.Contains(out.String(), token) || strings.Count(out.String(), "\n") != 1 {
				t.Fatalf("unsafe JSON failure=%q", out.String())
			}
			var envelope map[string]any
			if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil || envelope["operation"] != "acquire" || envelope["ok"] != false {
				t.Fatalf("envelope=%s err=%v", out.String(), decodeErr)
			}
			failure, ok := envelope["error"].(map[string]any)
			if !ok {
				t.Fatalf("failure=%v", envelope["error"])
			}
			details, ok := failure["details"].(map[string]any)
			if !ok || details["commitState"] != test.state || details["pendingPath"] != "/private/pending" || details["recoveryHint"] != "inspect pending operation" {
				t.Fatalf("failure details=%v", failure)
			}
			var text bytes.Buffer
			if err := output.WriteTextError(&text, err); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(text.String(), token) || !strings.Contains(text.String(), "operationId: operation") || !strings.Contains(text.String(), "commitState: "+test.state) || !strings.Contains(text.String(), "recoveryHint: inspect pending operation") {
				t.Fatalf("unsafe text failure=%q", text.String())
			}
		})
	}
}

func TestCLIConfigPrecedenceAndBlankEnvironment(t *testing.T) {
	clearWorkleaseEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	fileHome := filepath.Join(home, "yaml-home")
	envHome := filepath.Join(home, "env-home")
	flagHome := filepath.Join(home, "flag-home")
	for _, path := range []string{fileHome, envHome, flagHome} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configDir := filepath.Join(home, ".config", "worklease")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("home: "+fileHome+"\nagent_id: yaml-agent\nttl: 1s\npoll_interval: 100ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	detail := doctorConfigSourceDetail(t, []string{"worklease", "doctor", "--json"})
	for _, source := range []string{"home=file", "agent=file", "ttl=file", "poll_interval=file"} {
		if !strings.Contains(detail, source) {
			t.Fatalf("file precedence missing %q: %s", source, detail)
		}
	}
	t.Setenv("WORKLEASE_HOME", envHome)
	t.Setenv("WORKLEASE_AGENT_ID", "env-agent")
	t.Setenv("WORKLEASE_TTL", "2s")
	detail = doctorConfigSourceDetail(t, []string{"worklease", "doctor", "--json"})
	for _, source := range []string{"home=env", "agent=env", "ttl=env"} {
		if !strings.Contains(detail, source) {
			t.Fatalf("environment precedence missing %q: %s", source, detail)
		}
	}
	detail = doctorConfigSourceDetail(t, []string{"worklease", "doctor", "--json", "--home", flagHome})
	if !strings.Contains(detail, "home=flag") || !strings.Contains(detail, "agent=env") {
		t.Fatalf("flag/environment precedence incorrect: %s", detail)
	}
	t.Setenv("WORKLEASE_HOME", "")
	t.Setenv("WORKLEASE_AGENT_ID", "")
	t.Setenv("WORKLEASE_TTL", "")
	detail = doctorConfigSourceDetail(t, []string{"worklease", "doctor", "--json"})
	if !strings.Contains(detail, "home=file") || !strings.Contains(detail, "agent=file") || !strings.Contains(detail, "ttl=file") {
		t.Fatalf("blank environment was not unset: %s", detail)
	}
	noConfigHome := t.TempDir()
	t.Setenv("HOME", noConfigHome)
	detail = doctorConfigSourceDetail(t, []string{"worklease", "doctor", "--json"})
	if !strings.Contains(detail, "home=default") || !strings.Contains(detail, "config_path=default") {
		t.Fatalf("default precedence missing: %s", detail)
	}
}

func doctorConfigSourceDetail(t *testing.T, args []string) string {
	t.Helper()
	var out bytes.Buffer
	if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor: %v output=%s", err, out.String())
	}
	var envelope struct {
		Checks []struct {
			ID     string `json:"id"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, check := range envelope.Checks {
		if check.ID == "config.sources" {
			return check.Detail
		}
	}
	t.Fatalf("config.sources missing: %s", out.String())
	return ""
}

func TestDoctorUnsafeStateFailureEnvelopeAndHints(t *testing.T) {
	clearWorkleaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	if err := os.Chmod(home, 0o777); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "doctor", "--json", "--home", home}, "dev", "unknown", "unknown", &out, &stderr)
	if err == nil || stderr.Len() != 0 || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("unsafe home err=%v stdout=%q stderr=%q", err, out.String(), stderr.String())
	}
	if !strings.Contains(out.String(), `"id":"home.path"`) || !strings.Contains(out.String(), `"status":"fail"`) || !strings.Contains(out.String(), "safe ancestors") {
		t.Fatalf("unsafe home diagnostics=%s", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(home, "worklease.db")); !os.IsNotExist(statErr) {
		t.Fatalf("doctor created database: %v", statErr)
	}

	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(target, []byte("private database fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, "worklease.db")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = Run(context.Background(), []string{"worklease", "doctor", "--json", "--home", home}, "dev", "unknown", "unknown", &out, &stderr)
	if err == nil || strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), `"id":"db.open"`) || !strings.Contains(out.String(), "regular file") {
		t.Fatalf("unsafe database diagnostics err=%v output=%s", err, out.String())
	}
}

func clearWorkleaseEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"WORKLEASE_HOME", "WORKLEASE_CONFIG", "WORKLEASE_SESSION_ID", "WORKLEASE_AGENT_ID", "WORKLEASE_TTL", "WORKLEASE_MAX_DURATION", "WORKLEASE_POLL_INTERVAL", "WORKLEASE_RETENTION_DAYS", "WORKLEASE_HANDLE", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(name, "")
	}
}

func TestDoctorDoesNotExposePrivateHandleOrPythonState(t *testing.T) {
	clearWorkleaseEnvironment(t)
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("b", 64)
	if err := os.WriteFile(filepath.Join(home, "leases.sqlite3"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	handleDir := t.TempDir()
	if err := os.Chmod(handleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(handleDir, "private-handle.json")
	if err := os.WriteFile(handlePath, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKLEASE_HANDLE", handlePath)
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "doctor", "--json", "--home", home}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("doctor: %v output=%s", err, out.String())
	}
	if strings.Contains(out.String(), token) || !strings.Contains(out.String(), "state.python-era") || !strings.Contains(out.String(), "warn") {
		t.Fatalf("doctor exposed private state or omitted warning: %s", out.String())
	}
}
