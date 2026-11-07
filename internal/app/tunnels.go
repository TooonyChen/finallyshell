package app

import (
	"errors"
	"strings"

	"finallyshell/internal/config"
	"finallyshell/internal/sshx"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// tunnelState is a session's tunnels, as the "Tunnels" tab shows them:
// those of its host when it last connected.
type tunnelState struct {
	runs  []*tunnelRun
	table ui.ListState
}

// tunnelRun is one of a session's tunnels. The tunnel itself counts its
// connections and bytes, which the view reads as it draws.
type tunnelRun struct {
	spec     config.Tunnel
	fwd      *sshx.Tunnel // while it listens
	starting bool
	err      error // why it did not start, or stopped by itself
	// gen counts the starts and stops, so that a start that ends after
	// a stop, or another start, is undone.
	gen int
}

// startTunnels starts the enabled tunnels of tunnels on s's connection,
// which has just opened; the others are listed, stopped.
func (a *App) startTunnels(s *Session, tunnels []config.Tunnel) {
	s.stopTunnels()
	s.tunnels.runs = nil
	for _, t := range tunnels {
		run := &tunnelRun{spec: t}
		s.tunnels.runs = append(s.tunnels.runs, run)
		if t.Enabled() {
			a.startTunnel(s, run)
		}
	}
}

// startTunnel starts run on s's connection, on a goroutine: asking the
// server to listen takes a round trip.
func (a *App) startTunnel(s *Session, run *tunnelRun) {
	client := s.client
	if client == nil || run.fwd != nil {
		return
	}
	run.gen++
	gen, spec := run.gen, run.spec
	run.starting, run.err = true, nil
	// The tunnel tells of changes to its stats, and of stopping, from its
	// goroutines: posted, the windows draw them.
	notify := func() { a.post(run.refresh) }
	go func() {
		fwd, err := sshx.StartTunnel(client, spec, notify)
		a.post(func() {
			if run.gen != gen || s.client != client { // stopped meanwhile
				if fwd != nil {
					fwd.Stop()
				}
				return
			}
			run.starting, run.fwd, run.err = false, fwd, err
		})
	}()
}

// stop stops run, keeping it in the list.
func (run *tunnelRun) stop() {
	run.gen++
	run.starting, run.err = false, nil
	if run.fwd != nil {
		run.fwd.Stop()
		run.fwd = nil
	}
}

// refresh notes that run's tunnel stopped by itself, as when the server
// is gone.
func (run *tunnelRun) refresh() {
	if run.fwd == nil {
		return
	}
	select {
	case <-run.fwd.Done():
		run.err = run.fwd.Err()
		run.fwd = nil
	default:
	}
}

// stopTunnels stops s's tunnels, as it disconnects.
func (s *Session) stopTunnels() {
	for _, run := range s.tunnels.runs {
		run.stop()
	}
}

// listening is how many of s's tunnels listen.
func (s *Session) listening() int {
	n := 0
	for _, run := range s.tunnels.runs {
		if run.fwd != nil {
			n++
		}
	}
	return n
}

// describeTunnelError says why a tunnel is not listening, in the
// language shown.
func describeTunnelError(err error) string {
	var te *sshx.TunnelError
	switch {
	case errors.Is(err, sshx.ErrUnknownTunnel):
		return locales.ErrTunnelUnknown()
	case errors.Is(err, sshx.ErrConnectionClosed):
		return locales.ErrTunnelLost()
	case errors.As(err, &te):
		switch {
		case te.InUse:
			return locales.ErrTunnelInUse(te.Addr)
		case errors.Is(te.Err, sshx.ErrRemoteListenHost):
			return locales.ErrTunnelRemoteHostName(te.Addr)
		case te.Remote && strings.Contains(te.Err.Error(), "denied"):
			return locales.ErrTunnelRemoteDenied(te.Addr)
		}
		return locales.ErrTunnelListen(te.Addr, te.Err.Error())
	}
	return locales.ErrTunnelStopped(err.Error())
}

func tunnelStatusColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "type", Title: locales.ColTunnelType(), Width: 100},
		{ID: "listen", Title: locales.ColTunnelListen(), Width: 150},
		{ID: "target", Title: locales.ColTunnelTarget(), Width: 150},
		{ID: "status", Title: locales.ColTunnelStatus(), MinWidth: 180},
		{ID: "connections", Title: locales.ColTunnelConnections(), Width: 120},
		{ID: "traffic", Title: locales.ColTunnelTraffic(), Width: 150},
		{ID: "action", Title: "", Width: 80, Align: ui.Center, Fixed: true},
	}
}

