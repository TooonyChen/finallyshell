package app

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/sshx"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// WindowEditor is the connection editor, FinalShell's "New connection"
// window: one at a time, for a new host or an existing one.
const WindowEditor WindowKind = "editor"

// editorSpec is how the connection editor opens, for a new host or, with
// editing, one saved. It belongs to the main window and does not block
// it, as the connection manager is still a dialog in the main window: a
// modal window would be a sheet covering the main window on macOS, and
// would block every window of the app on Linux (see README). Once the
// manager is a window of its own, the editor can be modal to it, with
// Parent set to the manager's kind.
func editorSpec(editing bool) WindowSpec {
	title := locales.NewConnection
	if editing {
		title = locales.EditConnection
	}
	return WindowSpec{
		Kind:  WindowEditor,
		Title: title,
		Width: 680, Height: 600,
		MinWidth: 600, MinHeight: 440,
	}
}

// The editor's pages.
const (
	editorSSH      = "ssh"
	editorTerminal = "terminal"
	editorProxy    = "proxy"
	editorTunnels  = "tunnels"
)

// editorField names a field of the editor that can be wrong.
type editorField int

const (
	fieldAddr editorField = iota
	fieldPort
	fieldUser
	fieldKeyPath
	fieldTermType
)

// page is the editor's page that shows f.
func (f editorField) page() string {
	switch {
	case f == fieldTermType:
		return editorTerminal
	case slices.Contains(proxyFieldOrder, f):
		return editorProxy
	}
	return editorSSH
}

// connEditor is what the connection editor shows: the fields of a host,
// as typed, until OK saves them into the host.
type connEditor struct {
	id  string           // the host edited; "" for a new one
	nav widgets.NavState // the page shown

	name, addr, port, notes string
	auth                    config.AuthKind
	user                    string
	password                string
	keyPath, passphrase     string
	monitor                 bool
	termType                string
	backspace               config.BackspaceKey
	proxy                   proxyFields  // the Proxy page (editor_proxy.go)
	tunnels                 tunnelEditor // see editor_tunnels.go

	// checked is set once OK was pressed: from then on the errors show,
	// and follow the fields as they change.
	checked bool
	// saveErr says why saving failed, in the language shown.
	saveErr func() string
}

// newConnection opens the connection editor for a new host, or brings
// the open editor to the front. OK adds the host to the saved ones.
func (a *App) newConnection() {
	a.openEditor(nil)
}

// editConnection opens the connection editor on h, a saved host, or
// brings the open editor to the front, whichever host it edits: there is
// one editor at a time, and what it holds is not thrown away. OK saves
// the changes into h, which sessions open to it use the next time they
// connect.
func (a *App) editConnection(h *config.Host) {
	a.openEditor(h)
}

func (a *App) openEditor(h *config.Host) {
	if a.windowOpen(WindowEditor) {
		a.plat.FocusWindow(WindowEditor)
		return
	}
	ed := &connEditor{port: "22", user: "root", auth: config.AuthPassword, monitor: true}
	if h != nil {
		ed.id = h.ID
		ed.name, ed.addr, ed.notes = h.Name, h.Addr, h.Notes
		ed.port = strconv.Itoa(h.PortOr22())
		ed.auth, ed.user, ed.keyPath = h.Auth, h.User, h.KeyPath
		if ed.auth != config.AuthKey {
			ed.auth = config.AuthPassword
		}
		if ed.auth == config.AuthKey {
			ed.passphrase = a.passwords[h.ID]
		} else {
			ed.password = a.passwords[h.ID]
		}
		ed.monitor = h.MonitorEnabled()
		ed.termType, ed.backspace = h.TermType, h.Backspace
	}
	ed.proxy = a.proxyFieldsOf(h)
	ed.tunnels = newTunnelEditor(h)
	a.editor = ed
	a.openWindow(editorSpec(h != nil), a.editorView)
}

// validTermType tells whether s can be a terminal type, a terminfo
// name: letters, digits and a few signs.
func validTermType(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-.+_", r))
	})
}

// problems are what is wrong with the fields, said in the language shown.
func (ed *connEditor) problems() map[editorField]string {
	errs := map[editorField]string{}
	if strings.TrimSpace(ed.addr) == "" {
		errs[fieldAddr] = locales.ErrHostRequired()
	}
	if port, err := strconv.Atoi(strings.TrimSpace(ed.port)); err != nil || port < 1 || port > 65535 {
		errs[fieldPort] = locales.ErrPortInvalid()
	}
	if strings.TrimSpace(ed.user) == "" {
		errs[fieldUser] = locales.ErrUserRequired()
	}
	if ed.auth == config.AuthKey && strings.TrimSpace(ed.keyPath) == "" {
		errs[fieldKeyPath] = locales.ErrKeyFileRequired()
	}
	if !validTermType(strings.TrimSpace(ed.termType)) {
		errs[fieldTermType] = locales.ErrTermTypeInvalid()
	}
	ed.proxyProblems(errs)
	return errs
}

