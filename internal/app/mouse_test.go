package app

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// mouseTerm is a terminal of a connected session made as connect makes
// it, from the settings, with conn for its shell, shown on its own in a
// tester.
func mouseTerm(t *testing.T, a *App, conn *keyConn) (*Session, *terminal.Terminal, *ui.Tester) {
	t.Helper()
	s := demoSession(a, "web-1", "45.56.78.90")
	opts := a.terminalOptions(s.Host)
	opts.Conn = conn
	term, err := terminal.New(opts)
	if err != nil {
		t.Skip("no terminal:", err)
	}
	t.Cleanup(func() { term.Close() })
	s.term = term
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 400, 200)
	tt.Frame()
	return s, term, tt
}

// selectRow drags across the first row of the terminal tt shows, from
// the other end each time: the terminal counts clicks by the real clock,
// and two presses in a row at one place would be a double click.
func selectRow(tt *ui.Tester) {
	from, to := float32(1), float32(300)
	if selectBack = !selectBack; selectBack {
		from, to = to, from
	}
	tt.Press(from, 10)
	tt.Move(to, 10)
	tt.Release(to, 10)
}

var selectBack bool

// wheelField is what the Mouse page's lines a notch show.
func wheelField(t *testing.T, tt *ui.Tester) string {
	t.Helper()
	f, ok := tt.AccessByRole(ui.RoleTextField, "") // the page's only one
	if !ok {
		t.Fatal("no lines a notch")
	}
	return f.Value
}

// openMousePage opens the options window on the Mouse page.
func openMousePage(t *testing.T, a *App, p *fakePlatform) *ui.Tester {
	t.Helper()
	a.openOptions()
	tt := p.show(t, WindowOptions)
	if err := tt.Click(locales.OptionsMouse()); err != nil {
		t.Fatal(err)
	}
	if a.optionsNav.Page != pageMouse {
		t.Fatalf("page %q", a.optionsNav.Page)
	}
	return tt
}