// tunnelPanel is the "Tunnels" tab: the active session's tunnels, their
// state and what they carried, with a button to start or stop each.
func (a *App) tunnelPanel(c *ui.Context) {
	s := a.current()
	if s == nil || s.isNewTab() {
		return
	}
	ts := &s.tunnels
	connected := s.status == statusConnected && s.client != nil
	ui.Column(c.Key(s.ID)).Grow(1).Children(func() {
		ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#e5e7eb")).Children(func() {
			switch {
			case !connected:
				widgets.Muted(c, locales.TunnelsOffline()).Grow(1)
			case len(ts.runs) == 0:
				widgets.Muted(c, locales.TunnelsNone()).Grow(1)
			default:
				ui.Spacer(c)
			}
			if ui.Button(c, locales.TunnelsConfigure()).Clicked() {
				a.editTunnels(s.Host)
			}
		})
		runs := ts.runs
		ts.table.Key = func(i int) any { return runs[i] }
		ts.table.Label = func(i int) string { return tunnelKindLabel(runs[i].spec.Kind) + " " + runs[i].spec.ListenAddr() }
		ui.Table(c, &ts.table, tunnelStatusColumns(), len(runs), func(row, col int) {
			a.tunnelCell(c, s, runs[row], col, connected)
		}).Grow(1)
	})
}

func (a *App) tunnelCell(c *ui.Context, s *Session, run *tunnelRun, col int, connected bool) {
	t := &run.spec
	switch col {
	case 0:
		ui.Text(c, tunnelKindLabel(t.Kind)).SingleLine()
	case 1:
		addr := t.ListenAddr()
		if run.fwd != nil {
			addr = run.fwd.Addr().String()
		}
		ui.Text(c, addr).SingleLine()
	case 2:
		ui.Text(c, tunnelTarget(t)).SingleLine()
	case 3:
		ui.Column(c).Gap(2).MinWidth(0).Children(func() {
			color, text := theme.TextMuted, locales.TunnelStopped()
			switch {
			case run.starting:
				color, text = theme.Warning, locales.TunnelStarting()
			case run.fwd != nil:
				color, text = theme.Success, locales.TunnelListening()
			case run.err != nil:
				color, text = theme.Danger, describeTunnelError(run.err)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				widgets.StatusDot(c, color)
				st := ui.Text(c, text).MaxLines(2).Shrink(1).MinWidth(0).Tooltip(text)
				if run.err != nil && !run.starting {
					st.TextColor(theme.Danger)
				}
			})
			if run.fwd != nil {
				if err := run.fwd.DialErr(); err != nil {
					msg := locales.TunnelDialFailed(err.Error())
					widgets.Muted(c, msg).FontSize(11.5).SingleLine().Tooltip(msg)
				}
			}
		})
	case 4:
		if run.fwd != nil {
			st := run.fwd.Stats()
			ui.Text(c, locales.TunnelConnectionsCount(int(st.Active), int(st.Total))).SingleLine()
		}
	case 5:
		if run.fwd != nil {
			st := run.fwd.Stats()
			ui.Text(c, locales.TunnelTraffic(formatSize(st.Up), formatSize(st.Down))).SingleLine()
		}
	case 6:
		if run.fwd != nil || run.starting {
			if ui.Button(c, locales.TunnelStop()).Clicked() {
				run.stop()
			}
		} else if ui.Button(c, locales.TunnelStart()).Disabled(!connected).Clicked() && connected {
			a.startTunnel(s, run)
		}
	}
}

// editTunnels opens the connection editor on h's Tunnels page, or
// brings the open editor to the front as it is.
func (a *App) editTunnels(h *config.Host) {
	opening := !a.windowOpen(WindowEditor)
	a.editConnection(h)
	if opening && a.editor != nil {
		a.editor.nav.Page = editorTunnels
	}
}
