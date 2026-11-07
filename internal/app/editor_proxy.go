package app

// The connection editor's Proxy page: connect directly, through a SOCKS5
// or HTTP proxy, or through another saved host, a jump host. Connecting
// through them is in proxy.go and internal/sshx/proxy.go.

import (
	"errors"
	"strconv"
	"strings"

	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The Proxy page's fields that can be wrong.
const (
	fieldProxyAddr editorField = 100 + iota
	fieldProxyPort
	fieldJumpHost
)

// proxyFieldOrder are the Proxy page's fields, in the order they show.
var proxyFieldOrder = []editorField{fieldProxyAddr, fieldProxyPort, fieldJumpHost}

// proxyFields are the Proxy page's fields, as typed. Those of a kind not
// chosen are kept while the editor is open, and not saved.
type proxyFields struct {
	kind                       config.ProxyKind
	addr, port, user, password string
	jump                       string // the jump host's ID

	// saved is the host's proxy as the editor opened, whose JSON keys
	// this build does not know saving keeps; nil for none.
	saved *config.ProxyConfig
	// hosts are the saved hosts, which the jump host is one of.
	hosts *config.Store
}

// proxyFieldsOf are the Proxy page's fields for h, nil for a new host.
func (a *App) proxyFieldsOf(h *config.Host) proxyFields {
	pf := proxyFields{hosts: a.hosts}
	if h == nil || h.Proxy == nil {
		return pf
	}
	p := h.Proxy
	pf.saved = p
	pf.kind, pf.addr, pf.user, pf.jump = p.Kind, p.Addr, p.User, p.JumpHost
	if port := p.PortOrDefault(); port != 0 {
		pf.port = strconv.Itoa(port)
	}
	pf.password = a.proxySecrets()[proxySecretKey(h.ID)]
	return pf
}

// setKind chooses the kind, with its usual port while the port is empty
// or the last kind's.
func (pf *proxyFields) setKind(k config.ProxyKind) {
	if p := strings.TrimSpace(pf.port); p == "" || p == strconv.Itoa(config.DefaultProxyPort(pf.kind)) {
		pf.port = ""
		if port := config.DefaultProxyPort(k); port != 0 {
			pf.port = strconv.Itoa(port)
		}
	}
	pf.kind = k
}

// proxyProblems adds to errs what is wrong with the Proxy page.
func (ed *connEditor) proxyProblems(errs map[editorField]string) {
	pf := &ed.proxy
	switch pf.kind {
	case config.ProxySOCKS5, config.ProxyHTTP:
		if strings.TrimSpace(pf.addr) == "" {
			errs[fieldProxyAddr] = locales.ErrHostRequired()
		}
		if port, err := strconv.Atoi(strings.TrimSpace(pf.port)); err != nil || port < 1 || port > 65535 {
			errs[fieldProxyPort] = locales.ErrPortInvalid()
		}
	case config.ProxyJump:
		j := pf.hosts.Host(pf.jump)
		if j == nil || j.Deleted() || j.ID == ed.id {
			errs[fieldJumpHost] = locales.ErrJumpHostRequired()
			return
		}
		// The chain from here, as this host would be saved.
		self := &config.Host{ID: ed.id, Name: strings.TrimSpace(ed.name), Addr: strings.TrimSpace(ed.addr),
			Proxy: &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: pf.jump}}
		if self.Title() == "" {
			self.Name = locales.NewConnection()
		}
		_, err := pf.hosts.JumpHosts(self)
		var loop *config.JumpLoopError
		var missing *config.JumpError
		switch {
		case errors.As(err, &loop):
			errs[fieldJumpHost] = locales.ErrJumpLoop(strings.Join(loop.Names, " → "))
		case errors.As(err, &missing):
			errs[fieldJumpHost] = locales.ErrJumpHostMissing(missing.From)
		}
	}
}

// saveProxy saves the Proxy page into h, and keeps the proxy's password
// as the host's: in proxySecrets, not in hosts.json. A proxy with a user
// and no password asks for it as the host connects.
func (a *App) saveProxy(h *config.Host, pf *proxyFields) {
	key := proxySecretKey(h.ID)
	delete(a.proxySecrets(), key)
	if pf.kind == config.ProxyNone {
		h.Proxy = nil
		return
	}
	// A new value, not the saved one changed: a session connecting
	// reads its copy of the host's.
	var p config.ProxyConfig
	if pf.saved != nil {
		p = *pf.saved
	}
	p.Kind, p.Addr, p.Port, p.User, p.JumpHost = pf.kind, "", 0, "", ""
	switch pf.kind {
	case config.ProxyJump:
		p.JumpHost = pf.jump
	default:
		p.Addr, p.User = strings.TrimSpace(pf.addr), strings.TrimSpace(pf.user)
		p.Port, _ = strconv.Atoi(strings.TrimSpace(pf.port))
		if p.User != "" && pf.password != "" {
			a.proxySecrets()[key] = pf.password
		}
	}
	h.Proxy = &p
	pf.saved = &p
}

