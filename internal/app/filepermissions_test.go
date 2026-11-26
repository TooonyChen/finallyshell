package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func TestFilePermissionsInTable(t *testing.T) {
	f := newEditorFixture(t)
	tt := f.tt
	p := filepath.Join(f.dir, "app.yaml")
	const special = fs.ModeSetuid
	if err := os.Chmod(p, special|0o640); err != nil {
		t.Fatal(err)
	}
	checkMode := func(want fs.FileMode) {
		t.Helper()
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode(); got != want {
			t.Fatalf("mode %v, want %v", got, want)
		}
	}
	open := func() {
		t.Helper()
		f.menu(t, "app.yaml", locales.ChangePermissions())
		f.settled(t)
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}

	open()
	if d := f.a.fileDlg; d == nil || !d.perms.loaded || d.perms.mode != 0o640 {
		t.Fatalf("expected permissions 0640, dialog %+v", d)
	}
	if ok, found := tt.AccessByRole(ui.RoleButton, locales.Ok()); !found || !ok.Disabled {
		t.Fatalf("OK should be disabled with no changes: %+v", ok)
	}
	click(locales.PermissionExecute())
	saveShot(t, tt, "file-permissions-en.png")
	click(locales.Cancel())
	checkMode(special | 0o640)

	open()
	click(locales.PermissionRead())
	click(locales.PermissionWrite())
	// The action labels repeat in each row. Click Read in the Group row.
	read, found := tt.AccessByRole(ui.RoleCheckBox, locales.PermissionRead())
	group, grouped := tt.AccessByRole(ui.RoleGroup, locales.PermissionGroup())
	if !found || !grouped {
		t.Fatal("cannot locate the group read checkbox")
	}
	tt.ClickAt(read.Bounds.X+read.Bounds.W/2, group.Bounds.Y+group.Bounds.H/2)
	if got := f.a.fileDlg.perms.mode; got != 0 {
		t.Fatalf("unchecked mode %03o, want 000", got)
	}
	// Fail after reading the original permissions, then retry with the
	// same choices when the file is available again.
	if err := os.Rename(p, p+".moved"); err != nil {
		t.Fatal(err)
	}
	click(locales.Ok())
	f.settled(t)
	if d := f.a.fileDlg; d == nil || d.err == nil || d.perms.mode != 0 {
		t.Fatalf("expected a save error with the edited permissions, dialog %+v", d)
	}
	if err := os.Rename(p+".moved", p); err != nil {
		t.Fatal(err)
	}
	click(locales.Ok())
	f.settled(t)
	if f.a.fileDlg != nil {
		t.Fatalf("save left the dialog open: %+v", f.a.fileDlg)
	}
	checkMode(special)
	e, found := f.s.files.entry([]string{"app.yaml"})
	if !found || e.Mode.Perm() != 0 || !tt.HasText("----------") {
		t.Fatalf("table did not refresh the permissions: %+v; texts %q", e, tt.Texts())
	}
}

func TestFilePermissionsInTree(t *testing.T) {
	f := newEditorFixture(t)
	dir := filepath.Join(f.dir, "scripts")
	other := filepath.Join(f.dir, "work")
	link := filepath.Join(f.dir, "alias")
	item := filepath.Join(dir, "config")
	for _, p := range []string{dir, other} {
		if err := os.Mkdir(p, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(item, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.a.openDir(f.s, f.dir)
	f.settled(t)

	f.tt.RightClickAt(rowCenter(t, f.tt, "alias"))
	if err := f.tt.ChooseMenuItem(locales.ChangePermissions()); err == nil {
		t.Fatal("permissions menu was enabled for a symbolic link")
	}
	f.tt.Key(0, ui.KeyEscape)
	f.a.openDir(f.s, other)
	f.settled(t)

	f.p.SetLocale("zh-Hans")
	f.treeMenu(t, link, locales.ChangePermissions())
	f.settled(t)
	if !f.tt.HasText(locales.SymlinkPermissions()) {
		t.Fatalf("missing symbolic link message: %q", f.tt.Texts())
	}
	f.tt.Key(0, ui.KeyEscape)

	f.treeMenu(t, dir, locales.ChangePermissions())
	f.settled(t)
	if !f.tt.HasText("修改文件权限") || !f.tt.HasText("所有者") {
		t.Fatalf("missing Chinese permissions dialog: %q", f.tt.Texts())
	}
	if err := f.tt.Click(locales.PermissionWrite()); err != nil {
		t.Fatal(err)
	}
	saveShot(t, f.tt, "file-permissions-zh.png")
	if err := f.tt.Click(locales.Ok()); err != nil {
		t.Fatal(err)
	}
	f.settled(t)
	if f.a.fileDlg != nil || f.s.files.cwd != other {
		t.Fatalf("dialog %+v; cwd %q, want %q", f.a.fileDlg, f.s.files.cwd, other)
	}
	for p, want := range map[string]fs.FileMode{dir: 0o550, item: 0o600} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s: mode %03o, want %03o", p, got, want)
		}
	}
}
