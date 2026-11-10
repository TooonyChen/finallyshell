package monitor

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPTY opens a new pseudo-terminal's controller and its terminal.
func openPTY(t *testing.T) (ptmx, pts *os.File) {
	t.Helper()
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no pseudo-terminals:", err)
	}
	t.Cleanup(func() { ptmx.Close() })
	var unlock int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		t.Fatal("unlock pty:", e)
	}
	var n uint32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); e != 0 {
		t.Fatal("pty number:", e)
	}
	pts, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("open pty:", err)
	}
	t.Cleanup(func() { pts.Close() })
	return ptmx, pts
}

// The shell directory script runs here on this machine's sh and /proc,
// in the process tree an SSH connection has: a process named sshd, the
// session leader of a terminal, as the connection's process is of the
// shell's, with two children: one on the terminal, as the shell, and
// the script without one (setsid), as on an exec channel. The terminal's
// foreground is sshd itself, so the directory is its own.
func TestShellDirScript(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("no setsid")
	}
	tmp := t.TempDir()
	sshd := filepath.Join(tmp, "sshd")
	if err := os.Symlink(sh, sshd); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "work dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script, out := filepath.Join(tmp, "script.sh"), filepath.Join(tmp, "out")
	if err := os.WriteFile(script, []byte(shellScript), 0o644); err != nil {
		t.Fatal(err)
	}
	_, pts := openPTY(t)
	// The script's own directory is another, which it must not print.
	cmd := exec.Command(sshd, "-c", `sh -c "exec sleep 30" & (cd / && exec "$1" sh -s <"$2" >"$3"); kill $!`, "sshd", setsid, script, out)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pts, pts, pts
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
		t.Fatal("the script did not end")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	st := parseShell(string(data))
	if st.Dir != dir {
		t.Fatalf("shell dir %q, want %q (output %q)", st.Dir, dir, strings.TrimSpace(string(data)))
	}
	if st.Comm != "sshd" || len(st.Args) == 0 || st.Agent() != nil {
		t.Errorf("foreground %q %q, agent %v", st.Comm, st.Args, st.Agent())
	}
}

// The script tells the program in the terminal's foreground, here one
// named claude, in the process tree of a connection: a process named
// sshd leads the terminal's session; its child on the terminal, the
// shell, runs claude in a process group of its own that it gives the
// terminal, as an interactive shell does a command; and the script runs
// without a terminal, as on an exec channel. The shell is this test's
// binary (foregroundShell), which checks each step and writes how it
// went, rather than sh with job control, so that a step that fails says
// so. The tree needs no root and no terminal of the test's own.
//
// claude is this test's binary too (TestForegroundJob), under a link of
// that name, which waits: not a link to sleep, as uutils' coreutils, a
// multi-call binary that Ubuntu ships, refuses to run under a name that
// is not its own, and the "claude" would end at once.
func TestShellScriptForeground(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	sshd, claude := filepath.Join(tmp, "sshd"), filepath.Join(tmp, "claude")
	if err := os.Symlink(sh, sshd); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, claude); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "project")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script, out, report := filepath.Join(tmp, "script.sh"), filepath.Join(tmp, "out"), filepath.Join(tmp, "report")
	if err := os.WriteFile(script, []byte(shellScript), 0o644); err != nil {
		t.Fatal(err)
	}
	_, pts := openPTY(t)
	// "exit" after the shell keeps sh from replacing itself with it.
	cmd := exec.Command(sshd, "-c", `"$1" -test.run='^TestForegroundShell$'; exit $?`, "sshd", self)
	cmd.Env = append(os.Environ(), foregroundEnv+"="+strings.Join([]string{claude, script, out, report}, "\n"))
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pts, pts, pts
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		killNamed(claude)
		<-done
		t.Error("the shell did not end")
	}

	steps, _ := os.ReadFile(report)
	t.Logf("the shell's steps:\n%s", steps)
	if !strings.Contains(string(steps), "\ndone\n") {
		t.Fatal("the shell did not get to the end: its steps are above")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := parseShell(string(data))
	if got.Comm != "claude" || len(got.Args) != 2 || got.Args[0] != claude || got.Args[1] != foregroundJobArg || got.Dir != dir {
		t.Fatalf("read %+v from %q; the shell's steps:\n%s", got, data, steps)
	}
	if a := got.Agent(); a == nil || a.Name != "Claude Code" {
		t.Errorf("agent %v", a)
	}
}

