package app

import (
	"sync/atomic"
	"time"

	"finallyshell/internal/icons"
	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// A session's tab shows the logo of the coding agent that runs in its
// shell, as Claude Code or Codex, next to its name: the shell's follower
// (startShellDir) reads the program in the terminal's foreground with the
// directory, from the server's /proc (monitor.ShellState), and tells the
// agent by monitor.FindAgent.

// agentPoll is how often a tab showing an agent reads again, for the
// logo to go when the agent ends by itself; tests shorten it.
var agentPoll = 10 * time.Second

// agentWatch is what a session's follower shares with the UI thread of
// the program in the shell's foreground.
type agentWatch struct {
	// on is whether the tabs show agents (the setting): the follower
	// reads the program only then.
	on atomic.Bool
	// turns counts the changes of on. The follower sends a read only
	// when it differs from the last it sent, which the UI thread may not
	// have taken yet; after a change of the setting, which clears what
	// the tab shows, it sends the next read whatever it is.
	turns atomic.Int64
	// wake asks the follower to read the program, as after Ctrl+C.
	wake chan struct{}
}

// shellKey tells s's follower of a key typed in the terminal: Enter
// sends a command, which may change the directory and start a program;
// Ctrl+C, Ctrl+D and Ctrl+Z end or stop one.
func (s *Session) shellKey(mods ui.Modifiers, key ui.Key) {
	switch {
	case key == ui.KeyEnter:
		s.shellCommandSent()
	case mods == ui.Ctrl && (key == ui.KeyC || key == ui.KeyD || key == ui.KeyZ):
		if w := s.shell.agents; w != nil && w.on.Load() {
			w.poke()
		}
	}
}

// poke asks the follower to read the program, unless a read is asked
// already.
func (w *agentWatch) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// shellAgentRead takes a read of the agent in s's shell, nil for none.
func (a *App) shellAgentRead(s *Session, ag *monitor.Agent) {
	if a.settings.NoAgentIcons {
		ag = nil // turned off since the read
	}
	s.shell.agent = ag
}

// shownAgent is the agent s's tab shows: nil while it shows none.
func (a *App) shownAgent(s *Session) *monitor.Agent {
	if a.settings.NoAgentIcons || s.status != statusConnected {
		return nil
	}
	return s.shell.agent
}

// applyAgentIcons tells every session's follower whether the tabs show
// agents, after the setting changed: turned on, they read the program
// now; turned off, the logos go, and so do the reads.
func (a *App) applyAgentIcons() {
	on := !a.settings.NoAgentIcons
	for _, s := range a.sessions {
		w := s.shell.agents
		if w == nil || w.on.Load() == on {
			continue
		}
		w.turns.Add(1) // before on, which the follower reads after it
		w.on.Store(on)
		if !on {
			s.shell.agent = nil
		}
		w.poke()
	}
}

// agentIcon is the logo of ag, or a generic one for an agent without.
func agentIcon(ag *monitor.Agent) *ui.SVG {
	if ag.Icon == "" {
		return icons.Get("bot")
	}
	return icons.Agent(ag.Icon)
}

// sessionName is the name line of s's row: its name, and after it the
// logo of the agent running in its shell, which the name is cut before.
func (a *App) sessionName(c *ui.Context, s *Session, active bool) {
	ag := a.shownAgent(s)
	if ag == nil {
		rowName(c, s.label(), active)
		return
	}
	ui.Row(c).Gap(5).AlignItems(ui.Center).MinWidth(0).Children(func() {
		rowName(c, s.label(), active).MinWidth(0).Shrink(1)
		ui.Icon(c, agentIcon(ag)).FontSize(14).Shrink(0).Label(ag.Name).Tooltip(ag.Name)
	})
}

// agentIconsOption is the Session tabs part of the Terminal page.
func (a *App) agentIconsOption(c *ui.Context) {
	ui.Column(c).Gap(8).Padding(16, 0, 0, 0).Children(func() {
		ui.Text(c, locales.TerminalSessionTabs()).Bold()
		on := !a.settings.NoAgentIcons
		if ui.Checkbox(c, &on, locales.OptionAgentIcons()).Changed() {
			a.changeSettings(func() { a.settings.NoAgentIcons = !on })
			a.applyAgentIcons()
		}
		widgets.Muted(c, locales.OptionAgentIconsNote()).FontSize(12).PaddingStart(18)
	})
}
