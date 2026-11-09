package monitor

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startListener starts, on the server, a listener of the user's own on a
// port of its own, connected to itself, which sends 5000 bytes over the
// connection. It returns the port and the PID; the listener is killed
// when the test ends. The test is skipped on a server without python3.
func startListener(t *testing.T, ctx context.Context, client *ssh.Client) (int, int) {
	t.Helper()
	port := 40000 + rand.IntN(20000)
	py := `import socket, time
l = socket.socket(); l.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); l.bind(("127.0.0.1", ` + strconv.Itoa(port) + `)); l.listen()
c = socket.create_connection(("127.0.0.1", ` + strconv.Itoa(port) + `)); s, _ = l.accept(); c.sendall(b"x" * 5000); s.recv(10000)
time.sleep(120)`
	out, _, err := RunScript(ctx, client, "command -v python3 >/dev/null || { echo none; exit; }\nnohup python3 -c '"+py+"' >/dev/null 2>&1 &\necho $!\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) == "none" {
		t.Skip("no python3 on the server")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("listener's pid %q", out)
	}
	t.Cleanup(func() { RunScript(context.Background(), client, "kill -KILL "+strconv.Itoa(pid)+" 2>/dev/null\n") })
	return port, pid
}

// TestNetworkLive lists a real server's sockets, with ss and from /proc,
// and finds a listener the test started, by its port.
//
//	FINALLYSHELL_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/monitor -run Live
func TestNetworkLive(t *testing.T) {
	client := dialTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	port, pid := startListener(t, ctx, client)
	title := "127.0.0.1:" + strconv.Itoa(port)
	for _, noSS := range []bool{false, true} {
		l := NetLister{NoSS: noSS}
		var row NetGroup
		found := false
		for deadline := time.Now().Add(15 * time.Second); !found; time.Sleep(200 * time.Millisecond) {
			snap, err := l.List(ctx, client)
			if err != nil {
				t.Fatal(err)
			}
			if noSS && !snap.FromProc {
				t.Fatal("read with ss")
			}
			for _, g := range snap.Groups {
				if g.Listen && g.Title() == title && len(g.Conns) == 1 {
					row, found = g, true
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("no listener on %s (no ss %v) in %d rows", title, noSS, len(snap.Groups))
			}
		}
		if row.PID != pid || row.Name != "python3" || row.Proto != "tcp" {
			t.Errorf("no ss %v: row %+v", noSS, row)
		}
		c := row.Conns[0]
		if c.Remote.Addr().String() != "127.0.0.1" || c.State != "ESTAB" || c.PID != pid {
			t.Errorf("no ss %v: connection %+v", noSS, c)
		}
		if !noSS && (!c.HasBytes || c.Received != 5000) {
			t.Errorf("ss: bytes %+v", c)
		}
	}
}

// TestTraceLive traces a route on a real server: with NextTrace when it
// has it, else the fallback, else the missing tools are said.
func TestTraceLive(t *testing.T) {
	client := dialTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	updates := 0
	r := Trace(ctx, client, "127.0.0.1", "en", func(TraceResult) { updates++ })
	if !r.Done || updates == 0 {
		t.Fatalf("result %+v after %d updates", r, updates)
	}
	switch {
	case r.Tool == "":
		if !r.NoNextTrace || !errors.Is(r.Err, ErrNoTraceTool) {
			t.Errorf("no tool: %+v", r)
		}
	case r.Err == nil:
		if len(r.Hops) == 0 || r.Hops[len(r.Hops)-1].IP != "127.0.0.1" {
			t.Errorf("%s: hops %+v", r.Tool, r.Hops)
		}
	default:
		t.Logf("%s failed: %v", r.Tool, r.Err) // as without root
	}
	t.Logf("tool %q, no NextTrace %v, %d hops, err %v", r.Tool, r.NoNextTrace, len(r.Hops), r.Err)
}

// TestNextTraceStreamLive runs the trace script on a real server with a
// NextTrace of the test's own, in a folder of its own put first on the
// script's PATH, which writes the recorded RAW output a probe at a time:
// the hops come as they are written, before the trace ends.
func TestNextTraceStreamLive(t *testing.T) {
	client := dialTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, _, err := RunScript(ctx, client, "mktemp -d\n")
	dir := strings.TrimSpace(out)
	if err != nil || !strings.HasPrefix(dir, "/") {
		t.Fatalf("mktemp: %q %v", out, err)
	}
	t.Cleanup(func() { RunScript(context.Background(), client, "rm -rf '"+dir+"'\n") })
	fake := `#!/bin/sh
if [ "$1" = --help ]; then echo '  -k  --traceroute  Use traditional traceroute mode'; exit 0; fi
echo "$*" >"` + dir + `/args"
while IFS= read -r line; do
	echo "$line"
	case $line in *'|'*) sleep 0.1 ;; esac
done <"` + dir + `/out.txt"
`
	setup := "cat >'" + dir + "/out.txt' <<'FS_EOF'\n" + readFixture(t, "nexttrace-raw-en.txt") + "FS_EOF\n" +
		"cat >'" + dir + "/nexttrace' <<'FS_EOF'\n" + fake + "FS_EOF\n" +
		"chmod +x '" + dir + "/nexttrace'\n"
	if _, stderr, err := RunScript(ctx, client, setup); err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	start := time.Now()
	var partial time.Duration // when a part of the route came
	r := runTrace(ctx, client, "PATH='"+dir+"':$PATH\n"+traceScript("8.8.8.8", "en"), func(r TraceResult) {
		if !r.Done && len(r.Hops) > 0 && len(r.Hops) < 5 && partial == 0 {
			partial = time.Since(start)
		}
	})
	took := time.Since(start)
	if r.Err != nil || r.Tool != ToolNextTrace || len(r.Hops) != 5 || r.Hops[4].Host != "dns.google" {
		t.Fatalf("result %+v", r)
	}
	if partial == 0 || took-partial < 500*time.Millisecond {
		t.Errorf("first hops after %v, the end after %v", partial, took)
	}
	t.Logf("first hops after %v, the end after %v", partial, took)
	args, _, _ := RunScript(ctx, client, "cat '"+dir+"/args'\n")
	if got := strings.TrimSpace(args); got != "--traceroute --raw -M -g en 8.8.8.8" {
		t.Errorf("args %q", got)
	}
}
