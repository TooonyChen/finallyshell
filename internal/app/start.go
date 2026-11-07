package app

import (
	"slices"
	"strconv"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/icons"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// The start page is what a new tab shows, as FinalShell's "New tab": the
// hosts connected to last, to connect to again with a click.
//
// A new tab is a Session without a Host. It is a row of the session list
// as any session is, so it is selected, cycled with Ctrl+Tab, dragged and
// closed the same way; connecting from its page gives it the host, and
// the same row becomes the session. With no row selected, as once the
// last one is closed, the terminal area shows the page too, and
// connecting there opens a tab.

// startState is what a start page keeps: its search and the row chosen.
type startState struct {
	query string
	row   int // in the hosts listed, -1 for none
	list  ui.ListState
}

// recentLimit is how many recent hosts the start page lists.
const recentLimit = 30

// isNewTab tells whether s is a new tab, with no host yet.
func (s *Session) isNewTab() bool { return s.Host == nil }

// NewTab adds a new tab at the end of the session list and selects it,
// as the "+" button does; the app starts with one.
func (a *App) NewTab() {
	// Closed rather than connecting, the zero status: quitting ends nothing.
	s := newSession(nil)
	s.status = statusClosed
	a.sessions = append(a.sessions, s)
	a.active = len(a.sessions) - 1
}

// selectedNewTab is the selected row when it is a new tab, else nil.
func (a *App) selectedNewTab() *Session {
	if s := a.current(); s != nil && s.isNewTab() {
		return s
	}
	return nil
}

// fillTab turns the new tab into h's session, connecting it with the
// password or passphrase, and selects it.
func (a *App) fillTab(tab *Session, h *config.Host, secret string) {
	tab.Host = h
	a.active = slices.Index(a.sessions, tab)
	a.connect(tab, secret)
}

// startNewConnection opens the connection editor for a new host.
func (a *App) startNewConnection() { a.newConnection() }

// clearRecents empties the recent list, keeping the hosts.
func (a *App) clearRecents() {
	a.hosts.ClearRecents()
	a.saveHosts(a.hosts)
	a.redraw()
}

// quickHosts are the hosts the start page lists: the recent ones, the
// last connected first, or while searching, every host that matches, the
// recent ones first and then the others in the order they are kept.
func quickHosts(store *config.Store, query string) []*config.Host {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return store.Recent(recentLimit)
	}
	var hosts []*config.Host
	for _, h := range store.Recent(0) {
		if hostMatches(h, words) {
			hosts = append(hosts, h)
		}
	}
	for _, h := range store.Live() {
		if h.LastConnectedAt.IsZero() && hostMatches(h, words) {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// hostMatches tells whether each of the words, in lower case, is in the
// host's name, address, user or notes.
func hostMatches(h *config.Host, words []string) bool {
	text := strings.ToLower(strings.Join([]string{h.Name, h.Target(), h.Notes}, "\n"))
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// startKey keys the start page of a tab, apart from its terminal.
type startKey struct{ id int64 }

// startPage is the page of the new tab tab, or of no tab for nil.
func (a *App) startPage(c *ui.Context, tab *Session) {
	st, key := &a.start, startKey{}
	if tab != nil {
		st, key = &tab.start, startKey{tab.ID}
	}
	ui.Column(c.Key(key)).Fill().Background(theme.Background).Children(func() {
		if len(a.hosts.Live()) == 0 {
			a.startEmpty(c)
			return
		}
		ui.Column(c).Fill().MaxWidth(820).AlignSelf(ui.Center).Padding(24, 28, 16, 28).Gap(12).Children(func() {
			a.quickConnect(c, tab, st)
		})
	})
}

// startEmpty is the page while there are no hosts at all.
func (a *App) startEmpty(c *ui.Context) {
	ui.Column(c).Fill().Center().Gap(12).Children(func() {
		ui.Icon(c, icons.Get("server")).FontSize(40).TextColor(theme.TextMuted)
		ui.Text(c, locales.NoHostsTitle()).FontSize(16)
		widgets.Muted(c, locales.NoHostsBody())
		if ui.PrimaryButton(c, locales.NewConnection()).AutoFocus().Clicked() {
			a.startNewConnection()
		}
	})
}

// quickConnect is the list of hosts to connect to from tab, with its
// search, and the buttons under it.
func (a *App) quickConnect(c *ui.Context, tab *Session, st *startState) {
	hosts := quickHosts(a.hosts, st.query)
	if st.row >= len(hosts) {
		st.row = len(hosts) - 1
	}
	var connect *config.Host
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.QuickConnect()).FontSize(16).Bold().Grow(1)
		search := ui.SearchField(c, &st.query).Placeholder(locales.SearchHosts()).Label(locales.SearchHosts()).Width(300).AutoFocus()
		if search.Changed() {
			hosts = quickHosts(a.hosts, st.query)
			st.row = -1
			if st.query != "" && len(hosts) > 0 {
				st.row = 0
			}
		}
		// Up and Down choose a host from the search, Enter connects it,
		// or the first.
		switch {
		case search.Shortcut(0, ui.KeyDown) && len(hosts) > 0:
			st.row = min(st.row+1, len(hosts)-1)
		case search.Shortcut(0, ui.KeyUp) && len(hosts) > 0:
			st.row = max(st.row-1, 0)
		}
		if search.Submitted() && len(hosts) > 0 {
			connect = hosts[max(st.row, 0)]
		}
	})
	ui.Column(c).Grow(1).MinHeight(0).Border(1, theme.Border).Radius(6).Clip().Children(func() {
		// The list pads its rows by 4 more.
		ui.Row(c).Gap(12).Padding(6, 16).Background(ui.Hex("#f9fafb")).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			quickColumns(c, theme.TextMuted, locales.HostColName(), locales.HostColHost(), locales.HostColUser())
		})
		st.list.Selected = &st.row
		st.list.Key = func(i int) any { return hosts[i].ID }
		st.list.Label = func(i int) string { return hosts[i].Title() }
		list := ui.List(c, &st.list, len(hosts), func(i int) {
			h := hosts[i]
			color := theme.Text
			if i == st.row {
				color = theme.AccentText
			}
			row := ui.Row(c).Gap(12).Padding(7, 12).Cursor(ui.CursorPointer).AlignItems(ui.Center)
			if row.Clicked() {
				connect = h
			}
			row.Children(func() {
				ui.Row(c).Gap(8).Grow(3).Basis(0).MinWidth(0).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.Get("server")).FontSize(14).TextColor(color.Alpha(0.7))
					ui.Text(c, h.Title()).SingleLine().TextColor(color).Grow(1).MinWidth(0)
				})
				addr := h.Addr
				if h.PortOr22() != 22 {
					addr += ":" + strconv.Itoa(h.PortOr22())
				}
				quickColumns(c, color, "", addr, h.User)
			})
		}).Grow(1).Padding(4).Children(func() {
			msg := locales.NoRecentHosts()
			if strings.TrimSpace(st.query) != "" {
				msg = locales.NoMatchingHosts()
			}
			if len(hosts) == 0 {
				widgets.Muted(c, msg).Padding(12, 10)
			}
		})
		if list.Submitted() && st.row >= 0 && st.row < len(hosts) {
			connect = hosts[st.row]
		}
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, locales.ClearRecents()).Disabled(len(a.hosts.Recent(1)) == 0).Clicked() {
			a.clearRecents()
		}
		widgets.Muted(c, locales.WelcomeHint()).FontSize(12).Grow(1).TextAlign(ui.End)
		if ui.Button(c, locales.ConnectionManager()).Clicked() {
			a.openManager()
		}
	})
	if connect != nil {
		a.openSessionIn(tab, connect)
	}
}

// quickColumns lays out the columns of a quick connect row, past the
// first for name == "": the name, the host and the user.
func quickColumns(c *ui.Context, color ui.Color, name, host, user string) {
	if name != "" {
		ui.Text(c, name).SingleLine().TextColor(color).Grow(3).Basis(0).MinWidth(0)
	}
	ui.Text(c, host).SingleLine().TextColor(color).Grow(3).Basis(0).MinWidth(0)
	ui.Text(c, user).SingleLine().TextColor(color).Grow(2).Basis(0).MinWidth(0)
}
