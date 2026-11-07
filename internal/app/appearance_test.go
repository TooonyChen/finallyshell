package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// fakeImagePicker stands in for the dialog that picks an image, as
// fakeFilePicker does for other files.
type fakeImagePicker struct {
	imageMu    sync.Mutex
	image      string
	imageAsked []filePick
}

func (p *fakeImagePicker) OpenImage(parent WindowKind, title string) (string, error) {
	p.imageMu.Lock()
	defer p.imageMu.Unlock()
	p.imageAsked = append(p.imageAsked, filePick{parent: parent, title: title})
	return p.image, nil
}

// pickImage is what the next image dialogs return: "" for canceled.
func (p *fakeImagePicker) pickImage(path string) {
	p.imageMu.Lock()
	p.image = path
	p.imageMu.Unlock()
}

// imagePicks are the image dialogs asked for so far.
func (p *fakeImagePicker) imagePicks() []filePick {
	p.imageMu.Lock()
	defer p.imageMu.Unlock()
	return slices.Clone(p.imageAsked)
}

// writeImage writes a PNG of w×h pixels at path, a gradient of vivid
// colors, and returns its bytes.
func writeImage(t *testing.T, path string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(255 * x / w), uint8(255 * y / h), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lookConn is a terminal's connection that records the colors and the
// size of a cell the view tells it.
type lookConn struct {
	idleConn
	mu    sync.Mutex
	theme *terminal.Theme
	cellH int
}

func (c *lookConn) UseTheme(th *terminal.Theme) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.theme = th
	return nil
}

func (c *lookConn) ResizePixels(cols, rows, cellW, cellH int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cellH = cellH
	return nil
}

// told is the background and the height of a cell the view told.
func (c *lookConn) told() (ui.Color, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.theme == nil {
		return ui.Color{}, c.cellH
	}
	return c.theme.Background, c.cellH
}

// lookTerminal is a terminal for s made as connect makes it, fed a
// prompt, with a connection that records its look; the test is skipped
// without libghostty-vt.
func lookTerminal(t *testing.T, a *App, s *Session) *lookConn {
	t.Helper()
	done := make(chan struct{})
	conn := &lookConn{idleConn: idleConn{done}}
	opts := a.terminalOptions(s.Host)
	opts.Conn = conn
	term, err := terminal.New(opts)
	if err != nil {
		close(done)
		t.Skip("no terminal:", err)
	}
	t.Cleanup(func() { term.Close(); close(done) })
	term.Feed([]byte("[root@web-1 ~]# ls\r\n\x1b[1;34mlogs\x1b[0m  \x1b[1;32mdeploy.sh\x1b[0m  中文文件名.txt\r\n[root@web-1 ~]# "))
	s.term = term
	return conn
}

