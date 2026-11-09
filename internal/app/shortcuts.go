package app

import (
	"runtime"
	"slices"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The shortcuts are FinalShell's, as Options › Terminal › Shortcuts lists
// them: each action has one key, its platform's default unless the user
// rebinds it there. Three kinds of action take their keys in different
// places:
//
//   - the terminal's copy and paste are the terminal's keybinds
//     (terminal.Options.Keybinds), set as a session's terminal starts;
//   - reconnecting is a shortcut of the terminal area, so that it takes
//     Enter only while the focus is there, and only on a session that is
//     not connected;
//   - the others are the window's shortcuts, which come before the
//     terminal: their keys never reach the shell.
//
// Each must be a key no shell needs (shortcutProblem): never Ctrl or Alt
// with a letter alone, which readline and editors take, so on Linux and
// Windows the defaults are Ctrl+Shift ones, and on macOS Command ones.

// shortcutID names an action that has a shortcut.
type shortcutID int

const (
	scCopy shortcutID = iota
	scPaste
	scFind
	scFullScreen
	scReconnect
	scPreviousTab
	scNextTab
	scCloseTab
	scManager
	scNewTab
	scSwitchTab
	scFocusBar
	scLeftPanel
	scBottomPanel
)

// shortcutAction is an action of the shortcuts page.
type shortcutAction struct {
	id    shortcutID
	title func() string
	// field is where the settings keep the user's key.
	field func(*config.Shortcuts) *string
	// The defaults, on macOS and elsewhere.
	mac, other keyCombo
	// terminal binds the key in the terminal, which takes it as a session
	// connects: a change shows in sessions connected after it.
	terminal bool
	// digits takes the digits 1 to 9 with its modifiers, one a tab.
	digits bool
	// plain may be a key without modifiers: reconnecting, which takes its
	// key only on a session that is not connected, where the shell is
	// gone.
	plain bool
}

// shortcutActions are the shortcuts page's rows, in its order.
var shortcutActions = []shortcutAction{
	{
		id: scCopy, title: locales.ScTerminalCopy, terminal: true,
		field: func(s *config.Shortcuts) *string { return &s.Copy },
		mac:   keyCombo{ui.Super, ui.KeyC}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyC},
	},
	{
		id: scPaste, title: locales.ScTerminalPaste, terminal: true,
		field: func(s *config.Shortcuts) *string { return &s.Paste },
		mac:   keyCombo{ui.Super, ui.KeyV}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyV},
	},
	{
		id: scFind, title: locales.ScTerminalFind,
		field: func(s *config.Shortcuts) *string { return &s.Find },
		mac:   keyCombo{ui.Super, ui.KeyF}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyF},
	},
	{
		id: scFullScreen, title: locales.ScTerminalFullScreen,
		field: func(s *config.Shortcuts) *string { return &s.FullScreen },
		mac:   keyCombo{ui.Super, ui.KeyEnter}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyEnter},
	},
	{
		id: scReconnect, title: locales.ScTerminalReconnect, plain: true,
		field: func(s *config.Shortcuts) *string { return &s.Reconnect },
		mac:   keyCombo{0, ui.KeyEnter}, other: keyCombo{0, ui.KeyEnter},
	},
	{
		id: scPreviousTab, title: locales.ScPreviousTab,
		field: func(s *config.Shortcuts) *string { return &s.PreviousTab },
		mac:   keyCombo{ui.Super, ui.KeyBracketLeft}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyTab},
	},
	{
		id: scNextTab, title: locales.ScNextTab,
		field: func(s *config.Shortcuts) *string { return &s.NextTab },
		mac:   keyCombo{ui.Super, ui.KeyBracketRight}, other: keyCombo{ui.Ctrl, ui.KeyTab},
	},
	{
		// Not FinalShell's Cmd+Q on macOS, which quits there.
		id: scCloseTab, title: locales.ScCloseTab,
		field: func(s *config.Shortcuts) *string { return &s.CloseTab },
		mac:   keyCombo{ui.Super, ui.KeyW}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyW},
	},
	{
		id: scManager, title: locales.ConnectionManager,
		field: func(s *config.Shortcuts) *string { return &s.ConnectionManager },
		mac:   keyCombo{ui.Super, ui.KeyO}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyO},
	},
	{
		// Ctrl+Shift+T opened the connection manager before there were new
		// tabs; a new tab lists the recent hosts and has the manager's
		// button, so the key still leads there, as in every terminal app.
		id: scNewTab, title: locales.NewTab,
		field: func(s *config.Shortcuts) *string { return &s.NewTab },
		mac:   keyCombo{ui.Super, ui.KeyT}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyT},
	},
	{
		// Option with a digit types a character on macOS.
		id: scSwitchTab, title: locales.ScSwitchTab, digits: true,
		field: func(s *config.Shortcuts) *string { return &s.SwitchTab },
		mac:   keyCombo{ui.Super, ui.Key1}, other: keyCombo{ui.Alt, ui.Key1},
	},
	{
		// L for the line; Ctrl+Shift+K is the terminal's clear screen on
		// Windows, as in Windows Terminal, and Cmd+K on macOS.
		id: scFocusBar, title: locales.ScFocusCommandBar,
		field: func(s *config.Shortcuts) *string { return &s.FocusCommandBar },
		mac:   keyCombo{ui.Super | ui.Shift, ui.KeyL}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyL},
	},
	{
		// E as VS Code's Explorer, which shows its side bar's tree; no
		// terminal binds it. With Shift on macOS too, as the command bar's
		// key: Cmd+E is "Use Selection for Find" in Mac apps.
		id: scLeftPanel, title: locales.ScSwitchLeftPanel,
		field: func(s *config.Shortcuts) *string { return &s.SwitchLeftPanel },
		mac:   keyCombo{ui.Super | ui.Shift, ui.KeyE}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyE},
	},
	{
		// J as VS Code's panel; Ctrl+J alone is a newline in the shell.
		id: scBottomPanel, title: locales.ScToggleBottomPanel,
		field: func(s *config.Shortcuts) *string { return &s.ToggleBottomPanel },
		mac:   keyCombo{ui.Super, ui.KeyJ}, other: keyCombo{ui.Ctrl | ui.Shift, ui.KeyJ},
	},
}

