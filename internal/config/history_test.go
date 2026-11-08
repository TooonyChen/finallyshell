package config

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A host's history keeps each command once, the newest last, and goes
// with the host or with all of them.
func TestHistoryFile(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	if list, err := LoadHistory("h1"); err != nil || list != nil {
		t.Fatalf("no file: %q, %v", list, err)
	}
	for _, c := range []string{"ls -l", "cd /etc", "  ", "two\nlines", "echo \x1b[31m", "ls -l", "df -h"} {
		if err := AddHistory("h1", c); err != nil {
			t.Fatal(err)
		}
	}
	AddHistory("h2", "uptime")
	list, err := LoadHistory("h1")
	if want := []string{"cd /etc", "ls -l", "df -h"}; err != nil || !slices.Equal(list, want) {
		t.Errorf("history %q, %v; want %q", list, err, want)
	}
	data, _ := os.ReadFile(filepath.Join(HistoryDir(), "h1.txt"))
	if string(data) != "cd /etc\nls -l\ndf -h\n" {
		t.Errorf("file %q", data)
	}

	if err := DeleteHistory("h1"); err != nil {
		t.Fatal(err)
	}
	if list, _ := LoadHistory("h1"); list != nil {
		t.Errorf("deleted: %q", list)
	}
	if err := DeleteHistory("h1"); err != nil {
		t.Errorf("deleting again: %v", err)
	}
	if list, _ := LoadHistory("h2"); !slices.Equal(list, []string{"uptime"}) {
		t.Errorf("the other host's: %q", list)
	}
	if err := ClearAllHistory(); err != nil {
		t.Fatal(err)
	}
	if list, _ := LoadHistory("h2"); list != nil {
		t.Errorf("cleared: %q", list)
	}

	// A file written by hand, with duplicates and blank lines, reads
	// deduplicated.
	os.MkdirAll(HistoryDir(), 0o700)
	os.WriteFile(historyPath("h3"), []byte("a\r\n\nb\na\n"), 0o600)
	if list, _ := LoadHistory("h3"); !slices.Equal(list, []string{"b", "a"}) {
		t.Errorf("hand-written: %q", list)
	}
}

// An ID that is no plain file name stays inside the history folder.
func TestHistoryPath(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	for _, id := range []string{"../../etc/passwd", "a/b", `a\b`, ".hidden", "", "h123"} {
		p := historyPath(id)
		if filepath.Dir(p) != HistoryDir() || strings.HasPrefix(filepath.Base(p), ".") && id != "" {
			t.Errorf("%q: %s", id, p)
		}
	}
	if historyPath("h123") != filepath.Join(HistoryDir(), "h123.txt") {
		t.Errorf("plain ID: %s", historyPath("h123"))
	}
	if historyPath("a/b") == historyPath("a_b") {
		t.Error("two IDs share a file")
	}
}

// Past the limit, the oldest commands go.
func TestTrimHistory(t *testing.T) {
	list := []string{"aaaa", "bb", "cccccc", "d"} // 5+3+7+2 = 17 bytes
	if got := TrimHistory(list, 17); !slices.Equal(got, list) {
		t.Errorf("at the limit: %q", got)
	}
	if got := TrimHistory(list, 12); !slices.Equal(got, []string{"bb", "cccccc", "d"}) {
		t.Errorf("12 bytes: %q", got)
	}
	if got := TrimHistory(list, 2); !slices.Equal(got, []string{"d"}) {
		t.Errorf("2 bytes: %q", got)
	}
	if got := TrimHistory(list, 0); len(got) != 0 {
		t.Errorf("none: %q", got)
	}

	// A file over MaxHistoryBytes is cut as a command is added.
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	var b strings.Builder
	for i := range MaxHistoryBytes/1000 + 10 {
		b.WriteString(strings.Repeat("x", 1000))
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	os.MkdirAll(HistoryDir(), 0o700)
	os.WriteFile(historyPath("big"), []byte(b.String()), 0o600)
	if err := AddHistory("big", "newest"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(historyPath("big"))
	list, _ = LoadHistory("big")
	if fi.Size() > MaxHistoryBytes || list[len(list)-1] != "newest" || list[0] == strings.Repeat("x", 1000)+"0" {
		t.Errorf("size %d, first %q, last %q", fi.Size(), list[0][990:], list[len(list)-1])
	}
}

// bash's file, with and without times, and zsh's extended one, metafied.
func TestParseShellHistory(t *testing.T) {
	bash := "ls -l\n#1700000000\ncd /etc\n\nls -l\nyum -y install epel-release\n"
	if got, want := ParseShellHistory([]byte(bash), false), []string{"cd /etc", "ls -l", "yum -y install epel-release"}; !slices.Equal(got, want) {
		t.Errorf("bash %q, want %q", got, want)
	}
	// UTF-8 with a byte 0x83 in it, as "ă" (c4 83), is left alone in
	// bash's file.
	if got := ParseShellHistory([]byte("echo \xc4\x83\n"), false); !slices.Equal(got, []string{"echo ă"}) {
		t.Errorf("bash UTF-8 %q", got)
	}

	// zsh metafies the bytes 0x83 to 0x9d: "ă" is c4 83, written c4 83
	// a3; "中" (e4 b8 ad) is written as it is; "…" (e2 80 a6) too.
	zsh := ": 1700000000:0;ls -la\n" +
		": 1700000005:2;echo \xc4\x83\xa3 中\n" +
		": 1700000009:0;for i in 1 2; do\\\n" +
		"echo $i\\\n" +
		"done\n" +
		": 1700000010:0;git status\n" +
		"plain line\n"
	want := []string{"ls -la", "echo ă 中", "git status", "plain line"}
	if got := ParseShellHistory([]byte(zsh), true); !slices.Equal(got, want) {
		t.Errorf("zsh %q, want %q", got, want)
	}
	// Lines with control characters, as a file crafted to put escape
	// sequences into the terminal, are dropped; a tab stays.
	crafted := "ls\n" + "echo \x1b[2Jcleared\n" + ": 1700000000:0;rm \x07x\n" + "a\x7fb\n" + "c\u0085d\n" + "printf 'a\tb'\n"
	for _, zsh := range []bool{false, true} {
		if got := ParseShellHistory([]byte(crafted), zsh); !slices.Equal(got, []string{"ls", "printf 'a\tb'"}) {
			t.Errorf("crafted (zsh %v): %q", zsh, got)
		}
	}
	// A line that only looks like zsh's stays as it is.
	if got := ParseShellHistory([]byte(": not:zsh;x\n"), true); !slices.Equal(got, []string{": not:zsh;x"}) {
		t.Errorf("not extended %q", got)
	}
}

func TestAddCommand(t *testing.T) {
	if got := AddCommand([]string{"a", "b", "c"}, "a"); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Errorf("moved %q", got)
	}
	if got := AddCommand(nil, "x"); !slices.Equal(got, []string{"x"}) {
		t.Errorf("first %q", got)
	}
	if got := Deduped([]string{"a", "b", "a", "c", "b"}); !slices.Equal(got, []string{"a", "c", "b"}) {
		t.Errorf("deduped %q", got)
	}
}
