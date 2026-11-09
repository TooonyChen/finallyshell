package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func commits(subjects ...string) []Commit {
	var cs []Commit
	for _, s := range subjects {
		subject, body, _ := strings.Cut(s, "\n\n")
		cs = append(cs, ParseCommit(subject, body))
	}
	return cs
}

func TestParseCommit(t *testing.T) {
	for _, tc := range []struct {
		subject, body string
		want          Commit
	}{
		{"feat(app): add tabs (#3)", "", Commit{Type: "feat", Scope: "app", Description: "add tabs (#3)"}},
		{"fix: crash", "", Commit{Type: "fix", Description: "crash"}},
		{"feat(sshx)!: drop v1 keys", "", Commit{Type: "feat", Scope: "sshx", Description: "drop v1 keys", Breaking: true}},
		{"refactor!: rename config", "", Commit{Type: "refactor", Description: "rename config", Breaking: true}},
		{"Feat: capital type", "", Commit{Type: "feat", Description: "capital type"}},
		{"fix(config): new format", "Explain.\n\nBREAKING CHANGE: hosts.json v3", Commit{Type: "fix", Scope: "config", Description: "new format", Breaking: true}},
		{"fix: tidy", "BREAKING-CHANGE: yes", Commit{Type: "fix", Description: "tidy", Breaking: true}},
		{"fix: mention", "not a BREAKING CHANGE: footer", Commit{Type: "fix", Description: "mention"}},
		{"改名为 finalshelly", "", Commit{}},
		{"Merge branch 'main'", "", Commit{}},
	} {
		got := ParseCommit(tc.subject, tc.body)
		tc.want.Subject = tc.subject
		if got != tc.want {
			t.Errorf("ParseCommit(%q, %q) = %+v, want %+v", tc.subject, tc.body, got, tc.want)
		}
	}
}

