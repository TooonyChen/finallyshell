package monitor

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// dialTestServer connects to the server FINALLYSHELL_TEST_SSH names
// (user:password@host:port), skipping the test when it is not set.
func dialTestServer(t *testing.T) *ssh.Client {
	t.Helper()
	spec := os.Getenv("FINALLYSHELL_TEST_SSH")
	if spec == "" {
		t.Skip("FINALLYSHELL_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// startSleep starts a sleep of the user's own on the server, in the
// background of a session that returns at once, with env in its
// environment. Its seconds are a number of its own, as other tests, and
// other people, use the server too. It returns the PID and the command
// line; the sleep is killed when the test ends.
func startSleep(t *testing.T, ctx context.Context, client *ssh.Client, env string) (int, string) {
	t.Helper()
	args := "sleep " + strconv.Itoa(100000+rand.IntN(800000))
	out, _, err := RunScript(ctx, client, env+" nohup "+args+" >/dev/null 2>&1 &\necho $!\n")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("sleep's pid %q", out)
	}
	t.Cleanup(func() { RunScript(context.Background(), client, "kill -KILL "+strconv.Itoa(pid)+" 2>/dev/null\n") })
	return pid, args
}

// waitGone waits until pid, started at start, has ended.
func waitGone(t *testing.T, ctx context.Context, client *ssh.Client, pid int, start uint64) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		running, err := ProcessRunning(ctx, client, pid, start)
		if err != nil {
			t.Fatal(err)
		}
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d still running", pid)
		}
	}
}

// TestProcessesLive lists a real server's processes, reads the details
// of one of the user's own, and stops a sleep it started.
//
//	FINALLYSHELL_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/monitor -run Live
func TestProcessesLive(t *testing.T) {
	client := dialTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pid, args := startSleep(t, ctx, client, "FS_PROC_TEST=hello-procs")
	var l ProcessLister
	var procs []Process
	i := -1
	// The background sleep may take a moment to start on a busy server.
	for deadline := time.Now().Add(15 * time.Second); i < 0; time.Sleep(200 * time.Millisecond) {
		var err error
		if procs, err = l.List(ctx, client); err != nil {
			t.Fatal(err)
		}
		i = slices.IndexFunc(procs, func(p Process) bool { return p.PID == pid && p.Args == args })
		if i < 0 && time.Now().After(deadline) {
			t.Fatalf("%d processes, without the sleep %d", len(procs), pid)
		}
	}
	sleep := procs[i]
	if sleep.Name != "sleep" || sleep.User == "" || sleep.RSS == 0 || !strings.HasSuffix(sleep.Path, "/sleep") || sleep.Start == 0 {
		t.Errorf("sleep %+v", sleep)
	}
	if slices.ContainsFunc(procs, func(p Process) bool { return p.Name == "ls" && strings.Contains(p.Args, "/proc/") }) {
		t.Error("the script's own ls is listed")
	}
	// The second read has CPU percents over the time between.
	time.Sleep(time.Second)
	if _, err := l.List(ctx, client); err != nil {
		t.Fatal(err)
	}

	d, err := ReadProcessDetails(ctx, client, pid)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "sleep" || strings.Join(d.Args, " ") != args || d.Cwd == "" || d.Path != sleep.Path || d.EnvDenied || d.User != sleep.User || d.Start != sleep.Start {
		t.Errorf("details %+v", d)
	}
	if !slices.Contains(d.Env, EnvVar{"FS_PROC_TEST", "hello-procs"}) {
		t.Errorf("environment %v", d.Env)
	}
	// Another user's process: what needs root is denied.
	initDetails, err := ReadProcessDetails(ctx, client, 1)
	if err != nil || len(initDetails.Args) == 0 || !initDetails.EnvDenied {
		t.Errorf("pid 1: %+v, %v", initDetails, err)
	}
	if _, err := ReadProcessDetails(ctx, client, 999999999); !errors.Is(err, ErrNoProcess) {
		t.Errorf("no process: %v", err)
	}

	// Signals: one to init is refused, with the server's words.
	var se *SignalError
	if err := SendSignal(ctx, client, 1, initDetails.Start, "TERM"); !errors.As(err, &se) || !strings.Contains(se.Msg, "not permitted") {
		t.Errorf("TERM to 1: %v", err)
	}
	for _, sig := range []string{"STOP", "CONT", "TERM"} {
		if err := SendSignal(ctx, client, pid, sleep.Start, sig); err != nil {
			t.Fatalf("%s: %v", sig, err)
		}
	}
	waitGone(t, ctx, client, pid, sleep.Start)
}

// TestSignalCheckLive sends no signal to a process that is not the one
// meant: another start time, as a PID used again has, or one that ended.
func TestSignalCheckLive(t *testing.T) {
	client := dialTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pid, _ := startSleep(t, ctx, client, "")
	var d ProcessDetails
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		var err error
		if d, err = ReadProcessDetails(ctx, client, pid); err == nil && d.Name == "sleep" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the sleep %d: %+v, %v", pid, d, err)
		}
	}

	// Another start time: the PID is another process's, which is left
	// running.
	if err := SendSignal(ctx, client, pid, d.Start+1, "KILL"); !errors.Is(err, ErrNoProcess) {
		t.Errorf("another start time: %v", err)
	}
	if running, err := ProcessRunning(ctx, client, pid, d.Start); err != nil || !running {
		t.Fatalf("the sleep was killed: running %v, %v", running, err)
	}

	// Killed outside the app: the app's signal finds it gone.
	if _, _, err := RunScript(ctx, client, "kill -KILL "+strconv.Itoa(pid)+"\n"); err != nil {
		t.Fatal(err)
	}
	waitGone(t, ctx, client, pid, d.Start)
	if err := SendSignal(ctx, client, pid, d.Start, "TERM"); !errors.Is(err, ErrNoProcess) {
		t.Errorf("ended: %v", err)
	}
}
