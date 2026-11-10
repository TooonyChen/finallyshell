package main

import (
	"cmp"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Commit is a commit's Conventional Commits header and body.
type Commit struct {
	Type, Scope, Description string
	Breaking                 bool
	Subject                  string // the first line as written
}

var header = regexp.MustCompile(`^([a-zA-Z]+)(?:\(([^)]*)\))?(!)?: (.+)$`)

// ParseCommit reads a commit's subject and body. A subject that is not a
// Conventional Commit gives a Commit with no Type, which never releases.
func ParseCommit(subject, body string) Commit {
	c := Commit{Subject: subject}
	m := header.FindStringSubmatch(subject)
	if m == nil {
		return c
	}
	c.Type, c.Scope, c.Breaking, c.Description = strings.ToLower(m[1]), m[2], m[3] == "!", m[4]
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "BREAKING CHANGE:") || strings.HasPrefix(line, "BREAKING-CHANGE:") {
			c.Breaking = true
		}
	}
	return c
}

// Bump is how much a release raises the version.
type Bump int

const (
	None Bump = iota
	Patch
	Minor
	Major
)

func (b Bump) String() string {
	return [...]string{"none", "patch", "minor", "major"}[b]
}

// BumpOf is the bump the commits call for: a breaking change is major,
// feat is minor, fix and perf are patch, and the other types (docs,
// chore, test, ci, build, style, refactor) call for none. Release only
// warns when the chosen version is lower.
func BumpOf(commits []Commit) Bump {
	b := None
	for _, c := range commits {
		switch {
		case c.Type != "" && c.Breaking:
			return Major
		case c.Type == "feat":
			b = max(b, Minor)
		case c.Type == "fix", c.Type == "perf":
			b = max(b, Patch)
		}
	}
	return b
}

// Version is a release version, MAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// ParseVersion reads "1.2.3" or "v1.2.3". Pre-release and build suffixes
// are refused: releases never have them.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || (len(p) > 1 && p[0] == '0') {
			return Version{}, fmt.Errorf("version %q is not MAJOR.MINOR.PATCH", s)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, nil
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Compare is -1, 0 or +1 as v is lower than, the same as or higher than w.
func (v Version) Compare(w Version) int {
	if c := cmp.Compare(v.Major, w.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(v.Minor, w.Minor); c != 0 {
		return c
	}
	return cmp.Compare(v.Patch, w.Patch)
}

// Next is v raised by b. Before 1.0.0 a breaking change raises the minor
// version, as 0.x versions promise no compatibility.
func (v Version) Next(b Bump) Version {
	if b == Major && v.Major == 0 {
		b = Minor
	}
	switch b {
	case Major:
		return Version{v.Major + 1, 0, 0}
	case Minor:
		return Version{v.Major, v.Minor + 1, 0}
	case Patch:
		return Version{v.Major, v.Minor, v.Patch + 1}
	}
	return v
}

// PreviewVersion is the version of a preview build of main, n commits
// after last, the newest release tag: the next patch version with the
// pre-release "preview.<n>", 0.4.1-preview.3 three commits after v0.4.0.
// Semantic versioning orders a pre-release before its version, so the
// next release, patch or minor, is newer than every preview after the
// last one, and an app on a preview is offered it. With no release yet
// (last ""), it is 0.0.1-preview.<n>, n counting every commit. n is at
// least 1: a release's own commit has no preview.
func PreviewVersion(last string, n int) (string, error) {
	var v Version
	if last != "" {
		var err error
		if v, err = ParseVersion(last); err != nil {
			return "", err
		}
	}
	if n < 1 {
		return "", fmt.Errorf("a preview is at least one commit after the last release, not %d", n)
	}
	return fmt.Sprintf("%s-preview.%d", v.Next(Patch), n), nil
}
