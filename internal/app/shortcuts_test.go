package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The defaults are FinalShell's on each platform, each one a key the
// checks let through, and no two alike.
func TestShortcutDefaults(t *testing.T) {
	want := map[string][]string{
		"darwin": {"⌘C", "⌘V", "⌘F", "⌘↩", "↩", "⌘[", "⌘]", "⌘W", "⌘O", "⌘T", "⌘1…9", "⇧⌘L", "⇧⌘E", "⌘J"},
		"linux": {"Ctrl+Shift+C", "Ctrl+Shift+V", "Ctrl+Shift+F", "Ctrl+Shift+Enter", "Enter", "Ctrl+Shift+Tab", "Ctrl+Tab",
			"Ctrl+Shift+W", "Ctrl+Shift+O", "Ctrl+Shift+T", "Alt+1…9", "Ctrl+Shift+L", "Ctrl+Shift+E", "Ctrl+Shift+J"},
	}
	want["windows"] = want["linux"]
	for goos, labels := range want {
		var got []string
		for i := range shortcutActions {
			act := &shortcutActions[i]
			if act.id != shortcutID(i) {
				t.Fatalf("action %d has id %d", i, act.id)
			}
			k := act.defaultKey(goos)
			got = append(got, k.label(goos, act.digits))
			if p := checkShortcut(goos, act.id, k); p != keyOK {
				t.Errorf("%s: the default of %s is refused: %s", goos, act.title(), p.message(goos))
			}
			if c := shortcutConflicts(&config.Shortcuts{}, goos, act.id, k); len(c) > 0 {
				t.Errorf("%s: the default of %s clashes with %v", goos, act.title(), c)
			}
		}
		if !slices.Equal(got, labels) {
			t.Errorf("%s: defaults %q, want %q", goos, got, labels)
		}
	}
}

// Keys a shell needs are refused, on each platform, and others go.
func TestShortcutChecks(t *testing.T) {
	for _, tc := range []struct {
		goos string
		id   shortcutID
		k    keyCombo
		want shortcutProblem
	}{
		{"linux", scFind, keyCombo{0, ui.KeyF}, keyPlain},
		{"linux", scFind, keyCombo{ui.Shift, ui.KeyF}, keyPlain},
		{"linux", scFind, keyCombo{0, ui.KeyF5}, keyPlain},
		{"linux", scFind, keyCombo{ui.Ctrl, ui.KeyF}, keyShellCtrl},
		{"windows", scFind, keyCombo{ui.Ctrl, ui.KeyR}, keyShellCtrl},
		{"linux", scNextTab, keyCombo{ui.Ctrl, ui.KeyRight}, keyShellCtrl},
		{"linux", scNextTab, keyCombo{ui.Ctrl, ui.KeyBracketRight}, keyShellCtrl},
		{"linux", scFind, keyCombo{ui.Alt, ui.KeyF}, keyShellAlt},
		{"linux", scFind, keyCombo{ui.Alt | ui.Shift, ui.KeyF}, keyShellAlt},
		{"linux", scNewTab, keyCombo{ui.Alt, ui.Key1}, keyShellAlt}, // a digit alone is the tabs'
		{"linux", scPreviousTab, keyCombo{ui.Alt, ui.KeyLeft}, keyShellAlt},
		{"darwin", scFind, keyCombo{ui.Alt, ui.KeyB}, keyOption},
		{"darwin", scSwitchTab, keyCombo{ui.Alt, ui.Key1}, keyOption},
		{"darwin", scFind, keyCombo{ui.Ctrl, ui.KeyF}, keyShellCtrl},
		{"darwin", scCloseTab, keyCombo{ui.Super, ui.KeyQ}, keySystem},
		{"darwin", scNextTab, keyCombo{ui.Super, ui.KeyTab}, keySystem},
		{"linux", scNextTab, keyCombo{ui.Alt, ui.KeyTab}, keySystem},
		{"windows", scCloseTab, keyCombo{ui.Alt, ui.KeyF4}, keySystem},
		{"windows", scNewTab, keyCombo{ui.Super, ui.KeyT}, keySystem},
		{"windows", scFind, keyCombo{ui.Ctrl | ui.Alt, ui.KeyF}, keyAltGr},
		{"linux", scSwitchTab, keyCombo{ui.Alt, ui.KeyF}, keyNotDigit},
		{"linux", scSwitchTab, keyCombo{ui.Ctrl, ui.Key1}, keyShellCtrl},

		{"linux", scFind, keyCombo{ui.Ctrl | ui.Shift, ui.KeyX}, keyOK},
		{"linux", scFind, keyCombo{ui.Ctrl | ui.Alt, ui.KeyF}, keyOK},
		{"linux", scFind, keyCombo{ui.Ctrl, ui.KeyF3}, keyOK},
		{"linux", scNextTab, keyCombo{ui.Ctrl, ui.KeyPageDown}, keyOK},
		{"linux", scNewTab, keyCombo{ui.Super, ui.KeyT}, keyOK},
		{"darwin", scFind, keyCombo{ui.Super | ui.Shift, ui.KeyF}, keyOK},
		{"darwin", scFind, keyCombo{ui.Ctrl | ui.Shift, ui.KeyF}, keyOK},
		{"windows", scSwitchTab, keyCombo{ui.Alt, ui.Key3}, keyOK},
		{"linux", scReconnect, keyCombo{0, ui.KeyR}, keyOK}, // where no shell runs
	} {
		if got := checkShortcut(tc.goos, tc.id, tc.k); got != tc.want {
			t.Errorf("%s %s on %s: %d (%s), want %d", tc.goos, tc.k.label(tc.goos, false), actionOf(tc.id).title(), got, got.message(tc.goos), tc.want)
		}
	}
}

