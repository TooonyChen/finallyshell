package app

import (
	"fmt"

	"finalshelly/internal/monitor"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// monitorPanel shows the active session's server, as FinalShell's left
// column does.
func (a *App) monitorPanel(c *ui.Context) {
	s := a.current()
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
		serverStats(c, st)
	})
}

// serverStats shows st, or the skeleton for nil.
func serverStats(c *ui.Context, st *monitor.Stats) {
	if st == nil {
		ui.Text(c, locales.UptimeUnknown())
		ui.Text(c, locales.LoadAverage("-"))
		widgets.UsageBar(c, locales.Cpu(), 0, "0%", cpuColor)
		widgets.UsageBar(c, locales.Memory(), 0, "0%", memColor)
		widgets.UsageBar(c, locales.Swap(), 0, "0%", swapColor)
		procTable(c, nil)
		netRow(c, 0, 0, "")
		widgets.Sparkline(c, monitor.HistoryLen, nil, nil, formatBytes)
		latencyRow(c, "-")
		widgets.Sparkline(c, monitor.HistoryLen, nil, nil, formatMillis)
		diskTable(c, nil)
		return
	}
	ui.Text(c, locales.UptimeDays(int(st.Uptime.Hours()/24)))
	ui.Text(c, locales.LoadAverage(fmt.Sprintf("%.2f, %.2f, %.2f", st.Load[0], st.Load[1], st.Load[2])))
	widgets.UsageBar(c, locales.Cpu(), st.CPU/100, fmt.Sprintf("%.0f%%", st.CPU), cpuColor)
	widgets.UsageBar(c, locales.Memory(), ratio(st.MemUsed, st.MemTotal), usage(st.MemUsed, st.MemTotal), memColor)
	widgets.UsageBar(c, locales.Swap(), ratio(st.SwapUsed, st.SwapTotal), usage(st.SwapUsed, st.SwapTotal), swapColor)

	procTable(c, st.Procs)

	netRow(c, st.TxRate, st.RxRate, st.Iface)
	widgets.Sparkline(c, monitor.HistoryLen, [][]float64{st.TxHist, st.RxHist}, []ui.Color{ui.Hex("#fb923c"), ui.Hex("#4ade80")}, formatBytes)

	latencyRow(c, formatLatency(st.Latency))
	widgets.Sparkline(c, monitor.HistoryLen, [][]float64{st.LatHist}, []ui.Color{ui.Hex("#60a5fa")}, formatMillis)

	diskTable(c, st.Disks)
}

func formatMillis(v float64) string { return fmt.Sprintf("%.0f", v) }

var (
	cpuColor  = ui.Hex("#86efac")
	memColor  = ui.Hex("#fdba74")
	swapColor = ui.Hex("#93c5fd")
)

func netRow(c *ui.Context, tx, rx float64, iface string) {
	ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
		ui.Text(c, "↑"+formatBytes(tx)).TextColor(ui.Hex("#ea580c"))
		ui.Text(c, "↓"+formatBytes(rx)).TextColor(ui.Hex("#16a34a"))
		ui.Spacer(c)
		widgets.Muted(c, iface)
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

func procTable(c *ui.Context, procs []monitor.Proc) {
	white := ui.Hex("#ffffff")
	ui.Column(c).Border(1, ui.Hex("#d1d5db")).Background(ui.Hex("#fafafa")).Children(func() {
		ui.Row(c).Background(ui.Hex("#60a5fa")).PaddingX(6).Height(20).Children(func() {
			ui.Text(c, locales.Memory()).Width(54).TextColor(white).FontSize(12)
			ui.Text(c, locales.Cpu()).Width(40).TextColor(white).FontSize(12)
			ui.Text(c, locales.Command()).TextColor(white).FontSize(12)
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
	ui.Column(c).Margin(6, 0, 0, 0).Border(1, ui.Hex("#d1d5db")).Background(ui.Hex("#ffffff")).Children(func() {
		ui.Row(c).PaddingX(6).Height(20).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#e5e7eb")).Children(func() {
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
	ui.Column(c).Fill().BorderWidth(1, 0, 0, 0).BorderColor(ui.Hex("#d1d5db")).Children(func() {
		ui.Row(c).Padding(6, 6, 6, 10).Gap(2).Children(func() {
			ui.Text(c, locales.SessionsTitle(len(a.sessions))).Bold().Grow(1)
			if widgets.IconButton(c, "settings", locales.Options()).Clicked() {
				a.openOptions()
			}
			if widgets.IconButton(c, "plus", locales.NewConnection()).Clicked() {
				a.openManager()
			}
		})
		// No Key: with one, the list follows the chosen session by it, and
		// chooses none once that session is gone, even though a.active
		// already holds the neighbor closeSession picked, which is on the
		// same row. a.active is the app's: closeSession and moveSessions
		// keep it on the right row.
		a.sessList.Selected = &a.active
		a.sessList.Label = func(i int) string { return a.sessions[i].label() }
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
		if closing != nil {
			a.closeSession(closing)
		}
	})
}

// sessionRow is the tab of s. It reports whether the user closed it.
func (a *App) sessionRow(c *ui.Context, s *Session, active bool) (closed bool) {
	row := ui.Row(c).Gap(8).Padding(5, 4, 5, 8).Radius(5)
	hovered := row.Hovered()
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.Reconnect()).Chosen() {
			a.reconnect(s)
		}
		if m.Item(locales.DuplicateSession()).Chosen() {
			a.openSession(s.Host)
		}
		if m.Item(locales.Disconnect()).Disabled(s.status != statusConnected && s.status != statusConnecting).Chosen() {
			a.transfers.cancelSession(s)
			s.disconnect()
			s.status, s.err = statusClosed, nil
		}
		m.Separator()
		if m.Item(locales.Close()).Danger().Chosen() {
			closed = true
		}
	})
	if row.DoubleClicked() && s.status != statusConnected && s.status != statusConnecting {
		a.reconnect(s)
	}
	row.Children(func() {
		widgets.StatusDot(c, s.statusColor(theme))
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			name := ui.Text(c, s.label()).SingleLine()
			if active {
				name.Bold()
			}
			sub := s.Host.Target()
			if s.status != statusConnected {
				sub = s.statusText()
			}
			subColor := theme.TextMuted
			if active {
				subColor = theme.AccentText.Alpha(0.8)
			}
			ui.Text(c, sub).FontSize(11.5).SingleLine().TextColor(subColor)
		})
		closeBtn := widgets.IconButton(c, "x", locales.CloseSession()).Size(20, 20)
		if !hovered && !active {
			closeBtn.Invisible()
		}
		if closeBtn.Clicked() {
			closed = true
		}
	})
	return closed
}
