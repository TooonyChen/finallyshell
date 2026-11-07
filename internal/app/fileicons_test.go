package app

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/fileicons"
	"finallyshell/internal/remotefs"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fakeVSIXPicker stands in for the dialog that picks a VSIX.
type fakeVSIXPicker struct {
	vsixMu    sync.Mutex
	vsix      string
	vsixAsked []filePick
}

func (p *fakeVSIXPicker) OpenVSIX(parent WindowKind, title string) (string, error) {
	p.vsixMu.Lock()
	defer p.vsixMu.Unlock()
	p.vsixAsked = append(p.vsixAsked, filePick{parent: parent, title: title})
	return p.vsix, nil
}

// pickVSIX is what the next VSIX dialogs return: "" for canceled.
func (p *fakeVSIXPicker) pickVSIX(path string) {
	p.vsixMu.Lock()
	p.vsix = path
	p.vsixMu.Unlock()
}

func (p *fakeVSIXPicker) vsixPicks() []filePick {
	p.vsixMu.Lock()
	defer p.vsixMu.Unlock()
	return slices.Clone(p.vsixAsked)
}

// testIconSVG is the one icon of the test theme.
const testIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7" fill="#e11d48"/></svg>`

// testExtension is the files of an extension, publisher "pub", with a
// theme "t" labeled label that knows Go files and nothing else.
func testExtension(label, version string) map[string]string {
	return map[string]string{
		"package.json": `{"publisher": "pub", "name": "icons", "version": "` + version + `",
			"contributes": {"iconThemes": [{"id": "t", "label": "` + label + `", "path": "./t.json"}]}}`,
		"t.json": `{"iconDefinitions": {"go": {"iconPath": "./go.svg"}}, "fileExtensions": {"go": "go"}}`,
		"go.svg": testIconSVG,
	}
}

// writeVSIX writes the extension files as a VSIX at path.
func writeVSIX(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, _ := zw.Create("extension/" + name)
		w.Write([]byte(data))
	}
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// iconsSession is a connected session listing a project's files.
func iconsSession(a *App) *Session {
	s := demoSession(a, "web", "10.0.0.7")
	f := &s.files
	f.cwd, f.pathEdit = "/srv/app", "/srv/app"
	mod := time.Date(2026, 10, 2, 9, 41, 0, 0, time.Local)
	dir := func(name string) remotefs.Entry {
		return remotefs.Entry{Name: name, Mode: fs.ModeDir | 0o755, ModTime: mod}
	}
	file := func(name string, size int64) remotefs.Entry {
		return remotefs.Entry{Name: name, Size: size, Mode: 0o644, ModTime: mod}
	}
	f.entries = []remotefs.Entry{
		dir(".github"), dir("docs"), dir("node_modules"), dir("src"),
		file(".gitignore", 120), file("docker-compose.yml", 1_870), file("Dockerfile", 640),
		file("go.mod", 312), file("main.go", 4_210), file("Makefile", 980), file("nginx.conf", 2_048),
		file("package.json", 1_450), file("README.md", 6_300), file("deploy.sh", 760), file("logo.png", 48_000),
	}
	f.subdirs = map[string][]string{
		"/":        {"/etc", "/home", "/srv", "/usr"},
		"/srv":     {"/srv/app"},
		"/srv/app": {"/srv/app/.github", "/srv/app/docs", "/srv/app/node_modules", "/srv/app/src"},
	}
	f.tree.Open.Add("/srv")
	f.tree.Open.Add("/srv/app")
	a.active = len(a.sessions) - 1
	return s
}

// waitIconTheme draws until the theme the settings name is read.
func waitIconTheme(t *testing.T, a *App, p *fakePlatform, tt *ui.Tester) {
	t.Helper()
	p.until(t, "the icon theme", func() bool {
		tt.Frame()
		st := &a.iconThemes
		return st.loaded && st.key == a.settings.FileIconTheme
	})
	tt.Frame()
}

