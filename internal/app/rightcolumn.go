package app

import (
	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The right column is a column on the terminal's right, full height,
// that holds the bottom panel when it is moved there (bottomPanelSide
// "right"), and is meant to hold the server monitor too (#152): the
// monitor on top, the panel below. Its width, rightPanelWidth, is kept
// from the window's right edge: a wider window gives the room to the
// terminal.
//
// Hidden on the right (panelhide.go), the column goes away and a thin
// strip at the terminal's right edge keeps the button that shows it.

// defaultRightWidth is the right column's width, in DIPs, when none is
// saved, and where a double click on its divider puts it back.
const defaultRightWidth = 420

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
//
// The column's width is fixed from the right edge: ui.Split sizes its
// first pane, so the split is laid out in the other direction, where its
// first pane is the end one, and the panes are put back in the window's
// own direction.
func (a *App) withRightColumn(c *ui.Context, main, top, bottom func()) {
	if top == nil && bottom == nil {
		main()
		return
	}
	dir := c.Direction()
	flipped := ui.RightToLeft
	if dir == ui.RightToLeft {
		flipped = ui.LeftToRight
	}
	pane := func(build func()) func() {
		return func() { ui.Column(c).Direction(dir).Fill().Children(build) }
	}
	column := pane(func() { a.rightColumn(c, top, bottom) })
	ui.Row(c).Direction(flipped).Fill().Children(func() {
		split := ui.Split(c, &a.rightWidth, column, pane(main)).SplitReset(defaultRightWidth).Fill()
		a.splitMoved(c, split, &a.rightWidth, rightLimits)
	})
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

// movePanelLabel is the context menu's item that moves the panel to the
// other side.
func (a *App) movePanelLabel() string {
	if a.panelOnRight() {
		return locales.MovePanelBottom()
	}
	return locales.MovePanelRight()
}
