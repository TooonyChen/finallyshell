package app

import (
	"image/color"
	"slices"
	"testing"

	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// switchApp opens the rows of steps in order, each selected as it opens,
// as the app selects a new session or a new tab: a session name for a
// connected session with a colored terminal (coloredSession), "+" for a
// new tab. It returns the color each session's terminal shows.
func switchApp(t *testing.T, steps []string) (*App, *ui.Tester, map[*Session]color.RGBA) {
	t.Helper()
	a, p := testApp(t)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	t.Cleanup(a.CloseAll)
	shows := map[*Session]color.RGBA{}
	for _, step := range steps {
		if step == "+" {
			a.NewTab()
		} else {
			coloredSession(t, a, step, len(shows))
			a.active = len(a.sessions) - 1
		}
		tt.Frame()
		if s := a.current(); !s.isNewTab() {
			shows[s] = tt.Image().RGBAAt(termSpotX, termSpotY)
		}
	}
	for s, c := range shows {
		for o, d := range shows {
			if s != o && c == d {
				t.Fatalf("the terminals of %s and %s look alike: %v", s.label(), o.label(), c)
			}
		}
	}
	return a, tt, shows
}

// rowCenters returns the middle of each row of the session list, from
// those of the rows with names of their own: the rows of new tabs all show
// "New tab", and every row is as tall.
func rowCenters(t *testing.T, a *App, tt *ui.Tester) [][2]float32 {
	t.Helper()
	var named []int
	for i, s := range a.sessions {
		if !s.isNewTab() {
			named = append(named, i)
		}
	}
	if len(named) < 2 {
		t.Fatal("rowCenters needs two named rows")
	}
	first, last := named[0], named[len(named)-1]
	x, y0 := rowCenter(t, tt, a.sessions[first].label())
	_, y1 := rowCenter(t, tt, a.sessions[last].label())
	pitch := (y1 - y0) / float32(last-first)
	if pitch < 20 {
		t.Fatalf("rows %.1f apart", pitch)
	}
	var centers [][2]float32
	for i := range a.sessions {
		centers = append(centers, [2]float32{x, y0 + float32(i-first)*pitch})
	}
	for _, i := range named {
		if _, y := rowCenter(t, tt, a.sessions[i].label()); y != centers[i][1] {
			t.Fatalf("row %d (%s) at %.1f, not %.1f", i, a.sessions[i].label(), y, centers[i][1])
		}
	}
	return centers
}

// checkShows fails unless row i is selected and its terminal shows, or
// the start page for a new tab, in this frame and the next.
func checkShows(t *testing.T, a *App, tt *ui.Tester, shows map[*Session]color.RGBA, i int, how string) {
	t.Helper()
	want := a.sessions[i]
	for _, when := range []string{"", " a frame later"} {
		if got := a.current(); got != want {
			t.Fatalf("%s row %d (%s): row %d selected%s", how, i, want.label(), a.active, when)
		}
		px := tt.Image().RGBAAt(termSpotX, termSpotY)
		start := tt.HasText(locales.QuickConnect())
		if want.isNewTab() {
			if !start {
				t.Errorf("%s row %d (new tab): no start page%s", how, i, when)
			}
			for s, c := range shows {
				if px == c {
					t.Errorf("%s row %d (new tab): %s's terminal shows%s", how, i, s.label(), when)
				}
			}
		} else {
			if start {
				t.Errorf("%s row %d (%s): the start page shows%s", how, i, want.label(), when)
			}
			if px != shows[want] {
				t.Errorf("%s row %d (%s): the terminal shows %v, want %v%s", how, i, want.label(), px, shows[want], when)
			}
		}
		tt.Frame()
	}
}

// TestClickSessionRowSelectsIt clicks every row of the session list,
// whatever order its sessions and new tabs opened in, the older ones
// first and back: each click selects the row and shows its terminal, or
// its start page (#38). The keys that switch tabs keep working in
// between.
func TestClickSessionRowSelectsIt(t *testing.T) {
	cases := []struct {
		name  string
		steps []string
	}{
		{"sessions only", []string{"alpha", "bravo", "charlie"}},
		{"sessions then a new tab", []string{"alpha", "bravo", "+"}},
		{"a new tab first", []string{"+", "alpha", "bravo"}},
		{"interleaved", []string{"alpha", "+", "bravo", "+"}},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			a, tt, shows := switchApp(t, k.steps)
			n := len(a.sessions)
			if a.active != n-1 {
				t.Fatalf("the last row opened is not selected (active %d)", a.active)
			}
			centers := rowCenters(t, a, tt)
			order := []int{}
			for i := range n {
				order = append(order, i)
			}
			for i := n - 2; i >= 0; i-- {
				order = append(order, i)
			}
			for _, i := range order {
				tt.ClickAt(centers[i][0], centers[i][1])
				checkShows(t, a, tt, shows, i, "click")
			}
			if k.name == "interleaved" {
				saveShot(t, tt, "click-session-row.png")
			}

			// Row 0 is selected: the keys move from it, and a click after
			// them still selects.
			next, prev, digit := a.shortcut(scNextTab), a.shortcut(scPreviousTab), a.shortcut(scSwitchTab)
			tt.Key(next.Mods, next.Key)
			checkShows(t, a, tt, shows, 1, "next tab to")
			tt.Key(prev.Mods, prev.Key)
			checkShows(t, a, tt, shows, 0, "previous tab to")
			tt.Key(prev.Mods, prev.Key)
			checkShows(t, a, tt, shows, n-1, "previous tab around to")
			tt.Key(digit.Mods, digitKeys[1])
			checkShows(t, a, tt, shows, 1, "switch tab to")
			tt.ClickAt(centers[n-1][0], centers[n-1][1])
			checkShows(t, a, tt, shows, n-1, "click after the keys")
			tt.ClickAt(centers[0][0], centers[0][1])
			checkShows(t, a, tt, shows, 0, "click after the keys")
		})
	}
}

