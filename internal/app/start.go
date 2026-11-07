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
// hosts connected to last, to connect to again with a click, and under
// them the other hosts, the last added first, so that a host just added
// is there before it has ever connected.
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
	row   int // in the rows listed, a host's, -1 for none
	list  ui.ListState
}

// recentLimit is how many hosts the start page lists as recent; those
// connected to before them are among the other hosts.
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

// quickHosts are the hosts the start page lists, while searching those
// that match: first the recent ones, the last connected first, then the
// other hosts that are not deleted, the last added first, and after them
// those saved before the time a host is added was kept, in the order
// they are kept. recent is how many of them are recent.
func quickHosts(store *config.Store, query string) (hosts []*config.Host, recent int) {
	words := strings.Fields(strings.ToLower(query))
	recents := store.Recent(recentLimit)
	others := slices.DeleteFunc(store.Live(), func(h *config.Host) bool { return slices.Contains(recents, h) })
	// A zero time is the earliest: those hosts go last, in their order.
	slices.SortStableFunc(others, func(a, b *config.Host) int { return b.CreatedAt.Compare(a.CreatedAt) })
	unmatched := func(h *config.Host) bool { return !hostMatches(h, words) }
	recents = slices.DeleteFunc(recents, unmatched)
	others = slices.DeleteFunc(others, unmatched)
	return append(recents, others...), len(recents)
}

// quickHeader is the header of a part of the quick connect list, and its
// key there.
type quickHeader int

const (
	headerRecent quickHeader = iota
	headerOther
)

func (h quickHeader) String() string {
	if h == headerRecent {
		return locales.RecentHosts()
	}
	return locales.OtherHosts()
}

// quickRow is a row of the quick connect list: a host, or for none the
// header of the part under it.
type quickRow struct {
	host   *config.Host
	header quickHeader
}

// quickRows are the rows listing hosts, of which the first recent are
// recent: with a header over each part when there are both.
func quickRows(hosts []*config.Host, recent int) []quickRow {
	headers := recent > 0 && recent < len(hosts)
	rows := make([]quickRow, 0, len(hosts)+2)
	for i, h := range hosts {
		switch {
		case headers && i == 0:
			rows = append(rows, quickRow{header: headerRecent})
		case headers && i == recent:
			rows = append(rows, quickRow{header: headerOther})
		}
		rows = append(rows, quickRow{host: h})
	}
	return rows
}

// hostRows are the indexes of the rows of hosts, past the headers.
func hostRows(rows []quickRow) []int {
	var out []int
	for i, r := range rows {
		if r.host != nil {
			out = append(out, i)
		}
	}
	return out
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
	rows := quickRows(quickHosts(a.hosts, st.query))
	hostAt := hostRows(rows)
	if st.row >= len(rows) {
		st.row = -1
		if len(hostAt) > 0 {
			st.row = hostAt[len(hostAt)-1]
		}
	}
	if st.row >= 0 && rows[st.row].host == nil {
		st.row++ // a header: the host under it
	}
	var connect *config.Host
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.QuickConnect()).FontSize(16).Bold().Grow(1)
		search := ui.SearchField(c, &st.query).Placeholder(locales.SearchHosts()).Label(locales.SearchHosts()).Width(300).AutoFocus()
		if search.Changed() {
			rows = quickRows(quickHosts(a.hosts, st.query))
			hostAt = hostRows(rows)
			st.row = -1
			if st.query != "" && len(hostAt) > 0 {
				st.row = hostAt[0]
			}
		}
		if len(hostAt) == 0 {
			return
		}
		// Up and Down choose a host from the search, past the headers;
		// Enter connects it, or the first.
		at := slices.Index(hostAt, st.row)
		switch {
		case search.Shortcut(0, ui.KeyDown):
			st.row = hostAt[min(at+1, len(hostAt)-1)]
		case search.Shortcut(0, ui.KeyUp):
			st.row = hostAt[max(at-1, 0)]
		}
		if search.Submitted() {
			connect = rows[hostAt[max(slices.Index(hostAt, st.row), 0)]].host
		}
	})
	ui.Column(c).Grow(1).MinHeight(0).Border(1, theme.Border).Radius(6).Clip().Children(func() {
		// The list pads its rows by 4 more.
		ui.Row(c).Gap(12).Padding(6, 16).Background(ui.Hex("#f9fafb")).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			quickColumns(c, theme.TextMuted, locales.HostColName(), locales.HostColHost(), locales.HostColUser())
		})
		st.list.Selected = &st.row
		st.list.Key = func(i int) any {
			if rows[i].host == nil {
				return rows[i].header
			}
			return rows[i].host.ID
		}
		st.list.Label = func(i int) string {
			if rows[i].host == nil {
				return rows[i].header.String()
			}
			return rows[i].host.Title()
		}
		st.list.Header = func(i int) bool { return rows[i].host == nil }
		list := ui.List(c, &st.list, len(rows), func(i int) {
			h := rows[i].host
			if h == nil {
				ui.Text(c, rows[i].header.String()).FontSize(12).Bold().TextColor(theme.TextMuted).Padding(8, 12, 4, 12).Background(theme.Background)
				return
			}
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
			// Only a search lists nothing: with no hosts, the page is
			// startEmpty.
			if len(rows) == 0 {
				widgets.Muted(c, locales.NoMatchingHosts()).Padding(12, 10)
			}
		})
		if list.Submitted() && st.row >= 0 && st.row < len(rows) && rows[st.row].host != nil {
			connect = rows[st.row].host
		}
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, locales.ClearRecents()).Disabled(len(a.hosts.Recent(1)) == 0).Clicked() {
			a.clearRecents()
		}
		widgets.Muted(c, locales.WelcomeHint(a.shortcutLabel(scManager))).FontSize(12).Grow(1).TextAlign(ui.End)
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