func mustTheme(t *testing.T, name string) *terminal.Theme {
	t.Helper()
	th, err := terminal.GhosttyTheme(name)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func TestTerminalOptionsFromSettings(t *testing.T) {
	a, p := testApp(t)
	h := &config.Host{Addr: "example.com"}
	opts := a.terminalOptions(h)
	if opts.Font.Family != config.DefaultTermFontFamily || opts.Font.Size != config.DefaultTermFontSize || opts.Font.Weight != 0 || opts.Font.LineHeight != 0 || len(opts.Font.Features) != 0 {
		t.Errorf("default font %+v", opts.Font)
	}
	if opts.Theme == nil || opts.Theme.Background != mustTheme(t, "Tomorrow Night").Background || opts.DarkTheme != opts.Theme {
		t.Errorf("default theme %+v", opts.Theme)
	}
	if !opts.Transparent {
		t.Error("the terminal draws its background over the app's")
	}

	a.changeTerminalSettings(func() {
		a.settings.TermFontFamily, a.settings.TermFontSize = "Fira Code", 16
		a.settings.TermLineHeight, a.settings.TermFontWeight, a.settings.TermNoLigatures = 1.25, 500, true
		a.settings.TermTheme = "Dracula"
	})
	opts = a.terminalOptions(h)
	want := terminal.Font{Family: "Fira Code", Size: 16, Weight: 500, LineHeight: 1.25, Features: []string{"-calt", "-liga"}}
	if !sameFont(opts.Font, want) || opts.Theme.Background != mustTheme(t, "Dracula").Background {
		t.Errorf("font %+v, theme %+v", opts.Font, opts.Theme)
	}

	// A new run makes the same terminals.
	b := New(p)
	if got := b.terminalOptions(h); !sameFont(got.Font, want) || got.Theme.Background != opts.Theme.Background {
		t.Errorf("read back %+v, %+v", got.Font, got.Theme)
	}

	// A theme that is gone, as a file of the user's deleted, is the
	// default one.
	a.changeTerminalSettings(func() { a.settings.TermTheme = "No Such Theme" })
	if a.termTheme == nil || a.termTheme.Background != mustTheme(t, "Tomorrow Night").Background {
		t.Errorf("missing theme gave %+v", a.termTheme)
	}
}

// A change of the font or the colors shows in the terminals open, and in
// one made while it changed.
func TestLookAppliesToOpenTerminals(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "45.56.78.90")
	conn := lookTerminal(t, a, s)
	connecting := demoSession(a, "db-1", "10.0.0.12")
	made := a.terminalOptions(connecting.Host) // as connecting began
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tomorrow := mustTheme(t, "Tomorrow Night").Background
	until(t, tt, "the first look", func() bool {
		bg, h := conn.told()
		return bg == tomorrow && h > 0
	})
	_, cellH := conn.told()

	a.changeTerminalSettings(func() { a.settings.TermFontSize, a.settings.TermTheme = 24, "Dracula" })
	dracula := mustTheme(t, "Dracula").Background
	until(t, tt, "the new look", func() bool {
		bg, h := conn.told()
		return bg == dracula && h > cellH
	})
	if text := s.term.Text(); !strings.Contains(text, "deploy.sh") {
		t.Errorf("the terminal lost its text: %q", text)
	}

	// The terminal of a session that connected meanwhile, made with the
	// look of before, takes the new one.
	conn2 := &lookConn{idleConn: idleConn{make(chan struct{})}}
	defer close(conn2.done)
	made.Conn = conn2
	term, err := terminal.New(made)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	connecting.term = term
	a.updateTerminal(term, made)
	a.active = 1
	until(t, tt, "the late terminal's look", func() bool {
		bg, h := conn2.told()
		return bg == dracula && h >= cellH*24/14
	})
}

// The Font page sets each part of the font, saved and read back.
func TestFontPage(t *testing.T) {
	a, p := testApp(t)
	a.optionsNav.Page = pageFont
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.Frame()
	for _, s := range []string{locales.FontFamily(), locales.FontSize(), locales.FontLineHeight(), locales.FontWeight(), locales.FontLigatures(), locales.LookPreview(), "tester@web-1"} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-font-en.png")

	if err := tt.Click(locales.FontFamily()); err != nil {
		t.Fatal(err)
	}
	tt.Type("Mono")
	tt.Frame()
	saveShot(t, tt, "options-font-suggestions-en.png")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Fira Code")
	tt.Key(0, ui.KeyEscape) // closes the suggestions, not the window
	if !a.windowOpen(WindowOptions) {
		t.Fatal("Escape closed the window")
	}
	if err := tt.Click(locales.FontSize()); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	if err := tt.Click(locales.FontLineHeight()); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		tt.Key(0, ui.KeyUp)
	}
	if err := tt.Click(locales.FontWeightRegular()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(locales.FontWeightMedium()); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(locales.FontLigatures()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	want := config.Settings{TermFontFamily: "Fira Code", TermFontSize: 14.5, TermLineHeight: 1.2, TermFontWeight: 500, TermNoLigatures: true}
	if got := a.settings; got != want {
		t.Fatalf("settings %+v, want %+v", got, want)
	}
	if got := config.LoadSettings(); got != want {
		t.Errorf("saved %+v", got)
	}
	if f := a.termFont; f.Family != "Fira Code" || f.Size != 14.5 || f.Weight != 500 || f.LineHeight != 1.2 || !slices.Equal(f.Features, []string{"-calt", "-liga"}) {
		t.Errorf("font %+v", f)
	}

	if err := tt.Click(locales.LookRestoreDefaults()); err != nil {
		t.Fatal(err)
	}
	if a.settings != (config.Settings{}) || !sameFont(a.termFont, termFontOf(config.Settings{})) {
		t.Errorf("defaults: %+v, %+v", a.settings, a.termFont)
	}
	// A size typed back to the default is kept as the default.
	if err := tt.Click(locales.FontSize()); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyDown)
	if a.settings.TermFontSize != 0 {
		t.Errorf("default size kept as %v", a.settings.TermFontSize)
	}
}

