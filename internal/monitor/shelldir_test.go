package monitor

import "testing"

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
	} {
		if got := parseShellDir(tc.out); got != tc.want {
			t.Errorf("parseShellDir(%q) = %q, want %q", tc.out, got, tc.want)
		}
	}
}
