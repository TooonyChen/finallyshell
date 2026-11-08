// Command release computes the next release of the app from its git
// history and writes what a release ships besides the builds. The release
// workflow (.github/workflows/release.yml) runs it from the repository's
// root:
//
//	go run ./tools/release version    # the next version, or nothing
//	go run ./tools/release notes      # Markdown notes since the last release
//	go run ./tools/release licenses   # THIRD_PARTY_LICENSES.txt
//
// Releases are tagged vMAJOR.MINOR.PATCH. The commits since the last tag
// decide the next version by their Conventional Commits type: a breaking
// change raises the major version (the minor one before 1.0.0), feat the
// minor one, fix and perf the patch one, and the other types release
// nothing. With no tag yet, the version in mygo.json is the first release.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "version":
		err = printVersion()
	case "notes":
		err = printNotes()
	case "licenses":
		err = ThirdPartyLicenses(".", os.Stdout)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./tools/release version|notes|licenses")
	os.Exit(2)
}

func printVersion() error {
	last, err := lastTag()
	if err != nil {
		return err
	}
	tagged, err := headTagged()
	if err != nil {
		return err
	}
	base, err := mygoVersion()
	if err != nil {
		return err
	}
	commits, err := commitsSince(last)
	if err != nil {
		return err
	}
	v, err := NextVersion(last, base, commits, tagged)
	if err != nil {
		return err
	}
	switch {
	case tagged:
		fmt.Fprintln(os.Stderr, "release: HEAD is already released")
	case v == "":
		fmt.Fprintf(os.Stderr, "release: nothing to release since %s (no feat, fix, perf or breaking change)\n", last)
	default:
		if last == "" {
			fmt.Fprintf(os.Stderr, "release: %s (the first release, from mygo.json)\n", v)
		} else {
			fmt.Fprintf(os.Stderr, "release: %s (%s since %s)\n", v, BumpOf(commits), last)
		}
		fmt.Println(v)
	}
	return nil
}

func printNotes() error {
	last, err := lastTag()
	if err != nil {
		return err
	}
	commits, err := commitsSince(last)
	if err != nil {
		return err
	}
	fmt.Print(Notes(commits))
	return nil
}

// tagPattern matches release tags, for git's glob matching.
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

// lastTag is the newest release tag reachable from HEAD, or "".
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

func headTagged() (bool, error) {
	tags, err := git("tag", "--points-at", "HEAD", "--list", tagPattern)
	if err != nil {
		return false, err
	}
	for _, t := range strings.Fields(tags) {
		if _, err := ParseVersion(t); err == nil {
			return true, nil
		}
	}
	return false, nil
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

func mygoVersion() (string, error) {
	b, err := os.ReadFile("mygo.json")
	if err != nil {
		return "", err
	}
	var c struct{ Version string }
	if err := json.Unmarshal(b, &c); err != nil {
		return "", fmt.Errorf("mygo.json: %v", err)
	}
	return c.Version, nil
}