// Keys are written as the settings keep them, and read back.
func TestShortcutFormat(t *testing.T) {
	for _, tc := range []struct {
		k      keyCombo
		digits bool
		text   string
	}{
		{keyCombo{ui.Ctrl | ui.Shift, ui.KeyC}, false, "Ctrl+Shift+C"},
		{keyCombo{ui.Super, ui.KeyEnter}, false, "Super+Enter"},
		{keyCombo{ui.Ctrl | ui.Alt | ui.Shift | ui.Super, ui.KeyBracketLeft}, false, "Ctrl+Alt+Shift+Super+["},
		{keyCombo{0, ui.KeyF12}, false, "F12"},
		{keyCombo{ui.Ctrl, ui.KeyEqual}, false, "Ctrl+="},
		{keyCombo{ui.Alt, ui.Key1}, true, "Alt+1-9"},
		{keyCombo{}, false, "none"},
	} {
		if got := tc.k.format(tc.digits); got != tc.text {
			t.Errorf("%v: %q, want %q", tc.k, got, tc.text)
		}
		if got, ok := parseCombo(tc.text, tc.digits); !ok || got != tc.k {
			t.Errorf("%q read %v %v", tc.text, got, ok)
		}
	}
	for _, bad := range []string{"Ctrl+Nope", "Hyper+C", "Alt+1", ""} {
		if _, ok := parseCombo(bad, bad == "Alt+1"); ok {
			t.Errorf("%q read", bad)
		}
	}
	if got := (keyCombo{ui.Ctrl | ui.Shift | ui.Super, ui.KeyLeft}).label("darwin", false); got != "⌃⇧⌘←" {
		t.Errorf("macOS label %q", got)
	}
	if got := (keyCombo{ui.Super, ui.KeyT}).label("windows", false); got != "Win+T" {
		t.Errorf("Windows label %q", got)
	}
}

