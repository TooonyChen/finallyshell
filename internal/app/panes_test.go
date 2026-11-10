package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// shape writes a pane tree as nested splits, the panes by their names:
// "r(a,d(b,c))" is a beside b over c.
func shape(n *paneNode, names map[*pane]string) string {
	switch {
	case n == nil:
		return ""
	case n.pane != nil:
		return names[n.pane]
	case n.dir == splitRight:
		return "r(" + shape(n.first, names) + "," + shape(n.second, names) + ")"
	}
	return "d(" + shape(n.first, names) + "," + shape(n.second, names) + ")"
}

// The tree: splits nest in both directions, a pane taken out leaves its
// room to its sibling and the focus to the pane nearest it, and a pane
// moves by being taken out and put back beside another. Equalizing
// shares by the panes in a line; the focus moves by direction.
func TestPaneTree(t *testing.T) {
	var tr paneTree
	a, b, c, d := newPane(), newPane(), newPane(), newPane()
	names := map[*pane]string{a: "a", b: "b", c: "c", d: "d"}
	tr.reset(a)
	if !tr.insert(a, b, splitRight, false) || !tr.insert(b, c, splitDown, false) {
		t.Fatal("insert refused")
	}
	if tr.insert(a, b, splitDown, false) || tr.insert(d, newPane(), splitDown, false) {
		t.Fatal("insert of a pane in the tree, or beside one not in it")
	}
	if got := shape(tr.root, names); got != "r(a,d(b,c))" {
		t.Fatalf("tree %s", got)
	}
	if got := tr.panes(); !slices.Equal(got, []*pane{a, b, c}) {
		t.Fatalf("panes %v", got)
	}
	r := tr.rects()
	if r[a] != (paneRect{0, 0, 0.5, 1}) || r[b] != (paneRect{0.5, 0, 0.5, 0.5}) || r[c] != (paneRect{0.5, 0.5, 0.5, 0.5}) {
		t.Fatalf("rects %v", r)
	}

	// Focus by direction: from a, right is b, the first in line; from c,
	// up is b and left is a; nothing past the edges.
	for _, tc := range []struct {
		from *pane
		d    paneDir
		want *pane
	}{
		{a, paneRight, b}, {c, paneUp, b}, {c, paneLeft, a}, {b, paneDown, c},
		{a, paneLeft, nil}, {a, paneUp, nil}, {b, paneRight, nil}, {c, paneDown, nil},
	} {
		if got := tr.neighbor(tc.from, tc.d); got != tc.want {
			t.Errorf("from %s in %d: %s, want %s", names[tc.from], tc.d, names[got], names[tc.want])
		}
	}

	// Moving c beside a, before it: out, then in.
	tr.focus = c
	if next := tr.remove(c); next != b || tr.focus != b {
		t.Fatalf("after removing c, focus %s", names[tr.focus])
	}
	if got := shape(tr.root, names); got != "r(a,b)" {
		t.Fatalf("tree without c %s", got)
	}
	tr.insert(a, c, splitDown, true)
	if got := shape(tr.root, names); got != "r(d(c,a),b)" {
		t.Fatalf("c moved above a: %s", got)
	}
	// Taking a out gives the focus to the sibling's pane nearest it.
	tr.focus = a
	if tr.remove(a); tr.focus != c || shape(tr.root, names) != "r(c,b)" {
		t.Fatalf("focus %s, tree %s", names[tr.focus], shape(tr.root, names))
	}

	// Three side by side get a third each.
	tr.reset(a)
	tr.insert(a, b, splitRight, false)
	tr.insert(b, c, splitRight, false)
	tr.root.pct = 80
	tr.equalize()
	r = tr.rects()
	for _, p := range []*pane{a, b, c} {
		if w := r[p].w; w < 0.333 || w > 0.334 {
			t.Errorf("%s is %.3f wide after equalizing", names[p], w)
		}
	}
	tr.remove(a)
	tr.remove(b)
	if tr.remove(c); tr.root != nil || tr.focus != nil || tr.count() != 0 {
		t.Fatal("the last pane removed left a tree")
	}
}

