package app

import (
	"fmt"

	"finallyshell/internal/icons"
	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// monitorPanel shows the active session's server, as FinalShell's left
// column does: the skeleton for a new tab.
func (a *App) monitorPanel(c *ui.Context) {
	s := a.connSession()
	if s != nil && s.isNewTab() {
		s = nil
	}
	ui.Scroll(c).Fill().Children(func() {
		a.serverView(c, s)
	})
}

// serverView is a server as the side panel shows it: its IP, the System
// info button, and its stats. Without a session, or before the first
// stats, it is the skeleton FinalShell shows: dashes and empty bars.
func (a *App) serverView(c *ui.Context, s *Session) {
	ui.Column(c).FillWidth().Padding(10, 10, 8, 10).Gap(6).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			ip := "-"
			if s != nil {
				ip = s.ip
				if ip == "" {
					ip = s.Host.Addr
				}
			}
			ui.Text(c, "IP "+ip).SingleLine().Grow(1).MinWidth(0)
			if s != nil {
				cp := ui.Text(c, locales.Copy()).TextColor(theme.Accent).Cursor(ui.CursorPointer)
				if cp.Clicked() {
					c.WriteClipboard(ip)
				}
			}
		})
		a.sysInfoButton(c, s)
		var st *monitor.Stats
		switch {
		case s == nil:
		case s.status != statusConnected:
			widgets.Muted(c, s.statusText())
		case !execAllowed(s.Host):
			widgets.Muted(c, locales.MonitorOff())
		case !s.stats.OK:
			widgets.Muted(c, locales.ReadingServerInfo())
		default:
			st = &s.stats
		}
		serverStats(c, st, a.netPicker(s), a.openProcesses)
	})
}

// serverStats shows st, or the skeleton for nil; net is the choice of
// network interfaces, and a click on the process list calls openProcs.
func serverStats(c *ui.Context, st *monitor.Stats, net netPicker, openProcs func()) {
	if st == nil {
		ui.Text(c, locales.UptimeUnknown())
		ui.Text(c, locales.LoadAverage("-"))
		widgets.UsageBar(c, locales.Cpu(), 0, "0%", colors.cpu)
		widgets.UsageBar(c, locales.Memory(), 0, "0%", colors.mem)
		widgets.UsageBar(c, locales.Swap(), 0, "0%", colors.swap)
		procTable(c, nil, nil)
		netRow(c, 0, 0, "", "", nil)
		widgets.Sparkline(c, monitor.HistoryLen, nil, nil, formatBytes)
		latencyRow(c, "-")
		widgets.Sparkline(c, monitor.HistoryLen, nil, nil, formatMillis)
		diskTable(c, nil)
		return
	}
	ui.Text(c, locales.UptimeDays(int(st.Uptime.Hours()/24)))
	ui.Text(c, locales.LoadAverage(fmt.Sprintf("%.2f, %.2f, %.2f", st.Load[0], st.Load[1], st.Load[2])))
	widgets.UsageBar(c, locales.Cpu(), st.CPU/100, fmt.Sprintf("%.0f%%", st.CPU), colors.cpu)
	widgets.UsageBar(c, locales.Memory(), ratio(st.MemUsed, st.MemTotal), usage(st.MemUsed, st.MemTotal), colors.mem)
	widgets.UsageBar(c, locales.Swap(), ratio(st.SwapUsed, st.SwapTotal), usage(st.SwapUsed, st.SwapTotal), colors.swap)

	procTable(c, st.Procs, openProcs)

	netGraph(c, st, net)

	latencyRow(c, formatLatency(st.Latency))
	widgets.Sparkline(c, monitor.HistoryLen, [][]float64{st.LatHist}, []ui.Color{latencyChart}, formatMillis)

	diskTable(c, st.Disks)
}

func formatMillis(v float64) string { return fmt.Sprintf("%.0f", v) }