func TestBumpOf(t *testing.T) {
	for _, tc := range []struct {
		commits []Commit
		want    Bump
	}{
		{nil, None},
		{commits("docs: readme", "chore: deps", "test: more", "ci: cache", "build: flags", "style: fmt", "refactor: split"), None},
		{commits("初版", "Merge pull request #1"), None},
		{commits("docs: readme", "fix: crash"), Patch},
		{commits("perf: faster list"), Patch},
		{commits("fix: crash", "feat: tabs", "fix: other"), Minor},
		{commits("feat: tabs", "chore!: drop go 1.26"), Major},
		{commits("docs: x\n\nBREAKING CHANGE: y"), Major},
		{commits("not conventional\n\nBREAKING CHANGE: y"), None},
	} {
		if got := BumpOf(tc.commits); got != tc.want {
			t.Errorf("BumpOf(%v) = %v, want %v", tc.commits, got, tc.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	for _, s := range []string{"0.2.0", "v1.10.3"} {
		if _, err := ParseVersion(s); err != nil {
			t.Errorf("ParseVersion(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "1.2", "1.2.3.4", "1.2.3-rc.1", "01.2.3", "a.b.c", "-1.0.0"} {
		if v, err := ParseVersion(s); err == nil {
			t.Errorf("ParseVersion(%q) = %v, want an error", s, v)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"0.2.1", "0.2.1", 0},
		{"0.2.1", "0.3.0", -1},
		{"0.10.0", "0.9.9", 1},
		{"1.0.0", "0.99.99", 1},
		{"1.2.3", "1.2.4", -1},
	} {
		a, _ := ParseVersion(tc.a)
		b, _ := ParseVersion(tc.b)
		if got := a.Compare(b); got != tc.want {
			t.Errorf("%s.Compare(%s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// bilingual is a section's notes in both languages.
const bilingual = "### English\n\n- Tabs.\n\n### 简体中文\n\n- 标签页。"

func TestParseChangelog(t *testing.T) {
	text := "# Changelog\n\nIntro, not a section.\n\n## 0.3.0\n\n" + bilingual +
		"\n\n## [0.2.1] - 2026-11-08\r\n\r\n### English\r\n\r\n- Fix.\r\n\r\n## v0.2.0\n\nOld notes.\n"
	sections, err := ParseChangelog("CHANGELOG.md", text)
	if err != nil {
		t.Fatal(err)
	}
	want := []Section{
		{Version{0, 3, 0}, 5, bilingual},
		{Version{0, 2, 1}, 15, "### English\n\n- Fix."},
		{Version{0, 2, 0}, 21, "Old notes."},
	}
	if len(sections) != len(want) {
		t.Fatalf("ParseChangelog = %+v, want %+v", sections, want)
	}
	for i := range want {
		if sections[i] != want[i] {
			t.Errorf("section %d = %+v, want %+v", i, sections[i], want[i])
		}
	}
	if s, ok := Find(sections, Version{0, 2, 1}); !ok || s.Line != 15 {
		t.Errorf("Find 0.2.1 = %+v, %v", s, ok)
	}
	if _, ok := Find(sections, Version{0, 1, 0}); ok {
		t.Error("Find 0.1.0 found a section")
	}

	for _, tc := range []struct{ name, text, err string }{
		{"no sections", "# Changelog\n\nSoon.\n", "CHANGELOG.md has no version section"},
		{"empty", "", "CHANGELOG.md has no version section"},
		{"unreleased", "# Changelog\n\n## Unreleased\n\n- x\n", `CHANGELOG.md:3: "## Unreleased" is not a version`},
		{"two parts", "## 0.2\n", `CHANGELOG.md:1: "## 0.2" is not a version`},
		{"prerelease", "## 0.3.0-beta.1\n", "is not a version"},
		{"no heading text", "## 0.2.1\n\n## \n", `CHANGELOG.md:3: "## " is not a version`},
		{"oldest first", "## 0.2.0\n\nx\n\n## 0.2.1\n\ny\n", "CHANGELOG.md:5: 0.2.1 comes after 0.2.0"},
		{"twice", "## 0.2.1\n\nx\n\n## 0.2.1\n\ny\n", "CHANGELOG.md:5: 0.2.1 comes after 0.2.1"},
	} {
		if s, err := ParseChangelog("CHANGELOG.md", tc.text); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: ParseChangelog = %+v, %v, want an error with %q", tc.name, s, err, tc.err)
		}
	}
}

func TestCheck(t *testing.T) {
	v := Version{0, 2, 1}
	if err := Check("CHANGELOG.md", Section{Version: v, Notes: bilingual}); err != nil {
		t.Errorf("Check of both languages: %v", err)
	}
	if err := Check("CHANGELOG.md", Section{Version: v, Notes: "Intro.\n\n### 简体中文\n- 修复\n### English\n- Fix\n#### More\n- more"}); err != nil {
		t.Errorf("Check of Chinese first: %v", err)
	}
	for _, tc := range []struct{ notes, err string }{
		{"", `0.2.1 has no "### English" part`},
		{"- Fix.", `0.2.1 has no "### English" part`},
		{"### English\n\n- Fix.", `0.2.1 has no "### 简体中文" part`},
		{"### English\n\n### 简体中文\n\n- 修复。", `the "### English" part of 0.2.1 is empty`},
		{"### English\n\n- Fix.\n\n### 简体中文\n\n", `the "### 简体中文" part of 0.2.1 is empty`},
		{bilingual + "\n\n### English\n\n- Again.", `0.2.1 has two "### English" parts`},
	} {
		if err := Check("CHANGELOG.md", Section{Version: v, Notes: tc.notes}); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("Check(%q) = %v, want an error with %q", tc.notes, err, tc.err)
		}
	}
}

func TestRelease(t *testing.T) {
	changelog := func(versions ...string) []Section {
		var text strings.Builder
		for _, v := range versions {
			text.WriteString("## " + v + "\n\n" + bilingual + "\n\n")
		}
		s, err := ParseChangelog("CHANGELOG.md", text.String())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, tc := range []struct {
		name     string
		sections []Section
		last     string
		commits  []Commit
		want     string
		warning  string
	}{
		{"first release", changelog("0.2.0"), "", commits("feat: a"), "0.2.0", ""},
		{"newer", changelog("0.2.1", "0.2.0"), "v0.2.0", commits("fix: a"), "0.2.1", ""},
		{"newer than the commits call for", changelog("0.3.0"), "v0.2.0", commits("fix: a"), "0.3.0", ""},
		{"newer with commits of no bump", changelog("0.2.1"), "v0.2.0", commits("docs: a"), "0.2.1", ""},
		{"newer with no commits", changelog("0.2.1"), "v0.2.0", nil, "0.2.1", ""},
		{"same", changelog("0.2.1", "0.2.0"), "v0.2.1", commits("feat: a"), "", ""},
		{"lower than a feat", changelog("0.2.1"), "v0.2.0", commits("fix: a", "feat: b"), "0.2.1",
			"0.2.1 is lower than the commits since v0.2.0 call for: 0.3.0 (minor)"},
		{"lower than a breaking change", changelog("1.3.0"), "v1.2.0", commits("feat!: a"), "1.3.0",
			"1.3.0 is lower than the commits since v1.2.0 call for: 2.0.0 (major)"},
		{"breaking before 1.0", changelog("0.3.0"), "v0.2.0", commits("feat!: a"), "0.3.0", ""},
	} {
		got, warning, err := Release("CHANGELOG.md", tc.sections, tc.last, tc.commits)
		if err != nil || got != tc.want || warning != tc.warning {
			t.Errorf("%s: Release = %q, %q, %v, want %q, %q", tc.name, got, warning, err, tc.want, tc.warning)
		}
	}

	older := changelog("0.2.0")
	if v, _, err := Release("CHANGELOG.md", older, "v0.2.1", nil); err == nil ||
		err.Error() != "CHANGELOG.md:1: the newest section, 0.2.0, is older than the last release, v0.2.1" {
		t.Errorf("Release of an older section = %q, %v", v, err)
	}
	english, err := ParseChangelog("CHANGELOG.md", "## 0.2.1\n\n- Fix.\n")
	if err != nil {
		t.Fatal(err)
	}
	if v, _, err := Release("CHANGELOG.md", english, "v0.2.0", nil); err == nil || !strings.Contains(err.Error(), `no "### English" part`) {
		t.Errorf("Release of a section in one language = %q, %v", v, err)
	}
	if v, _, err := Release("CHANGELOG.md", english, "v0.2.1", nil); err != nil || v != "" {
		t.Errorf("Release of a released section = %q, %v; it is not checked again", v, err)
	}
	if v, _, err := Release("CHANGELOG.md", changelog("0.2.1"), "vX", nil); err == nil {
		t.Errorf("Release after a bad tag = %q, no error", v)
	}
}

// TestChangelogFile checks the repository's CHANGELOG.md, so a pull
// request that breaks it fails its checks, not the release.
func TestChangelogFile(t *testing.T) {
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	sections, err := ParseChangelog("CHANGELOG.md", string(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check("CHANGELOG.md", sections[0]); err != nil {
		t.Error(err)
	}
}

func TestDraft(t *testing.T) {
	got := Draft(commits(
		"fix(app): crash on close (#9)",
		"docs: readme",
		"feat(sshx)!: new key format (#8)",
		"feat: tabs (#7)",
		"perf(monitor): fewer polls",
		"fix: typo",
	))
	want := `## Breaking changes

- sshx: new key format (#8)

## Features

- sshx: new key format (#8)
- tabs (#7)

## Fixes

- app: crash on close (#9)
- typo

## Performance

- monitor: fewer polls
`
	if got != want {
		t.Errorf("Draft =\n%s\nwant\n%s", got, want)
	}
	if got := Draft(commits("docs: a")); got != "No user-facing changes.\n" {
		t.Errorf("Draft of docs only = %q", got)
	}
}

// TestGitHistory runs the git and file side on a scratch repository.
func TestGitHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Chdir(t.TempDir())
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(msg string) { run("commit", "--allow-empty", "-q", "-m", msg) }
	// section adds a version's section on top of CHANGELOG.md and commits it.
	section := func(v, msg string) {
		t.Helper()
		old, _ := os.ReadFile(changelog)
		text := "## " + v + "\n\n" + bilingual + "\n\n" + strings.TrimPrefix(string(old), "# Changelog\n\n")
		if err := os.WriteFile(changelog, []byte("# Changelog\n\n"+text), 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", changelog)
		commit(msg)
	}
	next := func() (string, string) {
		t.Helper()
		sections, err := readChangelog()
		if err != nil {
			t.Fatal(err)
		}
		last, err := lastTag()
		if err != nil {
			t.Fatal(err)
		}
		cs, err := commitsSince(last)
		if err != nil {
			t.Fatal(err)
		}
		v, warning, err := Release(changelog, sections, last, cs)
		if err != nil {
			t.Fatal(err)
		}
		return v, warning
	}

	run("init", "-q", "-b", "main")
	commit("初版")
	section("0.2.0", "feat: first (#1)")
	if v, _ := next(); v != "0.2.0" {
		t.Fatalf("first release = %q, want 0.2.0", v)
	}
	run("tag", "v0.2.0")
	if v, _ := next(); v != "" {
		t.Fatalf("tagged HEAD releases %q", v)
	}
	run("tag", "not-a-release")
	commit("fix(app): crash\n\nDetails.")
	commit("feat(sshx): agent")
	if v, _ := next(); v != "" {
		t.Fatalf("commits without a section release %q", v)
	}
	section("0.2.1", "docs: changelog for 0.2.1")
	if v, warning := next(); v != "0.2.1" || !strings.Contains(warning, "0.3.0 (minor)") {
		t.Fatalf("after the 0.2.1 section = %q, %q, want 0.2.1 and a warning", v, warning)
	}
	cs, err := commitsSince("v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 || cs[1].Scope != "sshx" || cs[2].Description != "crash" {
		t.Fatalf("commitsSince = %+v", cs)
	}
	run("tag", "v0.2.1")
	if v, _ := next(); v != "" {
		t.Fatalf("re-running after the release releases %q", v)
	}
	run("tag", "v0.10.0", "HEAD~1") // newest by version, not by name
	commit("fix: later")
	if last, _ := lastTag(); last != "v0.10.0" {
		t.Fatalf("lastTag = %q, want v0.10.0", last)
	}
}
func TestThirdPartyLicenses(t *testing.T) {
	if testing.Short() {
		t.Skip("lists the app's modules")
	}
	var b strings.Builder
	if err := ThirdPartyLicenses("../..", &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"Go (https://go.dev)",
		"github.com/egoist/mygo ",
		"github.com/pkg/sftp ",
		"golang.org/x/crypto ",
		"libghostty-vt",
		"Mitchell Hashimoto",
		"under the FreeType License",
		"The FreeType Project LICENSE",
		"Lucide icons",
		"LobeHub Icons",
		"Copyright (c) 2023 LobeHub",
		"VS Code icon theme material-icon-theme",
		"VS Code icon theme seti",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("third-party licenses lack %q", want)
		}
	}
	if strings.Contains(out, "\nfinallyshell ") {
		t.Error("third-party licenses list the app itself")
	}
}
