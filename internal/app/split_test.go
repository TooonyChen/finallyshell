package app

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

const winW, winH = 1280, 820

// divider is one of the window's four split dividers: the size it moves,
// where its handle is and the room its panes share.
type divider struct {
	name     string
	size     *float32
	vertical bool
	percent  bool
	at       func() (x, y float32) // the center of its handle
	room     func() float32        // the DIPs its panes share, less its line
	limits   splitLimits
}

// dips is the first pane's size in DIPs.
func (d divider) dips() float32 {
	if d.percent {
		return *d.size / 100 * d.room()
	}
	return *d.size
}

// dividers finds the four dividers of a's window, winW by winH, from the
// sizes it lays the panes out at. The panes meet at a line one DIP thick;
// the handle the pointer drags is centered on it.
func dividers(a *App, tt *ui.Tester) []divider {
	const h = winH
	mainX := func() float32 { return a.sideWidth + 1 }
	mainW := func() float32 { return winW - mainX() }
	return []divider{
		{
			name: "sidebar", size: &a.sideWidth, limits: sidebarLimits,
			at:   func() (float32, float32) { return a.sideWidth + 0.5, h / 3 },
			room: func() float32 { return winW - 1 },
		},
		{
			name: "monitor", size: &a.monitorPct, vertical: true, percent: true, limits: monitorLimits,
			at:   func() (float32, float32) { return a.sideWidth / 2, a.monitorPct/100*(h-1) + 0.5 },
			room: func() float32 { return h - 1 },
		},
		{
			name: "terminal", size: &a.terminalPct, vertical: true, percent: true, limits: terminalLimits,
			at:   func() (float32, float32) { return mainX() + mainW()/2, a.terminalPct/100*(h-1) + 0.5 },
			room: func() float32 { return h - 1 },
		},
		{
			name: "tree", size: &a.treeWidth, limits: treeLimits,
			at: func() (float32, float32) {
				// Level with the table's header, which is in the file panel.
				box, _ := tt.Find(locales.ColName())
				return mainX() + a.treeWidth + 0.5, box.Y + box.H/2
			},
			room: func() float32 { return mainW() - 1 },
		},
	}
}

// drag drags d's handle by delta DIPs along its axis, in steps.
func (d divider) drag(tt *ui.Tester, delta float32) {
	x, y := d.at()
	tt.Press(x, y)
	for i := float32(1); i <= 4; i++ {
		if d.vertical {
			tt.Move(x, y+delta*i/4)
		} else {
			tt.Move(x+delta*i/4, y)
		}
	}
	if d.vertical {
		tt.Release(x, y+delta)
	} else {
		tt.Release(x+delta, y)
	}
}

// shownWidth is how much of the text s the window shows, in DIPs.
func shownWidth(tt *ui.Tester, s string) (float32, bool) {
	box, ok := tt.Find(s)
	if !ok || box.H <= 0 || box.Y >= winH || box.X >= winW {
		return 0, ok
	}
	return min(box.W, winW-box.X), ok
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.6 }

// splitApp is the window with a connected session showing its files.
func splitApp(t *testing.T) (*App, *fakePlatform, *ui.Tester) {
	a, p := testApp(t)
	demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	tt := ui.NewTester(a.View, winW, winH)
	p.tt = tt
	tt.Frame()
	return a, p, tt
}

