package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/localterm"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func adminApp(t *testing.T) (*App, *fakePlatform, *ui.Tester, *atomic.Int32) {
	t.Helper()
	a, p, _ := localApp(t, testShell)
	closed := &atomic.Int32{}
	cmd, err := config.SplitCommandLine(testShell, false)
	if err != nil {
		t.Fatal(err)
	}
	a.localAdmin = localAdminState{initialized: true, supported: true, start: func(context.Context, []string, string) (localterm.Process, error) {
		return testAdminProcess(cmd, func() { closed.Add(1) }), nil
	}}
	tt := ui.NewTester(func(c *ui.Context) { c.SetMenuPresentation(ui.PopoverMenus); a.View(c) }, 1280, 820)
	p.tt = tt
	tt.Frame()
	return a, p, tt, closed
}

func openAdmin(t *testing.T, a *App, p *fakePlatform) *Session {
	t.Helper()
	a.openAdminLocal(nil)
	p.until(t, "administrator terminal launch", func() bool { return a.localAdmin.pending == nil })
	s := a.current()
	if !s.isLocal() || !s.localElevated || s.term() == nil {
		t.Fatalf("not administrator local: %#v", s)
	}
	untilScreen(t, p, s, "ready")
	return s
}

func TestAdminLocalMenuAndRename(t *testing.T) {
	a, p, tt, _ := adminApp(t)
	a.setLeftPage(leftConnections)
	tt.Frame()
	if err := tt.Click(locales.LocalTerminalPrivileges()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.LocalRegular()) || !tt.HasText(locales.LocalOpenAdministrator()) {
		t.Fatalf("menu: %q", tt.Texts())
	}
	if err := tt.Click(locales.LocalOpenAdministrator()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "menu launch", func() bool { return a.localAdmin.pending == nil })
	s := a.current()
	if !s.localElevated || len(a.sessions) != 1 {
		t.Fatalf("not converted: %d", len(a.sessions))
	}
	untilScreen(t, p, s, "ready")
	s.setName("maintenance")
	tt.Frame()
	if !tt.HasText("maintenance") || !tt.HasText(locales.LocalAdministrator()) {
		t.Fatalf("no persistent badge: %q", tt.Texts())
	}
	if !strings.Contains(s.target(), locales.LocalAdministrator()) {
		t.Fatalf("ambiguous send target %q", s.target())
	}
}

func TestAdminLocalCancelPreservesTab(t *testing.T) {
	a, p, tt, closed := adminApp(t)
	tab := a.current()
	release := make(chan struct{})
	a.localAdmin.start = func(context.Context, []string, string) (localterm.Process, error) {
		<-release
		return nil, localterm.ErrCanceled
	}
	a.openAdminLocal(tab)
	tt.Frame()
	if !tab.isNewTab() || len(a.sessions) != 1 {
		t.Fatal("tab changed before authorization")
	}
	close(release)
	p.until(t, "canceled authorization", func() bool { return a.localAdmin.pending == nil })
	if !tab.isNewTab() || len(a.sessions) != 1 || closed.Load() != 0 {
		t.Fatal("cancellation replaced the original tab")
	}
}

func TestAdminLocalLateResultDisposed(t *testing.T) {
	for _, how := range []string{"close-tab", "close-app", "ordinary-terminal"} {
		t.Run(how, func(t *testing.T) {
			a, p, _, closed := adminApp(t)
			tab := a.current()
			release := make(chan struct{})
			cmd, _ := config.SplitCommandLine(testShell, false)
			a.localAdmin.start = func(context.Context, []string, string) (localterm.Process, error) {
				<-release
				return testAdminProcess(cmd, func() { closed.Add(1) }), nil
			}
			a.openAdminLocal(tab)
			switch how {
			case "close-tab":
				a.closeSession(tab)
			case "close-app":
				a.CloseAll()
			case "ordinary-terminal":
				a.openLocal(tab)
			}
			close(release)
			p.until(t, "late process disposal", func() bool { return closed.Load() == 1 })
			if a.localAdmin.pending != nil {
				t.Fatal("pending request retained")
			}
			for _, s := range a.sessions {
				if s.localElevated {
					t.Fatal("closed request opened a terminal")
				}
			}
		})
	}
}

