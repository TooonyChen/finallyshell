package monitor

import (
	"context"
	"strings"

	"golang.org/x/crypto/ssh"
)

// shellDirScript prints the working directory of the interactive shell
// on the connection it runs on, for shells that don't report it (OSC 7).
// It is POSIX sh and reads only /proc, as BusyBox has it.
//
// It runs on an exec channel of the same connection as the shell, so the
// server's process for the connection (sshd, or sshd-session since
// OpenSSH 9.8, or dropbear) is the parent of both: the script walks up
// from its own process to the first such ancestor, then takes the one
// child of it with a controlling terminal, which is the shell (exec
// channels have none). The directory is that of the terminal's
// foreground process group, as a subshell or su to the same user moves
// it, else the shell's own. Root's processes can't be read by the user,
// and print nothing. It prints nothing either when there is no such
// child, or more than one.
//
// The fields of /proc/<pid>/stat after the name, which is in brackets
// and may hold spaces, are the state, the parent, the process group, the
// session, the terminal and the terminal's foreground process group.
const shellDirScript = `export LC_ALL=C
p=$$
while :; do
	read -r comm 2>/dev/null </proc/$p/comm || exit 0
	case $comm in sshd*|dropbear*) break ;; esac
	read -r s 2>/dev/null </proc/$p/stat || exit 0
	s=${s##*) }
	set -- $s
	[ "$2" -gt 1 ] 2>/dev/null || exit 0
	p=$2
done
kids=$(cat /proc/$p/task/*/children 2>/dev/null)
if [ -z "$kids" ]; then
	for f in /proc/[0-9]*/stat; do
		read -r s 2>/dev/null <$f || continue
		s=${s##*) }
		set -- $s
		[ "$2" = "$p" ] || continue
		c=${f#/proc/}
		kids="$kids ${c%/stat}"
	done
fi
n=0
for c in $kids; do
	read -r s 2>/dev/null </proc/$c/stat || continue
	s=${s##*) }
	set -- $s
	[ "$5" = 0 ] && continue
	n=$((n+1))
	shell=$c
	fg=$6
done
[ "$n" = 1 ] || exit 0
d=$(readlink /proc/$fg/cwd 2>/dev/null)
[ -n "$d" ] || d=$(readlink /proc/$shell/cwd 2>/dev/null)
echo "` + procMark + `dir---"
printf '%s\n' "$d"
`

// ShellDir reads the working directory of the interactive shell on
// client's connection, on a Linux server: "" when it can't tell, as on a
// server without /proc, for a root shell, or a directory since deleted.
func ShellDir(ctx context.Context, client *ssh.Client) (string, error) {
	out, _, err := RunScript(ctx, client, shellDirScript)
	if err != nil {
		return "", err
	}
	return parseShellDir(out), nil
}

// parseShellDir is the directory in shellDirScript's output, or "".
func parseShellDir(out string) string {
	_, d, ok := strings.Cut(out, procMark+"dir---\n")
	if !ok {
		return ""
	}
	d = strings.TrimSuffix(d, "\n")
	// The link of a deleted directory, which SFTP can't open.
	if !strings.HasPrefix(d, "/") || strings.HasSuffix(d, " (deleted)") || strings.Contains(d, "\n") {
		return ""
	}
	return d
}
