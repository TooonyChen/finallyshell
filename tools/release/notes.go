package main

import "strings"

// Notes is the Markdown of a release's notes: its breaking changes,
// features, fixes and performance work, newest first as given. Commits
// of other types are left out.
func Notes(commits []Commit) string {
	sections := []struct {
		title string
		match func(Commit) bool
	}{
		{"Breaking changes", func(c Commit) bool { return c.Type != "" && c.Breaking }},
		{"Features", func(c Commit) bool { return c.Type == "feat" }},
		{"Fixes", func(c Commit) bool { return c.Type == "fix" }},
		{"Performance", func(c Commit) bool { return c.Type == "perf" }},
	}
	var b strings.Builder
	for _, s := range sections {
		var lines []string
		for _, c := range commits {
			if s.match(c) {
				lines = append(lines, "- "+entry(c))
			}
		}
		if len(lines) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## " + s.title + "\n\n" + strings.Join(lines, "\n") + "\n")
	}
	if b.Len() == 0 {
		return "No user-facing changes.\n"
	}
	return b.String()
}

func entry(c Commit) string {
	if c.Scope == "" {
		return c.Description
	}
	return c.Scope + ": " + c.Description
}
