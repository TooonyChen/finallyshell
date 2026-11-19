package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// settled waits for the file panel to be done: no dialog at work, the
// folder read.
func (f *editorFixture) settled(t *testing.T) {
	t.Helper()
	f.p.until(t, "the file panel", func() bool {
		d := f.a.fileDlg
		return (d == nil || !d.busy) && !f.s.files.loading && len(f.s.files.treeBusy) == 0
	})
}

func (f *editorFixture) exists(name string) bool {
	_, err := os.Lstat(filepath.Join(f.dir, name))
	return err == nil
}

// menu right-clicks the row of name and chooses item from its menu.
func (f *editorFixture) menu(t *testing.T, name, item string) {
	t.Helper()
	f.tt.RightClickAt(rowCenter(t, f.tt, name))
	if err := f.tt.ChooseMenuItem(item); err != nil {
		t.Fatalf("%s: %v; menu %q", name, err, f.tt.Menu())
	}
	f.tt.Frame()
}

// treeMenu scrolls the folder tree to the folder p, right-clicks it and
// chooses item from its menu.
func (f *editorFixture) treeMenu(t *testing.T, p, item string) {
	t.Helper()
	f.showInTree(t, p)
	f.tt.RightClickAt(treeRowCenter(t, f.tt, p))
	if err := f.tt.ChooseMenuItem(item); err != nil {
		t.Fatalf("%s: %v; menu %q", p, err, f.tt.Menu())
	}
	f.tt.Frame()
}

func treeRowCenter(t *testing.T, tt *ui.Tester, dir string) (x, y float32) {
	t.Helper()
	return rowCenter(t, tt, locales.KindFolder()+" "+dir)
}

func (f *editorFixture) showInTree(t *testing.T, p string) {
	t.Helper()
	tree := &f.s.files.tree
	for i := range tree.Rows() {
		if tree.Item(i) == p {
			tree.List.ScrollIntoView(i)
			f.tt.Frame()
			f.tt.Frame()
			return
		}
	}
	t.Fatalf("%s is not in the tree", p)
}

