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

func TestNextVersion(t *testing.T) {
	for _, tc := range []struct {
		name, last, base string
		commits          []Commit
		tagged           bool
		want             string
	}{
		{"first release is mygo.json", "", "0.2.0", commits("chore: x"), false, "0.2.0"},
		{"feat", "v0.2.0", "0.2.0", commits("feat: a"), false, "0.3.0"},
		{"fix", "v0.3.0", "0.2.0", commits("fix: a", "docs: b"), false, "0.3.1"},
		{"breaking before 1.0", "v0.3.1", "0.2.0", commits("feat!: a"), false, "0.4.0"},
		{"breaking from 1.0", "v1.4.2", "0.2.0", commits("fix!: a"), false, "2.0.0"},
		{"feat from 1.0", "v1.4.2", "0.2.0", commits("feat: a"), false, "1.5.0"},
		{"nothing to release", "v0.3.0", "0.2.0", commits("docs: a", "chore: b"), false, ""},
		{"no commits", "v0.3.0", "0.2.0", nil, false, ""},
		{"already tagged", "v0.3.0", "0.2.0", commits("feat: a"), true, ""},
		{"already tagged first", "", "0.2.0", nil, true, ""},
	} {
		got, err := NextVersion(tc.last, tc.base, tc.commits, tc.tagged)
		if err != nil || got != tc.want {
			t.Errorf("%s: NextVersion = %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
	if _, err := NextVersion("", "0.2", nil, false); err == nil {
		t.Error("NextVersion with a bad mygo.json version: no error")
	}
}

func TestNotes(t *testing.T) {
	got := Notes(commits(
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
		t.Errorf("Notes =\n%s\nwant\n%s", got, want)
	}
	if got := Notes(commits("docs: a")); got != "No user-facing changes.\n" {
		t.Errorf("Notes of docs only = %q", got)
	}
}

// TestGitHistory runs the git side on a scratch repository.
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
	next := func() string {
		t.Helper()
		last, err := lastTag()
		if err != nil {
			t.Fatal(err)
		}
		tagged, err := headTagged()
		if err != nil {
			t.Fatal(err)
		}
		cs, err := commitsSince(last)
		if err != nil {
			t.Fatal(err)
		}
		v, err := NextVersion(last, "0.2.0", cs, tagged)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	run("init", "-q", "-b", "main")
	commit("初版")
	commit("feat: first (#1)")
	if v := next(); v != "0.2.0" {
		t.Fatalf("first release = %q, want 0.2.0", v)
	}
	run("tag", "v0.2.0")
	if v := next(); v != "" {
		t.Fatalf("tagged HEAD releases %q", v)
	}
	run("tag", "not-a-release")
	commit("docs: readme")
	if v := next(); v != "" {
		t.Fatalf("docs only releases %q", v)
	}
	commit("fix(app): crash\n\nDetails.")
	if v := next(); v != "0.2.1" {
		t.Fatalf("after a fix = %q, want 0.2.1", v)
	}
	commit("feat(sshx): agent\n\nBREAKING CHANGE: new config")
	if v := next(); v != "0.3.0" {
		t.Fatalf("after a breaking change = %q, want 0.3.0", v)
	}
	cs, err := commitsSince("v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 || cs[0].Scope != "sshx" || !cs[0].Breaking || cs[1].Description != "crash" {
		t.Fatalf("commitsSince = %+v", cs)
	}
	run("tag", "v0.3.0")
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