func TestDragSplitDividers(t *testing.T) {
	for i, name := range []string{"sidebar", "monitor", "terminal", "tree"} {
		t.Run(name, func(t *testing.T) {
			a, _, tt := splitApp(t)
			d := dividers(a, tt)[i]

			// The pointer shows that it resizes over the handle.
			x, y := d.at()
			tt.Move(x, y)
			want := ui.CursorResizeEW
			if d.vertical {
				want = ui.CursorResizeNS
			}
			if got := tt.Cursor(); got != want {
				t.Fatalf("cursor over the divider at (%.1f, %.1f) is %v, want %v", x, y, got, want)
			}

			before := d.dips()
			d.drag(tt, 60)
			if got := d.dips(); !near(got, before+60) {
				t.Errorf("dragged 60 DIPs: from %.1f to %.1f (size %.2f)", before, got, *d.size)
			}
			d.drag(tt, -100)
			if got := d.dips(); !near(got, before-40) {
				t.Errorf("dragged back 100 DIPs: %.1f, want %.1f", got, before-40)
			}

			// Far past either edge, the divider stops where its panes keep
			// their least sizes: more than the 40 DIPs MyGo keeps.
			d.drag(tt, -2000)
			if got := d.dips(); !near(got, d.limits.first) || got < 40 {
				t.Errorf("dragged to the start: %.1f, want %.0f", got, d.limits.first)
			}
			d.drag(tt, 4000)
			if got, want := d.dips(), d.room()-d.limits.second; !near(got, want) || d.room()-got < 40 {
				t.Errorf("dragged to the end: %.1f, want %.1f", got, want)
			}

			// A double click puts it back at its default.
			d.drag(tt, -150)
			x, y = d.at()
			tt.ClickAt(x, y)
			tt.ClickAt(x, y)
			def := map[string]float32{"sidebar": defaultSideWidth, "monitor": defaultMonitorPct, "terminal": defaultTerminalPct, "tree": defaultTreeWidth}[name]
			if !near(*d.size, def) {
				t.Errorf("double click: size %.2f, want %v", *d.size, def)
			}
		})
	}
}

func TestSplitDividersMoveWithArrows(t *testing.T) {
	a, _, tt := splitApp(t)
	for _, d := range dividers(a, tt) {
		// A click on the handle gives the divider the focus without moving it.
		x, y := d.at()
		before := d.dips()
		tt.ClickAt(x, y)
		if got := d.dips(); got != before {
			t.Errorf("%s: a click moved it from %.1f to %.1f", d.name, before, got)
		}
		back, forth := ui.KeyLeft, ui.KeyRight
		if d.vertical {
			back, forth = ui.KeyUp, ui.KeyDown
		}
		step := theme.Space(2.5)
		tt.Key(0, forth)
		if got := d.dips(); !near(got, before+step) {
			t.Errorf("%s: %v moved it from %.1f to %.1f, want %.1f", d.name, forth, before, got, before+step)
		}
		tt.Key(0, back)
		tt.Key(0, back)
		if got := d.dips(); !near(got, before-step) {
			t.Errorf("%s: %v twice: %.1f, want %.1f", d.name, back, got, before-step)
		}
		// The arrows stop at the least size too.
		d.drag(tt, d.limits.first+2*step-d.dips())
		for range 4 {
			tt.Key(0, back)
		}
		if got := d.dips(); !near(got, d.limits.first) {
			t.Errorf("%s: held %v: %.1f, want %.0f", d.name, back, got, d.limits.first)
		}
	}
}

// sizeConn is a terminal's connection that records the sizes it is told,
// as an SSH session's window-change requests.
type sizeConn struct {
	idleConn
	mu    sync.Mutex
	sizes [][2]int
}

func (c *sizeConn) Resize(cols, rows int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sizes = append(c.sizes, [2]int{cols, rows})
	return nil
}

func (c *sizeConn) last() (cols, rows int, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sizes) == 0 {
		return 0, 0, false
	}
	s := c.sizes[len(c.sizes)-1]
	return s[0], s[1], true
}

// until draws frames until cond holds, for what a goroutine does.
func until(t *testing.T, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
		tt.Frame()
	}
}

