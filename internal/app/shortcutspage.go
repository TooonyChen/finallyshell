package app

import (
	"runtime"

	"finalshelly/internal/config"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// shortcutsPage is the state of Options › Terminal › Shortcuts: the row
// chosen, and the dialog that takes a new key for it.
type shortcutsPage struct {
	list ui.ListState
	row  int
	// capturing shows the dialog for the action of capture.
	capturing bool
	capture   keyCapture
}

// keyCapture is what the dialog took: the key pressed last, for the
// action id.
type keyCapture struct {
	id    shortcutID
	key   keyCombo
	taken bool // a key was pressed
}

// optionAsAltShown tells whether the page has Option as Alt, which only
// macOS has.
var optionAsAltShown = runtime.GOOS == "darwin"

// Colors of what the dialog says of a key.
var (
	problemText = ui.Hex("#b91c1c")
	warningText = ui.Hex("#a16207")
)

// shortcutsPageView is the Shortcuts page: the actions and their keys,
// as FinalShell's, with Change, Reset and Reset all, and Option as Alt on
// macOS.
func (a *App) shortcutsPageView(c *ui.Context) {
	p := &a.shortcutsUI
	goos := runtime.GOOS
	if optionAsAltShown {
		ui.Column(c).Gap(4).Padding(0, 0, 6, 0).Children(func() {
			on := a.settings.OptionAsAlt
			if ui.Checkbox(c, &on, locales.OptionAsAlt()).Changed() {
				a.changeSettings(func() { a.settings.OptionAsAlt = on })
			}
			widgets.Muted(c, locales.OptionAsAltNote()).FontSize(12).PaddingStart(18)
		})
	}
	cols := []ui.TableColumn{
		{Title: locales.ShortcutColAction(), ID: "action"},
		{Title: locales.ShortcutColKey(), ID: "key", Width: 130},
		{Title: locales.ShortcutColDefault(), ID: "default", Width: 130},
	}
	p.list.Selected = &p.row
	p.list.Label = func(i int) string { return shortcutActions[i].title() }
	table := ui.Table(c, &p.list, cols, len(shortcutActions), func(row, col int) {
		act := &shortcutActions[row]
		switch col {
		case 0:
			ui.Text(c, act.title()).SingleLine()
		case 1:
			t := ui.Text(c, a.shortcutLabel(act.id)).SingleLine()
			if *act.field(&a.settings.Shortcuts) != "" {
				t.Bold() // the user's
			}
		case 2:
			t := ui.Text(c, act.defaultKey(goos).label(goos, act.digits)).SingleLine()
			if row != p.row {
				t.TextColor(theme.TextMuted)
			}
		}
	}).Height(328)
	chosen := p.row >= 0 && p.row < len(shortcutActions)
	if table.Submitted() && chosen {
		a.startCapture(shortcutActions[p.row].id)
	}
	ui.Row(c).Gap(8).Children(func() {
		if ui.Button(c, locales.ShortcutChange()).Disabled(!chosen).Clicked() {
			a.startCapture(shortcutActions[p.row].id)
		}
		if ui.Button(c, locales.ShortcutReset()).Disabled(!chosen || *shortcutActions[max(p.row, 0)].field(&a.settings.Shortcuts) == "").Clicked() {
			act := &shortcutActions[p.row]
			a.changeSettings(func() { rebind(&a.settings.Shortcuts, goos, act.id, act.defaultKey(goos)) })
		}
		ui.Box(c).Grow(1)
		if ui.Button(c, locales.ShortcutResetAll()).Disabled(a.settings.Shortcuts == config.Shortcuts{}).Clicked() {
			a.changeSettings(func() { a.settings.Shortcuts = config.Shortcuts{} })
		}
	})
	widgets.Muted(c, locales.ShortcutsNote()).FontSize(12)
	a.captureDialog(c)
}

// startCapture opens the dialog that takes a new key for id.
func (a *App) startCapture(id shortcutID) {
	a.shortcutsUI.capturing = true
	a.shortcutsUI.capture = keyCapture{id: id}
}

// captureDialog takes a key for an action: the key pressed shows, with
// why it cannot be the action's (a key the shell or the system needs) or
// what it clashes with, and OK binds it.
func (a *App) captureDialog(c *ui.Context) {
	p := &a.shortcutsUI
	goos := runtime.GOOS
	ui.Modal(c, &p.capturing, func() {
		cp := &p.capture
		act := actionOf(cp.id)
		ui.Column(c).Width(420).Gap(10).Children(func() {
			ui.Text(c, locales.ShortcutCaptureTitle(act.title())).FontSize(15).Bold()
			prompt := locales.ShortcutCapturePrompt()
			if act.digits {
				prompt = locales.ShortcutCaptureDigits()
			}
			widgets.Muted(c, prompt)
			text := locales.ShortcutCaptureWaiting()
			if cp.taken {
				text = cp.key.label(goos, act.digits)
			}
			field := ui.Box(c).Height(44).Center().Radius(6).Border(1, theme.Border).Background(theme.Surface).
				Focusable().AutoFocus().Label(locales.ShortcutCaptureTitle(act.title())).
				HandleInput(func(ev ui.InputEvent) bool {
					// Escape alone closes the dialog; the modifiers alone
					// wait for a key.
					if ev.Kind != ui.InputKeyDown || ev.Key == ui.KeyUnknown || ev.Key == ui.KeyEscape && ev.Mods == 0 {
						return false
					}
					k := keyCombo{ev.Mods, ev.Key}
					if act.digits && isDigit(k.Key) {
						k.Key = ui.Key1
					}
					cp.key, cp.taken = k, true
					return true
				})
			if field.Focused() {
				field.Border(2, theme.Accent)
			}
			field.Children(func() {
				t := ui.Text(c, text).FontSize(16)
				if !cp.taken {
					t.TextColor(theme.TextMuted)
				}
			})
			problem := keyOK
			if cp.taken {
				problem = checkShortcut(goos, cp.id, cp.key)
			}
			switch {
			case problem != keyOK:
				ui.Text(c, problem.message(goos)).TextColor(problemText)
			case cp.taken:
				for _, id := range shortcutConflicts(&a.settings.Shortcuts, goos, cp.id, cp.key) {
					ui.Text(c, locales.ShortcutConflict(actionOf(id).title())).TextColor(warningText)
				}
				if terminalKeyClash(cp.key, act.digits) {
					ui.Text(c, locales.ShortcutTerminalKey()).TextColor(warningText)
				}
			}
			ui.Row(c).Gap(8).Padding(6, 0, 0, 0).Children(func() {
				if ui.Button(c, locales.ShortcutUseDefault()).Clicked() {
					cp.key, cp.taken = act.defaultKey(goos), true
				}
				ui.Box(c).Grow(1)
				if ui.Button(c, locales.Cancel()).Clicked() {
					p.capturing = false
				}
				if ui.PrimaryButton(c, locales.Ok()).Disabled(!cp.taken || problem != keyOK).Clicked() {
					id, k := cp.id, cp.key
					a.changeSettings(func() { rebind(&a.settings.Shortcuts, goos, id, k) })
					p.capturing = false
				}
			})
		})
	})
}
