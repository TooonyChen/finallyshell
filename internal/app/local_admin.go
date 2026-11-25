package app

import (
	"context"
	"errors"
	"os"
	"slices"

	"finallyshell/internal/icons"
	"finallyshell/internal/localterm"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

type localAdminState struct {
	initialized, supported, elevated bool
	err                              error
	start                            localterm.Starter
	pending                          *adminLaunch
	icon                             *mygo.MenuIcon
}

type adminLaunch struct {
	s      *Session
	cancel context.CancelFunc
}

func (a *App) initLocalAdmin() {
	if a.localAdmin.initialized {
		return
	}
	a.localAdmin.initialized = true
	a.localAdmin.supported = localterm.Available()
	a.localAdmin.elevated, a.localAdmin.err = localterm.Elevated()
	a.localAdmin.start = localterm.Start
}

func (a *App) ordinaryLocalDisabled() bool {
	a.initLocalAdmin()
	return a.localAdmin.supported && (a.localAdmin.elevated || a.localAdmin.err != nil)
}

// localLaunchButton keeps the existing one-click ordinary launch. Only Windows
// gets the adjacent arrow, with the same menu in the sidebar and start page.
func (a *App) localLaunchButton(c *ui.Context, tab *Session, compact bool) bool {
	a.initLocalAdmin()
	clicked := false
	ui.Row(c.Key("local-launch")).Gap(0).AlignItems(ui.Stretch).Shrink(0).Children(func() {
		var b ui.Element
		if compact {
			b = headerButton(c, "square-terminal", locales.LocalTerminal())
		} else {
			b = localButton(c)
		}
		b.Disabled(a.ordinaryLocalDisabled())
		if a.ordinaryLocalDisabled() {
			b.Tooltip(locales.LocalRegularUnavailable())
		}
		clicked = b.Clicked()
		if !a.localAdmin.supported {
			return
		}
		arrow := widgets.IconButton(c, "chevron-down", locales.LocalTerminalPrivileges()).Width(20).Shrink(0)
		if compact {
			arrow.Width(12)
		}
		arrow.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(locales.LocalRegular()).Disabled(a.ordinaryLocalDisabled()).Chosen() {
				a.openLocal(tab)
			}
			if a.localAdmin.icon == nil {
				a.localAdmin.icon, _ = mygo.NewMenuIconSVG(icons.Get("shield"), true)
			}
			if m.Item(locales.LocalOpenAdministrator()).Icon(a.localAdmin.icon).Disabled(a.localAdmin.pending != nil || a.localAdmin.err != nil).Chosen() {
				a.openAdminLocal(tab)
			}
		}, ui.PopoverMenuButtonOptions{At: ui.AnchorTopEnd, Self: ui.AnchorBottomEnd})
	})
	return clicked
}

func (a *App) cancelAdminLaunch(s *Session, all bool) {
	if l := a.localAdmin.pending; l != nil && (all || l.s == s) {
		a.localAdmin.pending = nil
		l.cancel()
	}
}