// jumpChoice is a host the Proxy page offers as the jump host.
type jumpChoice struct {
	id, label string
}

// jumpChoices are the hosts that can be the jump host of the host with
// the ID: the others not deleted, in order, each labeled with its name
// and user@host, told apart when two read the same.
func jumpChoices(hosts *config.Store, id string) []jumpChoice {
	var out []jumpChoice
	seen := map[string]int{}
	for _, h := range hosts.Live() {
		if h.ID == id {
			continue
		}
		label := h.Target()
		if h.Name != "" {
			label = h.Name + " (" + label + ")"
		}
		if seen[label]++; seen[label] > 1 {
			label += " #" + strconv.Itoa(seen[label])
		}
		out = append(out, jumpChoice{h.ID, label})
	}
	return out
}

// proxyKinds are the kinds the Proxy page offers, in order, with their
// labels.
func proxyKinds() ([]config.ProxyKind, []string) {
	return []config.ProxyKind{config.ProxyNone, config.ProxySOCKS5, config.ProxyHTTP, config.ProxyJump},
		[]string{locales.ProxyNone(), "SOCKS5", "HTTP", locales.ProxyJumpHost()}
}

// proxyPage is the editor's Proxy page. input marks a text input whose
// Enter is OK.
func (a *App) proxyPage(c *ui.Context, ed *connEditor, errs map[editorField]string, input func(ui.Element) ui.Element) {
	pf := &ed.proxy
	ui.Form(c, func() {
		ui.Field(c, locales.FieldProxyType(), func() {
			kinds, labels := proxyKinds()
			chosen := labels[0]
			for i, k := range kinds {
				if k == pf.kind {
					chosen = labels[i]
				}
			}
			if ui.Select(c, &chosen, labels).Width(220).Changed() {
				for i, l := range labels {
					if l == chosen {
						pf.setKind(kinds[i])
					}
				}
			}
		}).Description(proxyKindHint(pf.kind))
		switch pf.kind {
		case config.ProxySOCKS5, config.ProxyHTTP:
			ui.Field(c, locales.FieldHost(), func() {
				input(ui.TextInput(c, &pf.addr).Placeholder(locales.FieldHostPlaceholder()))
			}).Error(errs[fieldProxyAddr])
			ui.Field(c, locales.FieldPort(), func() {
				input(ui.TextInput(c, &pf.port).Width(90))
			}).Error(errs[fieldProxyPort])
			ui.Field(c, locales.FieldUser(), func() {
				input(ui.TextInput(c, &pf.user).Placeholder(locales.ProxyUserPlaceholder()))
			})
			if strings.TrimSpace(pf.user) != "" {
				ui.Field(c, locales.FieldPassword(), func() {
					input(ui.TextInput(c, &pf.password).Password())
				}).Description(a.rememberNote(ed.remember))
			}
		case config.ProxyJump:
			choices := jumpChoices(pf.hosts, ed.id)
			if len(choices) == 0 {
				ui.Field(c, locales.FieldJumpHost(), func() {
					widgets.Muted(c, locales.NoJumpHosts())
				}).Error(errs[fieldJumpHost])
				break
			}
			labels := make([]string, len(choices))
			chosen := ""
			for i, ch := range choices {
				labels[i] = ch.label
				if ch.id == pf.jump {
					chosen = ch.label
				}
			}
			ui.Field(c, locales.FieldJumpHost(), func() {
				if ui.Select(c, &chosen, labels).Width(320).Changed() {
					for _, ch := range choices {
						if ch.label == chosen {
							pf.jump = ch.id
						}
					}
				}
			}).Error(errs[fieldJumpHost]).Description(locales.JumpHostNote())
		}
	})
}

// proxyKindHint says what the kind of proxy does.
func proxyKindHint(k config.ProxyKind) string {
	switch k {
	case config.ProxySOCKS5:
		return locales.ProxySOCKS5Hint()
	case config.ProxyHTTP:
		return locales.ProxyHTTPHint()
	case config.ProxyJump:
		return locales.ProxyJumpHint()
	}
	return locales.ProxyNoneHint()
}
