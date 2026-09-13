package release

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var semanticVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type changelogSection struct {
	title      string
	headingEnd int
	bodyStart  int
	bodyEnd    int
}

// PrepareChangelog promotes the Unreleased notes to version and leaves a new,
// empty Unreleased section in their place.
func PrepareChangelog(path, version, date string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	if err := validateDate(date); err != nil {
		return err
	}
	contents, mode, err := readChangelog(path)
	if err != nil {
		return err
	}
	sections := changelogSections(contents)
	unreleased := sectionsNamed(sections, "Unreleased")
	if len(unreleased) != 1 {
		return fmt.Errorf("changelog must contain exactly one ## Unreleased section; found %d", len(unreleased))
	}
	if err := validateUniqueVersions(sections); err != nil {
		return err
	}
	for _, section := range sections {
		if sectionVersion(section.title) == version {
			return fmt.Errorf("changelog already contains release section for version %s; refusing to overwrite it", version)
		}
	}

	section := unreleased[0]
	notes := strings.Trim(contents[section.bodyStart:section.bodyEnd], "\r\n")
	if !hasNotes(notes) {
		return fmt.Errorf("## Unreleased has no release notes to promote")
	}
	promoted := contents[:section.headingEnd] + "\n\n## " + version + " - " + date + "\n\n" + notes + "\n\n" + contents[section.bodyEnd:]
	if err := writeFileAtomic(path, []byte(promoted), mode); err != nil {
		return fmt.Errorf("write changelog: %w", err)
	}
	return nil
}

// ChangelogReleaseNotes returns the body of the one release section matching
// version. It rejects duplicate, malformed, and empty matching sections.
func ChangelogReleaseNotes(path, version string) (string, error) {
	if err := validateVersion(version); err != nil {
		return "", err
	}
	contents, _, err := readChangelog(path)
	if err != nil {
		return "", err
	}
	sections := changelogSections(contents)
	if err := validateUniqueVersions(sections); err != nil {
		return "", err
	}
	var matches []changelogSection
	for _, section := range sections {
		if sectionVersion(section.title) == version {
			matches = append(matches, section)
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("changelog must contain exactly one release section for version %s; found %d", version, len(matches))
	}
	section := matches[0]
	wantPrefix := version + " - "
	date := strings.TrimPrefix(section.title, wantPrefix)
	if !strings.HasPrefix(section.title, wantPrefix) || validateDate(date) != nil {
		return "", fmt.Errorf("release section for version %s must use heading ## %s - YYYY-MM-DD", version, version)
	}
	notes := strings.Trim(contents[section.bodyStart:section.bodyEnd], "\r\n")
	if !hasNotes(notes) {
		return "", fmt.Errorf("release section for version %s has no release notes", version)
	}
	return notes + "\n", nil
}

// WriteChangelogReleaseNotes validates and writes one version's notes without
// allowing the output to replace the source changelog.
func WriteChangelogReleaseNotes(changelogPath, outputPath, version string) error {
	same, err := sameFile(changelogPath, outputPath)
	if err != nil {
		return err
	}
	if same {
		return fmt.Errorf("release notes output must not overwrite changelog %s", changelogPath)
	}
	notes, err := ChangelogReleaseNotes(changelogPath, version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, []byte(notes), 0o644); err != nil {
		return fmt.Errorf("write release notes: %w", err)
	}
	return nil
}

func sameFile(first, second string) (bool, error) {
	firstAbsolute, err := filepath.Abs(first)
	if err != nil {
		return false, fmt.Errorf("resolve changelog path: %w", err)
	}
	secondAbsolute, err := filepath.Abs(second)
	if err != nil {
		return false, fmt.Errorf("resolve release notes path: %w", err)
	}
	if filepath.Clean(firstAbsolute) == filepath.Clean(secondAbsolute) {
		return true, nil
	}
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false, fmt.Errorf("read changelog %s: %w", first, err)
	}
	secondInfo, err := os.Stat(second)
	if err == nil {
		return os.SameFile(firstInfo, secondInfo), nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect release notes output %s: %w", second, err)
	}
	return false, nil
}

