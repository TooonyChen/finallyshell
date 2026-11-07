package app

import (
	"net"
	"slices"
	"strconv"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/icons"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// tunnelEditor is the connection editor's Tunnels page: the host's
// tunnels as edited, saved with the host on OK, and the dialog that adds
// or changes one.
type tunnelEditor struct {
	list  []config.Tunnel
	table ui.ListState
	row   int // the row chosen, -1 for none

	dialog  bool
	editing int // the row the dialog changes, -1 for a new tunnel
	form    tunnelForm
}

// tunnelForm is the dialog's fields, as typed.
type tunnelForm struct {
	kind                   config.TunnelKind
	listenPort             string
	public                 bool // listens on listenHost, not the loopback
	listenHost             string
	targetHost, targetPort string
	enabled                bool
	// keep is the tunnel edited, whose keys this build does not know
	// are kept.
	keep config.Tunnel
	// checked is set once OK was pressed, as the editor's.
	checked bool
}

// tunnelField names a field of the dialog that can be wrong.
type tunnelField int

const (
	fieldListenPort tunnelField = iota
	fieldListenHost
	fieldTargetHost
	fieldTargetPort
)

// newTunnelEditor is the page for h's tunnels; nil for a new host.
func newTunnelEditor(h *config.Host) tunnelEditor {
	te := tunnelEditor{row: -1, editing: -1}
	if h != nil {
		te.list = slices.Clone(h.Tunnels)
	}
	return te
}

// result is the tunnels to save: nil for none, as the host had.
func (te *tunnelEditor) result() []config.Tunnel {
	if len(te.list) == 0 {
		return nil
	}
	return slices.Clone(te.list)
}

// tunnelKindLabel is the kind of a tunnel in the language shown.
func tunnelKindLabel(k config.TunnelKind) string {
	switch k {
	case config.TunnelLocal:
		return locales.TunnelKindLocal()
	case config.TunnelRemote:
		return locales.TunnelKindRemote()
	case config.TunnelDynamic:
		return locales.TunnelKindDynamic()
	}
	return string(k)
}

// tunnelTarget is where t forwards to, as the tables show it.
func tunnelTarget(t *config.Tunnel) string {
	if t.Kind == config.TunnelDynamic {
		return locales.TunnelSocks()
	}
	return t.TargetAddr()
}

func tunnelColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "type", Title: locales.ColTunnelType(), Width: 110},
		{ID: "listen", Title: locales.ColTunnelListen(), MinWidth: 120},
		{ID: "target", Title: locales.ColTunnelTarget(), MinWidth: 120},
		{ID: "enabled", Title: locales.ColTunnelEnabled(), Width: 64, Align: ui.Center},
	}
}

// tunnelsPage is the editor's Tunnels page: a table of the host's
// tunnels, with buttons to add, change and remove them.
func (a *App) tunnelsPage(c *ui.Context, ed *connEditor) {
	te := &ed.tunnels
	widgets.Muted(c, locales.TunnelsIntro()).FontSize(12)
	list := te.list
	if te.row >= len(list) {
		te.row = -1
	}
	te.table.Selected = &te.row
	te.table.Label = func(i int) string { return tunnelKindLabel(list[i].Kind) + " " + list[i].ListenAddr() }
	table := ui.Table(c, &te.table, tunnelColumns(), len(list), func(row, col int) {
		t := &list[row]
		muted := theme.TextMuted
		if row == te.row {
			muted = theme.AccentText.Alpha(0.8)
		}
		switch col {
		case 0:
			ui.Text(c, tunnelKindLabel(t.Kind)).SingleLine()
		case 1:
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, t.ListenAddr()).SingleLine()
				if !config.IsLoopback(t.ListenHost) {
					ui.Icon(c, icons.Get("triangle-alert")).FontSize(13).TextColor(theme.Warning).Tooltip(publicWarning(t.Kind))
				}
			})
		case 2:
			text := ui.Text(c, tunnelTarget(t)).SingleLine()
			if t.Kind == config.TunnelDynamic {
				text.TextColor(muted)
			}
		case 3:
			on := t.Enabled()
			if ui.Checkbox(c, &on, "").Label(locales.TunnelStartWithSession()).Changed() {
				te.list[row].Disabled = !on
			}
		}
	}).Height(220).Border(1, theme.Border).Radius(6)
	table.Children(func() {
		if len(list) == 0 {
			widgets.Muted(c, locales.TunnelsEmpty()).Padding(36, 12)
		}
	})
	if table.Submitted() && te.row >= 0 {
		te.open(te.row)
	}
	chosen := te.row >= 0 && te.row < len(list)
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, locales.TunnelAdd()).Clicked() {
			te.open(-1)
		}
		if ui.Button(c, locales.TunnelEdit()).Disabled(!chosen).Clicked() && chosen {
			te.open(te.row)
		}
		if ui.Button(c, locales.TunnelRemove()).Disabled(!chosen).Clicked() && chosen {
			te.list = slices.Delete(te.list, te.row, te.row+1)
			te.row = min(te.row, len(te.list)-1)
		}
	})
	widgets.Muted(c, locales.TerminalNextConnect()).FontSize(12)
	ui.Modal(c, &te.dialog, func() { te.dialogView(c) })
}

