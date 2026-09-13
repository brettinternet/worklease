package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	appcli "github.com/brettinternet/worklease/internal/cli"
	apprelease "github.com/brettinternet/worklease/internal/release"
)

type target struct{ goos, goarch, platform, assetArch string }

type optionalString struct {
	value string
	set   bool
}

func (value *optionalString) Set(input string) error {
	value.value = input
	value.set = true
	return nil
}

func (value *optionalString) String() string { return value.value }

var targets = []target{
	{"linux", "amd64", "linux", "x64"},
	{"linux", "arm64", "linux", "arm64"},
	{"darwin", "amd64", "macos", "x64"},
	{"darwin", "arm64", "macos", "arm64"},
}

func main() {
	version := flag.String("version", "", "release version without v prefix")
	commit := flag.String("commit", "unknown", "source commit")
	buildTime := flag.String("build-time", time.Now().UTC().Format(time.RFC3339), "build timestamp")
	output := flag.String("output", "dist/release", "output directory")
	current := flag.Bool("current", false, "build only the current OS and architecture")
	changelog := flag.String("changelog", "CHANGELOG.md", "changelog path")
	var prepareChangelog optionalString
	var changelogNotes optionalString
	flag.Var(&prepareChangelog, "prepare-changelog", "promote Unreleased using this YYYY-MM-DD date")
	flag.Var(&changelogNotes, "changelog-notes", "write the matching version's release notes to this file")
	flag.Parse()
	if *version == "" || strings.HasPrefix(*version, "v") || strings.ContainsAny(*version, `/\\`) {
		fatal(fmt.Errorf("version must be a bare release version"))
	}
	if err := validateChangelogModes(prepareChangelog, changelogNotes); err != nil {
		fatal(err)
	}
	if prepareChangelog.set {
		if err := apprelease.PrepareChangelog(*changelog, *version, prepareChangelog.value); err != nil {
			fatal(err)
		}
		fmt.Printf("promoted %s Unreleased notes to %s (%s)\n", *changelog, *version, prepareChangelog.value)
		return
	}
	if changelogNotes.set {
		if err := apprelease.WriteChangelogReleaseNotes(*changelog, changelogNotes.value, *version); err != nil {
			fatal(err)
		}
		fmt.Println(changelogNotes.value)
		return
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatal(err)
	}
	manual := filepath.Join(*output, "worklease.1")
	var man bytes.Buffer
	root := appcli.NewRootCommand(*version, *commit, *buildTime, io.Discard, io.Discard)
	if err := appcli.WriteManPage(&man, root, time.Now().UTC().Format(time.DateOnly)); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(manual, man.Bytes(), 0o644); err != nil {
		fatal(err)
	}
	selected := targets
	if *current {
		selected = nil
		for _, candidate := range targets {
			if candidate.goos == runtime.GOOS && candidate.goarch == runtime.GOARCH {
				selected = append(selected, candidate)
			}
		}
		if len(selected) == 0 {
			fatal(fmt.Errorf("unsupported current target %s/%s", runtime.GOOS, runtime.GOARCH))
		}
	}
	working, err := os.MkdirTemp("", "worklease-release-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(working)
	for _, candidate := range selected {
		binary := filepath.Join(working, candidate.goos+"-"+candidate.goarch, "worklease")
		if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
			fatal(err)
		}
		ldflags := fmt.Sprintf("-s -w -X main.buildVersion=%s -X main.buildCommit=%s -X main.buildTime=%s", *version, *commit, *buildTime)
		command := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", binary, "./cmd/worklease")
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+candidate.goos, "GOARCH="+candidate.goarch)
		if output, err := command.CombinedOutput(); err != nil {
			fatal(fmt.Errorf("build %s/%s: %w: %s", candidate.goos, candidate.goarch, err, output))
		}
		archive := filepath.Join(*output, fmt.Sprintf("worklease-v%s-%s-%s.tar.gz", *version, candidate.platform, candidate.assetArch))
		if err := apprelease.CreateArchive(archive, binary, manual); err != nil {
			fatal(err)
		}
		fmt.Println(archive)
	}
	if _, err := apprelease.WriteChecksums(*output); err != nil {
		fatal(err)
	}
	if err := apprelease.VerifyChecksums(*output); err != nil {
		fatal(err)
	}
}

func validateChangelogModes(prepare, notes optionalString) error {
	if prepare.set && prepare.value == "" {
		return fmt.Errorf("--prepare-changelog requires a YYYY-MM-DD date")
	}
	if notes.set && notes.value == "" {
		return fmt.Errorf("--changelog-notes requires an output path")
	}
	if prepare.set && notes.set {
		return fmt.Errorf("--prepare-changelog and --changelog-notes cannot be used together")
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
