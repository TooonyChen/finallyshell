package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fakeFolderPicker stands in for the dialogs that pick a folder, or a
// file of a type, to import from, as fakeFilePicker does for key files.
type fakeFolderPicker struct {
	folderMu    sync.Mutex
	folder      string
	folderAsked []filePick
	folderErr   error // what the dialogs fail with
	file        string
	fileAsked   []typedPick
}

// typedPick is what a file of a type was asked for with.
type typedPick struct {
	filePick
	typeName   string
	extensions []string
}

func (p *fakeFolderPicker) OpenFileOfType(parent WindowKind, title, typeName string, extensions []string) (string, error) {
	p.folderMu.Lock()
	defer p.folderMu.Unlock()
	p.fileAsked = append(p.fileAsked, typedPick{filePick{parent: parent, title: title}, typeName, extensions})
	if p.folderErr != nil {
		return "", p.folderErr
	}
	return p.file, nil
}

// failPicks makes the next dialogs fail with err, nil for none.
func (p *fakeFolderPicker) failPicks(err error) {
	p.folderMu.Lock()
	p.folderErr = err
	p.folderMu.Unlock()
}

// pickTyped is what the next dialogs for a file of a type return: "" for
// canceled.
func (p *fakeFolderPicker) pickTyped(path string) {
	p.folderMu.Lock()
	p.file = path
	p.folderMu.Unlock()
}

func (p *fakeFolderPicker) typedPicks() []typedPick {
	p.folderMu.Lock()
	defer p.folderMu.Unlock()
	return slices.Clone(p.fileAsked)
}

// findFinalShell makes find what looks for FinalShell's folder.
func findFinalShell(a *App, find func() string) {
	for i, s := range a.importSources {
		if s.Kind == importer.FinalShell {
			r := *s.Reader
			r.Find = find
			a.importSources[i].Reader = &r
		}
	}
}

func (p *fakeFolderPicker) OpenFolder(parent WindowKind, title string) (string, error) {
	p.folderMu.Lock()
	defer p.folderMu.Unlock()
	p.folderAsked = append(p.folderAsked, filePick{parent: parent, title: title})
	if p.folderErr != nil {
		return "", p.folderErr
	}
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
	clickTool(t, manager, locales.ImportTitle())
	if !a.windowOpen(WindowImport) || a.imp == nil {
		t.Fatal("the import window is not open")
	}
	p.until(t, "the folder read", func() bool { return !a.imp.busy })
	imp = p.show(t, WindowImport)
	imp.Frame()
	return manager, imp
}

// clickTool clicks the toolbar control labeled label, through the
// toolbar's overflow menu ("More") when the toolbar has no room for it.
// How much fits depends on the fonts of the machine the tests run on.
func clickTool(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err == nil {
		return
	}
	if err := tt.Click("More"); err != nil {
		t.Fatalf("no %q in the toolbar or its overflow menu: %v; texts %q", label, err, tt.Texts())
	}
	if err := tt.ChooseMenuItem(label); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
}

// A manager too narrow for the import tool still imports, from the
// toolbar's overflow menu.
func TestImportFromOverflowMenu(t *testing.T) {
	a, p := testApp(t)
	findFinalShell(a, func() string { return finalShellSample(t, "finalshell") })
	a.openManager()
	manager := p.show(t, WindowManager)
	manager.SetSize(420, 500)
	manager.Frame()
	if err := manager.Click(locales.ImportTitle()); err == nil {
		t.Fatal("the import tool fits a narrow manager; the test needs it in the overflow menu")
	}
	clickTool(t, manager, locales.ImportTitle())
	if !a.windowOpen(WindowImport) || a.imp == nil {
		t.Fatal("the import window is not open")
	}
}