func actionOf(id shortcutID) *shortcutAction { return &shortcutActions[id] }

// defaultKey is the action's key on goos unless the user rebinds it.
func (act *shortcutAction) defaultKey(goos string) keyCombo {
	if goos == "darwin" {
		return act.mac
	}
	return act.other
}

// shortcutKey is the action's key in sc on goos: the user's, else the
// default, which a key the settings cannot read leaves too.
func shortcutKey(sc *config.Shortcuts, goos string, id shortcutID) keyCombo {
	act := actionOf(id)
	if s := *act.field(sc); s != "" {
		if k, ok := parseCombo(s, act.digits); ok {
			return k
		}
	}
	return act.defaultKey(goos)
}

// shortcut is the action's key, as the settings have it.
func (a *App) shortcut(id shortcutID) keyCombo {
	return shortcutKey(&a.settings.Shortcuts, runtime.GOOS, id)
}

// shortcutLabel is the action's key as the views show it, "None" for no
// key.
func (a *App) shortcutLabel(id shortcutID) string {
	k := a.shortcut(id)
	if k.none() {
		return locales.ShortcutNone()
	}
	return k.label(runtime.GOOS, actionOf(id).digits)
}

// sameKey tells whether the keys of two actions clash: the same key, or
// for an action of the digits, one of them with its modifiers.
func sameKey(a keyCombo, aDigits bool, b keyCombo, bDigits bool) bool {
	if a.none() || b.none() || a.Mods != b.Mods {
		return false
	}
	switch {
	case aDigits && bDigits:
		return true
	case aDigits:
		return isDigit(b.Key)
	case bDigits:
		return isDigit(a.Key)
	}
	return a.Key == b.Key
}

// rebind sets the action's key to k in sc, on goos, keeping only what
// differs from the default; another action that had the key is left
// without one, as the page warned (shortcutConflicts).
func rebind(sc *config.Shortcuts, goos string, id shortcutID, k keyCombo) {
	act := actionOf(id)
	for _, other := range shortcutConflicts(sc, goos, id, k) {
		o := actionOf(other)
		if o.defaultKey(goos).none() {
			*o.field(sc) = ""
		} else {
			*o.field(sc) = "none"
		}
	}
	if k == act.defaultKey(goos) {
		*act.field(sc) = ""
		return
	}
	*act.field(sc) = k.format(act.digits)
}

// shortcutConflicts are the other actions whose keys k, as the key of id,
// clashes with.
func shortcutConflicts(sc *config.Shortcuts, goos string, id shortcutID, k keyCombo) []shortcutID {
	var ids []shortcutID
	act := actionOf(id)
	for i := range shortcutActions {
		o := &shortcutActions[i]
		if o.id != id && sameKey(k, act.digits, shortcutKey(sc, goos, o.id), o.digits) {
			ids = append(ids, o.id)
		}
	}
	return ids
}

// A shortcutProblem is why a key cannot be an action's: a key the shell
// or the system needs.
type shortcutProblem int

