package app

import (
	"errors"
	"runtime"
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// testShell is the Local terminal program of the tests, so that they do
// not depend on the user's shell: it says it is ready, then echoes what
// it reads, as cat does.
const testShell = `sh -c 'echo ready; cat'`

// localApp is an app with a host and a new tab, whose local terminals run
// shell, drawn by its tester. It skips where no sh runs in a
// pseudo-terminal, or the terminal library cannot load.
func localApp(t *testing.T, shell string) (*App, *fakePlatform, *ui.Tester) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test program is sh")
	}
	if err := terminal.Load(); err != nil {
		t.Skip("no terminal:", err)
	}
	a, p, tt, _ := startApp(t)
	a.settings.LocalShell = shell
	t.Cleanup(a.history.pending.Wait)
	return a, p, tt
}

// untilScreen waits for s's terminal to show want.
func untilScreen(t *testing.T, p *fakePlatform, s *Session, want string) {
	t.Helper()
	p.until(t, "the terminal to show "+want, func() bool {
		return s.term() != nil && strings.Contains(s.term().Text(), want)
	})
}

// headerLocal clicks the session list's Local terminal button.
func headerLocal(t *testing.T, tt *ui.Tester) {
	t.Helper()
	set, ok := tt.Find(locales.Options())
	if !ok {
		t.Fatal("no options button")
	}
	tt.ClickAt(set.X+set.W+2+set.W/2, set.Y+set.H/2)
}

// The header's button opens a local terminal at the end of the list and
// selects it; its program runs, and the row, the monitor and the panel
// say what it is.
func TestLocalTerminalFromHeader(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	web := demoSession(a, "web-1", "203.0.113.10")
	nHosts := len(a.hosts.Live())
	a.active = 1
	tt.Frame()
	if !tt.HasText(locales.OpenPanelTab()) {
		t.Fatal("a session has no Open in its own tab")
	}
	if _, ok := tt.Find(locales.LocalTerminal()); !ok {
		t.Fatalf("no Local terminal button; texts %q", tt.Texts())
	}

	headerLocal(t, tt)
	if len(a.sessions) != 3 || a.active != 2 {
		t.Fatalf("%d rows, active %d", len(a.sessions), a.active)
	}
	s := a.sessions[2]
	if !s.isLocal() || s.isNewTab() || s.Host != nil || s.status != statusConnected || s.term() == nil {
		t.Fatalf("local %v, new tab %v, status %v, err %v", s.isLocal(), s.isNewTab(), s.status, s.err)
	}
	untilScreen(t, p, s, "ready")
	tt.Frame()
	for _, want := range []string{locales.SessionsTitle(2), locales.LocalTerminal(), locales.LocalThisComputer()} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
	if tt.HasText("IP ") || tt.HasText(locales.UptimeUnknown()) {
		t.Errorf("the monitor shows a server; texts %q", tt.Texts())
	}
	if tt.HasText(locales.OpenPanelTab()) {
		t.Error("Open in its own tab offered for a local terminal")
	}
	if !tt.HasText(locales.LocalNotAvailable()) { // the Files tab
		t.Errorf("Files: texts %q", tt.Texts())
	}

	// With "Don't select new tabs", it opens without being selected.
	a.settings.NoAutoSelectTab = true
	a.active = 1
	headerLocal(t, tt)
	if len(a.sessions) != 4 || a.active != 1 || !a.sessions[3].isLocal() || a.current() != web {
		t.Errorf("%d rows, active %d", len(a.sessions), a.active)
	}

	// Nothing of it is saved: the hosts and settings are as they were.
	if len(a.hosts.Live()) != nHosts {
		t.Errorf("%d hosts", len(a.hosts.Live()))
	}
}

// The start page's entry turns its new tab into a local terminal, as
// connecting there does; on the page without hosts too.
func TestLocalTerminalFromStartPage(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	a.setLeftPage(leftConnections) // the header's button is the sessions page's
	tt.Frame()
	tab := a.sessions[0]
	if err := tt.Click(locales.LocalTerminal()); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if len(a.sessions) != 1 || a.sessions[0] != tab || !tab.isLocal() || a.active != 0 {
		t.Fatalf("%d rows, active %d, local %v", len(a.sessions), a.active, tab.isLocal())
	}
	untilScreen(t, p, tab, "ready")

	// No hosts: the empty page has it too.
	b, q := testApp(t)
	b.settings.LocalShell = testShell
	b.NewTab()
	tt2 := ui.NewTester(b.View, 1280, 820)
	q.tt = tt2
	t.Cleanup(b.CloseAll)
	b.setLeftPage(leftConnections)
	tt2.Frame()
	if err := tt2.Click(locales.LocalTerminal()); err != nil {
		t.Fatalf("empty page: %v; texts %q", err, tt2.Texts())
	}
	if len(b.sessions) != 1 || !b.sessions[0].isLocal() {
		t.Fatalf("empty page: %d rows", len(b.sessions))
	}
	untilScreen(t, q, b.sessions[0], "ready")
}