// foregroundEnv gives foregroundShell claude's path, the script's, the
// output's and the report's, a line each.
const foregroundEnv = "FINALLYSHELL_TEST_FOREGROUND"

// TestForegroundShell is the shell of TestShellScriptForeground, which
// runs the test binary with foregroundEnv set; it does nothing else.
func TestForegroundShell(t *testing.T) {
	v := os.Getenv(foregroundEnv)
	if v == "" || v == "job" {
		t.Skip("the shell of TestShellScriptForeground")
	}
	f := strings.Split(v, "\n")
	if len(f) != 4 {
		t.Fatalf("%s %q", foregroundEnv, v)
	}
	if err := foregroundShell(f[0], f[1], f[2], f[3]); err != nil {
		t.Fatal(err)
	}
}

// foregroundJobArg runs TestForegroundJob alone.
const foregroundJobArg = "-test.run=^TestForegroundJob$"

// TestForegroundJob is the claude of TestShellScriptForeground, which
// runs the test binary as claude with foregroundEnv set to "job": it
// waits until it is killed.
func TestForegroundJob(t *testing.T) {
	if os.Getenv(foregroundEnv) != "job" {
		t.Skip("the claude of TestShellScriptForeground")
	}
	time.Sleep(time.Minute)
}

// foregroundShell runs claude in the foreground of the terminal, checks
// that it is, runs the script without a terminal into out, and ends
// claude, writing each step to report and "done" at the end.
func foregroundShell(claude, script, out, report string) error {
	rep, err := os.Create(report)
	if err != nil {
		return err
	}
	defer rep.Close()
	step := func(format string, args ...any) { fmt.Fprintf(rep, format+"\n", args...) }
	fail := func(what string, err error) error {
		step("failed: %s: %v", what, err)
		return fmt.Errorf("%s: %v", what, err)
	}

	// The terminal of the session, which sshd leads.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fail("open the controlling terminal", err)
	}
	defer tty.Close()
	fg, err := ttyForeground(tty)
	if err != nil {
		return fail("read the terminal's foreground", err)
	}
	step("the terminal's foreground: %d (this shell's group %d, the session's leader %d)", fg, syscall.Getpgrp(), os.Getppid())

	// claude in a group of its own, given the terminal. A group not in
	// the foreground is stopped by SIGTTOU as it gives it, unless it
	// ignores it, as shells do.
	signal.Ignore(syscall.SIGTTOU)
	job := exec.Command(claude, foregroundJobArg)
	job.Env = append(os.Environ(), foregroundEnv+"=job")
	job.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: int(tty.Fd())}
	var jobErr bytes.Buffer
	job.Stdout, job.Stderr = &jobErr, &jobErr
	if err := job.Start(); err != nil {
		return fail("start claude in the foreground", err)
	}
	pid := job.Process.Pid
	defer func() {
		// Whether claude still ran: a signal is the kill.
		alive := procState(pid)
		job.Process.Kill()
		err := job.Wait()
		step("claude before the kill: %s; then %v; its output %q", alive, err, jobErr.Bytes())
	}()
	if fg, err = ttyForeground(tty); err != nil || fg != pid {
		return fail("give claude the terminal", fmt.Errorf("foreground %d, claude %d, error %v", fg, pid, err))
	}
	step("claude %d has the terminal: %s", fg, procState(pid))

	// The script, on no terminal, as on an exec channel.
	in, err := os.Open(script)
	if err != nil {
		return fail("open the script", err)
	}
	defer in.Close()
	o, err := os.Create(out)
	if err != nil {
		return fail("create the output", err)
	}
	defer o.Close()
	run := exec.Command("sh", "-s")
	run.Dir = "/"
	run.Stdin, run.Stdout = in, o
	run.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stderr bytes.Buffer
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		return fail("run the script", fmt.Errorf("%v %s", err, stderr.Bytes()))
	}
	if data, _ := os.ReadFile(out); !strings.Contains(string(data), procMark+"dir---") {
		// What the script saw: the processes of the session.
		stats, _ := filepath.Glob("/proc/[0-9]*/stat")
		for _, f := range stats {
			b, _ := os.ReadFile(f)
			s := string(b)
			if i := strings.LastIndex(s, ") "); i >= 0 {
				if fields := strings.Fields(s[i+2:]); len(fields) > 5 && fields[3] == strconv.Itoa(os.Getppid()) {
					step("  %s", strings.Join(append([]string{s[:i+1]}, fields[:6]...), " "))
				}
			}
		}
		return fail("read the script's output", fmt.Errorf("%q", data))
	}
	step("the script ran; claude: %s", procState(pid))
	step("done")
	return nil
}