const (
	keyOK shortcutProblem = iota
	// keyPlain is a key without modifiers, or with Shift alone: it types,
	// or the shell takes it, as Tab, Enter and the arrows.
	keyPlain
	// keyShellCtrl is Ctrl alone with a key that sends a control
	// character or edits the line: Ctrl+C, Ctrl+R, Ctrl+W, Ctrl+Left...
	keyShellCtrl
	// keyShellAlt is Alt with a key that types, or edits the line: Meta in
	// the shell, as Alt+B and Alt+F move by words.
	keyShellAlt
	// keyOption is Option with such a key on macOS, which types a
	// character, or is Meta with Option as Alt.
	keyOption
	// keyAltGr is Ctrl+Alt with a key that types on Windows, where it is
	// AltGr, which types characters on many layouts.
	keyAltGr
	// keySystem is a key the system takes: Cmd+Q and Cmd+Tab on macOS,
	// Alt+Tab and Alt+F4 elsewhere, the Windows key on Windows.
	keySystem
	// keyNotDigit is a key other than the digits 1 to 9, for the action
	// that switches tabs.
	keyNotDigit
)

// message says the problem in the language shown.
func (p shortcutProblem) message(goos string) string {
	switch p {
	case keyPlain:
		if goos == "darwin" {
			return locales.ShortcutPlain("⌘")
		}
		return locales.ShortcutPlain("Ctrl+Shift")
	case keyShellCtrl:
		return locales.ShortcutShellCtrl()
	case keyShellAlt:
		return locales.ShortcutShellAlt()
	case keyOption:
		return locales.ShortcutOption()
	case keyAltGr:
		return locales.ShortcutAltGr()
	case keySystem:
		return locales.ShortcutSystem()
	case keyNotDigit:
		return locales.ShortcutDigits()
	}
	return ""
}

// macSystemKeys are the Command keys macOS and the app's menus take
// before the window: quitting, hiding, minimizing, switching apps and
// windows, Spotlight.
var macSystemKeys = []keyCombo{
	{ui.Super, ui.KeyQ}, {ui.Super, ui.KeyH}, {ui.Super | ui.Alt, ui.KeyH}, {ui.Super, ui.KeyM},
	{ui.Super, ui.KeyTab}, {ui.Super | ui.Shift, ui.KeyTab}, {ui.Super, ui.KeySpace},
	{ui.Super, ui.KeyBackquote}, {ui.Super | ui.Ctrl, ui.KeyQ}, {ui.Super | ui.Ctrl, ui.KeyF},
	{ui.Super | ui.Shift, ui.Key3}, {ui.Super | ui.Shift, ui.Key4}, {ui.Super | ui.Shift, ui.Key5},
}

// otherSystemKeys are those Windows and Linux desktops take.
var otherSystemKeys = []keyCombo{
	{ui.Alt, ui.KeyTab}, {ui.Alt | ui.Shift, ui.KeyTab}, {ui.Alt, ui.KeyF4}, {ui.Alt, ui.KeySpace},
	{ui.Ctrl | ui.Alt, ui.KeyDelete}, {ui.Ctrl, ui.KeyEscape}, {ui.Ctrl | ui.Shift, ui.KeyEscape},
}

// checkShortcut tells why k cannot be the key of id on goos, or keyOK.
func checkShortcut(goos string, id shortcutID, k keyCombo) shortcutProblem {
	act := actionOf(id)
	if act.digits && !isDigit(k.Key) {
		return keyNotDigit
	}
	if act.plain {
		return keyOK
	}
	m, key := k.Mods, k.Key
	system := otherSystemKeys
	if goos == "darwin" {
		system = macSystemKeys
	}
	for _, s := range system {
		if s == k {
			return keySystem
		}
	}
	shellKey := typesText(key) || editsLine(key)
	switch {
	case m&^ui.Shift == 0:
		return keyPlain
	case m&ui.Super != 0:
		if goos == "windows" {
			return keySystem
		}
		return keyOK
	case m&ui.Ctrl != 0 && m&ui.Alt != 0:
		if goos == "windows" && typesText(key) {
			return keyAltGr
		}
		return keyOK
	case m == ui.Ctrl && shellKey:
		return keyShellCtrl
	case m&ui.Alt != 0 && m&ui.Ctrl == 0:
		if goos == "darwin" && shellKey {
			return keyOption
		}
		// Alt with a digit is readline's numeric argument, which few
		// type; it switches tabs, as in GNOME Terminal.
		if shellKey && !(act.digits && isDigit(key)) {
			return keyShellAlt
		}
	}
	return keyOK
}

