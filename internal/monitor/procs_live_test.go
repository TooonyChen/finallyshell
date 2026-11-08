package monitor

import (
	"context"
	"errors"
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

// TestProcessesLive lists a real server's processes, reads the details
// of one of the user's own, and stops a sleep it started.
//
//	FINALLYSHELL_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/monitor -run Live
func TestProcessesLive(t *testing.T) {
	client := dialTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// A sleep of the user's own, in the background of a session that
	// returns at once, with a variable in its environment.
	out, _, err := RunScript(ctx, client, "FS_PROC_TEST=hello-procs nohup sleep 300 >/dev/null 2>&1 &\necho $!\n")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("sleep's pid %q", out)
	}
	t.Cleanup(func() { RunScript(context.Background(), client, "kill -KILL "+strconv.Itoa(pid)+" 2>/dev/null\n") })

	var l ProcessLister
	procs, err := l.List(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(procs, func(p Process) bool { return p.PID == pid })
	if len(procs) < 3 || i < 0 {
		t.Fatalf("%d processes, sleep %d among them: %v", len(procs), pid, i >= 0)
	}
	sleep := procs[i]
	if sleep.Name != "sleep" || !strings.HasPrefix(sleep.Args, "sleep 300") || sleep.User == "" || sleep.RSS == 0 || !strings.HasSuffix(sleep.Path, "/sleep") || sleep.Start == 0 {
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
	if d.Name != "sleep" || !slices.Equal(d.Args, []string{"sleep", "300"}) || d.Cwd == "" || d.Path != sleep.Path || d.EnvDenied || d.User != sleep.User {
		t.Errorf("details %+v", d)
	}
	if !slices.Contains(d.Env, EnvVar{"FS_PROC_TEST", "hello-procs"}) {
		t.Errorf("environment %v", d.Env)
	}
	// Another user's process: what needs root is denied.
	if d, err := ReadProcessDetails(ctx, client, 1); err != nil || len(d.Args) == 0 || !d.EnvDenied {
		t.Errorf("pid 1: %+v, %v", d, err)
	}
	if _, err := ReadProcessDetails(ctx, client, 999999999); !errors.Is(err, ErrNoProcess) {
		t.Errorf("no process: %v", err)
	}

	// Signals: one to init is refused, with the server's words.
	var se *SignalError
	if err := SendSignal(ctx, client, 1, "TERM"); !errors.As(err, &se) || !strings.Contains(se.Msg, "not permitted") {
		t.Errorf("TERM to 1: %v", err)
	}
	if err := SendSignal(ctx, client, pid, "STOP"); err != nil {
		t.Fatal(err)
	}
	if err := SendSignal(ctx, client, pid, "CONT"); err != nil {
		t.Fatal(err)
	}
	if err := SendSignal(ctx, client, pid, "TERM"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		running, err := ProcessRunning(ctx, client, pid, sleep.Start)
		if err != nil {
			t.Fatal(err)
		}
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sleep still running after TERM")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
