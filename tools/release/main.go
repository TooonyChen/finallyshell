// Command release works out the next release of the app and writes what a
// release ships besides the builds. The release workflow
// (.github/workflows/release.yml) runs it from the repository's root:
//
//	go run ./tools/release version          # the next version, or nothing
//	go run ./tools/release notes [version]  # its notes, from CHANGELOG.md
//	go run ./tools/release draft            # the commits since the last release
//	go run ./tools/release preview-version  # the version of a preview of HEAD
//	go run ./tools/release licenses         # THIRD_PARTY_LICENSES.txt
//
// Releases are tagged vMAJOR.MINOR.PATCH. CHANGELOG.md, written by hand,
// has a section per version, newest first, with the notes in English and
// Simplified Chinese; mygo build puts the same section into the update
// manifests. Its newest section is the next version when it is newer than
// the last tag; otherwise there is nothing to release. The commits since
// the last tag only check that version by their Conventional Commits type
// (see BumpOf): a version lower than they call for is a warning.
//
// The preview workflow (.github/workflows/preview.yml) publishes every
// push to main as a prerelease tagged v<version>-preview.<n> (see
// PreviewVersion). Those tags never count as the last release.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// changelog is the file of the release notes, which mygo build reads too.
const changelog = "CHANGELOG.md"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
	}
	var err error
	switch {
	case args[0] == "version" && len(args) == 1:
		err = printVersion()
	case args[0] == "notes" && len(args) <= 2:
		err = printNotes(args[1:])
	case args[0] == "draft" && len(args) == 1:
		err = printDraft()
	case args[0] == "preview-version" && len(args) == 1:
		err = printPreviewVersion()
	case args[0] == "licenses" && len(args) == 1:
		err = ThirdPartyLicenses(".", os.Stdout)
	default:
		usage()
	}
	if err != nil {
		report("error", err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./tools/release version | notes [version] | draft | preview-version | licenses")
	os.Exit(2)
}

// report writes a message to stderr; in GitHub Actions, as an annotation
// of the run (level "error", "warning" or "notice").
func report(level, msg string) {
	switch {
	case os.Getenv("GITHUB_ACTIONS") == "true":
		fmt.Fprintf(os.Stderr, "::%s::release: %s\n", level, msg)
	case level == "notice":
		fmt.Fprintf(os.Stderr, "release: %s\n", msg)
	default:
		fmt.Fprintf(os.Stderr, "release: %s: %s\n", level, msg)
	}
}

func readChangelog() ([]Section, error) {
	b, err := os.ReadFile(changelog)
	if err != nil {
		return nil, err
	}
	return ParseChangelog(changelog, string(b))
}

func printVersion() error {
	sections, err := readChangelog()
	if err != nil {
		return err
	}
	last, err := lastTag()
	if err != nil {
		return err
	}
	commits, err := commitsSince(last)
	if err != nil {
		return err
	}
	v, warning, err := Release(changelog, sections, last, commits)
	if err != nil {
		return err
	}
	if warning != "" {
		report("warning", warning)
	}
	switch {
	case v == "":
		report("notice", fmt.Sprintf("nothing to release: %s, the newest section of %s, is released as %s; add a section for the next version", sections[0].Version, changelog, last))
	case last == "":
		report("notice", v+", the first release")
		fmt.Println(v)
	default:
		report("notice", fmt.Sprintf("%s, after %s", v, last))
		fmt.Println(v)
	}
	return nil
}

// printNotes prints the section of the version given, or else the newest.
func printNotes(args []string) error {
	sections, err := readChangelog()
	if err != nil {
		return err
	}
	s := sections[0]
	if len(args) == 1 {
		v, err := ParseVersion(args[0])
		if err != nil {
			return err
		}
		var ok bool
		if s, ok = Find(sections, v); !ok {
			return fmt.Errorf("%s has no section for %s: add a \"## %s\" section", changelog, v, v)
		}
	}
	if err := Check(changelog, s); err != nil {
		return err
	}
	fmt.Println(s.Notes)
	return nil
}

func printDraft() error {
	last, err := lastTag()
	if err != nil {
		return err
	}
	commits, err := commitsSince(last)
	if err != nil {
		return err
	}
	fmt.Print(Draft(commits))
	return nil
}

// printPreviewVersion prints the version of a preview of HEAD, or nothing
// when HEAD is a release's own commit, and says why on stderr.
func printPreviewVersion() error {
	v, err := previewVersion()
	if err != nil {
		return err
	}
	if v == "" {
		report("notice", "nothing to preview: HEAD is the last release's commit")
		return nil
	}
	report("notice", v)
	fmt.Println(v)
	return nil
}

// previewVersion is the version of a preview of HEAD (PreviewVersion),
// counting the commits since the last release tag, or "" when HEAD is that
// tag's commit.
func previewVersion() (string, error) {
	last, err := lastTag()
	if err != nil {
		return "", err
	}
	rev := "HEAD"
	if last != "" {
		rev = last + "..HEAD"
	}
	out, err := git("rev-list", "--count", rev)
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return "", fmt.Errorf("git rev-list --count %s printed %q, not a number", rev, out)
	}
	if n == 0 {
		return "", nil
	}
	return PreviewVersion(last, n)
}

// tagPattern matches release tags, for git's glob matching. It matches
// preview tags too, such as v0.4.1-preview.3, which lastTag skips as
// ParseVersion refuses them.
const tagPattern = "v[0-9]*.[0-9]*.[0-9]*"

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(e.Stderr)))
		}
		return "", fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// lastTag is the newest release tag reachable from HEAD, HEAD's own
// included, or "".
func lastTag() (string, error) {
	tags, err := git("tag", "--merged", "HEAD", "--list", tagPattern, "--sort=-v:refname")
	if err != nil {
		return "", err
	}
	for _, t := range strings.Fields(tags) {
		if _, err := ParseVersion(t); err == nil {
			return t, nil
		}
	}
	return "", nil
}

// commitsSince lists the commits after tag up to HEAD, newest first, or
// all of HEAD's history when tag is "".
func commitsSince(tag string) ([]Commit, error) {
	rev := "HEAD"
	if tag != "" {
		rev = tag + "..HEAD"
	}
	out, err := git("log", "--no-merges", "--format=%s%x00%b%x1e", rev)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		subject, body, _ := strings.Cut(rec, "\x00")
		commits = append(commits, ParseCommit(subject, body))
	}
	return commits, nil
}
