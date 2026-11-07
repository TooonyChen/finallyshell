package app

import (
	"slices"
	"strconv"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/icons"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// hostForm edits a host in the connection manager.
type hostForm struct {
	editing    *config.Host // nil for a new host
	name       string
	addr       string
	port       string
	user       string
	auth       config.AuthKind
	password   string
	keyPath    string
	passphrase string
	// err says what is wrong, in the language shown when it is drawn.
	err func() string
}

func (a *App) openManager() {
	a.managerOpen = true
	a.formOpen = len(a.hosts.Live()) == 0
	if a.formOpen {
		a.startForm(nil)
	}
}

func (a *App) startForm(h *config.Host) {
	a.form = hostForm{editing: h, port: "22", user: "root", auth: config.AuthPassword}
	if h != nil {
		a.form.name, a.form.addr, a.form.user, a.form.keyPath = h.Name, h.Addr, h.User, h.KeyPath
		a.form.port = strconv.Itoa(h.PortOr22())
		a.form.auth = h.Auth
		if h.Auth == config.AuthKey {
			a.form.passphrase = a.passwords[h.ID]
		} else {
			a.form.password = a.passwords[h.ID]
		}
	}
	a.formOpen = true
}

func (a *App) dialogs(c *ui.Context) {
	ui.Modal(c, &a.managerOpen, func() {
		ui.Column(c).Width(540).Gap(12).Children(func() {
			if a.formOpen {
				a.hostFormView(c)
			} else {
				a.hostListView(c)
			}
		})
	})
	ui.Modal(c, &a.pwOpen, func() { a.passwordView(c) })
	ui.Modal(c, &a.settingsOpen, func() { a.settingsView(c) })
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
			a.startForm(nil)
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
			a.startForm(hosts[a.hostRow])
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

func (a *App) hostFormView(c *ui.Context) {
	f := &a.form
	title := locales.NewConnection()
	if f.editing != nil {
		title = locales.EditConnection()
	}
	ui.Text(c, title).FontSize(16).Bold()
	widgets.Field(c, locales.FieldName(), func() {
		ui.TextInput(c, &f.name).Placeholder(locales.FieldNamePlaceholder()).Grow(1)
	})
	widgets.Field(c, locales.FieldHost(), func() {
		ui.TextInput(c, &f.addr).Placeholder(locales.FieldHostPlaceholder()).Grow(1).AutoFocus()
		ui.Text(c, locales.FieldPort())
		ui.TextInput(c, &f.port).Width(64)
	})
	widgets.Field(c, locales.FieldUser(), func() { ui.TextInput(c, &f.user).Grow(1) })
	widgets.Field(c, locales.FieldAuth(), func() {
		byPassword, byKey := locales.AuthPassword(), locales.AuthKey()
		chosen := byPassword
		if f.auth == config.AuthKey {
			chosen = byKey
		}
		if ui.Select(c, &chosen, []string{byPassword, byKey}).Width(140).Changed() {
			f.auth = config.AuthPassword
			if chosen == byKey {
				f.auth = config.AuthKey
			}
		}
	})
	if f.auth == config.AuthKey {
		widgets.Field(c, locales.FieldKeyFile(), func() {
			ui.TextInput(c, &f.keyPath).Placeholder("~/.ssh/id_ed25519").Grow(1)
		})
		widgets.Field(c, locales.FieldPassphrase(), func() {
			ui.TextInput(c, &f.passphrase).Password().Placeholder(locales.PassphrasePlaceholder()).Grow(1)
		})
	} else {
		widgets.Field(c, locales.FieldPassword(), func() { ui.TextInput(c, &f.password).Password().Grow(1) })
	}
	widgets.Muted(c, locales.PasswordNote()).FontSize(12)
	if f.err != nil {
		ui.Text(c, f.err()).TextColor(theme.Danger)
	}
	ui.Row(c).Gap(8).Children(func() {
		if len(a.hosts.Live()) > 0 && ui.Button(c, locales.Back()).Clicked() {
			a.formOpen = false
		}
		ui.Spacer(c)
		if ui.Button(c, locales.Save()).Clicked() {
			a.saveForm()
		}
		if ui.PrimaryButton(c, locales.SaveAndConnect()).Clicked() {
			if h := a.saveForm(); h != nil {
				a.managerOpen = false
				a.openSession(h)
			}
		}
	})
}

// saveForm checks the form and saves its host, returning it, or nil with
// the form's error set.
func (a *App) saveForm() *config.Host {
	f := &a.form
	f.addr, f.user = strings.TrimSpace(f.addr), strings.TrimSpace(f.user)
	port, err := strconv.Atoi(strings.TrimSpace(f.port))
	switch {
	case f.addr == "":
		f.err = locales.ErrHostRequired
		return nil
	case f.user == "":
		f.err = locales.ErrUserRequired
		return nil
	case err != nil || port <= 0 || port > 65535:
		f.err = locales.ErrPortInvalid
		return nil
	case f.auth == config.AuthKey && strings.TrimSpace(f.keyPath) == "":
		f.err = locales.ErrKeyFileRequired
		return nil
	}
	h := f.editing
	if h == nil {
		h = &config.Host{ID: config.NewHostID()}
		a.hosts.Add(h)
	}
	h.Name, h.Addr, h.Port, h.User, h.Auth = strings.TrimSpace(f.name), f.addr, port, f.user, f.auth
	if f.auth == config.AuthKey {
		h.KeyPath = strings.TrimSpace(f.keyPath)
		a.passwords[h.ID] = f.passphrase
	} else {
		h.KeyPath = ""
		a.passwords[h.ID] = f.password
	}
	if err := a.saveHosts(a.hosts); err != nil {
		f.err = func() string { return locales.ErrSaveFailed(err.Error()) }
	} else {
		f.err = nil
	}
	a.formOpen = false
	a.hostRow = slices.Index(a.hosts.Live(), h)
	return h
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
			a.openSession(h)
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

// settingsView is the settings window: for now, the language.
func (a *App) settingsView(c *ui.Context) {
	ui.Column(c).Width(380).Gap(14).Children(func() {
		ui.Text(c, locales.Settings()).FontSize(16).Bold()
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, locales.SettingsLanguage()).Bold()
			chosen := a.settings.Language
			ui.RadioGroup(c, func() {
				ui.Radio(c, &chosen, "", locales.FollowSystem())
				for _, tag := range a.plat.Languages() {
					ui.Radio(c, &chosen, tag, i18n.Autonym(tag))
				}
			}).Label(locales.SettingsLanguage())
			if chosen != a.settings.Language {
				a.setLanguage(chosen)
			}
		})
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.PrimaryButton(c, locales.Done()).Clicked() {
				a.settingsOpen = false
			}
		})
	})
}
