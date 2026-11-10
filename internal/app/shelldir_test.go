package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/monitor"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// followHome is a home folder with folders a, b and c in it.
func followHome(t *testing.T) string {
	t.Helper()
	home := filepath.ToSlash(t.TempDir())
	for _, d := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// followServer is a session connected to a test server that serves home
// over SFTP, with the keyboard in the terminal, and the directory reads
// soon after Enter. monitor turns the host's exec channels on, for the
// reads of the server (which the test server refuses, so tests stand in
// for them).
func followServer(t *testing.T, home string, monitor bool) (*App, *fakePlatform, *Session, *ui.Tester) {
	t.Helper()
	reads := shellDirReads
	shellDirReads = []time.Duration{10 * time.Millisecond, 30 * time.Millisecond}
	t.Cleanup(func() { shellDirReads = reads })
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
	s.term().Feed([]byte("tester@web-1:~$ "))
	tt.ClickAt(760, 200) // in the terminal
	return a, p, s, tt
}

// reportDir has the shell report dir as shells do with OSC 7.
func reportDir(s *Session, dir string) {
	s.term().Feed([]byte("\x1b]7;file://web-1" + dir + "\x07"))
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
	home := followHome(t)
	a, p, s, tt := followServer(t, home, false)
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
// directory and foreground program, counting the reads.
type fakeShellDir struct {
	mu    sync.Mutex
	dir   string
	fg    []string // the foreground program's arguments
	err   error    // what the reads fail with, if they do
	reads int
	// paneDirs are the folders of the shells of a split session, by
	// their pick's index, over dir; picks are the picks read.
	paneDirs map[int]string
	picks    []monitor.ShellPick
}

// setPane sets the folder of the shell of pick index i.
func (fd *fakeShellDir) setPane(i int, dir string) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	if fd.paneDirs == nil {
		fd.paneDirs = map[int]string{}
	}
	fd.paneDirs[i] = dir
}

// failWith has the reads fail with err; nil reads again.
func (fd *fakeShellDir) failWith(err error) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.err = err
}

// run has the shell run args in its foreground: none for its prompt.
func (fd *fakeShellDir) run(args ...string) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.fg = args
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

func (fd *fakeShellDir) read(ctx context.Context, client *ssh.Client, pick monitor.ShellPick) (monitor.Shell, error) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.reads++
	fd.picks = append(fd.picks, pick)
	if fd.err != nil {
		return monitor.Shell{}, fd.err
	}
	sh := monitor.Shell{Dir: fd.dir, Comm: "bash", Args: []string{"-bash"}}
	if d, ok := fd.paneDirs[pick.Index]; ok {
		sh.Dir = d
	}
	if len(fd.fg) > 0 {
		sh.Comm, sh.Args = path.Base(fd.fg[0]), fd.fg
	}
	return sh, nil
}

// standInShell has the followers read fd in place of the server.
func standInShell(t *testing.T, fd *fakeShellDir) {
	read := readShellState
	readShellState = fd.read
	t.Cleanup(func() { readShellState = read })
}

// A shell that reports nothing is read from the server as the session
// starts, and after a command, typed in the terminal or sent by the
// command bar, and never otherwise. The reads at the start tell a first
// command that stays in the folder from a cd: a folder the user browsed
// to before it stays.
func TestFilesFollowShellRead(t *testing.T) {
	home := followHome(t)
	fd := &fakeShellDir{dir: home}
	standInShell(t, fd)
	a, p, s, tt := followServer(t, home, true)
	f := &s.files

	settle(p, 150*time.Millisecond)
	start := len(shellDirReads)
	if n := fd.count(); n != start {
		t.Fatalf("%d reads before any command, want %d", n, start)
	}
	a.openDir(s, home+"/c")
	p.until(t, "the panel in c", func() bool { return f.cwd == home+"/c" && !f.loading })
	tt.ClickAt(760, 200)
	tt.Key(0, ui.KeyEnter) // ls, in the home folder
	settle(p, 150*time.Millisecond)
	if f.cwd != home+"/c" {
		t.Fatalf("the first command, which kept the shell's folder, moved the panel to %q", f.cwd)
	}
	if n := fd.count(); n != start+len(shellDirReads) {
		t.Errorf("%d reads after a command, want %d", n, start+len(shellDirReads))
	}

	fd.set(home + "/a")
	tt.Key(0, ui.KeyEnter)
	p.until(t, "the panel in a", func() bool { return f.cwd == home+"/a" && !f.loading })

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