// TestDoubleClickRowReconnects double-clicks a disconnected session's
// row, which reconnects it, as before #38 made the click the list's: a
// single click only selects it, and a double click on a connected
// session's row leaves it connected.
func TestDoubleClickRowReconnects(t *testing.T) {
	a, tt, _ := switchApp(t, []string{"alpha", "bravo", "+"})
	alpha, bravo := a.sessions[0], a.sessions[1]
	bravo.status = statusClosed
	// Refused at once: the reconnect only has to start.
	bravo.Host.Addr, bravo.Host.Port = "127.0.0.1", 1
	a.passwords[bravo.Host.ID] = "secret"
	tt.Frame()
	centers := rowCenters(t, a, tt)

	tt.ClickAt(centers[1][0], centers[1][1])
	tt.Frame()
	if a.current() != bravo || bravo.status != statusClosed {
		t.Fatalf("a click: row %d selected, bravo %v", a.active, bravo.status)
	}
	tt.ClickAt(centers[0][0], centers[0][1])
	tt.ClickAt(centers[0][0], centers[0][1])
	tt.Frame()
	if a.current() != alpha || alpha.status != statusConnected {
		t.Fatalf("a double click on alpha: row %d selected, alpha %v", a.active, alpha.status)
	}
	tt.ClickAt(centers[1][0], centers[1][1])
	tt.ClickAt(centers[1][0], centers[1][1])
	tt.Frame()
	if a.current() != bravo || bravo.status != statusConnecting {
		t.Fatalf("a double click on bravo: row %d selected, bravo %v", a.active, bravo.status)
	}
	if !slices.Contains(a.sessions, bravo) || len(a.sessions) != 3 {
		t.Errorf("reconnecting changed the rows: %d", len(a.sessions))
	}
}
