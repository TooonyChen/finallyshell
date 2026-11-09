package app

import (
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The bottom panel under the terminal can collapse to its row of tabs,
// which stays at the bottom of the main area, and the terminal takes the
// rest of the height. A click on a tab's label opens the panel on that
// tab, at the height it had (terminalPct stays as it was); so do the
// button at the right end of the row, the row's context menu and the
// shortcut (scBottomPanel), which also collapse it.
//
// On the right of the terminal (rightcolumn.go), hiding the panel takes
// its column away, and a strip at the terminal's right edge keeps the
// button that shows it; under the monitor there, it collapses to its row
// of tabs at the column's bottom.
//
// The state is the settings' BottomPanelHidden, saved with the pane
// sizes. A panel row (paneltab.go) is the panel itself and ignores it, as
// full screen, which shows the terminal alone, does.

// setBottomPanelHidden collapses the bottom panel, or opens it, and
// saves the choice.
func (a *App) setBottomPanelHidden(hidden bool) {
	if a.bottomHidden == hidden {
		return
	}
	a.bottomHidden = hidden
	a.saveLayout()
}

// toggleBottomPanel opens the collapsed bottom panel, or collapses it.
func (a *App) toggleBottomPanel() { a.setBottomPanelHidden(!a.bottomHidden) }

// openBottomTab shows tab in the bottom panel of the selected row: a
// panel row's own, else the one under the terminal, which opens if it
// was collapsed.
func (a *App) openBottomTab(tab int) {
	*a.shownBottomTab() = tab
	if !a.current().isPanelTab() {
		a.setBottomPanelHidden(false)
	}
}

// collapsedMainArea is the main area while the bottom panel is collapsed:
// the terminal over the panel's row of tabs.
func (a *App) collapsedMainArea(c *ui.Context) {
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).FillWidth().Children(func() { a.terminalArea(c) })
		a.panelTabRow(c, a.bottomTabs(), &a.bottomTab, true)
	})
}

// panelTabRow is the bottom panel's row of tabs, on the tab *tab, with
// its buttons at the right end: over the panel's page, or, collapsed,
// alone under the terminal, where it shows no tab as chosen and a click
// on one opens the panel on it. Under the terminal it has the button
// that collapses and opens the panel, and the context menu
// (bottomPanelMenu); a panel row's has neither.
func (a *App) panelTabRow(c *ui.Context, tabs []bottomTab, tab *int, collapsed bool) {
	underTerminal := !a.current().isPanelTab()
	row := ui.Row(c).PaddingX(6).AlignItems(ui.Center).BorderColor(colors.line)
	if collapsed {
		row.BorderWidth(1, 0, 0, 0)
	} else {
		row.BorderWidth(0, 0, 1, 0)
	}
	if underTerminal {
		row.ContextMenu(a.bottomPanelMenu)
	}
	row.Children(func() {
		labels := make([]string, len(tabs))
		for i, t := range tabs {
			labels[i] = t.label
		}
		// The tabs scroll sideways where they do not fit, as in the
		// right column, so that the buttons stay in view; the wheel
		// scrolls them too (tabsWheel).
		ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Scrollbars(false).
			TrackScroll(&a.panelTabsScroll).HandleInput(a.tabsWheel).Children(func() {
			if collapsed {
				a.collapsedTabs(c, labels, tab)
			} else {
				ui.Tabs(c, tab, labels...)
			}
		})
		if underTerminal {
			a.bottomPanelButton(c)
		}
		a.panelCornerButton(c, *tab)
	})
}

// collapsedTabs are the tabs of the collapsed bottom panel: ui.Tabs's
// look with none chosen, as no page shows, and a click on one, the tab
// of *tab's included, which ui.Tabs would not report, opens the panel on
// it.
func (a *App) collapsedTabs(c *ui.Context, labels []string, tab *int) {
	t := c.Theme()
	chosen := *tab
	tabs := ui.TabsBase(c, &chosen, len(labels))
	tabs.List.Gap(t.Space(1)).Children(func() {
		for i, label := range labels {
			el := tabs.Tab(i).Padding(t.Space(2), t.Space(3)).FocusRing(false).TextColor(t.TextMuted)
			if el.Hovered() {
				el.TextColor(t.Text)
			}
			el.Children(func() { ui.Text(c, label).SingleLine() })
			if el.Clicked() {
				chosen = i
			}
		}
	})
	if chosen != *tab || tabs.List.Changed() {
		*tab = chosen
		a.setBottomPanelHidden(false)
	}
}

// bottomPanelButton is the button that collapses the bottom panel, a
// chevron down, or opens it, a chevron up; alone on the right, where it
// hides beside the terminal, the chevrons point right and left.
func (a *App) bottomPanelButton(c *ui.Context) {
	hide, show := "chevron-down", "chevron-up"
	if a.panelOnRight() && !a.monitorOnRight() {
		hide, show = "chevron-right", "chevron-left"
	}
	icon, label := hide, locales.HidePanel()
	if a.bottomHidden {
		icon, label = show, locales.ShowPanel()
	}
	if widgets.IconButton(c, icon, label).Clicked() {
		a.toggleBottomPanel()
	}
}

// bottomPanelMenu is the context menu of the bottom panel's row of tabs
// under the terminal: where the panel goes, and whether it shows.
func (a *App) bottomPanelMenu(m *ui.Menu) {
	label := locales.HidePanel()
	if a.bottomHidden {
		label = locales.ShowPanel()
	}
	if m.Item(label).Chosen() {
		a.toggleBottomPanel()
	}
	m.Separator()
	if m.Item(a.movePanelLabel()).Chosen() {
		a.setPanelOnRight(!a.panelOnRight())
	}
}

// tabsWheel has the vertical wheel, or a vertical swipe of a touchpad,
// over the panel's tabs scroll them sideways while they do not fit: down
// toward the last tab. Sideways scrolling, and any while they fit, goes on
// to the container as it would without it.
func (a *App) tabsWheel(ev ui.InputEvent) bool {
	s := &a.panelTabsScroll
	if ev.Kind != ui.InputScroll || ev.DX != 0 || ev.DY == 0 || s.MaxX <= 0 || ev.Mods&ui.Ctrl != 0 {
		return false
	}
	s.X = min(max(s.X+ev.DY, 0), s.MaxX)
	return true
}
