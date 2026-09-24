package cli

import "os/exec"

// detachLaunch reaps short-lived children without blocking the UI or managing
// their claims. The worker remains independent of the queue's context.
func detachLaunch(cmd *exec.Cmd) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd.Wait()
	}()
	return done
}
