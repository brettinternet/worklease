package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// CLIResult contains one in-process CLI invocation's injected output.
type CLIResult struct {
	Stdout []byte
	Stderr []byte
	Err    error
}

// CLIRunner is a command entry point with injected output writers.
type CLIRunner func(context.Context, []string, io.Writer, io.Writer) error

// RunCLI invokes a command runner without mutating process writers or exiting.
func RunCLI(ctx context.Context, args []string, runner CLIRunner) CLIResult {
	var stdout, stderr bytes.Buffer
	err := runner(ctx, append([]string(nil), args...), &stdout, &stderr)
	return CLIResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Err: err}
}

// DecodeJSON decodes the invocation's single JSON result.
func (r CLIResult) DecodeJSON(target any) error {
	return json.Unmarshal(r.Stdout, target)
}
