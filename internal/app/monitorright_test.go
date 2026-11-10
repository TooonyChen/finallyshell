package app

import (
	"slices"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The IP line of panelApp's monitor, on web-1.
const webIP = "IP web-1.example"

// moveMonitor chooses item, "Move to right side" or "Move to left side",
// in the monitor's context menu, which must not have the other.
func moveMonitor(t *testing.T, tt *ui.Tester, item, other string) {
	t.Helper()
	if err := tt.RightClick(webIP); err != nil {
		t.Fatal(err)
	}
	if m := tt.Menu(); len(m) < 2 || m[0] != item || m[1] != "-" || slices.Contains(m, other) {
		t.Fatalf("menu %q", m)
	}
	if err := tt.ChooseMenuItem(item); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// find is where text is, which must show.
func find(t *testing.T, tt *ui.Tester, text string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(text)
	if !ok {
		t.Fatalf("no %q; texts %q", text, tt.Texts())
	}
	return r
}

// The monitor's menu moves it to the right column and back; with the
// panel at the bottom, it has the column alone, full height, and the
// session list fills the left column. The side is saved, monitorPct
// kept.
func TestMonitorOnRight(t *testing.T) {
	a, p, tt, web, _ := panelApp(t)
	cols0, rows0 := web.term().Size()
	if r := find(t, tt, webIP); r.X > a.sideWidth {
		t.Fatalf("monitor at %+v at the start", r)
	}
	listY := find(t, tt, "db-1").Y
	pct := a.monitorPct
	moveMonitor(t, tt, locales.MoveMonitorRight(), locales.MoveMonitorLeft())
	if !a.monitorOnRight() || config.LoadSettings().MonitorSide != config.PanelRight {
		t.Fatalf("side %q, saved %q", a.monitorSide, config.LoadSettings().MonitorSide)
	}
	left := winW - a.rightWidth
	if r := find(t, tt, webIP); r.X < left || r.Y > 40 {
		t.Errorf("monitor at %+v; the column starts at %v", r, left)
	}
	// Full height: the disks, its last part, show near the top half.
	if r := find(t, tt, locales.DiskPath()); r.X < left {
		t.Errorf("disks at %+v", r)
	}
	// The session list takes the left column.
	if r := find(t, tt, "db-1"); r.X > a.sideWidth || r.Y >= listY || r.Y > 120 {
		t.Errorf("db-1 at %+v, was at y %v", r, listY)
	}
	// The terminal over the panel, left of the column.
	if r := find(t, tt, locales.TabFiles()); r.X > left || r.Y < 300 {
		t.Errorf("Files tab at %+v", r)
	}
	if cols, rows := web.term().Size(); cols >= cols0 || rows != rows0 {
		t.Errorf("terminal %dx%d, %dx%d before", cols, rows, cols0, rows0)
	}
	saveShot(t, tt, "monitor-right-panel-bottom-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "monitor-right-panel-bottom-zh.png")
	p.SetLocale("en")
	tt.Frame()

	if b := New(p); !b.monitorOnRight() {
		t.Error("side not read back")
	}

	// A click on its process list still opens the Processes tab.
	a.bottomTab = tabFiles
	if err := tt.Click("mysqld"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomTab != tabProcesses {
		t.Errorf("tab %d after the monitor's click", a.bottomTab)
	}

	// The panel's bottom collapsed: its tab row under the terminal.
	a.setBottomPanelHidden(true)
	tt.Frame()
	if r := find(t, tt, locales.TabFiles()); r.X > left || r.Y < 780 {
		t.Errorf("collapsed tab row at %+v", r)
	}
	if r := find(t, tt, webIP); r.X < left {
		t.Errorf("monitor at %+v with the panel collapsed", r)
	}
	a.setBottomPanelHidden(false)
	tt.Frame()

	// Back to the left, at the size it had.
	moveMonitor(t, tt, locales.MoveMonitorLeft(), locales.MoveMonitorRight())
	if a.monitorOnRight() || config.LoadSettings().MonitorSide != "" {
		t.Fatalf("side %q, saved %q", a.monitorSide, config.LoadSettings().MonitorSide)
	}
	if a.monitorPct != pct {
		t.Errorf("monitorPct %v, was %v", a.monitorPct, pct)
	}
	if r := find(t, tt, webIP); r.X > a.sideWidth {
		t.Errorf("monitor back at %+v", r)
	}
	if r := find(t, tt, "db-1"); r.Y != listY {
		t.Errorf("db-1 back at %+v, was at y %v", r, listY)
	}
	if cols, rows := web.term().Size(); cols != cols0 || rows != rows0 {
		t.Errorf("terminal %dx%d back, %dx%d before", cols, rows, cols0, rows0)
	}
}

// With the panel on the right too, the monitor is on top of the column
// and the panel below, divided at rightMonitorPct: dragged, saved, read
// back and reset by a double click.
func TestMonitorAndPanelOnRight(t *testing.T) {
	a, p, tt, web, _ := panelApp(t)
	a.setMonitorOnRight(true)
	a.setPanelOnRight(true)
	tt.Frame()
	cols0, rows0 := web.term().Size()
	left := winW - a.rightWidth
	ip, files := find(t, tt, webIP), find(t, tt, locales.TabFiles())
	if ip.X < left || ip.Y > 40 {
		t.Errorf("monitor at %+v", ip)
	}
	// The panel starts at rightMonitorPct of the height.
	if want := float32(winH) * defaultRightMonitorPct / 100; files.X < left || files.Y < want || files.Y > want+40 {
		t.Errorf("Files tab at %+v, want under y %v", files, want)
	}
	saveShot(t, tt, "monitor-right-panel-right-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "monitor-right-panel-right-zh.png")
	p.SetLocale("en")
	tt.Frame()

	x := left + a.rightWidth/2
	handle := func() float32 {
		_, y := handleNear(t, tt, x, find(t, tt, locales.TabFiles()).Y, 0, -0.5, ui.CursorResizeNS)
		return y
	}
	y := handle()
	tt.Press(x, y)
	for i := float32(1); i <= 4; i++ {
		tt.Move(x, y+100*i/4)
	}
	tt.Release(x, y+100)
	tt.Frame()
	if a.rightMonitorPct <= defaultRightMonitorPct+5 {
		t.Fatalf("rightMonitorPct %v after the drag", a.rightMonitorPct)
	}
	tt.Advance(time.Second)
	if got := config.LoadSettings().RightMonitorPercent; got != a.rightMonitorPct {
		t.Errorf("saved %v, want %v", got, a.rightMonitorPct)
	}
	if b := New(p); b.rightMonitorPct != a.rightMonitorPct {
		t.Errorf("read back %v, want %v", b.rightMonitorPct, a.rightMonitorPct)
	}
	// The monitor keeps its least height when dragged up all the way.
	y = handle()
	tt.Press(x, y)
	for i := float32(1); i <= 4; i++ {
		tt.Move(x, y-2000*i/4)
	}
	tt.Release(x, 0)
	tt.Frame()
	if got := a.rightMonitorPct / 100 * (winH - 1); got < rightMonitorLimits.first-0.5 {
		t.Errorf("monitor %v DIPs high, the least is %v", got, rightMonitorLimits.first)
	}
	y = handle()
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if a.rightMonitorPct != defaultRightMonitorPct {
		t.Errorf("rightMonitorPct %v after a double click", a.rightMonitorPct)
	}

	// Hidden, the panel collapses to its tabs at the column's bottom,
	// where Show panel is, and the monitor takes the height.
	if err := tt.Click(locales.HidePanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.bottomHidden || tt.HasText("7 processes") {
		t.Fatalf("hidden %v", a.bottomHidden)
	}
	if r := find(t, tt, locales.TabFiles()); r.X < left || r.Y < 780 {
		t.Errorf("collapsed tab row at %+v", r)
	}
	if r := find(t, tt, locales.ShowPanel()); r.X < left || r.Y < 780 {
		t.Errorf("Show panel at %+v", r)
	}
	if r := find(t, tt, webIP); r.X < left || r.Y > 40 {
		t.Errorf("monitor at %+v", r)
	}
	if cols, rows := web.term().Size(); cols != cols0 || rows != rows0 {
		t.Errorf("terminal %dx%d, %dx%d with the panel shown", cols, rows, cols0, rows0)
	}
	saveShot(t, tt, "monitor-right-panel-hidden-en.png")
	// A tab opens it on that tab.
	if err := tt.Click(locales.TabNetwork()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomHidden || a.bottomTab != tabNetwork {
		t.Errorf("hidden %v, tab %d", a.bottomHidden, a.bottomTab)
	}

	// The panel back to the bottom: the monitor fills the column.
	a.setPanelOnRight(false)
	tt.Frame()
	if r := find(t, tt, locales.TabFiles()); r.X > left {
		t.Errorf("Files tab at %+v", r)
	}

	// A panel row's panel has the monitor beside it, on its session.
	a.openPanelTab(web, tabProcesses)
	tt.Frame()
	if r := find(t, tt, locales.BackToTerminal()); r.X > left || r.Y > 40 {
		t.Errorf("the panel row's tab row at %+v", r)
	}
	if r := find(t, tt, webIP); r.X < left {
		t.Errorf("monitor at %+v in a panel row", r)
	}

	// Full screen shows the terminal alone.
	a.active = 0
	a.setFullScreen(true)
	tt.Frame()
	if tt.HasText(webIP) {
		t.Error("the monitor in full screen")
	}
}

// The monitor's context menu on the right.
func TestMonitorRightMenuShots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, tt, _ := monitorPartsApp(t, lang)
			a.setMonitorOnRight(true)
			tt.Frame()
			if err := tt.RightClick("IP 45.56.78.90"); err != nil {
				t.Fatal(err)
			}
			if m := tt.Menu(); len(m) == 0 || m[0] != locales.MoveMonitorLeft() {
				t.Fatalf("menu %q", m)
			}
			tt.Frame()
			saveShot(t, tt, "monitor-right-menu-"+lang+".png")
		})
	}
}
