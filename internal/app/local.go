package app

import (
	"errors"
	"slices"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// A local terminal is a tab whose terminal runs a program on this
// computer, in a pseudo-terminal of MyGo's terminal plugin, rather than
// a shell on a server: the Local terminal program of Options › Terminal
// (Settings.LocalShell), else the user's login shell, or PowerShell on
// Windows, in the home folder.
//
// It is a Session with local set and no Host, so it is neither a new tab
// nor a session of a server: its row, terminal, find bar, command bar and
// the Commands tab work as a session's, sending to the program, and the
// Transfers tab shows the queue of every session as on any tab; what
// reads the tab's server (the monitor, the Files, Tunnels, Processes and
// Network pages, the coding agent's icon, Tab completion, the server's
// history) says it is not there or does nothing. Its command
// bar keeps a history of its own, under localHistoryID. It has no panel
// row, it is not in "All sessions" of the send target (sendtarget.go),
// and nothing of it is saved: local terminals do not come back after a
// restart.
//
// Its states are a session's: connected while the program runs, closed
// with errShellExited once it exits, failed when it could not start.
// Reconnecting starts the program again in the same tab. Splitting it
// runs the program once more in the new pane (panes.go); a pane whose
// program exits closes, the last one closing the tab's as above.

// localHistoryID is the command bar's history of local terminals, kept
// with the hosts' (whose IDs are "h" and digits) in history/local.txt.
const localHistoryID = "local"

// errShellExited is a local terminal whose program exited.
var errShellExited = errors.New("shell exited")

// localStartError is a local terminal whose program could not start:
// the program of the setting, or the setting itself (a quote not
// closed).
type localStartError struct {
	program string
	err     error
}

func (e *localStartError) Error() string { return e.program + ": " + e.err.Error() }

func (e *localStartError) Unwrap() error { return e.err }

// isLocal tells whether s is a local terminal; false for nil.
func (s *Session) isLocal() bool { return s != nil && s.local }

// historyID is the key of s's command bar history: its host's ID, or
// localHistoryID for a local terminal.
func (s *Session) historyID() string {
	if s.isLocal() {
		return localHistoryID
	}
	return s.Host.ID
}

// openLocal opens a local terminal in tab, a new tab, or at the end of
// the list for nil, selected as a new session is (Settings.NoAutoSelectTab).
func (a *App) openLocal(tab *Session) {
	if tab != nil && tab.isNewTab() && !tab.isPanelTab() && slices.Contains(a.sessions, tab) {
		tab.local = true
		a.active = slices.Index(a.sessions, tab)
		a.connectLocal(tab)
		return
	}
	s := newSession(nil)
	s.local = true
	a.sessions = append(a.sessions, s)
	if !a.settings.NoAutoSelectTab || a.current() == nil {
		a.active = len(a.sessions) - 1
	}
	a.connectLocal(s)
}

// connectLocal starts s's program, in a terminal of its own, in each of
// its panes: one for a tab starting, every pane of its layout when it
// starts again. Making the terminal finds the program on the PATH and
// returns at once; the program starts as a view first shows it. A
// program that is not found fails the tab with the error; one that
// cannot start once found writes its error in the terminal and exits.
func (a *App) connectLocal(s *Session) {
	s.disconnect()
	if s.panes.root == nil {
		s.panes.reset(newPane())
	}
	for _, p := range s.panes.panes() {
		p.title = ""
		term, err := a.openLocalPane(s, p)
		if err != nil {
			s.status, s.err = statusFailed, err
			return
		}
		p.term = term
		s.panes.opened(p)
	}
	s.panes.refocus = true
	s.status, s.err = statusConnected, nil
}

// openLocalPane starts s's program for p, one of its panes, in a
// terminal of its own.
func (a *App) openLocalPane(s *Session, p *pane) (*terminal.Terminal, error) {
	cmd, err := a.settings.LocalShellCommand()
	if err != nil {
		return nil, &localStartError{program: a.settings.LocalShell, err: err}
	}
	opts := a.terminalOptions(nil)
	opts.Command = cmd
	var term *terminal.Terminal
	opts.OnTitle = func(title string) {
		a.post(func() {
			if p.term == term {
				p.title = title
			}
		})
	}
	// Not when the tab ended it itself, as it disconnects, closes or
	// starts again: that terminal is gone from the pane already.
	opts.OnExit = func(int) { a.post(func() { a.paneExited(s, p) }) }
	term, err = terminal.New(opts)
	if err != nil {
		program := "shell"
		if len(cmd) > 0 {
			program = cmd[0]
		}
		return nil, &localStartError{program: program, err: err}
	}
	return term, nil
}

// target is where s's shell runs, as lists of sessions say it under or
// beside its name: its host's user@address, or "This computer".
func (s *Session) target() string {
	if s.isLocal() {
		return locales.LocalThisComputer()
	}
	return s.Host.Target()
}

// sessionMark is the mark before a session's name: its status dot, or
// for a local terminal a computer in the dot's color, as it is no
// server, as wide as the dot so that the names line up. On the selected
// row of the list (active) the computer takes the row's text color, as
// its thin lines would not show in the dot's on the accent; the line
// under the name says the state.
func sessionMark(c *ui.Context, s *Session, active bool) {
	if !s.isLocal() {
		widgets.StatusDot(c, s.statusColor(theme))
		return
	}
	color := s.statusColor(theme)
	if active {
		color = theme.AccentText
	}
	ui.Box(c).Size(8, 8).Center().Shrink(0).Children(func() {
		ui.Icon(c, icons.Get("laptop")).FontSize(13).TextColor(color)
	})
}

// localUnavailable is what a page of the bottom panel that reads a
// server shows for a local terminal.
func localUnavailable(c *ui.Context) {
	ui.Column(c.Key("local-unavailable")).Grow(1).Children(func() {
		widgets.Muted(c, locales.LocalNotAvailable()).Padding(12)
	})
}

// localMonitor is the server monitor's place for a local terminal: a
// muted line, no stats, no polling.
func localMonitor(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(10, 10, 8, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Get("laptop")).FontSize(14).TextColor(theme.TextMuted)
		widgets.Muted(c, locales.LocalTerminal()).SingleLine().MinWidth(0).Shrink(1)
	})
}

// localShellOption is the Local terminal part of the Terminal page: the
// program local terminals run.
func (a *App) localShellOption(c *ui.Context) {
	ui.Column(c).Gap(8).Padding(16, 0, 0, 0).Children(func() {
		ui.Text(c, locales.LocalTerminal()).Bold()
		ui.Form(c, func() {
			ui.Field(c, locales.OptionLocalShell(), func() {
				v := a.settings.LocalShell
				if ui.TextInput(c, &v).Placeholder(locales.OptionLocalShellDefault()).Width(360).Shrink(1).MinWidth(120).
					Label(locales.OptionLocalShell()).Changed() {
					a.changeSettings(func() { a.settings.LocalShell = v })
				}
			}).Description(locales.OptionLocalShellNote())
		})
	})
}

// localQuitCounts are the sessions quitting ends: servers' connected or
// connecting, and local terminals whose program runs.
func (a *App) localQuitCounts() (remote, local int) {
	for _, s := range a.sessions {
		if s.status != statusConnected && s.status != statusConnecting {
			continue
		}
		if s.isLocal() {
			local++
		} else {
			remote++
		}
	}
	return remote, local
}