func TestDraggingDividersResizesTerminal(t *testing.T) {
	a, _, tt := splitApp(t)
	s := a.current()
	done := make(chan struct{})
	defer close(done)
	conn := &sizeConn{idleConn: idleConn{done}}
	term, err := terminal.New(terminal.Options{Conn: conn, Font: a.termFont, Theme: a.termTheme, DarkTheme: a.termTheme})
	if err != nil {
		t.Skip("no terminal:", err)
	}
	defer term.Close()
	s.term = term
	tt.Frame()

	// told checks that the terminal and the other end have the same size,
	// and returns it.
	told := func(what string) (cols, rows int) {
		t.Helper()
		cols, rows = term.Size()
		until(t, tt, what+" told to the connection", func() bool {
			c, r, ok := conn.last()
			return ok && c == cols && r == rows
		})
		return cols, rows
	}
	cols0, rows0 := told("the first size")
	if cols0 < 80 || rows0 < 10 {
		t.Fatalf("first size %dx%d", cols0, rows0)
	}

	ds := dividers(a, tt)
	ds[0].drag(tt, 200) // a wider sidebar
	cols1, rows1 := told("the narrower size")
	if cols1 >= cols0 || rows1 != rows0 {
		t.Errorf("sidebar 200 DIPs wider: %dx%d, was %dx%d", cols1, rows1, cols0, rows0)
	}
	saveShot(t, tt, "split-sidebar-wide.png")

	ds[2].drag(tt, 150) // a taller terminal
	cols2, rows2 := told("the taller size")
	if rows2 <= rows1 || cols2 != cols1 {
		t.Errorf("terminal 150 DIPs taller: %dx%d, was %dx%d", cols2, rows2, cols1, rows1)
	}

	// The least sizes leave the terminal a few rows, and the other panes
	// what they show.
	for _, i := range []int{0, 1, 2, 3} {
		ds[i].drag(tt, -2000)
	}
	if _, rows := told("the shortest size"); rows < 4 {
		t.Errorf("shortest terminal: %d rows", rows)
	}
	for _, want := range []string{"IP 45.56.78.90", "Sessions (1)", "web-1", "lib64", "libacl.so.1", locales.TabTransfers()} {
		if w, _ := shownWidth(tt, want); w < 20 {
			t.Errorf("least panes: %q hidden (%.1f DIPs wide)", want, w)
		}
	}
	saveShot(t, tt, "split-least.png")
	ds[1].drag(tt, 2000)
	if w, _ := shownWidth(tt, "web-1"); w < 20 {
		t.Error("the tallest monitor hides the sessions")
	}
	ds[0].drag(tt, 2000)
	ds[2].drag(tt, 2000)
	ds[3].drag(tt, 2000)
	if cols, rows := told("the smallest size"); cols < 30 || rows < 10 {
		t.Errorf("smallest terminal with the widest sidebar: %dx%d", cols, rows)
	}
	if box, _ := tt.Find(locales.ColName()); winW-box.X < 200 {
		t.Errorf("the widest tree leaves the table %.1f DIPs", winW-box.X)
	}
	saveShot(t, tt, "split-extremes.png")
}

func TestSplitSizesPersist(t *testing.T) {
	a, _, tt := splitApp(t)
	var saved []config.Settings
	a.saveSettings = func(s config.Settings) error {
		saved = append(saved, s)
		return config.SaveSettings(s)
	}
	ds := dividers(a, tt)
	for _, d := range ds {
		d.drag(tt, 30)
	}
	if len(saved) != 0 {
		t.Errorf("saved %d times while dragging", len(saved))
	}
	tt.Advance(time.Second)
	if len(saved) != 1 {
		t.Fatalf("saved %d times once the dividers rested", len(saved))
	}
	got := config.LoadSettings()
	if got.SidebarWidth != a.sideWidth || got.MonitorPercent != a.monitorPct || got.TerminalPercent != a.terminalPct || got.TreeWidth != a.treeWidth {
		t.Errorf("saved %+v, shown %v %v %v %v", got, a.sideWidth, a.monitorPct, a.terminalPct, a.treeWidth)
	}
	if a.sideWidth == defaultSideWidth || a.treeWidth == defaultTreeWidth {
		t.Errorf("not dragged: %v %v", a.sideWidth, a.treeWidth)
	}

	// The next start takes them back.
	b := New(a.plat)
	if b.sideWidth != a.sideWidth || b.monitorPct != a.monitorPct || b.terminalPct != a.terminalPct || b.treeWidth != a.treeWidth {
		t.Errorf("restored %v %v %v %v, saved %+v", b.sideWidth, b.monitorPct, b.terminalPct, b.treeWidth, got)
	}

	// A move the window closes on is saved too.
	ds[0].drag(tt, 20)
	a.CloseAll()
	if len(saved) != 2 || saved[1].SidebarWidth != a.sideWidth {
		t.Errorf("closing saved %d times, last %+v", len(saved), saved[len(saved)-1])
	}
}

