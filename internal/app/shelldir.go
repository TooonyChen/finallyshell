package app

import (
	"context"
	"sync/atomic"
	"time"

	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// shellDirState follows the working directory of a pane's shell, for
// the file panel to go where the focused pane's shell goes (see
// followShellDir), and the program in its foreground, for the tab to
// show the coding agent that runs there (agenticon.go).
type shellDirState struct {
	// dir is the shell's directory as last read, first as the shell
	// starts: "" before that. The panel moves when a command changes it,
	// so a command that leaves it, as ls, keeps the folder the user
	// browsed.
	dir string
	// wake asks the connection's follower to read the directory, and the
	// foreground program, after a command: nil while none runs.
	wake chan struct{}
	// agents is what the follower shares with the UI thread of the
	// foreground program: nil while none runs.
	agents *agentWatch
	// agent is the coding agent in the shell's foreground, as last read:
	// nil for none.
	agent *monitor.Agent
}

// shellDirReads are how long the follower waits after a command before
// each read of the directory: soon, for the prompt that comes back at
// once, and again later, for a slow link or a slow cd.
var shellDirReads = []time.Duration{250 * time.Millisecond, 1250 * time.Millisecond}

// shellDirTimeout is how long a read of the server waits for its answer.
const shellDirTimeout = 10 * time.Second

// readShellState reads the directory and foreground program of a shell
// on the server; tests stand in for it.
var readShellState = monitor.ShellState

// startShellDir starts following the directory of the shell term shows on
// client, p's of s's panes, and the program in its foreground, until ctx
// ends. A shell that reports its directory (OSC 7) tells it; else, when
// probe allows exec channels, the server's /proc does (monitor.ShellState,
// for the shell p.pick says), which also tells the program, while the tabs
// show agents. Runs on the UI thread.
//
// One goroutine reads, one read at a time, so a tab never has more than
// one in flight: after a command (wake), and after a key that can end or
// stop a program (agentWatch.wake), it reads as shellDirReads says, and,
// while the tab shows an agent, every agentPoll, so that the agent goes
// when it ends by itself.
func (a *App) startShellDir(ctx context.Context, s *Session, p *pane, client *ssh.Client, term *terminal.Terminal, probe bool) {
	wake := make(chan struct{}, 1)
	w := &agentWatch{wake: make(chan struct{}, 1)}
	w.on.Store(!a.settings.NoAgentIcons)
	p.shell = shellDirState{wake: wake, agents: w}
	delays, readServer, poll := shellDirReads, readShellState, agentPoll // as tests set them now
	// sent is the agent the follower last sent the UI thread, which the
	// tab shows once it has taken it, and sentTurn the setting's turn
	// then (agentWatch.turns): only the goroutine uses them.
	var sent *monitor.Agent
	sentTurn := w.turns.Load()
	// read reads the directory, when dir, and the foreground program,
	// when the tabs show agents, and reports whether the tab shows one
	// after it. A read that fails, as one that times out, tells nothing:
	// the tab keeps what it shows, and keeps reading while that is an
	// agent.
	read := func(dir, baseline bool) (agent bool) {
		turn := w.turns.Load() // before on: a turn read is never newer
		agents := probe && w.on.Load()
		d := ""
		if dir {
			d = term.Dir()
		}
		serverDir := dir && d == "" && probe
		var sh monitor.Shell
		var err error
		if serverDir || agents {
			rctx, cancel := context.WithTimeout(ctx, shellDirTimeout)
			sh, err = readServer(rctx, client, p.shellPick())
			cancel()
		}
		if serverDir {
			d = sh.Dir
		}
		ag := sent
		if agents && err == nil {
			ag = sh.Agent()
		}
		newDir := dir && d != ""
		// Only a change redraws: one from what was sent, though the UI
		// thread may not have taken it yet, or since the setting changed.
		newAgent := agents && err == nil && (ag != sent || turn != sentTurn)
		if ctx.Err() != nil || !newDir && !newAgent {
			return agents && ag != nil
		}
		if newAgent {
			sent, sentTurn = ag, turn
		}
		a.post(func() {
			if ctx.Err() != nil || p.shell.wake != wake {
				return
			}
			if newDir {
				a.shellDirRead(s, p, d, baseline)
			}
			if newAgent {
				a.shellAgentRead(p, ag)
			}
		})
		return agents && ag != nil
	}
	// The first reads, as the shell starts, are where it is before any
	// command, from which a command that keeps it is told apart: without
	// them, the first command's read would always be a change, and take
	// the panel from a folder the user browsed to. They don't move the
	// panel, which the user may be browsing already.
	wake <- struct{}{}
	shellFollowers.Add(1) // here, so that it counts from the start
	go func() {
		defer shellFollowers.Add(-1)
		baseline, agent := true, false
		for {
			// While the tab shows an agent, it reads again now and then.
			var tick *time.Timer
			var ticked <-chan time.Time
			if agent && w.on.Load() {
				tick = time.NewTimer(poll)
				ticked = tick.C
			}
			dir, polled := true, false
			select {
			case <-ctx.Done():
			case <-wake:
			case <-w.wake:
				dir = false // the agent alone: the directory follows commands
			case <-ticked:
				polled = true
			}
			if tick != nil {
				tick.Stop()
			}
			switch {
			case ctx.Err() != nil:
				return
			case polled:
				agent = read(false, false)
				continue
			}
			for i := 0; i < len(delays); {
				select {
				case <-ctx.Done():
					return
				case <-wake: // a command: wait for it from the start
					i, baseline, dir = 0, false, true
					continue
				case <-w.wake:
					i = 0
					continue
				case <-time.After(delays[i]):
				}
				agent = read(dir, baseline)
				i++
			}
			if dir {
				baseline = false
			}
		}
	}()
}

// shellFollowers counts the followers that run, for the tests to see
// that they end with their connection.
var shellFollowers atomic.Int64

// shellCommandSent tells the follower of the shell of s's focused pane
// that a command may have changed its directory: the command bar sent a
// line.
func (s *Session) shellCommandSent() {
	if p := s.pane(); p != nil {
		p.shellCommandSent()
	}
}

// shellCommandSent tells p's follower that a command may have changed
// the shell's directory: Enter was pressed in its terminal, or the
// command bar sent it a line.
func (p *pane) shellCommandSent() {
	if p.shell.wake != nil {
		select {
		case p.shell.wake <- struct{}{}:
		default:
		}
	}
}

// shellDirRead takes a read of the directory of the shell of p, one of
// s's panes, moving the file panel there when it changed, p has the
// focus and the panel follows the terminal; a baseline read, as the
// shell starts, only keeps it.
func (a *App) shellDirRead(s *Session, p *pane, dir string, baseline bool) {
	if dir == p.shell.dir {
		return
	}
	p.shell.dir = dir
	if !baseline && p == s.pane() {
		a.followShellDir(s)
	}
}

// followShellDir moves s's file panel to the directory of the shell of
// its focused pane, if the panel follows it and is elsewhere.
func (a *App) followShellDir(s *Session) {
	p := s.pane()
	if p == nil {
		return
	}
	d := p.shell.dir
	if a.settings.NoFollowTerminalDir || d == "" || s.sftp == nil || d == s.files.cwd {
		return
	}
	a.listDir(s, d, true)
}

// shellDir is the working directory of the shell of s's focused pane, as
// it reports it (OSC 7) or as last read from the server, or "".
func (s *Session) shellDir() string {
	p := s.pane()
	if p == nil {
		return ""
	}
	if p.term != nil {
		if d := p.term.Dir(); d != "" {
			return d
		}
	}
	return p.shell.dir
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