func validateVersion(version string) error {
	if !isSemanticVersion(version) {
		return fmt.Errorf("version %q must be a bare semantic version such as 1.2.3 or 1.2.3-rc.1", version)
	}
	return nil
}

func isSemanticVersion(version string) bool {
	return semanticVersion.MatchString(version) && !hasInvalidNumericIdentifier(version)
}

func hasInvalidNumericIdentifier(version string) bool {
	withoutBuild, _, _ := strings.Cut(version, "+")
	core, prerelease, _ := strings.Cut(withoutBuild, "-")
	identifiers := strings.Split(core, ".")
	if prerelease != "" {
		identifiers = append(identifiers, strings.Split(prerelease, ".")...)
	}
	for _, identifier := range identifiers {
		if len(identifier) > 1 && identifier[0] == '0' && strings.IndexFunc(identifier, func(character rune) bool {
			return character < '0' || character > '9'
		}) == -1 {
			return true
		}
	}
	return false
}

func validateDate(date string) error {
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil || parsed.Format(time.DateOnly) != date {
		return fmt.Errorf("date %q must be a valid date in YYYY-MM-DD format", date)
	}
	return nil
}

func readChangelog(path string) (string, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("read changelog %s: %w", path, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read changelog %s: %w", path, err)
	}
	return string(contents), info.Mode().Perm(), nil
}

func changelogSections(contents string) []changelogSection {
	var sections []changelogSection
	fenceCharacter := byte(0)
	fenceLength := 0
	for offset := 0; offset < len(contents); {
		lineEnd := strings.IndexByte(contents[offset:], '\n')
		if lineEnd == -1 {
			lineEnd = len(contents)
		} else {
			lineEnd += offset
		}
		line := strings.TrimSuffix(contents[offset:lineEnd], "\r")
		character, length, rest, isFence := markdownFence(line)
		if fenceCharacter != 0 {
			if isFence && character == fenceCharacter && length >= fenceLength && strings.TrimSpace(rest) == "" {
				fenceCharacter = 0
				fenceLength = 0
			}
		} else if isFence {
			fenceCharacter = character
			fenceLength = length
		} else if title, ok := levelTwoHeading(line); ok {
			if len(sections) > 0 {
				sections[len(sections)-1].bodyEnd = offset
			}
			sections = append(sections, changelogSection{
				title:      title,
				headingEnd: lineEnd,
				bodyStart:  lineEnd,
				bodyEnd:    len(contents),
			})
		}
		if lineEnd == len(contents) {
			break
		}
		offset = lineEnd + 1
	}
	return sections
}

func markdownFence(line string) (byte, int, string, bool) {
	indent := 0
	for indent < len(line) && indent < 3 && line[indent] == ' ' {
		indent++
	}
	line = line[indent:]
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, "", false
	}
	character := line[0]
	length := 0
	for length < len(line) && line[length] == character {
		length++
	}
	if length < 3 {
		return 0, 0, "", false
	}
	return character, length, line[length:], true
}

func levelTwoHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "##") || len(line) < 3 || (line[2] != ' ' && line[2] != '\t') {
		return "", false
	}
	title := strings.TrimSpace(line[3:])
	if title == "" {
		return "", false
	}
	return title, true
}

func sectionsNamed(sections []changelogSection, title string) []changelogSection {
	var matches []changelogSection
	for _, section := range sections {
		if section.title == title {
			matches = append(matches, section)
		}
	}
	return matches
}

func sectionVersion(title string) string {
	fields := strings.Fields(title)
	if len(fields) == 0 || !isSemanticVersion(fields[0]) {
		return ""
	}
	return fields[0]
}

func validateUniqueVersions(sections []changelogSection) error {
	seen := make(map[string]bool)
	for _, section := range sections {
		version := sectionVersion(section.title)
		if version == "" {
			continue
		}
		if seen[version] {
			return fmt.Errorf("changelog contains duplicate release sections for version %s", version)
		}
		seen[version] = true
	}
	return nil
}

func hasNotes(notes string) bool {
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		categoryHeading := strings.HasPrefix(line, "###") && (len(line) == 3 || line[3] == ' ' || line[3] == '\t')
		if line != "" && !categoryHeading {
			return true
		}
	}
	return false
}

func writeFileAtomic(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".changelog-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