// The Color scheme page lists Ghostty's themes, which its search
// filters, and the one clicked colors the terminals.
func TestColorsPage(t *testing.T) {
	a, p := testApp(t)
	a.optionsNav.Page = pageColors
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.Frame()
	for _, s := range []string{"Tomorrow Night", locales.ThemeDefault(), locales.ThemesSource(), "tester@web-1"} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-colors-en.png")

	a.look.themeQuery = "dracula"
	tt.Frame()
	if !tt.HasText("Dracula") || tt.HasText("Tomorrow Night") {
		t.Fatalf("the search shows %q", tt.Texts())
	}
	if a.settings.TermTheme != "" {
		t.Fatalf("searching chose %q", a.settings.TermTheme)
	}
	if err := tt.Click("Dracula"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.TermTheme != "Dracula" || config.LoadSettings().TermTheme != "Dracula" || a.termTheme.Background != mustTheme(t, "Dracula").Background {
		t.Fatalf("chose %q, theme %+v", a.settings.TermTheme, a.termTheme)
	}
	saveShot(t, tt, "options-colors-search-en.png")

	a.look.themeQuery = "no theme is called this"
	tt.Frame()
	if !tt.HasText(locales.NoMatchingThemes()) || a.settings.TermTheme != "Dracula" {
		t.Errorf("no match: %q, theme %q", tt.Texts(), a.settings.TermTheme)
	}
	a.look.themeQuery = ""
	tt.Frame()
	if err := tt.Click(locales.LookRestoreDefaults()); err != nil {
		t.Fatal(err)
	}
	if a.settings.TermTheme != "" || a.look.themeName != config.DefaultTermTheme {
		t.Errorf("default: %q, %q", a.settings.TermTheme, a.look.themeName)
	}
}

// The pages of the terminal's look, in both languages.
func TestLookPagesInChinese(t *testing.T) {
	a, p := testApp(t)
	src := filepath.Join(t.TempDir(), "sea.png")
	writeImage(t, src, 320, 200)
	name, img, err := importBackground(src)
	if err != nil {
		t.Fatal(err)
	}
	a.setBackground(name, img)
	a.openOptions()
	p.SetLocale("zh-Hans")
	tt := p.show(t, WindowOptions)
	for _, pg := range []struct {
		title string
		texts []string
		shot  string
	}{
		{"字体", []string{"字体名称", "字号", "行高", "粗细", "连字", "预览", "恢复默认"}, "options-font-zh.png"},
		{"配色", []string{"Tomorrow Night", "默认", "预览"}, "options-colors-zh.png"},
		{"背景图片", []string{"图片", "选择…", "移除", "填充方式", "铺满", "完整显示", "平铺", "不透明度"}, "options-background-zh.png"},
	} {
		if err := tt.Click(pg.title); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for _, s := range pg.texts {
			if !tt.HasText(s) {
				t.Errorf("%s: no %q; texts %q", pg.title, s, tt.Texts())
			}
		}
		saveShot(t, tt, pg.shot)
	}

	s := demoSession(a, "web-1", "45.56.78.90")
	lookTerminal(t, a, s)
	a.active = 0
	main := ui.NewTester(a.View, 1280, 820)
	p.tt = main
	main.SetLocale("zh-Hans")
	main.Frame()
	saveShot(t, main, "terminal-background-zh.png")
}

// waitBackground asks for a's background image and waits until it is
// read, returning it.
func waitBackground(t *testing.T, a *App, p *fakePlatform) *ui.Bitmap {
	t.Helper()
	a.backgroundImage()
	p.until(t, "the background read", func() bool { return a.look.image != nil || a.look.imageErr != nil })
	return a.look.image
}

// copies are the background images the app keeps.
func copies(t *testing.T) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(config.Dir(), "background-*"))
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range names {
		names[i] = filepath.Base(n)
	}
	return names
}

