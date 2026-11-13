package app

import (
	"strings"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// fakeFullScreen is the main window's full screen in the system's.
type fakeFullScreen struct {
	full  bool
	calls []bool
}

func (p *fakeFullScreen) SetFullScreen(on bool) { p.full = on; p.calls = append(p.calls, on) }
func (p *fakeFullScreen) IsFullScreen() bool    { return p.full }

// findTerm is a session whose terminal shows lines with "needle" three
// times, and once in the command, with the window showing it.
func findTerm(t *testing.T) (*App, *fakePlatform, *Session, *keyConn, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	demoSession(a, "db-1", "10.0.0.12")
	a.active = 0
	conn := &keyConn{done: make(chan struct{})}
	t.Cleanup(func() { close(conn.done) })
	opts := a.terminalOptions(s.Host)
	opts.Conn = conn
	term, err := terminal.New(opts)
	if err != nil {
		t.Skip("no terminal:", err)
	}
	t.Cleanup(func() { term.Close() })
	s.setTerm(term)
	term.Feed([]byte("[root@web-1 ~]# grep -n needle notes.txt\r\n" +
		"3:a needle in a haystack\r\n" +
		"9:no hay here\r\n" +
		"12:the NEEDLE again, and a third needle\r\n" +
		"[root@web-1 ~]# "))
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.ClickAt(760, 260) // in the terminal
	conn.sent(t, term)
	return a, p, s, conn, tt
}

// The find bar drives the terminal's search: it counts the matches, Enter
// and Shift+Enter go through them, and Escape ends the search, giving the
// terminal the keyboard again.
func TestFindBar(t *testing.T) {
	a, p, s, conn, tt := findTerm(t)
	term := s.term()
	find := a.shortcut(scFind)
	tt.Key(find.Mods, find.Key)
	if !s.pane().find.open || conn.sent(t, term) != "" {
		t.Fatal("the find shortcut opened no find bar")
	}
	tt.Type("needle")
	status := func() terminal.SearchStatus {
		t.Helper()
		var st terminal.SearchStatus
		p.until(t, "the search", func() bool {
			st = term.SearchStatus()
			return st.Text == "needle" && !st.Searching && st.Total > 0
		})
		tt.Frame()
		return st
	}
	if st := status(); st.Total != 4 || st.Selected != 1 {
		t.Fatalf("status %+v", st)
	}
	if conn.sent(t, term) != "" {
		t.Error("typing in the find bar reached the shell")
	}
	if !tt.HasText("1 of 4") {
		t.Errorf("no count; texts %q", tt.Texts())
	}
	saveShot(t, tt, "find-en.png")

	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if st := status(); st.Selected != 2 || !tt.HasText("2 of 4") {
		t.Errorf("Enter: %+v; texts %q", st, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter) // round past the oldest
	tt.Frame()
	if st := status(); st.Selected != 1 {
		t.Errorf("Enter three times more: %+v", st)
	}
	tt.Key(ui.Shift, ui.KeyEnter) // back round to the oldest
	tt.Frame()
	if st := status(); st.Selected != 4 || !tt.HasText("4 of 4") {
		t.Errorf("Shift+Enter: %+v", st)
	}
	if got := conn.sent(t, term); got != "" {
		t.Errorf("the bar's keys sent %q", got)
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("第 4 项，共 4 项") {
		t.Errorf("no count in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "find-zh.png")
	p.SetLocale("en")

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.pane().find.open || term.SearchStatus().Text != "" {
		t.Errorf("Escape: open %v, search %+v", s.pane().find.open, term.SearchStatus())
	}
	tt.Key(ui.Ctrl, ui.KeyC)
	if got := conn.sent(t, term); got != "\x03" {
		t.Errorf("Ctrl+C after the find bar sent %q", got)
	}

	// Opening it again searches for the same text, which the key selects
	// to type over.
	tt.Key(find.Mods, find.Key)
	tt.Frame()
	if st := status(); st.Total != 4 {
		t.Errorf("again: %+v", st)
	}
	tt.Type("zzz")
	p.until(t, "no matches", func() bool { return term.SearchStatus().Text == "zzz" && !term.SearchStatus().Searching })
	tt.Frame()
	if !tt.HasText("No matches") || strings.Contains(s.pane().find.query, "needle") {
		t.Errorf("typed over: query %q; texts %q", s.pane().find.query, tt.Texts())
	}
	if a.current() != s {
		t.Error("the tab changed")
	}
}

// Full screen shows the terminal alone, in the system's full screen; the
// key leaves it, and Escape too where no shell runs.
func TestFullScreen(t *testing.T) {
	a, p, s, conn, tt := findTerm(t)
	panes := []string{"Sessions (2)", locales.TabFiles(), "IP 45.56.78.90"}
	for _, x := range panes {
		if !tt.HasText(x) {
			t.Fatalf("no %q before full screen", x)
		}
	}
	fullScreen := a.shortcut(scFullScreen)
	tt.Key(fullScreen.Mods, fullScreen.Key)
	if !a.fullScreen.on || !p.full {
		t.Fatalf("full screen %v, window %v", a.fullScreen.on, p.full)
	}
	for _, x := range panes {
		if tt.HasText(x) {
			t.Errorf("%q shows in full screen", x)
		}
	}
	tt.ClickAt(640, 300)
	conn.sent(t, s.term())
	tt.Key(0, ui.KeyEscape)
	if got := conn.sent(t, s.term()); got != "\x1b" || !a.fullScreen.on {
		t.Errorf("Escape over the shell: sent %q, full screen %v", got, a.fullScreen.on)
	}
	saveShot(t, tt, "fullscreen-en.png")
	tt.Key(fullScreen.Mods, fullScreen.Key)
	if a.fullScreen.on || p.full {
		t.Fatalf("still full screen %v, window %v", a.fullScreen.on, p.full)
	}
	for _, x := range panes {
		if !tt.HasText(x) {
			t.Errorf("no %q after full screen", x)
		}
	}

	// On a new tab, Escape leaves.
	p.SetLocale("zh-Hans")
	a.NewTab()
	tt.Key(fullScreen.Mods, fullScreen.Key)
	if !a.fullScreen.on || tt.HasText("会话 (2)") || !tt.HasText("快速连接") {
		t.Fatalf("full screen on a new tab; texts %q", tt.Texts())
	}
	saveShot(t, tt, "fullscreen-zh.png")
	tt.Key(0, ui.KeyEscape)
	if a.fullScreen.on || p.full {
		t.Errorf("Escape on a new tab: full screen %v, window %v", a.fullScreen.on, p.full)
	}

	// A window in the system's full screen already stays there; one the
	// app put there that leaves by the system's means ends the app's.
	p.full = true
	tt.Key(fullScreen.Mods, fullScreen.Key)
	tt.Key(fullScreen.Mods, fullScreen.Key)
	if !p.full || a.fullScreen.on {
		t.Errorf("the user's full screen: window %v, app %v", p.full, a.fullScreen.on)
	}
	p.full = false
	tt.Key(fullScreen.Mods, fullScreen.Key)
	p.full = false // the green button
	a.WindowLeftFullScreen()
	tt.Frame()
	if a.fullScreen.on || !tt.HasText("会话 (2)") {
		t.Errorf("left by the system: app %v", a.fullScreen.on)
	}
}
