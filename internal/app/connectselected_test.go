package app

import (
	"fmt"
	"runtime"
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

// The list's keyboard cursor can stay on a connection just removed from
// the selection, or move independently with Ctrl+Down. Enter must open
// the selected connection rather than that unselected cursor.
func TestConnectSelectedSkipsDeselectedCursor(t *testing.T) {
	for _, manager := range []bool{false, true} {
		for _, keyboard := range []bool{false, true} {
			t.Run(fmt.Sprintf("manager=%v/keyboard=%v", manager, keyboard), func(t *testing.T) {
				if keyboard && runtime.GOOS == "darwin" {
					t.Skip("Ctrl+Down moves the unselected cursor only on Windows and Linux")
				}
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
				tt.ClickAt(center("web-1"))
				if keyboard {
					tt.Key(ui.Ctrl, ui.KeyDown)
				} else {
					x, y := center("raspberry")
					tt.ClickAtWith(ui.Cmd, x, y)
					tt.ClickAtWith(ui.Cmd, x, y)
				}
				tt.Frame()
				cursor, _ := m.chosen()
				if m.selected.Len() != 1 || !m.selected.Has(managerItem{host: "web-1"}) || m.selected.Has(cursor) {
					t.Fatalf("test must leave a different cursor: %+v", cursor)
				}
				tt.Key(0, ui.KeyEnter)
				tt.Frame()
				if got := sessionHosts(a); !slices.Equal(got, []string{"web-1"}) {
					t.Fatalf("Enter connected %v rather than the selected web-1", got)
				}
			})
		}
	}
}

func TestUnselectedCursorIgnoresRenameAndDelete(t *testing.T) {
	for _, manager := range []bool{false, true} {
		for _, key := range []ui.Key{ui.KeyF2, ui.KeyDelete} {
			t.Run(fmt.Sprintf("manager=%v/key=%v", manager, key), func(t *testing.T) {
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
				m := &a.connTree
				if manager {
					m = &a.manager
				}
				tt.ClickAt(center("web-1"))
				x, y := center("web-1")
				tt.ClickAtWith(ui.Cmd, x, y)
				tt.Key(0, key)
				tt.Frame()
				if m.prompt != nil || m.question != nil || a.hosts.Host("web-1").Deleted() {
					t.Fatal("an unselected keyboard cursor can rename or delete a connection")
				}
			})
		}
	}
}

func TestSearchEnterUsesSelectedConnections(t *testing.T) {
	for _, manager := range []bool{false, true} {
		t.Run(fmt.Sprintf("manager=%v", manager), func(t *testing.T) {
			var a *App
			var tt *ui.Tester
			var center func(string) (float32, float32)
			label := locales.SearchConnections()
			if manager {
				a, _, tt = openTestManager(t)
				center = func(name string) (float32, float32) { return rowCenter(t, tt, name) }
				label = locales.SearchHosts()
			} else {
				a, _, tt = connPageApp(t, "en")
				center = func(name string) (float32, float32) { return connRow(t, tt, name) }
			}
			t.Cleanup(a.CloseAll)
			unreachable(a)
			tt.ClickAt(center("web-1"))
			x, y := center("raspberry")
			tt.ClickAtWith(ui.Cmd, x, y)
			tt.ClickAtWith(ui.Cmd, x, y)
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if got := sessionHosts(a); !slices.Equal(got, []string{"web-1"}) {
				t.Fatalf("search Enter connected %v rather than the selected web-1", got)
			}
		})
	}
}

func TestConnectSelectedMenuCountsOnlyVisibleRows(t *testing.T) {
	a, _, tt := connPageApp(t, "en")
	m := &a.connTree
	tt.ClickAt(connRow(t, tt, "raspberry"))
	m.selected.Add(managerItem{host: "web-1"})
	m.selected.Add(managerItem{host: "gone"})
	m.tree.Open.Remove(managerItem{folder: "prod"})
	tt.Frame()
	tt.RightClickAt(connRow(t, tt, "raspberry"))
	tt.Frame()
	if slices.Contains(tt.Menu(), locales.ConnectSelected()) {
		t.Fatal("hidden selections made the single visible selected row a batch")
	}
}

func TestSingleSelectedFolderIgnoresHiddenSelections(t *testing.T) {
	a, _, tt := connPageApp(t, "en")
	m := &a.connTree
	tt.ClickAt(connRow(t, tt, "Lab"))
	// Selection keys survive when their rows disappear. Hidden or
	// permanently removed connections must not prevent Enter on a
	// single visible folder from opening or closing it.
	m.selected.Add(managerItem{host: "gone"})
	m.selected.Add(managerItem{host: "web-1"})
	m.tree.Open.Remove(managerItem{folder: "prod"})
	tt.Frame()
	if !m.tree.Open.Has(managerItem{folder: "lab"}) {
		t.Fatal("Lab must start open")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if m.tree.Open.Has(managerItem{folder: "lab"}) {
		t.Fatal("invisible selections prevented opening/closing the only selected folder")
	}
}

func TestMoveSelectedFolderKeepsSelectedDescendants(t *testing.T) {
	a, _, tt := connPageApp(t, "en")
	m := &a.connTree
	for _, it := range []managerItem{{folder: "prod"}, {folder: "db"}, {host: "pg"}, {host: "web-1"}, {host: "web-2"}} {
		m.selected.Add(it)
	}
	tt.Frame()
	// The list's multi-row drag passes every selected row, including
	// descendants of selected folders. Moving the parent must retain
	// that hierarchy rather than moving each descendant beside it.
	items := m.tree.List.DragRows([]int{0, 1, 2, 3, 4}).(managerDrag)
	a.moveItems(m, items, "lab")
	if a.hosts.Folder("prod").Parent != "lab" || a.hosts.Folder("db").Parent != "prod" ||
		a.hosts.Host("pg").Folder != "db" || a.hosts.Host("web-1").Folder != "prod" || a.hosts.Host("web-2").Folder != "prod" {
		t.Fatal("dragging a selected parent and descendants flattened their hierarchy")
	}
}

func TestRejectedSelectedFolderMoveKeepsDescendants(t *testing.T) {
	for _, target := range []string{"db", ""} {
		t.Run("target="+target, func(t *testing.T) {
			a, _, tt := connPageApp(t, "en")
			tt.Frame()
			a.moveItems(&a.connTree, managerDrag{{folder: "prod"}, {folder: "db"}, {host: "pg"}, {host: "web-1"}}, target)
			if a.hosts.Folder("prod").Parent != "" || a.hosts.Folder("db").Parent != "prod" ||
				a.hosts.Host("pg").Folder != "db" || a.hosts.Host("web-1").Folder != "prod" {
				t.Fatal("rejected/no-op parent move moved its descendants separately")
			}
		})
	}
}

func TestSelectedConnectionsMenuScreenshots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		for _, manager := range []bool{false, true} {
			t.Run(fmt.Sprintf("lang=%s/manager=%v", lang, manager), func(t *testing.T) {
				a, p := testApp(t)
				p.SetLocale(lang)
				managerHosts(a)
				var view func(*ui.Context)
				name := "connections-selected-"
				if manager {
					a.openManager()
					view = a.managerView
					name = "manager-selected-"
				} else {
					a.connTree.tree.Open.Clear()
					a.restoreLeftPanel()
					a.left.page = leftConnections
					view = a.View
				}
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetMenuPresentation(ui.PopoverMenus)
					view(c)
				}, 1000, 720)
				tt.SetScale(2)
				tt.SetLocale(lang)
				p.tt = tt
				tt.Frame()
				center := func(name string) (float32, float32) {
					if manager {
						return rowCenter(t, tt, name)
					}
					return connRow(t, tt, name)
				}
				tt.ClickAt(center("web-1"))
				x, y := center("web-2")
				tt.ClickAtWith(ui.Cmd, x, y)
				tt.RightClickAt(center("web-1"))
				tt.Frame()
				if !tt.HasText(locales.ConnectSelected()) {
					t.Fatal("selected menu was not rendered")
				}
				saveShot(t, tt, name+lang+".png")
			})
		}
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
