package app

import (
	"finalshelly/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

var (
	termBG   = ui.Hex("#1d1f21") // Tomorrow Night's background
	termText = ui.Hex("#c5c8c6")
)

// terminalArea shows the active session's terminal, or what keeps it
// from showing.
func (a *App) terminalArea(c *ui.Context) {
	s := a.current()
	if s == nil || s.isNewTab() {
		a.startPage(c, s)
		return
	}
	area := ui.Column(c.Key(s.ID)).Fill().Background(termBG)
	a.reconnectShortcut(area, s)
	area.Children(func() {
		if s.status != statusConnected {
			a.statusBanner(c, s)
		}
		if s.term != nil {
			ui.Box(c).Grow(1).Padding(4, 0, 0, 6).Children(func() {
				view := terminal.View(c, s.term).Fill().AutoFocus()
				a.findBar(c, s, view)
			})
			return
		}
		connecting := s.status == statusConnecting
		msg := ui.Column(c.Key(connecting)).Grow(1).Center().Gap(12)
		if !connecting {
			// For the key that connects again.
			msg.Focusable().FocusRing(false).AutoFocus()
		}
		msg.Children(func() {
			if s.status == statusConnecting {
				ui.Spinner(c).TextColor(termText).FontSize(22)
				ui.Text(c, locales.ConnectingTo(s.Host.Target())).TextColor(termText)
			} else {
				ui.Text(c, s.statusText()).TextColor(termText).FontSize(15)
			}
		})
	})
}

// statusBanner is the bar over the terminal of a session that is not
// connected, with what went wrong and a button to connect again.
func (a *App) statusBanner(c *ui.Context, s *Session) {
	if s.status == statusConnecting && s.term == nil {
		return
	}
	bg := ui.Hex("#7f1d1d")
	if s.status == statusConnecting {
		bg = ui.Hex("#713f12")
	}
	ui.Row(c).Background(bg).Padding(6, 10).Gap(10).Children(func() {
		msg := s.statusText()
		if s.err != nil {
			msg = describeError(s.err)
		}
		ui.Text(c, msg).TextColor(ui.Hex("#fef2f2")).Grow(1).MinWidth(0)
		if s.status != statusConnecting && ui.Button(c, locales.Reconnect()).Clicked() {
			a.reconnect(s)
		}
	})
}
