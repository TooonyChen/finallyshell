package app

import (
	"context"
	"time"

	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// shellDirState follows the working directory of a session's shell, for
// the file panel to go where the shell goes (see followShell).
type shellDirState struct {
	// dir is the shell's directory as last read: "" before the first
	// read of the connection. The panel moves when it changes, so a
	// command that leaves it, as ls, keeps the folder the user browsed.
	dir string
	// wake asks the connection's follower to read the directory, after
	// a command: nil while none runs.
	wake chan struct{}
}

// shellDirReads are how long the follower waits after a command before
// each read of the directory: soon, for the prompt that comes back at
// once, and again later, for a slow link or a slow cd.
var shellDirReads = []time.Duration{250 * time.Millisecond, 1250 * time.Millisecond}

// shellDirTimeout is how long a read of the server waits for its answer.
const shellDirTimeout = 10 * time.Second

// readShellDir reads the shell's directory on the server; tests stand in
// for it.
var readShellDir = monitor.ShellDir

// startShellDir starts following the directory of the shell term shows on
// client, until ctx ends. A shell that reports its directory (OSC 7)
// tells it; else, when probe allows exec channels, the server's /proc
// does (monitor.ShellDir). Runs on the UI thread.
func (a *App) startShellDir(ctx context.Context, s *Session, client *ssh.Client, term *terminal.Terminal, probe bool) {
	wake := make(chan struct{}, 1)
	s.shell = shellDirState{wake: wake}
	delays, readServer := shellDirReads, readShellDir // as tests set them now
	read := func() {
		d := term.Dir()
		if d == "" && probe {
			rctx, cancel := context.WithTimeout(ctx, shellDirTimeout)
			d, _ = readServer(rctx, client)
			cancel()
		}
		if d == "" || ctx.Err() != nil {
			return
		}
		a.post(func() {
			if ctx.Err() == nil && s.shell.wake == wake {
				a.shellDirRead(s, d)
			}
		})
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
			for i := 0; i < len(delays); {
				select {
				case <-ctx.Done():
					return
				case <-wake: // another command: wait for it from the start
					i = 0
					continue
				case <-time.After(delays[i]):
				}
				read()
				i++
			}
		}
	}()
}

// shellCommandSent tells s's follower that a command may have changed the
// shell's directory: Enter was pressed in the terminal, or the command
// bar sent a line.
func (s *Session) shellCommandSent() {
	if s.shell.wake != nil {
		select {
		case s.shell.wake <- struct{}{}:
		default:
		}
	}
}

// shellDirRead takes a read of s's shell directory, moving the file panel
// there when it changed and the panel follows the terminal.
func (a *App) shellDirRead(s *Session, dir string) {
	if dir == s.shell.dir {
		return
	}
	s.shell.dir = dir
	a.followShellDir(s)
}

// followShellDir moves s's file panel to the shell's directory, if the
// panel follows it and is elsewhere.
func (a *App) followShellDir(s *Session) {
	d := s.shell.dir
	if a.settings.NoFollowTerminalDir || d == "" || s.sftp == nil || d == s.files.cwd {
		return
	}
	a.listDir(s, d, true)
}

// shellDir is the shell's working directory, as it reports it (OSC 7) or
// as last read from the server, or "".
func (s *Session) shellDir() string {
	if s.term != nil {
		if d := s.term.Dir(); d != "" {
			return d
		}
	}
	return s.shell.dir
}

// followButton is the file panel's toggle for following the terminal's
// directory, for every session.
func (a *App) followButton(c *ui.Context, s *Session) {
	on := !a.settings.NoFollowTerminalDir
	b := widgets.ToggleIconButton(c, "folder-sync", locales.FollowTerminalDir(), on)
	if b.Clicked() {
		a.changeSettings(func() { a.settings.NoFollowTerminalDir = on })
		if !on { // turned on: go where the shell is now
			a.followShellDir(s)
		}
	}
}
