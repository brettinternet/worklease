package lease

import (
	"context"

	"github.com/brettinternet/worklease/internal/store"
)

// RemoteTransactionCheck lets storage-neutral adapters enforce installation,
// role, authority, and incarnation inside the transaction that reads data.
func (s *Service) RemoteTransactionCheck(actor RemoteActor, requiredRole string) func(context.Context, *store.Tx) error {
	return s.RemoteActorTransactionCheck(&actor, requiredRole)
}

// RemoteActorTransactionCheck also returns the authenticated installation
// identity through actor for adapters that persist installation-attributed
// replay in the same transaction.
func (s *Service) RemoteActorTransactionCheck(actor *RemoteActor, requiredRole string) func(context.Context, *store.Tx) error {
	return func(ctx context.Context, tx *store.Tx) error {
		return s.authorizeRemoteContext(ctx, tx, actor, requiredRole)
	}
}

// AuthorizeRemote is retained for callers that need only an authorization
// probe. Data-returning remote adapters should use RemoteTransactionCheck.
func (s *Service) AuthorizeRemote(ctx context.Context, actor RemoteActor, requiredRole string) error {
	check := s.RemoteTransactionCheck(actor, requiredRole)
	return s.st.Read(ctx, func(tx *store.Tx) error { return check(ctx, tx) })
}
