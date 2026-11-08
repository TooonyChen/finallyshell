package app

import (
	"runtime"
	"strings"

	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// commandBarPage is the Terminal › Command bar page: the bar itself, each
// of its popups, reading the server's history, and clearing the history
// the app keeps. Each switch applies at once, to every session.
func (a *App) commandBarPage(c *ui.Context) {
	on := a.commandBarOn()
	ui.Column(c).Gap(8).Children(func() {
		show := on
		if ui.Checkbox(c, &show, locales.OptionShowCommandBar()).Changed() {
			a.changeSettings(func() { a.settings.NoCommandBar = !show })
		}
		widgets.Muted(c, locales.OptionShowCommandBarNote()).FontSize(12).PaddingStart(18)
	})

	// The keys, here rather than in the bar, which shows "Command" alone.
	ui.Column(c).Gap(6).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.CmdBarKeys()).Bold()
		for _, k := range a.commandBarKeys(runtime.GOOS) {
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, k.keys).Width(110).Shrink(0)
				widgets.Muted(c, k.does).Grow(1).MinWidth(0)
			})
		}
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.CmdBarPopups()).Bold()
		for _, sw := range []struct {
			label, note string
			off         *bool
		}{
			{locales.OptionCmdBarPaths(), locales.OptionCmdBarPathsNote(), &a.settings.NoCommandBarPaths},
			{locales.OptionCmdBarHistory(), locales.OptionCmdBarHistoryNote(), &a.settings.NoCommandBarHistory},
			{locales.OptionCmdBarCommands(), locales.OptionCmdBarCommandsNote(), &a.settings.NoCommandBarCommands},
		} {
			checked := !*sw.off
			if ui.Checkbox(c, &checked, sw.label).Disabled(!on).Changed() {
				a.changeSettings(func() { *sw.off = !checked })
			}
			widgets.Muted(c, sw.note).FontSize(12).PaddingStart(18)
		}
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.CmdBarHistory()).Bold()
		server := !a.settings.NoServerHistory
		if ui.Checkbox(c, &server, locales.OptionServerHistory()).Disabled(!on || a.settings.NoCommandBarHistory).Changed() {
			a.changeSettings(func() { a.settings.NoServerHistory = !server })
		}
		widgets.Muted(c, locales.OptionServerHistoryNote()).FontSize(12).PaddingStart(18)
		ui.Row(c).Padding(4, 0, 0, 0).Children(func() {
			if ui.Button(c, locales.ClearAllHistory()).Clicked() {
				a.askClearAllHistory()
			}
		})
		widgets.Muted(c, locales.ClearAllHistoryNote()).FontSize(12)
	})
}

// commandBarKey is a line of the page's Keys: keys, and what they do.
type commandBarKey struct{ keys, does string }

// commandBarKeys are the keys of the bar on goos, as the Keys section
// lists them, with the Focus command bar shortcut as it is bound.
func (a *App) commandBarKeys(goos string) []commandBarKey {
	label := func(ks ...keyCombo) string {
		var s []string
		for _, k := range ks {
			s = append(s, k.label(goos, false))
		}
		return strings.Join(s, " / ")
	}
	return []commandBarKey{
		{label(keyCombo{0, ui.KeyEnter}), locales.CmdBarKeyEnter()},
		{label(keyCombo{0, ui.KeyTab}), locales.CmdBarKeyTab()},
		{label(keyCombo{0, ui.KeyUp}, historyKey), locales.CmdBarKeyHistory()},
		{label(keyCombo{0, ui.KeyLeft}, keyCombo{0, ui.KeyRight}), locales.CmdBarKeyFields()},
		{label(keyCombo{0, ui.KeyEscape}), locales.CmdBarKeyEsc()},
		{a.shortcutLabel(scFocusBar), locales.CmdBarKeyFocus()},
	}
}

// askClearAllHistory asks, then deletes the history the app keeps of
// every host.
func (a *App) askClearAllHistory() {
	q := Confirmation{
		Title:   locales.AppTitle(),
		Message: locales.ClearAllHistoryTitle(),
		Detail:  locales.ClearAllHistoryDetail(),
		OK:      locales.ClearAllHistoryYes(),
		Cancel:  locales.Cancel(),
	}
	go func() {
		if a.plat.Confirm(q) {
			a.post(a.clearAllHistory)
		}
	}()
}
