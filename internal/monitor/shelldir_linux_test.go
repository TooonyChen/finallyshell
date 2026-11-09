package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := os.WriteFile(script, []byte(shellDirScript), 0o644); err != nil {
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
	if got := parseShellDir(string(data)); got != dir {
		t.Fatalf("shell dir %q, want %q (output %q)", got, dir, strings.TrimSpace(string(data)))
	}
}

// With no sshd above it, as where the server is not OpenSSH or Dropbear,
// the script prints nothing, and ends well.
func TestShellDirScriptNoSSHD(t *testing.T) {
	out, code := runLocal(t, shellDirScript)
	if code != 0 || parseShellDir(out) != "" {
		t.Fatalf("output %q, exit %d", out, code)
	}
}
