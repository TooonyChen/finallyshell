package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"github.com/egoist/mygo/ui"

	"github.com/egoist/mygo/plugins/terminal"
)

// Alternate-screen applications can use any of the three VT modes.
// Reconnecting must preserve the main screen's tail in every case.
func TestAuditReconnectAlternateModes(t *testing.T) {
	for _, mode := range []int{47, 1047, 1049} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			conn := newPaneConn(&transcriptConn{Reader: bytes.NewReader(nil)})
			previous, err := terminal.New(terminal.Options{Conn: conn})
			if err != nil {
				t.Fatal(err)
			}
			defer previous.Close()
			previous.Resize(40, 8) // include main-screen text and scrollback
			for i := range 12 {
				previous.Feed([]byte(fmt.Sprintf("main row %02d\r\n", i)))
			}
			previous.Feed([]byte(fmt.Sprintf("\x1b[?%dh\x1b[Halternate program", mode)))
			conn.start()
			historyDone(t, previous)
			nextConn := newPaneConn(&transcriptConn{Reader: bytes.NewReader(nil)})
			next, err := terminal.New(terminal.Options{Conn: nextConn})
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			if err := restorePaneHistory(context.Background(), next, previous); err != nil {
				t.Fatal(err)
			}
			history := string(next.History())
			for i := range 12 {
				if !strings.Contains(history, fmt.Sprintf("main row %02d", i)) {
					t.Fatalf("mode %d lost main row %d: %q", mode, i, history)
				}
			}
		})
	}
}

func TestAuditSSHReconnectAlternateMainTail(t *testing.T) {
	a, p, s, _, srv := paneHost(t, followHome(t), false)
	var output strings.Builder
	for i := range 12 {
		fmt.Fprintf(&output, "main row %02d\r\n", i)
	}
	output.WriteString("\x1b[?47h\x1b[Halternate program")
	if err := srv.WriteShell(0, []byte(output.String())); err != nil {
		t.Fatal(err)
	}
	p.until(t, "alternate program", func() bool { return strings.Contains(s.term().Text(), "alternate program") })
	srv.DropConnections()
	p.until(t, "connection lost", func() bool { return s.status == statusClosed })
	a.reconnect(s)
	p.until(t, "reconnected", func() bool { return s.status != statusConnecting })
	if s.status != statusConnected || !strings.Contains(string(s.term().History()), "main row 11") {
		t.Fatal("real SSH reconnect lost the main screen tail after mode 47")
	}
}

// A queued failure belongs to the client that requested SFTP. Delivering
// it after a successful reconnect must not turn the current files red.
func TestAuditSSHReconnectStaleSFTPFailure(t *testing.T) {
	oldServer := sshtest.Start(t) // shell works; SFTP is refused
	a, p := testApp(t)
	h := &config.Host{ID: "stale-sftp", Name: "old", Addr: oldServer.Host, Port: oldServer.Port, User: "root", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	a.passwords[h.ID] = sshtest.Password
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	s := a.current()
	defer a.CloseAll()
	p.until(t, "first SSH shell", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status != statusConnecting
	})
	if s.status != statusConnected {
		t.Fatalf("first login: %v", s.err)
	}
	var queuedFailure func()
	select {
	case queuedFailure = <-p.posts:
	case <-time.After(5 * time.Second):
		t.Fatal("no SFTP failure callback")
	}
	nextServer := sshtest.StartSFTP(t, followHome(t))
	h.Addr, h.Port = nextServer.Host, nextServer.Port
	a.reconnect(s)
	p.until(t, "new SSH and SFTP", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusConnected && s.sftp != nil && !s.files.loading
	})
	if s.files.err != nil {
		t.Fatalf("new files failed: %v", s.files.err)
	}
	queuedFailure()
	if s.files.err != nil {
		t.Fatalf("old attempt overwrote new SFTP result: %v", s.files.err)
	}
}

func TestAuditSSHReconnectStaleDirectoryFailure(t *testing.T) {
	a, p, s, _, _ := paneHost(t, followHome(t), false)
	settle(p, 100*time.Millisecond) // finish the initial directory-tree reads
	a.openDir(s, s.files.cwd+"/missing-for-audit")
	var queuedFailure func()
	select {
	case queuedFailure = <-p.posts:
	case <-time.After(5 * time.Second):
		t.Fatal("no directory failure callback")
	}
	a.reconnect(s)
	p.until(t, "new SSH and files", func() bool {
		return s.status == statusConnected && s.sftp != nil && !s.files.loading
	})
	if s.files.err != nil {
		t.Fatalf("new files failed: %v", s.files.err)
	}
	queuedFailure()
	if s.files.err != nil {
		t.Fatalf("old directory result overwrote reconnect: %v", s.files.err)
	}
}

func TestAuditSSHReconnectStaleTreeResult(t *testing.T) {
	a, p, s, _, _ := paneHost(t, followHome(t), false)
	settle(p, 100*time.Millisecond)
	dir := s.files.cwd
	a.loadSubdirs(s, dir)
	var queuedResult func()
	select {
	case queuedResult = <-p.posts:
	case <-time.After(5 * time.Second):
		t.Fatal("no directory tree result")
	}
	newChild := filepath.Join(dir, "new-after-reconnect")
	if err := os.Mkdir(newChild, 0o755); err != nil {
		t.Fatal(err)
	}
	a.reconnect(s)
	p.until(t, "new SSH and files", func() bool { return s.status == statusConnected && s.sftp != nil && !s.files.loading })
	if s.files.treeBusy[dir] {
		t.Error("reconnect retained a canceled tree read as busy")
	}
	if !slices.Contains(s.files.subdirs[dir], newChild) {
		t.Fatalf("new directory tree: %v", s.files.subdirs[dir])
	}
	queuedResult()
	if !slices.Contains(s.files.subdirs[dir], newChild) {
		t.Fatalf("old tree callback overwrote current result: %v", s.files.subdirs[dir])
	}
}