// saveEditor checks the editor's fields and saves them, closing the
// editor; when something is wrong it shows the first page with an error,
// and keeps the editor open.
func (a *App) saveEditor() {
	ed := a.editor
	ed.checked = true
	if errs := ed.problems(); len(errs) > 0 {
		first, here := "", false
		for _, f := range append([]editorField{fieldAddr, fieldPort, fieldUser, fieldKeyPath, fieldTermType}, proxyFieldOrder...) {
			if errs[f] != "" {
				first = cmp.Or(first, f.page())
				here = here || f.page() == ed.nav.Page
			}
		}
		if !here {
			ed.nav.Page = first
		}
		return
	}
	var h *config.Host
	if ed.id != "" {
		// Deleted in the manager while the editor was open: not saved back.
		if h = a.hosts.Host(ed.id); h == nil || h.Deleted() {
			ed.saveErr = locales.ErrHostDeleted
			return
		}
	} else {
		h = &config.Host{}
		a.hosts.Add(h)
		ed.id = h.ID
	}
	port, _ := strconv.Atoi(strings.TrimSpace(ed.port))
	h.Name, h.Addr, h.Port, h.Notes = strings.TrimSpace(ed.name), strings.TrimSpace(ed.addr), port, ed.notes
	h.User, h.Auth = strings.TrimSpace(ed.user), ed.auth
	secret := ed.password
	h.KeyPath = ""
	if ed.auth == config.AuthKey {
		h.KeyPath, secret = strings.TrimSpace(ed.keyPath), ed.passphrase
	}
	if secret != "" || ed.auth == config.AuthKey {
		a.passwords[h.ID] = secret
	} else {
		delete(a.passwords, h.ID) // asked for as the host connects
	}
	h.NoMonitor = !ed.monitor
	h.TermType, h.Backspace = strings.TrimSpace(ed.termType), ed.backspace
	if h.TermType == config.DefaultTermType {
		h.TermType = ""
	}
	a.saveProxy(h, &ed.proxy)
	h.Tunnels = ed.tunnels.result()
	a.hostRow = slices.Index(a.hosts.Live(), h)
	a.redraw() // the manager and the sessions show the host
	if err := a.saveHosts(a.hosts); err != nil {
		ed.saveErr = func() string { return locales.ErrSaveFailed(err.Error()) }
		return
	}
	ed.saveErr = nil
	a.closeWindow(WindowEditor)
}

// browseKey asks for the private key file, starting where the one typed
// is, else in ~/.ssh.
func (a *App) browseKey() {
	ed := a.editor
	dir := ""
	if p := strings.TrimSpace(ed.keyPath); p != "" {
		dir = filepath.Dir(sshx.ExpandHome(p))
	} else if home, err := os.UserHomeDir(); err == nil {
		if fi, err := os.Stat(filepath.Join(home, ".ssh")); err == nil && fi.IsDir() {
			dir = filepath.Join(home, ".ssh")
		}
	}
	title := locales.ChooseKeyFile()
	go func() {
		path, err := a.plat.OpenFile(WindowEditor, title, dir)
		if err != nil || path == "" {
			return
		}
		a.post(func() {
			if a.editor == ed {
				ed.keyPath = path
			}
		})
	}()
}

// termTypes are the terminal types the editor suggests.
var termTypes = []string{"xterm-256color", "xterm", "xterm-color", "vt100", "vt220", "linux", "screen-256color", "tmux-256color", "ansi"}

// editorView is the connection editor's window: the pages on the left,
// OK and Cancel below. Enter is OK, but in the notes, where it breaks
// the line, and Escape is Cancel.
func (a *App) editorView(c *ui.Context) {
	ed := a.editor
	if ed == nil {
		return
	}
	// Enter where no field takes it, and Enter in a text input, which
	// takes it as submitting.
	submit := c.Shortcut(0, ui.KeyEnter)
	input := func(e ui.Element) ui.Element {
		submit = e.Submitted() || submit
		return e
	}
	var errs map[editorField]string
	if ed.checked {
		errs = ed.problems()
	}
	pages := []widgets.NavPage{
		{ID: editorSSH, Title: locales.EditorPageSSH, View: func(c *ui.Context) { a.sshPage(c, ed, errs, input) }},
		{ID: editorTerminal, Title: locales.EditorPageTerminal, View: func(c *ui.Context) { a.terminalPage(c, ed, errs, input) }},
		{ID: editorProxy, Title: locales.EditorPageProxy, View: func(c *ui.Context) { a.proxyPage(c, ed, errs, input) }},
		{ID: editorTunnels, Title: locales.EditorPageTunnels, View: func(c *ui.Context) { a.tunnelsPage(c, ed) }},
	}
	ui.Column(c).Fill().Children(func() {
		widgets.NavPages(c, &ed.nav, pages).Grow(1).MinHeight(0)
		ui.Row(c).Gap(8).Padding(10, 16).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
			if ed.saveErr != nil {
				ui.Text(c, ed.saveErr()).TextColor(theme.Danger).Grow(1).MinWidth(0)
			} else {
				ui.Spacer(c)
			}
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.closeWindow(WindowEditor)
			}
			if ui.PrimaryButton(c, locales.Ok()).Clicked() {
				submit = true
			}
		})
	})
	if submit && a.editor == ed {
		a.saveEditor()
	}
}