func TestSplitSizesOutOfRangeIgnored(t *testing.T) {
	_, p := testApp(t)
	for _, tc := range []struct {
		s    config.Settings
		want [4]float32
	}{
		{config.Settings{}, [4]float32{defaultSideWidth, defaultMonitorPct, defaultTerminalPct, defaultTreeWidth}},
		{config.Settings{SidebarWidth: 300, MonitorPercent: 40, TerminalPercent: 75, TreeWidth: 150}, [4]float32{300, 40, 75, 150}},
		{config.Settings{SidebarWidth: 5, MonitorPercent: 0, TerminalPercent: 100, TreeWidth: -20}, [4]float32{defaultSideWidth, defaultMonitorPct, defaultTerminalPct, defaultTreeWidth}},
		{config.Settings{SidebarWidth: 1e6, MonitorPercent: -3, TerminalPercent: 250, TreeWidth: 99999}, [4]float32{defaultSideWidth, defaultMonitorPct, defaultTerminalPct, defaultTreeWidth}},
	} {
		if err := config.SaveSettings(tc.s); err != nil {
			t.Fatal(err)
		}
		a := New(p)
		if got := [4]float32{a.sideWidth, a.monitorPct, a.terminalPct, a.treeWidth}; got != tc.want {
			t.Errorf("saved %+v: restored %v, want %v", tc.s, got, tc.want)
		}
	}
	// A file that is not JSON leaves the defaults.
	os.WriteFile(config.Dir()+"/settings.json", []byte("{"), 0o600)
	if a := New(p); a.sideWidth != defaultSideWidth {
		t.Errorf("broken file: sidebar %v", a.sideWidth)
	}
}

// tabDivider is a divider of the Processes or Network tab: where its
// handle is, found anew as it moves, the size it moves, and where the
// settings keep it.
type tabDivider struct {
	name     string
	vertical bool
	at       func() (x, y float32)
	size     *float32
	def      float32
	saved    func(config.Settings) float32
	restored func(*App) float32
}

// handleNear finds a divider's handle from (x, y), moving the pointer by
// (dx, dy) until it shows cursor, and returns the handle's middle.
func handleNear(t *testing.T, tt *ui.Tester, x, y, dx, dy float32, cursor ui.Cursor) (float32, float32) {
	t.Helper()
	x0, y0 := x, y
	for range 200 {
		tt.Move(x, y)
		if tt.Cursor() == cursor {
			n := float32(0)
			for tt.Move(x+dx*(n+1), y+dy*(n+1)); tt.Cursor() == cursor; tt.Move(x+dx*(n+1), y+dy*(n+1)) {
				n++
			}
			return x + dx*n/2, y + dy*n/2
		}
		x, y = x+dx, y+dy
	}
	t.Fatalf("no divider from (%.1f, %.1f) by (%.1f, %.1f)", x0, y0, dx, dy)
	return 0, 0
}