// netRow is the speeds above the network graph and, at the right, label:
// the button of the interfaces' menu when there is a menu.
func netRow(c *ui.Context, tx, rx float64, label, tip string, menu func(m *ui.Menu)) {
	ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
		ui.Text(c, "↑"+formatBytes(tx)).TextColor(colors.tx).Shrink(0)
		ui.Text(c, "↓"+formatBytes(rx)).TextColor(colors.rx).Shrink(0)
		ui.Spacer(c)
		if menu != nil {
			netLabelButton(c, label, tip, menu)
		} else {
			widgets.Muted(c, label)
		}
	})
}

func latencyRow(c *ui.Context, latency string) {
	ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
		ui.Text(c, latency).TextColor(theme.Accent)
		ui.Spacer(c)
		widgets.Muted(c, locales.Latency())
	})
}

func ratio(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func usage(used, total uint64) string {
	return fmt.Sprintf("%.0f%%  %s/%s", 100*ratio(used, total), formatBytes(float64(used)), formatBytes(float64(total)))
}

// procTable is the processes using the most memory; a click opens the
// process manager with open, when it is not nil.
func procTable(c *ui.Context, procs []monitor.Proc, open func()) {
	headText := colors.procHeadText
	table := ui.Column(c).Border(1, colors.frame).Background(colors.procTable)
	if open != nil {
		table.Cursor(ui.CursorPointer).Tooltip(locales.OpenProcesses()).Role(ui.RoleButton).Label(locales.OpenProcesses())
		if table.Clicked() {
			open()
		}
	}
	table.Children(func() {
		ui.Row(c).Background(colors.procHead).PaddingX(6).Height(20).Children(func() {
			ui.Text(c, locales.Memory()).Width(54).TextColor(headText).FontSize(12)
			ui.Text(c, locales.Cpu()).Width(40).TextColor(headText).FontSize(12)
			ui.Text(c, locales.Command()).TextColor(headText).FontSize(12)
		})
		for i, p := range procs {
			ui.Row(c.Key(i)).PaddingX(6).Height(19).Children(func() {
				ui.Text(c, formatBytes(float64(p.RSS))).Width(54).FontSize(12)
				ui.Text(c, fmt.Sprintf("%.1f", p.CPU)).Width(40).FontSize(12)
				ui.Text(c, p.Name).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			})
		}
	})
}

func diskTable(c *ui.Context, disks []monitor.Disk) {
	ui.Column(c).Margin(6, 0, 0, 0).Border(1, colors.frame).Background(colors.diskTable).Children(func() {
		ui.Row(c).PaddingX(6).Height(20).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
			ui.Text(c, locales.DiskPath()).Grow(1).FontSize(12)
			ui.Text(c, locales.DiskFreeSize()).FontSize(12)
		})
		for i, d := range disks {
			ui.Row(c.Key(i)).PaddingX(6).Height(19).Gap(6).Children(func() {
				ui.Text(c, d.Mount).FontSize(12).SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, formatBytes(float64(d.Avail))+"/"+formatBytes(float64(d.Size))).FontSize(12)
			})
		}
	})
}

// sessionPanel is the list of open sessions: the vertical tabs.
func (a *App) sessionPanel(c *ui.Context) {
	ui.Column(c).Fill().BorderWidth(1, 0, 0, 0).BorderColor(colors.frame).Children(func() {
		ui.Row(c).Padding(6, 6, 6, 10).Gap(2).Children(func() {
			ui.Text(c, locales.SessionsTitle(a.sessionCount())).Bold().Grow(1)
			if widgets.IconButton(c, "settings", locales.Options()).Clicked() {
				a.openOptions()
			}
			if widgets.IconButton(c, "plus", locales.NewTab()).Clicked() {
				a.NewTab()
			}
		})
		// No Key: with one, the list follows the chosen session by it, and
		// chooses none once that session is gone, even though a.active
		// already holds the neighbor closeSession picked, which is on the
		// same row. a.active is the app's: closeSession and moveSessions
		// keep it on the right row.
		a.sessList.Selected = &a.active
		a.sessList.Label = func(i int) string { return listLabel(a.sessions[i]) }
		a.sessList.Reorder = a.moveSessions
		// A row asks to close its session, which closes once the list is
		// built: closing it in the row would leave the rows after it to
		// build with fewer sessions than the list was given.
		var closing *Session
		ui.List(c, &a.sessList, len(a.sessions), func(i int) {
			if a.sessionRow(c, a.sessions[i], i == a.active) {
				closing = a.sessions[i]
			}
		}).Grow(1).Padding(0, 4, 4, 4).Children(func() {
			if len(a.sessions) == 0 {
				widgets.Muted(c, locales.NoSessions()).Padding(8, 10)
			}
		})
		a.renameKey(c)
		if closing != nil {
			a.closeSession(closing)
		}
	})
}

