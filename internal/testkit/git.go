package testkit

import (
	"os"
	"os/exec"
)

// GitCommand creates a Git subprocess isolated from the caller's repository.
// Git exports repository-local variables to hooks; fixture commands must not
// inherit them or they can mutate the repository running the test.
func GitCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = Environment(os.Environ(), nil)
	return cmd
}
