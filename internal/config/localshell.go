package config

import (
	"errors"
	"runtime"
	"strings"
	"unicode"
)

// ErrUnclosedQuote is a command line with a quote that does not end.
var ErrUnclosedQuote = errors.New("unclosed quote")

// LocalShellCommand is Settings.LocalShell split into the program and
// its arguments: nil for the terminal's default.
func (s Settings) LocalShellCommand() ([]string, error) {
	return SplitCommandLine(s.LocalShell, runtime.GOOS == "windows")
}

// SplitCommandLine splits line into words as a shell would, without
// expanding anything: blanks separate words, and quotes group them, so
// that "" is an empty word. Inside single quotes every character is
// itself. Elsewhere a backslash escapes the next character (inside
// double quotes only ", \, $ and `, as in sh), except on Windows
// (windows set), where it is a path's separator and always itself, so
// that C:\Windows\System32\cmd.exe and "C:\Program Files\PowerShell\7\pwsh.exe"
// read as typed. A line of blanks is nil, the terminal's default
// program.
func SplitCommandLine(line string, windows bool) ([]string, error) {
	var (
		words []string
		word  strings.Builder
		in    bool // a word is started, which may be empty
		quote rune // the quote open, 0 for none
	)
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		next := func(set string) bool {
			return !windows && i+1 < len(rs) && (set == "" || strings.ContainsRune(set, rs[i+1]))
		}
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case quote == '"':
			switch {
			case r == '"':
				quote = 0
			case r == '\\' && next("\"\\$`"):
				i++
				word.WriteRune(rs[i])
			default:
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, in = r, true
		case r == '\\' && next(""):
			i++
			word.WriteRune(rs[i])
			in = true
		case unicode.IsSpace(r):
			if in {
				words = append(words, word.String())
				word.Reset()
				in = false
			}
		default:
			word.WriteRune(r)
			in = true
		}
	}
	if quote != 0 {
		return nil, ErrUnclosedQuote
	}
	if in {
		words = append(words, word.String())
	}
	return words, nil
}
