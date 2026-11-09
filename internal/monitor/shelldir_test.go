package monitor

import (
	"slices"
	"testing"
)

func TestParseShellDir(t *testing.T) {
	for _, tc := range []struct{ out, want string }{
		{procMark + "dir---\n/var/log\n", "/var/log"},
		{"motd from a login script\n" + procMark + "dir---\n/home/a b\n", "/home/a b"},
		{procMark + "dir---\n/\n", "/"},
		{"", ""},                              // no /proc, no sshd, or no shell
		{procMark + "dir---\n\n", ""},         // the shell's directory can't be read
		{procMark + "dir---\nrelative\n", ""}, // not what readlink prints
		{procMark + "dir---\n/srv/old (deleted)\n", ""},
		{procMark + "dir---\n/a\n/b\n", ""},
		{procMark + "dir---\n/var/log\n" + procMark + "fg---\nbash\n-bash\n", "/var/log"},
		{procMark + "dir---\n\n" + procMark + "fg---\nclaude\nclaude\n", ""}, // a root shell's
	} {
		if got := parseShell(tc.out).Dir; got != tc.want {
			t.Errorf("parseShell(%q).Dir = %q, want %q", tc.out, got, tc.want)
		}
	}
}

func TestParseShellForeground(t *testing.T) {
	for _, tc := range []struct {
		out  string
		comm string
		args []string
	}{
		{procMark + "dir---\n/srv\n" + procMark + "fg---\nbash\n-bash\n", "bash", []string{"-bash"}},
		{procMark + "dir---\n/srv\n" + procMark + "fg---\nnode\nnode\n/usr/bin/claude\n--resume\n\n", "node", []string{"node", "/usr/bin/claude", "--resume"}},
		{procMark + "dir---\n/srv\n" + procMark + "fg---\nclaude\nclaude\n\n\n", "claude", []string{"claude"}}, // a title set, NULs after it
		{procMark + "dir---\n/srv\n" + procMark + "fg---\nsleep\n", "sleep", nil},                              // a zombie
		{procMark + "dir---\n/srv\n" + procMark + "fg---\n\n", "", nil},                                        // gone
		{procMark + "dir---\n/srv\n", "", nil},                                                                 // no foreground section
		{"", "", nil},
	} {
		sh := parseShell(tc.out)
		if sh.Comm != tc.comm || !slices.Equal(sh.Args, tc.args) {
			t.Errorf("parseShell(%q) = %q %q, want %q %q", tc.out, sh.Comm, sh.Args, tc.comm, tc.args)
		}
	}
	sh := parseShell(procMark + "dir---\n/srv\n" + procMark + "fg---\nnode\nnode\n/usr/lib/node_modules/@openai/codex/bin/codex.js\n")
	if a := sh.Agent(); a == nil || a.Name != "Codex" {
		t.Errorf("agent %v", a)
	}
}
