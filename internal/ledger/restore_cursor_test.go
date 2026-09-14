package ledger

import (
	"context"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func TestCursorRestoreIncarnationMismatchIsDistinct(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cursor := EncodeCursor(st.AuthorityID(), strings.Repeat("f", 32), "events", "", 0)
	_, err = New(st).Events(context.Background(), cursor, 1)
	if failure := reason.As(err); failure == nil || failure.Reason != reason.ReasonAuthorityRestored || failure.Details["restoreId"] != st.RestoreID() {
		t.Fatalf("cursor restore error=%v", err)
	}
	current := EncodeCursor(st.AuthorityID(), st.RestoreID(), "events", "", 0)
	if _, err := New(st).Events(context.Background(), current, 1); err != nil {
		t.Fatal(err)
	}
}
