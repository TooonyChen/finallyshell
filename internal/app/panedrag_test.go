package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// picks are the shell picks of ps, as their followers read them.
func picks(ps ...*pane) []uint64 {
	var out []uint64
	for _, p := range ps {
		out = append(out, p.pick.Load())
	}
	return out
}

// Moving a pane takes it out and puts it on a side of another, which
// splits; it keeps the shell its follower reads (its pick, by the order
// the shells opened in, not by where the pane is), and takes the focus.
// A move that would change nothing, onto itself or onto the side of its
// sibling it is on, changes nothing, the divider's share included.
// Swapping trades two panes' places, picks kept.
func TestPaneMove(t *testing.T) {
	a, b, c := newPane(), newPane(), newPane()
	names := map[*pane]string{a: "a", b: "b", c: "c"}
	// r(a,d(b,c)), the shells opened a, b, c.
	build := func() *paneTree {
		tr := &paneTree{}
		tr.reset(a)
		tr.opened(a)
		tr.insert(a, b, splitRight, false)
		tr.opened(b)
		tr.insert(b, c, splitDown, false)
		tr.opened(c)
		tr.focus = b
		return tr
	}
	want := picks(build().panes()...)
	for _, tc := range []struct {
		side paneDir
		tree string
	}{
		{paneLeft, "r(r(c,a),b)"},
		{paneRight, "r(r(a,c),b)"},
		{paneUp, "r(d(c,a),b)"},
		{paneDown, "r(d(a,c),b)"},
	} {
		tr := build()
		if !tr.move(c, a, tc.side) {
			t.Fatalf("side %d: refused", tc.side)
		}
		if got := shape(tr.root, names); got != tc.tree {
			t.Errorf("c onto side %d of a: %s, want %s", tc.side, got, tc.tree)
		}
		if tr.focus != c || !tr.refocus {
			t.Errorf("side %d: focus on %s", tc.side, names[tr.focus])
		}
		if !slices.Equal(tr.order, []*pane{a, b, c}) || !slices.Equal(picks(a, b, c), want) {
			t.Errorf("side %d: order %v, picks %v, want %v", tc.side, tr.order, picks(a, b, c), want)
		}
		// A shell opened after keeps counting after them.
		d := newPane()
		tr.insert(a, d, splitRight, false)
		tr.opened(d)
		if got := c.shellPick(); got.Index != 3 || got.Count != 4 {
			t.Errorf("side %d: c's pick after another shell %+v", tc.side, got)
		}
	}

	// Nothing to do: onto itself, or onto the side of its sibling it is
	// on already; zoom and the divider stay.
	tr := build()
	tr.root.pct = 30
	for _, tc := range []struct {
		p, at *pane
		side  paneDir
	}{
		{c, c, paneLeft}, {c, b, paneDown}, {b, c, paneUp},
	} {
		if !tr.stays(tc.p, tc.at, tc.side) || tr.move(tc.p, tc.at, tc.side) {
			t.Errorf("%s onto side %d of %s moved", names[tc.p], tc.side, names[tc.at])
		}
	}
	if got := shape(tr.root, names); got != "r(a,d(b,c))" || tr.root.pct != 30 || tr.focus != b {
		t.Fatalf("after moves to nowhere: %s at %.0f%%, focus %s", got, tr.root.pct, names[tr.focus])
	}
	// The other side of the sibling does move it.
	if !tr.move(c, b, paneUp) || shape(tr.root, names) != "r(a,d(c,b))" {
		t.Fatalf("c above b: %s", shape(tr.root, names))
	}

	// Swapping with the neighbor on a side.
	tr = build()
	tr.focus = a
	if !tr.swap(a, paneRight) || shape(tr.root, names) != "r(b,d(a,c))" || tr.focus != a {
		t.Fatalf("a swapped right: %s, focus %s", shape(tr.root, names), names[tr.focus])
	}
	if tr.leaf(a).id != a.id || tr.leaf(b).id != b.id {
		t.Error("the leaves keep the ids of the panes they had")
	}
	if tr.swap(c, paneRight) || !slices.Equal(picks(a, b, c), want) {
		t.Errorf("swap at the edge, or picks %v", picks(a, b, c))
	}
}