// paneHost connects a session to a test server serving home, drawn by a
// tester, with the keyboard in its terminal.
func paneHost(t *testing.T, home string, mon bool) (*App, *fakePlatform, *Session, *ui.Tester, *sshtest.Server) {
	t.Helper()
	srv := sshtest.StartSFTP(t, home)
	a, p := testApp(t)
	connectionProbeTimeout = 2 * time.Second
	t.Cleanup(func() { connectionProbeTimeout = 10 * time.Second })
	h := &config.Host{ID: "panes", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "root", Auth: config.AuthPassword, NoMonitor: !mon}
	a.hosts.Add(h)
	a.passwords[h.ID] = sshtest.Password
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	s := a.current()
	p.until(t, "the files", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusFailed || s.files.cwd == home && !s.files.loading
	})
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	t.Cleanup(a.CloseAll)
	t.Cleanup(a.history.pending.Wait)
	tt.ClickAt(760, 200) // in the terminal
	return a, p, s, tt, srv
}

// barPlaceholder is what the command bar shows while empty.
func barPlaceholder(tt *ui.Tester) string {
	info, _ := tt.Access(locales.CmdBarLabel())
	return info.Placeholder
}

// press presses the key of the action id.
func press(tt *ui.Tester, a *App, id shortcutID) {
	k := a.shortcut(id)
	tt.Key(k.Mods, k.Key)
	tt.Frame()
}

// untilPanes waits for s to have n panes.
func untilPanes(t *testing.T, p *fakePlatform, s *Session, n int) {
	t.Helper()
	p.until(t, "the panes", func() bool { return s.panes.count() == n })
}

// paneAt is the middle of pane q of s on the window: the terminal area is
// right of the sidebar and over the command bar.
func paneAt(t *testing.T, a *App, tt *ui.Tester, s *Session, q *pane) (x, y float32) {
	t.Helper()
	bar, ok := tt.Access(locales.CmdBarLabel())
	if !ok {
		t.Fatal("no command bar")
	}
	x0, w := a.sideWidth+1, float32(1280)-a.sideWidth-1
	h := bar.Bounds.Y - 6
	r := s.panes.rects()[q]
	return x0 + (r.x+r.w/2)*w, (r.y + r.h/2) * h
}

// typeInBar sends line from the command bar.
func typeInBar(t *testing.T, tt *ui.Tester, line string) {
	t.Helper()
	focusBar(t, tt)
	tt.Type(line)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
}

// untilInput waits for the server's shells to have read want, each.
func untilInput(t *testing.T, p *fakePlatform, srv *sshtest.Server, want ...string) {
	t.Helper()
	var got []string
	deadline := time.Now().Add(5 * time.Second)
	p.until(t, "the shells' input", func() bool {
		got = got[:0]
		for i := range want {
			got = append(got, srv.ShellInput(i))
		}
		return slices.Equal(got, want) || time.Now().After(deadline)
	})
	if !slices.Equal(got, want) {
		t.Fatalf("the shells read %q, want %q", got, want)
	}
}

