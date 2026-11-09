package app

import (
	"slices"

	"finallyshell/internal/config"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The parts of the server monitor that can be hidden, by their key in
// the monitorHidden setting. The IP line and the System info button
// always show.
const (
	partUptime    = "uptime"    // the uptime and load lines
	partCPU       = "cpu"       // the CPU, memory and swap bars
	partProcesses = "processes" // the top-processes table
	partNetwork   = "network"   // the speeds row and its graph
	partLatency   = "latency"   // the latency row and its graph
	partDisks     = "disks"     // the disk table
)

// monitorParts are the parts in the order the panel and its menu show
// them, with their names in the menu.
var monitorParts = []monitorPart{
	{partUptime, locales.MonitorPartUptime},
	{partCPU, locales.MonitorPartCpu},
	{partProcesses, locales.MonitorPartProcesses},
	{partNetwork, locales.MonitorPartNetwork},
	{partLatency, locales.MonitorPartLatency},
	{partDisks, locales.MonitorPartDisks},
}

type monitorPart struct {
	key   string
	label func() string
}

// hiddenParts is the set of parts the monitor leaves out, as the
// monitorHidden setting lists them. Keys it does not know, as a later
// version's, are kept but mean nothing.
type hiddenParts struct{ config.Names }

// shows says whether the part key shows.
func (h hiddenParts) shows(key string) bool { return !h.Has(key) }

// none says whether every part is hidden.
func (h hiddenParts) none() bool {
	return !slices.ContainsFunc(monitorParts, func(p monitorPart) bool { return h.shows(p.key) })
}

// monitorHidden is the parts the user hid.
func (a *App) monitorHidden() hiddenParts { return hiddenParts{a.settings.MonitorHidden} }

// monitorMenu is the monitor panel's context menu: a check box per
// part. Items about the panel itself, such as which side it is on, go
// here above the parts, then m.Separator().
func (a *App) monitorMenu(m *ui.Menu) {
	a.monitorPartItems(m)
}

// monitorPartItems are the check boxes of the parts; choosing one shows
// or hides its part, for every host, and saves it.
func (a *App) monitorPartItems(m *ui.Menu) {
	hidden := a.monitorHidden()
	for _, p := range monitorParts {
		if m.Item(p.label()).ID("part:" + p.key).Checked(hidden.shows(p.key)).Chosen() {
			a.changeSettings(func() { a.settings.MonitorHidden = hidden.Toggle(p.key) })
		}
	}
}

// monitorHint is what the panel says under the IP when every part is
// hidden, so that the menu can be found again.
func monitorHint(c *ui.Context) {
	widgets.Muted(c, locales.MonitorPartsHint()).Margin(4, 0, 0, 0)
}