// open shows the dialog for the tunnel in row, or a new one for -1.
func (te *tunnelEditor) open(row int) {
	t := config.Tunnel{Kind: config.TunnelLocal, TargetHost: config.LoopbackHost}
	if row >= 0 {
		t = te.list[row]
	}
	f := tunnelForm{kind: t.Kind, keep: t, enabled: t.Enabled(), targetHost: t.TargetHost, listenHost: t.ListenHost}
	if t.ListenPort != 0 {
		f.listenPort = strconv.Itoa(t.ListenPort)
	}
	if t.TargetPort != 0 {
		f.targetPort = strconv.Itoa(t.TargetPort)
	}
	if !config.IsLoopback(t.ListenHost) {
		f.public = true
	} else {
		f.listenHost = ""
	}
	te.form, te.editing, te.dialog = f, row, true
}

// validPort reads a port from 1 to 65535.
func validPort(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil && n >= 1 && n <= 65535
}

// validHost tells whether s can be an address or a host name.
func validHost(s string) bool {
	if s == "" {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	return !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_')
	})
}

// tunnel is the tunnel the form says, with the keys the one edited had
// that this build does not know.
func (f *tunnelForm) tunnel() config.Tunnel {
	t := f.keep
	t.Kind = f.kind
	t.ListenPort, _ = strconv.Atoi(strings.TrimSpace(f.listenPort))
	t.ListenHost = ""
	if f.public {
		t.ListenHost = strings.TrimSpace(f.listenHost)
	}
	if remoteSide(f.kind) && strings.EqualFold(t.ListenHost, "localhost") {
		t.ListenHost = "" // the server's 127.0.0.1
	}
	t.TargetHost, t.TargetPort = "", 0
	if f.kind != config.TunnelDynamic {
		t.TargetHost = strings.TrimSpace(f.targetHost)
		t.TargetPort, _ = strconv.Atoi(strings.TrimSpace(f.targetPort))
	}
	t.Disabled = !f.enabled
	return t
}

// remoteSide tells whether a tunnel of kind listens on the server.
func remoteSide(k config.TunnelKind) bool { return k == config.TunnelRemote }

// sameListener tells whether a and b would listen on the same port of
// the same machine.
func sameListener(a, b *config.Tunnel) bool {
	if remoteSide(a.Kind) != remoteSide(b.Kind) || a.ListenPort != b.ListenPort {
		return false
	}
	ha, hb := a.ListenHostOrLoopback(), b.ListenHostOrLoopback()
	wildcard := func(h string) bool { ip := net.ParseIP(h); return ip != nil && ip.IsUnspecified() }
	return ha == hb || wildcard(ha) || wildcard(hb)
}

// problems are what is wrong with the dialog's fields, in the language
// shown.
func (te *tunnelEditor) problems() map[tunnelField]string {
	f := &te.form
	errs := map[tunnelField]string{}
	if _, ok := validPort(f.listenPort); !ok {
		errs[fieldListenPort] = locales.ErrPortInvalid()
	}
	if host := strings.TrimSpace(f.listenHost); f.public && !validHost(host) {
		errs[fieldListenHost] = locales.ErrListenAddrInvalid()
	} else if f.public && remoteSide(f.kind) && !strings.EqualFold(host, "localhost") && net.ParseIP(host) == nil {
		// Sent to the server as an IP address: x/crypto would look a
		// name up on this computer.
		errs[fieldListenHost] = locales.ErrRemoteListenAddrIP()
	}
	if f.kind != config.TunnelDynamic {
		if strings.TrimSpace(f.targetHost) == "" {
			errs[fieldTargetHost] = locales.ErrTargetHostRequired()
		} else if !validHost(strings.TrimSpace(f.targetHost)) {
			errs[fieldTargetHost] = locales.ErrListenAddrInvalid()
		}
		if _, ok := validPort(f.targetPort); !ok {
			errs[fieldTargetPort] = locales.ErrPortInvalid()
		}
	}
	if errs[fieldListenPort] == "" && errs[fieldListenHost] == "" {
		t := f.tunnel()
		for i := range te.list {
			if i != te.editing && sameListener(&t, &te.list[i]) {
				errs[fieldListenPort] = locales.ErrTunnelDuplicate()
				break
			}
		}
	}
	return errs
}

