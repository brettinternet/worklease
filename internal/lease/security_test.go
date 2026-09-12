package lease

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/store"
)

func TestClientCredentialIsHashOnlyAndPublicViewsAreTokenFree(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(context.Background(), req("secret-resource", id, token))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(grant.Receipt.RequestHash, token) {
		t.Fatal("token appeared in request hash")
	}
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		var claimHash, epochHash string
		if err := tx.QueryRowContext(context.Background(), `SELECT token_hash FROM claims WHERE claim_id=?`, id).Scan(&claimHash); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT token_hash FROM epochs WHERE claim_id=?`, id).Scan(&epochHash); err != nil {
			return err
		}
		if claimHash == token || epochHash == token || len(claimHash) != 64 || len(epochHash) != 64 {
			t.Fatalf("unsafe credential state")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err := svc.Status(context.Background(), Selector{ClaimID: id})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(status), token) {
		t.Fatal("token appeared in public status")
	}
}
