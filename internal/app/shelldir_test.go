package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// followServer is a session connected to a test server over SFTP, with
// folders a, b and c in its home, the keyboard in the terminal, and the
// directory reads soon after Enter. monitor turns the host's exec
// channels on, for the reads of the server (which the test server
// refuses, so tests stand in for them).
func followServer(t *testing.T, monitor bool) (*App, *fakePlatform, *Session, *ui.Tester, string) {
	t.Helper()
	reads := shellDirReads
	shellDirReads = []time.Duration{10 * time.Millisecond, 30 * time.Millisecond}
	t.Cleanup(func() { shellDirReads = reads })
	home := filepath.ToSlash(t.TempDir())
	for _, d := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	srv := sshtest.StartSFTP(t, home)
	a, p := testApp(t)
	h := &config.Host{ID: "follow", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: !monitor}
	a.hosts.Add(h)
	a.passwords[h.ID] = sshtest.Password
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	s := a.current()
	p.until(t, "the files", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusFailed || s.files.cwd == home && !s.files.loading
	})
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	t.Cleanup(a.CloseAll)
	s.term.Feed([]byte("tester@web-1:~$ "))
	tt.ClickAt(760, 200) // in the terminal
	return a, p, s, tt, home
}

// reportDir has the shell report dir as shells do with OSC 7.
func reportDir(s *Session, dir string) {
	s.term.Feed([]byte("\x1b]7;file://web-1" + dir + "\x07"))
}

// settle runs what the app posts for d, longer than the directory reads
// after a command take.
func settle(p *fakePlatform, d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case fn := <-p.posts:
			fn()
		case <-deadline:
			if p.tt != nil {
				p.tt.Frame()
			}
			return
		}
	}
}

// After a command, the file panel goes to the shell's directory, as the
// shell reports it, only when that changed: a folder the user browsed
// stays until the next cd. The toolbar's toggle turns it off for every
// session, and back on at the shell's folder; a folder SFTP can't open
// leaves the panel as it was, with no error.
func TestFilesFollowShell(t *testing.T) {
	a, p, s, tt, home := followServer(t, false)
	f := &s.files

	reportDir(s, home+"/a")
	tt.Key(0, ui.KeyEnter)
	p.until(t, "the panel in a", func() bool { return f.cwd == home+"/a" && !f.loading })
	saveShot(t, tt, "files-follow-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "files-follow-zh-CN.png")
	p.SetLocale("en")

	// The user browses to b; a command that stays in a keeps it there.
	a.openDir(s, home+"/b")
	p.until(t, "the panel in b", func() bool { return f.cwd == home+"/b" && !f.loading })
	tt.Key(0, ui.KeyEnter)
	settle(p, 150*time.Millisecond)
	if f.cwd != home+"/b" {
		t.Fatalf("a command in the same folder moved the panel to %q", f.cwd)
	}

	// Off: a cd leaves the panel; on again, it goes there.
	if err := tt.Click(locales.FollowTerminalDir()); err != nil {
		t.Fatal(err)
	}
	if !a.settings.NoFollowTerminalDir {
		t.Fatal("the toggle did not turn following off")
	}
	saveShot(t, tt, "files-follow-off-en.png")
	tt.ClickAt(760, 200) // back in the terminal
	reportDir(s, home+"/c")
	tt.Key(0, ui.KeyEnter)
	settle(p, 150*time.Millisecond)
	if f.cwd != home+"/b" {
		t.Fatalf("following off, the panel went to %q", f.cwd)
	}
	if err := tt.Click(locales.FollowTerminalDir()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the panel in c", func() bool { return f.cwd == home+"/c" && !f.loading })
	if a.settings.NoFollowTerminalDir {
		t.Fatal("the toggle did not turn following on")
	}
	tt.ClickAt(760, 200)

	// A folder the panel can't open, as one on another host the shell
	// went to with ssh.
	reportDir(s, home+"/missing")
	tt.Key(0, ui.KeyEnter)
	settle(p, 150*time.Millisecond)
	if f.cwd != home+"/c" || f.err != nil || f.pathEdit != home+"/c" {
		t.Fatalf("a missing folder: panel in %q, path %q, error %v", f.cwd, f.pathEdit, f.err)
	}
}

// fakeShellDir stands in for the server's answer of the shell's
// directory, counting the reads.
type fakeShellDir struct {
	mu    sync.Mutex
	dir   string
	reads int
}

func (fd *fakeShellDir) set(dir string) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.dir = dir
}

func (fd *fakeShellDir) count() int {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return fd.reads
}

func (fd *fakeShellDir) read(ctx context.Context, client *ssh.Client) (string, error) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.reads++
	return fd.dir, nil
}

// A shell that reports nothing is read from the server after a command,
// typed in the terminal or sent by the command bar, and never otherwise.
func TestFilesFollowShellRead(t *testing.T) {
	fd := &fakeShellDir{}
	read := readShellDir
	readShellDir = fd.read
	t.Cleanup(func() { readShellDir = read })
	a, p, s, tt, home := followServer(t, true)
	f := &s.files

	settle(p, 150*time.Millisecond)
	if n := fd.count(); n != 0 {
		t.Fatalf("%d reads before any command", n)
	}
	fd.set(home + "/a")
	tt.Key(0, ui.KeyEnter)
	p.until(t, "the panel in a", func() bool { return f.cwd == home+"/a" && !f.loading })
	settle(p, 150*time.Millisecond)
	if n := fd.count(); n != len(shellDirReads) {
		t.Errorf("%d reads after a command, want %d", n, len(shellDirReads))
	}

	// The command bar's line, and the path picker's folder.
	fd.set(home + "/b")
	if err := tt.Click(locales.CmdBarLabel()); err != nil {
		t.Fatal(err)
	}
	tt.Type("cd ../b")
	tt.Key(0, ui.KeyEnter)
	p.until(t, "the panel in b", func() bool { return f.cwd == home+"/b" && !f.loading })
	if got := a.listKey(s, "x"); got != home+"/b/x" {
		t.Errorf("the path picker lists %q", got)
	}
}
