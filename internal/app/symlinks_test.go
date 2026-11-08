package app

import (
	"io/fs"
	"testing"
	"time"

	"finallyshell/internal/remotefs"

	"github.com/egoist/mygo/ui"
)

// linksSession is a connected session listing / with links in it, as a
// usrmerge system has.
func linksSession(a *App) *Session {
	s := demoSession(a, "web", "10.0.0.7")
	f := &s.files
	f.cwd, f.pathEdit = "/", "/"
	mod := time.Date(2026, 10, 2, 9, 41, 0, 0, time.Local)
	dir := func(name string) remotefs.Entry {
		return remotefs.Entry{Name: name, Mode: fs.ModeDir | 0o755, ModTime: mod}
	}
	link := func(name, target string, mode fs.FileMode, size int64) remotefs.Entry {
		return remotefs.Entry{Name: name, Size: size, Mode: mode, ModTime: mod, Link: true, Target: target}
	}
	broken := link("initrd.img.old", "boot/initrd.img-6.5.0-14-generic", fs.ModeSymlink|0o777, 32)
	broken.Broken = true
	f.entries = []remotefs.Entry{
		link("bin", "usr/bin", fs.ModeDir|0o755, 0), dir("boot"), dir("etc"), dir("home"),
		link("lib", "usr/lib", fs.ModeDir|0o755, 0), dir("usr"),
		link("initrd.img", "boot/initrd.img-6.8.0-45-generic", fs.ModeSymlink|0o777, 71_303_168),
		broken,
		link("vmlinuz", "/boot/vmlinuz-6.8.0-45-generic", fs.ModeSymlink|0o777, 14_928_264),
		{Name: "swap.img", Size: 2_147_483_648, Mode: 0o600, ModTime: mod},
	}
	f.subdirs = map[string][]string{"/": {"/bin", "/boot", "/etc", "/home", "/lib", "/usr"}}
	a.active = len(a.sessions) - 1
	return s
}

// The file panel marks links: an arrow on the icon, the target after the
// name, the target's size, and a broken link in red that a double click
// doesn't open.
func TestSymlinksInFilePanel(t *testing.T) {
	a, p := testApp(t)
	linksSession(a)
	tt := ui.NewTester(a.View, 1280, 1000)
	tt.SetScale(2)
	p.tt = tt
	tt.Frame()
	waitIconTheme(t, a, p, tt)

	for _, want := range []string{"bin", "→ usr/bin", "→ boot/initrd.img-6.8.0-45-generic", "→ /boot/vmlinuz-6.8.0-45-generic", "14.2 MB", "Broken link", "Link", "Folder"} {
		if !tt.HasText(want) {
			t.Errorf("English: missing %q; texts %q", want, tt.Texts())
		}
	}
	if tt.HasText("32 B") {
		t.Error("a broken link shows the size of the link itself")
	}
	saveShot(t, tt, "symlinks-en.png")

	r, ok := tt.Find("initrd.img.old")
	if !ok {
		t.Fatal("no broken link row")
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if len(a.fe.tabs) != 0 {
		t.Errorf("a double click opened the broken link: %d tabs", len(a.fe.tabs))
	}
	for _, e := range a.current().files.entries {
		if want := e.Name == "initrd.img" || e.Name == "vmlinuz" || e.Name == "swap.img"; editable(e) != want {
			t.Errorf("%s: editable %v", e.Name, !want)
		}
	}

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"失效的链接", "链接", "文件夹", "→ usr/lib"} {
		if !tt.HasText(want) {
			t.Errorf("Chinese: missing %q; texts %q", want, tt.Texts())
		}
	}
	saveShot(t, tt, "symlinks-zh.png")
}
