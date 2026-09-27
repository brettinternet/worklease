package runs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Environment protocol between a launcher and a detached supervisor. The
// supervisor strips both variables from its worker's environment.
const (
	EnvSupervisorID = "WORKLEASE_RUN_SUPERVISOR_ID"
	EnvReadyFD      = "WORKLEASE_RUN_READY_FD"
	readyFD         = 3
)

// ReadyTimeout bounds how long a launcher waits for the supervisor to acquire
// its claim and start the worker.
const ReadyTimeout = 2 * time.Minute

// StartDetached starts `worklease run` in a new session so it outlives the
// caller, then waits until the supervisor reports the worker running or the
// run failed. executable is the worklease binary; args exclude argv[0].
//
// The returned done channel closes when the supervisor process is reaped. A
// short-lived caller may ignore it.
func StartDetached(executable string, args []string, dir string, env []string, id string) (Record, <-chan struct{}, error) {
	if !ValidID(id) {
		return Record{}, nil, fmt.Errorf("invalid run ID %q", id)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return Record{}, nil, err
	}
	defer reader.Close()
	cmd := &exec.Cmd{Path: executable, Args: append([]string{"worklease"}, args...), Dir: dir, ExtraFiles: []*os.File{writer}, SysProcAttr: &syscall.SysProcAttr{Setsid: true}}
	cmd.Env = append(stripProtocol(env), EnvSupervisorID+"="+id, fmt.Sprintf("%s=%d", EnvReadyFD, readyFD))
	err = cmd.Start()
	writer.Close()
	if err != nil {
		return Record{}, nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()
	_ = reader.SetReadDeadline(time.Now().Add(ReadyTimeout))
	data, err := io.ReadAll(io.LimitReader(reader, maxRecord+1))
	if err != nil && len(data) == 0 {
		return Record{}, done, fmt.Errorf("run %s did not report readiness: %w", id, err)
	}
	var record Record
	if len(data) == 0 || json.Unmarshal(data, &record) != nil || record.ID != id {
		return Record{}, done, fmt.Errorf("run %s exited before reporting readiness; see worklease runs show %s", id, id)
	}
	return record, done, nil
}

// ReadyWriter returns the launcher's readiness pipe when this process was
// started by StartDetached, and the assigned run ID.
func ReadyWriter(getenv func(string) string) (*os.File, string) {
	if getenv(EnvReadyFD) != fmt.Sprint(readyFD) || !ValidID(getenv(EnvSupervisorID)) {
		return nil, ""
	}
	// The worker must not inherit the pipe, or the launcher would wait for it.
	syscall.CloseOnExec(readyFD)
	return os.NewFile(readyFD, "worklease-run-ready"), getenv(EnvSupervisorID)
}

// WorkerEnv removes the launcher protocol from an environment.
func WorkerEnv(env []string) []string { return stripProtocol(env) }

func stripProtocol(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, EnvSupervisorID+"=") || strings.HasPrefix(entry, EnvReadyFD+"=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}
