package cli

import (
	"encoding/json"
	"testing"
)

func TestHookTargetsAcceptNativeFileEditorsAndRejectBash(t *testing.T) {
	tests := []struct {
		tool  string
		field string
		want  string
		ok    bool
	}{
		{tool: "Edit", field: "file_path", want: "edit.go", ok: true},
		{tool: "Write", field: "file_path", want: "write.go", ok: true},
		{tool: "MultiEdit", field: "file_path", want: "multi.go", ok: true},
		{tool: "NotebookEdit", field: "notebook_path", want: "book.ipynb", ok: true},
		{tool: "Bash", field: "command", want: "worklease status", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			raw, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			targets, err := hookTargets(hookEvent{ToolName: tc.tool, ToolInput: map[string]json.RawMessage{tc.field: raw}})
			if tc.ok {
				if err != nil || len(targets) != 1 || targets[0] != tc.want {
					t.Fatalf("targets=%v err=%v", targets, err)
				}
			} else if err == nil {
				t.Fatalf("unsupported tool accepted: %v", targets)
			}
		})
	}
}

func TestHookTargetsRejectMalformedInput(t *testing.T) {
	for _, event := range []hookEvent{
		{ToolName: "Edit", ToolInput: map[string]json.RawMessage{}},
		{ToolName: "Write", ToolInput: map[string]json.RawMessage{"file_path": json.RawMessage(`123`)}},
		{ToolName: "NotebookEdit", ToolInput: map[string]json.RawMessage{"notebook_path": json.RawMessage(`""`)}},
	} {
		if targets, err := hookTargets(event); err == nil {
			t.Fatalf("malformed event accepted: %v", targets)
		}
	}
}