// Rebinding keeps only what differs from the defaults, takes the key
// from the action that had it, and survives a new run; a key the
// settings cannot read is the default.
func TestRebindPersists(t *testing.T) {
	a, p := testApp(t)
	sc := &a.settings.Shortcuts
	a.changeSettings(func() {
		rebind(sc, "linux", scFind, keyCombo{ui.Ctrl | ui.Shift, ui.KeyW}) // close tab's
	})
	if sc.Find != "Ctrl+Shift+W" || sc.CloseTab != "none" {
		t.Fatalf("shortcuts %+v", *sc)
	}
	a.changeSettings(func() { rebind(sc, "linux", scFind, actionOf(scFind).other) })
	if sc.Find != "" || sc.CloseTab != "none" {
		t.Fatalf("back to the default: %+v", *sc)
	}
	a.changeSettings(func() { rebind(sc, "linux", scSwitchTab, keyCombo{ui.Ctrl | ui.Alt, ui.Key5}) })
	data, err := os.ReadFile(filepath.Join(config.Dir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	json.Unmarshal(data, &file)
	if got, _ := json.Marshal(file["shortcuts"]); string(got) != `{"closeTab":"none","switchTab":"Ctrl+Alt+1-9"}` {
		t.Errorf("saved %s", got)
	}
	b := New(p)
	if b.shortcut(scCloseTab) != (keyCombo{}) || b.shortcut(scSwitchTab) != (keyCombo{ui.Ctrl | ui.Alt, ui.Key1}) || b.shortcut(scFind) != actionOf(scFind).other {
		t.Errorf("read back %+v", b.settings.Shortcuts)
	}
	if b.shortcutLabel(scCloseTab) != locales.ShortcutNone() {
		t.Errorf("no key shows %q", b.shortcutLabel(scCloseTab))
	}
	b.settings.Shortcuts.Find = "Ctrl+Nonsense"
	if b.shortcut(scFind) != actionOf(scFind).other {
		t.Errorf("an unreadable key: %v", b.shortcut(scFind))
	}
	// Settings without shortcuts write none.
	data, _ = json.Marshal(config.Settings{})
	if strings.Contains(string(data), "shortcuts") {
		t.Errorf("empty settings %s", data)
	}
}

// The terminal's keybinds put copy and paste on the user's keys, and on
// Windows leave Ctrl+C and Ctrl+V to the shell.
func TestTerminalKeybinds(t *testing.T) {
	if got := terminalKeybinds("linux", &config.Shortcuts{}); len(got) != 0 {
		t.Errorf("the defaults bind %+v", got)
	}
	win := terminalKeybinds("windows", &config.Shortcuts{})
	if !slices.Equal(win, []terminal.Keybind{{Mods: ui.Ctrl, Key: ui.KeyC}, {Mods: ui.Ctrl, Key: ui.KeyV}}) {
		t.Errorf("Windows %+v", win)
	}
	var sc config.Shortcuts
	rebind(&sc, "linux", scCopy, keyCombo{ui.Ctrl | ui.Shift, ui.KeyV})
	rebind(&sc, "linux", scPaste, keyCombo{ui.Ctrl | ui.Shift, ui.KeyC})
	want := []terminal.Keybind{
		{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyC}, {Mods: ui.Ctrl | ui.Shift, Key: ui.KeyV},
		{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyV, Action: terminal.ActionCopy},
		{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyC, Action: terminal.ActionPaste},
	}
	if got := terminalKeybinds("linux", &sc); !slices.Equal(got, want) {
		t.Errorf("swapped %+v", got)
	}
}

// openShortcutsPage opens the options window on the Shortcuts page.
func openShortcutsPage(t *testing.T, a *App, p *fakePlatform) *ui.Tester {
	t.Helper()
	a.openOptions()
	tt := p.show(t, WindowOptions)
	if err := tt.Click(locales.OptionsShortcuts()); err != nil {
		t.Fatal(err)
	}
	if a.optionsNav.Page != pageShortcuts {
		t.Fatalf("page %q", a.optionsNav.Page)
	}
	return tt
}

// The Shortcuts page lists the actions, takes a new key in its dialog,
// refuses the shell's keys, warns of clashes, and resets.
func TestShortcutsPage(t *testing.T) {
	a, p := testApp(t)
	tt := openShortcutsPage(t, a, p)
	for _, act := range shortcutActions {
		if !tt.HasText(act.title()) || !tt.HasText(a.shortcutLabel(act.id)) {
			t.Errorf("no row %q %q; texts %q", act.title(), a.shortcutLabel(act.id), tt.Texts())
		}
	}
	if tt.HasText(locales.OptionAsAlt()) != optionAsAltShown {
		t.Errorf("Option as Alt shows %v", !optionAsAltShown)
	}
	saveShot(t, tt, "shortcuts-en.png")

	if err := tt.Click(locales.ScTerminalFind()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.ShortcutChange()); err != nil {
		t.Fatal(err)
	}
	if !a.shortcutsUI.capturing || !tt.HasText(locales.ShortcutCaptureWaiting()) {
		t.Fatalf("no dialog; texts %q", tt.Texts())
	}
	// A key of the shell's is refused.
	tt.Key(ui.Ctrl, ui.KeyR)
	if !tt.HasText("Ctrl+R") || !tt.HasText(locales.ShortcutShellCtrl()) {
		t.Errorf("Ctrl+R: texts %q", tt.Texts())
	}
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if !a.shortcutsUI.capturing || a.settings.Shortcuts.Find != "" {
		t.Fatalf("Ctrl+R taken: %+v", a.settings.Shortcuts)
	}
	saveShot(t, tt, "shortcuts-refused-en.png")
	// One of another action's is a clash, which it gives up.
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyT)
	if !tt.HasText(locales.ShortcutConflict(locales.NewTab())) {
		t.Errorf("Ctrl+Shift+T: no clash; texts %q", tt.Texts())
	}
	saveShot(t, tt, "shortcuts-conflict-en.png")
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if a.shortcutsUI.capturing || a.settings.Shortcuts.Find != "Ctrl+Shift+T" || a.settings.Shortcuts.NewTab != "none" {
		t.Fatalf("after OK: %+v", a.settings.Shortcuts)
	}
	if config.LoadSettings().Shortcuts != a.settings.Shortcuts {
		t.Errorf("not saved: %+v", config.LoadSettings().Shortcuts)
	}
	// The terminal's own keys are a clash too.
	if err := tt.Click(locales.ShortcutChange()); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyA)
	if !tt.HasText(locales.ShortcutTerminalKey()) {
		t.Errorf("Ctrl+Shift+A: texts %q", tt.Texts())
	}
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	if a.settings.Shortcuts.Find != "Ctrl+Shift+T" {
		t.Errorf("Cancel kept %+v", a.settings.Shortcuts)
	}

	// Chinese.
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"快捷键", "终端：复制", "终端：查找", "终端：全屏", "上一个标签", "修改…", "全部恢复默认"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "shortcuts-zh.png")
	if err := tt.Click("修改…"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyO)
	if !tt.HasText("“连接管理器”正在使用这个组合键，保存后它将没有快捷键。") {
		t.Errorf("no clash in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "shortcuts-conflict-zh.png")
	tt.Key(ui.Alt, ui.KeyF)
	if !tt.HasText("Alt 加这个键在 shell 里是 Meta 键（如 Alt+B、Alt+F 按词移动）。") {
		t.Errorf("no refusal in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "shortcuts-refused-zh.png")
	if err := tt.Click("使用默认"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("确定"); err != nil {
		t.Fatal(err)
	}
	if a.settings.Shortcuts.Find != "" {
		t.Errorf("default: %+v", a.settings.Shortcuts)
	}

	// Reset all.
	if err := tt.Click("全部恢复默认"); err != nil {
		t.Fatal(err)
	}
	if a.settings.Shortcuts != (config.Shortcuts{}) || config.LoadSettings().Shortcuts != (config.Shortcuts{}) {
		t.Errorf("reset all: %+v", a.settings.Shortcuts)
	}
}

// Option as Alt, a check box of the page on macOS, reaches the options
// of the terminals made after it.
func TestOptionAsAlt(t *testing.T) {
	shown := optionAsAltShown
	optionAsAltShown = true
	defer func() { optionAsAltShown = shown }()
	a, p := testApp(t)
	tt := openShortcutsPage(t, a, p)
	if a.terminalOptions(nil).OptionAsAlt {
		t.Fatal("on by default")
	}
	if err := tt.Click(locales.OptionAsAlt()); err != nil {
		t.Fatal(err)
	}
	if !a.settings.OptionAsAlt || !config.LoadSettings().OptionAsAlt || !a.terminalOptions(nil).OptionAsAlt {
		t.Errorf("not on: %+v", a.settings)
	}
	saveShot(t, tt, "shortcuts-mac-en.png")
	if err := tt.Click(locales.OptionAsAlt()); err != nil {
		t.Fatal(err)
	}
	if a.terminalOptions(nil).OptionAsAlt {
		t.Error("still on")
	}
}

// The key of reconnecting connects a session again while the focus is in
// its terminal, and only once it is not connected.
func TestReconnectKey(t *testing.T) {
	a, p := testApp(t)
	h := refusedHost(t, a, "gone")
	s := newSession(h)
	a.sessions, a.active = []*Session{s}, 0
	s.status, s.err = statusClosed, errConnectionLost
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	tt.ClickAt(760, 260) // in the terminal area
	tt.Key(0, ui.KeyEnter)
	if s.status != statusConnecting {
		t.Fatalf("Enter: status %v", s.status)
	}
	p.until(t, "the connection to fail", func() bool { return s.status == statusFailed })
	tt.Frame()
	tt.Key(0, ui.KeyEnter) // the message has the focus now
	if s.status != statusConnecting {
		t.Fatalf("Enter after it failed: status %v", s.status)
	}
	p.until(t, "the connection to fail", func() bool { return s.status == statusFailed })
}
