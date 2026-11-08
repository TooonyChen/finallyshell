package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// The command bar's history (internal/app/commandbar.go) is kept per
// host, in history/<host ID>.txt in Dir(): one command per line, the
// oldest first, each command once, a command sent again moved to the
// end. It has no limit on the number of commands, as FinalShell's, and is
// cut from the oldest only once it passes MaxHistoryBytes. The format is
// kept this plain so that sync could carry it later.

// MaxHistoryBytes is how large a host's history file grows before its
// oldest commands are dropped.
const MaxHistoryBytes = 10 << 20

// historyMu keeps the history files' reads and writes one at a time: the
// app writes them on goroutines.
var historyMu sync.Mutex

// HistoryDir is the folder of the history files.
func HistoryDir() string { return filepath.Join(Dir(), "history") }

// historyPath is the file of a host's history. Host IDs are the app's
// own ("h" and digits), but one from another device's sync or a
// hand-edited hosts.json could hold anything, so any byte that is not a
// plain file name's is written as %xx.
func historyPath(hostID string) string {
	var b strings.Builder
	for i := 0; i < len(hostID); i++ {
		c := hostID[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_',
			c == '.' && i > 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02x", c)
		}
	}
	return filepath.Join(HistoryDir(), b.String()+".txt")
}

// LoadHistory reads a host's history, the oldest command first: nil when
// it has none.
func LoadHistory(hostID string) ([]string, error) {
	historyMu.Lock()
	defer historyMu.Unlock()
	return loadHistory(hostID)
}

func loadHistory(hostID string) ([]string, error) {
	data, err := os.ReadFile(historyPath(hostID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []string
	for line := range strings.Lines(string(data)) {
		if line = strings.TrimRight(line, "\r\n"); Keepable(line) {
			list = append(list, line)
		}
	}
	return Deduped(list), nil
}

// AddHistory adds cmd to a host's history, as the newest command; one
// already there moves to the end. A command of blanks, or with a control
// character (a second line, an escape sequence), is not kept.
func AddHistory(hostID, cmd string) error {
	if !Keepable(cmd) {
		return nil
	}
	historyMu.Lock()
	defer historyMu.Unlock()
	list, err := loadHistory(hostID)
	if err != nil {
		return err
	}
	list = TrimHistory(AddCommand(list, cmd), MaxHistoryBytes)
	return writeHistory(hostID, list)
}

// writeHistory replaces a host's history file with list, through a
// temporary file, so that a crash leaves the old one or the new one.
func writeHistory(hostID string, list []string) error {
	if err := os.MkdirAll(HistoryDir(), 0o700); err != nil {
		return err
	}
	var b bytes.Buffer
	for _, c := range list {
		b.WriteString(c)
		b.WriteByte('\n')
	}
	p := historyPath(hostID)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// DeleteHistory deletes a host's history, as the host is deleted for
// good. A host without one is no error.
func DeleteHistory(hostID string) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	if err := os.Remove(historyPath(hostID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ClearAllHistory deletes the history of every host.
func ClearAllHistory() error {
	historyMu.Lock()
	defer historyMu.Unlock()
	return os.RemoveAll(HistoryDir())
}

// Keepable tells whether cmd can go in a history file: one line that is
// not only blanks.
func Keepable(cmd string) bool {
	return strings.TrimSpace(cmd) != "" && !HasControl(cmd, true)
}

// HasControl tells whether s has a control character: C0 (U+0000 to
// U+001F), DEL or C1 (U+0080 to U+009F), tabs aside with tab set. A
// command with one, inserted in the command bar and sent, would run more
// than it shows (a newline) or drive the terminal (an escape sequence),
// so the history keeps none and the bar inserts none.
func HasControl(s string, tab bool) bool {
	for _, r := range s {
		if (r < 0x20 && !(tab && r == '\t')) || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

// AddCommand returns list, the oldest first, with cmd as its newest
// command, taken out of where it was before.
func AddCommand(list []string, cmd string) []string {
	list = slices.DeleteFunc(list, func(c string) bool { return c == cmd })
	return append(list, cmd)
}

// Deduped returns list, the oldest first, with each command once, where
// it was last.
func Deduped(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		if c := list[i]; !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	slices.Reverse(out)
	return out
}

// TrimHistory drops the oldest commands of list until it takes at most
// limit bytes as a file, each command and its newline.
func TrimHistory(list []string, limit int) []string {
	size := 0
	for _, c := range list {
		size += len(c) + 1
	}
	i := 0
	for size > limit && i < len(list) {
		size -= len(list[i]) + 1
		i++
	}
	return list[i:]
}

// ParseShellHistory reads a shell's history file: bash's, with the
// "#<seconds>" lines HISTTIMEFORMAT writes between commands, or, with
// zsh set, zsh's, whose extended format puts ": <start>:<elapsed>;"
// before each command, and which writes the bytes 0x83 to 0x9d, and NUL,
// "metafied": 0x83, then the byte xor 0x20. A command of several lines,
// written with a backslash ending each but the last, is left out, as are
// blank lines, the command bar holding one line, and lines with a control
// character other than tab (HasControl), so that a file written to put
// escape sequences into the terminal puts nothing there. The result is the oldest
// first, each command once.
func ParseShellHistory(data []byte, zsh bool) []string {
	if zsh {
		data = unmetafy(data)
	}
	var list []string
	var cont bool // the line before ended with a backslash
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\r\n")
		was := cont
		cont = strings.HasSuffix(line, "\\") && !strings.HasSuffix(line, "\\\\")
		if was || cont || isTimestamp(line) {
			continue // a line of a command of several, or a time
		}
		if cmd, ok := zshCommand(line); ok {
			line = cmd
		}
		if Keepable(line) {
			list = append(list, line)
		}
	}
	return Deduped(list)
}

// isTimestamp tells whether line is bash's "#1700000000".
func isTimestamp(line string) bool {
	if len(line) < 2 || line[0] != '#' {
		return false
	}
	for _, c := range line[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// zshCommand returns the command of a line of zsh's extended history,
// ": 1700000000:0;ls -l".
func zshCommand(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, ": ")
	if !ok {
		return "", false
	}
	meta, cmd, ok := strings.Cut(rest, ";")
	if !ok {
		return "", false
	}
	start, elapsed, ok := strings.Cut(meta, ":")
	if !ok || !digits(start) || !digits(elapsed) {
		return "", false
	}
	return cmd, true
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// unmetafy undoes zsh's metafication: 0x83 followed by a byte xor 0x20.
// In a file of zsh's every 0x83 is one, as an 0x83 of the text is
// metafied too.
func unmetafy(data []byte) []byte {
	if bytes.IndexByte(data, 0x83) < 0 {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		if data[i] == 0x83 && i+1 < len(data) {
			i++
			out = append(out, data[i]^0x20)
			continue
		}
		out = append(out, data[i])
	}
	return out
}