// The table's menu, F2 and Delete rename, create and delete files, and
// the table chooses what was renamed or made.
func TestFileOpsInTable(t *testing.T) {
	f := newEditorFixture(t)
	a, tt, s := f.a, f.tt, f.s

	f.menu(t, "app.yaml", locales.Rename())
	d := a.fileDlg
	if d == nil || d.op != opRename || d.name != "app.yaml" || !tt.HasText(locales.RenameFileTitle("app.yaml")) {
		t.Fatalf("dialog %+v; texts %q", d, tt.Texts())
	}
	saveShot(t, tt, "file-rename-en.png")
	// The name without its extension is chosen: typing replaces it.
	tt.Type("config")
	tt.Key(0, ui.KeyEnter)
	f.settled(t)
	if a.fileDlg != nil || f.exists("app.yaml") || !f.exists("config.yaml") {
		t.Fatalf("dialog %+v; app.yaml %v, config.yaml %v", a.fileDlg, f.exists("app.yaml"), f.exists("config.yaml"))
	}
	if got := s.files.chosenNames(); !slices.Equal(got, []string{"config.yaml"}) {
		t.Errorf("chosen %q", got)
	}
	if got := f.read(t, "config.yaml"); got != appYAML {
		t.Errorf("config.yaml holds %q", got)
	}

	// A taken name: the dialog says so and stays, and nothing changes.
	f.menu(t, "config.yaml", locales.Rename())
	a.fileDlg.name = "deploy.sh"
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	f.settled(t)
	if a.fileDlg == nil || !tt.HasText(locales.FileNameTaken("deploy.sh")) {
		t.Fatalf("no error; texts %q", tt.Texts())
	}
	if f.read(t, "deploy.sh") != deployScript || f.read(t, "config.yaml") != appYAML {
		t.Error("a rename onto a taken name changed the files")
	}
	saveShot(t, tt, "file-rename-taken-en.png")
	// A name with a slash is refused before it goes to the server.
	a.fileDlg.name = "a/b"
	tt.Frame()
	if err := tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(locales.BadFileName()) {
		t.Errorf("no error for a/b; texts %q", tt.Texts())
	}
	tt.Click(locales.Cancel())
	tt.Frame()
	if a.fileDlg != nil {
		t.Fatal("Cancel left the dialog")
	}

	// New folder and new file, chosen once made.
	for _, c := range []struct{ item, name string }{{locales.NewFolder(), "logs"}, {locales.NewFile(), "notes.md"}} {
		f.menu(t, "deploy.sh", c.item)
		if a.fileDlg == nil || a.fileDlg.name != "" {
			t.Fatalf("%s: dialog %+v", c.item, a.fileDlg)
		}
		tt.Type(c.name)
		tt.Key(0, ui.KeyEnter)
		f.settled(t)
		if a.fileDlg != nil || !f.exists(c.name) {
			t.Fatalf("%s: %s not made; dialog %+v", c.item, c.name, a.fileDlg)
		}
		if got := s.files.chosenNames(); !slices.Equal(got, []string{c.name}) {
			t.Errorf("%s: chosen %q", c.item, got)
		}
	}
	if fi, err := os.Stat(filepath.Join(f.dir, "logs")); err != nil || !fi.IsDir() {
		t.Errorf("logs: %v", err)
	}

	// F2 renames the row chosen.
	tt.ClickAt(rowCenter(t, tt, "legacy.ini"))
	tt.Key(0, ui.KeyF2)
	tt.Frame()
	if a.fileDlg == nil || a.fileDlg.op != opRename || a.fileDlg.old != "legacy.ini" {
		t.Fatalf("F2: dialog %+v", a.fileDlg)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.fileDlg != nil {
		t.Fatal("Escape left the dialog")
	}

	// Delete asks, for a folder with what it holds, and deletes.
	os.WriteFile(filepath.Join(f.dir, "logs", "app.log"), []byte("x"), 0o644)
	tt.ClickAt(rowCenter(t, tt, "logs"))
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	if a.fileDlg == nil || a.fileDlg.op != opDelete || !tt.HasText(locales.DeleteFileTitle("logs")) || !tt.HasText(locales.DeleteFoldersBody(1)) {
		t.Fatalf("Delete: dialog %+v; texts %q", a.fileDlg, tt.Texts())
	}
	saveShot(t, tt, "file-delete-en.png")
	if err := tt.Click(locales.Delete()); err != nil {
		t.Fatal(err)
	}
	f.settled(t)
	if a.fileDlg != nil || f.exists("logs") {
		t.Fatalf("logs not deleted; dialog %+v", a.fileDlg)
	}
	if slices.ContainsFunc(s.files.entries, func(e remotefs.Entry) bool { return e.Name == "logs" }) {
		t.Error("the table still lists logs")
	}

	// Several at once, from the menu.
	s.files.chosen.Clear()
	s.files.chosen.Add("notes.md")
	s.files.chosen.Add("big.log")
	f.menu(t, "notes.md", locales.Delete())
	if !tt.HasText(locales.DeleteFilesTitle(2)) || !tt.HasText(locales.DeleteFilesBody(2)) {
		t.Fatalf("texts %q", tt.Texts())
	}
	tt.Click(locales.Delete())
	f.settled(t)
	if f.exists("notes.md") || f.exists("big.log") || !f.exists("deploy.sh") {
		t.Errorf("notes.md %v, big.log %v, deploy.sh %v", f.exists("notes.md"), f.exists("big.log"), f.exists("deploy.sh"))
	}

	// A delete that fails half way asks again for what is left: here
	// ghost isn't there, so the first try removes legacy.ini and fails.
	a.askDelete(s, f.dir, []string{"legacy.ini", "ghost"}, false)
	tt.Frame()
	tt.Click(locales.Delete())
	f.settled(t)
	if a.fileDlg == nil || a.fileDlg.err == nil || f.exists("legacy.ini") {
		t.Fatalf("dialog %+v; legacy.ini %v", a.fileDlg, f.exists("legacy.ini"))
	}
	if !slices.Equal(a.fileDlg.names, []string{"ghost"}) || !tt.HasText(locales.DeleteFileTitle("ghost")) {
		t.Fatalf("left %q; texts %q", a.fileDlg.names, tt.Texts())
	}
	os.WriteFile(filepath.Join(f.dir, "ghost"), nil, 0o644)
	tt.Click(locales.Delete())
	f.settled(t)
	if a.fileDlg != nil || f.exists("ghost") {
		t.Fatalf("the second try: dialog %+v; ghost %v", a.fileDlg, f.exists("ghost"))
	}

	// The dialogs in Chinese.
	f.p.SetLocale("zh-Hans")
	f.menu(t, "deploy.sh", locales.Rename())
	if !tt.HasText("重命名“deploy.sh”") {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "file-rename-zh.png")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.ClickAt(rowCenter(t, tt, "deploy.sh"))
	tt.Key(0, ui.KeyDelete)
	tt.Frame()
	if !tt.HasText("删除“deploy.sh”？") || !tt.HasText("将从服务器上删除，无法撤销。") {
		t.Errorf("Chinese: texts %q", tt.Texts())
	}
	saveShot(t, tt, "file-delete-zh.png")
}

// The folder tree's menu acts on the folder right-clicked, not on the
// current one, and the panel follows a current folder renamed or deleted.
func TestFileOpsInTree(t *testing.T) {
	f := newEditorFixture(t)
	a, tt, s := f.a, f.tt, f.s
	for _, d := range []string{"srv/www", "docs"} {
		os.MkdirAll(filepath.Join(f.dir, d), 0o755)
	}
	// In docs, srv shows in the tree alone, not in the table.
	a.openDir(s, f.dir)
	f.settled(t)
	a.openDir(s, filepath.Join(f.dir, "docs"))
	f.settled(t)
	docs := s.files.cwd

	f.treeMenu(t, filepath.Join(f.dir, "srv"), locales.NewFolder())
	if a.fileDlg == nil || a.fileDlg.dir != filepath.Join(f.dir, "srv") {
		t.Fatalf("dialog %+v", a.fileDlg)
	}
	tt.Type("cache")
	tt.Key(0, ui.KeyEnter)
	f.settled(t)
	if !f.exists("srv/cache") || s.files.cwd != docs {
		t.Fatalf("srv/cache %v; cwd %s", f.exists("srv/cache"), s.files.cwd)
	}

	// Renaming a folder that isn't current leaves the current one.
	f.treeMenu(t, filepath.Join(f.dir, "srv"), locales.Rename())
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("service")
	tt.Key(0, ui.KeyEnter)
	f.settled(t)
	if !f.exists("service/cache") || f.exists("srv") || s.files.cwd != docs {
		t.Fatalf("service/cache %v, srv %v; cwd %s", f.exists("service/cache"), f.exists("srv"), s.files.cwd)
	}
	if !tt.HasText("service") || tt.HasText("srv") {
		t.Errorf("the tree: texts %q", tt.Texts())
	}

	// Renaming a folder the current one is in follows it.
	a.openDir(s, filepath.Join(f.dir, "service", "www"))
	f.settled(t)
	f.treeMenu(t, filepath.Join(f.dir, "service"), locales.Rename())
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("svc")
	tt.Key(0, ui.KeyEnter)
	f.settled(t)
	if want := filepath.Join(f.dir, "svc", "www"); s.files.cwd != want || s.files.err != nil {
		t.Fatalf("cwd %s, want %s; err %v", s.files.cwd, want, s.files.err)
	}
	if !s.files.tree.Open.Has(filepath.Join(f.dir, "svc")) || s.files.tree.Open.Has(filepath.Join(f.dir, "service")) {
		t.Error("the tree's open folders didn't follow the rename")
	}

	// Deleting it goes to the folder it was in.
	f.treeMenu(t, filepath.Join(f.dir, "svc"), locales.Delete())
	if !tt.HasText(locales.DeleteFileTitle("svc")) {
		t.Fatalf("texts %q", tt.Texts())
	}
	saveShot(t, tt, "tree-delete-en.png")
	tt.Click(locales.Delete())
	f.settled(t)
	if f.exists("svc") || s.files.cwd != f.dir || s.files.err != nil {
		t.Fatalf("svc %v; cwd %s; err %v", f.exists("svc"), s.files.cwd, s.files.err)
	}

	// The root has no Rename or Delete.
	f.showInTree(t, "/")
	tt.RightClickAt(treeRowCenter(t, tt, "/"))
	if menu := tt.Menu(); slices.Contains(menu, locales.Rename()) || slices.Contains(menu, locales.Delete()) || !slices.Contains(menu, locales.NewFolder()) {
		t.Errorf("menu of /: %q", menu)
	}
	tt.CloseMenu()
}