// testTabDividers drags each of ds, waits for the save, starts a new App
// on the same settings, and then puts them back with a double click.
func testTabDividers(t *testing.T, a *App, tt *ui.Tester, ds []tabDivider) {
	t.Helper()
	var saved []config.Settings
	a.saveSettings = func(s config.Settings) error {
		saved = append(saved, s)
		return config.SaveSettings(s)
	}
	for _, d := range ds {
		if *d.size != d.def {
			t.Fatalf("%s: starts at %v, want %v", d.name, *d.size, d.def)
		}
		x, y := d.at()
		tt.Press(x, y)
		for i := float32(1); i <= 4; i++ {
			if d.vertical {
				tt.Move(x, y+10*i)
			} else {
				tt.Move(x+10*i, y)
			}
		}
		if d.vertical {
			tt.Release(x, y+40)
		} else {
			tt.Release(x+40, y)
		}
		if *d.size <= d.def {
			t.Errorf("%s: dragged 40 DIPs, size %v", d.name, *d.size)
		}
	}
	if len(saved) != 0 {
		t.Errorf("saved %d times while dragging", len(saved))
	}
	tt.Advance(time.Second)
	if len(saved) != 1 {
		t.Fatalf("saved %d times once the dividers rested", len(saved))
	}
	got := config.LoadSettings()
	b := New(a.plat)
	for _, d := range ds {
		if v := d.saved(got); v != *d.size {
			t.Errorf("%s: saved %v, shown %v", d.name, v, *d.size)
		}
		if v := d.restored(b); v != *d.size {
			t.Errorf("%s: the next start has %v, saved %v", d.name, v, *d.size)
		}
	}

	// A double click puts them back, and that is saved too.
	for _, d := range ds {
		x, y := d.at()
		tt.ClickAt(x, y)
		tt.ClickAt(x, y)
		if *d.size != d.def {
			t.Errorf("%s: double click: size %v, want %v", d.name, *d.size, d.def)
		}
	}
	tt.Advance(time.Second)
	got = config.LoadSettings()
	b = New(a.plat)
	for _, d := range ds {
		if v := d.saved(got); v != d.def {
			t.Errorf("%s: saved %v after the reset, want %v", d.name, v, d.def)
		}
		if v := d.restored(b); v != d.def {
			t.Errorf("%s: the next start has %v after the reset", d.name, v)
		}
	}
}

func TestProcessesSplitSizesPersist(t *testing.T) {
	a, p, s, _, tt := processesApp(t)
	a.openProcTab(s, s.procs.shown()[0]) // java
	p.until(t, "the details", func() bool { return !s.procs.tabs[0].loading })
	tt.Frame()
	testTabDividers(t, a, tt, []tabDivider{
		{
			name: "list", vertical: true, size: &a.processes.listPct, def: defaultProcListPct,
			at: func() (float32, float32) {
				// Above the details' tabs.
				box, _ := tt.Find("7267-java")
				return handleNear(t, tt, box.X+box.W/2, box.Y, 0, -0.5, ui.CursorResizeNS)
			},
			saved:    func(s config.Settings) float32 { return s.ProcListPercent },
			restored: func(b *App) float32 { return b.processes.listPct },
		},
		{
			name: "fields", size: &a.processes.fieldsPct, def: defaultProcFieldsPct,
			at: func() (float32, float32) {
				// Left of the environment's title.
				box, _ := tt.Find(locales.ProcEnvironment())
				return handleNear(t, tt, box.X, box.Y+box.H/2, -0.5, 0, ui.CursorResizeEW)
			},
			saved:    func(s config.Settings) float32 { return s.ProcFieldsPercent },
			restored: func(b *App) float32 { return b.processes.fieldsPct },
		},
	})
}

func TestNetworkSplitSizePersists(t *testing.T) {
	a, _, s, _, _, tt := networkApp(t)
	// In the panel's tab of its own, which shares the sessions' size.
	a.openPanelTab(s, tabNetwork)
	a.openNetTab(s, netGroup(t, s, "0.0.0.0:80"))
	tt.Frame()
	testTabDividers(t, a, tt, []tabDivider{{
		name: "list", vertical: true, size: &a.network.listPct, def: defaultNetListPct,
		at: func() (float32, float32) {
			// Above the count of the connections, right of the tabs.
			box, _ := tt.Find("4 connections")
			return handleNear(t, tt, box.X+box.W/2, box.Y, 0, -0.5, ui.CursorResizeNS)
		},
		saved:    func(s config.Settings) float32 { return s.NetListPercent },
		restored: func(b *App) float32 { return b.network.listPct },
	}})
}

