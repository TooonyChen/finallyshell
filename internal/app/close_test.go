package app

import (
	"image/color"
	"slices"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// termSpotX, termSpotY is a point in the terminal area, in DIPs.
const termSpotX, termSpotY = 760, 260

// closeApp opens three sessions whose terminals are filled with a color
// each, and selects the one at selected. It returns the color each
// terminal shows, by session name.
func closeApp(t *testing.T, selected int) (*App, *ui.Tester, map[string]color.RGBA) {
	t.Helper()
	a, p := testApp(t)
	for i, name := range []string{"alpha", "bravo", "charlie"} {
		coloredSession(t, a, name, i)
	}
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	shows := map[string]color.RGBA{}
	for i, s := range a.sessions {
		a.active = i
		tt.Frame()
		shows[s.label()] = tt.Image().RGBAAt(termSpotX, termSpotY)
	}
	if shows["alpha"] == shows["bravo"] || shows["bravo"] == shows["charlie"] || shows["alpha"] == shows["charlie"] {
		t.Fatalf("the terminals look alike: %v", shows)
	}
	a.active = selected
	tt.Frame()
	return a, tt, shows
}

// coloredSession adds a connected session named name whose terminal is
// erased in ANSI background color 1+color (red, green, yellow, blue, …),
// so that a test tells by a pixel which terminal shows.
func coloredSession(t *testing.T, a *App, name string, color int) *Session {
	t.Helper()
	s := demoSession(a, name, name+".example")
	done := make(chan struct{})
	term, err := terminal.New(terminal.Options{Conn: idleConn{done}, Font: a.termFont, Theme: a.termTheme, DarkTheme: a.termTheme})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close(); close(done) })
	s.term = term
	term.Feed([]byte("\x1b[4" + string(rune('1'+color)) + "m\x1b[2J"))
	return s
}

// rowCenter is the middle of the session row of name.
func rowCenter(t *testing.T, tt *ui.Tester, name string) (x, y float32) {
	t.Helper()
	r, ok := tt.Find(name)
	if !ok {
		t.Fatalf("no row %q; texts %q", name, tt.Texts())
	}
	return r.X + r.W/2, r.Y + r.H/2
}

// closeWith closes the session at row with the shortcut, the row's close
// button or its context menu.
var closeWith = map[string]func(t *testing.T, a *App, tt *ui.Tester, row int){
	"shortcut": func(t *testing.T, a *App, tt *ui.Tester, row int) {
		if row != a.active {
			t.Fatal("the shortcut closes the selected session")
		}
		tt.Key(ui.Ctrl|ui.Shift, ui.KeyW)
	},
	"button": func(t *testing.T, a *App, tt *ui.Tester, row int) {
		// The close button shows on the selected row, and on the row
		// under the pointer.
		btn, ok := tt.Find(locales.CloseSession())
		if !ok {
			t.Fatalf("no close button; texts %q", tt.Texts())
		}
		_, y := rowCenter(t, tt, a.sessions[row].label())
		tt.Move(btn.X+btn.W/2, y)
		tt.ClickAt(btn.X+btn.W/2, y)
	},
	"menu": func(t *testing.T, a *App, tt *ui.Tester, row int) {
		tt.RightClickAt(rowCenter(t, tt, a.sessions[row].label()))
		if err := tt.ChooseMenuItem(locales.Close()); err != nil {
			t.Fatalf("%v; menu %q", err, tt.Menu())
		}
	},
}

// TestCloseSessionKeepsOneSelected closes sessions in each way: closing
// the selected one selects the next, or the previous for the last, as tab
// bars do, and closing another keeps the selected one.
func TestCloseSessionKeepsOneSelected(t *testing.T) {
	cases := []struct {
		name            string
		selected, close int
		want            string
	}{
		{"selected middle", 1, 1, "charlie"},
		{"selected first", 0, 0, "bravo"},
		{"selected last", 2, 2, "bravo"},
		{"other above", 1, 0, "bravo"},
		{"other below", 1, 2, "bravo"},
		{"other first, last selected", 2, 0, "charlie"},
	}
	for _, how := range []string{"shortcut", "button", "menu"} {
		for _, k := range cases {
			if how == "shortcut" && k.selected != k.close {
				continue
			}
			t.Run(how+"/"+k.name, func(t *testing.T) {
				a, tt, shows := closeApp(t, k.selected)
				gone := a.sessions[k.close]
				closeWith[how](t, a, tt, k.close)
				tt.Frame()
				if len(a.sessions) != 2 || slices.Contains(a.sessions, gone) {
					t.Fatalf("%s not closed: %d sessions", gone.label(), len(a.sessions))
				}
				cur := a.current()
				if cur == nil {
					t.Fatalf("no session selected (active %d)", a.active)
				}
				if cur.label() != k.want {
					t.Fatalf("selected %s, want %s", cur.label(), k.want)
				}
				if tt.HasText(locales.WelcomeHint(a.shortcutLabel(scManager))) {
					t.Error("the welcome page shows")
				}
				if got := tt.Image().RGBAAt(termSpotX, termSpotY); got != shows[k.want] {
					t.Errorf("the terminal shows %v, want %s's %v", got, k.want, shows[k.want])
				}
				// It stays so in the frames after.
				tt.Frame()
				if c := a.current(); c == nil || c.label() != k.want {
					t.Errorf("selection lost a frame later (active %d)", a.active)
				}
				if how == "button" && k.selected == 1 && k.close == 1 {
					saveShot(t, tt, "close-selected-middle.png")
				}
			})
		}
	}
}