func TestAdminLocalFailurePreservesTab(t *testing.T) {
	a, p, _, closed := adminApp(t)
	tab := a.current()
	a.localAdmin.start = func(context.Context, []string, string) (localterm.Process, error) {
		return testAdminProcess([]string{"finallyshell-nonexistent-waiter"}, func() { closed.Add(1) }), nil
	}
	a.openAdminLocal(tab)
	p.until(t, "startup failure", func() bool { return a.localAdmin.pending == nil })
	if !tab.isNewTab() || closed.Load() != 1 {
		t.Fatal("failed terminal replaced tab or leaked process")
	}
}

func TestAdminLocalReconnectAndSplit(t *testing.T) {
	a, p, _, closed := adminApp(t)
	s := openAdmin(t, a, p)
	old := s.term()
	a.reconnect(s)
	p.until(t, "administrator reconnect", func() bool { return a.localAdmin.pending == nil })
	untilScreen(t, p, s, "ready")
	if a.current() != s || !s.localElevated || old == s.term() {
		t.Fatal("reconnect lost privilege or tab")
	}
	// Old terminal exit callbacks must not terminate the newly installed pane.
	p.drain()
	if s.status != statusConnected {
		t.Fatal("stale exit closed reconnected terminal")
	}
	a.splitPane(s, splitRight)
	p.until(t, "administrator split", func() bool { return a.localAdmin.pending == nil })
	if s.panes.count() != 2 || !s.localElevated {
		t.Fatal("split lost privilege")
	}
	untilScreen(t, p, s, "ready")
	if closed.Load() < 1 {
		t.Fatal("old helper not closed")
	}
}

func TestAdminLocalReconnectPartialCancelPreservesPanes(t *testing.T) {
	a, p, _, closed := adminApp(t)
	s := openAdmin(t, a, p)
	a.splitPane(s, splitDown)
	p.until(t, "split", func() bool { return a.localAdmin.pending == nil })
	untilScreen(t, p, s, "ready")
	panes := slices.Clone(s.panes.panes())
	first, second := panes[0].term, panes[1].term
	cmd, _ := config.SplitCommandLine(testShell, false)
	var calls atomic.Int32
	a.localAdmin.start = func(context.Context, []string, string) (localterm.Process, error) {
		if calls.Add(1) == 2 {
			return nil, localterm.ErrCanceled
		}
		return testAdminProcess(cmd, func() { closed.Add(1) }), nil
	}
	a.reconnect(s)
	p.until(t, "partial cancellation", func() bool { return a.localAdmin.pending == nil })
	if panes[0].term != first || panes[1].term != second || s.panes.count() != 2 || s.status != statusConnected {
		t.Fatal("partial cancellation changed live panes")
	}
	if closed.Load() != 1 {
		t.Fatalf("prepared helper not disposed: %d", closed.Load())
	}
}

func TestAdminLocalSettingsSnapshotAndDefault(t *testing.T) {
	a, p, _, _ := adminApp(t)
	started := make(chan []string, 1)
	cmd, _ := config.SplitCommandLine(testShell, false)
	a.settings.LocalShell = `sh -c 'echo ready; cat'`
	a.localAdmin.start = func(_ context.Context, args []string, _ string) (localterm.Process, error) {
		started <- args
		return testAdminProcess(cmd, nil), nil
	}
	a.openAdminLocal(nil)
	a.settings.LocalShell = "changed-after-click"
	if got := <-started; !slices.Equal(got, cmd) {
		t.Fatalf("settings changed during UAC: %q", got)
	}
	p.until(t, "launch", func() bool { return a.localAdmin.pending == nil })
	a.settings.LocalShell = testShell
	a.openLocal(nil)
	if a.current().localElevated {
		t.Fatal("administrator launch became the ordinary default")
	}
}

func TestAdminLocalAlreadyElevatedDisablesOrdinary(t *testing.T) {
	a, _, tt, _ := adminApp(t)
	a.localAdmin.elevated = true
	a.openLocal(nil)
	if len(a.sessions) != 1 || !a.current().isNewTab() {
		t.Fatal("inherited administrator shell labeled ordinary")
	}
	a.setLeftPage(leftConnections)
	tt.Frame()
	if button, ok := tt.AccessByRole(ui.RoleButton, locales.LocalTerminal()); !ok || !button.Disabled {
		t.Fatal("ordinary button is enabled in elevated app")
	}
	tt.Click(locales.LocalTerminal())
	if len(a.sessions) != 1 || !a.current().isNewTab() {
		t.Fatal("disabled ordinary action opened a terminal")
	}
}