// sessionCount is the number of sessions, new tabs and panel rows left
// out: neither has a Host.
func (a *App) sessionCount() int {
	n := 0
	for _, s := range a.sessions {
		if !s.isNewTab() {
			n++
		}
	}
	return n
}

// sessionRow is the tab of s. It reports whether the user closed it.
func (a *App) sessionRow(c *ui.Context, s *Session, active bool) (closed bool) {
	if s.isPanelTab() {
		return a.panelRow(c, s, active)
	}
	row := ui.Row(c).Gap(8).Padding(5, 4, 5, 8).Radius(5)
	hovered := row.Hovered()
	if s.isNewTab() {
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.Close()).Danger().Chosen() {
				closed = true
			}
		})
		row.Children(func() {
			// As wide as a status dot, so that the names line up.
			ui.Box(c).Size(8, 8).Center().Shrink(0).Children(func() {
				ui.Icon(c, icons.Get("app-window")).FontSize(13).TextColor(rowSubColor(active))
			})
			closed = rowText(c, locales.NewTab(), locales.NotConnected(), active, hovered) || closed
		})
		return closed
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.Reconnect()).Chosen() {
			a.reconnect(s)
		}
		if m.Item(locales.DuplicateSession()).Chosen() {
			a.openSessionIn(nil, s.Host) // in a tab of its own, though a new tab is selected
		}
		if m.Item(locales.Disconnect()).Disabled(s.status != statusConnected && s.status != statusConnecting).Chosen() {
			a.transfers.cancelSession(s)
			s.disconnect()
			s.status, s.err = statusClosed, nil
		}
		m.Separator()
		a.renameMenu(m, s)
		m.Separator()
		if m.Item(locales.Close()).Danger().Chosen() {
			closed = true
		}
	})
	// RowDoubleClicked, not DoubleClicked: that makes this row clickable,
	// and the press goes to the innermost clickable element, so the
	// list's row, which chooses the session, would never see a click.
	renaming := a.renaming(s)
	if row.RowDoubleClicked() && !renaming && s.status != statusConnected && s.status != statusConnecting {
		a.reconnect(s)
	}
	if s.name != "" && !renaming {
		row.Tooltip(renamedTooltip(s))
	}
	row.Children(func() {
		widgets.StatusDot(c, s.statusColor(theme))
		sub := s.Host.Target()
		if s.status != statusConnected {
			sub = s.statusText()
		}
		name := func() { rowName(c, s.label(), active) }
		if renaming {
			name = func() { a.renameField(c, active) }
		}
		closed = rowLines(c, name, sub, active, hovered) || closed
	})
	return closed
}

// rowText is the name and the line under it of a session row, and its
// close button, which shows on the selected row and the row hovered. It
// reports whether the button was clicked.
func rowText(c *ui.Context, name, sub string, active, hovered bool) bool {
	return rowLines(c, func() { rowName(c, name, active) }, sub, active, hovered)
}

// rowLines is rowText with its name line built by name: the field of a
// row being renamed (rename.go).
func rowLines(c *ui.Context, name func(), sub string, active, hovered bool) bool {
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		name()
		ui.Text(c, sub).FontSize(11.5).SingleLine().TextColor(rowSubColor(active))
	})
	closeBtn := widgets.IconButton(c, "x", locales.CloseSession()).Size(20, 20)
	if !hovered && !active {
		closeBtn.Invisible()
	}
	return closeBtn.Clicked()
}

// rowName is the name line of a session row.
func rowName(c *ui.Context, name string, active bool) {
	t := ui.Text(c, name).SingleLine()
	if active {
		t.Bold()
	}
}

func rowSubColor(active bool) ui.Color {
	if active {
		return theme.AccentText.Alpha(0.8)
	}
	return theme.TextMuted
}
