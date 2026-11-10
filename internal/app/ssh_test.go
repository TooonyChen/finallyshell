package app

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/monitor"
	"finallyshell/internal/remotefs"

	"github.com/egoist/mygo/ui"
)

// TestSSH runs against a real server, named as user:password@host:port
// by FINALLYSHELL_TEST_SSH:
//
//	FINALLYSHELL_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/app -run SSH
func TestSSH(t *testing.T) {
	a, p, s := connectTestServer(t)
	s.term().Send([]byte("echo hello-$((40+2))\r"))
	p.until(t, "the shell's output", func() bool { return strings.Contains(s.term().Text(), "hello-42") })
	p.until(t, "the server's stats", func() bool { return s.stats.OK && s.stats.MemTotal > 0 && len(s.stats.LatHist) >= 2 })
	// Every network interface, each with a history of its own, lo last
	// and never the busiest.
	p.until(t, "the interfaces' history", func() bool { return len(s.stats.Ifaces) > 0 && len(s.stats.Ifaces[0].RxHist) > 0 })
	if ifs := s.stats.Ifaces; s.stats.Iface == "lo" || slices.IndexFunc(ifs, func(it monitor.Iface) bool { return it.Name == "lo" }) > 0 && ifs[len(ifs)-1].Name != "lo" {
		t.Errorf("interfaces %+v, busiest %q", ifs, s.stats.Iface)
	}
	p.until(t, "the home directory", func() bool { return s.files.cwd != "" && !s.files.loading })
	home := s.files.cwd

	// System info, read over an exec channel of its own.
	a.openSysInfo(s)
	p.until(t, "the system info", func() bool { return !a.sysInfo.loading })
	if info := a.sysInfo.info; a.sysInfo.err != nil || info.Hostname == "" || info.Kernel == "" || info.CPUs == 0 || info.MemTotal == 0 {
		t.Errorf("system info %+v, err %v", info, a.sysInfo.err)
	}
	a.closeSysInfo()

	// Upload by dropping on the table, then download it back.
	local := filepath.Join(t.TempDir(), "hello.txt")
	os.WriteFile(local, []byte("hi from finallyshell"), 0o644)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	box, _ := tt.Find("Name")
	if !tt.DropFiles(box.X+40, box.Y+120, local) {
		t.Fatal("the table took no files")
	}
	p.until(t, "the upload", func() bool { return len(a.transfers.jobs) == 1 && a.transfers.active() == 0 })
	if j := a.transfers.jobs[0]; j.state != transferDone {
		t.Fatalf("upload: %v", j.err)
	}
	p.until(t, "the new file listed", func() bool {
		return slices.ContainsFunc(s.files.entries, func(e remotefs.Entry) bool { return e.Name == "hello.txt" })
	})
	a.download(s, []string{"hello.txt"}, "")
	p.until(t, "the download", func() bool { return len(a.transfers.jobs) == 2 && a.transfers.active() == 0 })
	if data, _ := os.ReadFile(filepath.Join(p.downloads, "hello.txt")); string(data) != "hi from finallyshell" {
		t.Errorf("round trip %q", data)
	}
	s.sftp.Remove(home + "/hello.txt")

	// A folder of many small files as an archive, both ways.
	testArchiveTransfers(t, a, p, s, home)

	// The shell's TERM is the host's terminal type.
	s.term().Send([]byte("echo term-$TERM\r"))
	p.until(t, "the default TERM", func() bool { return strings.Contains(s.term().Text(), "term-xterm-256color") })

	// Reconnecting asks nothing: the key is known now. It takes the
	// host's changes.
	s.Host.TermType, s.Host.Backspace = "vt220", config.BackspaceCtrlH
	a.reconnect(s)
	p.until(t, "the reconnection", func() bool { return s.status == statusConnected || s.status == statusFailed || s.hostKeyQ != nil })
	if s.status != statusConnected {
		t.Fatalf("reconnect: status %d err %v question %v", s.status, s.err, s.hostKeyQ != nil)
	}
	s.term().Send([]byte("echo term-$TERM\r"))
	p.until(t, "the host's TERM", func() bool { return strings.Contains(s.term().Text(), "term-vt220") })
	s.term().Send([]byte("stty -a | tr ';' '\\n' | grep -w erase | tr -d ' ' | sed 's/^/stty-/'\r"))
	p.until(t, "the erase character", func() bool { return strings.Contains(s.term().Text(), "stty-erase=^H") })
	s.Host.TermType, s.Host.Backspace = "", config.BackspaceDEL

	// A wrong password fails with a message in the app's words.
	bad := &config.Host{ID: "bad", Addr: s.Host.Addr, Port: s.Host.Port, User: s.Host.User, Auth: config.AuthPassword}
	a.passwords[bad.ID] = "wrong"
	a.openSession(bad)
	b := a.current()
	p.until(t, "the failure", func() bool { return b.status == statusFailed })
	if got := describeError(b.err); !strings.Contains(got, "Authentication failed") {
		t.Errorf("wrong password: %q", got)
	}
	a.CloseAll()
}

