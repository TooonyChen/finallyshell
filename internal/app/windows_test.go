package app

import (
	"slices"
	"testing"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// fakeWindows stands in for the secondary windows: it keeps what the app
// opened, and draws a window's view only when a test shows it.
type fakeWindows struct {
	windows map[WindowKind]*fakeWindow
	focused []WindowKind // FocusWindow's calls
	redraws int
}

// fakeWindow is a secondary window the app opened.
type fakeWindow struct {
	spec   WindowSpec
	view   func(c *ui.Context)
	closed func()
	tt     *ui.Tester // the window's view, once a test shows it
}

func (p *fakeWindows) OpenWindow(spec WindowSpec, view func(c *ui.Context), closed func()) {
	if p.windows == nil {
		p.windows = map[WindowKind]*fakeWindow{}
	}
	if p.windows[spec.Kind] != nil {
		panic("a second window of kind " + string(spec.Kind))
	}
	if spec.Width < spec.MinWidth || spec.Height < spec.MinHeight {
		panic("window " + string(spec.Kind) + " opens below its least size")
	}
	p.windows[spec.Kind] = &fakeWindow{spec: spec, view: view, closed: closed}
}

func (p *fakeWindows) FocusWindow(kind WindowKind) { p.focused = append(p.focused, kind) }

// CloseWindow closes the window and its children, children first, as
// MyGo does.
func (p *fakeWindows) CloseWindow(kind WindowKind) {
	w := p.windows[kind]
	if w == nil {
		return
	}
	for k, c := range p.windows {
		if c.spec.Parent == kind {
			p.CloseWindow(k)
		}
	}
	delete(p.windows, kind)
	w.closed()
}

func (p *fakeWindows) Redraw() { p.redraws++ }

func (p *fakeWindows) setLocale(tag string) {
	for _, w := range p.windows {
		if w.tt != nil {
			w.tt.SetLocale(tag)
		}
	}
}

// show draws the open window of kind with a tester of its own, at its
// size and twice the pixels, which follows the language as the main one
// does.
func (p *fakePlatform) show(t *testing.T, kind WindowKind) *ui.Tester {
	t.Helper()
	w := p.windows[kind]
	if w == nil {
		t.Fatalf("no %s window open", kind)
	}
	if w.tt == nil {
		w.tt = ui.NewTester(w.view, w.spec.Width, w.spec.Height)
		w.tt.SetScale(2)
		w.tt.SetLocale(p.locale)
	}
	return w.tt
}

func TestWindowOpensOncePerKind(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	a.openOptions()
	if len(p.windows) != 1 || !a.windowOpen(WindowOptions) {
		t.Fatalf("windows %v", p.windows)
	}
	if !slices.Equal(p.focused, []WindowKind{WindowOptions}) {
		t.Errorf("opening it again focused %v", p.focused)
	}
	spec := p.windows[WindowOptions].spec
	if spec.Title() != locales.Options() || spec.Parent != "" || spec.Modal || spec.MinWidth == 0 || spec.MinHeight == 0 {
		t.Errorf("spec %+v", spec)
	}

	a.closeWindow(WindowOptions)
	if a.windowOpen(WindowOptions) || len(p.windows) != 0 {
		t.Fatalf("still open: %v", p.windows)
	}
	a.openOptions()
	if !a.windowOpen(WindowOptions) || len(p.focused) != 1 {
		t.Errorf("not opened again: windows %v, focused %v", p.windows, p.focused)
	}
}

// windowSpecs are the windows the app opens: a new kind goes here too.
var windowSpecs = []WindowSpec{optionsWindow, editorSpec(true), managerSpec}

func TestWindowSpecsFitTheirLeastSize(t *testing.T) {
	kinds := map[WindowKind]bool{}
	for _, s := range windowSpecs {
		if s.Kind == "" || kinds[s.Kind] || s.Title == nil || s.Title() == "" {
			t.Errorf("%s: kind or title missing, or the kind twice", s.Kind)
		}
		kinds[s.Kind] = true
		if s.Width < s.MinWidth || s.Height < s.MinHeight || s.Width <= 0 || s.Height <= 0 {
			t.Errorf("%s opens at %dx%d, its least size %dx%d", s.Kind, s.Width, s.Height, s.MinWidth, s.MinHeight)
		}
	}

	// A spec smaller than its least size opens at the least size.
	a, p := testApp(t)
	a.openWindow(WindowSpec{Kind: "test-small", Title: locales.Options, Width: 100, Height: 50, MinWidth: 300, MinHeight: 200}, func(*ui.Context) {})
	if s := p.windows["test-small"].spec; s.Width != 300 || s.Height != 200 {
		t.Errorf("opened at %dx%d", s.Width, s.Height)
	}
}

// A dialog of a dialog, as the connection editor of the connection
// manager: modal to it, and closed with it.
func TestModalChildOfChild(t *testing.T) {
	const manager, editor WindowKind = "test-manager", "test-editor"
	a, p := testApp(t)
	view := func(c *ui.Context) { ui.Text(c, "x") }
	a.openWindow(WindowSpec{Kind: manager, Title: locales.ManagerTitle, Width: 600, Height: 400}, view)
	a.openWindow(WindowSpec{Kind: editor, Title: locales.EditConnection, Width: 500, Height: 400, Parent: manager, Modal: true}, view)
	if w := p.windows[editor]; w == nil || w.spec.Parent != manager || !w.spec.Modal {
		t.Fatalf("editor %+v", w)
	}

	a.closeWindow(manager)
	if a.windowOpen(manager) || a.windowOpen(editor) || len(p.windows) != 0 {
		t.Fatalf("open after closing the manager: %v", a.windows)
	}

	// Without its parent open, the editor belongs to the main window.
	a.openWindow(WindowSpec{Kind: editor, Title: locales.EditConnection, Width: 500, Height: 400, Parent: manager}, view)
	if w := p.windows[editor]; w == nil || w.spec.Parent != "" {
		t.Fatalf("editor %+v", w)
	}
}

func TestEscapeClosesWindow(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.Key(0, ui.KeyEscape)
	if a.windowOpen(WindowOptions) {
		t.Error("Escape left the options window open")
	}
}

// On macOS Cmd+W closes a secondary window too, and the key of closing a
// tab once the user moved it; elsewhere it does not.
func TestCmdWClosesWindowOnMac(t *testing.T) {
	defer func(was bool) { macOS = was }(macOS)
	for _, mac := range []bool{true, false} {
		macOS = mac
		a, p := testApp(t)
		a.openOptions()
		tt := p.show(t, WindowOptions)
		tt.Key(ui.Super, ui.KeyW)
		if a.windowOpen(WindowOptions) == mac {
			t.Errorf("mac %v: Cmd+W left the options window open: %v", mac, a.windowOpen(WindowOptions))
		}
		rebind(&a.settings.Shortcuts, "darwin", scCloseTab, keyCombo{ui.Super | ui.Shift, ui.KeyW})
		a.openOptions()
		tt = p.show(t, WindowOptions)
		tt.Key(ui.Super|ui.Shift, ui.KeyW)
		if a.windowOpen(WindowOptions) == mac {
			t.Errorf("mac %v: the close tab key left the options window open: %v", mac, a.windowOpen(WindowOptions))
		}
		if !mac {
			tt.Key(0, ui.KeyEscape)
			if a.windowOpen(WindowOptions) {
				t.Error("Escape left the options window open")
			}
		}
	}
}

func TestClosingMainClosesWindows(t *testing.T) {
	const manager, editor WindowKind = "test-manager", "test-editor"
	a, p := testApp(t)
	view := func(c *ui.Context) {}
	a.openOptions()
	a.openWindow(WindowSpec{Kind: manager, Title: locales.ManagerTitle, Width: 600, Height: 400}, view)
	a.openWindow(WindowSpec{Kind: editor, Title: locales.EditConnection, Width: 500, Height: 400, Parent: manager, Modal: true}, view)
	a.CloseAll()
	if len(a.windows) != 0 || len(p.windows) != 0 {
		t.Errorf("open after the main window closed: %v", a.windows)
	}
}

func TestOptionsWindowChangesLanguage(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	mainWin := ui.NewTester(a.View, 1280, 820)
	mainWin.SetScale(2)
	p.tt = mainWin
	if err := mainWin.Click(locales.Options()); err != nil {
		t.Fatal(err)
	}
	if !a.windowOpen(WindowOptions) {
		t.Fatal("options window not opened")
	}
	if mainWin.HasText(locales.FollowSystem()) {
		t.Error("the options show in the main window")
	}
	saveShot(t, mainWin, "main-with-options-window.png")

	tt := p.show(t, WindowOptions)
	if !tt.HasText(locales.FollowSystem()) || !tt.HasText("简体中文") {
		t.Fatalf("options not shown; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-en.png")

	redraws := p.redraws
	if err := tt.Click("简体中文"); err != nil {
		t.Fatal(err)
	}
	if a.settings.Language != "zh-Hans" || p.locale != "zh-Hans" || config.LoadSettings().Language != "zh-Hans" {
		t.Fatalf("language %q, shown %q", a.settings.Language, p.locale)
	}
	if p.redraws == redraws {
		t.Error("the other windows were not redrawn")
	}
	if got := p.windows[WindowOptions].spec.Title(); got != "选项" {
		t.Errorf("title %q", got)
	}
	if !tt.HasText("界面语言") || !tt.HasText("完成") {
		t.Errorf("options not in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-zh.png")
	mainWin.Frame()
	if !mainWin.HasText("会话 (1)") {
		t.Errorf("main window not in Chinese; texts %q", mainWin.Texts())
	}

	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowOptions) {
		t.Fatal("options window still open")
	}

	if err := mainWin.Click("选项"); err != nil {
		t.Fatal(err)
	}
	if err := p.show(t, WindowOptions).Click("跟随系统"); err != nil {
		t.Fatal(err)
	}
	if a.settings.Language != "" || config.LoadSettings().Language != "" {
		t.Errorf("not back to the system's language: %q", a.settings.Language)
	}
}