// launchAdmin snapshots settings on the UI thread, waits for UAC off it, and
// hands results back only while this exact request is still current.
func (a *App) launchAdmin(s *Session, count int, done func([]localterm.Process, error)) {
	a.initLocalAdmin()
	if !a.localAdmin.supported {
		a.notify(locales.LocalAdminUnavailable())
		return
	}
	if a.localAdmin.err != nil {
		a.notify(adminError(a.localAdmin.err))
		return
	}
	if a.localAdmin.pending != nil {
		a.notify(locales.LocalAdminWaiting())
		return
	}
	cmd, err := a.settings.LocalShellCommand()
	if err != nil {
		a.notify(adminError(err))
		return
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		a.notify(adminError(err))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &adminLaunch{s: s, cancel: cancel}
	a.localAdmin.pending = l
	start := a.localAdmin.start
	go func() {
		var processes []localterm.Process
		var stops []func() bool
		var err error
		for range count {
			var proc localterm.Process
			proc, err = start(ctx, cmd, dir)
			if err != nil {
				break
			}
			if proc == nil {
				err = errors.New("no administrator terminal process")
				break
			}
			processes = append(processes, proc)
			stops = append(stops, context.AfterFunc(ctx, func() { proc.Close() }))
		}
		a.post(func() {
			valid := a.localAdmin.pending == l && ctx.Err() == nil
			for _, stop := range stops {
				if !stop() {
					valid = false
				}
			}
			if !valid || err != nil {
				for _, proc := range processes {
					proc.Close()
				}
			}
			if !valid {
				cancel()
				return
			}
			a.localAdmin.pending = nil
			defer cancel()
			done(processes, err)
		})
	}()
}

func adminError(err error) string {
	if errors.Is(err, localterm.ErrCanceled) || errors.Is(err, context.Canceled) {
		return locales.LocalAdminCanceled()
	}
	return locales.LocalAdminFailed(err.Error())
}

func (a *App) openAdminLocal(tab *Session) {
	a.launchAdmin(tab, 1, func(procs []localterm.Process, err error) {
		if err != nil {
			a.notify(adminError(err))
			return
		}
		if tab != nil && (!slices.Contains(a.sessions, tab) || !tab.isNewTab()) {
			procs[0].Close()
			return
		}
		s := tab
		if s == nil {
			s = newSession(nil)
		}
		p := newPane()
		term, err := a.adminTerminal(s, p, procs[0])
		if err != nil {
			procs[0].Close()
			a.notify(adminError(err))
			return
		}
		if tab == nil {
			a.sessions = append(a.sessions, s)
			if !a.settings.NoAutoSelectTab || a.current() == nil {
				a.active = len(a.sessions) - 1
			}
		}
		s.local, s.localElevated = true, true
		s.panes.reset(p)
		p.term = term
		s.panes.opened(p)
		s.panes.refocus = true
		s.status, s.err = statusConnected, nil
	})
}

func (a *App) adminTerminal(s *Session, p *pane, proc localterm.Process) (*terminal.Terminal, error) {
	opts := a.terminalOptions(nil)
	proc.Activate()
	opts.Command = proc.CommandLine()
	var term *terminal.Terminal
	opts.OnTitle = func(title string) {
		a.post(func() {
			if p.term == term {
				p.title = title
			}
		})
	}
	opts.OnExit = func(int) {
		proc.Close()
		a.post(func() {
			if p.term != term {
				return
			}
			if err := proc.Err(); err != nil {
				if s.split() {
					a.paneExited(s, p)
					a.notify(adminError(err))
				} else {
					s.status, s.err = statusFailed, &localStartError{program: locales.LocalAdministrator(), err: err}
				}
				return
			}
			a.paneExited(s, p)
		})
	}
	returnTerm, err := terminal.New(opts)
	term = returnTerm
	if err != nil {
		proc.Close()
	}
	return term, err
}

// Reconnection prepares every pane before replacing any existing terminal.
// Canceling a later UAC request leaves the previous layout and displays intact.
func (a *App) reconnectAdminLocal(s *Session) {
	panes := slices.Clone(s.panes.panes())
	if len(panes) == 0 {
		panes = []*pane{newPane()}
	}
	a.launchAdmin(s, len(panes), func(procs []localterm.Process, err error) {
		if err != nil {
			a.notify(adminError(err))
			return
		}
		if !slices.Contains(a.sessions, s) || !s.localElevated {
			for _, proc := range procs {
				proc.Close()
			}
			return
		}
		current := s.panes.panes()
		if len(current) != 0 && !slices.Equal(current, panes) {
			for _, proc := range procs {
				proc.Close()
			}
			return
		}
		terms := make([]*terminal.Terminal, len(panes))
		for i, p := range panes {
			terms[i], err = a.adminTerminal(s, p, procs[i])
			if err != nil {
				for _, t := range terms {
					if t != nil {
						t.Close()
					}
				}
				for _, proc := range procs {
					proc.Close()
				}
				a.notify(adminError(err))
				return
			}
		}
		s.disconnect()
		if s.panes.root == nil {
			s.panes.reset(panes[0])
		}
		for i, p := range panes {
			p.term, p.title = terms[i], ""
			s.panes.opened(p)
		}
		s.status, s.err = statusConnected, nil
		s.panes.refocus = true
	})
}

func (a *App) splitAdminLocal(s *Session, at, p *pane, dir splitDir) {
	previous := at.term
	a.launchAdmin(s, 1, func(procs []localterm.Process, err error) {
		if err != nil {
			a.notify(adminError(err))
			return
		}
		if !slices.Contains(a.sessions, s) || !canSplit(s) || !s.panes.has(at) || at.term != previous {
			procs[0].Close()
			return
		}
		term, err := a.adminTerminal(s, p, procs[0])
		a.paneOpened(s, at, p, dir, nil, shellSpec{}, term, err)
	})
}

func adminBadge(c *ui.Context, active bool) {
	fg, bg := theme.Warning, theme.Warning.Alpha(0.12)
	if active {
		fg, bg = theme.AccentText, theme.AccentText.Alpha(0.22)
	}
	ui.Row(c).Gap(3).AlignItems(ui.Center).Padding(1, 5).Radius(5).Background(bg).Shrink(0).Label(locales.LocalAdministrator()).Children(func() {
		ui.Icon(c, icons.Get("shield")).FontSize(12).TextColor(fg)
		ui.Text(c, locales.LocalAdministrator()).FontSize(10.5).TextColor(fg).SingleLine()
	})
}

func (a *App) localAdminHeader(c *ui.Context, s *Session) {
	if !s.localElevated {
		return
	}
	ui.Row(c.Key("local-admin-header")).Gap(8).AlignItems(ui.Center).Padding(6, 10).Background(theme.Surface).Children(func() {
		ui.Icon(c, icons.Get("square-terminal")).FontSize(14).TextColor(theme.TextMuted)
		ui.Text(c, s.label()).SingleLine().MinWidth(0).Shrink(1).TextColor(theme.Text)
		adminBadge(c, false)
		ui.Spacer(c)
		widgets.Muted(c, s.statusText()).FontSize(11)
	})
}