// connectTestServer opens a session to the server FINALLYSHELL_TEST_SSH
// names, trusting its key, and waits for the shell; it skips the test
// when the variable is not set.
func connectTestServer(t *testing.T) (*App, *fakePlatform, *Session) {
	t.Helper()
	spec := os.Getenv("FINALLYSHELL_TEST_SSH")
	if spec == "" {
		t.Skip("FINALLYSHELL_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	hostname, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)

	a, p := testApp(t)
	h := &config.Host{ID: "test", Addr: hostname, Port: port, User: user, Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.passwords[h.ID] = pass
	// From a new tab, as the app starts: the tab becomes the session.
	a.NewTab()
	tab := a.current()
	a.openSession(h)
	s := a.current()
	if s != tab || len(a.sessions) != 1 || s.Host != h {
		t.Fatalf("the new tab did not become the session: %d rows", len(a.sessions))
	}

	p.until(t, "the host key question", func() bool { return s.hostKeyQ != nil || s.status == statusFailed })
	if s.status == statusFailed {
		t.Fatal(describeError(s.err))
	}
	if !strings.HasPrefix(s.hostKeyQ.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint %q", s.hostKeyQ.Fingerprint)
	}
	s.hostKeyQ.Answer <- true
	s.hostKeyQ = nil

	p.until(t, "the shell", func() bool { return s.status == statusConnected || s.status == statusFailed })
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	return a, p, s
}

// TestSSHProcesses lists the real server's processes in the Processes
// tab, opens the details of a sleep the shell started, and kills it; a
// signal to a sleep killed outside the app is not sent. The sleeps have
// seconds of their own, as other tests, and other people, use the server
// too.
func TestSSHProcesses(t *testing.T) {
	killWait = 300 * time.Millisecond
	defer func() { killWait = 3 * time.Second }()
	a, p, s := connectTestServer(t)
	defer a.CloseAll()
	args := "sleep " + strconv.Itoa(100000+rand.IntN(800000))
	other := "sleep " + strconv.Itoa(900000+rand.IntN(90000))
	s.term().Send([]byte("FS_PROCS_TEST=from-finallyshell " + args + " &\r" + other + " &\r"))
	a.bottomTab = tabProcesses
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	find := func(args string) (monitor.Process, bool) {
		i := slices.IndexFunc(s.procs.list, func(pr monitor.Process) bool { return pr.Args == args })
		if i < 0 {
			return monitor.Process{}, false
		}
		return s.procs.list[i], true
	}
	var sleep, sleep2 monitor.Process
	p.until(t, "the sleeps listed", func() bool {
		var ok, ok2 bool
		sleep, ok = find(args)
		sleep2, ok2 = find(other)
		return ok && ok2
	})
	if s.procs.err != nil || len(s.procs.list) < 3 || sleep.Name != "sleep" || sleep.User == "" || sleep.Path == "" {
		t.Fatalf("list of %d, err %v, sleep %+v", len(s.procs.list), s.procs.err, sleep)
	}
	s.procs.filter = args
	p.until(t, "the sleep alone in the table", func() bool {
		shown := s.procs.shown()
		return len(shown) == 1 && shown[0].PID == sleep.PID && tt.HasText(args)
	})

	a.openProcTab(s, sleep)
	tab := s.procs.tabs[0]
	p.until(t, "the details", func() bool { return !tab.loading })
	if tab.err != nil || tab.d.Cwd == "" || tab.d.EnvDenied || !slices.Contains(tab.d.Env, monitor.EnvVar{Name: "FS_PROCS_TEST", Value: "from-finallyshell"}) {
		t.Errorf("details %+v, err %v", tab.d, tab.err)
	}

	a.askSignal(s, sleep, "TERM", true)
	a.sendSignal(a.processes.dlg)
	p.until(t, "the kill", func() bool { return a.processes.dlg == nil || a.processes.dlg.err != nil || a.processes.dlg.still })
	if d := a.processes.dlg; d != nil {
		t.Fatalf("kill: err %v, still running %v", d.err, d.still)
	}
	p.until(t, "the sleep gone", func() bool {
		_, ok := find(args)
		return !ok
	})

	// The other sleep ends outside the app while the dialog is open: the
	// signal is not sent, and the dialog says the process ended.
	a.askSignal(s, sleep2, "HUP", false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, _, err := monitor.RunScript(ctx, s.client, "kill -KILL "+strconv.Itoa(sleep2.PID)+"\n"); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the other sleep ended", func() bool {
		running, err := monitor.ProcessRunning(ctx, s.client, sleep2.PID, sleep2.Start)
		return err == nil && !running
	})
	a.sendSignal(a.processes.dlg)
	p.until(t, "the answer", func() bool { return a.processes.dlg != nil && !a.processes.dlg.busy })
	if d := a.processes.dlg; !d.gone || !errors.Is(d.err, monitor.ErrNoProcess) {
		t.Errorf("signal to an ended process: gone %v, err %v", d.gone, d.err)
	}
}

// The file panel follows a cd in the shell, which a plain bash doesn't
// report (no OSC 7): the server's /proc tells. A command that keeps the
// directory, the first one too, leaves the folder the user browsed.
func TestSSHFollowShell(t *testing.T) {
	a, p, s := connectTestServer(t)
	defer a.CloseAll()
	p.until(t, "the home directory", func() bool { return s.files.cwd != "" && !s.files.loading })
	home := s.files.cwd
	p.until(t, "the shell's folder as it starts", func() bool { return s.pane().shell.dir == home })
	run := func(cmd string) {
		s.term().Send([]byte(cmd + "\r"))
		s.shellCommandSent() // as Enter does
	}
	stays := func(dir string) {
		t.Helper()
		settle(p, 2*time.Second)
		if s.files.cwd != dir {
			t.Errorf("a command in the same folder moved the panel to %q, from %q", s.files.cwd, dir)
		}
	}
	a.openDir(s, "/etc")
	p.until(t, "the panel in /etc", func() bool { return s.files.cwd == "/etc" && !s.files.loading })
	run("true")
	stays("/etc")
	run("cd /usr/share")
	p.until(t, "the panel in /usr/share", func() bool { return s.files.cwd == "/usr/share" && !s.files.loading })
	a.openDir(s, "/etc")
	p.until(t, "the panel in /etc", func() bool { return s.files.cwd == "/etc" && !s.files.loading })
	run("true")
	stays("/etc")
	run("cd /var")
	p.until(t, "the panel in /var", func() bool { return s.files.cwd == "/var" && !s.files.loading })
}

// A program named claude in the shell's foreground shows Claude Code's
// logo on the tab, read from the server's /proc; Ctrl+C ends it, and the
// logo goes. The fake claude is a shell script that waits, in a folder of
// the test's own under the user's home.
func TestSSHAgentIcon(t *testing.T) {
	poll := agentPoll
	agentPoll = time.Second
	defer func() { agentPoll = poll }()
	a, p, s := connectTestServer(t)
	defer a.CloseAll()
	dir := ".finallyshell-agent-test-" + strconv.Itoa(rand.IntN(1_000_000))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, stderr, err := monitor.RunScript(ctx, s.client, "mkdir -p ~/"+dir+" && printf '#!/bin/sh\\nwhile :; do sleep 1; done\\n' >~/"+dir+"/claude && chmod +x ~/"+dir+"/claude\n"); err != nil {
		t.Fatal(err, stderr)
	}
	defer monitor.RunScript(context.Background(), s.client, "rm -rf ~/"+dir+"\n")
	p.until(t, "the shell's folder as it starts", func() bool { return s.pane().shell.dir != "" })
	if a.shownAgent(s) != nil {
		t.Fatalf("an agent at the prompt: %v", a.shownAgent(s))
	}
	s.term().Send([]byte("~/" + dir + "/claude --resume\r"))
	s.pane().shellKey(0, ui.KeyEnter) // as Enter does
	p.until(t, "Claude Code", func() bool { return a.shownAgent(s) != nil })
	if ag := a.shownAgent(s); ag.Name != "Claude Code" {
		t.Fatalf("agent %s", ag.Name)
	}
	s.term().Send([]byte{3})
	s.pane().shellKey(ui.Ctrl, ui.KeyC) // as Ctrl+C does
	p.until(t, "no agent", func() bool { return a.shownAgent(s) == nil })
}

// The Network tab on a real server: a listener the test starts, found by
// its port, with its connection; and a trace route, which says what the
// server lacks.
func TestSSHNetwork(t *testing.T) {
	a, p, s := connectTestServer(t)
	defer a.CloseAll()
	port := strconv.Itoa(40000 + rand.IntN(20000))
	py := `import socket, time; l = socket.socket(); l.bind(("127.0.0.1", ` + port + `)); l.listen(); c = socket.create_connection(("127.0.0.1", ` + port + `)); s, _ = l.accept(); time.sleep(60)`
	s.term().Send([]byte("python3 -c '" + py + "' &\r"))
	a.bottomTab = tabNetwork
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	title := "127.0.0.1:" + port
	var row monitor.NetGroup
	p.until(t, "the listener", func() bool {
		for _, g := range s.net.snap.Groups {
			if g.Listen && g.Title() == title && len(g.Conns) == 1 {
				row = g
				return true
			}
		}
		return false
	})
	if row.Name != "python3" || row.PID == 0 || row.Conns[0].Remote.Addr().String() != "127.0.0.1" {
		t.Errorf("row %+v", row)
	}
	s.net.filter = port
	p.until(t, "the row alone", func() bool { return len(s.net.shown()) >= 1 && tt.HasText("127.0.0.1") })
	a.openNetTab(s, row)
	tt.Frame()
	if !tt.HasText("ESTAB") || tt.HasText("Location") {
		t.Errorf("connections: %q", tt.Texts())
	}
	saveShot(t, tt, "ssh-network-en.png")

	a.startTrace(s, "127.0.0.1")
	tr := s.net.tabs[s.net.tab].trace
	p.until(t, "the trace", func() bool { return !tr.running })
	tt.Frame()
	switch r := tr.r; {
	case r.NoNextTrace && r.Tool == "":
		if !errors.Is(r.Err, monitor.ErrNoTraceTool) || !tt.HasText(monitor.NextTraceInstall) {
			t.Errorf("no tool: %+v", r)
		}
	case r.Err == nil:
		if len(r.Hops) == 0 {
			t.Errorf("%s: no hops", r.Tool)
		}
	}
	saveShot(t, tt, "ssh-network-trace-en.png")
}
