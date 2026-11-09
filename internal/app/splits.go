package app

import (
	"time"

	"finallyshell/internal/config"

	"github.com/egoist/mygo/ui"
)

// The pane sizes when none are saved, and where a double click on a
// divider puts it back.
const (
	defaultSideWidth   = 240 // DIPs
	defaultMonitorPct  = 62
	defaultTerminalPct = 64
	defaultTreeWidth   = 200 // DIPs
	// The monitor's share of the right column above the bottom panel,
	// when both are there (#152).
	defaultRightMonitorPct = 45
)

// splitLimits are the smallest sizes, in DIPs, a divider's drag leaves to
// the pane before it (first) and the one after it (second). MyGo's own
// limit, Space(10), is 30 DIPs with this app's theme: too little for a
// sidebar or a terminal to show anything.
type splitLimits struct {
	first, second float32
	vertical      bool // the panes are stacked, the size their height
	percent       bool // the size is first's percentage of the room
}

var (
	// The sidebar keeps the monitor's tables readable; the main area keeps
	// the file table's columns.
	sidebarLimits = splitLimits{first: 180, second: 420}
	// The monitor keeps its IP line; the session list its title and a row.
	monitorLimits = splitLimits{first: 60, second: 90, vertical: true, percent: true}
	// The terminal keeps a few rows above its command bar; the bottom
	// panel its tabs, path bar and a row of the table.
	terminalLimits = splitLimits{first: 140, second: 120, vertical: true, percent: true}
	// The tree keeps a short folder name; the table its name column.
	treeLimits = splitLimits{first: 100, second: 240}
)

// maxSavedWidth is the widest a saved sidebar, tree or right column may
// be: more is not a size a divider was dragged to, and is ignored.
const maxSavedWidth = 1600

// layoutSaveDelay is how long the dividers rest before their sizes are
// saved, so that a drag writes the settings once, not at each move.
const layoutSaveDelay = 400 * time.Millisecond

// restoreLayout takes the pane sizes saved in the settings, keeping the
// defaults for those missing or out of range.
func (a *App) restoreLayout() {
	s := a.settings
	if s.SidebarWidth >= sidebarLimits.first && s.SidebarWidth <= maxSavedWidth {
		a.sideWidth = s.SidebarWidth
	}
	if s.TreeWidth >= treeLimits.first && s.TreeWidth <= maxSavedWidth {
		a.treeWidth = s.TreeWidth
	}
	if s.MonitorPercent > 0 && s.MonitorPercent < 100 {
		a.monitorPct = s.MonitorPercent
	}
	if s.TerminalPercent > 0 && s.TerminalPercent < 100 {
		a.terminalPct = s.TerminalPercent
	}
	a.bottomHidden = s.BottomPanelHidden
	if s.BottomPanelSide == config.PanelRight {
		a.bottomSide = config.PanelRight
	}
	if s.RightPanelWidth >= rightLimits.first && s.RightPanelWidth <= maxSavedWidth {
		a.rightWidth = s.RightPanelWidth
	}
	// The Processes and Network tabs' sizes, which the sessions share.
	if s.ProcListPercent > 0 && s.ProcListPercent < 100 {
		a.processes.listPct = s.ProcListPercent
	}
	if s.ProcFieldsPercent > 0 && s.ProcFieldsPercent < 100 {
		a.processes.fieldsPct = s.ProcFieldsPercent
	}
	if s.NetListPercent > 0 && s.NetListPercent < 100 {
		a.network.listPct = s.NetListPercent
	}
}

// splitMoved keeps the panes of split at least l's sizes when the user
// moved its divider, and saves the sizes once it rests. Only a move is
// clamped: a window too small for the size the user chose leaves it to
// MyGo, which gives the size back when there is room again.
func (a *App) splitMoved(c *ui.Context, split ui.Element, size *float32, l splitLimits) {
	if !split.Changed() {
		return
	}
	b := split.Bounds()
	room := b.W - 1 // less the divider's line
	if l.vertical {
		room = b.H - 1
	}
	if room > 0 {
		v := *size
		if l.percent {
			v = v / 100 * room
		}
		// With no room for both, the first pane keeps its size.
		// A size within the limits stays as it is, not converted back
		// and forth: a double click saves the default itself.
		if clamped := max(min(v, room-l.second), l.first); clamped != v {
			if l.percent {
				clamped = clamped * 100 / room
			}
			*size = clamped
		}
	}
	a.layoutDirty, a.layoutAt = true, c.Now()
	c.After(layoutSaveDelay)
}

// saveLayoutAtRest saves the pane sizes once the dividers stopped moving,
// asking for the frame that does while they wait: a frame asks only for
// the next one it needs.
func (a *App) saveLayoutAtRest(c *ui.Context) {
	if !a.layoutDirty {
		return
	}
	if wait := layoutSaveDelay - c.Now().Sub(a.layoutAt); wait > 0 {
		c.After(wait)
		return
	}
	a.saveLayout()
}

func (a *App) saveLayout() {
	a.layoutDirty = false
	a.settings.SidebarWidth = a.sideWidth
	a.settings.MonitorPercent = a.monitorPct
	a.settings.TerminalPercent = a.terminalPct
	a.settings.BottomPanelHidden = a.bottomHidden
	a.settings.BottomPanelSide = a.bottomSide
	a.settings.RightPanelWidth = a.rightWidth
	a.settings.TreeWidth = a.treeWidth
	a.settings.ProcListPercent = a.processes.listPct
	a.settings.ProcFieldsPercent = a.processes.fieldsPct
	a.settings.NetListPercent = a.network.listPct
	a.saveSettings(a.settings)
}