// terminalKeyClash tells whether k is a key the terminal binds itself
// on this platform, as Ctrl+Shift+A selects all and Shift+Page Up
// scrolls: a shortcut on it takes it from the terminal. The terminal's
// first copy and paste keys are the page's own, which clash as actions.
func terminalKeyClash(k keyCombo, digits bool) bool {
	skip := []keyCombo{actionOf(scCopy).defaultKey(runtime.GOOS), actionOf(scPaste).defaultKey(runtime.GOOS)}
	for _, b := range terminalKeybinds(runtime.GOOS, &config.Shortcuts{}) {
		if b.Action == terminal.ActionNone {
			skip = append(skip, keyCombo{b.Mods, b.Key})
		}
	}
	for _, d := range terminal.DefaultKeybinds() {
		dk := keyCombo{d.Mods, d.Key}
		if !slices.Contains(skip, dk) && sameKey(k, digits, dk, false) {
			return true
		}
	}
	return false
}

// terminalKeybinds are the keybinds of a terminal on goos with the
// shortcuts of sc, over the terminal's defaults: the copy and paste keys
// the user chose in place of the defaults' first ones, and on Windows
// none on Ctrl+C and Ctrl+V, which the shell keeps (Ctrl+C interrupts,
// Ctrl+V inserts the next key as typed) as it does elsewhere.
func terminalKeybinds(goos string, sc *config.Shortcuts) []terminal.Keybind {
	var unbind, bind []terminal.Keybind
	if goos == "windows" {
		unbind = append(unbind,
			terminal.Keybind{Mods: ui.Ctrl, Key: ui.KeyC, Action: terminal.ActionNone},
			terminal.Keybind{Mods: ui.Ctrl, Key: ui.KeyV, Action: terminal.ActionNone})
	}
	for _, x := range []struct {
		id     shortcutID
		action terminal.Action
	}{{scCopy, terminal.ActionCopy}, {scPaste, terminal.ActionPaste}} {
		def, k := actionOf(x.id).defaultKey(goos), shortcutKey(sc, goos, x.id)
		if k == def {
			continue
		}
		unbind = append(unbind, terminal.Keybind{Mods: def.Mods, Key: def.Key, Action: terminal.ActionNone})
		if !k.none() {
			bind = append(bind, terminal.Keybind{Mods: k.Mods, Key: k.Key, Action: x.action})
		}
	}
	// Each binding replaces those of its key, in order: the unbindings
	// first, so that a key moved from one action to the other stays.
	return append(unbind, bind...)
}

// keyOptions sets the keys of a new terminal in o as the settings have
// them (see terminalOptions): the copy and paste keys, and Option as Alt.
// A running terminal keeps its own, so a change shows in the sessions
// connected after it.
func (a *App) keyOptions(o *terminal.Options) {
	o.Keybinds = terminalKeybinds(runtime.GOOS, &a.settings.Shortcuts)
	o.OptionAsAlt = a.settings.OptionAsAlt
}

// shortcuts takes the window's keys, which come before the terminal's.
func (a *App) shortcuts(c *ui.Context) {
	pressed := func(id shortcutID) bool {
		k := a.shortcut(id)
		return !k.none() && c.Shortcut(k.Mods, k.Key)
	}
	switch {
	case pressed(scManager):
		a.openManager()
	case pressed(scNewTab):
		a.NewTab()
	case pressed(scNextTab):
		a.cycle(1)
	case pressed(scPreviousTab):
		a.cycle(-1)
	case pressed(scCloseTab):
		if s := a.current(); s != nil {
			a.closeSession(s)
		}
	case pressed(scFind):
		a.openFind()
	case pressed(scFullScreen) && (a.fullScreen.on || !a.current().isPanelTab()):
		// A panel row has no terminal to show alone; it leaves full screen.
		a.setFullScreen(!a.fullScreen.on)
	case a.commandBarOn() && pressed(scFocusBar):
		a.toggleBarFocus()
	case pressed(scLeftPanel):
		a.switchLeftPanel()
	case pressed(scBottomPanel) && !a.fullScreen.on && !a.current().isPanelTab():
		// Only where the panel under the terminal shows.
		a.toggleBottomPanel()
	case a.fullScreen.on && !a.terminalLive() && c.Shortcut(0, ui.KeyEscape):
		// Esc is the shell's while it runs, as vi's; it leaves full screen
		// where no shell takes it.
		a.setFullScreen(false)
	}
	if k := a.shortcut(scSwitchTab); !k.none() {
		for i, d := range digitKeys {
			if c.Shortcut(k.Mods, d) && i < len(a.sessions) {
				a.active = i
			}
		}
	}
}

// reconnectShortcut takes the key of reconnecting on el, the terminal
// area of s: only while the focus is in it, so that a field elsewhere
// keeps Enter, and while s is not connected.
func (a *App) reconnectShortcut(el ui.Element, s *Session) {
	if s.isNewTab() || s.status == statusConnected || s.status == statusConnecting || s.find.open {
		return
	}
	if k := a.shortcut(scReconnect); !k.none() && el.Shortcut(k.Mods, k.Key) {
		a.reconnect(s)
	}
}
