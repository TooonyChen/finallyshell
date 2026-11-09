package app

import (
	"slices"
	"strings"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// connShown are the titles of the rows the Connections page shows, in
// order, indented as deep as they are.
func connShown(a *App) []string {
	m := &a.connTree
	var out []string
	for i := range m.tree.Rows() {
		out = append(out, strings.Repeat("  ", m.tree.Depth(i))+a.managerTitle(m.tree.Item(i)))
	}
	return out
}

// connRow is the center of the row of the page's tree named name.
func connRow(t *testing.T, tt *ui.Tester, name string) (x, y float32) {
	t.Helper()
	r, ok := tt.AccessByRole(ui.RoleTreeItem, name)
	if !ok {
		t.Fatalf("no row %q; texts %q", name, tt.Texts())
	}
	return r.Bounds.X + r.Bounds.W/2, r.Bounds.Y + r.Bounds.H/2
}

// connMenu right-clicks the page's row name and chooses item, in a
// submenu after the path before it.
func connMenu(t *testing.T, tt *ui.Tester, name string, path ...string) {
	t.Helper()
	tt.RightClickAt(connRow(t, tt, name))
	if err := tt.ChooseMenuItem(path...); err != nil {
		t.Fatalf("%s: %v; menu %q", name, err, tt.Menu())
	}
	tt.Frame()
}

// connPageApp is the main window on the hosts of managerHosts, on the
// Connections page, in lang.
func connPageApp(t *testing.T, lang string) (*App, *fakePlatform, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	p.SetLocale(lang)
	managerHosts(a)
	// As New does, with the hosts it read.
	a.connTree.tree.Open.Clear()
	a.restoreLeftPanel()
	tt := ui.NewTester(a.View, 1280, 820)
	tt.SetScale(2)
	tt.SetLocale(lang)
	p.tt = tt
	tt.Frame()
	if err := tt.Click(locales.ConnectionsPage()); err != nil {
		t.Fatal(err)
	}
	return a, p, tt
}

// The header switches the panel between the sessions and the tree of the
// connections, which has the manager's folders, every one open at first,
// and no deleted host; the page is kept.
func TestConnectionPage(t *testing.T) {
	a, p, tt := connPageApp(t, "en")
	if a.left.page != leftConnections || config.LoadSettings().LeftPanelPage != config.LeftPanelConnections {
		t.Fatalf("page %d, saved %q", a.left.page, config.LoadSettings().LeftPanelPage)
	}
	want := []string{"Production", "  Databases", "    pg-main", "  web-1", "  web-2", "Lab", "  raspberry", "vps"}
	if got := connShown(a); !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
	if tt.HasText("old-box") || tt.HasText(locales.NoSessions()) || !tt.HasText(locales.SessionsTitle(0)) {
		t.Errorf("texts %q", tt.Texts())
	}
	// One column: no address, no notes.
	if _, ok := tt.AccessByRole(ui.RoleTreeItem, "web-1"); !ok || tt.HasText("nginx, front") {
		t.Errorf("texts %q", tt.Texts())
	}
	if got := hostTooltip(a.hosts.Host("web-1")); got != "root@203.0.113.10:22\nnginx, front" {
		t.Errorf("tooltip %q", got)
	}
	if got := hostTooltip(a.hosts.Host("web-2")); got != "deploy@203.0.113.11:2222" {
		t.Errorf("tooltip %q", got)
	}
	saveShot(t, tt, "conntree-en.png")
	// At the narrowest the sidebar goes, the header keeps its buttons.
	a.sideWidth = sidebarLimits.first
	tt.Frame()
	if r, ok := tt.Access(locales.NewConnection()); !ok || r.Bounds.X+r.Bounds.W > a.sideWidth {
		t.Errorf("New connection at %+v, ok %v", r.Bounds, ok)
	}
	saveShot(t, tt, "conntree-narrow-en.png")
	a.sideWidth = defaultSideWidth
	tt.Frame()

	// A folder closes with its arrow, and Right opens it again.
	x, y := connRow(t, tt, "Production")
	r, _ := tt.AccessByRole(ui.RoleTreeItem, "Production")
	tt.ClickAt(r.Bounds.X+12, y)
	if got := connShown(a); !slices.Equal(got, []string{"Production", "Lab", "  raspberry", "vps"}) {
		t.Fatalf("closed: %q", got)
	}
	tt.ClickAt(x, y)
	tt.Key(0, ui.KeyRight)
	if got := connShown(a); !slices.Equal(got, want) {
		t.Fatalf("opened: %q", got)
	}

	// Back to the sessions.
	if err := tt.Click(locales.SessionsTitle(0)); err != nil {
		t.Fatal(err)
	}
	if a.left.page != leftSessions || !tt.HasText(locales.NoSessions()) || config.LoadSettings().LeftPanelPage != "" {
		t.Errorf("page %d; texts %q", a.left.page, tt.Texts())
	}
	p.SetLocale("zh-Hans")
	tt.Frame()
	if err := tt.Click("连接"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	for _, s := range []string{"会话 (0)", "连接", "搜索连接", "Production"} {
		if !tt.HasText(s) {
			if _, ok := tt.Access(s); !ok {
				t.Errorf("missing %q; texts %q", s, tt.Texts())
			}
		}
	}
	saveShot(t, tt, "conntree-zh.png")
}

// A double click or Enter on a host opens a session to it, in the new tab
// selected, and the page stays; the host's row shows a dot while it is
// open.
func TestConnectFromPage(t *testing.T) {
	srv := startFakeSSH(t)
	a, p, tt := connPageApp(t, "en")
	t.Cleanup(a.CloseAll)
	for _, id := range []string{"web-2", "pg"} {
		h := a.hosts.Host(id)
		h.Addr, h.Port, h.User, h.Auth, h.NoMonitor = srv.host, srv.port, "u", config.AuthPassword, true
		a.passwords[id] = "secret"
	}
	a.NewTab()
	tab := a.sessions[0]
	tt.Frame()
	x, y := connRow(t, tt, "web-2")
	tt.ClickAt(x, y)
	tt.ClickAt(x, y) // a double click
	if len(a.sessions) != 1 || tab.Host != a.hosts.Host("web-2") || a.current() != tab || a.left.page != leftConnections {
		t.Fatalf("%d sessions, tab %v, page %d", len(a.sessions), tab.Host, a.left.page)
	}
	if _, ok := a.sessionDot(a.hosts.Host("web-2")); !ok {
		t.Error("no dot while connecting")
	}
	p.until(t, "the shell", func() bool {
		if tab.hostKeyQ != nil {
			tab.hostKeyQ.Answer <- true
			tab.hostKeyQ = nil
		}
		return tab.status == statusConnected || tab.status == statusFailed
	})
	if tab.status != statusConnected {
		t.Fatal(describeError(tab.err))
	}
	if c, ok := a.sessionDot(a.hosts.Host("web-2")); !ok || c != theme.Success {
		t.Errorf("dot %v %v", c, ok)
	}
	if _, ok := a.sessionDot(a.hosts.Host("web-1")); ok {
		t.Error("a dot on a host with no session")
	}
	if !tt.HasText(locales.SessionsTitle(1)) {
		t.Errorf("the count; texts %q", tt.Texts())
	}

	// Enter on the next one opens it in a tab of its own, the page still
	// there.
	tt.ClickAt(connRow(t, tt, "pg-main"))
	tt.Key(0, ui.KeyEnter)
	if len(a.sessions) != 2 || a.sessions[1].Host != a.hosts.Host("pg") || a.left.page != leftConnections {
		t.Fatalf("%d sessions", len(a.sessions))
	}
	p.until(t, "the second shell", func() bool {
		s := a.sessions[1]
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusConnected || s.status == statusFailed
	})
	tt.Frame()
	saveShot(t, tt, "conntree-open-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "conntree-open-zh.png")
	p.SetLocale("")

	// Closed, the dot goes.
	a.closeSession(a.sessions[1])
	if _, ok := a.sessionDot(a.hosts.Host("pg")); ok {
		t.Error("a dot after the session closed")
	}
}

// The search filters the tree as the manager's does, opening the folders
// holding what it finds, and Enter connects the first; cleared, the
// folders are as they were.
func TestConnectionPageSearch(t *testing.T) {
	a, p, tt := connPageApp(t, "en")
	r, _ := tt.AccessByRole(ui.RoleTreeItem, "Production")
	tt.ClickAt(r.Bounds.X+12, r.Bounds.Y+r.Bounds.H/2) // closed
	if err := tt.Click(locales.SearchConnections()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		query string
		want  []string
	}{
		{"pg", []string{"Production", "  Databases", "    pg-main"}},
		{"DEPLOY", []string{"Production", "  web-2"}},
		{"rack 4", []string{"Production", "  web-1"}},
		{"web 2222", []string{"Production", "  web-2"}},
		{"old-box", nil},
	} {
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(q.query)
		if got := connShown(a); !slices.Equal(got, q.want) {
			t.Errorf("%q: %q, want %q", q.query, got, q.want)
		}
	}
	if !tt.HasText(locales.NoMatchingHosts()) {
		t.Errorf("no message; texts %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("rasp")
	if it, _ := a.connTree.chosen(); it.host != "pi" {
		t.Errorf("chosen %v", it)
	}
	saveShot(t, tt, "conntree-search-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "conntree-search-zh.png")
	p.SetLocale("")
	tt.Frame()
	// The search does not change the folders kept open.
	if ids := config.LoadSettings().ConnectionTreeExpanded; ids == nil || !slices.Equal(*ids, []string{"db", "lab"}) {
		t.Errorf("kept %v", ids)
	}
	// The manager's search is its own.
	if a.manager.query != "" {
		t.Errorf("manager's query %q", a.manager.query)
	}

	tt.Key(0, ui.KeyEscape)
	if got := connShown(a); a.connTree.query != "" || !slices.Equal(got, []string{"Production", "Lab", "  raspberry", "vps"}) {
		t.Errorf("cleared %q: %q", a.connTree.query, got)
	}
	tt.Type("vps")
	tt.Key(0, ui.KeyEnter)
	if len(a.sessions) != 1 || a.sessions[0].Host != a.hosts.Host("vps") {
		t.Errorf("Enter in the search: %d sessions", len(a.sessions))
	}
	t.Cleanup(a.CloseAll)
}

// The folders open are kept by their IDs, across a restart and a rename.
func TestConnectionPageKeepsFolders(t *testing.T) {
	a, p, tt := connPageApp(t, "en")
	r, _ := tt.AccessByRole(ui.RoleTreeItem, "Production")
	tt.ClickAt(r.Bounds.X+12, r.Bounds.Y+r.Bounds.H/2)
	r, _ = tt.AccessByRole(ui.RoleTreeItem, "Databases") // still open, inside
	if ids := config.LoadSettings().ConnectionTreeExpanded; ids == nil || !slices.Equal(*ids, []string{"db", "lab"}) {
		t.Fatalf("kept %v", ids)
	}
	a.hosts.Folder("lab").Name = "Homelab"
	if err := config.SaveHosts(a.hosts); err != nil {
		t.Fatal(err)
	}

	b := New(p)
	b.saveHosts = func(*config.Store) error { return nil }
	if b.left.page != leftConnections {
		t.Errorf("page %d", b.left.page)
	}
	tt2 := ui.NewTester(b.View, 1280, 820)
	tt2.Frame()
	if got := connShown(b); !slices.Equal(got, []string{"Production", "Homelab", "  raspberry", "vps"}) {
		t.Fatalf("after a restart: %q", got)
	}
	// Opened, Databases is open still.
	r, _ = tt2.AccessByRole(ui.RoleTreeItem, "Production")
	tt2.ClickAt(r.Bounds.X+12, r.Bounds.Y+r.Bounds.H/2)
	if got := connShown(b); !slices.Equal(got[:3], []string{"Production", "  Databases", "    pg-main"}) {
		t.Errorf("rows %q", got)
	}
	// All closed is kept too, as none.
	b.connTree.tree.Open.Clear()
	tt2.Frame()
	if ids := config.LoadSettings().ConnectionTreeExpanded; ids == nil || len(*ids) != 0 {
		t.Errorf("kept %v", ids)
	}
	c := New(p)
	c.hosts = b.hosts
	tt3 := ui.NewTester(c.View, 1280, 820)
	tt3.Frame()
	if got := connShown(c); !slices.Equal(got, []string{"Production", "Homelab", "vps"}) {
		t.Errorf("all closed: %q", got)
	}
}

// What the page renames, moves and deletes shows in the manager, and the
// other way.
func TestConnectionPageAndManager(t *testing.T) {
	a, p, tt := connPageApp(t, "en")
	a.openManager()
	mw := p.show(t, WindowManager)
	mw.Frame()

	// Renamed on the page, with its menu and F2.
	connMenu(t, tt, "web-2", locales.Rename())
	if !tt.HasText(locales.RenameConnection()) {
		t.Fatalf("no prompt; texts %q", tt.Texts())
	}
	answer(t, tt, "web-two")
	mw.Frame()
	if !slices.Contains(shown(a), "  web-two") {
		t.Errorf("manager %q", shown(a))
	}
	tt.ClickAt(connRow(t, tt, "Lab"))
	tt.Key(0, ui.KeyF2)
	answer(t, tt, "Homelab")
	mw.Frame()
	if !slices.Contains(shown(a), "Homelab") || !slices.Contains(connShown(a), "Homelab") {
		t.Errorf("manager %q, page %q", shown(a), connShown(a))
	}

	// Moved: dragged onto a folder, and with the menu.
	x, y := connRow(t, tt, "vps")
	tx, ty := connRow(t, tt, "Databases")
	dragTo(tt, x, y, tx, ty)
	if h := a.hosts.Host("vps"); h.Folder != "db" {
		t.Fatalf("vps in %q; rows %q", h.Folder, connShown(a))
	}
	connMenu(t, tt, "raspberry", locales.MoveToFolder(), "Production")
	mw.Frame()
	if h := a.hosts.Host("pi"); h.Folder != "prod" || !slices.Contains(shown(a), "    vps") || !slices.Contains(shown(a), "  raspberry") {
		t.Errorf("pi in %q; manager %q", h.Folder, shown(a))
	}

	// Deleted on the page: it asks first, as the page does not list the
	// deleted hosts to restore.
	connMenu(t, tt, "web-1", locales.Delete())
	if a.hosts.Host("web-1").Deleted() || !tt.HasText(locales.DeleteHostTitle("web-1")) || !tt.HasText(locales.DeleteHostBody()) {
		t.Fatalf("no question; texts %q", tt.Texts())
	}
	saveShot(t, tt, "conntree-delete-en.png")
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	if a.hosts.Host("web-1").Deleted() {
		t.Fatal("Cancel deleted it")
	}
	tt.ClickAt(connRow(t, tt, "web-1"))
	tt.Key(0, ui.KeyDelete)
	if err := tt.Click(locales.Delete()); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	mw.Frame()
	if !a.hosts.Host("web-1").Deleted() || slices.Contains(connShown(a), "  web-1") || slices.Contains(shown(a), "  web-1") {
		t.Errorf("page %q, manager %q", connShown(a), shown(a))
	}
	// A folder, asking as the manager does.
	connMenu(t, tt, "Homelab", locales.Delete())
	if err := tt.Click(locales.Delete()); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if a.hosts.Folder("lab") != nil {
		t.Error("folder kept")
	}

	// The other way: what the manager changes shows on the page.
	rowMenu(t, mw, "pg-main", locales.Rename())
	answer(t, mw, "postgres")
	tt.Frame()
	if !slices.Contains(connShown(a), "    postgres") {
		t.Errorf("page %q", connShown(a))
	}
	rowMenu(t, mw, "web-two", locales.Delete())
	tt.Frame()
	if slices.Contains(connShown(a), "  web-two") || a.manager.question != nil {
		t.Errorf("page %q", connShown(a))
	}
	rowMenu(t, mw, "vps", locales.MoveToFolder(), locales.TopLevel())
	tt.Frame()
	if got := connShown(a); got[len(got)-1] != "vps" {
		t.Errorf("page %q", got)
	}
	// The page's prompt is in the main window, not the manager's.
	if a.manager.prompt != nil || a.connTree.prompt != nil {
		t.Error("a prompt left open")
	}
}

// The page's menus are the manager's; a new connection or folder goes in
// the folder chosen.
func TestConnectionPageMenus(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			managerHosts(a)
			a.connTree.tree.Open.Clear()
			a.restoreLeftPanel()
			a.left.page = leftConnections
			for _, id := range []string{"web-1", "vps"} {
				s := newSession(a.hosts.Host(id))
				s.status = statusConnected
				a.sessions = append(a.sessions, s)
			}
			a.active = 0
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetMenuPresentation(ui.PopoverMenus)
				a.View(c)
			}, 1280, 820)
			tt.SetScale(2)
			tt.SetLocale(lang)
			p.tt = tt
			tt.Frame()
			tt.RightClickAt(connRow(t, tt, "web-1"))
			tt.Frame()
			for _, s := range []string{locales.Connect(), locales.Edit(), locales.Duplicate(), locales.Rename(), locales.MoveToFolder(), locales.CopyHost(), locales.Delete()} {
				if !tt.HasText(s) {
					t.Errorf("menu without %q; texts %q", s, tt.Texts())
				}
			}
			saveShot(t, tt, "conntree-menu-"+lang+".png")
			tt.CloseMenu()
			tt.RightClickAt(connRow(t, tt, "Lab"))
			tt.Frame()
			for _, s := range []string{locales.NewConnection(), locales.NewSubfolder(), locales.Rename(), locales.Delete()} {
				if !tt.HasText(s) {
					t.Errorf("folder menu without %q", s)
				}
			}
			saveShot(t, tt, "conntree-folder-menu-"+lang+".png")
		})
	}

	a, p, tt := connPageApp(t, "en")
	connMenu(t, tt, "web-1", locales.CopyHost())
	if got := tt.Clipboard(); got != "203.0.113.10" {
		t.Errorf("copied %q", got)
	}
	connMenu(t, tt, "web-1", locales.Duplicate())
	if !slices.Contains(connShown(a), "  web-1 (copy)") {
		t.Errorf("rows %q", connShown(a))
	}
	if it, _ := a.connTree.chosen(); it.host == "" || a.hosts.Host(it.host).Name != "web-1 (copy)" {
		t.Errorf("chosen %v", it)
	}
	// Edit opens the editor in the main window, not modal.
	connMenu(t, tt, "web-1", locales.Edit())
	if spec := p.windows[WindowEditor].spec; !a.windowOpen(WindowEditor) || spec.Parent != WindowMain || spec.Modal || a.editor.id != "web-1" {
		t.Fatalf("editor %+v", spec)
	}
	a.closeWindow(WindowEditor)
	// A new connection, with the folder chosen, goes in it and is chosen
	// on the page.
	tt.ClickAt(connRow(t, tt, "Lab"))
	if err := tt.Click(locales.NewConnection()); err != nil {
		t.Fatal(err)
	}
	ed := editorWindow(t, a, p)
	ed.Type("lab.example.com")
	ed.Key(0, ui.KeyEnter)
	h := a.hosts.Hosts[len(a.hosts.Hosts)-1]
	if a.windowOpen(WindowEditor) || h.Addr != "lab.example.com" || h.Folder != "lab" {
		t.Fatalf("host %+v", h)
	}
	tt.Frame()
	if it, _ := a.connTree.chosen(); it.host != h.ID {
		t.Errorf("chosen %v; rows %q", it, connShown(a))
	}
	// A new folder from the room past the rows, at the top.
	a.connTree.tree.Open.Clear() // room past the rows
	tt.Frame()
	tt.RightClickAt(100, 790)
	if err := tt.ChooseMenuItem(locales.NewFolder()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	answer(t, tt, "Cloud")
	f := a.hosts.Folders[len(a.hosts.Folders)-1]
	if f.Name != "Cloud" || f.Parent != "" {
		t.Errorf("folder %+v", f)
	}
}

// The shortcut switches the pages: to the connections with the keyboard
// in the search, and back.
func TestConnectionPageShortcut(t *testing.T) {
	a, _ := testApp(t)
	managerHosts(a)
	tt := ui.NewTester(a.View, 1280, 820)
	tt.Frame()
	k := a.shortcut(scLeftPanel)
	tt.Key(k.Mods, k.Key)
	if a.left.page != leftConnections || !tt.Focused(locales.SearchConnections()) {
		t.Fatalf("page %d, search focused %v", a.left.page, tt.Focused(locales.SearchConnections()))
	}
	tt.Type("pi")
	if got := connShown(a); !slices.Equal(got, []string{"Lab", "  raspberry"}) {
		t.Errorf("rows %q", got)
	}
	tt.Key(k.Mods, k.Key)
	if a.left.page != leftSessions {
		t.Errorf("page %d", a.left.page)
	}
	// Rebound, the new key does it.
	rebind(&a.settings.Shortcuts, "linux", scLeftPanel, keyCombo{ui.Ctrl | ui.Alt, ui.KeyJ})
	tt.Key(ui.Ctrl|ui.Alt, ui.KeyJ)
	if a.left.page != leftConnections {
		t.Errorf("rebound: page %d", a.left.page)
	}
}