// The Mouse page shows its options in both languages, and each one it
// changes is saved.
func TestMousePage(t *testing.T) {
	a, p := testApp(t)
	tt := openMousePage(t, a, p)
	for _, s := range []string{
		locales.MouseSelection(), locales.OptionCopyOnSelect(), locales.MouseRightClick(), locales.RightClickMenu(),
		locales.MouseLinks(), locales.OptionOpenLinks("Ctrl"), locales.MouseScrolling(), locales.OptionWheelLines(),
	} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	if got := wheelField(t, tt); got != "3" {
		t.Errorf("lines %q", got)
	}
	if tt.HasText(locales.OptionsComingLater()) {
		t.Error("the Mouse page is still to come")
	}
	saveShot(t, tt, "options-mouse-en.png")

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"鼠标", "选择", "选中即复制", "右键", "显示右键菜单", "链接", "Ctrl+单击打开链接", "滚动", "鼠标滚轮每格滚动行数", "已打开的会话在重新连接后生效。"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-mouse-zh.png")
	p.SetLocale("en")
	tt.Frame()

	// The right click, chosen from its list.
	if err := tt.Click(locales.RightClickMenu()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.RightClickCopyOrPaste()); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{locales.OptionCopyOnSelect(), locales.OptionOpenLinks("Ctrl")} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	// The wheel's lines, stepped up.
	for range 2 {
		if err := tt.Click("Increase"); err != nil {
			t.Fatal(err)
		}
	}
	want := config.Settings{CopyOnSelect: true, RightClick: config.RightClickCopyOrPaste, NoLinkClick: true, WheelLines: 5}
	if a.settings != want || config.LoadSettings() != want {
		t.Fatalf("settings %+v, saved %+v", a.settings, config.LoadSettings())
	}
	saveShot(t, tt, "options-mouse-changed-en.png")

	// The page shows them, and a new run reads them back.
	if !tt.HasText(locales.RightClickCopyOrPaste()) || wheelField(t, tt) != "5" {
		t.Errorf("lines %q; texts %q", wheelField(t, tt), tt.Texts())
	}
	if b := New(p); b.settings != want {
		t.Fatalf("read back %+v", b.settings)
	}

	// Back to the defaults, written as nothing.
	for range 2 {
		if err := tt.Click("Decrease"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click(locales.RightClickCopyOrPaste()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.RightClickMenu()); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{locales.OptionCopyOnSelect(), locales.OptionOpenLinks("Ctrl")} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if a.settings != (config.Settings{}) || config.LoadSettings() != (config.Settings{}) {
		t.Fatalf("back to the defaults: %+v, saved %+v", a.settings, config.LoadSettings())
	}
}

// The Mouse page names Cmd on macOS.
func TestMousePageNamesCmdOnMac(t *testing.T) {
	old := mouseGOOS
	mouseGOOS = "darwin"
	t.Cleanup(func() { mouseGOOS = old })
	a, p := testApp(t)
	tt := openMousePage(t, a, p)
	if !tt.HasText("Cmd+click opens links") || tt.HasText(locales.OptionWheelLinesWindows()) {
		t.Errorf("texts %q", tt.Texts())
	}
	mouseGOOS = "windows"
	tt.Frame()
	if !tt.HasText("Ctrl+click opens links") || !tt.HasText(locales.OptionWheelLinesWindows()) {
		t.Errorf("texts %q", tt.Texts())
	}
}

// A right click shows the context menu by default; set to paste, it
// pastes the clipboard into the shell, from the next connection on.
func TestRightClickPaste(t *testing.T) {
	a, _ := testApp(t)
	conn := &keyConn{done: make(chan struct{})}
	defer close(conn.done)
	_, term, tt := mouseTerm(t, a, conn)
	tt.SetClipboard("echo pasted")
	tt.RightClickAt(200, 100)
	if got := conn.sent(t, term); got != "" {
		t.Errorf("the default right click sent %q", got)
	}
	if menu := tt.Menu(); len(menu) == 0 {
		t.Error("no context menu")
	}
	tt.CloseMenu()

	// The terminal open keeps its menu; the next one pastes.
	a.changeTerminalSettings(func() { a.settings.RightClick = config.RightClickPaste })
	tt.RightClickAt(200, 100)
	if got := conn.sent(t, term); got != "" {
		t.Errorf("the open terminal's right click sent %q", got)
	}
	tt.CloseMenu()
	conn2 := &keyConn{done: make(chan struct{})}
	defer close(conn2.done)
	_, term2, tt2 := mouseTerm(t, a, conn2)
	tt2.SetClipboard("echo pasted")
	tt2.RightClickAt(200, 100)
	if got := conn2.sent(t, term2); got != "echo pasted" {
		t.Errorf("right click sent %q", got)
	}
	if menu := tt2.Menu(); len(menu) != 0 {
		t.Errorf("a menu %q", menu)
	}

	// Copy or paste copies a selection, and pastes without one.
	a.changeTerminalSettings(func() { a.settings.RightClick = config.RightClickCopyOrPaste })
	conn3 := &keyConn{done: make(chan struct{})}
	defer close(conn3.done)
	_, term3, tt3 := mouseTerm(t, a, conn3)
	term3.Feed([]byte("hello world"))
	tt3.Frame()
	tt3.SetClipboard("echo pasted")
	selectRow(tt3)
	tt3.RightClickAt(200, 100)
	if got := conn3.sent(t, term3); got != "" || tt3.Clipboard() != "hello world" {
		t.Errorf("with a selection: sent %q, copied %q", got, tt3.Clipboard())
	}
	tt3.RightClickAt(200, 100)
	if got := conn3.sent(t, term3); got != "hello world" {
		t.Errorf("without a selection: sent %q", got)
	}

	// Ignore does nothing at all.
	a.changeTerminalSettings(func() { a.settings.RightClick = config.RightClickIgnore })
	conn4 := &keyConn{done: make(chan struct{})}
	defer close(conn4.done)
	_, term4, tt4 := mouseTerm(t, a, conn4)
	tt4.SetClipboard("echo pasted")
	tt4.RightClickAt(200, 100)
	if got := conn4.sent(t, term4); got != "" || len(tt4.Menu()) != 0 {
		t.Errorf("ignored: sent %q, menu %q", got, tt4.Menu())
	}
}

// Copy on select copies a selection as it ends, once the Mouse page turns
// it on, in the terminals already open too.
func TestCopyOnSelect(t *testing.T) {
	a, p := testApp(t)
	conn := &keyConn{done: make(chan struct{})}
	defer close(conn.done)
	_, term, tt := mouseTerm(t, a, conn)
	term.Feed([]byte("hello world\r\nsecond line"))
	tt.Frame()
	tt.SetClipboard("untouched")
	selectRow(tt)
	if got := tt.Clipboard(); got != "untouched" {
		t.Fatalf("copied %q by default", got)
	}

	page := openMousePage(t, a, p)
	if err := page.Click(locales.OptionCopyOnSelect()); err != nil {
		t.Fatal(err)
	}
	selectRow(tt)
	if got := tt.Clipboard(); got != "hello world" {
		t.Fatalf("copied %q", got)
	}

	if err := page.Click(locales.OptionCopyOnSelect()); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboard("untouched")
	selectRow(tt)
	if got := tt.Clipboard(); got != "untouched" {
		t.Fatalf("copied %q once turned off", got)
	}
}

// The wheel scrolls as many lines a notch as the settings say, in the
// terminals already open too.
func TestWheelLines(t *testing.T) {
	a, _ := testApp(t)
	a.settings.CopyOnSelect = true // to read the top row
	conn := &keyConn{done: make(chan struct{})}
	defer close(conn.done)
	_, term, tt := mouseTerm(t, a, conn)
	var out strings.Builder
	for i := range 100 {
		fmt.Fprintf(&out, "\r\nline %02d", i)
	}
	term.Feed([]byte(out.String()))
	tt.Frame()
	top := func() string {
		t.Helper()
		selectRow(tt)
		return tt.Clipboard()
	}
	bottom := top()
	notch := map[string]float32{"linux": 50, "windows": 99}[runtime.GOOS]
	if notch == 0 {
		notch = 40
	}
	var at int
	if _, err := fmt.Sscanf(bottom, "line %d", &at); err != nil {
		t.Fatalf("top row %q", bottom)
	}
	for _, tc := range []struct{ lines, want int }{{0, 3}, {10, 10}, {1, 1}} {
		a.changeTerminalSettings(func() { a.settings.WheelLines = tc.lines })
		tt.Wheel(200, 100, 0, -notch)
		tt.Frame()
		at -= tc.want
		if got, want := top(), fmt.Sprintf("line %02d", at); got != want {
			t.Errorf("%d lines: top row %q, want %q", tc.lines, got, want)
			fmt.Sscanf(got, "line %d", &at)
		}
	}
}

// Ctrl+click opens a link the terminal shows, unless the settings say
// not to, from the next connection on.
func TestLinkClick(t *testing.T) {
	for _, off := range []bool{false, true} {
		t.Run(fmt.Sprint("off=", off), func(t *testing.T) {
			a, _ := testApp(t)
			a.settings.NoLinkClick = off
			conn := &keyConn{done: make(chan struct{})}
			defer close(conn.done)
			_, term, tt := mouseTerm(t, a, conn)
			term.Feed([]byte("https://example.com/"))
			tt.Frame()
			tt.ClickAtWith(ui.Cmd, 20, 10)
			opened := tt.OpenedURLs()
			if off && len(opened) != 0 || !off && (len(opened) != 1 || opened[0] != "https://example.com/") {
				t.Errorf("opened %q", opened)
			}
		})
	}
}

func TestMouseSettingsMapping(t *testing.T) {
	for _, tc := range []struct {
		r    config.RightClick
		want terminal.RightClickAction
	}{
		{"", terminal.RightClickContextMenu},
		{config.RightClickPaste, terminal.RightClickPaste},
		{config.RightClickCopy, terminal.RightClickCopy},
		{config.RightClickCopyOrPaste, terminal.RightClickCopyOrPaste},
		{config.RightClickIgnore, terminal.RightClickIgnore},
		{"middle-finger", terminal.RightClickContextMenu}, // a newer version's
	} {
		if got := rightClickAction(tc.r); got != tc.want {
			t.Errorf("%q: %v, want %v", tc.r, got, tc.want)
		}
	}
	for in, want := range map[int]int{0: 3, -2: 3, 1: 1, 7: 7, 20: 20, 500: 20} {
		if got := wheelLines(config.Settings{WheelLines: in}); got != want {
			t.Errorf("wheel lines %d: %d, want %d", in, got, want)
		}
	}
}