// procState is what /proc tells of process pid: its name and state, and
// its arguments.
func procState(pid int) string {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return err.Error()
	}
	s := string(stat)
	if i := strings.LastIndex(s, ") "); i >= 0 && len(s) > i+3 {
		s = s[:i+3] // "pid (name) S"
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return fmt.Sprintf("%s, cmdline %q %v", s, cmdline, err)
}

// ttyForeground is the foreground process group of the terminal tty.
func ttyForeground(tty *os.File) (int, error) {
	var pgrp int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&pgrp))); e != 0 {
		return 0, e
	}
	return int(pgrp), nil
}

// killNamed kills the processes whose first argument is prog.
func killNamed(prog string) {
	cmdlines, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, f := range cmdlines {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if arg0, _, _ := strings.Cut(string(b), "\x00"); arg0 == prog {
			var pid int
			if _, err := fmt.Sscanf(f, "/proc/%d/cmdline", &pid); err == nil {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
}

// With no sshd above it, as where the server is not OpenSSH or Dropbear,
// the script prints nothing, and ends well.
func TestShellDirScriptNoSSHD(t *testing.T) {
	out, code := runLocal(t, shellScript)
	if sh := parseShell(out); code != 0 || sh.Dir != "" || sh.Comm != "" || sh.Args != nil {
		t.Fatalf("output %q, exit %d", out, code)
	}
}

// With two shells on the connection, as two panes of a split session,
// the script reads the one the pick names, by the order they started
// in, and nothing when the number of shells is not the pick's. The
// tree is TestShellDirScript's, the connection's process with two
// children each leading a terminal of its own (setsid -c), in folders
// of their own, and the script without one.
func TestShellDirScriptPanes(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	setsid, err := exec.LookPath("setsid")
	if err != nil {
		t.Skip("no setsid")
	}
	tmp := t.TempDir()
	sshd := filepath.Join(tmp, "sshd")
	if err := os.Symlink(sh, sshd); err != nil {
		t.Fatal(err)
	}
	dirs := []string{filepath.Join(tmp, "first"), filepath.Join(tmp, "second")}
	for _, d := range dirs {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, pts1 := openPTY(t)
	_, pts2 := openPTY(t)
	run := func(pick ShellPick) Shell {
		t.Helper()
		script, out := filepath.Join(tmp, "script.sh"), filepath.Join(tmp, "out")
		if err := os.WriteFile(script, []byte(pick.script()), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Remove(out)
		// The second starts a while after the first, as a pane opened
		// later; the script runs once both are there.
		cmd := exec.Command(sshd, "-c", `(cd "$5" && exec "$1" -c sleep 30 <"$3" >"$3" 2>&1) & a=$!
sleep 0.2
(cd "$6" && exec "$1" -c sleep 30 <"$4" >"$4" 2>&1) & b=$!
sleep 0.2
(cd / && exec "$1" sh -s <"$2" >"$7")
kill $a $b`, "sshd", setsid, script, pts1.Name(), pts2.Name(), dirs[0], dirs[1], out)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			cmd.Process.Kill()
			t.Fatal("the script did not end")
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return parseShell(string(data))
	}
	for _, tc := range []struct {
		pick ShellPick
		dir  string
	}{
		{ShellPick{1, 2}, dirs[0]},
		{ShellPick{2, 2}, dirs[1]},
		{ShellPick{}, ""},     // one shell asked for: two there
		{ShellPick{1, 3}, ""}, // a pane still opening, or one ending
	} {
		if got := run(tc.pick); got.Dir != tc.dir {
			t.Errorf("%+v: read %+v, want the folder %q", tc.pick, got, tc.dir)
		}
	}
}