// sshPage is the editor's first page: the address, how to log in, and
// whether to monitor the server. input marks a text input whose Enter is
// OK.
func (a *App) sshPage(c *ui.Context, ed *connEditor, errs map[editorField]string, input func(ui.Element) ui.Element) {
	ui.Form(c, func() {
		ui.Fieldset(c, locales.SectionGeneral(), func() {
			ui.Field(c, locales.FieldName(), func() {
				input(ui.TextInput(c, &ed.name).Placeholder(locales.FieldNamePlaceholder()))
			})
			ui.Field(c, locales.FieldHost(), func() {
				input(ui.TextInput(c, &ed.addr).Placeholder(locales.FieldHostPlaceholder()).AutoFocus())
			}).Error(errs[fieldAddr])
			ui.Field(c, locales.FieldPort(), func() {
				input(ui.TextInput(c, &ed.port).Width(90))
			}).Error(errs[fieldPort])
			ui.Field(c, locales.FieldNotes(), func() {
				ui.TextArea(c, &ed.notes).Lines(2, 4)
			})
		})
		ui.Fieldset(c, locales.SectionAuth(), func() {
			ui.Field(c, locales.FieldAuth(), func() {
				byPassword, byKey := locales.AuthPassword(), locales.AuthKey()
				chosen := byPassword
				if ed.auth == config.AuthKey {
					chosen = byKey
				}
				if ui.Select(c, &chosen, []string{byPassword, byKey}).Width(160).Changed() {
					ed.auth = config.AuthPassword
					if chosen == byKey {
						ed.auth = config.AuthKey
					}
				}
			})
			ui.Field(c, locales.FieldUser(), func() {
				input(ui.TextInput(c, &ed.user))
			}).Error(errs[fieldUser])
			if ed.auth == config.AuthKey {
				ui.Field(c, locales.FieldKeyFile(), func() {
					ui.Row(c).Gap(8).Children(func() {
						input(ui.TextInput(c, &ed.keyPath).Placeholder("~/.ssh/id_ed25519").Grow(1).MinWidth(0))
						if ui.Button(c, locales.Browse()).Clicked() {
							a.browseKey()
						}
					})
				}).Error(errs[fieldKeyPath])
				ui.Field(c, locales.FieldPassphrase(), func() {
					input(ui.TextInput(c, &ed.passphrase).Password().Placeholder(locales.PassphrasePlaceholder()))
				}).Description(locales.PasswordNote())
			} else {
				ui.Field(c, locales.FieldPassword(), func() {
					input(ui.TextInput(c, &ed.password).Password())
				}).Description(locales.PasswordNote())
			}
		})
		ui.Fieldset(c, locales.SectionAdvanced(), func() {
			ui.Field(c, "", func() {
				ui.Checkbox(c, &ed.monitor, locales.MonitorChannel())
			}).Description(locales.MonitorChannelHint())
		})
	})
}

// terminalPage is the editor's page of the terminal's options.
func (a *App) terminalPage(c *ui.Context, ed *connEditor, errs map[editorField]string, input func(ui.Element) ui.Element) {
	ui.Form(c, func() {
		ui.Field(c, locales.FieldTermType(), func() {
			input(ui.Autocomplete(c, &ed.termType, termTypes).Placeholder(config.DefaultTermType).Width(220))
		}).Error(errs[fieldTermType])
		ui.Field(c, locales.FieldEncoding(), func() {
			// The terminal reads and writes UTF-8 alone: libghostty-vt
			// decodes nothing else, and the app converts nothing.
			utf8 := "UTF-8"
			ui.Select(c, &utf8, []string{utf8}).Width(220).Disabled(true)
		}).Description(locales.EncodingHint())
		ui.Field(c, locales.FieldBackspace(), func() {
			del, ctrlH := locales.BackspaceDEL(), locales.BackspaceCtrlH()
			chosen := del
			if ed.backspace == config.BackspaceCtrlH {
				chosen = ctrlH
			}
			if ui.Select(c, &chosen, []string{del, ctrlH}).Width(220).Changed() {
				ed.backspace = config.BackspaceDEL
				if chosen == ctrlH {
					ed.backspace = config.BackspaceCtrlH
				}
			}
		})
	})
	widgets.Muted(c, locales.TerminalNextConnect()).FontSize(12)
}
