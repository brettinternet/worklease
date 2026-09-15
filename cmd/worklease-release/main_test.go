package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestReleaseWorkflowPublishesValidatedChangelogNotes(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, required := range []string{
		`${{ github.event_name == 'workflow_dispatch' && inputs.release_tag || github.ref_name }}`,
		`--changelog-notes "$RUNNER_TEMP/release-notes.md"`,
		`body_path: ${{ runner.temp }}/release-notes.md`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("release workflow does not contain %q", required)
		}
	}
	if strings.Count(text, "--changelog-notes") != 2 {
		t.Fatal("release workflow must validate notes in go-assets and extract them again for publication")
	}
	if strings.Contains(text, "generate_release_notes:") {
		t.Fatal("release workflow still enables GitHub-generated release notes")
	}
}

func TestReleaseWorkflowValidatesAndPublishesContainer(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, required := range []string{
		"Container validation (linux-${{ matrix.arch }})",
		"arch: amd64",
		"arch: arm64",
		"scripts/test-container-image.sh",
		"if: github.event_name == 'push'",
		"ghcr.io/brettinternet/worklease",
		"--platform linux/amd64,linux/arm64",
		"docker buildx imagetools inspect",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("release workflow does not contain %q", required)
		}
	}
	if strings.Count(text, "packages: write") != 1 {
		t.Fatal("package-write permission must be limited to one container publication job")
	}

	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	container := string(dockerfile)
	for _, required := range []string{"FROM scratch", "COPY --chown=65532:65532", "USER 65532:65532", `ENTRYPOINT ["/worklease"]`} {
		if !strings.Contains(container, required) {
			t.Fatalf("Dockerfile does not contain %q", required)
		}
	}
}

func TestExplicitEmptyChangelogModesAreRejected(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare optionalString
		notes   optionalString
		want    string
	}{
		{name: "prepare", prepare: optionalString{set: true}, want: "requires a YYYY-MM-DD date"},
		{name: "notes", notes: optionalString{set: true}, want: "requires an output path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateChangelogModes(test.prepare, test.notes)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want containing %q", err, test.want)
			}
		})
	}
}

func TestReleaseTargetsCoverSupportedNativeArchives(t *testing.T) {
	var names []string
	for _, target := range targets {
		names = append(names, fmt.Sprintf("worklease-v1.2.3-%s-%s.tar.gz", target.platform, target.assetArch))
	}
	want := []string{
		"worklease-v1.2.3-linux-x64.tar.gz",
		"worklease-v1.2.3-linux-arm64.tar.gz",
		"worklease-v1.2.3-macos-x64.tar.gz",
		"worklease-v1.2.3-macos-arm64.tar.gz",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("archive names=%v want=%v", names, want)
	}
}