// Splitting opens another shell on the same connection, without logging
// in again, in a pane that takes the focus; splits nest. The focus moves
// by click and by the keys, zoom shows the focused pane alone, dividers
// drag within limits and a double click equalizes. The bar, a quick
// command and the command editor send to the focused pane alone.
func TestSplitPanes(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	first := s.pane()
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	second := s.pane()
	if second == first || srv.Logins() != 1 || srv.Shells() != 2 || !s.split() {
		t.Fatalf("focus %v, logins %d, shells %d", second == first, srv.Logins(), srv.Shells())
	}
	if barPlaceholder(tt) != locales.CmdBarFocusedPane() {
		t.Errorf("the bar's placeholder %q", barPlaceholder(tt))
	}
	// The row's menu splits too: the focused pane, down.
	tt.RightClickAt(rowCenter(t, tt, "web-1"))
	if menu := tt.Menu(); !slices.Contains(menu, locales.SplitDown()) || !slices.Contains(menu, locales.ClosePane()) {
		t.Fatalf("row menu %q", menu)
	}
	if err := tt.ChooseMenuItem(locales.SplitDown()); err != nil {
		t.Fatal(err)
	}
	untilPanes(t, p, s, 3)
	third := s.pane()
	names := map[*pane]string{first: "1", second: "2", third: "3"}
	if got := shape(s.panes.root, names); got != "r(1,d(2,3))" || srv.Logins() != 1 || srv.Shells() != 3 {
		t.Fatalf("tree %s, logins %d, shells %d", got, srv.Logins(), srv.Shells())
	}
	if !tt.HasText("3") {
		t.Error("the row shows no pane count")
	}
	if _, ok := tt.Access(locales.PaneCount(3)); !ok {
		t.Error("the row's badge is not named")
	}

	// Focus by the keys, then by a click.
	press(tt, a, scFocusPaneUp)
	if s.pane() != second {
		t.Fatalf("up: focus on %s", names[s.pane()])
	}
	press(tt, a, scFocusPaneLeft)
	if s.pane() != first {
		t.Fatalf("left: focus on %s", names[s.pane()])
	}
	tt.ClickAt(paneAt(t, a, tt, s, third))
	tt.Frame()
	if s.pane() != third {
		t.Fatalf("click: focus on %s", names[s.pane()])
	}

	// What is sent goes to the focused pane's shell: the bar's line, a
	// quick command and the command editor's text.
	typeInBar(t, tt, "uptime")
	untilInput(t, p, srv, "", "", "uptime\r")
	tt.ClickAt(paneAt(t, a, tt, s, first))
	demoQuick(a, false)
	quickTab(t, tt)
	if err := tt.Click("Disk"); err != nil {
		t.Fatal(err)
	}
	untilInput(t, p, srv, "df -h\r", "", "uptime\r")
	tt.ClickAt(paneAt(t, a, tt, s, second))
	showCmdEditor(t, a, tt)
	setCmdText(a, tt, "whoami")
	clickSend(t, tt, locales.QuickSend())
	untilInput(t, p, srv, "df -h\r", "whoami\r", "uptime\r")
	a.bottomTab = tabFiles
	tt.Frame()

	// Zoom: the focused pane alone, and back.
	tt.ClickAt(paneAt(t, a, tt, s, second))
	press(tt, a, scZoomPane)
	if !s.panes.zoomed || s.pane() != second {
		t.Fatal("not zoomed")
	}
	saveShot(t, tt, "panes-zoomed-en.png")
	press(tt, a, scZoomPane)
	if s.panes.zoomed {
		t.Fatal("still zoomed")
	}

	// The root's divider dragged far left stops at the least width; a
	// double click puts it in the middle.
	root := s.panes.root
	_, ay := paneAt(t, a, tt, s, first)
	divX := a.sideWidth + 1 + (1280-a.sideWidth-1)*root.pct/100
	tt.Press(divX, ay)
	tt.Move(divX-200, ay)
	tt.Move(a.sideWidth+5, ay)
	tt.Release(a.sideWidth+5, ay)
	tt.Frame()
	if w := (1280 - a.sideWidth - 1) * root.pct / 100; root.pct >= 50 || w < paneLimitsRight.first-2 {
		t.Fatalf("dragged to %.1f%% (%.0f DIPs), from the middle, at least %.0f", root.pct, w, paneLimitsRight.first)
	}
	divX = a.sideWidth + 1 + (1280-a.sideWidth-1)*root.pct/100
	tt.ClickAt(divX, ay)
	tt.ClickAt(divX, ay)
	tt.Frame()
	if root.pct != 50 {
		t.Errorf("double click: %.1f%%", root.pct)
	}

	// The veil and the border take no input: a click in a pane without
	// the focus reaches its terminal, here as a mouse report to the
	// program that asked for them, and focuses the pane on its way.
	tt.ClickAt(paneAt(t, a, tt, s, third))
	first.term.Feed([]byte("\x1b[?1000h"))
	tt.Frame()
	before := srv.ShellInput(0)
	tt.ClickAt(paneAt(t, a, tt, s, first))
	tt.Frame()
	deadline := time.Now().Add(5 * time.Second)
	p.until(t, "the mouse report", func() bool {
		return strings.HasPrefix(strings.TrimPrefix(srv.ShellInput(0), before), "\x1b[M") || time.Now().After(deadline)
	})
	if got := strings.TrimPrefix(srv.ShellInput(0), before); !strings.HasPrefix(got, "\x1b[M") || s.pane() != first {
		t.Errorf("a click in a pane without the focus: its shell read %q, focus on %s", got, names[s.pane()])
	}
}

