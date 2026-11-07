package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// fakeFolderPicker stands in for the dialog that picks a folder to read,
// as fakeFilePicker does for files.
type fakeFolderPicker struct {
	folderMu    sync.Mutex
	folder      string
	folderAsked []filePick
}

func (p *fakeFolderPicker) OpenFolder(parent WindowKind, title string) (string, error) {
	p.folderMu.Lock()
	defer p.folderMu.Unlock()
	p.folderAsked = append(p.folderAsked, filePick{parent: parent, title: title})
	return p.folder, nil
}

// pickFolder is what the next folder dialogs return: "" for canceled.
func (p *fakeFolderPicker) pickFolder(path string) {
	p.folderMu.Lock()
	p.folder = path
	p.folderMu.Unlock()
}

func (p *fakeFolderPicker) folderPicks() []filePick {
	p.folderMu.Lock()
	defer p.folderMu.Unlock()
	return slices.Clone(p.folderAsked)
}

// finalShellSample is the importer's sample of FinalShell's data folder.
func finalShellSample(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "importer", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// openImport opens the manager, then the import window from its
// toolbar, and waits for it to read.
func openImport(t *testing.T, a *App, p *fakePlatform) (manager, imp *ui.Tester) {
	t.Helper()
	a.openManager()
	manager = p.show(t, WindowManager)
	manager.Frame()
	if err := manager.Click(locales.ImportFinalShell()); err != nil {
		t.Fatalf("%v; texts %q", err, manager.Texts())
	}
	if !a.windowOpen(WindowImport) || a.imp == nil {
		t.Fatal("the import window is not open")
	}
	p.until(t, "the folder read", func() bool { return !a.imp.busy })
	imp = p.show(t, WindowImport)
	imp.Frame()
	return manager, imp
}

// importBox is the check box of the row of the host named name.
func importBox(t *testing.T, tt *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	box, ok := tt.AccessByRole(ui.RoleCheckBox, name)
	if !ok {
		t.Fatalf("no check box %q", name)
	}
	return box
}

func TestImportFinalShell(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	managerHosts(a)
	// pg-main is here already, by its address, port and user.
	a.hosts.Add(&config.Host{ID: "mine", Name: "my pg", Addr: "db.example.com", Port: 5022, User: "postgres", Auth: config.AuthPassword})
	sample := finalShellSample(t, "finalshell")
	a.findFinalShell = func() string { return sample }
	manager, tt := openImport(t, a, p)
	spec := p.windows[WindowImport].spec
	if spec.Parent != WindowManager || !spec.Modal || spec.Title() != locales.ImportTitle() {
		t.Errorf("spec %+v", spec)
	}
	if len(p.folderPicks()) != 0 {
		t.Errorf("asked for the folder that was found: %v", p.folderPicks())
	}

	for _, s := range []string{"web-1", "bastion", "pg-main", "树莓派", "192.0.2.10", "bastion.example.com:2222", "db.example.com:5022",
		"deploy", "Production / Databases", "实验室 / Pi", locales.FinalShellFolderAt(filepath.Join(sample, "conn")),
		locales.ImportPresent("my pg"), locales.ImportSameAs("web-1"), locales.ImportPasswordsNote(),
		locales.ImportChosen(4, 6), locales.ImportCount(4), locales.ImportProblems(3, 2),
		locales.ProblemLine("1005_connect_config.json", locales.ProblemBroken()), locales.ProblemLine("1008_connect_config.json", locales.ProblemNoHost())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q", s)
		}
	}
	if !tt.HasText(locales.ImportNeedsKey()+locales.ListSeparator()+locales.ImportNoProxy()) || tt.HasText("c2FtcGxl") {
		t.Errorf("texts %q", tt.Texts())
	}
	for name, want := range map[string]bool{"web-1": true, "bastion": true, "树莓派": true, "192.0.2.40": true, "pg-main": false, "web-1 again": false} {
		box := importBox(t, tt, name)
		if box.Checked != want || box.Disabled == want {
			t.Errorf("%s: %+v", name, box)
		}
	}
	saveShot(t, tt, "import-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"从 FinalShell 导入", "全选", "全不选", "导入 4 个", "已选 4 / 6 个", "文件夹", "说明", "已存在：“my pg”", "导入后需要选择私钥文件", "选择文件夹…"} {
		if !tt.HasText(s) && spec.Title() != s {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-zh.png")
	p.SetLocale("")
	tt.Frame()

	// Space and Enter toggle the row chosen; a skipped row stays out.
	tt.ClickAt(rowCenter(t, tt, "bastion.example.com:2222"))
	if a.imp.row < 0 || a.imp.rows[a.imp.row].e.Host.Name != "bastion" {
		t.Fatalf("row %d", a.imp.row)
	}
	tt.Key(0, ui.KeySpace)
	if importBox(t, tt, "bastion").Checked {
		t.Error("space did not uncheck")
	}
	tt.Key(0, ui.KeyEnter)
	if !importBox(t, tt, "bastion").Checked {
		t.Error("enter did not check")
	}
	tt.ClickAt(rowCenter(t, tt, "db.example.com:5022"))
	tt.Key(0, ui.KeySpace)
	if importBox(t, tt, "pg-main").Checked {
		t.Error("a present host checked")
	}

	// Select none, then all: the hosts present stay out.
	if err := tt.Click(locales.SelectNone()); err != nil {
		t.Fatal(err)
	}
	if box, _ := tt.AccessByRole(ui.RoleButton, locales.ImportCount(0)); !box.Disabled || importBox(t, tt, "web-1").Checked {
		t.Errorf("none: import %+v", box)
	}
	if err := tt.Click(locales.SelectAll()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.ImportCount(4)) || importBox(t, tt, "pg-main").Checked {
		t.Errorf("all: texts %q", tt.Texts())
	}
	// Unchecking a row leaves it out.
	box := importBox(t, tt, "192.0.2.40")
	tt.ClickAt(box.Bounds.X+box.Bounds.W/2, box.Bounds.Y+box.Bounds.H/2)
	if importBox(t, tt, "192.0.2.40").Checked || !tt.HasText(locales.ImportCount(3)) {
		t.Fatalf("unchecked: texts %q", tt.Texts())
	}

	before := len(a.hosts.Hosts)
	if err := tt.Click(locales.ImportCount(3)); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowImport) || a.imp != nil {
		t.Error("the window stays open")
	}
	if len(a.hosts.Hosts) != before+3 {
		t.Fatalf("%d hosts added", len(a.hosts.Hosts)-before)
	}
	added := a.hosts.Hosts[before:]
	var names []string
	for _, h := range added {
		names = append(names, a.folderLabel(h.Folder)+"|"+h.Title())
	}
	// Into the folder named Production the store had, and new ones.
	if want := []string{"|web-1", "Production|bastion", "实验室 / Pi|树莓派"}; !slices.Equal(names, want) || added[1].Folder != "prod" {
		t.Errorf("added %q, want %q", names, want)
	}
	if h := added[1]; h.Auth != config.AuthKey || h.KeyPath != "" || h.Port != 2222 || h.Proxy != nil {
		t.Errorf("bastion %+v", h)
	}
	if strings.Contains(string(*saved), "c2FtcGxl") || !strings.Contains(string(*saved), "bastion.example.com") {
		t.Errorf("saved %s", *saved)
	}

	// The manager shows the first one chosen, its folders open, and how
	// it went.
	manager.Frame()
	manager.Frame()
	if it, ok := a.managerChosen(); !ok || it.host != added[0].ID {
		t.Errorf("chosen %+v", it)
	}
	if got := shown(a); !slices.Contains(got, "    树莓派") || !slices.Contains(got, "  bastion") {
		t.Errorf("rows %q", got)
	}
	result := locales.ImportResult(3, 5, 3) + " " + locales.ImportKeysNote(1)
	if !manager.HasText(result) {
		t.Errorf("no %q; texts %q", result, manager.Texts())
	}
	saveShot(t, manager, "import-done-en.png")

	// Importing again finds them all present.
	_, tt = openImport(t, a, p)
	if !tt.HasText(locales.ImportCount(1)) || !tt.HasText(locales.ImportPresent("bastion")) {
		t.Errorf("again: texts %q", tt.Texts())
	}
	a.closeWindow(WindowImport)
}

// Without FinalShell's folder the window asks for it, and says so when
// the choice is canceled; another folder can be chosen at any time.
func TestImportChooseFolder(t *testing.T) {
	a, p := testApp(t)
	a.findFinalShell = func() string { return "" }
	_, tt := openImport(t, a, p)
	picks := p.folderPicks()
	if len(picks) != 1 || picks[0].parent != WindowImport || picks[0].title != locales.ChooseFinalShellFolder() {
		t.Fatalf("picks %+v", picks)
	}
	if !tt.HasText(locales.FinalShellNotFound()) || a.imp.src != nil {
		t.Fatalf("texts %q", tt.Texts())
	}
	if box, _ := tt.AccessByRole(ui.RoleButton, locales.ImportCount(0)); !box.Disabled {
		t.Errorf("import %+v", box)
	}
	saveShot(t, tt, "import-not-found-en.png")

	// A folder without connections.
	empty := t.TempDir()
	p.pickFolder(empty)
	if err := tt.Click(locales.ChooseFolder()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the folder read", func() bool { return !a.imp.busy })
	tt.Frame()
	if !tt.HasText(locales.FinalShellNoConnections(empty)) {
		t.Errorf("texts %q", tt.Texts())
	}

	// An export, with its directories as folders.
	export := finalShellSample(t, "export")
	p.pickFolder(export)
	if err := tt.Click(locales.ChooseFolder()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the folder read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if !tt.HasText("stage-web") || !tt.HasText("Staging / Web") || !tt.HasText(locales.ImportCount(2)) {
		t.Errorf("texts %q", tt.Texts())
	}
	// Canceling another choice keeps what it shows.
	p.pickFolder("")
	if err := tt.Click(locales.ChooseFolder()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the dialog", func() bool { return !a.imp.busy })
	tt.Frame()
	if a.imp.src == nil || a.imp.dir != export || !tt.HasText("stage-web") {
		t.Errorf("after canceling: %+v", a.imp)
	}
	if err := tt.Click(locales.ImportCount(2)); err != nil {
		t.Fatal(err)
	}
	if len(a.hosts.Live()) != 2 || a.folderLabel(a.hosts.Live()[1].Folder) != "Staging / Web" {
		t.Errorf("hosts %+v", a.hosts.Live())
	}
}

// A host imported has no password: the first connection asks for it,
// with "Remember password" checked, and stores it once it logs in.
func TestImportedHostAsksPassword(t *testing.T) {
	srv := startFakeSSH(t)
	a, p := testApp(t)
	t.Cleanup(a.CloseAll)
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "conn"), 0o700)
	conn := `{"id":"1","name":"local","host":"` + srv.host + `","port":` + strconv.Itoa(srv.port) +
		`,"user_name":"u","conection_type":100,"authentication_type":1,"password":"c2FtcGxlLW5vdC1hLXJlYWwtc2VjcmV0"}`
	if err := os.WriteFile(filepath.Join(dir, "conn", "1_connect_config.json"), []byte(conn), 0o600); err != nil {
		t.Fatal(err)
	}
	a.findFinalShell = func() string { return dir }
	_, tt := openImport(t, a, p)
	if err := tt.Click(locales.ImportCount(1)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	h := a.hosts.Live()[0]
	if !h.RemembersPassword() || a.passwords[h.ID] != "" {
		t.Fatalf("host %+v", h)
	}

	main := ui.NewTester(a.View, 1280, 820)
	p.tt = main
	a.connectFromManager(h)
	p.until(t, "the prompt", func() bool { return a.pwOpen })
	if !a.pwRemember || a.pwErr != nil {
		t.Errorf("remember %v err %v", a.pwRemember, a.pwErr)
	}
	main.Frame()
	main.Type("secret")
	if err := main.Click(locales.Connect()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the connection", settled(a.current()))
	if s := a.current(); s.status != statusConnected {
		t.Fatalf("status %d err %v", s.status, s.err)
	}
	key := hostSecretKey(h.ID, secretPassword)
	p.until(t, "the password stored", func() bool { return p.secrets()[key] == "secret" })
}
