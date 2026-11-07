package app

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// managerHosts fills the store as a user's might be: folders two deep,
// hosts in them and at the top, one deleted.
func managerHosts(a *App) {
	s := a.hosts
	s.Folders = []*config.Folder{
		{ID: "prod", Name: "Production"},
		{ID: "db", Name: "Databases", Parent: "prod"},
		{ID: "lab", Name: "Lab"},
	}
	for _, h := range []*config.Host{
		{ID: "web-1", Name: "web-1", Addr: "203.0.113.10", User: "root", Folder: "prod", Notes: "nginx, front\nrack 4"},
		{ID: "web-2", Name: "web-2", Addr: "203.0.113.11", Port: 2222, User: "deploy", Folder: "prod"},
		{ID: "pg", Name: "pg-main", Addr: "10.0.0.5", User: "postgres", Folder: "db", Notes: "primary"},
		{ID: "pi", Name: "raspberry", Addr: "192.168.1.20", User: "pi", Folder: "lab"},
		{ID: "vps", Name: "vps", Addr: "vps.example.com", User: "admin"},
		{ID: "old", Name: "old-box", Addr: "10.0.0.99", User: "root", Folder: "lab", DeletedAt: time.Date(2026, 11, 1, 9, 30, 0, 0, time.Local)},
	} {
		h.Auth = config.AuthKey
		s.Add(h)
	}
}

// openTestManager opens the manager on the hosts of managerHosts and
// shows its window.
func openTestManager(t *testing.T) (*App, *fakePlatform, *ui.Tester) {
	t.Helper()
	a, p := testApp(t)
	managerHosts(a)
	a.openManager()
	tt := p.show(t, WindowManager)
	tt.Frame()
	return a, p, tt
}

// shown are the titles of the rows the manager shows, in order.
func shown(a *App) []string {
	m := &a.manager
	var out []string
	for i := range m.tree.Rows() {
		out = append(out, strings.Repeat("  ", m.tree.Depth(i))+a.managerTitle(m.tree.Item(i)))
	}
	return out
}

// rowMenu right-clicks the row showing name and chooses item, in a
// submenu after the path before it.
func rowMenu(t *testing.T, tt *ui.Tester, name string, path ...string) {
	t.Helper()
	tt.RightClickAt(rowCenter(t, tt, name))
	if err := tt.ChooseMenuItem(path...); err != nil {
		t.Fatalf("%s: %v; menu %q", name, err, tt.Menu())
	}
	tt.Frame()
}

// answer types name into the manager's prompt, and presses OK.
func answer(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(name)
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
}

func dragTo(tt *ui.Tester, fromX, fromY, toX, toY float32) {
	tt.Press(fromX, fromY)
	for k := 1; k <= 4; k++ {
		f := float32(k) / 4
		tt.Move(fromX+(toX-fromX)*f, fromY+(toY-fromY)*f)
	}
	tt.Release(toX, toY)
	tt.Frame()
}