// saveDialog puts the dialog's tunnel in the list, unless something is
// wrong with it.
func (te *tunnelEditor) saveDialog() {
	te.form.checked = true
	if len(te.problems()) > 0 {
		return
	}
	t := te.form.tunnel()
	if te.editing >= 0 && te.editing < len(te.list) {
		te.list[te.editing] = t
		te.row = te.editing
	} else {
		te.list = append(te.list, t)
		te.row = len(te.list) - 1
	}
	te.dialog = false
}

// publicWarning says who else can use a tunnel of kind that listens on
// an address other than the loopback.
func publicWarning(k config.TunnelKind) string {
	if remoteSide(k) {
		return locales.TunnelPublicWarnRemote()
	}
	return locales.TunnelPublicWarnLocal()
}

// dialogView is the dialog that adds or changes a tunnel. Enter is OK,
// and Escape, as a click outside, Cancel.
func (te *tunnelEditor) dialogView(c *ui.Context) {
	f := &te.form
	var errs map[tunnelField]string
	if f.checked {
		errs = te.problems()
	}
	// Enter where no field takes it, and in a text input, which takes it as
	// submitting: the editor's Enter waits while the dialog shows.
	submit := c.Shortcut(0, ui.KeyEnter)
	input := func(e ui.Element) ui.Element {
		submit = e.Submitted() || submit
		return e
	}
	title := locales.TunnelNew()
	if te.editing >= 0 {
		title = locales.TunnelChange()
	}
	ui.Column(c).Width(460).Gap(10).Children(func() {
		ui.Text(c, title).FontSize(15).Bold()
		ui.Form(c, func() {
			ui.Field(c, locales.FieldTunnelType(), func() {
				kinds := []config.TunnelKind{config.TunnelLocal, config.TunnelRemote, config.TunnelDynamic}
				labels := make([]string, len(kinds))
				for i, k := range kinds {
					labels[i] = tunnelKindLabel(k)
				}
				chosen := tunnelKindLabel(f.kind)
				if ui.Select(c, &chosen, labels).Width(200).Changed() {
					f.kind = kinds[max(0, slices.Index(labels, chosen))]
				}
			}).Description(kindHint(f.kind))
			ui.Field(c, locales.FieldListenPort(), func() {
				input(ui.TextInput(c, &f.listenPort).Width(90).AutoFocus())
			}).Error(errs[fieldListenPort])
			loopback := locales.TunnelLoopbackNote()
			if remoteSide(f.kind) {
				loopback = locales.TunnelLoopbackNoteRemote()
			}
			if f.public {
				loopback = ""
			}
			ui.Field(c, "", func() {
				if ui.Checkbox(c, &f.public, locales.TunnelOtherHosts()).Changed() && f.public && strings.TrimSpace(f.listenHost) == "" {
					f.listenHost = "0.0.0.0"
				}
			}).Description(loopback)
			if f.public {
				ui.Field(c, locales.FieldListenAddr(), func() {
					input(ui.TextInput(c, &f.listenHost).Width(200))
				}).Error(errs[fieldListenHost])
				ui.Row(c).Gap(8).Padding(8, 10).Radius(6).Background(theme.Warning.Alpha(0.12)).Children(func() {
					ui.Icon(c, icons.Get("triangle-alert")).FontSize(15).TextColor(theme.Warning)
					ui.Text(c, publicWarning(f.kind)).FontSize(12).Grow(1).MinWidth(0)
				})
			}
			if f.kind != config.TunnelDynamic {
				placeholder := locales.TargetHostPlaceholderLocal()
				if remoteSide(f.kind) {
					placeholder = locales.TargetHostPlaceholderRemote()
				}
				ui.Field(c, locales.FieldTargetHost(), func() {
					input(ui.TextInput(c, &f.targetHost).Placeholder(placeholder))
				}).Error(errs[fieldTargetHost])
				ui.Field(c, locales.FieldTargetPort(), func() {
					input(ui.TextInput(c, &f.targetPort).Width(90))
				}).Error(errs[fieldTargetPort])
			}
			ui.Field(c, "", func() {
				ui.Checkbox(c, &f.enabled, locales.TunnelStartWithSession())
			})
		})
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, locales.Cancel()).Clicked() {
				te.dialog = false
			}
			if ui.PrimaryButton(c, locales.Ok()).Clicked() {
				submit = true
			}
		})
	})
	if submit {
		te.saveDialog()
	}
}

// kindHint says what a tunnel of kind does.
func kindHint(k config.TunnelKind) string {
	switch k {
	case config.TunnelRemote:
		return locales.TunnelHintRemote()
	case config.TunnelDynamic:
		return locales.TunnelHintDynamic()
	}
	return locales.TunnelHintLocal()
}