func TestAdminLocalDuplicateAndNoAutoSelect(t *testing.T) {
	a, p, tt, _ := adminApp(t)
	s := openAdmin(t, a, p)
	a.settings.NoAutoSelectTab = true
	tt.Frame()
	tt.RightClickAt(rowCenter(t, tt, s.target()))
	if err := tt.Click(locales.DuplicateSession()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "duplicate administrator", func() bool { return a.localAdmin.pending == nil })
	last := a.sessions[len(a.sessions)-1]
	if len(a.sessions) != 3 || a.current() != s || !last.localElevated {
		t.Fatal("duplicate lost privilege or selection preference")
	}
}

func TestAdminLocalBusyAndUnsupported(t *testing.T) {
	a, p, _, _ := adminApp(t)
	release := make(chan struct{})
	var calls atomic.Int32
	a.localAdmin.start = func(context.Context, []string, string) (localterm.Process, error) {
		calls.Add(1)
		<-release
		return nil, errors.New("denied")
	}
	a.openAdminLocal(nil)
	a.openAdminLocal(nil)
	close(release)
	p.until(t, "denied", func() bool { return a.localAdmin.pending == nil })
	if calls.Load() != 1 {
		t.Fatal("multiple simultaneous UAC requests")
	}
	a.localAdmin.supported = false
	a.openAdminLocal(nil)
	if len(a.sessions) != 1 {
		t.Fatal("unsupported platform opened administrator tab")
	}
}

func TestAdminLocalShots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p, tt, _ := adminApp(t)
			p.SetLocale(lang)
			demoSession(a, "web-1", "203.0.113.10")
			a.settings.LocalShell = testShell
			a.openLocal(nil)
			untilScreen(t, p, a.current(), "ready")
			s := openAdmin(t, a, p)
			s.setName("maintenance")
			a.bottomTab = tabCommands
			tt.Frame()
			saveShot(t, tt, "local-admin-"+lang+".png")
			a.active = 0
			a.setLeftPage(leftConnections)
			tt.Frame()
			if err := tt.Click(locales.LocalTerminalPrivileges()); err != nil {
				t.Fatal(err)
			}
			saveShot(t, tt, "local-admin-menu-"+lang+".png")
		})
	}
}

type fakeAdminProcess struct {
	command []string
	closeFn func()
	once    sync.Once
	err     error
}

func (p *fakeAdminProcess) CommandLine() []string { return slices.Clone(p.command) }
func (p *fakeAdminProcess) Activate()             {}
func (p *fakeAdminProcess) Err() error            { return p.err }
func (p *fakeAdminProcess) Close() error {
	p.once.Do(func() {
		if p.closeFn != nil {
			p.closeFn()
		}
	})
	return nil
}
func testAdminProcess(command []string, closeFn func()) localterm.Process {
	return &fakeAdminProcess{command: command, closeFn: closeFn}
}

func TestAdminLocalSplitStartupFailureKeepsOtherPane(t *testing.T) {
	a, p, _, closed := adminApp(t)
	s := openAdmin(t, a, p)
	original := s.term()
	command, _ := config.SplitCommandLine(`sh -c 'echo failed; exit 1'`, false)
	a.localAdmin.start = func(context.Context, []string, string) (localterm.Process, error) {
		return &fakeAdminProcess{command: command, closeFn: func() { closed.Add(1) }, err: errors.New("administrator shell startup refused")}, nil
	}
	a.splitPane(s, splitRight)
	p.until(t, "split request", func() bool { return a.localAdmin.pending == nil })
	p.until(t, "split startup failure", func() bool { return s.status == statusFailed || s.panes.count() == 1 })
	if s.status != statusConnected || s.panes.count() != 1 || s.term() != original {
		t.Fatalf("startup failure disabled original pane: status %v, panes %d", s.status, s.panes.count())
	}
}
