package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareChangelogPromotesUnreleasedVerbatim(t *testing.T) {
	path := writeTestChangelog(t, `# Changelog

## Unreleased

### Fixed

- First fix.

### Added

- Added later, in this order.

## 1.1.0 - 2026-09-12

### Added

- Existing notes.
`)

	if err := PrepareChangelog(path, "1.2.0", "2026-09-13"); err != nil {
		t.Fatal(err)
	}
	got := readTestChangelog(t, path)
	want := `# Changelog

## Unreleased

## 1.2.0 - 2026-09-13

### Fixed

- First fix.

### Added

- Added later, in this order.

## 1.1.0 - 2026-09-12

### Added

- Existing notes.
`
	if got != want {
		t.Fatalf("promoted changelog:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrepareAndExtractPreserveHeadingsInsideFencedCode(t *testing.T) {
	contents := "# Changelog\n\n## Unreleased\n\n### Added\n\n- Documented example:\n\n```markdown\n## sample\n\nExample body.\n```\n\n## 1.1.0 - 2026-09-12\n\n- Older.\n"
	path := writeTestChangelog(t, contents)
	if err := PrepareChangelog(path, "1.2.0", "2026-09-13"); err != nil {
		t.Fatal(err)
	}
	notes, err := ChangelogReleaseNotes(path, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	want := "### Added\n\n- Documented example:\n\n```markdown\n## sample\n\nExample body.\n```\n"
	if notes != want {
		t.Fatalf("notes=%q want=%q", notes, want)
	}
}

func TestPrepareChangelogRejectsInvalidStateWithoutChangingFile(t *testing.T) {
	valid := `# Changelog

## Unreleased

### Added

- New behavior.

## 1.1.0 - 2026-09-12

- Existing behavior.
`
	tests := []struct {
		name      string
		contents  string
		version   string
		date      string
		wantError string
	}{
		{name: "version prefix", contents: valid, version: "v1.2.0", date: "2026-09-13", wantError: "bare semantic version"},
		{name: "version leading zero", contents: valid, version: "1.02.0", date: "2026-09-13", wantError: "bare semantic version"},
		{name: "malformed date", contents: valid, version: "1.2.0", date: "2026-02-30", wantError: "valid date"},
		{name: "empty unreleased", contents: "# Changelog\n\n## Unreleased\n\n### Added\n\n## 1.1.0 - 2026-09-12\n\n- Existing.\n", version: "1.2.0", date: "2026-09-13", wantError: "no release notes"},
		{name: "tab heading only", contents: "# Changelog\n\n## Unreleased\n\n###\tAdded\n\n## 1.1.0 - 2026-09-12\n\n- Existing.\n", version: "1.2.0", date: "2026-09-13", wantError: "no release notes"},
		{name: "existing target", contents: valid, version: "1.1.0", date: "2026-09-13", wantError: "refusing to overwrite"},
		{name: "duplicate versions", contents: valid + "\n## 1.1.0 - 2026-09-11\n\n- Duplicate.\n", version: "1.2.0", date: "2026-09-13", wantError: "duplicate release sections"},
		{name: "duplicate unreleased", contents: valid + "\n## Unreleased\n\n- Duplicate.\n", version: "1.2.0", date: "2026-09-13", wantError: "exactly one ## Unreleased"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestChangelog(t, test.contents)
			err := PrepareChangelog(path, test.version, test.date)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v, want containing %q", err, test.wantError)
			}
			if got := readTestChangelog(t, path); got != test.contents {
				t.Fatalf("failed preparation changed changelog:\n%s", got)
			}
		})
	}
}

func TestChangelogReleaseNotesReturnsExactMatchingBody(t *testing.T) {
	path := writeTestChangelog(t, `# Changelog

## Unreleased

## 1.2.0 - 2026-09-13

### Fixed

- Keep **this** formatting.

### Added

- Keep this order.

## 1.1.0 - 2026-09-12

- Older.
`)
	got, err := ChangelogReleaseNotes(path, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	want := "### Fixed\n\n- Keep **this** formatting.\n\n### Added\n\n- Keep this order.\n"
	if got != want {
		t.Fatalf("notes=%q want=%q", got, want)
	}
}

func TestChangelogReleaseNotesRejectsMissingMalformedDuplicateAndEmptySections(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantError string
	}{
		{name: "missing", contents: "# Changelog\n\n## Unreleased\n", wantError: "found 0"},
		{name: "malformed date", contents: "# Changelog\n\n## 1.2.0 - someday\n\n- Notes.\n", wantError: "YYYY-MM-DD"},
		{name: "duplicate", contents: "# Changelog\n\n## 1.2.0 - 2026-09-13\n\n- One.\n\n## 1.2.0 - 2026-09-12\n\n- Two.\n", wantError: "duplicate release sections"},
		{name: "malformed duplicate spacing", contents: "# Changelog\n\n## 1.2.0 - 2026-09-13\n\n- One.\n\n## 1.2.0  - 2026-09-12\n\n- Two.\n", wantError: "duplicate release sections"},
		{name: "empty", contents: "# Changelog\n\n## 1.2.0 - 2026-09-13\n\n### Added\n", wantError: "no release notes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestChangelog(t, test.contents)
			_, err := ChangelogReleaseNotes(path, "1.2.0")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestWriteChangelogReleaseNotesRejectsSourceAsOutput(t *testing.T) {
	contents := "# Changelog\n\n## 1.2.0 - 2026-09-13\n\n- Notes.\n"
	path := writeTestChangelog(t, contents)
	err := WriteChangelogReleaseNotes(path, path, "1.2.0")
	if err == nil || !strings.Contains(err.Error(), "must not overwrite") {
		t.Fatalf("error=%v", err)
	}
	if got := readTestChangelog(t, path); got != contents {
		t.Fatalf("source changelog changed to %q", got)
	}
}

func writeTestChangelog(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "CHANGELOG.md")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readTestChangelog(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
