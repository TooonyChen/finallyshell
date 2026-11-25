package app

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"finallyshell/locales"
	"github.com/egoist/mygo/ui"
)

func chooseSessionGroup(t *testing.T, tt *ui.Tester, item string) {
	t.Helper()
	if err := tt.ChooseMenuItem(item); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
}

func TestSessionGroupCloseOthersTargetsRightClickedRow(t *testing.T) {
	a, tt, shows := closeApp(t, 0)
	keep := a.sessions[1]
	var canceled int
	for _, s := range a.sessions {
		if s != keep {
			s.cancel = func() { canceled++ }
		}
	}
	tt.RightClickAt(rowCenter(t, tt, keep.label()))
	chooseSessionGroup(t, tt, locales.CloseOtherSessions())
	if len(a.sessions) != 1 || a.current() != keep || canceled != 2 {
		t.Fatalf("rows %q, current %v, canceled %d", rowNames(a), a.current(), canceled)
	}
	if keep.term() == nil || tt.Image().RGBAAt(termSpotX, termSpotY) != shows["bravo"] {
		t.Fatal("kept terminal changed")
	}
	tt.Frame()
	if a.current() != keep {
		t.Fatal("retained row lost selection")
	}
}

func TestSessionGroupCloseAllRowAndBlank(t *testing.T) {
	for _, blank := range []bool{false, true} {
		t.Run(fmt.Sprint(blank), func(t *testing.T) {
			a, tt, _ := closeApp(t, 0)
			var canceled int
			for _, s := range a.sessions {
				s.cancel = func() { canceled++ }
			}
			hosts := len(a.hosts.Live())
			if blank {
				tt.RightClickAt(100, 790)
				if !slices.Equal(tt.Menu(), []string{locales.CloseAllSessions()}) {
					t.Fatalf("blank menu %q", tt.Menu())
				}
			} else {
				tt.RightClickAt(rowCenter(t, tt, a.sessions[1].label()))
			}
			chooseSessionGroup(t, tt, locales.CloseAllSessions())
			if len(a.sessions) != 0 || a.current() != nil || canceled != 3 {
				t.Fatalf("rows %q, selected %d, canceled %d", rowNames(a), a.active, canceled)
			}
			if a.fe.discard || len(a.hosts.Live()) != hosts || !tt.HasText(locales.NoSessions()) {
				t.Fatal("bulk close ran app shutdown or changed saved hosts")
			}
			// Closing all leaves a usable application and no stale list selection.
			a.NewTab()
			tt.Frame()
			if len(a.sessions) != 1 || a.current() == nil || !a.current().isNewTab() {
				t.Fatal("cannot open new tab after closing all")
			}
		})
	}
}

