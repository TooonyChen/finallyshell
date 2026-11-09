package app

import (
	"slices"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// A panel row shows a session's bottom panel (Files, Transfers, Tunnels,
// Processes, Network) in the whole main area, without the terminal, as
// FinalShell opens a panel in a tab of its own. The button at the right
// of the bottom panel's tabs opens it, right below its session in the
// list, indented, with the icon and the name of the tab it shows.
//
// It is a second view of the session, not a second connection: a Session
// without a Host whose panelOf is the session, whose connection, files,
// tunnels, processes and polls it reads (connSession), with a tab of the
// panel of its own (panelTab). A session has one at most; it moves with
// its session, closes with it, and stays as the session reconnects. It
// is no session of its own: it is not counted, connects nothing, and
// closing it ends nothing.
//
// As it has no Host, isNewTab holds for it too: isPanelTab tells it from
// a new tab.

// panelIndent is how far a panel row's content is indented from its
// session's.
const panelIndent = 16

// isPanelTab tells whether s is a panel row; false for nil.
func (s *Session) isPanelTab() bool { return s != nil && s.panelOf != nil }

// connSession is the session whose connection the selected row shows:
// a panel row's session, else the selected row itself, nil for none.
// The bottom panel's pages and the side panel's monitor show it.
func (a *App) connSession() *Session {
	s := a.current()
	if s.isPanelTab() {
		return s.panelOf
	}
	return s
}

// shownBottomTab is the tab of the bottom panel the selected row shows:
// a panel row's own, else the one of the sessions' rows.
func (a *App) shownBottomTab() *int {
	if s := a.current(); s.isPanelTab() {
		return &s.panelTab
	}
	return &a.bottomTab
}

// panelRowOf is s's panel row, nil for none.
func (a *App) panelRowOf(s *Session) *Session {
	for _, p := range a.sessions {
		if p.panelOf == s {
			return p
		}
	}
	return nil
}

// openPanelTab selects s's panel row, adding it right below s first,
// showing tab, when s has none.
func (a *App) openPanelTab(s *Session, tab int) {
	p := a.panelRowOf(s)
	if p == nil {
		i := slices.Index(a.sessions, s)
		if i < 0 {
			return
		}
		// Closed, as a new tab is: quitting ends nothing.
		p = &Session{ID: nextSessionID.Add(1), status: statusClosed, start: startState{row: -1}, panelOf: s, panelTab: tab}
		a.sessions = slices.Insert(a.sessions, i+1, p)
	}
	a.active = slices.Index(a.sessions, p)
}

// backToTerminal selects the session of the panel row p.
func (a *App) backToTerminal(p *Session) {
	if i := slices.Index(a.sessions, p.panelOf); i >= 0 {
		a.active = i
	}
}

// panelArea is the main area of the panel row p: the bottom panel alone,
// on p's tab.
func (a *App) panelArea(c *ui.Context, p *Session) { a.bottomPanel(c, &p.panelTab) }

// panelCornerButton is the button at the right end of the bottom panel's
// tabs: on a session's row it opens the panel row, on tab, and on a
// panel row it goes back to the session's terminal. A new tab has none.
func (a *App) panelCornerButton(c *ui.Context, tab int) {
	s := a.current()
	switch {
	case s.isPanelTab():
		if widgets.IconButton(c, "minimize-2", locales.BackToTerminal()).Clicked() {
			a.backToTerminal(s)
		}
	case s == nil || s.isNewTab():
	default:
		if widgets.IconButton(c, "maximize-2", locales.OpenPanelTab()).Clicked() {
			a.openPanelTab(s, tab)
		}
	}
}

// panelTabIcons and panelTabNames are the icon and the name of each tab
// of the bottom panel, by its constant, as a panel row shows it.
var (
	panelTabIcons = [...]string{tabFiles: "folder", tabTransfers: "arrow-down-up", tabTunnels: "cable", tabProcesses: "cpu", tabNetwork: "network"}
	panelTabNames = [...]func() string{tabFiles: locales.TabFiles, tabTransfers: locales.TabTransfers, tabTunnels: locales.TabTunnels, tabProcesses: locales.TabProcesses, tabNetwork: locales.TabNetwork}
)

// panelTabName is the name of the panel row p: its tab's.
func panelTabName(p *Session) string {
	if p.panelTab < 0 || p.panelTab >= len(panelTabNames) {
		return ""
	}
	return panelTabNames[p.panelTab]()
}

// panelLabel is the panel row p where only text fits, as its tooltip and
// the text the list finds as the user types: "server1 | Network".
func panelLabel(p *Session) string {
	return locales.PanelTabLabel(p.panelOf.label(), panelTabName(p))
}

// listLabel is the text of the row s in the session list.
func listLabel(s *Session) string {
	if s.isPanelTab() {
		return panelLabel(s)
	}
	return s.label()
}

// panelRow is the row of the panel row p in the session list: indented
// under its session's, one line with its tab's icon and name. It reports
// whether the user closed it.
func (a *App) panelRow(c *ui.Context, p *Session, active bool) (closed bool) {
	row := ui.Row(c).Gap(8).Padding(5, 4, 5, 8+panelIndent).Radius(5).AlignItems(ui.Center).Tooltip(panelLabel(p))
	hovered := row.Hovered()
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.BackToTerminal()).Chosen() {
			a.backToTerminal(p)
		}
		m.Separator()
		if m.Item(locales.Close()).Danger().Chosen() {
			closed = true
		}
	})
	row.Children(func() {
		icon := "app-window"
		if p.panelTab >= 0 && p.panelTab < len(panelTabIcons) {
			icon = panelTabIcons[p.panelTab]
		}
		ui.Icon(c, icons.Get(icon)).FontSize(14).Shrink(0)
		name := ui.Text(c, panelTabName(p)).SingleLine().Grow(1).MinWidth(0)
		if active {
			name.Bold()
		}
		closeBtn := widgets.IconButton(c, "x", locales.Close()).Size(20, 20)
		if !hovered && !active {
			closeBtn.Invisible()
		}
		if closeBtn.Clicked() {
			closed = true
		}
	})
	return closed
}