func TestTildePath(t *testing.T) {
	for _, c := range []struct{ path, home, goos, want string }{
		{"/home/ann/.finalshell/conn", "/home/ann", "linux", "~/.finalshell/conn"},
		{"/home/ann/.finalshell/conn", "/home/ann/", "linux", "~/.finalshell/conn"},
		{"/Users/ann/Library/FinalShell/conn", "/Users/ann", "darwin", "~/Library/FinalShell/conn"},
		{"/home/annie/x", "/home/ann", "linux", "/home/annie/x"},
		{"/home/ann", "/home/ann", "linux", "/home/ann"},
		{"/etc/ssh/ssh_config", "/", "linux", "/etc/ssh/ssh_config"},
		{"/x", "", "linux", "/x"},
		{`C:\Users\ann\AppData\Local\finalshell\conn`, `C:\Users\ann`, "windows", `C:\Users\ann\AppData\Local\finalshell\conn`},
	} {
		if got := tildePath(c.path, c.home, c.goos); got != c.want {
			t.Errorf("%q in %q on %s: %q", c.path, c.home, c.goos, got)
		}
	}
}

// sourcesShot saves a frame of the window with its list of sources open,
// with SHOT_DIR set, and closes the list.
func sourcesShot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if os.Getenv("SHOT_DIR") == "" {
		return
	}
	sel, ok := tt.AccessByRole(ui.RolePopUpButton, locales.ImportFrom())
	if !ok {
		t.Fatalf("no select; texts %q", tt.Texts())
	}
	tt.ClickAt(sel.Bounds.X+sel.Bounds.W/2, sel.Bounds.Y+sel.Bounds.H/2)
	tt.Frame()
	saveShot(t, tt, name)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
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
	// FinalShell's folder in a home folder, which the window writes "~".
	home := t.TempDir()
	sample := filepath.Join(home, ".finalshell")
	if err := os.CopyFS(sample, os.DirFS(finalShellSample(t, "finalshell"))); err != nil {
		t.Fatal(err)
	}
	a.home = func() string { return home }
	findFinalShell(a, func() string { return sample })
	manager, tt := openImport(t, a, p)
	spec := p.windows[WindowImport].spec
	if spec.Parent != WindowManager || !spec.Modal || spec.Title() != locales.ImportTitle() {
		t.Errorf("spec %+v", spec)
	}
	if len(p.folderPicks()) != 0 {
		t.Errorf("asked for the folder that was found: %v", p.folderPicks())
	}

	for _, s := range []string{"web-1", "bastion", "pg-main", "树莓派", "192.0.2.10", "bastion.example.com:2222", "db.example.com:5022",
		"deploy", "Production / Databases", "实验室 / Pi", tildePath(filepath.Join(sample, "conn"), home, runtime.GOOS), locales.ImportFrom(), locales.ChooseFolder(),
		locales.ImportPresent("my pg"), locales.ImportSameAs("web-1"), locales.ImportPasswordsNote("FinalShell"),
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
	sourcesShot(t, tt, "import-sources-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"导入连接", "导入来源", "全选", "全不选", "导入 4 个", "已选 4 / 6 个", "文件夹", "说明", "已存在：“my pg”", "导入后需要选择私钥文件", "代理没有导入", "选择文件夹…", "密码不导入（FinalShell 对密码做了加密），第一次连接时会询问密码。"} {
		if !tt.HasText(s) && spec.Title() != s {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-zh.png")
	sourcesShot(t, tt, "import-sources-zh.png")
	tt.SetDark(true)
	tt.Frame()
	if theme != darkTheme {
		t.Error("not dark")
	}
	saveShot(t, tt, "import-dark-zh.png")
	sourcesShot(t, tt, "import-sources-dark-zh.png")
	tt.SetDark(false)
	tt.Frame()
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
		// Added now, for quick connect to list them first of the others.
		if time.Since(h.CreatedAt) > time.Minute {
			t.Errorf("%s created %v", h.Title(), h.CreatedAt)
		}
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
	if it, ok := a.manager.chosen(); !ok || it.host != added[0].ID {
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
	findFinalShell(a, func() string { return "" })
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
	if !tt.HasText(locales.ImportNoConnections(empty)) {
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
	if a.imp.src == nil || a.imp.path != export || !tt.HasText("stage-web") {
		t.Errorf("after canceling: %+v", a.imp)
	}
	if err := tt.Click(locales.ImportCount(2)); err != nil {
		t.Fatal(err)
	}
	if len(a.hosts.Live()) != 2 || a.folderLabel(a.hosts.Live()[1].Folder) != "Staging / Web" {
		t.Errorf("hosts %+v", a.hosts.Live())
	}
}

// When the chooser itself fails, the window says so and offers it again.
func TestImportChooserFails(t *testing.T) {
	a, p := testApp(t)
	findFinalShell(a, func() string { return "" })
	p.failPicks(errors.New("no portal"))
	_, tt := openImport(t, a, p)
	if !tt.HasText(locales.ImportReadFailed("no portal")) || a.imp.path != "" {
		t.Fatalf("texts %q", tt.Texts())
	}
	p.failPicks(nil)
	p.pickFolder(finalShellSample(t, "finalshell"))
	if err := tt.Click(locales.ChooseFolder()); err != nil {
		t.Fatalf("no chooser: %v; texts %q", err, tt.Texts())
	}
	p.until(t, "the folder read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if !tt.HasText("web-1") || a.imp.err != nil {
		t.Errorf("texts %q", tt.Texts())
	}

	// A file source too, after a canceled source switch.
	addListSource(a)
	p.failPicks(errors.New("no portal"))
	choose(t, tt, locales.ImportFrom(), "Server list")
	p.until(t, "the chooser", func() bool { return !a.imp.busy })
	tt.Frame()
	p.failPicks(nil)
	p.pickTyped(filepath.Join(t.TempDir(), "servers.csv"))
	if err := tt.Click(locales.ChooseFile()); err != nil {
		t.Fatalf("no chooser: %v; texts %q", err, tt.Texts())
	}
	p.until(t, "the list read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if !tt.HasText("app-1") {
		t.Errorf("texts %q", tt.Texts())
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
	findFinalShell(a, func() string { return dir })
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

// Every source of importer has its texts, and so is listed.
func TestImportSourcesListed(t *testing.T) {
	sources := allImportSources()
	if len(sources) != len(importer.Readers()) {
		t.Fatalf("listed %d of %d sources: each needs its texts in importTexts", len(sources), len(importer.Readers()))
	}
	for _, s := range sources {
		if s.name == nil || s.choose == nil || s.notFound == nil || s.name() == "" ||
			!s.Folder && len(s.Extensions) > 0 && s.fileTypes == nil {
			t.Errorf("%s: texts %+v", s.Kind, s.importText)
		}
	}
	if sources[0].Kind != importer.FinalShell {
		t.Errorf("first %s", sources[0].Kind)
	}
}

// listSource is a made-up source of a file, as a list of servers, which
// addListSource adds to a's sources.
const listSource importer.Kind = "test-list"

func addListSource(a *App) {
	read := func(path string) (*importer.Source, error) {
		return &importer.Source{Kind: listSource, Path: path, Entries: []*importer.Entry{
			{File: "servers.csv:2", Host: &config.Host{Name: "app-1", Addr: "192.0.2.50", User: "ops", Auth: config.AuthPassword}, Password: "sample-not-a-real-secret"},
			{File: "servers.csv:3", Host: &config.Host{Name: "app-2", Addr: "192.0.2.51", User: "ops", Auth: config.AuthPassword}, KeyFile: "~/.ssh/id_ed25519", Note: "test-note"},
		}, Problems: []importer.Problem{importer.NewProblem("servers.csv", 4, importer.NoHost, nil)}}, nil
	}
	a.importSources = append(a.importSources, importSource{
		Reader: &importer.Reader{Kind: listSource, Extensions: []string{"csv", "txt"}, Read: read},
		importText: importText{
			name:      func() string { return "Server list" },
			choose:    func() string { return "Choose a server list" },
			fileTypes: func() string { return "Server lists" },
			notFound:  func() string { return "Choose a list of servers." },
		},
	})
}

// The window opens on FinalShell, then on the source chosen last.
// Choosing a source of a file without a place of its own asks for the
// file; choosing FinalShell again finds it again.
func TestImportSources(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	sample := finalShellSample(t, "finalshell")
	findFinalShell(a, func() string { return sample })
	addListSource(a)
	_, tt := openImport(t, a, p)
	if a.imp.source.Kind != importer.FinalShell || !tt.HasText("web-1") {
		t.Fatalf("opened on %s; texts %q", a.imp.source.Kind, tt.Texts())
	}
	if sel, ok := tt.AccessByRole(ui.RolePopUpButton, locales.ImportFrom()); !ok || sel.Value != "FinalShell" {
		t.Errorf("select %+v", sel)
	}

	// The list: its chooser opens at once; canceled, the window says what
	// to choose.
	choose(t, tt, locales.ImportFrom(), "Server list")
	p.until(t, "the chooser", func() bool { return !a.imp.busy })
	tt.Frame()
	picks := p.typedPicks()
	if len(picks) != 1 || picks[0].parent != WindowImport || picks[0].title != "Choose a server list" ||
		picks[0].typeName != "Server lists" || !slices.Equal(picks[0].extensions, []string{"csv", "txt"}) {
		t.Fatalf("picks %+v", picks)
	}
	if !tt.HasText("Choose a list of servers.") || tt.HasText("web-1") || tt.HasText(locales.ChooseFolder()) ||
		tt.HasText(locales.ImportPasswordsNote("FinalShell")) || a.importKind != listSource {
		t.Errorf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "import-source-en.png")

	list := filepath.Join(t.TempDir(), "servers.csv")
	p.pickTyped(list)
	if err := tt.Click(locales.ChooseFile()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the list read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	for _, s := range []string{"app-1", "app-2", list, "test-note", locales.ImportCount(2), locales.ImportProblems(1, 0),
		locales.ProblemLine("servers.csv:4", locales.ProblemNoHost())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-list-en.png")

	// FinalShell again, found again; then the list again, chosen again.
	choose(t, tt, locales.ImportFrom(), locales.ImportSourceFinalShell())
	p.until(t, "FinalShell read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()
	if a.imp.src.Kind != importer.FinalShell || !tt.HasText("web-1") || tt.HasText("app-1") || !tt.HasText(locales.ImportPasswordsNote("FinalShell")) {
		t.Fatalf("texts %q", tt.Texts())
	}
	if len(p.folderPicks()) != 0 || len(p.typedPicks()) != 2 {
		t.Errorf("picks %v %v", p.folderPicks(), p.typedPicks())
	}
	choose(t, tt, locales.ImportFrom(), "Server list")
	p.until(t, "the list read", func() bool { return !a.imp.busy && a.imp.src != nil })
	tt.Frame()

	// Importing keeps the list's password in the credential store, not in
	// hosts.json, and makes its key file the host's key.
	if err := tt.Click(locales.ImportCount(2)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	hosts := a.hosts.Live()
	if len(hosts) != 2 || hosts[0].Name != "app-1" || hosts[1].Auth != config.AuthKey || hosts[1].KeyPath != "~/.ssh/id_ed25519" {
		t.Fatalf("hosts %+v", hosts)
	}
	key := hostSecretKey(hosts[0].ID, secretPassword)
	p.until(t, "the password stored", func() bool { return p.secrets()[key] == "sample-not-a-real-secret" })
	if a.passwords[hosts[0].ID] != "sample-not-a-real-secret" || !hosts[0].RemembersPassword() {
		t.Errorf("password %q", a.passwords[hosts[0].ID])
	}
	if _, ok := p.secrets()[hostSecretKey(hosts[1].ID, secretPassword)]; ok {
		t.Error("a password stored for the key host")
	}
	if strings.Contains(string(*saved), "sample-not-a-real-secret") {
		t.Errorf("saved %s", *saved)
	}
	// No key file to choose: the list's host has its own.
	manager := p.show(t, WindowManager)
	manager.Frame()
	if want := locales.ImportResult(2, 0, 1); !manager.HasText(want) || manager.HasText(locales.ImportKeysNote(1)) {
		t.Errorf("no %q; texts %q", want, manager.Texts())
	}

	// The window opens on the list the next time.
	_, tt = openImport(t, a, p)
	if a.imp.source.Kind != listSource || !tt.HasText(locales.ImportPresent("app-1")) {
		t.Errorf("opened on %s; texts %q", a.imp.source.Kind, tt.Texts())
	}
	a.closeWindow(WindowImport)
}
