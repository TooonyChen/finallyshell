package app

import (
	"slices"
	"strconv"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// addTerm is one more session, connected unless closed, whose terminal
// writes to the conn returned.
func addTerm(t *testing.T, a *App, name, addr string, closed bool) (*Session, *keyConn) {
	t.Helper()
	s := demoSession(a, name, addr)
	if closed {
		s.status = statusClosed
	}
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
	return s, conn
}

// sendFarm is barTerm's web-1 and three more rows: web-2, connected,
// db-1, not connected, and a new tab; web-1 has a panel row too.
type sendFarm struct {
	a      *App
	p      *fakePlatform
	tt     *ui.Tester
	web1   *Session
	web2   *Session
	db1    *Session
	c1, c2 *keyConn
	c3     *keyConn
}

func newSendFarm(t *testing.T) *sendFarm {
	t.Helper()
	a, p, s, conn, tt := barTerm(t)
	f := &sendFarm{a: a, p: p, tt: tt, web1: s, c1: conn}
	f.web2, f.c2 = addTerm(t, a, "web-2", "45.56.78.91", false)
	f.db1, f.c3 = addTerm(t, a, "db-1", "10.0.0.12", true)
	a.NewTab()
	a.openPanelTab(s, tabCommands)
	a.active = 0
	tt.Frame()
	return f
}

// sent is what each session's shell got since the last call: web-1,
// web-2, db-1.
func (f *sendFarm) sent(t *testing.T) []string {
	t.Helper()
	return []string{f.c1.sent(t, f.web1.term()), f.c2.sent(t, f.web2.term()), f.c3.sent(t, f.db1.term())}
}

func (f *sendFarm) want(t *testing.T, what string, want ...string) {
	t.Helper()
	if got := f.sent(t); !slices.Equal(got, want) {
		t.Errorf("%s: sent %q, want %q", what, got, want)
	}
}

// With All sessions, a quick command and a line of the bar reach every
// connected session once, the panel row not doubling its session, and
// skip the new tab and the session not connected. The first send asks;
// Cancel sends nothing, and the sends after Send don't ask again. Each
// host's history gets the lines.
func TestSendTargetAll(t *testing.T) {
	f := newSendFarm(t)
	a, tt := f.a, f.tt
	demoQuick(a, false)
	a.bottomTab = tabCommands // the panel row's name says Commands too
	tt.Frame()
	if err := tt.Click(locales.SendToCurrent()); err != nil {
		t.Fatalf("no Send to: %v; texts %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem(locales.SendToAll()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if a.sendTo.mode != targetAll || !tt.HasText(locales.SendToAllSessionsCount(2)) || !tt.HasText(locales.SendToAllCount(2)) {
		t.Fatalf("target %d; texts %q", a.sendTo.mode, tt.Texts())
	}
	saveShot(t, tt, "send-to-all-en.png")

	// The first send asks, naming the sessions; Cancel sends nothing.
	if err := tt.Click("Disk"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sendTo.ask == nil || !tt.HasText(locales.SendToConfirm(2)) || !tt.HasText("web-2") {
		t.Fatalf("no question: %q", tt.Texts())
	}
	if names := askNames(a); !slices.Equal(names, []string{"web-1", "web-2"}) {
		t.Errorf("asked for %q", names)
	}
	saveShot(t, tt, "send-to-confirm-en.png")
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	f.want(t, "after Cancel", "", "", "")
	if a.sendTo.ask != nil {
		t.Fatal("the question stayed")
	}

	// Asked again, as nothing was sent; Send sends to each once.
	tt.Click("Disk")
	tt.Frame()
	if a.sendTo.ask == nil {
		t.Fatal("not asked again after Cancel")
	}
	if err := tt.Click(locales.QuickSend()); err != nil {
		t.Fatal(err)
	}
	f.want(t, "Disk", "df -h\r", "df -h\r", "")

	// The next sends don't ask.
	tt.Click("Memory")
	if a.sendTo.ask != nil {
		t.Fatal("asked again for the same target")
	}
	f.want(t, "Memory", "free -m\r", "free -m\r", "")

	// From the panel row: its session once.
	a.active = slices.IndexFunc(a.sessions, (*Session).isPanelTab)
	tt.Frame()
	if err := tt.Click("Ports"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	f.want(t, "from the panel row", "ss -tlnp\r", "ss -tlnp\r", "")

	// The bar: each session gets the line, the bar is emptied.
	a.active = 0
	tt.Frame()
	focusBar(t, tt)
	tt.Type("uptime")
	tt.Key(0, ui.KeyEnter)
	if a.sendTo.ask != nil {
		t.Fatal("the bar asked")
	}
	f.want(t, "the bar", "uptime\r", "uptime\r", "")
	tt.Frame()
	if f.web1.bar.text != "" {
		t.Errorf("bar holds %q", f.web1.bar.text)
	}

	a.history.pending.Wait()
	for _, s := range []*Session{f.web1, f.web2} {
		if got := a.hostHistory(s.Host.ID).cmds; !slices.Equal(got, []string{"df -h", "free -m", "ss -tlnp", "uptime"}) {
			t.Errorf("%s's history %q", s.label(), got)
		}
	}
	if got := a.hostHistory(f.db1.Host.ID).cmds; len(got) != 0 {
		t.Errorf("db-1's history %q", got)
	}

	// Back to Current session, and All again: asks again.
	a.setTarget(targetCurrent)
	tt.Frame()
	tt.Click("Disk")
	if a.sendTo.ask != nil {
		t.Fatal("Current session asked")
	}
	f.want(t, "Current session", "df -h\r", "", "")
	a.setTarget(targetAll)
	tt.Frame()
	tt.Click("Disk")
	tt.Frame()
	if a.sendTo.ask == nil {
		t.Fatal("not asked after the target changed")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.sendTo.ask != nil {
		t.Fatal("Escape kept the question")
	}
	f.want(t, "after Escape", "", "", "")

	f.p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "send-to-all-zh.png")
	tt.Click("Disk")
	tt.Frame()
	saveShot(t, tt, "send-to-confirm-zh.png")
	f.p.SetLocale("en")
}

// askNames are the names of the sessions the question lists.
func askNames(a *App) []string {
	var out []string
	for _, s := range a.sendTo.ask.sessions {
		out = append(out, s.label())
	}
	return out
}

// Choose sessions…, from the bar's button: the sessions checked get the
// line, after the question; a session closed drops out of the set, and
// with none left the target is the current session again.
func TestSendTargetChosen(t *testing.T) {
	f := newSendFarm(t)
	a, tt := f.a, f.tt
	tip := locales.SendToTip(locales.SendToCurrent())
	if err := tt.Click(tip); err != nil {
		t.Fatalf("no target button: %v; texts %q", err, tt.Texts())
	}
	if got := tt.Menu(); !slices.Equal(got, []string{locales.SendToCurrent(), locales.SendToAll(), locales.SendToChoose()}) {
		t.Errorf("menu %q", got)
	}
	if err := tt.ChooseMenuItem(locales.SendToChoose()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sendTo.picking == "" {
		t.Fatal("no popover")
	}
	// Every session is listed, with the one not connected; not the new
	// tab nor the panel row.
	for _, name := range []string{"web-1", "web-2", "db-1"} {
		if _, ok := tt.Access(locales.SendToTip(name)); !ok {
			t.Errorf("%s not listed", name)
		}
	}
	if _, ok := tt.Access(locales.SendToTip(locales.NewTab())); ok {
		t.Error("the new tab is listed")
	}
	if err := tt.Click(locales.SendToTip("web-2")); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.SendToTip("db-1")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.sendTo.mode != targetChosen || len(a.sendTo.chosen) != 2 || !tt.HasText(locales.SendToSessions(2)) {
		t.Fatalf("target %+v; texts %q", a.sendTo, tt.Texts())
	}
	saveShot(t, tt, "send-to-choose-en.png")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.sendTo.picking != "" {
		t.Fatal("Escape kept the popover")
	}

	focusBar(t, tt)
	tt.Type("hostname")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.sendTo.ask == nil {
		t.Fatal("no question")
	}
	if names := askNames(a); !slices.Equal(names, []string{"web-2"}) {
		t.Errorf("asked for %q, db-1 not connected", names)
	}
	if f.web1.bar.text != "hostname" {
		t.Errorf("the bar lost its line while asking: %q", f.web1.bar.text)
	}
	saveShot(t, tt, "send-to-chosen-bar-en.png")
	tt.Click(locales.QuickSend())
	tt.Frame()
	f.want(t, "chosen", "", "hostname\r", "")
	if f.web1.bar.text != "" {
		t.Errorf("bar holds %q", f.web1.bar.text)
	}
	tt.Type("id")
	tt.Key(0, ui.KeyEnter)
	f.want(t, "chosen again", "", "id\r", "")

	f.p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "send-to-chosen-bar-zh.png")
	a.sendTo.picking = "bar-" + strconv.FormatInt(f.web1.ID, 10)
	tt.Frame()
	saveShot(t, tt, "send-to-choose-zh.png")
	a.sendTo.picking = ""
	f.p.SetLocale("en")

	// Closing web-2 and db-1 empties the set: Current session again.
	a.closeSession(f.web2)
	tt.Frame()
	if a.sendTo.mode != targetChosen || !tt.HasText(locales.SendToSessions(1)) {
		t.Errorf("after closing web-2: %+v", a.sendTo)
	}
	a.closeSession(f.db1)
	tt.Frame()
	if a.sendTo.mode != targetCurrent || len(a.sendTo.chosen) != 0 {
		t.Errorf("after closing both: %+v", a.sendTo)
	}
}

// The target is not saved: a new App sends to the current session.
func TestSendTargetNotSaved(t *testing.T) {
	a, p := testApp(t)
	a.setTarget(targetAll)
	if b := New(p); b.sendTo.mode != targetCurrent || b.sendTo.confirmed {
		t.Errorf("new App's target %+v", b.sendTo)
	}
}
