package app

import (
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func (a *App) dialogs(c *ui.Context) {
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

func (a *App) passwordView(c *ui.Context) {
	need := a.pwNeed
	if need == nil {
		return
	}
	ui.Column(c).Width(380).Gap(12).Children(func() {
		ui.Text(c, need.title()).FontSize(16).Bold()
		ui.Column(c).Gap(2).Children(func() {
			widgets.Muted(c, need.who)
			if need.detail != "" {
				widgets.Muted(c, need.detail).FontSize(12)
			}
			if need.via != "" {
				widgets.Muted(c, locales.PasswordOnTheWay(need.via)).FontSize(12)
			}
		})
		if a.pwErr != nil {
			ui.Text(c, describeError(a.pwErr)).TextColor(theme.Danger)
		}
		submit := ui.TextInput(c, &a.pwText).Password().AutoFocus().Submitted()
		ui.Column(c).Gap(4).Children(func() {
			a.rememberBox(c, &a.pwRemember, need.passphrase)
			widgets.Muted(c, a.rememberNote(a.pwRemember)).FontSize(12)
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.pwOpen = false
			}
			if ui.PrimaryButton(c, locales.Connect()).Clicked() {
				submit = true
			}
		})
		if submit {
			a.passwordEntered()
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
