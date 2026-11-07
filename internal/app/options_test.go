package app

import (
	"net"
	"slices"
	"sync"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"
)

// fakeSystem stands in for the tray, the message box and quitting.
type fakeSystem struct {
	noTray bool // the system has none, as Linux without AppIndicator
	inTray bool // the main window is hidden in the tray
	answer bool // what Confirm answers
	quits  int

	mu    sync.Mutex // Confirm runs on a goroutine
	asked []Confirmation
}

func (p *fakeSystem) HideToTray() bool {
	if p.noTray {
		return false
	}
	p.inTray = true
	return true
}

func (p *fakeSystem) Confirm(q Confirmation) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, q)
	return p.answer
}

// questions are the questions Confirm was asked.
func (p *fakeSystem) questions() []Confirmation {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.asked)
}

func (p *fakeSystem) Quit() { p.quits++ }

// The options window's tree shows every page, in both languages; the
// pages still to come say so.
func TestOptionsPages(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	for _, pg := range optionPages {
		if !tt.HasText(pg.title()) {
			t.Errorf("no entry %q; texts %q", pg.title(), tt.Texts())
		}
	}
	for _, s := range []string{locales.OptionAutoSelectTab(), locales.OptionMinimizeToTray(), locales.OptionConfirmClose(), locales.FollowSystem()} {
		if !tt.HasText(s) {
			t.Errorf("the General page has no %q", s)
		}
	}
	saveShot(t, tt, "options-general-en.png")

	// A page still to come says so, while there is one.
	var later *optionPage
	for i := range optionPages {
		if optionPages[i].view == nil {
			later = &optionPages[i]
			break
		}
	}
	if later != nil {
		if err := tt.Click(later.title()); err != nil {
			t.Fatal(err)
		}
		if a.optionsNav.Page != later.id || !tt.HasText(locales.OptionsComingLater()) || tt.HasText(locales.OptionAutoSelectTab()) {
			t.Fatalf("page %q; texts %q", a.optionsNav.Page, tt.Texts())
		}
		saveShot(t, tt, "options-placeholder-en.png")
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"常规", "终端", "鼠标", "快捷键", "配色", "背景图片", "字体"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	if later != nil {
		if !tt.HasText("这些选项将在以后的版本中提供。") {
			t.Errorf("no placeholder in Chinese; texts %q", tt.Texts())
		}
		saveShot(t, tt, "options-placeholder-zh.png")
	}
	if err := tt.Click("常规"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"自动选中标签", "关闭时最小化到托盘", "关闭窗口前确认", "界面语言"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	if got := p.windows[WindowOptions].spec.Title(); got != "选项" {
		t.Errorf("title %q", got)
	}
	saveShot(t, tt, "options-general-zh.png")
}

// Each option of the General page applies at once and is saved.
func TestGeneralOptionsPersist(t *testing.T) {
	a, p := testApp(t)
	if a.settings != (config.Settings{}) {
		t.Fatalf("a new app's settings %+v", a.settings)
	}
	a.openOptions()
	tt := p.show(t, WindowOptions)
	for _, label := range []string{locales.OptionAutoSelectTab(), locales.OptionMinimizeToTray(), locales.OptionConfirmClose()} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	want := config.Settings{NoAutoSelectTab: true, MinimizeToTray: true, NoConfirmClose: true}
	if a.settings != want || config.LoadSettings() != want {
		t.Fatalf("settings %+v, saved %+v", a.settings, config.LoadSettings())
	}

	// A new run reads them back, and the page shows them.
	b := New(p)
	if b.settings != want {
		t.Fatalf("read back %+v", b.settings)
	}
	for _, label := range []string{locales.OptionAutoSelectTab(), locales.OptionMinimizeToTray(), locales.OptionConfirmClose()} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if a.settings != (config.Settings{}) || config.LoadSettings() != (config.Settings{}) {
		t.Fatalf("back to the defaults: %+v, saved %+v", a.settings, config.LoadSettings())
	}
}

// refusedHost is a host whose connection is refused at once.
func refusedHost(t *testing.T, a *App, name string) *config.Host {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	h := &config.Host{ID: config.NewHostID(), Name: name, Addr: "127.0.0.1", Port: port, User: "root", Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.passwords[h.ID] = "secret"
	return h
}

func TestAutoSelectTab(t *testing.T) {
	for _, tc := range []struct {
		name       string
		noAuto     bool
		open       int // sessions open before
		wantActive int
	}{
		{"on", false, 2, 2},
		{"off", true, 2, 0},
		{"off with none open", true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, p := testApp(t)
			a.settings.NoAutoSelectTab = tc.noAuto
			for i := range tc.open {
				demoSession(a, "open", "10.0.0."+string(rune('1'+i)))
			}
			if tc.open > 0 {
				a.active = 0
			}
			s := newSessionFor(t, a, p, refusedHost(t, a, "new"))
			if a.active != tc.wantActive {
				t.Errorf("active %d, want %d", a.active, tc.wantActive)
			}
			if a.sessions[len(a.sessions)-1] != s {
				t.Error("the new session is not the last tab")
			}
		})
	}
}

// newSessionFor opens a session for h and waits for its connection to
// end, refused.
func newSessionFor(t *testing.T, a *App, p *fakePlatform, h *config.Host) *Session {
	t.Helper()
	a.openSession(h)
	s := a.sessions[len(a.sessions)-1]
	p.until(t, "the refusal", func() bool { return s.status == statusFailed })
	return s
}

// drain runs what the app posted, as the UI thread does.
func (p *fakePlatform) drain() {
	for {
		select {
		case fn := <-p.posts:
			fn()
		default:
			return
		}
	}
}

// untilAsked waits for the app to ask whether to quit, and runs what it
// posts with the answer.
func untilAsked(t *testing.T, a *App, p *fakePlatform, asked int) {
	t.Helper()
	p.until(t, "the question", func() bool { return len(p.questions()) == asked && !a.askingQuit })
}

func TestCloseAndQuit(t *testing.T) {
	type step struct {
		quit     bool // the tray's Quit, else the close button
		answer   bool // to the question, if asked
		allowed  bool // the window closes, or the app quits, at once
		asked    bool
		quits    bool // the app quit after the answer
		hides    bool
		sessions int
	}
	for _, tc := range []struct {
		name            string
		tray, noConfirm bool
		noTray          bool
		step
	}{
		{name: "close, no sessions", step: step{allowed: true}},
		{name: "close, sessions, yes", step: step{sessions: 1, answer: true, asked: true, quits: true}},
		{name: "close, sessions, no", step: step{sessions: 2, asked: true}},
		{name: "close, sessions, no confirm", noConfirm: true, step: step{sessions: 1, allowed: true}},
		{name: "close to tray", tray: true, step: step{sessions: 1, hides: true}},
		{name: "close to tray, no sessions", tray: true, step: step{hides: true}},
		{name: "close, tray on but none", tray: true, noTray: true, step: step{sessions: 1, asked: true, answer: true, quits: true}},
		{name: "quit, no sessions", tray: true, step: step{quit: true, allowed: true}},
		{name: "quit, sessions, yes", tray: true, step: step{quit: true, sessions: 1, answer: true, asked: true, quits: true}},
		{name: "quit, sessions, no", tray: true, step: step{quit: true, sessions: 1, asked: true}},
		{name: "quit, sessions, no confirm", tray: true, noConfirm: true, step: step{quit: true, sessions: 1, allowed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, p := testApp(t)
			a.settings.MinimizeToTray, a.settings.NoConfirmClose = tc.tray, tc.noConfirm
			p.noTray, p.answer = tc.noTray, tc.answer
			for range tc.sessions {
				demoSession(a, "web", "10.0.0.1")
			}
			a.openOptions()
			var allowed bool
			if tc.quit {
				allowed = a.QuitRequested()
			} else {
				allowed = a.CloseRequested()
			}
			if allowed != tc.allowed {
				t.Fatalf("allowed %v", allowed)
			}
			if tc.asked {
				untilAsked(t, a, p, 1)
				q := p.questions()[0]
				if q.Detail != locales.ConfirmQuitBody(tc.sessions) || q.OK != locales.ConfirmQuitOK() || q.Cancel != locales.Cancel() {
					t.Errorf("asked %+v", q)
				}
			}
			p.drain()
			if q := p.questions(); !tc.asked && len(q) != 0 {
				t.Errorf("asked %+v", q)
			}
			if got := p.quits > 0; got != tc.quits {
				t.Errorf("quit %v", got)
			}
			if p.inTray != tc.hides {
				t.Errorf("in the tray %v", p.inTray)
			}
			if tc.hides && a.windowOpen(WindowOptions) {
				t.Error("the options window stayed open behind the tray")
			}
			// Once the app goes ahead, the quit it starts closes the
			// window without asking again; before, a second close asks
			// again, after the first answer.
			if tc.allowed || tc.quits {
				if !a.QuitRequested() || !a.CloseRequested() {
					t.Error("the quit went ahead, then was stopped")
				}
			}
		})
	}
}

// A second close while the question shows asks no second question.
func TestCloseAsksOnce(t *testing.T) {
	a, p := testApp(t)
	demoSession(a, "web", "10.0.0.1")
	block := make(chan struct{})
	p.answer = false
	a.plat = blockingConfirm{p, block}
	if a.CloseRequested() || a.CloseRequested() || a.QuitRequested() {
		t.Fatal("closed with a session open")
	}
	close(block)
	untilAsked(t, a, p, 1)
	if a.CloseRequested() {
		t.Fatal("closed after No")
	}
	untilAsked(t, a, p, 2)
}

// blockingConfirm answers once block is closed.
type blockingConfirm struct {
	*fakePlatform
	block chan struct{}
}

func (b blockingConfirm) Confirm(q Confirmation) bool {
	<-b.block
	return b.fakePlatform.Confirm(q)
}

// Only connected and connecting sessions count as open.
func TestLiveSessions(t *testing.T) {
	a, _ := testApp(t)
	for _, st := range []sessionStatus{statusConnected, statusConnecting, statusFailed, statusClosed} {
		demoSession(a, "web", "10.0.0.1").status = st
	}
	if n := a.liveSessions(); n != 2 {
		t.Errorf("%d open", n)
	}
	a.sessions = nil
	if !a.QuitRequested() {
		t.Error("asked with no session open")
	}
}
