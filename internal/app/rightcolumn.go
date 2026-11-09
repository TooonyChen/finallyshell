package app

import (
	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The right column is a column on the terminal's right, full height,
// that holds the bottom panel when it is moved there (bottomPanelSide
// "right"), the server monitor when it is (monitorSide "right"), or
// both: the monitor on top, the panel below, divided at
// rightMonitorPercent. Its width, rightPanelWidth, is kept from the
// window's right edge: a wider window gives the room to the terminal.
//
// Hidden on the right (panelhide.go), the panel goes away. Alone in the
// column, it takes the column with it, and a thin strip at the
// terminal's right edge keeps the button that shows it; under the
// monitor, it collapses to its row of tabs at the column's bottom, as
// under the terminal, and the monitor takes the height.
//
// With the monitor on the right, the session panel fills the left
// column. A panel row's panel has the monitor beside it, still
// following the current session; full screen shows neither.

// defaultRightWidth is the right column's width, in DIPs, when none is
// saved, and where a double click on its divider puts it back.
const defaultRightWidth = 460

// rightLimits keep the right column wide enough for the panes inside
// the panel to keep their own least sizes, the widest being the Processes
// details' fields beside the environment (procFieldsLimits, 200 DIPs
// each, and the divider), and the terminal a few words of a line.
var rightLimits = splitLimits{first: 410, second: 320}

// rightMonitorLimits keep the monitor its IP line above the panel, and
// the panel its tabs, path bar and a row of the table.
var rightMonitorLimits = splitLimits{first: 60, second: 120, vertical: true, percent: true}

// panelOnRight reports whether the bottom panel is in the right column.
func (a *App) panelOnRight() bool { return a.bottomSide == config.PanelRight }

// setPanelOnRight moves the bottom panel to the right column, or back
// under the terminal, and saves the choice. Whether it is hidden stays as
// it was.
func (a *App) setPanelOnRight(right bool) {
	side := ""
	if right {
		side = config.PanelRight
	}
	if a.bottomSide == side {
		return
	}
	a.bottomSide = side
	a.saveLayout()
}

// withRightColumn lays out main, the terminal, with the right column on
// its right holding top and bottom, either of which may be nil: with
// both, top is above bottom, divided at rightMonitorPct; with one, it
// fills the column; with neither, main takes the whole width.
func (a *App) withRightColumn(c *ui.Context, main, top, bottom func()) {
	if top == nil && bottom == nil {
		main()
		return
	}
	column := func() { a.rightColumn(c, top, bottom) }
	a.endSplit(c, &a.rightWidth, defaultRightWidth, rightLimits, main, column).Fill()
}

// rightColumn is the right column's content: top over bottom, or the one
// of them that is there.
func (a *App) rightColumn(c *ui.Context, top, bottom func()) {
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		switch {
		case top != nil && bottom != nil:
			split := ui.SplitVerticalPercent(c, &a.rightMonitorPct, top, bottom).SplitReset(defaultRightMonitorPct).Fill()
			a.splitMoved(c, split, &a.rightMonitorPct, rightMonitorLimits)
		case top != nil:
			top()
		default:
			bottom()
		}
	})
}

// hiddenPanelStrip is the thin strip at the terminal's right edge while
// the panel on the right is hidden: the button that shows it, and the
// panel's context menu.
func (a *App) hiddenPanelStrip(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).FillHeight().Shrink(0).PaddingY(t.Space(1)).PaddingX(2).AlignItems(ui.Center).
		Background(theme.Background).BorderColor(colors.line).BorderWidth(0, 0, 0, 1).
		ContextMenu(a.bottomPanelMenu).Children(func() {
		a.bottomPanelButton(c)
	})
}

// hiddenRightMainArea is the main area while the panel on the right is
// hidden: the terminal, and the strip on its right.
func (a *App) hiddenRightMainArea(c *ui.Context) {
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(theme.Background).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() { a.terminalArea(c) })
		a.hiddenPanelStrip(c)
	})
}

// monitorOnRight reports whether the server monitor is in the right
// column.
func (a *App) monitorOnRight() bool { return a.monitorSide == config.PanelRight }

// setMonitorOnRight moves the server monitor to the right column, or
// back over the session list, and saves the choice.
func (a *App) setMonitorOnRight(right bool) {
	side := ""
	if right {
		side = config.PanelRight
	}
	if a.monitorSide == side {
		return
	}
	a.monitorSide = side
	a.saveLayout()
}

// rightMonitor is the monitor in the right column, on the left column's
// color, as it is there.
func (a *App) rightMonitor(c *ui.Context) {
	ui.Column(c).Fill().Background(colors.sidebar).Children(func() { a.monitorPanel(c) })
}

// monitorOverTabs is the right column with the monitor and the hidden
// panel: the monitor over the panel's row of tabs.
func (a *App) monitorOverTabs(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).FillWidth().Children(func() { a.rightMonitor(c) })
		a.panelTabRow(c, a.bottomTabs(), &a.bottomTab, true)
	})
}

// moveMonitorLabel is the monitor's context menu's item that moves it to
// the other side.
func (a *App) moveMonitorLabel() string {
	if a.monitorOnRight() {
		return locales.MoveMonitorLeft()
	}
	return locales.MoveMonitorRight()
}

// movePanelLabel is the context menu's item that moves the panel to the
// other side.
func (a *App) movePanelLabel() string {
	if a.panelOnRight() {
		return locales.MovePanelBottom()
	}
	return locales.MovePanelRight()
}
