package app

import (
	"slices"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The terminal's background shows at (termBgX, termSpotY), right of
// the colored cells; (termBgX, lowSpotY) is below the open bottom panel's
// tab row: the panel's page while it is open, the terminal while it is
// collapsed.
const termBgX, lowSpotY = 1100, 700

// panelTabsY is where the bottom panel's tab row is: the Y of the Files
// label.
func panelTabsY(t *testing.T, tt *ui.Tester) float32 {
	t.Helper()
	r, ok := tt.Find(locales.TabFiles())
	if !ok {
		t.Fatalf("no Files tab; texts %q", tt.Texts())
	}
	return r.Y
}

// The button collapses the bottom panel to its tab row, and the terminal
// takes the height; the state is saved, and the button opens it again on
// the same tab, at the same height.
func TestBottomPanelButton(t *testing.T) {
	a, p, tt, _, _ := panelApp(t)
	termBg := tt.Image().RGBAAt(termBgX, termSpotY)
	openY := panelTabsY(t, tt)
	pct := a.terminalPct
	if _, ok := tt.Find(locales.ShowPanel()); ok {
		t.Fatal("Show panel on an open panel")
	}
	if tt.Image().RGBAAt(termBgX, lowSpotY) == termBg {
		t.Fatal("the terminal under the open panel's tabs")
	}
	p.until(t, "the processes", func() bool { return a.current().procs.loaded })
	tt.Frame()
	if !tt.HasText("7 processes") {
		t.Fatalf("no process page; texts %q", tt.Texts())
	}
	saveShot(t, tt, "panel-open-en.png")

	if err := tt.Click(locales.HidePanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.bottomHidden || !config.LoadSettings().BottomPanelHidden {
		t.Fatalf("hidden %v, saved %v", a.bottomHidden, config.LoadSettings().BottomPanelHidden)
	}
	if y := panelTabsY(t, tt); y < 780 || y <= openY {
		t.Errorf("collapsed tab row at %v, open at %v", y, openY)
	}
	if tt.HasText("7 processes") {
		t.Error("the process page shows in the collapsed panel")
	}
	if got := tt.Image().RGBAAt(termBgX, lowSpotY); got != termBg {
		t.Errorf("the terminal does not take the height: %v, want %v", got, termBg)
	}
	if _, ok := tt.Find(locales.OpenPanelTab()); !ok {
		t.Error("no Open in its own tab on the collapsed row")
	}
	saveShot(t, tt, "panel-collapsed-en.png")

	if err := tt.Click(locales.ShowPanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomHidden || config.LoadSettings().BottomPanelHidden || a.bottomTab != tabProcesses || a.terminalPct != pct {
		t.Errorf("hidden %v, tab %d, terminal %v%% want %v%%", a.bottomHidden, a.bottomTab, a.terminalPct, pct)
	}
	if y := panelTabsY(t, tt); y != openY || !tt.HasText("7 processes") {
		t.Errorf("open again at %v, want %v; texts %q", y, openY, tt.Texts())
	}

	// Chinese.
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "panel-open-zh.png")
	if err := tt.Click("隐藏面板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.bottomHidden || !tt.HasText("显示面板") {
		t.Errorf("hidden %v; texts %q", a.bottomHidden, tt.Texts())
	}
	saveShot(t, tt, "panel-collapsed-zh.png")
}

// The shortcut and the tab row's context menu collapse and open the
// panel; a click on a tab's label opens it on that tab.
func TestBottomPanelShortcutMenuAndTabs(t *testing.T) {
	a, _, tt, _, _ := panelApp(t)
	k := a.shortcut(scBottomPanel)
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if !a.bottomHidden {
		t.Fatal("the shortcut did not collapse the panel")
	}
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if a.bottomHidden {
		t.Fatal("the shortcut did not open the panel")
	}

	if err := tt.RightClick(locales.TabFiles()); err != nil {
		t.Fatal(err)
	}
	if m := tt.Menu(); !slices.Contains(m, locales.HidePanel()) {
		t.Fatalf("menu %q", m)
	}
	if err := tt.ChooseMenuItem(locales.HidePanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.bottomHidden {
		t.Fatal("the menu did not collapse the panel")
	}
	if err := tt.RightClick(locales.TabFiles()); err != nil {
		t.Fatal(err)
	}
	if m := tt.Menu(); !slices.Contains(m, locales.ShowPanel()) || slices.Contains(m, locales.HidePanel()) {
		t.Fatalf("collapsed menu %q", m)
	}
	if err := tt.ChooseMenuItem(locales.ShowPanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomHidden {
		t.Fatal("the menu did not open the panel")
	}

	// Collapsed, the tab of the panel's own (Processes) opens it too.
	for _, tab := range []int{tabNetwork, tabProcesses} {
		a.setBottomPanelHidden(true)
		tt.Frame()
		if err := tt.Click(a.bottomTabs()[tab].label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.bottomHidden || a.bottomTab != tab {
			t.Errorf("tab %d: hidden %v on tab %d", tab, a.bottomHidden, a.bottomTab)
		}
	}

	// Full screen shows the terminal alone: the key does nothing there.
	a.setFullScreen(true)
	tt.Frame()
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if a.bottomHidden {
		t.Error("the shortcut collapsed the panel in full screen")
	}
}

// The process list of the server monitor opens a collapsed panel on the
// Processes tab; a panel row ignores the setting.
func TestBottomPanelOpenedByTabs(t *testing.T) {
	a, _, tt, web, _ := panelApp(t)
	a.bottomTab = tabFiles
	a.setBottomPanelHidden(true)
	tt.Frame()
	if err := tt.Click("mysqld"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomHidden || a.bottomTab != tabProcesses {
		t.Errorf("after the monitor's click: hidden %v, tab %d", a.bottomHidden, a.bottomTab)
	}

	// A panel row is the panel itself: it shows its page while the panel
	// under the terminal is collapsed, without the button or the menu,
	// and its tabs leave the setting alone.
	a.setBottomPanelHidden(true)
	a.openPanelTab(web, tabNetwork)
	tt.Frame()
	if _, ok := tt.Find(locales.ShowPanel()); ok {
		t.Error("Show panel on a panel row")
	}
	if r, ok := tt.Find(locales.BackToTerminal()); !ok || r.Y > 40 {
		t.Errorf("the panel row's tab row at %+v, %v", r, ok)
	}
	k := a.shortcut(scBottomPanel)
	tt.Key(k.Mods, k.Key)
	if err := tt.Click(locales.TabFiles()); err != nil {
		t.Fatal(err)
	}
	tt.Click("mysqld")
	tt.Frame()
	if !a.bottomHidden || a.current().panelTab != tabProcesses {
		t.Errorf("on the panel row: hidden %v, panel tab %d", a.bottomHidden, a.current().panelTab)
	}
}

// bottomPanelHidden is read back at the next start; with no session, the
// start page takes the height.
func TestBottomPanelHiddenRestored(t *testing.T) {
	a, p := testApp(t)
	a.setBottomPanelHidden(true)
	b := New(p)
	if !b.bottomHidden {
		t.Error("not read back")
	}
	tt := ui.NewTester(b.View, 1280, 820)
	tt.Frame()
	if y := panelTabsY(t, tt); y < 780 {
		t.Errorf("collapsed tab row at %v", y)
	}
	if _, ok := tt.Find(locales.ShowPanel()); !ok {
		t.Error("no Show panel")
	}
	saveShot(t, tt, "panel-collapsed-start-en.png")
	a.setBottomPanelHidden(false)
	if b := New(p); b.bottomHidden {
		t.Error("open not read back")
	}
}
