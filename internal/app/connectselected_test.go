package app

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func TestConnectSelected(t *testing.T) {
	for _, manager := range []bool{false, true} {
		for _, enter := range []bool{false, true} {
			name := "page"
			if manager {
				name = "manager"
			}
			if enter {
				name += "/enter"
			}
			t.Run(name, func(t *testing.T) {
				var a *App
				var tt *ui.Tester
				var center func(string) (float32, float32)
				if manager {
					a, _, tt = openTestManager(t)
					center = func(name string) (float32, float32) { return rowCenter(t, tt, name) }
				} else {
					a, _, tt = connPageApp(t, "en")
					center = func(name string) (float32, float32) { return connRow(t, tt, name) }
				}
				t.Cleanup(a.CloseAll)
				unreachable(a)
				m := &a.connTree
				if manager {
					m = &a.manager
				}
				click := func(mods ui.Modifiers, name string) {
					if mods == 0 {
						tt.Advance(time.Second) // ordinary clicks are separate from double-clicks
					}
					x, y := center(name)
					tt.ClickAtWith(mods, x, y)
					tt.Frame()
				}
				click(0, "web-1")
				click(ui.Cmd, "raspberry")
				if len(a.selectedHosts(m)) != 2 {
					t.Fatal("Ctrl/Cmd-click did not add a connection")
				}
				click(ui.Cmd, "raspberry")
				if len(a.selectedHosts(m)) != 1 {
					t.Fatal("Ctrl/Cmd-click did not remove a connection")
				}
				click(0, "web-1")
				click(ui.Shift, "web-2")
				if len(a.selectedHosts(m)) != 2 || !m.selected.Has(managerItem{host: "web-1"}) || !m.selected.Has(managerItem{host: "web-2"}) {
					t.Fatal("Shift-click did not select the range")
				}
				if len(a.sessions) != 0 {
					t.Fatal("choosing rows unexpectedly opened sessions")
				}
				if enter {
					tt.Key(0, ui.KeyEnter)
				} else {
					tt.RightClickAt(center("web-1"))
					tt.Frame()
					if len(a.selectedHosts(m)) != 2 {
						t.Fatal("right-click lost the selection")
					}
					if err := tt.ChooseMenuItem(locales.ConnectSelected()); err != nil {
						t.Fatal(err)
					}
				}
				tt.Frame()
				if got := sessionHosts(a); !slices.Equal(got, []string{"web-1", "web-2"}) {
					t.Fatalf("connected %v", got)
				}
			})
		}
	}
}

func TestSelectedConnectionsStayWithinVisibleSelection(t *testing.T) {
	a, _, tt := connPageApp(t, "en")
	m := &a.connTree
	tt.ClickAt(connRow(t, tt, "web-1"))
	x, y := connRow(t, tt, "raspberry")
	tt.ClickAtWith(ui.Shift, x, y)
	tt.Frame()
	if len(a.selectedHosts(m)) != 3 { // web-1, web-2, raspberry; no implicit folder contents
		t.Fatal("range included connections outside the selected rows")
	}
	m.tree.Open.Remove(managerItem{folder: "prod"})
	tt.Frame()
	if hosts := a.selectedHosts(m); len(hosts) != 1 || hosts[0].ID != "pi" {
		t.Fatal("collapsed connections would be connected")
	}
	tt.RightClickAt(connRow(t, tt, "vps"))
	tt.Frame()
	if m.selected.Len() != 1 || !m.selected.Has(managerItem{host: "vps"}) || slices.Contains(tt.Menu(), locales.ConnectSelected()) {
		t.Fatal("right-click outside the selection did not choose only that connection")
	}
}

func TestEnterWithNoSelectedConnection(t *testing.T) {
	a, _, tt := connPageApp(t, "en")
	t.Cleanup(a.CloseAll)
	unreachable(a)
	x, y := connRow(t, tt, "web-1")
	tt.ClickAt(x, y)
	tt.ClickAtWith(ui.Cmd, x, y)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(a.sessions) != 0 || a.connTree.selected.Len() != 0 {
		t.Fatal("Enter connected an unselected row")
	}
}

func TestConnectSelectedAsksForLargeSelection(t *testing.T) {
	a, _, tt := connPageApp(t, "zh-Hans")
	t.Cleanup(a.CloseAll)
	m := &a.connTree
	var want []string
	for i := range connectAllAsk + 1 {
		id := fmt.Sprintf("chosen-%02d", i)
		a.hosts.Add(&config.Host{ID: id, Name: id})
		m.selected.Add(managerItem{host: id})
		want = append(want, id)
	}
	unreachable(a)
	tt.Frame()
	a.connectSelected(m)
	tt.Frame()
	if m.question == nil || len(a.sessions) != 0 || !tt.HasText(locales.ConnectSelectedBody()) {
		t.Fatal("large selection did not ask before connecting")
	}
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	if m.question != nil || len(a.sessions) != 0 {
		t.Fatal("cancelling opened connections")
	}
	a.connectSelected(m)
	tt.Frame()
	button, ok := tt.AccessByRole(ui.RoleButton, locales.Connect())
	if !ok {
		t.Fatal("no connect button in the confirmation")
	}
	tt.ClickAt(button.Bounds.X+button.Bounds.W/2, button.Bounds.Y+button.Bounds.H/2)
	if got := sessionHosts(a); !slices.Equal(got, want) {
		t.Fatalf("connected %v, want %v", got, want)
	}
}