// The file panel shows each file's icon from Material by default, from
// Seti or none (Minimal) as the settings say, changing as they change.
func TestFileIconsInFilePanel(t *testing.T) {
	a, p := testApp(t)
	iconsSession(a)
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame()
	waitIconTheme(t, a, p, tt)

	th := a.iconThemes.theme
	if th == nil || a.iconThemes.fallback != nil {
		t.Fatalf("no Material: %v", a.iconThemes.fallback)
	}
	for name, want := range map[string]string{"docker-compose.yml": "docker", "main.go": "go", "package.json": "nodejs", "Dockerfile": "docker"} {
		if ic := th.File(name); ic == nil || ic.ID != want {
			t.Errorf("%s: %+v", name, ic)
		}
	}
	if ic := th.Folder("src", false, false); ic == nil || ic.ID != "folder-src" {
		t.Errorf("src: %+v", ic)
	}
	saveShot(t, tt, "file-icons-material-en.png")

	a.changeSettings(func() { a.settings.FileIconTheme = fileicons.KeySeti })
	waitIconTheme(t, a, p, tt)
	if ic := a.iconThemes.theme.File("main.go"); ic == nil || ic.Font == "" {
		t.Errorf("Seti main.go: %+v", ic)
	}
	saveShot(t, tt, "file-icons-seti-en.png")

	a.changeSettings(func() { a.settings.FileIconTheme = fileicons.KeyMinimal })
	tt.Frame()
	if a.iconThemes.theme != nil || config.LoadSettings().FileIconTheme != fileicons.KeyMinimal {
		t.Errorf("minimal: %v, saved %q", a.iconThemes.theme, config.LoadSettings().FileIconTheme)
	}
	saveShot(t, tt, "file-icons-minimal-en.png")

	p.SetLocale("zh-Hans")
	a.changeSettings(func() { a.settings.FileIconTheme = fileicons.KeyMaterial })
	waitIconTheme(t, a, p, tt)
	saveShot(t, tt, "file-icons-material-zh.png")
}