func TestTabSplitSizesOutOfRangeIgnored(t *testing.T) {
	_, p := testApp(t)
	for _, tc := range []struct {
		s    config.Settings
		want [3]float32
	}{
		{config.Settings{}, [3]float32{defaultProcListPct, defaultProcFieldsPct, defaultNetListPct}},
		{config.Settings{ProcListPercent: 30, ProcFieldsPercent: 70, NetListPercent: 42.5}, [3]float32{30, 70, 42.5}},
		{config.Settings{ProcListPercent: 100, ProcFieldsPercent: -5, NetListPercent: 250}, [3]float32{defaultProcListPct, defaultProcFieldsPct, defaultNetListPct}},
	} {
		if err := config.SaveSettings(tc.s); err != nil {
			t.Fatal(err)
		}
		a := New(p)
		if got := [3]float32{a.processes.listPct, a.processes.fieldsPct, a.network.listPct}; got != tc.want {
			t.Errorf("saved %+v: restored %v, want %v", tc.s, got, tc.want)
		}
	}
}

// TestSSHResize drags dividers in a live session and checks that the
// server's terminal takes the new size:
//
//	FINALLYSHELL_TEST_SSH=tester:secret@127.0.0.1:2222 go test ./internal/app -run SSHResize
func TestSSHResize(t *testing.T) {
	spec := os.Getenv("FINALLYSHELL_TEST_SSH")
	if spec == "" {
		t.Skip("FINALLYSHELL_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	hostname, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)

	a, p := testApp(t)
	h := &config.Host{ID: "test", Addr: hostname, Port: port, User: user, Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.passwords[h.ID] = pass
	tt := ui.NewTester(a.View, winW, winH)
	p.tt = tt
	a.openSession(h)
	s := a.current()
	p.until(t, "the host key question", func() bool { return s.hostKeyQ != nil || s.status == statusFailed })
	if s.status == statusFailed {
		t.Fatal(describeError(s.err))
	}
	s.hostKeyQ.Answer <- true
	s.hostKeyQ = nil
	p.until(t, "the shell", func() bool { return s.status == statusConnected || s.status == statusFailed })
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	defer a.CloseAll()
	p.until(t, "the files", func() bool { return s.files.cwd != "" && !s.files.loading })

	// sttySize asks the shell for its size, which it has from the server's
	// pseudo-terminal: the size the app last told it.
	n := 0
	sttySize := func(what string) (rows, cols int) {
		t.Helper()
		n++
		mark := fmt.Sprintf("size-%d:", n)
		s.term.Send([]byte("echo " + mark + "$(stty size)\r"))
		p.until(t, what, func() bool {
			for line := range strings.SplitSeq(s.term.Text(), "\n") {
				// The output, not the command echoed with it.
				if rest, ok := strings.CutPrefix(strings.TrimSpace(line), mark); ok {
					if _, err := fmt.Sscan(rest, &rows, &cols); err == nil {
						return true
					}
				}
			}
			return false
		})
		return rows, cols
	}
	// checkSize waits for the server to have the terminal's size.
	checkSize := func(what string) (rows, cols int) {
		t.Helper()
		for range 50 {
			wc, wr := s.term.Size()
			if rows, cols = sttySize(what); rows == wr && cols == wc {
				return rows, cols
			}
			tt.Advance(50 * time.Millisecond)
		}
		wc, wr := s.term.Size()
		t.Fatalf("%s: the server says %dx%d, the terminal is %dx%d", what, cols, rows, wc, wr)
		return
	}

	rows0, cols0 := checkSize("the first size")
	ds := dividers(a, tt)
	ds[0].drag(tt, 160)
	rows1, cols1 := checkSize("the size with a wider sidebar")
	if cols1 >= cols0 || rows1 != rows0 {
		t.Errorf("wider sidebar: %dx%d, was %dx%d", cols1, rows1, cols0, rows0)
	}
	ds[2].drag(tt, 120)
	rows2, cols2 := checkSize("the size with a taller terminal")
	if rows2 <= rows1 || cols2 != cols1 {
		t.Errorf("taller terminal: %dx%d, was %dx%d", cols2, rows2, cols1, rows1)
	}
	t.Logf("stty size: %dx%d, %dx%d with a wider sidebar, %dx%d with a taller terminal", cols0, rows0, cols1, rows1, cols2, rows2)
	saveShot(t, tt, "split-ssh.png")
}
