package app

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The window of panelApp is 1280 DIPs wide: the right column's left edge
// is at 1280 - its width, and termSpotX is in the terminal beside it.

// moveRight moves the panel to the right with the tab row's context menu.
func moveRight(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.RightClick(locales.TabFiles()); err != nil {
		t.Fatal(err)
	}
	if m := tt.Menu(); !slices.Contains(m, locales.MovePanelRight()) || slices.Contains(m, locales.MovePanelBottom()) {
		t.Fatalf("menu %q", m)
	}
	if err := tt.ChooseMenuItem(locales.MovePanelRight()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// The tab row's context menu moves the panel to the right of the
// terminal, full height, and back under it; the side is saved.
func TestPanelOnRight(t *testing.T) {
	a, p, tt, web, _ := panelApp(t)
	cols0, rows0 := web.term().Size()
	p.until(t, "the processes", func() bool { return a.current().procs.loaded })
	moveRight(t, tt)
	if !a.panelOnRight() || config.LoadSettings().BottomPanelSide != config.PanelRight {
		t.Fatalf("side %q, saved %q", a.bottomSide, config.LoadSettings().BottomPanelSide)
	}
	left := winW - a.rightWidth
	r, ok := tt.Find(locales.TabFiles())
	if !ok || r.X < left || r.Y > 40 {
		t.Errorf("Files tab at %+v, %v; the column starts at %v", r, ok, left)
	}
	if !tt.HasText("7 processes") {
		t.Errorf("no process page; texts %q", tt.Texts())
	}
	// The terminal takes the height, and gives the column its width.
	if cols, rows := web.term().Size(); cols >= cols0 || rows <= rows0 {
		t.Errorf("terminal %dx%d on the right, %dx%d under it", cols, rows, cols0, rows0)
	}
	saveShot(t, tt, "panel-right-processes-en.png")
	a.bottomTab = tabFiles
	tt.Frame()
	tt.Frame()
	saveShot(t, tt, "panel-right-files-en.png")

	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "panel-right-files-zh.png")
	a.bottomTab = tabProcesses
	tt.Frame()
	saveShot(t, tt, "panel-right-processes-zh.png")
	p.SetLocale("en")
	tt.Frame()

	// Restored at the next start.
	if b := New(p); !b.panelOnRight() {
		t.Error("side not read back")
	}

	// Back to the bottom.
	if err := tt.RightClick(locales.TabFiles()); err != nil {
		t.Fatal(err)
	}
	if m := tt.Menu(); !slices.Contains(m, locales.MovePanelBottom()) || slices.Contains(m, locales.MovePanelRight()) {
		t.Fatalf("menu on the right %q", m)
	}
	if err := tt.ChooseMenuItem(locales.MovePanelBottom()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.panelOnRight() || config.LoadSettings().BottomPanelSide != "" {
		t.Fatalf("side %q, saved %q", a.bottomSide, config.LoadSettings().BottomPanelSide)
	}
	if r, ok := tt.Find(locales.TabFiles()); !ok || r.X > left || r.Y < 300 {
		t.Errorf("Files tab back at %+v, %v", r, ok)
	}
}

// The right column's width is dragged, kept from the right edge when the
// window grows, saved, read back, and reset by a double click.
func TestRightPanelWidth(t *testing.T) {
	a, p, tt, _, _ := panelApp(t)
	a.setPanelOnRight(true)
	tt.Frame()
	const y = 400
	// handle is the middle of the divider's handle, found from inside the
	// column.
	handle := func() float32 {
		x, _ := handleNear(t, tt, winW-a.rightWidth+20, y, -0.5, 0, ui.CursorResizeEW)
		return x
	}
	drag := func(by float32) {
		t.Helper()
		x := handle()
		tt.Press(x, y)
		for i := float32(1); i <= 4; i++ {
			tt.Move(x+by*i/4, y)
		}
		tt.Release(x+by, y)
		tt.Frame()
	}
	drag(-100)
	if a.rightWidth != defaultRightWidth+100 {
		t.Fatalf("width %v after the drag, want %v", a.rightWidth, defaultRightWidth+100)
	}
	tt.Advance(time.Second)
	if w := config.LoadSettings().RightPanelWidth; w != a.rightWidth {
		t.Errorf("saved %v, want %v", w, a.rightWidth)
	}
	if b := New(p); b.rightWidth != a.rightWidth {
		t.Errorf("read back %v, want %v", b.rightWidth, a.rightWidth)
	}

	// The terminal keeps its least width, and the column its own.
	drag(-2000)
	if room := winW - a.sideWidth - 1; a.rightWidth > room-rightLimits.second+0.5 {
		t.Errorf("width %v leaves the terminal less than %v", a.rightWidth, rightLimits.second)
	}
	saveShot(t, tt, "panel-right-widest-en.png")
	drag(2000)
	if a.rightWidth != rightLimits.first {
		t.Errorf("width %v, want the least, %v", a.rightWidth, rightLimits.first)
	}
	saveShot(t, tt, "panel-right-narrowest-en.png")

	// A double click puts the default back.
	x := handle()
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if a.rightWidth != defaultRightWidth {
		t.Errorf("width %v after a double click, want %v", a.rightWidth, defaultRightWidth)
	}

	// A wider window gives the room to the terminal.
	tt.SetSize(1600, 820)
	tt.Frame()
	if r, ok := tt.Find(locales.TabFiles()); !ok || r.X < 1600-defaultRightWidth || r.X > 1600-defaultRightWidth+40 {
		t.Errorf("Files tab at %+v in a wider window", r)
	}
	if a.rightWidth != defaultRightWidth {
		t.Errorf("width %v in a wider window", a.rightWidth)
	}
}

// Hidden on the right, the column goes away and a strip keeps the button
// that shows it again; the shortcut, the strip's menu and opening a tab
// work the same.
func TestPanelHiddenOnRight(t *testing.T) {
	a, p, tt, web, _ := panelApp(t)
	a.setPanelOnRight(true)
	tt.Frame()
	cols0, rows0 := web.term().Size()
	if err := tt.Click(locales.HidePanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.bottomHidden || !config.LoadSettings().BottomPanelHidden {
		t.Fatal("not hidden")
	}
	if tt.HasText(locales.TabFiles()) || tt.HasText("7 processes") {
		t.Errorf("the panel shows while hidden; texts %q", tt.Texts())
	}
	// The terminal takes the width, up to the strip.
	if cols, rows := web.term().Size(); cols <= cols0 || rows != rows0 {
		t.Errorf("terminal %dx%d hidden, %dx%d shown", cols, rows, cols0, rows0)
	}
	r, ok := tt.Find(locales.ShowPanel())
	if !ok || r.X < 1240 {
		t.Fatalf("Show panel at %+v, %v", r, ok)
	}
	saveShot(t, tt, "panel-right-hidden-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "panel-right-hidden-zh.png")
	p.SetLocale("en")
	tt.Frame()

	if err := tt.Click(locales.ShowPanel()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomHidden || !tt.HasText(locales.TabFiles()) {
		t.Fatalf("hidden %v after Show panel", a.bottomHidden)
	}

	// The shortcut.
	k := a.shortcut(scBottomPanel)
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if !a.bottomHidden || tt.HasText(locales.TabFiles()) {
		t.Fatal("the shortcut did not hide the panel on the right")
	}

	// Opening a tab, as the monitor's process list does, shows it.
	a.bottomTab = tabFiles
	if err := tt.Click("mysqld"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.bottomHidden || a.bottomTab != tabProcesses || !a.panelOnRight() {
		t.Errorf("after the monitor's click: hidden %v, tab %d, side %q", a.bottomHidden, a.bottomTab, a.bottomSide)
	}

	// The strip's menu moves the hidden panel to the bottom, still
	// collapsed.
	a.setBottomPanelHidden(true)
	tt.Frame()
	if err := tt.RightClick(locales.ShowPanel()); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.MovePanelBottom()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if a.panelOnRight() || !a.bottomHidden {
		t.Errorf("side %q, hidden %v", a.bottomSide, a.bottomHidden)
	}
	if y := panelTabsY(t, tt); y < 780 {
		t.Errorf("collapsed tab row at %v", y)
	}

	// A panel row ignores the side: its panel fills the main area.
	a.setPanelOnRight(true)
	a.setBottomPanelHidden(false)
	a.openPanelTab(web, tabNetwork)
	tt.Frame()
	if r, ok := tt.Find(locales.BackToTerminal()); !ok || r.Y > 40 {
		t.Errorf("the panel row's tab row at %+v, %v", r, ok)
	}
	if r, ok := tt.Find(locales.TabFiles()); !ok || r.X > a.sideWidth+60 {
		t.Errorf("the panel row's Files tab at %+v, %v", r, ok)
	}

	// Full screen shows the terminal alone.
	a.active = 0
	a.setFullScreen(true)
	tt.Frame()
	if tt.HasText(locales.TabFiles()) || tt.HasText(locales.ShowPanel()) {
		t.Errorf("the panel in full screen; texts %q", tt.Texts())
	}
}

// In the tall, narrow column, the Processes and Network tabs' dividers
// still move, are saved and reset, at the default width and at the least.
func TestPanelOnRightTabDividers(t *testing.T) {
	for _, width := range []float32{defaultRightWidth, rightLimits.first} {
		a, p, s, _, tt := processesApp(t)
		a.rightWidth = width
		a.setPanelOnRight(true)
		a.openProcTab(s, s.procs.shown()[0]) // java
		p.until(t, "the details", func() bool { return !s.procs.tabs[0].loading })
		tt.Frame()
		saveShot(t, tt, fmt.Sprintf("panel-right-proc-details-%v-en.png", width))
		// The toolbar keeps its button in the column.
		if r, ok := tt.Find(locales.Refresh()); !ok || r.X+r.W > winW {
			t.Errorf("width %v: Refresh at %+v, %v", width, r, ok)
		}
		testTabDividers(t, a, tt, []tabDivider{
			{
				name: "list", vertical: true, size: &a.processes.listPct, def: defaultProcListPct,
				at: func() (float32, float32) {
					box, _ := tt.Find("7267-java")
					return handleNear(t, tt, box.X+box.W/2, box.Y, 0, -0.5, ui.CursorResizeNS)
				},
				saved:    func(s config.Settings) float32 { return s.ProcListPercent },
				restored: func(b *App) float32 { return b.processes.listPct },
			},
			{
				name: "fields", size: &a.processes.fieldsPct, def: defaultProcFieldsPct,
				at: func() (float32, float32) {
					box, _ := tt.Find(locales.ProcEnvironment())
					return handleNear(t, tt, box.X, box.Y+box.H/2, -0.5, 0, ui.CursorResizeEW)
				},
				saved:    func(s config.Settings) float32 { return s.ProcFieldsPercent },
				restored: func(b *App) float32 { return b.processes.fieldsPct },
			},
		})

		b, _, ns, _, _, nt := networkApp(t)
		b.rightWidth = width
		b.setPanelOnRight(true)
		b.openNetTab(ns, netGroup(t, ns, "0.0.0.0:80"))
		nt.Frame()
		nt.Frame()
		saveShot(t, nt, fmt.Sprintf("panel-right-network-%v-en.png", width))
		testTabDividers(t, b, nt, []tabDivider{{
			name: "list", vertical: true, size: &b.network.listPct, def: defaultNetListPct,
			at: func() (float32, float32) {
				box, _ := nt.Find("4 connections")
				return handleNear(t, nt, box.X+box.W/2, box.Y, 0, -0.5, ui.CursorResizeNS)
			},
			saved:    func(s config.Settings) float32 { return s.NetListPercent },
			restored: func(b *App) float32 { return b.network.listPct },
		}})
	}
}

// In the column at its least width, where the English tabs do not fit,
// the last comes into view with a plain vertical wheel over the tab row,
// down toward it, and up goes back to the first; the buttons stay where
// they are.
func TestPanelTabsWheel(t *testing.T) {
	a, _, tt, _, _ := panelApp(t)
	a.rightWidth = rightLimits.first
	a.setPanelOnRight(true)
	tt.Frame()
	hide, ok := tt.Find(locales.HidePanel())
	if !ok {
		t.Fatal("no Hide panel button")
	}
	// last is the part of the Commands tab's label in view.
	last := func() ui.Rect {
		t.Helper()
		r, ok := tt.Find(locales.TabCommands())
		if !ok {
			t.Fatalf("no Commands tab; texts %q", tt.Texts())
		}
		return r
	}
	// The label's width, all of it in view at the bottom.
	full := func() float32 {
		a.setPanelOnRight(false)
		tt.Frame()
		defer func() { a.setPanelOnRight(true); tt.Frame() }()
		return last().W
	}()
	if r := last(); r.W >= full || a.panelTabsScroll.MaxX <= 0 {
		t.Fatalf("Commands %+v in view at %v DIPs: nothing to scroll", r, a.rightWidth)
	}
	files, _ := tt.Find(locales.TabFiles())
	saveShot(t, tt, "panel-right-tabs-start-en.png")

	y := hide.Y + hide.H/2
	tt.Wheel(files.X+files.W/2, y, 0, 120)
	tt.Advance(time.Second)
	tt.Frame()
	if r := last(); !near(r.W, full) || r.X+r.W > hide.X {
		t.Errorf("Commands at %+v after the wheel (%v wide), the buttons from %v", r, full, hide.X)
	}
	if h, _ := tt.Find(locales.HidePanel()); h != hide {
		t.Errorf("Hide panel moved to %+v from %+v", h, hide)
	}
	saveShot(t, tt, "panel-right-tabs-wheeled-en.png")

	tt.Wheel(files.X+files.W/2, y, 0, -120)
	tt.Advance(time.Second)
	tt.Frame()
	if r, _ := tt.Find(locales.TabFiles()); r.X != files.X {
		t.Errorf("Files at %+v after the wheel back, was %+v", r, files)
	}

	// Over the terminal, the wheel leaves the tabs alone.
	tt.Wheel(termSpotX, termSpotY, 0, 120)
	tt.Advance(time.Second)
	tt.Frame()
	if r, _ := tt.Find(locales.TabFiles()); r.X != files.X {
		t.Errorf("Files at %+v after the wheel over the terminal", r)
	}
}
