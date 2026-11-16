package monitor

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runLocal runs a script as RunScript does, with this machine's sh, and
// returns its output and exit status.
func runLocal(t *testing.T, script string) (string, int) {
	t.Helper()
	cmd := exec.Command("sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// The signal script kills only the process that started when the list
// said: one whose PID another process has now is left alone. It runs
// here with this machine's sh and /proc, on a process whose name has the
// ") " the script cuts the stat line after: a shell script named "a) (b",
// which the kernel names the process after, looping without exec, so it
// is /bin/sh itself, which runs anywhere.
func TestSignalScript(t *testing.T) {
	const name = "a) (b"
	script := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do sleep 1 >/dev/null 2>&1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := exec.Command(script)
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	// The loop and its sleeps, whatever the test left running.
	t.Cleanup(func() { syscall.Kill(-pid, syscall.SIGKILL) })
	// why says how the process ended, for a failure: its exit and what it
	// wrote. Read once it has ended, as the output is then all written.
	why := func(err error) string { return err.Error() + "; output " + strconv.Quote(output.String()) }

	var st procStat
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		select {
		case err := <-exited:
			t.Fatalf("the script %q ended by itself before the test: %s", script, why(err))
		default:
		}
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			t.Fatalf("the script's stat: %v", err)
		}
		var ok bool
		if _, st, ok = parseProcStat(strings.TrimSpace(string(data))); ok && st.name == name && st.state != 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the script did not start as %q: stat %q", name, data)
		}
	}

	// Another start time: not the process meant, so no kill.
	out, status := runLocal(t, signalScript(pid, st.start+1, "TERM"))
	if !strings.Contains(out, signalGone) || status != signalGoneStatus || signalResult(out, "", nil) != ErrNoProcess {
		t.Errorf("other start: %q, status %d", out, status)
	}
	select {
	case err := <-exited:
		t.Fatalf("killed all the same, or ended by itself: %s", why(err))
	case <-time.After(100 * time.Millisecond):
	}

	// Its own start time: killed.
	out, status = runLocal(t, signalScript(pid, st.start, "TERM"))
	if out != "" || status != 0 {
		t.Errorf("own start: %q, status %d", out, status)
	}
	select {
	case err := <-exited:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
			t.Errorf("ended otherwise than by TERM: %s", why(err))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not killed")
	}

	// Gone: no kill, and the same answer.
	out, status = runLocal(t, signalScript(pid, st.start, "KILL"))
	if !strings.Contains(out, signalGone) || status != signalGoneStatus {
		t.Errorf("gone: %q, status %d", out, status)
	}
	// Without a start time, kill alone, which fails on its own.
	if s := signalScript(pid, 0, "HUP"); strings.Contains(s, "/stat") || !strings.Contains(s, "kill -HUP "+strconv.Itoa(pid)) {
		t.Errorf("no start: %q", s)
	}
	if err := signalResult("", "", nil); err != nil {
		t.Errorf("sent: %v", err)
	}
}

// Old procps (3.3.10, CentOS 7) rejects the :32 width override, so the
// first ps of procsScript fails. The fallback must still list every
// process: without -e, ps on an exec channel with no controlling
// terminal lists only the script's own processes, which parse drops,
// and the Processes tab shows 0 processes (#213).
func TestProcsScriptOldProcps(t *testing.T) {
	realPS, err := exec.LookPath("ps")
	if err != nil {
		t.Skip("ps not on PATH")
	}
	// A ps that acts like old procps on the width override, like a
	// modern procps on -e, and like ps without -e on an exec channel:
	// only the invoker's own shell and itself.
	stub := "#!/bin/sh\n" +
		"case \"$*\" in *user:32=*) exit 1;; esac\n" +
		"case \"$*\" in *-e*) exec " + realPS + " \"$@\";; esac\n" +
		"printf ' %s 0 root 1000 sh -s\\n' \"$PPID\"\n" +
		"printf ' %s %s root 1000 ps -o pid=,ppid=,user=,rss=,args=\\n' \"$$\" \"$PPID\"\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	out, status := runLocal(t, procsScript)
	if status != 0 {
		t.Fatalf("script exited %d: %q", status, out)
	}
	var l ProcessLister
	procs, err := l.parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) == 0 {
		t.Fatal("no processes listed: the fallback did not select all processes")
	}
	// PID 1 is always there when ps selects every process.
	if i := slices.IndexFunc(procs, func(p Process) bool { return p.PID == 1 }); i < 0 {
		t.Errorf("%d processes, without pid 1", len(procs))
	}
}
