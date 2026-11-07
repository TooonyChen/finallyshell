package fileicons

import (
	"path"
	"strings"
)

// Language returns the id of the language VS Code takes a file named name
// to be in, or "" when it knows none: by the whole name first, then by
// the longest pattern of names that matches, then by the longest
// extension, as VS Code picks among its built-in languages.
func Language(name string) string {
	lower := strings.ToLower(name)
	if id, ok := languageFileNames[lower]; ok {
		return id
	}
	best, id := "", ""
	for p, l := range languagePatterns {
		if len(p) > len(best) || len(p) == len(best) && p < best {
			if ok, _ := path.Match(p, lower); ok {
				best, id = p, l
			}
		}
	}
	if id != "" {
		return id
	}
	best = ""
	for i := range len(lower) {
		if lower[i] != '.' {
			continue
		}
		if l, ok := languageExtensions[lower[i:]]; ok {
			best, id = lower[i:], l
			break
		}
	}
	for s, l := range languageSuffixes {
		if len(s) > len(best) && strings.HasSuffix(lower, s) {
			best, id = s, l
		}
	}
	return id
}
