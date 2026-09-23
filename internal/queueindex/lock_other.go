//go:build !darwin && !linux

package queueindex

import (
	"context"
	"errors"
)

func (i *Index) TryRefreshLock(Partition) (func(), bool, error) {
	return nil, false, errors.New("queue refresh locks are unsupported on this platform")
}
func (i *Index) WaitRefreshLock(context.Context, Partition) (func(), error) {
	return nil, errors.New("queue refresh locks are unsupported on this platform")
}
