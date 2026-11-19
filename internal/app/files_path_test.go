package app

import (
	"os"
	"path"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func TestFilePathBar(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "files", "127.0.0.1")
	s.sftp = localSFTP(t)
	home := t.TempDir()
	parent := path.Join(home, "project files")
	child := path.Join(parent, "project files")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path.Join(child, "readme.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	view := func(c *ui.Context) { a.filePathBar(c, s) }
	tt := ui.NewTester(view, 1000, 70)
	p.tt = tt
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	waitDir := func(want string) {
		p.until(t, "the directory listing", func() bool { return s.files.cwd == want && !s.files.loading })
	}
	clickPencil := func() {
		t.Helper()
		b, ok := tt.AccessByRole(ui.RoleToggleButton, locales.EditRemotePath())
		if !ok {
			t.Fatal("path editor button is missing")
		}
		tt.ClickAt(b.Bounds.X+b.Bounds.W/2, b.Bounds.Y+b.Bounds.H/2)
	}
	a.openDir(s, child)
	waitDir(child)
	checkInput := func(want string) {
		t.Helper()
		in, ok := tt.AccessByRole(ui.RoleTextField, locales.EditRemotePath())
		if !ok || in.Value != want {
			t.Fatalf("path input = %q, present %v; want %q", in.Value, ok, want)
		}
		if _, ok := tt.AccessByRole(ui.RoleToggleButton, locales.EditRemotePath()); ok {
			t.Fatal("path input mode shows the pencil")
		}
	}
	checkInput(child)
	click(locales.EditRemotePath())
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("..")
	tt.Key(0, ui.KeyEnter)
	waitDir(parent)
	checkInput(parent)

	a.openOptions()
	options := p.show(t, WindowOptions)
	choose := func(label string) {
		t.Helper()
		if err := options.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	choose(locales.OptionsFileManager())
	if radio, ok := options.AccessByRole(ui.RoleRadio, locales.FilePathBarInput()); !ok || !radio.Checked {
		t.Fatal("path input is not selected by default")
	}
	choose(locales.FilePathBarBreadcrumbs())
	if !a.settings.FilePathBreadcrumbs || !config.LoadSettings().FilePathBreadcrumbs {
		t.Fatal("breadcrumb preference was not saved")
	}
	a.openDir(s, child)
	waitDir(child)
	if !tt.HasText("project files") || len(s.files.entries) != 1 || s.files.entries[0].Name != "readme.txt" {
		t.Fatalf("child was not shown: %q, entries %v", tt.Texts(), s.files.entries)
	}
	click(parent)
	waitDir(parent)

	edit := func(dir string) {
		t.Helper()
		clickPencil()
		if !s.files.pathEditing {
			t.Fatal("path editor did not open")
		}
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(dir)
		if s.files.pathEdit != dir {
			t.Fatalf("path editor has %q, want %q", s.files.pathEdit, dir)
		}
	}
	edit("project files")
	tt.Key(0, ui.KeyEnter)
	waitDir(child)
	edit("/missing")
	tt.Key(0, ui.KeyEscape)
	if s.files.pathEditing || s.files.cwd != child {
		t.Fatalf("Escape left editing=%v, directory=%q", s.files.pathEditing, s.files.cwd)
	}
	edit("/missing")
	clickPencil()
	if s.files.pathEditing || s.files.pathEdit != child {
		t.Fatalf("second pencil click left editing=%v, draft=%q", s.files.pathEditing, s.files.pathEdit)
	}
	edit(path.Join(child, "missing"))
	tt.Key(0, ui.KeyEnter)
	p.until(t, "the missing-directory error", func() bool { return s.files.err != nil && !s.files.loading })
	if s.files.cwd != child || s.files.pathEditing {
		t.Fatalf("invalid path left editing=%v, directory=%q", s.files.pathEditing, s.files.cwd)
	}

	tt.SetSize(220, 70)
	click(locales.MoreParentFolders())
	if err := tt.ChooseMenuItem(home); err != nil {
		t.Fatalf("hidden ancestor: %v, menu %q", err, tt.Menu())
	}
	waitDir(home)

	edit("/unfinished")
	choose(locales.FilePathBarInput())
	checkInput(home)
	if s.files.pathEditing || a.settings.FilePathBreadcrumbs || config.LoadSettings().FilePathBreadcrumbs {
		t.Fatal("switching to path input did not clear the draft and save the choice")
	}
}
