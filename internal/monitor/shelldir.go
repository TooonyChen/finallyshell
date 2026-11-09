package monitor

import (
	"context"
	"strings"

	"golang.org/x/crypto/ssh"
)

// shellScript prints the working directory of the interactive shell on
// the connection it runs on, for shells that don't report it (OSC 7),
// and the program in the foreground of its terminal, for the session
// tab to show the coding agent that runs there. It is POSIX sh and reads
// only /proc, as BusyBox has it.
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
// The foreground program is the group's leader: its name (comm), then
// its arguments, one a line, at most 4 KiB of them. At the prompt it is
// the shell itself. A root process's name and arguments can be read
// too, unless /proc is mounted with hidepid.
//
// The fields of /proc/<pid>/stat after the name, which is in brackets
// and may hold spaces, are the state, the parent, the process group, the
// session, the terminal and the terminal's foreground process group.
const shellScript = `export LC_ALL=C
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
echo "` + procMark + `fg---"
c=
read -r c 2>/dev/null </proc/$fg/comm
printf '%s\n' "$c"
dd if=/proc/$fg/cmdline bs=4096 count=1 2>/dev/null | tr '\0' '\n'
`

// Shell is what ShellState reads of the interactive shell on a
// connection. Its zero value is a shell the server told nothing of.
type Shell struct {
	// Dir is the working directory of the program in the shell's
	// foreground, else the shell's own: "" when it can't tell, as for a
	// root shell, or a directory since deleted.
	Dir string
	// Comm is the name of the program in the terminal's foreground, the
	// leader of its process group, as the kernel has it (at most 15
	// bytes): the shell's at its prompt, "" when unknown.
	Comm string
	// Args are that program's arguments, its own name first, as far as
	// 4 KiB of them go.
	Args []string
}

// Agent is the coding agent that the shell's foreground program is, or
// nil.
func (sh Shell) Agent() *Agent { return FindAgent(sh.Comm, sh.Args) }

// ShellState reads the interactive shell on client's connection, on a
// Linux server: its directory and its foreground program. It is the zero
// Shell when the server can't tell, as one without /proc.
func ShellState(ctx context.Context, client *ssh.Client) (Shell, error) {
	out, _, err := RunScript(ctx, client, shellScript)
	if err != nil {
		return Shell{}, err
	}
	return parseShell(out), nil
}

// parseShell reads shellScript's output.
func parseShell(out string) Shell {
	_, rest, ok := strings.Cut(out, procMark+"dir---\n")
	if !ok {
		return Shell{}
	}
	d, fg, _ := strings.Cut(rest, procMark+"fg---\n")
	sh := Shell{Dir: shellDir(d)}
	if comm, args, ok := strings.Cut(fg, "\n"); ok {
		sh.Comm = comm
		if args = strings.TrimRight(args, "\n"); args != "" { // "" for a zombie
			sh.Args = strings.Split(args, "\n")
		}
	}
	return sh
}

// shellDir is the directory in the dir section of shellScript's output,
// or "".
func shellDir(d string) string {
	d = strings.TrimSuffix(d, "\n")
	// The link of a deleted directory, which SFTP can't open.
	if !strings.HasPrefix(d, "/") || strings.HasSuffix(d, " (deleted)") || strings.Contains(d, "\n") {
		return ""
	}
	return d
}
