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
		ui.Column(c).FillWidth().Padding(10, 10, 8, 10).Gap(6).Children(func() {
			if s == nil {
				widgets.Muted(c, locales.NotConnected()).Padding(4, 0)
				return
			}
			st := s.stats
			ui.Row(c).Gap(6).Children(func() {
				ip := s.ip
				if ip == "" {
					ip = s.Host.Addr
				}
				ui.Text(c, "IP "+ip).SingleLine().Grow(1).MinWidth(0)
				cp := ui.Text(c, locales.Copy()).TextColor(theme.Accent).Cursor(ui.CursorPointer)
				if cp.Clicked() {
					c.WriteClipboard(ip)
				}
			})
			if !st.OK || s.status != statusConnected {
				if s.status == statusConnected {
					widgets.Muted(c, locales.ReadingServerInfo())
				} else {
					widgets.Muted(c, s.statusText())
				}
				return
			}
			ui.Text(c, locales.UptimeDays(int(st.Uptime.Hours()/24)))
			ui.Text(c, locales.LoadAverage(fmt.Sprintf("%.2f, %.2f, %.2f", st.Load[0], st.Load[1], st.Load[2])))
			widgets.UsageBar(c, locales.Cpu(), st.CPU/100, fmt.Sprintf("%.0f%%", st.CPU), ui.Hex("#86efac"))
			widgets.UsageBar(c, locales.Memory(), ratio(st.MemUsed, st.MemTotal), usage(st.MemUsed, st.MemTotal), ui.Hex("#fdba74"))
			widgets.UsageBar(c, locales.Swap(), ratio(st.SwapUsed, st.SwapTotal), usage(st.SwapUsed, st.SwapTotal), ui.Hex("#93c5fd"))

			procTable(c, st.Procs)

			ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
				ui.Text(c, "↑"+formatBytes(st.TxRate)).TextColor(ui.Hex("#ea580c"))
				ui.Text(c, "↓"+formatBytes(st.RxRate)).TextColor(ui.Hex("#16a34a"))
				ui.Spacer(c)
				widgets.Muted(c, st.Iface)
			})
			widgets.Sparkline(c, monitor.HistoryLen, [][]float64{st.TxHist, st.RxHist}, []ui.Color{ui.Hex("#fb923c"), ui.Hex("#4ade80")}, formatBytes)

			ui.Row(c).Gap(8).Margin(4, 0, 0, 0).Children(func() {
				ui.Text(c, formatLatency(st.Latency)).TextColor(theme.Accent)
				ui.Spacer(c)
				widgets.Muted(c, locales.Latency())
			})
			widgets.Sparkline(c, monitor.HistoryLen, [][]float64{st.LatHist}, []ui.Color{ui.Hex("#60a5fa")}, func(v float64) string { return fmt.Sprintf("%.0f", v) })

			diskTable(c, st.Disks)
		})
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
			if widgets.IconButton(c, "settings", locales.Settings()).Clicked() {
				a.settingsOpen = true
			}
			if widgets.IconButton(c, "plus", locales.NewConnection()).Clicked() {
				a.openManager()
			}
		})
		a.sessList.Selected = &a.active
		a.sessList.Key = func(i int) any { return a.sessions[i].ID }
		a.sessList.Label = func(i int) string { return a.sessions[i].label() }
		a.sessList.Reorder = a.moveSessions
		ui.List(c, &a.sessList, len(a.sessions), func(i int) {
			a.sessionRow(c, a.sessions[i], i == a.active)
		}).Grow(1).Padding(0, 4, 4, 4).Children(func() {
			if len(a.sessions) == 0 {
				widgets.Muted(c, locales.NoSessions()).Padding(8, 10)
			}
		})
	})
}

func (a *App) sessionRow(c *ui.Context, s *Session, active bool) {
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
			a.closeSession(s)
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
			a.closeSession(s)
		}
	})
}
