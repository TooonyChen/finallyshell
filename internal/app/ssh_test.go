package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"finalshelly/internal/config"
	"finalshelly/internal/remotefs"

	"github.com/egoist/mygo/ui"
)

// TestSSH runs against a real server, named as user:password@host:port
// by FINALSHELLY_TEST_SSH:
//
//	FINALSHELLY_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/app -run SSH
func TestSSH(t *testing.T) {
	a, p, s := connectTestServer(t)
	s.term.Send([]byte("echo hello-$((40+2))\r"))
	p.until(t, "the shell's output", func() bool { return strings.Contains(s.term.Text(), "hello-42") })
	p.until(t, "the server's stats", func() bool { return s.stats.OK && s.stats.MemTotal > 0 && len(s.stats.LatHist) >= 2 })
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
	os.WriteFile(local, []byte("hi from finalshelly"), 0o644)
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
	if data, _ := os.ReadFile(filepath.Join(p.downloads, "hello.txt")); string(data) != "hi from finalshelly" {
		t.Errorf("round trip %q", data)
	}
	s.sftp.Remove(home + "/hello.txt")

	// The shell's TERM is the host's terminal type.
	s.term.Send([]byte("echo term-$TERM\r"))
	p.until(t, "the default TERM", func() bool { return strings.Contains(s.term.Text(), "term-xterm-256color") })

	// Reconnecting asks nothing: the key is known now. It takes the
	// host's changes.
	s.Host.TermType, s.Host.Backspace = "vt220", config.BackspaceCtrlH
	a.reconnect(s)
	p.until(t, "the reconnection", func() bool { return s.status == statusConnected || s.status == statusFailed || s.hostKeyQ != nil })
	if s.status != statusConnected {
		t.Fatalf("reconnect: status %d err %v question %v", s.status, s.err, s.hostKeyQ != nil)
	}
	s.term.Send([]byte("echo term-$TERM\r"))
	p.until(t, "the host's TERM", func() bool { return strings.Contains(s.term.Text(), "term-vt220") })
	s.term.Send([]byte("stty -a | tr ';' '\\n' | grep -w erase | tr -d ' ' | sed 's/^/stty-/'\r"))
	p.until(t, "the erase character", func() bool { return strings.Contains(s.term.Text(), "stty-erase=^H") })
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

// connectTestServer opens a session to the server FINALSHELLY_TEST_SSH
// names, trusting its key, and waits for the shell; it skips the test
// when the variable is not set.
func connectTestServer(t *testing.T) (*App, *fakePlatform, *Session) {
	t.Helper()
	spec := os.Getenv("FINALSHELLY_TEST_SSH")
	if spec == "" {
		t.Skip("FINALSHELLY_TEST_SSH not set")
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