// chooseImage picks path with the Background image page's Choose button,
// and waits for the app to take it.
func chooseImage(t *testing.T, a *App, p *fakePlatform, tt *ui.Tester, path string) {
	t.Helper()
	p.pickImage(path)
	if err := tt.Click(locales.ChooseImage()); err != nil {
		t.Fatal(err)
	}
	if !a.look.choosing {
		t.Fatal("no dialog")
	}
	p.until(t, "the image", func() bool { return !a.look.choosing })
	tt.Frame()
}

func TestBackgroundImage(t *testing.T) {
	a, p := testApp(t)
	src := filepath.Join(t.TempDir(), "sea.png")
	data := writeImage(t, src, 320, 200)
	a.optionsNav.Page = pageBackground
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.Frame()
	if !tt.HasText(locales.BackgroundNone()) {
		t.Errorf("no image: %q", tt.Texts())
	}
	saveShot(t, tt, "options-background-empty-en.png")

	// Canceled, nothing changes.
	chooseImage(t, a, p, tt, "")
	if a.settings.TermBackground != "" || len(copies(t)) != 0 {
		t.Fatalf("canceled: %q, %v", a.settings.TermBackground, copies(t))
	}

	chooseImage(t, a, p, tt, src)
	if picks := p.imagePicks(); len(picks) != 2 || picks[1] != (filePick{parent: WindowOptions, title: locales.ChooseImageTitle()}) {
		t.Errorf("asked %+v", picks)
	}
	name := a.settings.TermBackground
	if cs := copies(t); len(cs) != 1 || cs[0] != name || config.LoadSettings().TermBackground != name {
		t.Fatalf("copies %v, setting %q, saved %q", cs, name, config.LoadSettings().TermBackground)
	}
	path, _ := config.BackgroundPath(name)
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Error("the copy is not the image")
	}
	if a.look.image == nil || a.look.imageErr != nil || tt.HasText(locales.BackgroundNone()) {
		t.Fatalf("image %v, error %v", a.look.image, a.look.imageErr)
	}
	a.changeTerminalSettings(func() { a.settings.TermBackgroundOpacity = 45 })
	tt.Frame()
	if !tt.HasText("45%") {
		t.Errorf("opacity: %q", tt.Texts())
	}
	saveShot(t, tt, "options-background-en.png")

	// The copy stays when the original goes.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if b := New(p); waitBackground(t, b, p) == nil || b.look.imageErr != nil {
		t.Errorf("a new run: image %v, error %v", b.look.image, b.look.imageErr)
	}

	// The terminal shows the image under its text.
	s := demoSession(a, "web-1", "45.56.78.90")
	lookTerminal(t, a, s)
	a.active = 0
	main := ui.NewTester(a.View, 1280, 820)
	p.tt = main
	main.Frame()
	saveShot(t, main, "terminal-background-en.png")
	plain := a.termColors().Background
	x := int(a.sideWidth) + 500
	y := int(820*a.terminalPct/100) - 40
	if px := main.Image().RGBAAt(x, y); px.R == plain.R && px.G == plain.G && px.B == plain.B {
		t.Errorf("at %d,%d the plain background %v shows", x, y, px)
	}
	a.changeTerminalSettings(func() { a.settings.TermBackgroundFit, a.settings.TermBackgroundOpacity = config.BackgroundTile, 60 })
	main.Frame()
	saveShot(t, main, "terminal-background-tile-en.png")
	a.changeTerminalSettings(func() { a.settings.TermBackgroundFit = config.BackgroundContain })
	main.Frame()
	saveShot(t, main, "terminal-background-contain-en.png")

	// Another image replaces the copy.
	other := filepath.Join(t.TempDir(), "hills.jpeg.png")
	writeImage(t, other, 64, 64)
	chooseImage(t, a, p, tt, other)
	if cs := copies(t); len(cs) != 1 || cs[0] == name || cs[0] != a.settings.TermBackground {
		t.Fatalf("after another: copies %v, setting %q", cs, a.settings.TermBackground)
	}

	if err := tt.Click(locales.RemoveImage()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.TermBackground != "" || config.LoadSettings().TermBackground != "" || a.look.image != nil || len(copies(t)) != 0 {
		t.Fatalf("removed: %q, image %v, copies %v", a.settings.TermBackground, a.look.image, copies(t))
	}
	if !tt.HasText(locales.BackgroundNone()) {
		t.Errorf("after removing: %q", tt.Texts())
	}
	main.Frame()
	if px := main.Image().RGBAAt(x, y); px.R != plain.R || px.G != plain.G || px.B != plain.B {
		t.Errorf("at %d,%d %v, not the plain background %v", x, y, px, plain)
	}
}

