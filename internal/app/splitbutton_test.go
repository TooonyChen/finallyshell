package app

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// splitMenu opens the menu of the command bar's Split button.
func splitMenu(t *testing.T, tt *ui.Tester) []string {
	t.Helper()
	if err := tt.Click(locales.CmdBarSplit()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	menu := tt.Menu()
	if menu == nil {
		t.Fatal("the Split button opened no menu")
	}
	return menu
}

// chooseSplit chooses item from the Split button's menu.
func chooseSplit(t *testing.T, tt *ui.Tester, item ...string) {
	t.Helper()
	splitMenu(t, tt)
	if err := tt.ChooseMenuItem(item...); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// The command bar's Split button opens the session row's panes' items,
// with their keys, for the focused pane: Zoom, Equalize, Move pane and
// Close this pane only with several panes. Each item leaves the keyboard
// in the focused pane's terminal.
func TestSplitButton(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	info, ok := tt.Access(locales.CmdBarSplit())
	if !ok || info.Disabled {
		t.Fatalf("the Split button: %+v, found %v", info, ok)
	}
	if bar, _ := tt.Access(locales.CmdBarLabel()); info.Bounds.X < bar.Bounds.X+bar.Bounds.W {
		t.Error("the Split button is not after the input")
	}
	if hist, ok := tt.Access(locales.CmdBarHistory()); !ok || info.Bounds.X < hist.Bounds.X+hist.Bounds.W {
		t.Error("the Split button is not after History")
	}

	// The same items as the row's menu, with their keys, drawn in the
	// window.
	want := []string{locales.SplitRight(), locales.SplitDown(), "-", locales.ZoomPane(), locales.EqualizePanes(), locales.MovePane(), locales.ClosePane()}
	if menu := splitMenu(t, tt); !slices.Equal(menu, want) {
		t.Fatalf("menu %q, want %q", menu, want)
	}
	for _, id := range []shortcutID{scSplitRight, scSplitDown, scZoomPane, scCloseTab} {
		if k := a.shortcut(id).label(runtime.GOOS, false); !tt.HasText(k) {
			t.Errorf("the menu shows no %s", k)
		}
	}
	for _, item := range []string{locales.ZoomPane(), locales.EqualizePanes(), locales.ClosePane()} {
		if err := tt.ChooseMenuItem(item); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("%s with one pane: %v", item, err)
		}
	}
	tt.CloseMenu()
	tt.Frame()

	// Split right, then down: the new pane has the focus and the keys.
	first := s.pane()
	chooseSplit(t, tt, locales.SplitRight())
	untilPanes(t, p, s, 2)
	second := s.pane()
	tt.Frame()
	tt.Type("a")
	untilInput(t, p, srv, "", "a")
	chooseSplit(t, tt, locales.SplitDown())
	untilPanes(t, p, s, 3)
	third := s.pane()
	names := map[*pane]string{first: "1", second: "2", third: "3"}
	if got := shape(s.panes.root, names); got != "r(1,d(2,3))" {
		t.Fatalf("tree %s", got)
	}

	// Equalize, Zoom, Move pane and Close act on the focused pane, and
	// give its terminal the keyboard back.
	s.panes.root.pct = 30
	chooseSplit(t, tt, locales.EqualizePanes())
	if s.panes.root.pct != 50 {
		t.Errorf("equalized: %.1f%%", s.panes.root.pct)
	}
	tt.Type("b")
	untilInput(t, p, srv, "", "a", "b")
	chooseSplit(t, tt, locales.ZoomPane())
	if !s.panes.zoomed || s.pane() != third {
		t.Fatal("not zoomed")
	}
	chooseSplit(t, tt, locales.ZoomPane())
	if s.panes.zoomed {
		t.Fatal("still zoomed")
	}
	chooseSplit(t, tt, locales.MovePane(), locales.MovePaneUp())
	if got := shape(s.panes.root, names); got != "r(1,d(3,2))" || s.pane() != third {
		t.Fatalf("moved up: %s", got)
	}
	tt.Type("c")
	untilInput(t, p, srv, "", "a", "bc")
	chooseSplit(t, tt, locales.ClosePane())
	if s.panes.count() != 2 || s.panes.has(third) {
		t.Fatalf("%d panes", s.panes.count())
	}
	tt.Type("d")
	untilInput(t, p, srv, "", "ad", "bc")

	// Disconnected, the button is disabled.
	s.disconnect()
	s.status, s.err = statusClosed, nil
	tt.Frame()
	if info, ok := tt.Access(locales.CmdBarSplit()); !ok || !info.Disabled {
		t.Errorf("the Split button of a closed session: %+v, found %v", info, ok)
	}

	// A new tab has no command bar; with the bar turned off there is no
	// button.
	a.NewTab()
	tt.Frame()
	if _, ok := tt.Access(locales.CmdBarSplit()); ok {
		t.Error("a new tab shows the Split button")
	}
	a.active = slices.Index(a.sessions, s)
	a.settings.NoCommandBar = true
	tt.Frame()
	if _, ok := tt.Access(locales.CmdBarSplit()); ok {
		t.Error("the Split button shows with the command bar off")
	}
}

// A local terminal splits from the button too.
func TestSplitButtonLocal(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	tt.Frame()
	chooseSplit(t, tt, locales.SplitRight())
	untilPanes(t, p, s, 2)
	if s.panes.root.dir != splitRight {
		t.Fatalf("split %v", s.panes.root.dir)
	}
}

// The screenshots of the PR: the Split button's menu with two panes, and
// with one, its pane items disabled; in English and Simplified Chinese,
// with SHOT_DIR set.
func TestSplitButtonScreenshots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		a, p, s, tt, _ := paneHost(t, followHome(t), false)
		tt.SetScale(2)
		p.SetLocale(lang)
		tt.SetLocale(lang)
		s.pane().term.Feed([]byte("root@web-1:/srv/app# "))
		tt.Frame()
		splitMenu(t, tt)
		saveShot(t, tt, "split-button-one-"+lang+".png")
		tt.CloseMenu()
		press(tt, a, scSplitRight)
		untilPanes(t, p, s, 2)
		ps := s.panes.panes()
		ps[0].term.Feed([]byte("\x1b[2J\x1b[Hroot@web-1:/srv/app# docker compose ps\r\nNAME          STATUS\r\napp-web-1     Up 6 days\r\napp-db-1      Up 6 days\r\nroot@web-1:/srv/app# "))
		ps[1].term.Feed([]byte("root@web-1:~# tail -f access.log\r\n\"GET /health\" 200\r\n\"GET /api/me\" 200\r\n"))
		tt.ClickAt(paneAt(t, a, tt, s, ps[0]))
		tt.Frame()
		splitMenu(t, tt)
		saveShot(t, tt, "split-button-"+lang+".png")
		tt.CloseMenu()
	}
}
