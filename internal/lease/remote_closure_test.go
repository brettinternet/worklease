package lease

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func TestRemoteRecoveryClosureOverThirtyTwoFailsAtomically(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	err := st.Write(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET recovery_mode=1,recovery_revision=1,restored_at=? WHERE singleton=1`, clock.Now().UnixMicro()); err != nil {
			return err
		}
		for i := 0; i < 17; i++ {
			claimID := fmt.Sprintf("%032x", i+1)
			if _, err := tx.ExecContext(context.Background(), `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,end_reason,final_revision,admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, claimID, strings.Repeat("a", 64), "agent", "session", "work", "local-coordination", 0, clock.Now().UnixMicro(), i+1, clock.Now().UnixMicro(), "restored", 2, (10 * time.Second).Microseconds(), clock.Now().Add(time.Minute).UnixMicro(), actor.InstallationID, st.RestoreID()); err != nil {
				return err
			}
			resources := []string{fmt.Sprintf("github:org/repo#%02d", i*2), fmt.Sprintf("github:org/repo#%02d", i*2+1), fmt.Sprintf("github:org/repo#%02d", i*2+2)}
			for position, resource := range resources {
				if _, err := tx.ExecContext(context.Background(), `INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,?)`, claimID, resource, position); err != nil {
					return err
				}
			}
			operationID := fmt.Sprintf("%032x", i+100)
			if _, err := tx.ExecContext(context.Background(), `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`, claimID, operationID, "exec", strings.Repeat("b", 64), clock.Now().Add(time.Hour).UnixMicro(), 1, "started", clock.Now().UnixMicro(), i+1, actor.InstallationID, st.RestoreID()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	request := remoteAcquire(st, clock, actor, "f")
	request.Resources = []string{"github:org/repo#00"}
	_, err = svc.Acquire(context.Background(), request)
	if !isReason(err, reason.ReasonRecoveryRequired) {
		t.Fatalf("closure error=%v", err)
	}
	var claims int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM claims WHERE claim_id=?`, request.ClaimID).Scan(&claims)
	}); err != nil {
		t.Fatal(err)
	}
	if claims != 0 {
		t.Fatalf("partial claim persisted: %d", claims)
	}
}
