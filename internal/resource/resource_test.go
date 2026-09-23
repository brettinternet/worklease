package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestValidationRejectsUnsafeResourceInputs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		valid   func() error
		wantErr string
	}{
		{name: "resource edge whitespace", valid: func() error { _, err := validateResource(" resource "); return err }, wantErr: "must not start or end with whitespace"},
		{name: "resource control char", valid: func() error { _, err := validateResource("resource\x01"); return err }, wantErr: "must not contain control characters"},
		{name: "resource whitespace-only", valid: func() error { _, err := validateResource(" "); return err }, wantErr: "must not start or end with whitespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.valid(); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
	if _, err := ValidateIdentity("item", " item "); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
	if _, err := validateResource("ok:source"); err != nil {
		t.Fatalf("valid resource rejected: %v", err)
	}
}

func TestRFC3986EncodingAndCanonicalCoordinationKey(t *testing.T) {
	if got := RFC3986Encode("a:b#c/d% e~"); got != "a%3Ab%23c%2Fd%25%20e~" {
		t.Fatalf("encoding = %q", got)
	}
	first, err := Resolve(Input{Provider: "generic", Source: "source:a", Item: "item/b#c"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(Input{Provider: "generic", Source: "source", Item: "a:item/b#c"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Resource == second.Resource {
		t.Fatal("delimiter-containing identities collided")
	}
	githubA, err := Resolve(Input{Provider: "github", Source: "owner/repo", Item: "a#b"})
	if err != nil {
		t.Fatal(err)
	}
	githubB, err := Resolve(Input{Provider: "github", Source: "owner/repo#a", Item: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if githubA.Resource == githubB.Resource {
		t.Fatal("GitHub delimiter-containing identities collided")
	}
	wantJSON := `{"item":"item/b#c","provider":"generic","source":"source:a"}`
	want := sha256Hex([]byte(wantJSON))
	if !strings.HasSuffix(first.Resource, want) {
		t.Fatalf("resource = %q, want hash %s", first.Resource, want)
	}
}

func sha256Hex(data []byte) string {
	// Keep the fixture's hash calculation independent from policy internals.
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestVersionedKeyVectors(t *testing.T) {
	fixtureBytes, err := os.ReadFile("testdata/key-vectors-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Source   string `json:"source"`
		Item     string `json:"item"`
		Resource string `json:"resource"`
	}
	var fixture struct {
		Version          int      `json:"version"`
		KeyPolicyVersion int      `json:"keyPolicyVersion"`
		Vectors          []vector `json:"vectors"`
		InvalidVectors   []vector `json:"invalidVectors"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || fixture.KeyPolicyVersion != KeyPolicyVersion || KeyPolicyVersion != 1 {
		t.Fatalf("fixture/policy versions = %d/%d, implementation = %d; want 1", fixture.Version, fixture.KeyPolicyVersion, KeyPolicyVersion)
	}
	if len(fixture.Vectors) == 0 || len(fixture.InvalidVectors) == 0 {
		t.Fatal("fixture must contain positive and negative vectors")
	}

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, "docs", "backlog"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "--quiet")
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "fixture")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "--quiet", "--detach", linked, "HEAD")
	common, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	replacer := strings.NewReplacer("${PRIMARY}", repo, "${LINKED}", linked, "${COMMON_DIR}", RFC3986Encode(common))
	for _, vector := range fixture.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			key, err := Resolve(Input{Provider: vector.Provider, Source: replacer.Replace(vector.Source), Item: vector.Item})
			if err != nil {
				t.Fatal(err)
			}
			want := replacer.Replace(vector.Resource)
			if key.Resource != want {
				t.Fatalf("resource = %q, want %q", key.Resource, want)
			}
		})
	}
	for _, vector := range fixture.InvalidVectors {
		t.Run(vector.Name, func(t *testing.T) {
			if _, err := Resolve(Input{Provider: vector.Provider, Source: vector.Source, Item: vector.Item}); err == nil {
				t.Fatal("invalid vector was accepted")
			}
		})
	}
}

func TestStaticPolicyGoldenDerivations(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "--quiet")
	if err := os.Mkdir(filepath.Join(repo, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "docs", "source.md")
	if err := os.WriteFile(file, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	common := RFC3986Encode(filepath.Join(resolvedRepo, ".git"))
	tests := []struct {
		name string
		in   Input
		want string
	}{
		{"backlog-md", Input{Provider: "backlog-md", Source: filepath.Join(repo, "docs"), Item: "ITEM:1"}, "backlog-md:" + common + ":docs:ITEM%3A1"},
		{"markdown", Input{Provider: "markdown", Source: file, Item: "ignored:item"}, "markdown:" + common + ":docs%2Fsource.md:__source__"},
		{"github", Input{Provider: "github", Source: "Owner/Repo.GIT", Item: "ITEM:1"}, "github:owner%2Frepo#ITEM%3A1"},
		{"linear", Input{Provider: "linear", Source: "Team/One", Item: "ITEM:1"}, "coordination:linear:87307cd8e0948bb7ce7f928f38d4e35133c2ea903c2b2ab12553e498e9028e04"},
		{"generic", Input{Provider: "generic", Source: "Team/One", Item: "ITEM:1"}, "coordination:generic:dc0294c2ce98dc5b852df6f629dc7d314906783f84b3563f163311ed54f9b985"},
		{"path", Input{Path: file}, "path:" + common + ":docs%2Fsource.md"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(test.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Resource != test.want {
				t.Fatalf("resource = %q, want %q", got.Resource, test.want)
			}
		})
	}
}

func TestAllStaticPoliciesAndMetadata(t *testing.T) {
	for _, name := range Names() {
		p, err := Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		in := Input{Provider: name, Source: "owner/repo", Item: "ITEM:1"}
		if name == "backlog-md" || name == "markdown" {
			in.Source = t.TempDir()
		}
		if name == "path" {
			continue
		}
		key, err := p.Key(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if key.Provider != name || key.Resource == "" || key.ProviderFencing {
			t.Fatalf("%s: %+v", name, key)
		}
		d := p.Describe()
		if d.Name != name || d.ContractVersion != 1 || d.KeyPolicyVersion != 1 {
			t.Fatalf("descriptor %s: %+v", name, d)
		}
		if name == "backlog-md" && !strings.Contains(d.Resource, "<item>") {
			t.Fatalf("backlog descriptor omits item: %+v", d)
		}
	}
}

func TestLinkedWorktreesAndSymlinkMissingLeaf(t *testing.T) {
	root := t.TempDir()
	oldGitDir, hadGitDir := os.LookupEnv("GIT_DIR")
	_ = os.Setenv("GIT_DIR", filepath.Join(root, "not-the-repository"))
	t.Cleanup(func() {
		if hadGitDir {
			_ = os.Setenv("GIT_DIR", oldGitDir)
		} else {
			_ = os.Unsetenv("GIT_DIR")
		}
	})
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "--quiet")
	os.MkdirAll(filepath.Join(repo, "docs"), 0755)
	os.WriteFile(filepath.Join(repo, "docs", "source.md"), []byte("x"), 0644)
	git(t, repo, "add", ".")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "init")
	linked := filepath.Join(root, "linked")
	git(t, repo, "worktree", "add", "--quiet", "--detach", linked, "HEAD")
	mainKey, err := Resolve(Input{Provider: "markdown", Source: filepath.Join(repo, "docs", "source.md"), Item: "x"})
	if err != nil {
		t.Fatal(err)
	}
	relativeKey, err := Resolve(Input{Provider: "markdown", Source: "docs/source.md", Item: "x", WorkingDir: repo})
	if err != nil {
		t.Fatal(err)
	}
	if relativeKey.Resource != mainKey.Resource {
		t.Fatalf("working directory was ignored: %q != %q", relativeKey.Resource, mainKey.Resource)
	}
	linkedKey, err := Resolve(Input{Provider: "markdown", Source: filepath.Join(linked, "docs", "source.md"), Item: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if mainKey.Resource != linkedKey.Resource {
		t.Fatalf("linked resources differ: %q != %q", mainKey.Resource, linkedKey.Resource)
	}
	mainPath, err := Resolve(Input{Path: filepath.Join(repo, "docs", "source.md")})
	if err != nil {
		t.Fatal(err)
	}
	linkedPath, err := Resolve(Input{Path: filepath.Join(linked, "docs", "source.md")})
	if err != nil {
		t.Fatal(err)
	}
	if mainPath.Resource != linkedPath.Resource {
		t.Fatalf("linked path resources differ: %q != %q", mainPath.Resource, linkedPath.Resource)
	}
	os.Symlink(filepath.Join(repo, "docs"), filepath.Join(repo, "docs-alias"))
	aliasPath, err := Resolve(Input{Path: filepath.Join(repo, "docs-alias", "new.md")})
	if err != nil {
		t.Fatal(err)
	}
	canonicalPathKey, err := Resolve(Input{Path: filepath.Join(repo, "docs", "new.md")})
	if err != nil {
		t.Fatal(err)
	}
	if aliasPath.Resource != canonicalPathKey.Resource {
		t.Fatalf("path symlink/missing leaf differ: %q != %q", aliasPath.Resource, canonicalPathKey.Resource)
	}
	os.Symlink(filepath.Join(repo, "docs"), filepath.Join(root, "docs-alias"))
	missing := filepath.Join(root, "docs-alias", "new.md")
	aliasKey, err := Resolve(Input{Provider: "markdown", Source: missing, Item: "x"})
	if err != nil {
		t.Fatal(err)
	}
	missingKey, err := Resolve(Input{Provider: "markdown", Source: filepath.Join(repo, "docs", "new.md"), Item: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if aliasKey.Resource != missingKey.Resource {
		t.Fatalf("symlink/missing leaf differ: %q != %q", aliasKey.Resource, missingKey.Resource)
	}
	otherRepo := filepath.Join(root, "other")
	os.Mkdir(otherRepo, 0755)
	git(t, otherRepo, "init", "--quiet")
	otherKey, err := Resolve(Input{Provider: "markdown", Source: filepath.Join(otherRepo, "docs", "source.md"), Item: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if otherKey.Resource == mainKey.Resource {
		t.Fatal("unrelated repositories share a resource")
	}
}

func TestPathCanonicalContainmentAndExactSemantics(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0755)
	git(t, repo, "init", "--quiet")
	os.WriteFile(filepath.Join(repo, "a"), []byte("a"), 0644)
	one, err := Resolve(Input{Path: filepath.Join(repo, "a")})
	if err != nil {
		t.Fatal(err)
	}
	child, err := Resolve(Input{Path: filepath.Join(repo, "a", "child")})
	if err == nil {
		t.Fatal("file child unexpectedly accepted")
	}
	if child.Resource != "" {
		t.Fatal("invalid result should be empty")
	}
	other, err := Resolve(Input{Path: filepath.Join(repo, "b")})
	if err != nil {
		t.Fatal(err)
	}
	if one.Resource == other.Resource {
		t.Fatal("different exact paths collided")
	}
	for _, globLike := range []string{"a*", "a?"} {
		globKey, err := Resolve(Input{Path: filepath.Join(repo, globLike)})
		if err != nil {
			t.Fatal(err)
		}
		if globKey.Resource == one.Resource || globKey.Resource == other.Resource {
			t.Fatalf("glob-like path overlapped: %q", globLike)
		}
	}
	if _, err := Resolve(Input{Path: filepath.Join(repo, "..", "outside")}); err == nil {
		t.Fatal("traversal accepted")
	}
	if _, err := Resolve(Input{Path: filepath.Join(root, "outside")}); err == nil {
		t.Fatal("non-Git path accepted")
	}
	outsideFile := filepath.Join(root, "outside.md")
	os.WriteFile(outsideFile, []byte("outside"), 0644)
	os.Symlink(outsideFile, filepath.Join(repo, "escape"))
	if _, err := Resolve(Input{Path: filepath.Join(repo, "escape")}); err == nil {
		t.Fatal("symlink escape accepted")
	}
	otherRepo := filepath.Join(root, "other-repo")
	os.Mkdir(otherRepo, 0755)
	git(t, otherRepo, "init", "--quiet")
	otherFile := filepath.Join(otherRepo, "other.md")
	os.WriteFile(otherFile, []byte("other"), 0644)
	os.Symlink(otherFile, filepath.Join(repo, "other-repo-file"))
	if _, err := Resolve(Input{Path: filepath.Join(repo, "other-repo-file")}); err == nil {
		t.Fatal("symlink into a different Git common dir accepted")
	}
	if _, err := Resolve(Input{Path: repo}); err == nil {
		t.Fatal("directory path accepted")
	}
}

func TestPathPreservesGitRootWhitespace(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo ")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "--quiet")
	file := filepath.Join(repo, "file")
	if err := os.WriteFile(file, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	key, err := Resolve(Input{Path: file})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(key.Resource, "repo%20%2F.git") {
		t.Fatalf("Git root whitespace was lost: %q", key.Resource)
	}
}

func TestValidationAndDeterministicRegistry(t *testing.T) {
	exact, err := Resolve(Input{Provider: "generic", Source: " source/ ", Item: " item "})
	if err != nil {
		t.Fatal(err)
	}
	if exact.Source != " source/ " || exact.Item != " item " {
		t.Fatalf("identity bytes were changed: %+v", exact)
	}
	trailing, err := Resolve(Input{Provider: "linear", Source: "team/", Item: "issue"})
	if err != nil {
		t.Fatal(err)
	}
	withoutTrailing, err := Resolve(Input{Provider: "linear", Source: "team", Item: "issue"})
	if err != nil {
		t.Fatal(err)
	}
	if trailing.Resource == withoutTrailing.Resource {
		t.Fatal("linear trailing slash was normalized")
	}
	genericTrailing, err := Resolve(Input{Provider: "generic", Source: "team/", Item: "issue"})
	if err != nil {
		t.Fatal(err)
	}
	genericWithoutTrailing, err := Resolve(Input{Provider: "generic", Source: "team", Item: "issue"})
	if err != nil {
		t.Fatal(err)
	}
	if genericTrailing.Resource == genericWithoutTrailing.Resource {
		t.Fatal("generic trailing slash was normalized")
	}
	if got := Names(); strings.Join(got, ",") != "backlog-md,generic,github,linear,markdown,path" {
		t.Fatal(got)
	}
	if _, err := Lookup("unknown"); err == nil || !strings.Contains(err.Error(), "unknown policy") {
		t.Fatal(err)
	}
	for _, value := range []string{"", " ", "a\n", strings.Repeat("x", 1025)} {
		if _, err := Direct(value, false); err == nil {
			t.Fatalf("accepted invalid resource %q", value)
		}
	}
	for _, value := range []string{"", " \t", "a\x01", strings.Repeat("x", 1025)} {
		if _, err := Resolve(Input{Provider: "generic", Source: value, Item: "item"}); err == nil {
			t.Fatalf("accepted invalid source %q", value)
		}
	}
	b, _ := json.Marshal(Descriptors())
	if !json.Valid(b) {
		t.Fatal("invalid descriptor JSON")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := testkit.GitCommand(append([]string{"-C", dir}, args...)...)
	cmd.Env = withoutGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out)
	}
}
func withoutGitEnv() []string {
	out := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			out = append(out, v)
		}
	}
	return out
}