// The side of a pane a drop goes to is the one nearest the pointer.
func TestDropSide(t *testing.T) {
	for _, tc := range []struct {
		x, y float32
		want paneDir
	}{
		{10, 100, paneLeft}, {390, 100, paneRight}, {200, 10, paneUp}, {200, 190, paneDown},
		{150, 100, paneLeft}, {300, 100, paneRight}, {200, 60, paneUp}, {200, 140, paneDown},
	} {
		if got := dropSide(tc.x, tc.y, 400, 200); got != tc.want {
			t.Errorf("(%v, %v) in 400×200: %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
	if got := lastLine("one\ntwo  \n   \n\n"); got != "two" {
		t.Errorf("lastLine %q", got)
	}
}

// paneSpot is the point of pane q of s at fx, fy of its width and height,
// on the window.
func paneSpot(t *testing.T, a *App, tt *ui.Tester, s *Session, q *pane, fx, fy float32) (x, y float32) {
	t.Helper()
	bar, ok := tt.Access(locales.CmdBarLabel())
	if !ok {
		t.Fatal("no command bar")
	}
	x0, w := a.sideWidth+1, float32(1280)-a.sideWidth-1
	h := bar.Bounds.Y - 6
	r := s.panes.rects()[q]
	return x0 + (r.x+fx*r.w)*w, (r.y + fy*r.h) * h
}

// gripOf shows q's grip, hovering the top of q, and returns where it is.
func gripOf(t *testing.T, a *App, tt *ui.Tester, s *Session, q *pane) (x, y float32) {
	t.Helper()
	x, _ = paneSpot(t, a, tt, s, q, 0.5, 0)
	_, y0 := paneSpot(t, a, tt, s, q, 0.5, 0)
	tt.Move(x+30, y0+14) // in the strip, off the grip
	tt.Frame()
	g, ok := tt.Find(locales.PaneGrip())
	if !ok {
		t.Fatalf("no grip over the top of the pane; texts %q", tt.Texts())
	}
	return g.X + g.W/2, g.Y + g.H/2
}

// dragGrip drags q's grip to (x, y) in steps, without letting go.
func dragGrip(t *testing.T, a *App, tt *ui.Tester, s *Session, q *pane, x, y float32) {
	t.Helper()
	gx, gy := gripOf(t, a, tt, s, q)
	tt.Press(gx, gy)
	for i := float32(1); i <= 5; i++ {
		tt.Move(gx+(x-gx)*i/5, gy+(y-gy)*i/5)
	}
	tt.Frame()
}

// A pane drags by its grip onto a side of another, which splits there;
// the shells and their terminals stay as they are, and keep the folders
// their followers read. Esc gives a drag up; a drop on the pane itself
// does nothing. The grip shows only with two panes or more, over the
// top of a pane; a drag elsewhere in a pane selects its text, and a
// click in the strip beside the grip reaches the terminal.
func TestDragPane(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	first := s.pane()
	first.term.Feed([]byte("select this line\r\n"))

	// One pane: no grip.
	x, y := paneSpot(t, a, tt, s, first, 0.5, 0)
	tt.Move(x+30, y+14)
	tt.Frame()
	if _, ok := tt.Find(locales.PaneGrip()); ok {
		t.Fatal("a grip with one pane")
	}

	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	press(tt, a, scSplitDown)
	untilPanes(t, p, s, 3)
	ps := s.panes.panes()
	names := map[*pane]string{ps[0]: "1", ps[1]: "2", ps[2]: "3"}
	terms := []any{ps[0].term, ps[1].term, ps[2].term}
	pickBefore := picks(ps...)
	if got := shape(s.panes.root, names); got != "r(1,d(2,3))" {
		t.Fatalf("tree %s", got)
	}

	// Off the strip, no grip.
	tt.Move(paneSpot(t, a, tt, s, ps[2], 0.5, 0.5))
	tt.Frame()
	if _, ok := tt.Find(locales.PaneGrip()); ok {
		t.Fatal("a grip in the middle of a pane")
	}

	// Esc gives it up: over pane 1's right side, nothing moves.
	x, y = paneSpot(t, a, tt, s, ps[0], 0.9, 0.5)
	dragGrip(t, a, tt, s, ps[2], x, y)
	if a.moving != ps[2] {
		t.Fatal("not dragging pane 3")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Release(x, y)
	tt.Frame()
	if got := shape(s.panes.root, names); got != "r(1,d(2,3))" || a.moving != nil {
		t.Fatalf("after Esc: %s", got)
	}

	// Onto itself: nothing.
	x, y = paneSpot(t, a, tt, s, ps[2], 0.2, 0.5)
	dragGrip(t, a, tt, s, ps[2], x, y)
	tt.Release(x, y)
	tt.Frame()
	if got := shape(s.panes.root, names); got != "r(1,d(2,3))" {
		t.Fatalf("dropped on itself: %s", got)
	}

	// Onto each side of pane 1.
	for _, tc := range []struct {
		fx, fy float32
		tree   string
	}{
		{0.9, 0.5, "r(r(1,3),2)"},
		{0.1, 0.5, "r(r(3,1),2)"},
		{0.5, 0.1, "r(d(3,1),2)"},
		{0.5, 0.9, "r(d(1,3),2)"},
	} {
		// Back where it was first: under pane 2.
		if !s.panes.move(ps[2], ps[1], paneDown) && shape(s.panes.root, names) != "r(1,d(2,3))" {
			t.Fatalf("put back: %s", shape(s.panes.root, names))
		}
		s.panes.focus = ps[1]
		tt.Frame()
		x, y = paneSpot(t, a, tt, s, ps[0], tc.fx, tc.fy)
		dragGrip(t, a, tt, s, ps[2], x, y)
		tt.Release(x, y)
		tt.Frame()
		if got := shape(s.panes.root, names); got != tc.tree {
			t.Errorf("dropped at (%.1f, %.1f) of pane 1: %s, want %s", tc.fx, tc.fy, got, tc.tree)
		}
		if s.pane() != ps[2] || a.moving != nil {
			t.Errorf("after the drop, focus on %s", names[s.pane()])
		}
	}
	for i, q := range ps {
		if any(q.term) != terms[i] {
			t.Fatalf("pane %d has another terminal", i+1)
		}
	}
	if got := picks(ps...); !slices.Equal(got, pickBefore) || srv.Shells() != 3 {
		t.Fatalf("picks %v, want %v; shells %d", got, pickBefore, srv.Shells())
	}
	// Each shell still takes what is sent to its pane.
	tt.ClickAt(paneSpot(t, a, tt, s, ps[1], 0.5, 0.5))
	typeInBar(t, tt, "two")
	untilInput(t, p, srv, "", "two\r", "")

	// A drag inside a pane, not from its grip, selects text.
	tt.SetClipboard("")
	x, y = paneSpot(t, a, tt, s, ps[0], 0, 0)
	cell := float32(4) // past the padding, in the first row
	tt.Press(x+8, y+cell+6)
	for i := float32(1); i <= 5; i++ {
		tt.Move(x+8+i*30, y+cell+6)
	}
	tt.Release(x+160, y+cell+6)
	tt.Frame()
	k := a.shortcut(scCopy)
	tt.Key(k.Mods, k.Key)
	tt.Frame()
	if got := tt.Clipboard(); !strings.HasPrefix("select this line", got) || got == "" || a.moving != nil {
		t.Errorf("a drag in the pane copied %q", got)
	}

	// The strip beside the grip is the terminal's: a click there is a
	// mouse report to the program that asked for them.
	ps[0].term.Feed([]byte("\x1b[?1000h"))
	tt.Frame()
	gx, gy := gripOf(t, a, tt, s, ps[0])
	before := srv.ShellInput(0)
	tt.ClickAt(gx+40, gy)
	tt.Frame()
	deadline := time.Now().Add(5 * time.Second)
	p.until(t, "the mouse report", func() bool {
		return strings.HasPrefix(strings.TrimPrefix(srv.ShellInput(0), before), "\x1b[M") || time.Now().After(deadline)
	})
	if got := strings.TrimPrefix(srv.ShellInput(0), before); !strings.HasPrefix(got, "\x1b[M") {
		t.Errorf("a click beside the grip: the shell read %q", got)
	}

	// Zoomed, no grip.
	press(tt, a, scZoomPane)
	tt.Move(gx+30, gy+8)
	tt.Frame()
	if _, ok := tt.Find(locales.PaneGrip()); ok {
		t.Error("a grip while zoomed")
	}
}

// The Move pane keys and the row's menu trade the focused pane's place
// with its neighbor's.
func TestSwapPaneKeys(t *testing.T) {
	a, p, s, tt, _ := paneHost(t, followHome(t), false)
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	ps := s.panes.panes()
	names := map[*pane]string{ps[0]: "1", ps[1]: "2"}
	a.settings.Shortcuts.MovePaneLeft = "Ctrl+Shift+Left"
	tt.Frame() // the window takes the keys of its last frame
	press(tt, a, scMovePaneLeft)
	if got := shape(s.panes.root, names); got != "r(2,1)" || s.pane() != ps[1] {
		t.Fatalf("after the key: %s, focus %s", got, names[s.pane()])
	}
	tt.RightClickAt(rowCenter(t, tt, "web-1"))
	if err := tt.ChooseMenuItem(locales.MovePane(), locales.MovePaneRight()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := shape(s.panes.root, names); got != "r(1,2)" {
		t.Fatalf("after the menu: %s", got)
	}
}

// A pane moved keeps following its own shell's folder: its follower
// reads the shell it opened, wherever the pane goes.
func TestMovedPaneFollowsItsShell(t *testing.T) {
	home := followHome(t)
	fd := &fakeShellDir{dir: home}
	standInShell(t, fd)
	a, p, s, tt, _ := paneHost(t, home, true)
	fd.setPane(1, home+"/a")
	fd.setPane(2, home+"/b")
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	ps := s.panes.panes()
	p.until(t, "the shells' folders", func() bool { return ps[0].shell.dir == home+"/a" && ps[1].shell.dir == home+"/b" })

	// The second pane onto the left of the first.
	x, y := paneSpot(t, a, tt, s, ps[0], 0.1, 0.5)
	dragGrip(t, a, tt, s, ps[1], x, y)
	tt.Release(x, y)
	tt.Frame()
	if got := s.panes.panes(); !slices.Equal(got, []*pane{ps[1], ps[0]}) {
		t.Fatal("not moved")
	}
	if got := ps[1].shellPick(); got.Index != 2 || got.Count != 2 {
		t.Fatalf("the moved pane's pick %+v", got)
	}
	p.until(t, "the panel in b", func() bool { return s.files.cwd == home+"/b" && !s.files.loading })
	// A command in each shell reads its folder again: each pane keeps its
	// own.
	tt.ClickAt(paneSpot(t, a, tt, s, ps[0], 0.5, 0.5))
	typeInBar(t, tt, "pwd")
	p.until(t, "the panel in a", func() bool { return s.files.cwd == home+"/a" && !s.files.loading })
	settle(p, 300*time.Millisecond)
	if ps[0].shell.dir != home+"/a" || ps[1].shell.dir != home+"/b" {
		t.Fatalf("folders %q and %q", ps[0].shell.dir, ps[1].shell.dir)
	}
}

// The screenshots of the PR: a pane's grip, a drag over another pane
// with its four sides, the one under the pointer lit up, and the panes
// after the drop; in English and Simplified Chinese, with SHOT_DIR set.
func TestDragPaneScreenshots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		a, p, s, _, _ := paneHost(t, followHome(t), false)
		tt := ui.NewTester(a.View, 1280, 820)
		p.tt = tt
		tt.SetScale(2)
		p.SetLocale(lang)
		tt.SetLocale(lang)
		press(tt, a, scSplitRight)
		untilPanes(t, p, s, 2)
		press(tt, a, scSplitDown)
		untilPanes(t, p, s, 3)
		ps := s.panes.panes()
		ps[0].term.Feed([]byte("root@web-1:/srv/app# docker compose up -d\r\n[+] Running 3/3\r\n \x1b[32m✔\x1b[0m Container app-web-1  \x1b[36mStarted\x1b[0m\r\nroot@web-1:/srv/app# "))
		ps[1].term.Feed([]byte("root@web-1:~# tail -f /var/log/nginx/access.log\r\n10.0.0.7 - - [10/Nov 09:41:02] \"GET /health\" 200\r\n10.0.0.9 - - [10/Nov 09:41:05] \"POST /login\" 302\r\n"))
		ps[2].term.Feed([]byte("  PID USER      CPU% MEM%  Command\r\n 7267 tomcat    33.4 46.2  java -jar app.jar\r\n  812 mysql      1.7  2.9  mysqld\r\n"))
		tt.ClickAt(paneSpot(t, a, tt, s, ps[1], 0.5, 0.5))
		tt.Frame()

		gx, gy := gripOf(t, a, tt, s, ps[1])
		tt.Move(gx, gy)
		tt.Frame()
		saveShot(t, tt, "pane-grip-"+lang+".png")

		x, y := paneSpot(t, a, tt, s, ps[0], 0.82, 0.45)
		dragGrip(t, a, tt, s, ps[1], x, y)
		tt.Frame()
		saveShot(t, tt, "pane-drag-"+lang+".png")
		tt.Release(x, y)
		tt.Frame()
		if got := s.panes.panes(); !slices.Equal(got, []*pane{ps[0], ps[1], ps[2]}) || s.panes.root.first.pane != nil {
			t.Fatalf("%s: not moved", lang)
		}
		tt.Move(paneSpot(t, a, tt, s, ps[2], 0.5, 0.6))
		tt.Frame()
		saveShot(t, tt, "pane-dropped-"+lang+".png")
	}
}
