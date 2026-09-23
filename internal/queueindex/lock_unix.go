//go:build darwin || linux

package queueindex

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// TryRefreshLock acquires the advisory lock for one exact partition. The
// returned release function must be called when refresh completes.
func (i *Index) TryRefreshLock(p Partition) (release func(), acquired bool, err error) {
	key, err := p.key()
	if err != nil {
		return nil, false, err
	}
	path := filepath.Join(i.dir, "refresh-"+key+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	_ = f.Chmod(0600)
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, true, nil
}
func (i *Index) WaitRefreshLock(ctx context.Context, p Partition) (func(), error) {
	for {
		release, ok, err := i.TryRefreshLock(p)
		if err != nil {
			return nil, err
		}
		if ok {
			return release, nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
