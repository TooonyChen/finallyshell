package app

import (
	"runtime"

	"finalshelly/internal/config"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The lines a notch of the mouse wheel scrolls a terminal, as the Mouse
// page offers them; the terminal's own is 3.
const (
	defaultWheelLines = 3
	maxWheelLines     = 20
)

// mouseGOOS is the system whose keys the Mouse page names; tests set it.
var mouseGOOS = runtime.GOOS

// rightClicks are the choices of what a right click does, in the order the
// Mouse page lists them.
var rightClicks = []struct {
	value  config.RightClick
	action terminal.RightClickAction
	label  func() string
}{
	{config.RightClickMenu, terminal.RightClickContextMenu, locales.RightClickMenu},
	{config.RightClickPaste, terminal.RightClickPaste, locales.RightClickPaste},
	{config.RightClickCopy, terminal.RightClickCopy, locales.RightClickCopy},
	{config.RightClickCopyOrPaste, terminal.RightClickCopyOrPaste, locales.RightClickCopyOrPaste},
	{config.RightClickIgnore, terminal.RightClickIgnore, locales.RightClickIgnore},
}

// rightClickAction is the terminal's action for r: the context menu for a
// value it does not know, as a newer version may have written.
func rightClickAction(r config.RightClick) terminal.RightClickAction {
	for _, c := range rightClicks {
		if c.value == r {
			return c.action
		}
	}
	return terminal.RightClickContextMenu
}

// wheelLines is how many lines a notch of the wheel scrolls, as the
// settings say, kept in the range the Mouse page offers.
func wheelLines(s config.Settings) int {
	if s.WheelLines <= 0 {
		return defaultWheelLines
	}
	return min(s.WheelLines, maxWheelLines)
}

func copyOnSelect(s config.Settings) terminal.CopyOnSelect {
	if s.CopyOnSelect {
		return terminal.CopyOnSelectClipboard
	}
	return terminal.CopyOnSelectNone
}

func scrollMultiplier(s config.Settings) terminal.ScrollMultiplier {
	return terminal.ScrollMultiplier{Discrete: float32(wheelLines(s))}
}

// mouseOptions sets the options of a new terminal that the Mouse page
// sets.
func (a *App) mouseOptions(o *terminal.Options) {
	s := a.settings
	o.CopyOnSelect = copyOnSelect(s)
	o.ScrollMultiplier = scrollMultiplier(s)
	o.RightClick = rightClickAction(s.RightClick)
	if s.NoLinkClick {
		// Every link taken, and none opened: the terminal has no option
		// that turns Ctrl+click off, and opens what OpenLink leaves.
		o.OpenLink = func(string) bool { return true }
	}
}

// applyMouse gives an open terminal the Mouse page's settings that it can
// change as it runs: copy on select and the wheel. A right click and
// Ctrl+click keep what they did until the session connects again, as the
// terminal takes them only as it starts.
func (a *App) applyMouse(t *terminal.Terminal) {
	t.SetCopyOnSelect(copyOnSelect(a.settings))
	t.SetScrollMultiplier(scrollMultiplier(a.settings))
}

// changeTerminalSettings changes the settings with fn, as changeSettings
// does, and gives the open terminals the change.
func (a *App) changeTerminalSettings(fn func()) {
	a.changeSettings(fn)
	a.applyTerminalSettings()
}

// mousePage is the Mouse page of the terminal's options: selecting, the
// right click, links and the wheel.
func (a *App) mousePage(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.MouseSelection()).Bold()
		on := a.settings.CopyOnSelect
		if ui.Checkbox(c, &on, locales.OptionCopyOnSelect()).Changed() {
			a.changeTerminalSettings(func() { a.settings.CopyOnSelect = on })
		}
		widgets.Muted(c, locales.OptionCopyOnSelectNote()).FontSize(12).PaddingStart(18)
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.MouseRightClick()).Bold()
		labels := make([]string, len(rightClicks))
		chosen := rightClicks[0].label()
		for i, rc := range rightClicks {
			labels[i] = rc.label()
			if rc.value == a.settings.RightClick {
				chosen = labels[i]
			}
		}
		if ui.Select(c, &chosen, labels).Width(320).Label(locales.MouseRightClick()).Changed() {
			for i, rc := range rightClicks {
				if labels[i] == chosen {
					a.changeTerminalSettings(func() { a.settings.RightClick = rc.value })
				}
			}
		}
		widgets.Muted(c, locales.MouseNextConnect()).FontSize(12)
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.MouseLinks()).Bold()
		key := "Ctrl"
		if mouseGOOS == "darwin" {
			key = "Cmd"
		}
		on := !a.settings.NoLinkClick
		if ui.Checkbox(c, &on, locales.OptionOpenLinks(key)).Changed() {
			a.changeTerminalSettings(func() { a.settings.NoLinkClick = !on })
		}
		widgets.Muted(c, locales.OptionOpenLinksNote()).FontSize(12).PaddingStart(18)
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.MouseScrolling()).Bold()
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.OptionWheelLines())
			lines := float64(wheelLines(a.settings))
			if ui.NumberInput(c, &lines, 1, maxWheelLines, 1).Width(90).Label(locales.OptionWheelLines()).Changed() {
				a.changeTerminalSettings(func() {
					a.settings.WheelLines = int(lines)
					if a.settings.WheelLines == defaultWheelLines {
						a.settings.WheelLines = 0 // the default, written as nothing
					}
				})
			}
		})
		if mouseGOOS == "windows" {
			widgets.Muted(c, locales.OptionWheelLinesWindows()).FontSize(12)
		}
	})
}