// The close key closes the focused pane while there are several, its
// neighbor taking the focus, then the session with the last; a shell
// that exits closes its pane alone.
func TestClosePanes(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	press(tt, a, scSplitDown)
	untilPanes(t, p, s, 3)
	ps := s.panes.panes()

	// The second pane's shell exits: its pane goes, the session stays.
	srv.EndShell(1)
	untilPanes(t, p, s, 2)
	if s.status != statusConnected || s.panes.has(ps[1]) || !s.panes.has(ps[2]) {
		t.Fatalf("status %d after a shell exited", s.status)
	}
	if s.pane() != ps[2] {
		t.Fatal("the focus left the third pane")
	}

	press(tt, a, scCloseTab)
	if s.panes.count() != 1 || s.pane() != ps[0] || !slices.Contains(a.sessions, s) {
		t.Fatalf("%d panes after the close key", s.panes.count())
	}
	if s.split() || barPlaceholder(tt) != "" {
		t.Error("one pane still shown split")
	}
	press(tt, a, scCloseTab)
	if slices.Contains(a.sessions, s) {
		t.Fatal("the last pane's close key left the session")
	}
}

// A dropped connection ends every pane: the session shows Connection
// lost, keeping the layout, and Reconnect opens a shell in each pane,
// on one login. A server that refuses another shell (MaxSessions) adds
// no pane, and says so.
func TestPanesReconnect(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	press(tt, a, scSplitDown)
	untilPanes(t, p, s, 3)
	ps := s.panes.panes()
	names := map[*pane]string{ps[0]: "1", ps[1]: "2", ps[2]: "3"}
	s.panes.root.pct = 30

	srv.DropConnections()
	p.until(t, "the session lost", func() bool { return s.status != statusConnected })
	settle(p, 300*time.Millisecond) // the other panes' ends
	if s.status != statusClosed || s.err != errConnectionLost || s.panes.count() != 3 {
		t.Fatalf("status %d (%v), %d panes", s.status, s.err, s.panes.count())
	}
	if !tt.HasText(locales.ConnectionLost()) {
		t.Errorf("no Connection lost; texts %q", tt.Texts())
	}

	if err := tt.Click(locales.Reconnect()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "connected again", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status != statusConnecting
	})
	if s.status != statusConnected || srv.Logins() != 2 || srv.Shells() != 6 {
		t.Fatalf("status %d (%v), logins %d, shells %d", s.status, s.err, srv.Logins(), srv.Shells())
	}
	if got := shape(s.panes.root, names); got != "r(1,d(2,3))" || s.panes.root.pct != 30 {
		t.Fatalf("layout %s at %.0f%%", got, s.panes.root.pct)
	}
	for _, q := range ps {
		if q.term == nil {
			t.Fatalf("pane %s has no terminal", names[q])
		}
	}
	tt.ClickAt(paneAt(t, a, tt, s, ps[1]))
	typeInBar(t, tt, "ls")
	untilInput(t, p, srv, "", "", "", "", "ls\r", "")

	// No more shells on this connection: the split fails and says so.
	p.until(t, "SFTP tried", func() bool { return s.files.cwd != "" && !s.files.loading })
	srv.LimitSessions(4) // three shells and SFTP
	press(tt, a, scSplitRight)
	p.until(t, "the refusal", func() bool { return tt.HasText(locales.ErrPaneRefused()) })
	if s.panes.count() != 3 || s.status != statusConnected {
		t.Fatalf("%d panes, status %d after a refusal", s.panes.count(), s.status)
	}
}

// "All sessions" sends once to each session, to its focused pane, never
// to its other panes.
func TestPanesSendToAll(t *testing.T) {
	a, p, s, tt, srv := paneHost(t, followHome(t), false)
	press(tt, a, scSplitRight)
	untilPanes(t, p, s, 2)
	srv2 := sshtest.Start(t)
	h2 := &config.Host{ID: "db", Name: "db-1", Addr: srv2.Host, Port: srv2.Port, User: "root", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h2)
	s2 := connectQuiet(t, a, p, h2)
	a.active = slices.Index(a.sessions, s)
	tt.Frame()
	tt.ClickAt(paneAt(t, a, tt, s, s.panes.panes()[0]))
	a.setTarget(targetAll)
	a.sendTo.confirmed = true
	typeInBar(t, tt, "date")
	untilInput(t, p, srv, "date\r", "")
	deadline := time.Now().Add(5 * time.Second)
	p.until(t, "db-1's shell", func() bool { return srv2.ShellInput(0) != "" || time.Now().After(deadline) })
	if got := srv2.ShellInput(0); got != "date\r" || srv2.Shells() != 1 || s2.split() {
		t.Fatalf("db-1 read %q", got)
	}
}