func TestSessionGroupEmptyAndSingleDisabled(t *testing.T) {
	a, p := testApp(t)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	tt.RightClickAt(100, 790)
	if !slices.Equal(tt.Menu(), []string{locales.CloseAllSessions()}) {
		t.Fatalf("empty menu %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem(locales.CloseAllSessions()); err == nil {
		t.Fatal("empty Close all is enabled")
	}
	tt.CloseMenu()
	a.NewTab()
	tt.Frame()
	tt.RightClickAt(rowCenter(t, tt, locales.NotConnected()))
	if !slices.Equal(tt.Menu(), []string{locales.Close(), locales.CloseOtherSessions(), locales.CloseAllSessions()}) {
		t.Fatalf("new tab menu %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem(locales.CloseOtherSessions()); err == nil {
		t.Fatal("Close others enabled with one tab")
	}
	chooseSessionGroup(t, tt, locales.CloseAllSessions())
	if len(a.sessions) != 0 {
		t.Fatal("new tab left behind")
	}
}

func TestSessionGroupPreservesOwnerAndPanel(t *testing.T) {
	for _, panelTarget := range []bool{false, true} {
		t.Run(fmt.Sprint(panelTarget), func(t *testing.T) {
			a, _, tt, web, _ := panelApp(t)
			a.openPanelTab(web, tabProcesses)
			panel := a.current()
			tt.Frame()
			target, label := web, web.label()
			if panelTarget {
				target, label = panel, locales.TabProcesses()
			}
			tt.RightClickAt(rowCenter(t, tt, label))
			chooseSessionGroup(t, tt, locales.CloseOtherSessions())
			if !slices.Equal(a.sessions, []*Session{web, panel}) || a.current() != target || a.connSession() != web || web.term() == nil {
				t.Fatalf("owner/panel lost: rows %q active %d", rowNames(a), a.active)
			}
			tt.RightClickAt(rowCenter(t, tt, label))
			if err := tt.ChooseMenuItem(locales.CloseOtherSessions()); err == nil {
				t.Fatal("same connection's panel counted as another session")
			}
			chooseSessionGroup(t, tt, locales.CloseAllSessions())
			if len(a.sessions) != 0 || web.term() != nil {
				t.Fatal("owner or panel left after Close all")
			}
		})
	}
}

func TestSessionGroupStaleTargetAndRename(t *testing.T) {
	a, tt, _ := closeApp(t, 0)
	gone := a.sessions[1]
	a.closeSession(gone)
	a.closeSessionGroup(gone)
	if len(a.sessions) != 2 {
		t.Fatal("stale target closed live sessions")
	}
	a.startRename(a.sessions[1])
	a.closeSessionGroup(a.sessions[0])
	tt.Frame()
	if a.rename.s != nil || len(a.sessions) != 1 {
		t.Fatal("closed row kept rename state")
	}
}

func TestSessionGroupCloseLocalProcess(t *testing.T) {
	a, p, tt := localApp(t, testShell)
	a.openLocal(nil)
	s := a.current()
	untilScreen(t, p, s, "ready")
	term := s.term()
	// Preserve another real tab while closing a running shell.
	keep := a.sessions[0]
	a.closeSessionGroup(keep)
	tt.Frame()
	if a.current() != keep || s.term() != nil {
		t.Fatal("local process pane left after close")
	}
	select {
	case <-term.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("local shell survived bulk close")
	}
}

func TestSessionGroupClosesConnectingAndManyRows(t *testing.T) {
	a, _ := testApp(t)
	keep := demoSession(a, "keep", "keep.example")
	var contexts []context.Context
	for i := 0; i < 90; i++ {
		s := newSession(nil)
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.status = statusConnecting
		contexts = append(contexts, ctx)
		a.sessions = append(a.sessions, s)
	}
	a.closeSessionGroup(keep)
	if !slices.Equal(a.sessions, []*Session{keep}) || a.current() != keep {
		t.Fatal("bulk close skipped rows")
	}
	for _, ctx := range contexts {
		if ctx.Err() == nil {
			t.Fatal("connection request not canceled")
		}
	}
}

func TestSessionGroupMenuShots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			coloredSession(t, a, "web-1", 1)
			coloredSession(t, a, "db-1", 2)
			a.active = 0
			tt := ui.NewTester(func(c *ui.Context) { c.SetMenuPresentation(ui.PopoverMenus); a.View(c) }, 1280, 820)
			p.tt = tt
			tt.Frame()
			tt.RightClickAt(rowCenter(t, tt, "db-1"))
			if !tt.HasText(locales.CloseOtherSessions()) || !tt.HasText(locales.CloseAllSessions()) {
				t.Fatalf("popover %q", tt.Texts())
			}
			saveShot(t, tt, "sessions-close-row-"+lang+".png")
			tt.CloseMenu()
			tt.RightClickAt(100, 790)
			if !slices.Equal(tt.Menu(), []string{locales.CloseAllSessions()}) {
				t.Fatalf("blank popover %q", tt.Menu())
			}
			saveShot(t, tt, "sessions-close-blank-"+lang+".png")
		})
	}
}