// A file that is no image, or none at all, is refused without a copy,
// keeping the image there was; a copy gone missing says so.
func TestBackgroundImageErrors(t *testing.T) {
	a, p := testApp(t)
	dir := t.TempDir()
	a.optionsNav.Page = pageBackground
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.Frame()

	good := filepath.Join(dir, "good.png")
	writeImage(t, good, 40, 30)
	chooseImage(t, a, p, tt, good)
	name := a.settings.TermBackground
	if name == "" {
		t.Fatal("no image")
	}

	notes := filepath.Join(dir, "notes.png")
	os.WriteFile(notes, []byte("these are notes, not pixels"), 0o600)
	chooseImage(t, a, p, tt, notes)
	if !tt.HasText(locales.ErrImageFormat()) {
		t.Errorf("no format error: %q", tt.Texts())
	}
	if a.settings.TermBackground != name || a.look.image == nil || !slices.Equal(copies(t), []string{name}) {
		t.Errorf("the bad file changed things: %q, %v", a.settings.TermBackground, copies(t))
	}
	saveShot(t, tt, "options-background-error-en.png")

	chooseImage(t, a, p, tt, filepath.Join(dir, "gone.png"))
	if msg := describeImageError(a.look.imageErr); !strings.HasPrefix(msg, strings.TrimSuffix(locales.ErrImageRead(""), " ")) || !tt.HasText(msg) {
		t.Errorf("missing file: %q; texts %q", msg, tt.Texts())
	}
	if !slices.Equal(copies(t), []string{name}) {
		t.Errorf("copies %v", copies(t))
	}

	// A good image after clears the error.
	chooseImage(t, a, p, tt, good)
	if a.look.imageErr != nil || len(copies(t)) != 1 {
		t.Errorf("error %v, copies %v", a.look.imageErr, copies(t))
	}

	// Settings naming a copy that is gone, or a file that is not one,
	// start without an image and say so.
	for _, bad := range []string{"background-1.png", "../hosts.json"} {
		s := a.settings
		s.TermBackground = bad
		config.SaveSettings(s)
		b := New(p)
		if waitBackground(t, b, p) != nil || describeImageError(b.look.imageErr) != locales.ErrImageMissing() {
			t.Errorf("%s: image %v, error %v", bad, b.look.image, b.look.imageErr)
		}
		b.removeBackground()
		if b.settings.TermBackground != "" || b.look.imageErr != nil {
			t.Errorf("%s removed: %q, %v", bad, b.settings.TermBackground, b.look.imageErr)
		}
	}
	if cs := copies(t); len(cs) != 1 {
		t.Errorf("copies %v", cs)
	}
}