// The File icon theme page lists the bundled themes and switches between
// them, in both languages.
func TestFileIconsPage(t *testing.T) {
	a, p := testApp(t)
	a.optionsNav.Page = pageFileIcons
	a.openOptions()
	tt := p.show(t, WindowOptions)
	waitIconTheme(t, a, p, tt)
	p.until(t, "the list", func() bool { tt.Frame(); return a.iconThemes.scanned })
	for _, s := range []string{
		locales.OptionsFileManager(), locales.OptionsFileIcons(), locales.FileIconsPreview(),
		"Material Icon Theme", "Seti (Visual Studio Code)", locales.FileIconsMinimal(),
		locales.InstallVSIX(), "docker-compose.yml",
	} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-file-icons-en.png")

	if err := tt.Click("Seti (Visual Studio Code)"); err != nil {
		t.Fatal(err)
	}
	if a.settings.FileIconTheme != fileicons.KeySeti || config.LoadSettings().FileIconTheme != fileicons.KeySeti {
		t.Fatalf("seti: %q", a.settings.FileIconTheme)
	}
	waitIconTheme(t, a, p, tt)
	saveShot(t, tt, "options-file-icons-seti-en.png")

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"文件管理", "文件图标主题", "预览", "内置", "简洁", "从 VSIX 安装…"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	if err := tt.Click("简洁"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.FileIconTheme != fileicons.KeyMinimal {
		t.Fatalf("minimal: %q", a.settings.FileIconTheme)
	}
	saveShot(t, tt, "options-file-icons-minimal-zh.png")
	if err := tt.Click("Material Icon Theme"); err != nil {
		t.Fatal(err)
	}
	waitIconTheme(t, a, p, tt)
	saveShot(t, tt, "options-file-icons-zh.png")
}

// installVSIX picks path in the VSIX dialog and installs it from the page.
func installVSIX(t *testing.T, a *App, p *fakePlatform, tt *ui.Tester, path string) {
	t.Helper()
	p.pickVSIX(path)
	if err := tt.Click(locales.InstallVSIX()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the install", func() bool { tt.Frame(); return !a.iconThemes.installing && !a.iconThemes.scanning })
	tt.Frame()
}

// A theme installed from a VSIX is copied into the settings folder,
// chosen, listed and shown; a file that is no theme says why; removing
// the theme deletes its copy and shows the default.
func TestInstallIconTheme(t *testing.T) {
	a, p := testApp(t)
	src := t.TempDir()
	a.optionsNav.Page = pageFileIcons
	a.openOptions()
	tt := p.show(t, WindowOptions)
	waitIconTheme(t, a, p, tt)

	// Canceled, nothing changes.
	installVSIX(t, a, p, tt, "")
	if a.settings.FileIconTheme != "" || a.iconThemes.installErr != nil {
		t.Fatalf("canceled: %q %v", a.settings.FileIconTheme, a.iconThemes.installErr)
	}
	if picks := p.vsixPicks(); len(picks) != 1 || picks[0] != (filePick{parent: WindowOptions, title: locales.InstallVSIXTitle()}) {
		t.Errorf("asked %+v", picks)
	}

	installVSIX(t, a, p, tt, writeVSIX(t, filepath.Join(src, "icons.vsix"), testExtension("Test Icons", "1.0.0")))
	if a.settings.FileIconTheme != "pub.icons/t" || config.LoadSettings().FileIconTheme != "pub.icons/t" {
		t.Fatalf("chosen %q", a.settings.FileIconTheme)
	}
	kept := fileicons.InstalledPath(config.IconThemesDir(), "pub.icons")
	if _, err := os.Stat(kept); err != nil {
		t.Fatal(err)
	}
	waitIconTheme(t, a, p, tt)
	if ic := a.iconThemes.theme.File("main.go"); ic == nil || ic.ID != "go" {
		t.Errorf("main.go: %+v", ic)
	}
	for _, s := range []string{locales.FileIconsInstalled(), "Test Icons", "1.0.0"} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-file-icons-installed-en.png")

	// A newer version replaces it.
	installVSIX(t, a, p, tt, writeVSIX(t, filepath.Join(src, "icons-2.vsix"), testExtension("Test Icons", "2.0.0")))
	waitIconTheme(t, a, p, tt)
	if left, _ := filepath.Glob(filepath.Join(config.IconThemesDir(), "*")); len(left) != 1 || !tt.HasText("2.0.0") {
		t.Errorf("after the update: %v; texts %q", left, tt.Texts())
	}

	// Not a VSIX, or no icon theme: the error, and nothing else changes.
	notes := filepath.Join(src, "notes.vsix")
	os.WriteFile(notes, []byte("not a zip"), 0o644)
	installVSIX(t, a, p, tt, notes)
	if !tt.HasText(locales.VsixNotPackage()) || a.settings.FileIconTheme != "pub.icons/t" {
		t.Errorf("not a VSIX: %q; texts %q", a.settings.FileIconTheme, tt.Texts())
	}
	installVSIX(t, a, p, tt, writeVSIX(t, filepath.Join(src, "colors.vsix"), map[string]string{
		"package.json": `{"publisher": "pub", "name": "colors", "contributes": {"themes": []}}`,
	}))
	if !tt.HasText(locales.VsixNoIconTheme()) || tt.HasText(locales.VsixNotPackage()) {
		t.Errorf("no icon theme; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-file-icons-error-en.png")

	// Removing asks, then deletes the copy, and Material shows.
	p.answer = true
	if err := tt.Click(locales.RemoveNamedIconTheme("Test Icons")); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the removal", func() bool {
		tt.Frame()
		return a.settings.FileIconTheme == fileicons.KeyMaterial && !a.iconThemes.scanning
	})
	if qs := p.questions(); len(qs) != 1 || qs[0].Message != locales.RemoveIconThemeTitle("Test Icons") {
		t.Errorf("asked %+v", qs)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("copy left: %v", err)
	}
	waitIconTheme(t, a, p, tt)
	if tt.HasText("Test Icons") || a.iconThemes.fallback != nil {
		t.Errorf("still listed; texts %q", tt.Texts())
	}
}

// The themes of the extensions installed in VS Code are listed and can
// be chosen; once the extension is gone, Material shows, and the page
// says why.
func TestDetectedIconTheme(t *testing.T) {
	a, p := testApp(t)
	home := t.TempDir()
	a.home = func() string { return home }
	ext := filepath.Join(home, ".vscode", "extensions", "pub.icons-1.4.0")
	for name, data := range testExtension("Editor Icons", "1.4.0") {
		os.MkdirAll(ext, 0o755)
		os.WriteFile(filepath.Join(ext, name), []byte(data), 0o644)
	}
	a.optionsNav.Page = pageFileIcons
	a.openOptions()
	tt := p.show(t, WindowOptions)
	p.until(t, "the list", func() bool { tt.Frame(); return a.iconThemes.scanned })
	tt.Frame()
	for _, s := range []string{locales.FileIconsDetected(), "Editor Icons", "VS Code · 1.4.0"} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	if err := tt.Click("Editor Icons"); err != nil {
		t.Fatal(err)
	}
	waitIconTheme(t, a, p, tt)
	if a.settings.FileIconTheme != "pub.icons/t" || a.iconThemes.fallback != nil {
		t.Fatalf("chosen %q, %v", a.settings.FileIconTheme, a.iconThemes.fallback)
	}
	if ic := a.iconThemes.theme.File("main.go"); ic == nil || ic.ID != "go" {
		t.Errorf("main.go: %+v", ic)
	}
	saveShot(t, tt, "options-file-icons-detected-en.png")

	// Uninstalled from VS Code: a new run shows Material, and says so.
	os.RemoveAll(ext)
	b := New(p)
	b.home = a.home
	b.optionsNav.Page = pageFileIcons
	p.CloseWindow(WindowOptions)
	b.openOptions()
	tt = p.show(t, WindowOptions)
	waitIconTheme(t, b, p, tt)
	if b.iconThemes.fallback != errIconThemeMissing || b.iconThemes.theme == nil || b.settings.FileIconTheme != "pub.icons/t" {
		t.Fatalf("fallback %v, theme %v, setting %q", b.iconThemes.fallback, b.iconThemes.theme, b.settings.FileIconTheme)
	}
	if ic := b.iconThemes.theme.File("main.go"); ic == nil || ic.ID != "go" {
		t.Errorf("Material main.go: %+v", ic)
	}
	if !tt.HasText(locales.IconThemeMissing()) {
		t.Errorf("no notice; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-file-icons-missing-en.png")
}
