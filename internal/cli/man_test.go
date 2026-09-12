package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteManPageDerivesRegisteredCommandsFlagsExamplesAndVersion(t *testing.T) {
	root := NewRootCommand("1.2.3", "abc", "now", &bytes.Buffer{}, &bytes.Buffer{})
	var output bytes.Buffer
	if err := WriteManPage(&output, root, "2026-09-12"); err != nil {
		t.Fatal(err)
	}
	page := output.String()
	for _, want := range []string{`.TH WORKLEASE 1 "2026-09-12" "worklease 1.2.3"`, ".SH \"COMMAND REFERENCE\"", "worklease acquire \\-\\-path README.md"} {
		if !strings.Contains(page, want) {
			t.Errorf("manual missing %q", want)
		}
	}
	for _, flag := range root.VisibleFlags() {
		if flag != nil && len(flag.Names()) > 0 && !strings.Contains(page, roff("--"+flag.Names()[0])) {
			t.Errorf("manual missing global flag --%s", flag.Names()[0])
		}
	}
	for _, example := range manExamples(root.Description) {
		if !strings.Contains(page, roff(example)) {
			t.Errorf("manual missing root example %q", example)
		}
	}
	for _, command := range visibleCommands(root) {
		path := roff(strings.Join(command.Path(), " "))
		if !strings.Contains(page, `.SS "`+path+`"`) {
			t.Errorf("manual missing command %s", strings.Join(command.Path(), " "))
		}
		for _, flag := range command.VisibleFlags() {
			if flag != nil && len(flag.Names()) > 0 && !strings.Contains(page, roff("--"+flag.Names()[0])) {
				t.Errorf("manual missing flag --%s for %s", flag.Names()[0], path)
			}
		}
		for _, example := range manExamples(command.Description) {
			if !strings.Contains(page, roff(example)) {
				t.Errorf("manual missing example %q", example)
			}
		}
	}
}

func TestWriteManPageRejectsIncompleteInput(t *testing.T) {
	if err := WriteManPage(nil, nil, ""); err == nil {
		t.Fatal("expected validation error")
	}
}