func TestManagerWindow(t *testing.T) {
	a, p, tt := openTestManager(t)
	spec := p.windows[WindowManager].spec
	if spec.Title() != locales.ManagerTitle() || spec.Parent != WindowMain || spec.Modal || a.windowOpen(WindowEditor) {
		t.Fatalf("spec %+v, editor %v", spec, a.windowOpen(WindowEditor))
	}
	// Every folder open, folders first, the deleted host hidden.
	want := []string{"Production", "  Databases", "    pg-main", "  web-1", "  web-2", "Lab", "  raspberry", "vps"}
	if got := shown(a); !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
	for _, s := range []string{locales.HostColName(), locales.HostColHost(), locales.HostColPort(), locales.HostColUser(), locales.HostColNotes(),
		"203.0.113.11", "2222", "postgres", "nginx, front", locales.NewConnection(), locales.NewFolder(), locales.ShowDeleted(),
		locales.CloseAfterConnect(), locales.FilterAll()} {
		if !tt.HasText(s) {
			t.Errorf("missing %q", s)
		}
	}
	if tt.HasText("old-box") || tt.HasText("rack 4") {
		t.Errorf("a deleted host, or a second line of notes; texts %q", tt.Texts())
	}
	if box, ok := tt.AccessByRole(ui.RoleCheckBox, locales.CloseAfterConnect()); !ok || !box.Checked {
		t.Errorf("close after connecting %+v", box)
	}
	saveShot(t, tt, "manager-en.png")

	// Opening it again brings it to the front.
	a.openManager()
	if !slices.Equal(p.focused, []WindowKind{WindowManager}) {
		t.Errorf("focused %v", p.focused)
	}

	// Collapse all, then expand all.
	if err := tt.Click(locales.CollapseAll()); err != nil {
		t.Fatal(err)
	}
	if got := shown(a); !slices.Equal(got, []string{"Production", "Lab", "vps"}) {
		t.Errorf("collapsed %q", got)
	}
	if err := tt.Click(locales.ExpandAll()); err != nil {
		t.Fatal(err)
	}
	if got := shown(a); !slices.Equal(got, want) {
		t.Errorf("expanded %q", got)
	}

	// The filter shows one folder.
	if err := tt.Click(locales.FilterAll()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Production / Databases"); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if got := shown(a); a.manager.filter != "db" || !slices.Equal(got, []string{"pg-main"}) {
		t.Errorf("filter %q: %q", a.manager.filter, got)
	}
	a.manager.filter = ""
	tt.Frame()

	// Sorting by a column sorts the hosts in each folder, after its
	// folders.
	if err := tt.Click(locales.HostColUser()); err != nil {
		t.Fatal(err)
	}
	if got := shown(a); a.manager.sort.Column != "user" || !slices.Equal(got[:5], []string{"Production", "  Databases", "    pg-main", "  web-2", "  web-1"}) {
		t.Errorf("by user %v: %q", a.manager.sort, got)
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"连接管理器", "新建连接", "新建文件夹", "显示已删除", "连接后关闭窗口", "名称", "主机", "端口", "用户名", "备注"} {
		if !tt.HasText(s) && spec.Title() != s {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	a.manager.sort = ui.SortOrder{}
	tt.Frame()
	saveShot(t, tt, "manager-zh.png")
}

// With nothing to list, the manager opens the editor for a new host, modal
// to it; with a new host saved in a folder chosen, it goes in the folder.
func TestManagerEditorModal(t *testing.T) {
	a, p := testApp(t)
	a.hosts.Add(&config.Host{ID: "gone", Name: "gone-host", Addr: "10.0.0.9", User: "root", Auth: config.AuthPassword, DeletedAt: time.Now()})
	a.openManager()
	if !a.windowOpen(WindowEditor) || a.editor.id != "" {
		t.Fatalf("editor %v", a.windowOpen(WindowEditor))
	}
	if spec := p.windows[WindowEditor].spec; spec.Parent != WindowManager || !spec.Modal {
		t.Errorf("editor %+v", spec)
	}
	a.closeWindow(WindowManager)
	if a.windowOpen(WindowEditor) {
		t.Fatal("the editor outlived the manager")
	}

	// Opened from the main window, it is the main window's, and does not
	// block it, the manager open or not.
	a.newConnection()
	if spec := p.windows[WindowEditor].spec; spec.Parent != WindowMain || spec.Modal {
		t.Errorf("editor from the main window %+v", spec)
	}
	a.closeWindow(WindowEditor)

	// New connection with a folder chosen makes the host in it.
	a.hosts.Folders = []*config.Folder{{ID: "f", Name: "Lab"}}
	a.hosts.Add(&config.Host{ID: "x", Name: "x", Addr: "x.example.com", User: "root", Auth: config.AuthKey})
	a.openManager()
	tt := p.show(t, WindowManager)
	tt.Frame()
	tt.ClickAt(rowCenter(t, tt, "Lab"))
	if err := tt.Click(locales.NewConnection()); err != nil {
		t.Fatal(err)
	}
	ed := editorWindow(t, a, p)
	ed.Type("lab.example.com")
	ed.Key(0, ui.KeyEnter)
	h := a.hosts.Hosts[len(a.hosts.Hosts)-1]
	if a.windowOpen(WindowEditor) || h.Addr != "lab.example.com" || h.Folder != "f" {
		t.Fatalf("host %+v", h)
	}
	tt.Frame()
	if it, _ := a.managerChosen(); it.host != h.ID || !slices.Contains(shown(a), "  lab.example.com") {
		t.Errorf("chosen %v, rows %q", it, shown(a))
	}

	// On macOS the editor is a sheet, without a title bar: it shows its
	// title.
	sheets = true
	t.Cleanup(func() { sheets = false })
	rowMenu(t, tt, "x", locales.Edit())
	ed = editorWindow(t, a, p)
	if !ed.HasText(locales.EditConnection()) || !ed.HasText(locales.Ok()) {
		t.Errorf("sheet; texts %q", ed.Texts())
	}
	saveShot(t, ed, "manager-editor-sheet-en.png")
}

func TestManagerSearch(t *testing.T) {
	a, p, tt := openTestManager(t)
	tt.Click(locales.CollapseAll())
	for _, q := range []struct {
		query string
		want  []string
	}{
		{"pg", []string{"Production", "  Databases", "    pg-main"}}, // name
		{"203.0.11", []string{"Production", "  web-1", "  web-2"}},   // host
		{"DEPLOY", []string{"Production", "  web-2"}},                // user, in any case
		{"nginx", []string{"Production", "  web-1"}},                 // notes
		{"rack 4", []string{"Production", "  web-1"}},                // the second line of the notes
		{"web 2222", []string{"Production", "  web-2"}},              // every word
		{"old-box", nil}, // deleted
	} {
		a.manager.query = q.query
		a.searchChanged()
		tt.Frame()
		if got := shown(a); !slices.Equal(got, q.want) {
			t.Errorf("%q: %q, want %q", q.query, got, q.want)
		}
	}
	if !tt.HasText(locales.NoMatchingHosts()) {
		t.Errorf("no message; texts %q", tt.Texts())
	}

	// As the user types: the first host found is chosen, Enter connects
	// it.
	if err := tt.Click(locales.SearchHosts()); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("rasp")
	if got := shown(a); !slices.Equal(got, []string{"Lab", "  raspberry"}) {
		t.Fatalf("typed: %q", got)
	}
	if it, _ := a.managerChosen(); it.host != "pi" {
		t.Errorf("chosen %v", it)
	}
	saveShot(t, tt, "manager-search-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	saveShot(t, tt, "manager-search-zh.png")
	p.SetLocale("")

	// With deleted hosts shown, it finds them too.
	a.manager.showDeleted = true
	a.manager.query = "old"
	a.searchChanged()
	tt.Frame()
	if got := shown(a); !slices.Equal(got, []string{"Lab", "  old-box"}) {
		t.Errorf("deleted: %q", got)
	}

	// Clearing it, as Escape does, opens what was open before; the window
	// stays.
	tt.Key(0, ui.KeyEscape)
	if !a.windowOpen(WindowManager) {
		t.Fatal("Escape in the search closed the window")
	}
	if got := shown(a); a.manager.query != "" || !slices.Equal(got, []string{"Production", "Lab", "vps"}) {
		t.Errorf("cleared %q: %q", a.manager.query, got)
	}
	tt.Type("vps")
	tt.Key(0, ui.KeyEnter)
	if len(a.sessions) != 1 || a.sessions[0].Host != a.hosts.Host("vps") || a.windowOpen(WindowManager) {
		t.Errorf("Enter in the search: sessions %d, manager %v", len(a.sessions), a.windowOpen(WindowManager))
	}
	t.Cleanup(a.CloseAll)
}

func TestManagerFolders(t *testing.T) {
	a, p, tt := openTestManager(t)
	saves := 0
	a.saveHosts = func(*config.Store) error { saves++; return nil }

	// A new folder goes in the folder chosen.
	tt.ClickAt(rowCenter(t, tt, "Lab"))
	if err := tt.Click(locales.NewFolder()); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused(locales.FolderName()) || !tt.HasText(locales.NewFolder()) {
		t.Fatalf("no prompt; texts %q", tt.Texts())
	}
	// Escape closes the prompt alone.
	tt.Key(0, ui.KeyEscape)
	if !a.windowOpen(WindowManager) || a.manager.prompt != nil || len(a.hosts.Folders) != 3 {
		t.Fatalf("Escape: manager %v, prompt %v", a.windowOpen(WindowManager), a.manager.prompt)
	}
	tt.Click(locales.NewFolder())
	answer(t, tt, "Pis")
	f := a.hosts.Folders[3]
	if f.Name != "Pis" || f.Parent != "lab" || saves != 1 {
		t.Fatalf("folder %+v, %d saves", f, saves)
	}
	if got := shown(a); !slices.Equal(got[5:8], []string{"Lab", "  Pis", "  raspberry"}) {
		t.Errorf("rows %q", got)
	}
	if it, _ := a.managerChosen(); it.folder != f.ID {
		t.Errorf("chosen %v", it)
	}

	// A subfolder from the menu; a name is needed.
	rowMenu(t, tt, "Pis", locales.NewSubfolder())
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	if b, ok := tt.AccessByRole(ui.RoleButton, locales.Ok()); !ok || !b.Disabled {
		t.Errorf("OK with no name: %+v", b)
	}
	tt.Type("Zero")
	tt.Key(0, ui.KeyEnter)
	sub := a.hosts.Folders[4]
	if sub.Name != "Zero" || sub.Parent != f.ID {
		t.Fatalf("subfolder %+v", sub)
	}

	// Rename.
	rowMenu(t, tt, "Pis", locales.Rename())
	answer(t, tt, "Raspberries")
	if f.Name != "Raspberries" {
		t.Errorf("renamed %q", f.Name)
	}

	// Deleting a folder says where what it holds goes, and moves it there.
	a.hosts.Host("pi").Folder = f.ID
	tt.Frame()
	rowMenu(t, tt, "Raspberries", locales.Delete())
	if !tt.HasText(locales.DeleteFolderTitle("Raspberries")) || !tt.HasText(locales.DeleteFolderBody("Lab")) {
		t.Fatalf("no question; texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager-delete-folder-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("其中的连接和文件夹会移到“Lab”。") {
		t.Errorf("not in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager-delete-folder-zh.png")
	p.SetLocale("")
	tt.Frame()
	if err := tt.Click(locales.Delete()); err != nil {
		t.Fatal(err)
	}
	if a.hosts.Folder(f.ID) != nil || sub.Parent != "lab" || a.hosts.Host("pi").Folder != "lab" {
		t.Fatalf("folders %v, pi in %q", a.hosts.Folders, a.hosts.Host("pi").Folder)
	}
	// At the top, they go to the top.
	rowMenu(t, tt, "Production", locales.Delete())
	if !tt.HasText(locales.DeleteFolderBodyTop()) {
		t.Errorf("texts %q", tt.Texts())
	}
	tt.Click(locales.Cancel())
	if a.hosts.Folder("prod") == nil {
		t.Error("Cancel deleted it")
	}
}

func TestManagerMove(t *testing.T) {
	a, _, tt := openTestManager(t)
	saves := 0
	a.saveHosts = func(*config.Store) error { saves++; return nil }

	// A host dragged onto a folder goes in it.
	x, y := rowCenter(t, tt, "vps")
	tx, ty := rowCenter(t, tt, "Lab")
	dragTo(tt, x-200, y, tx-200, ty)
	if h := a.hosts.Host("vps"); h.Folder != "lab" || saves != 1 {
		t.Fatalf("vps in %q, %d saves; rows %q", h.Folder, saves, shown(a))
	}
	// Onto a host, in its folder.
	x, y = rowCenter(t, tt, "raspberry")
	tx, ty = rowCenter(t, tt, "pg-main")
	dragTo(tt, x-200, y, tx-200, ty)
	if h := a.hosts.Host("pi"); h.Folder != "db" {
		t.Fatalf("pi in %q; rows %q", h.Folder, shown(a))
	}
	// A folder into another.
	x, y = rowCenter(t, tt, "Lab")
	tx, ty = rowCenter(t, tt, "web-2")
	dragTo(tt, x-200, y, tx-200, ty)
	if f := a.hosts.Folder("lab"); f.Parent != "prod" {
		t.Fatalf("Lab in %q; rows %q", f.Parent, shown(a))
	}
	// Not into itself or a folder inside it.
	a.moveItems(managerDrag{{folder: "prod"}}, "lab")
	a.moveItems(managerDrag{{folder: "prod"}}, "prod")
	if f := a.hosts.Folder("prod"); f.Parent != "" || saves != 3 {
		t.Fatalf("Production in %q, %d saves", f.Parent, saves)
	}
	if want := []string{"Production", "  Databases", "    pg-main", "    raspberry", "  Lab", "    vps", "  web-1", "  web-2"}; !slices.Equal(shown(a), want) {
		t.Errorf("rows %q, want %q", shown(a), want)
	}

	// With the menu, to the top level.
	tt.Frame()
	rowMenu(t, tt, "vps", locales.MoveToFolder(), locales.TopLevel())
	if h := a.hosts.Host("vps"); h.Folder != "" {
		t.Fatalf("vps in %q", h.Folder)
	}
	rowMenu(t, tt, "Lab", locales.MoveToFolder(), locales.TopLevel())
	if f := a.hosts.Folder("lab"); f.Parent != "" {
		t.Fatalf("Lab in %q", f.Parent)
	}
	// Dragged past the rows, to the top too.
	x, y = rowCenter(t, tt, "pg-main")
	dragTo(tt, x-200, y, x-200, 560)
	if h := a.hosts.Host("pg"); h.Folder != "" {
		t.Errorf("pg in %q; rows %q", h.Folder, shown(a))
	}
}

func TestManagerSoftDeleteAndRestore(t *testing.T) {
	a, p, tt := openTestManager(t)
	saved := savedHosts(a)
	rowMenu(t, tt, "web-2", locales.Delete())
	h := a.hosts.Host("web-2")
	if !h.Deleted() || slices.Contains(shown(a), "  web-2") || !strings.Contains(string(*saved), `"deletedAt"`) {
		t.Fatalf("deleted %v; rows %q", h.DeletedAt, shown(a))
	}
	// The Delete key, on the host chosen.
	tt.ClickAt(rowCenter(t, tt, "vps"))
	tt.Key(0, ui.KeyDelete)
	if !a.hosts.Host("vps").Deleted() {
		t.Fatal("Delete deleted nothing")
	}

	// Shown greyed, with their own menu.
	if err := tt.Click(locales.ShowDeleted()); err != nil {
		t.Fatal(err)
	}
	if got := shown(a); !slices.Contains(got, "  web-2") || !slices.Contains(got, "  old-box") || !slices.Contains(got, "vps") {
		t.Fatalf("show deleted: %q", got)
	}
	tt.RightClickAt(rowCenter(t, tt, "web-2"))
	if menu := tt.Menu(); !slices.Equal(menu, []string{locales.Restore(), locales.DeleteForever(), "", locales.CopyHost()}) &&
		!slices.Equal(menu, []string{locales.Restore(), locales.DeleteForever(), "-", locales.CopyHost()}) {
		t.Errorf("menu %q", menu)
	}
	tt.CloseMenu()
	saveShot(t, tt, "manager-deleted-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("显示已删除") {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager-deleted-zh.png")
	p.SetLocale("")
	tt.Frame()

	// A deleted host does not connect.
	tt.ClickAt(rowCenter(t, tt, "web-2"))
	tt.Key(0, ui.KeyEnter)
	if len(a.sessions) != 0 {
		t.Fatal("a deleted host connected")
	}
	rowMenu(t, tt, "web-2", locales.Restore())
	if h.Deleted() || !a.windowOpen(WindowManager) {
		t.Fatalf("restored %v", h.DeletedAt)
	}
	if it, _ := a.managerChosen(); it.host != "web-2" {
		t.Errorf("chosen %v", it)
	}
	tt.Click(locales.ShowDeleted())
	if got := shown(a); !slices.Contains(got, "  web-2") || slices.Contains(got, "  old-box") {
		t.Errorf("rows %q", got)
	}
}

func TestManagerDeleteForever(t *testing.T) {
	a, p, tt := openTestManager(t)
	saved := savedHosts(a)
	a.passwords["old"] = "pw"
	a.manager.showDeleted = true
	tt.Frame()
	rowMenu(t, tt, "old-box", locales.DeleteForever())
	if !tt.HasText(locales.DeleteForeverTitle("old-box")) || !tt.HasText(locales.DeleteForeverBody()) {
		t.Fatalf("no question; texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager-delete-forever-en.png")
	if err := tt.Click(locales.Cancel()); err != nil {
		t.Fatal(err)
	}
	if a.hosts.Host("old") == nil {
		t.Fatal("Cancel deleted it")
	}
	tt.ClickAt(rowCenter(t, tt, "old-box"))
	tt.Key(0, ui.KeyDelete) // on a deleted host, asks the same
	if err := tt.Click(locales.DeleteForever()); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	if a.hosts.Host("old") != nil || len(a.hosts.Hosts) != 5 || strings.Contains(string(*saved), "old-box") {
		t.Fatalf("hosts %v; saved %s", a.hosts.Hosts, *saved)
	}
	if _, ok := a.passwords["old"]; ok {
		t.Error("its password is kept")
	}
	if p.redraws == 0 || slices.Contains(shown(a), "  old-box") {
		t.Errorf("rows %q", shown(a))
	}

	// A failed save says so in the manager.
	a.saveHosts = func(*config.Store) error { return errors.New("disk full") }
	a.deleteHostForever(a.hosts.Host("vps"))
	tt.Frame()
	if !tt.HasText(locales.ErrSaveFailed("disk full")) {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestManagerDuplicateRenameCopy(t *testing.T) {
	a, _, tt := openTestManager(t)
	a.passwords["web-1"] = "phrase"
	rowMenu(t, tt, "web-1", locales.Duplicate())
	i := slices.IndexFunc(a.hosts.Hosts, func(h *config.Host) bool { return h.ID == "web-1" })
	dup := a.hosts.Hosts[i+1]
	if dup.ID == "web-1" || dup.Name != "web-1 (copy)" || dup.Addr != "203.0.113.10" || dup.Folder != "prod" || dup.Notes == "" ||
		a.passwords[dup.ID] != "phrase" || !dup.LastConnectedAt.IsZero() {
		t.Fatalf("copy %+v", dup)
	}
	if it, _ := a.managerChosen(); it.host != dup.ID {
		t.Errorf("chosen %v", it)
	}

	rowMenu(t, tt, "web-1 (copy)", locales.Rename())
	answer(t, tt, "web-3")
	if dup.Name != "web-3" {
		t.Errorf("renamed %q", dup.Name)
	}
	// F2 renames too; a host may go without a name.
	tt.ClickAt(rowCenter(t, tt, "web-3"))
	tt.Key(0, ui.KeyF2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyEnter)
	if dup.Name != "" || !slices.Contains(shown(a), "  203.0.113.10") {
		t.Errorf("name %q; rows %q", dup.Name, shown(a))
	}

	rowMenu(t, tt, "pg-main", locales.CopyHost())
	if got := tt.Clipboard(); got != "10.0.0.5" {
		t.Errorf("copied %q", got)
	}
}

// TestManagerMenu shows the context menus, drawn in the window for the
// screenshot.
func TestManagerMenu(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			managerHosts(a)
			a.openManager()
			w := p.windows[WindowManager]
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetMenuPresentation(ui.PopoverMenus)
				w.view(c)
			}, w.spec.Width, w.spec.Height)
			tt.SetScale(2)
			tt.SetLocale(lang)
			tt.Frame()
			tt.RightClickAt(rowCenter(t, tt, "web-1"))
			tt.Frame()
			want := []string{locales.Connect(), locales.Edit(), locales.Duplicate(), locales.Rename(), locales.MoveToFolder(), locales.CopyHost(), locales.Delete()}
			for _, s := range want {
				if !tt.HasText(s) {
					t.Errorf("menu without %q; texts %q", s, tt.Texts())
				}
			}
			saveShot(t, tt, "manager-menu-"+lang+".png")
			tt.CloseMenu()
			tt.RightClickAt(rowCenter(t, tt, "Lab"))
			tt.Frame()
			for _, s := range []string{locales.NewSubfolder(), locales.Rename(), locales.Delete()} {
				if !tt.HasText(s) {
					t.Errorf("folder menu without %q", s)
				}
			}
			saveShot(t, tt, "manager-folder-menu-"+lang+".png")
		})
	}
}

// Connecting from the manager opens the session in the main window, in the
// new tab selected there, brings the main window forward, and closes the
// manager unless it is to stay open.
func TestConnectFromManager(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	managerHosts(a)
	h := a.hosts.Host("web-2")
	h.Addr, h.Port, h.User, h.Auth = srv.host, srv.port, "u", config.AuthPassword
	h.NoMonitor = true
	a.passwords[h.ID] = "secret"
	a.NewTab()
	tab := a.sessions[0]
	mainWin := ui.NewTester(a.View, 1280, 820)
	p.tt = mainWin
	a.openManager()
	tt := p.show(t, WindowManager)
	tt.Frame()
	x, y := rowCenter(t, tt, "web-2")
	tt.ClickAt(x, y)
	tt.ClickAt(x, y) // a double click
	if a.windowOpen(WindowManager) || len(a.sessions) != 1 || tab.Host != h || a.current() != tab {
		t.Fatalf("manager %v, %d sessions, tab host %v", a.windowOpen(WindowManager), len(a.sessions), tab.Host)
	}
	if !slices.Contains(p.focused, WindowMain) {
		t.Errorf("focused %v", p.focused)
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

	// Kept open: the next session gets a tab of its own.
	a.openManager()
	tt = p.show(t, WindowManager)
	tt.Frame()
	if err := tt.Click(locales.CloseAfterConnect()); err != nil {
		t.Fatal(err)
	}
	if !a.settings.KeepManagerOpen || !config.LoadSettings().KeepManagerOpen {
		t.Fatal("the setting is not kept")
	}
	rowMenu(t, tt, "web-2", locales.Connect())
	if !a.windowOpen(WindowManager) || len(a.sessions) != 2 || a.sessions[1].Host != h {
		t.Fatalf("manager %v, %d sessions", a.windowOpen(WindowManager), len(a.sessions))
	}
	p.until(t, "the second shell", func() bool {
		s := a.sessions[1]
		return s.status == statusConnected || s.status == statusFailed
	})
}

// The manager and the Proxy page agree on jump hosts: a copy keeps its
// proxy and its password, and deleting a jump host in use says which
// hosts it leaves without a way in.
func TestManagerJumpHosts(t *testing.T) {
	a, p, tt := openTestManager(t)
	web := a.hosts.Host("web-1")
	web.Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "vps"}
	pg := a.hosts.Host("pg")
	pg.Proxy = &config.ProxyConfig{Kind: config.ProxySOCKS5, Addr: "127.0.0.1", Port: 1080, User: "u"}
	a.passwords[proxySecretKey("pg")] = "proxy-pw"
	pg.Tunnels = []config.Tunnel{
		{Kind: config.TunnelLocal, ListenPort: 15432, TargetHost: "127.0.0.1", TargetPort: 5432},
		{Kind: config.TunnelDynamic, ListenPort: 1080, Disabled: true},
	}

	rowMenu(t, tt, "pg-main", locales.Duplicate())
	dup := a.hosts.Hosts[slices.Index(a.hosts.Hosts, pg)+1]
	if dup.Proxy == nil || dup.Proxy == pg.Proxy || dup.Proxy.Addr != "127.0.0.1" || dup.Proxy.User != "u" ||
		a.passwords[proxySecretKey(dup.ID)] != "proxy-pw" {
		t.Fatalf("copy's proxy %+v, password %q", dup.Proxy, a.passwords[proxySecretKey(dup.ID)])
	}
	// Its tunnels, of its own.
	if !reflect.DeepEqual(dup.Tunnels, pg.Tunnels) {
		t.Fatalf("copy's tunnels %+v", dup.Tunnels)
	}
	dup.Tunnels[0].ListenPort = 25432
	if pg.Tunnels[0].ListenPort != 15432 {
		t.Error("the copy shares the tunnels")
	}
	rowMenu(t, tt, "web-1", locales.Duplicate())
	wdup := a.hosts.Hosts[slices.Index(a.hosts.Hosts, web)+1]
	if jumps, err := a.hosts.JumpHosts(wdup); err != nil || len(jumps) != 1 || jumps[0].ID != "vps" {
		t.Fatalf("copy's jump hosts %v, %v", jumps, err)
	}

	// vps is the jump host of web-1 and its copy: deleting it asks.
	rowMenu(t, tt, "vps", locales.Delete())
	users := "“web-1”, “web-1 (copy)”"
	if a.hosts.Host("vps").Deleted() || !tt.HasText(locales.DeleteHostTitle("vps")) || !tt.HasText(locales.JumpHostInUse(users)) {
		t.Fatalf("no question; texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager-delete-jump-host-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("它是 “web-1”、“web-1 (copy)” 的跳板机，恢复之前这些连接都连不上。") {
		t.Errorf("not in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "manager-delete-jump-host-zh.png")
	p.SetLocale("")
	tt.Frame()
	if err := tt.Click(locales.Delete()); err != nil {
		t.Fatal(err)
	}
	if !a.hosts.Host("vps").Deleted() {
		t.Fatal("not deleted")
	}
	if _, err := a.hosts.JumpHosts(web); err == nil {
		t.Error("a deleted jump host still works")
	}

	// Deleting it for good says so too, and forgets its proxy password.
	a.manager.showDeleted = true
	a.passwords[proxySecretKey("vps")] = "x"
	tt.Frame()
	rowMenu(t, tt, "vps", locales.DeleteForever())
	if !tt.HasText(locales.DeleteForeverBody() + " " + locales.JumpHostInUseForever(users)) {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click(locales.DeleteForever()); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.passwords[proxySecretKey("vps")]; ok || a.hosts.Host("vps") != nil {
		t.Error("the proxy password is kept")
	}

	// A jump host restored works again.
	a.hosts.Host("pi").Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: "old"}
	tt.Frame()
	rowMenu(t, tt, "old-box", locales.Restore())
	if _, err := a.hosts.JumpHosts(a.hosts.Host("pi")); err != nil {
		t.Error(err)
	}
}