// The file panel follows the focused pane's shell: each pane's follower
// reads its own shell (its pick), and focusing a pane takes the panel to
// its folder.
func TestPanesFollowFocusedShell(t *testing.T) {
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
	if got := ps[1].shellPick(); got.Index != 2 || got.Count != 2 {
		t.Fatalf("the second pane's pick %+v", got)
	}
	tt.ClickAt(paneAt(t, a, tt, s, ps[0]))
	p.until(t, "the panel in a", func() bool { return s.files.cwd == home+"/a" && !s.files.loading })
	press(tt, a, scFocusPaneRight)
	p.until(t, "the panel in b", func() bool { return s.files.cwd == home+"/b" && !s.files.loading })
	if s.shellDir() != home+"/b" {
		t.Errorf("the session's folder %q", s.shellDir())
	}
}

// A local terminal splits into another run of its program; a pane whose
// program exits closes, and the last one's exit ends the tab's as
// before.
func TestLocalPanes(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	press(tt, a, scSplitDown)
	if s.panes.count() != 2 || s.panes.root.dir != splitDown {
		t.Fatalf("%d panes", s.panes.count())
	}
	untilScreen(t, p, s, "ready")
	ps := s.panes.panes()
	ps[1].term.Send([]byte{4}) // cat ends at the end of its input
	untilPanes(t, p, s, 1)
	if s.status != statusConnected || s.pane() != ps[0] {
		t.Fatalf("status %d", s.status)
	}
	ps[0].term.Send([]byte{4})
	p.until(t, "the tab's end", func() bool { return s.status != statusConnected })
	if s.err != errShellExited || s.panes.count() != 1 {
		t.Fatalf("status %d (%v)", s.status, s.err)
	}
}

// The screenshots of the PR: three panes, the row's menu with the panes'
// items, the badge, zoomed; in English and Simplified Chinese, with
// SHOT_DIR set.
func TestPanesScreenshots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		a, p, s, _, _ := paneHost(t, followHome(t), false)
		// The menus drawn in the window, for the screenshot to show them.
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetMenuPresentation(ui.PopoverMenus)
			a.View(c)
		}, 1280, 820)
		p.tt = tt
		tt.SetScale(2)
		p.SetLocale(lang)
		tt.SetLocale(lang)
		press(tt, a, scSplitRight)
		untilPanes(t, p, s, 2)
		press(tt, a, scSplitDown)
		untilPanes(t, p, s, 3)
		ps := s.panes.panes()
		ps[0].term.Feed([]byte("root@web-1:/srv/app# docker compose up -d --build\r\n[+] Running 3/3\r\n \x1b[32m✔\x1b[0m Container app-web-1  \x1b[36mStarted\x1b[0m\r\nroot@web-1:/srv/app# curl -s localhost/health\r\n{\"status\":\"ok\",\"version\":\"2.4.2\"}\r\nroot@web-1:/srv/app# "))
		ps[1].term.Feed([]byte("root@web-1:~# tail -f /var/log/nginx/access.log\r\n10.0.0.7 - - [10/Nov 09:41:02] \"GET /health\" 200\r\n10.0.0.9 - - [10/Nov 09:41:05] \"POST /login\" 302\r\n"))
		ps[2].term.Feed([]byte("  PID USER      CPU% MEM%  Command\r\n 7267 tomcat    33.4 46.2  java -jar app.jar\r\n  812 mysql      1.7  2.9  mysqld\r\n"))
		tt.ClickAt(paneAt(t, a, tt, s, ps[0]))
		tt.Frame()
		saveShot(t, tt, "panes-"+lang+".png")
		tt.RightClickAt(rowCenter(t, tt, "web-1"))
		tt.Frame()
		if menu := tt.Menu(); !slices.Contains(menu, locales.SplitRight()) {
			t.Fatalf("%s: row menu %q", lang, menu)
		}
		saveShot(t, tt, "panes-menu-"+lang+".png")
		tt.CloseMenu()
		press(tt, a, scZoomPane)
		saveShot(t, tt, "panes-zoomed-"+lang+".png")
		if lang == "zh-Hans" && barPlaceholder(tt) != "发到获得焦点的分屏" {
			t.Errorf("the Chinese placeholder %q", barPlaceholder(tt))
		}
	}
}
