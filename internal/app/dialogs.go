package app

import (
	"finalshelly/internal/icons"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// openManager shows the connection manager, and the connection editor
// for a new host when there is no host to list.
func (a *App) openManager() {
	a.managerOpen = true
	if len(a.hosts.Live()) == 0 {
		a.newConnection()
	}
}

func (a *App) dialogs(c *ui.Context) {
	ui.Modal(c, &a.managerOpen, func() {
		ui.Column(c).Width(540).Gap(12).Children(func() { a.hostListView(c) })
	})
	ui.Modal(c, &a.pwOpen, func() { a.passwordView(c) })
	if s := a.pendingHostKey(); s != nil {
		open := true
		ui.Modal(c, &open, func() { a.hostKeyView(c, s) })
		if !open && s.hostKeyQ != nil { // Escape or a click outside
			s.hostKeyQ.Answer <- false
			s.hostKeyQ = nil
		}
	}
	if q := a.replace; q != nil {
		open := true
		ui.Modal(c, &open, func() { a.replaceView(c, q) })
		if !open {
			a.replace = nil
		}
	}
}

func (a *App) hostListView(c *ui.Context) {
	ui.Row(c).Children(func() {
		ui.Text(c, locales.ManagerTitle()).FontSize(16).Bold().Grow(1)
		if ui.Button(c, locales.NewHost()).Clicked() {
			a.newConnection()
		}
	})
	hosts := a.hosts.Live()
	a.hostList.Selected = &a.hostRow
	a.hostList.Key = func(i int) any { return hosts[i].ID }
	a.hostList.Label = func(i int) string { return hosts[i].Title() }
	list := ui.List(c, &a.hostList, len(hosts), func(i int) {
		h := hosts[i]
		sub := theme.TextMuted
		if i == a.hostRow {
			sub = theme.AccentText.Alpha(0.8)
		}
		ui.Row(c).Gap(10).Padding(6, 8).Children(func() {
			ui.Icon(c, icons.Get("server")).FontSize(16).TextColor(sub)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, h.Title()).SingleLine()
				ui.Text(c, h.Target()).FontSize(11.5).TextColor(sub).SingleLine()
			})
		})
	}).Height(300).Border(1, theme.Border).Radius(6)
	if list.Submitted() && a.hostRow >= 0 && a.hostRow < len(hosts) {
		a.managerOpen = false
		a.openSession(hosts[a.hostRow])
	}
	chosen := a.hostRow >= 0 && a.hostRow < len(hosts)
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, locales.Edit()).Disabled(!chosen).Clicked() {
			a.editConnection(hosts[a.hostRow])
		}
		if ui.Button(c, locales.Delete()).Disabled(!chosen).Clicked() {
			a.hosts.Remove(hosts[a.hostRow].ID) // for good, until there is a list of deleted hosts
			a.hostRow = -1
			a.saveHosts(a.hosts)
		}
		ui.Spacer(c)
		if ui.Button(c, locales.Cancel()).Clicked() {
			a.managerOpen = false
		}
		if ui.PrimaryButton(c, locales.Connect()).Disabled(!chosen).Clicked() {
			a.managerOpen = false
			a.openSession(hosts[a.hostRow])
		}
	})
}

func (a *App) passwordView(c *ui.Context) {
	h := a.pwHost
	if h == nil {
		return
	}
	ui.Column(c).Width(380).Gap(12).Children(func() {
		ui.Text(c, locales.EnterPassword()).FontSize(16).Bold()
		widgets.Muted(c, h.Target())
		submit := ui.TextInput(c, &a.pwText).Password().AutoFocus().Submitted()
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.pwOpen = false
			}
			if ui.PrimaryButton(c, locales.Connect()).Clicked() {
				submit = true
			}
		})
		if submit {
			a.passwords[h.ID] = a.pwText
			a.pwOpen, a.pwText = false, ""
			a.openSessionIn(a.pwTab, h)
		}
	})
}

func (a *App) pendingHostKey() *Session {
	for _, s := range a.sessions {
		if s.hostKeyQ != nil {
			return s
		}
	}
	return nil
}

func (a *App) hostKeyView(c *ui.Context, s *Session) {
	q := s.hostKeyQ
	ui.Column(c).Width(460).Gap(10).Children(func() {
		ui.Text(c, locales.HostKeyTitle()).FontSize(16).Bold()
		ui.Text(c, locales.HostKeyBody(q.Host))
		ui.Column(c).Gap(4).Padding(8).Radius(6).Background(ui.Hex("#f3f4f6")).Children(func() {
			widgets.Muted(c, q.KeyType)
			ui.Text(c, q.Fingerprint).Font("monospace").Selectable()
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.Reject()).Clicked() {
				q.Answer <- false
				s.hostKeyQ = nil
			}
			if ui.PrimaryButton(c, locales.TrustAndConnect()).Clicked() {
				q.Answer <- true
				s.hostKeyQ = nil
			}
		})
	})
}

func (a *App) replaceView(c *ui.Context, q *replaceQuestion) {
	ui.Column(c).Width(420).Gap(12).Children(func() {
		ui.Text(c, locales.ReplaceTitle()).FontSize(16).Bold()
		ui.Text(c, locales.ReplaceBody(q.count, q.dir))
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.replace = nil
			}
			if ui.PrimaryButton(c, locales.Replace()).Clicked() {
				a.replace = nil
				q.yes()
			}
		})
	})
}