// The command bar and a quick command send to the local shell; the bar
// keeps the lines in a history of its own, and Tab completes nothing.
func TestLocalTerminalSends(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	demoQuick(a, false)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	tt.Frame()

	focusBar(t, tt)
	tt.Type("hello-bar")
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if s.bar.popup != popupNone {
		t.Errorf("Tab opened popup %d", s.bar.popup)
	}
	tt.Key(0, ui.KeyEnter)
	// The pseudo-terminal echoes the line, and cat writes it again.
	p.until(t, "the line twice", func() bool { return strings.Count(s.term().Text(), "hello-bar") >= 2 })
	a.history.pending.Wait()
	if got := a.hostHistory(localHistoryID).cmds; !slices.Equal(got, []string{"hello-bar"}) {
		t.Errorf("history %q", got)
	}
	if list, _ := config.LoadHistory(localHistoryID); !slices.Equal(list, []string{"hello-bar"}) {
		t.Errorf("history file %q", list)
	}
	tt.Key(0, ui.KeyUp) // the history picker
	tt.Frame()
	if s.bar.popup != popupHistory || !tt.HasText("hello-bar") {
		t.Errorf("history picker %d; texts %q", s.bar.popup, tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	quickTab(t, tt)
	tt.Frame()
	quick := a.quick().InGroup("")[0]
	quick.Text = "echo quick-$((40+2))"
	if err := tt.Click(quick.Title()); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	// cat writes back what it read; sh does not run it.
	p.until(t, "the quick command", func() bool { return strings.Count(s.term().Text(), "echo quick-$((40+2))") >= 2 })
	if tt.HasText(locales.LocalNotAvailable()) {
		t.Error("the Commands tab is not available")
	}
}

// Every tab of the bottom panel, under the terminal or on the right, the
// monitor on either side, and the panel hidden: Files, Tunnels,
// Processes and Network, which read the tab's server, say they are not
// available; Transfers shows the queue of every session, as on any tab;
// Commands works; and nothing panics or polls.
func TestLocalTerminalPanels(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	// A server's transfers, which the queue shows whatever tab is shown.
	web := demoSession(a, "web-1", "203.0.113.10")
	for _, j := range []*transferJob{
		{dir: download, src: "/var/log/syslog", dstDir: "/home/me/Downloads", state: transferDone, done: 61_000, total: 61_000, local: "/home/me/Downloads/syslog"},
		{dir: download, src: "/srv/backup.tar.gz", dstDir: "/home/me/Downloads", state: transferFailed, total: -1, err: errors.New("connection lost")},
	} {
		a.transfers.nextID++
		j.id, j.sess = a.transfers.nextID, web
		a.transfers.jobs = append(a.transfers.jobs, j)
	}
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	layouts := []struct {
		name          string
		panel, mon    string
		hidden        bool
		monitorOnLeft bool
	}{
		{"under", "", "", false, true},
		{"right", config.PanelRight, "", false, true},
		{"monitor right", "", config.PanelRight, false, false},
		{"both right", config.PanelRight, config.PanelRight, false, false},
		{"hidden", "", config.PanelRight, true, false},
	}
	for _, l := range layouts {
		a.bottomSide, a.monitorSide, a.bottomHidden = l.panel, l.mon, l.hidden
		for tab := range len(a.bottomTabs()) {
			a.bottomTab = tab
			tt.Frame()
			if l.hidden {
				continue
			}
			na := tt.HasText(locales.LocalNotAvailable())
			if na != (tab != tabCommands && tab != tabTransfers) {
				t.Errorf("%s, tab %d: not available %v; texts %q", l.name, tab, na, tt.Texts())
			}
			if tab == tabCommands && !tt.HasText(locales.SendTo()) {
				t.Errorf("%s: Commands: texts %q", l.name, tt.Texts())
			}
			if tab == tabTransfers {
				if !tt.HasText("syslog") || !tt.HasText("backup.tar.gz") || !tt.HasText(locales.ClearFinished()) {
					t.Errorf("%s: Transfers: texts %q", l.name, tt.Texts())
				}
			}
		}
		if !tt.HasText(locales.LocalTerminal()) || tt.HasText("IP ") {
			t.Errorf("%s: monitor: texts %q", l.name, tt.Texts())
		}
		if len(a.polls) != 0 {
			t.Errorf("%s: %d polls", l.name, len(a.polls))
		}
	}
	// The queue's actions are its transfers' own: Retry asks web-1, which
	// has no SFTP, not the local terminal shown, so it stays off.
	a.bottomSide, a.monitorSide, a.bottomHidden = "", "", false
	a.bottomTab = tabTransfers
	tt.Frame()
	if b, ok := tt.AccessByRole(ui.RoleButton, locales.Retry()); !ok || !b.Disabled {
		t.Errorf("Retry %+v, shown %v", b, ok)
	}
	if err := tt.Click(locales.Retry()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.transfers.jobs) != 2 || a.transfers.jobs[1].state != transferFailed {
		t.Errorf("Retry from a local tab: %d jobs", len(a.transfers.jobs))
	}

	// Full screen shows the terminal alone.
	a.bottomSide, a.monitorSide, a.bottomHidden = "", "", false
	a.setFullScreen(true)
	tt.Frame()
	a.setFullScreen(false)
	tt.Frame()
	if s.status != statusConnected {
		t.Errorf("status %v", s.status)
	}
}

// The row's menu: Duplicate opens another local terminal, Rename names
// it, Disconnect ends the program, Reconnect starts it again, Close
// closes it. Double-clicking the row of one that ended starts it again.
func TestLocalTerminalMenu(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	tt.Frame()

	// Its row is the one that says "This computer"; Rename names it.
	tt.RightClickAt(rowCenter(t, tt, locales.LocalThisComputer()))
	items := tt.Menu()
	for _, want := range []string{locales.Reconnect(), locales.DuplicateSession(), locales.Disconnect(), locales.Rename(), locales.Close()} {
		if !slices.Contains(items, want) {
			t.Errorf("menu %q lacks %q", items, want)
		}
	}
	if err := tt.ChooseMenuItem(locales.Rename()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("scratch")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.label() != "scratch" || !tt.HasText("scratch") {
		t.Fatalf("renamed %q; texts %q", s.label(), tt.Texts())
	}
	if renamedTooltip(s) != locales.LocalTerminal() {
		t.Errorf("tooltip %q", renamedTooltip(s))
	}
	s.setName(locales.LocalTerminal())
	if s.name != "" {
		t.Errorf("the default name kept as %q", s.name)
	}
	s.setName("scratch")
	tt.Frame()
	row := func() (float32, float32) { return rowCenter(t, tt, "scratch") }

	// Duplicate: another local terminal, a program of its own.
	tt.RightClickAt(row())
	if err := tt.ChooseMenuItem(locales.DuplicateSession()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.sessions) != 3 || !a.sessions[2].isLocal() || a.sessions[2] == s || a.current() != a.sessions[2] {
		t.Fatalf("duplicate: %d rows, active %d", len(a.sessions), a.active)
	}
	dup := a.sessions[2]
	untilScreen(t, p, dup, "ready")
	if dup.term() == s.term() || dup.label() != locales.LocalTerminal() {
		t.Errorf("duplicate %q", dup.label())
	}
	a.active = 1
	tt.Frame()

	// Disconnect, then Reconnect.
	tt.RightClickAt(row())
	if err := tt.ChooseMenuItem(locales.Disconnect()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.status != statusClosed || s.term() != nil || !tt.HasText(locales.StatusDisconnected()) {
		t.Fatalf("disconnect: status %v; texts %q", s.status, tt.Texts())
	}
	tt.RightClickAt(row())
	if err := tt.ChooseMenuItem(locales.Reconnect()); err != nil {
		t.Fatal(err)
	}
	if s.status != statusConnected || s.term() == nil {
		t.Fatalf("reconnect: status %v, err %v", s.status, s.err)
	}
	untilScreen(t, p, s, "ready")

	// Close: the program ends with the tab.
	term := s.term()
	tt.Frame()
	tt.RightClickAt(row())
	if err := tt.ChooseMenuItem(locales.Close()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if slices.Contains(a.sessions, s) || s.term() != nil {
		t.Fatalf("close: %d rows", len(a.sessions))
	}
	p.until(t, "the program to end", func() bool {
		select {
		case <-term.Done():
			return true
		default:
			return false
		}
	})
	p.drain()
	if s.status != statusClosed && s.status != statusConnected {
		t.Errorf("status %v after close", s.status)
	}
}

// When the program exits the tab shows Exited, with Reconnect, which
// starts it again in the same tab, as Enter in the terminal area does.
func TestLocalTerminalExit(t *testing.T) {
	a, p, tt := localApp(t, `sh -c 'echo ready; read line; exit 3'`)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	s.term().Send([]byte("bye\r"))
	p.until(t, "the program to exit", func() bool { return s.status == statusClosed })
	tt.Frame()
	if !errors.Is(s.err, errShellExited) || !tt.HasText(locales.StatusExited()) || !tt.HasText(locales.Reconnect()) {
		t.Fatalf("err %v; texts %q", s.err, tt.Texts())
	}
	if n := a.liveSessions(); n != 0 {
		t.Errorf("%d live after the exit", n)
	}
	if err := tt.Click(locales.Reconnect()); err != nil {
		t.Fatal(err)
	}
	if s.status != statusConnected || a.current() != s || len(a.sessions) != 2 {
		t.Fatalf("status %v, rows %d", s.status, len(a.sessions))
	}
	untilScreen(t, p, s, "ready")
	s.term().Send([]byte("bye\r"))
	p.until(t, "the program to exit again", func() bool { return s.status == statusClosed })
	tt.Frame()
	tt.ClickAt(760, 200) // the terminal area, which takes Enter
	k := a.shortcut(scReconnect)
	tt.Key(k.Mods, k.Key)
	if s.status != statusConnected {
		t.Fatalf("Enter: status %v", s.status)
	}
	untilScreen(t, p, s, "ready")
}

// The setting is split into words as a shell would; a program that is
// not there, or a quote not closed, shows its error in the tab.
func TestLocalTerminalSetting(t *testing.T) {
	a, p, tt := localApp(t, `sh -c "echo 'two words'; cat"`)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "two words")

	a.settings.LocalShell = "no-such-program-for-finallyshell --flag"
	a.openLocal(nil)
	bad := a.current()
	tt.Frame()
	var le *localStartError
	if bad.status != statusFailed || !errors.As(bad.err, &le) || le.program != "no-such-program-for-finallyshell" {
		t.Fatalf("status %v, err %v", bad.status, bad.err)
	}
	if msg := describeError(bad.err); !tt.HasText(msg) || !strings.Contains(msg, "no-such-program-for-finallyshell") {
		t.Errorf("no %q; texts %q", msg, tt.Texts())
	}
	if !tt.HasText(locales.StatusFailed()) {
		t.Errorf("row: texts %q", tt.Texts())
	}

	a.settings.LocalShell = `sh -c 'echo`
	a.reconnect(bad)
	tt.Frame()
	if bad.status != statusFailed || !tt.HasText(locales.ErrLocalShellQuote(`sh -c 'echo`)) {
		t.Errorf("quote: status %v; texts %q", bad.status, tt.Texts())
	}

	// Fixed, Reconnect starts it.
	a.settings.LocalShell = testShell
	if err := tt.Click(locales.Reconnect()); err != nil {
		t.Fatal(err)
	}
	untilScreen(t, p, bad, "ready")
}

// All sessions skips local terminals; Choose sessions lists them, and a
// local terminal checked there is sent to. Current session sends to a
// local terminal as to any tab.
func TestLocalTerminalSendTarget(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	web, conn := addTerm(t, a, "web-1", "203.0.113.10", false)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	tt.Frame()

	if got := a.allSessions(); !slices.Equal(got, []*Session{web}) {
		t.Errorf("all sessions %v", got)
	}
	if got := a.targetSessions(); !slices.Equal(got, []*Session{web, s}) {
		t.Errorf("target sessions %v", got)
	}
	a.setTarget(targetAll)
	if got := a.sendTargets(s); !slices.Equal(got, []*Session{web}) {
		t.Errorf("All from the local terminal: %v", got)
	}
	if a.targetLabel(true) != locales.SendToAllCount(1) {
		t.Errorf("label %q", a.targetLabel(true))
	}

	// Choose sessions lists it, with its mark and name.
	quickTab(t, tt)
	a.sendTo.picking = "quick"
	tt.Frame()
	tip := locales.SendToTip(locales.LocalTerminal())
	if _, ok := tt.Find(tip); !ok {
		t.Fatalf("no %q in the popover; texts %q", tip, tt.Texts())
	}
	saveShot(t, tt, "local-choose-en.png")
	if err := tt.Click(tip); err != nil {
		t.Fatal(err)
	}
	if a.sendTo.mode != targetChosen || !a.sendTo.chosen[s.ID] {
		t.Fatalf("mode %v, chosen %v", a.sendTo.mode, a.sendTo.chosen)
	}
	a.sendTo.picking = ""
	tt.Frame()
	if got := a.sendTargets(web); !slices.Equal(got, []*Session{s}) {
		t.Errorf("chosen: %v", got)
	}
	// The question names it.
	a.sendToTargets(web, func(t *Session) { a.sendLine(t, "chosen-line", true) }, nil)
	tt.Frame()
	if a.sendTo.ask == nil || !tt.HasText(locales.LocalThisComputer()) {
		t.Fatalf("no question; texts %q", tt.Texts())
	}
	if err := tt.Click(locales.QuickSend()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the chosen line", func() bool { return strings.Count(s.term().Text(), "chosen-line") >= 2 })
	if got := conn.sent(t, web.term()); got != "" {
		t.Errorf("web-1 got %q", got)
	}

	// Current session.
	a.setTarget(targetCurrent)
	if got := a.sendTargets(s); !slices.Equal(got, []*Session{s}) {
		t.Errorf("current: %v", got)
	}
}

// Quitting asks about a local terminal whose program runs, in a line of
// its own.
func TestLocalTerminalQuit(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	demoSession(a, "web-1", "203.0.113.10")
	a.openLocal(nil)
	tt.Frame()
	if n := a.liveSessions(); n != 2 {
		t.Fatalf("%d live", n)
	}
	p.answer = false
	if a.QuitRequested() {
		t.Fatal("quit with a local terminal open")
	}
	untilAsked(t, a, p, 1)
	want := locales.ConfirmQuitBody(1) + "\n" + locales.ConfirmQuitLocal(1)
	if q := p.questions()[0]; q.Detail != want {
		t.Errorf("detail %q, want %q", q.Detail, want)
	}
}

// Options › Terminal has the program's field, which saves what is typed,
// in both languages.
func TestLocalTerminalOption(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	if err := tt.Click(locales.OptionsTerminal()); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{locales.OptionLocalShell(), locales.OptionLocalShellNote()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	f, ok := tt.AccessByRole(ui.RoleTextField, locales.OptionLocalShell())
	if !ok {
		t.Fatalf("no field; texts %q", tt.Texts())
	}
	tt.ClickAt(f.Bounds.X+f.Bounds.W/2, f.Bounds.Y+f.Bounds.H/2)
	tt.Type("pwsh -NoLogo")
	tt.Frame()
	if a.settings.LocalShell != "pwsh -NoLogo" || config.LoadSettings().LocalShell != "pwsh -NoLogo" {
		t.Errorf("setting %q, saved %q", a.settings.LocalShell, config.LoadSettings().LocalShell)
	}
	if cmd, _ := a.settings.LocalShellCommand(); !slices.Equal(cmd, []string{"pwsh", "-NoLogo"}) {
		t.Errorf("command %q", cmd)
	}
	saveShot(t, tt, "local-option-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("本地终端程序") {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "local-option-zh.png")
	p.SetLocale("en")
}

// The local terminal's scenes, in both languages: its tab on the
// Commands, Files and Transfers tabs beside sessions of servers, and the
// start page with its entry.
func TestLocalTerminalShots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p, tt := localApp(t, `env PS1='you@laptop ~ % ' sh -i`)
			p.SetLocale(lang)
			demoQuick(a, lang == "zh-Hans")
			demoArchiveTransfers(a, demoSession(a, "web-1", "45.56.78.90"))
			demoSession(a, "db-1", "10.0.0.12").status = statusConnecting
			a.openLocal(nil)
			s := a.current()
			untilScreen(t, p, s, "you@laptop")
			s.term().Send([]byte("echo \"$((6*7)) on this computer\"\r"))
			untilScreen(t, p, s, "42 on this computer")
			a.bottomTab = tabCommands
			tt.Frame()
			saveShot(t, tt, "local-commands-"+lang+".png")
			a.bottomTab = tabFiles
			tt.Frame()
			saveShot(t, tt, "local-files-"+lang+".png")
			a.bottomTab = tabTransfers
			tt.Frame()
			if tt.HasText(locales.LocalNotAvailable()) || !tt.HasText("node_modules") {
				t.Errorf("Transfers: texts %q", tt.Texts())
			}
			saveShot(t, tt, "local-transfers-"+lang+".png")
			a.active = 0 // the new tab
			tt.Frame()
			saveShot(t, tt, "local-start-"+lang+".png")
		})
	}
}
