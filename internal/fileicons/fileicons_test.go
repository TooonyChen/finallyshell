package fileicons

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/egoist/mygo/ui"
)

// testSVG is an icon file; its color tells icons apart in nothing but
// their IDs, which the tests compare.
const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><rect width="16" height="16" fill="#123456"/></svg>`

// testTheme is a theme of icons named after what they are for, every
// rule of the format used once, in JSON with comments and trailing
// commas as VS Code takes it.
const testTheme = `{
	// icons
	"iconDefinitions": {
		"file": {"iconPath": "./i/x.svg"},
		"folder": {"iconPath": "./i/x.svg"},
		"folder-open": {"iconPath": "./i/x.svg"},
		"root": {"iconPath": "./i/x.svg"},
		"root-open": {"iconPath": "./i/x.svg"},
		"name-readme": {"iconPath": "./i/x.svg"},
		"ext-ts": {"iconPath": "./i/x.svg"},
		"ext-test-ts": {"iconPath": "./i/x.svg"},
		"ext-gitignore": {"iconPath": "./i/x.svg"},
		"lang-go": {"iconPath": "./i/x.svg"},
		"lang-makefile": {"iconPath": "./i/x.svg"},
		"lang-dockercompose": {"iconPath": "./i/x.svg"},
		"folder-src": {"iconPath": "./i/x.svg"},
		"folder-src-open": {"iconPath": "./i/x.svg"},
		"folder-docs": {"iconPath": "./i/x.svg"},
		"root-named": {"iconPath": "./i/x.svg"},
		"light-ts": {"iconPath": "./i/x.svg"},
		"broken": {"iconPath": "./i/broken.svg"},
		"escapes": {"iconPath": "../../outside.svg"},
		"missing": {"iconPath": "./i/missing.svg"},
	},
	"file": "file",
	"folder": "folder",
	"folderExpanded": "folder-open",
	"rootFolder": "root",
	"rootFolderExpanded": "root-open",
	"fileNames": {"README.md": "name-readme", "bad.txt": "broken", "evil.txt": "escapes", "gone.ts": "missing"},
	"fileExtensions": {"TS": "ext-ts", "test.ts": "ext-test-ts", "gitignore": "ext-gitignore", "txt": "file"},
	"languageIds": {"go": "lang-go", "makefile": "lang-makefile", "dockercompose": "lang-dockercompose"},
	"folderNames": {"SRC": "folder-src", "docs": "folder-docs"},
	"folderNamesExpanded": {"src": "folder-src-open"},
	"rootFolderNames": {"/": "root-named"},
	/* VS Code shows these on light themes, as the app is. */
	"light": {"fileExtensions": {"ts": "light-ts"}},
}`

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"package.json": {Data: []byte(`{"publisher": "pub", "name": "icons", "version": "1.2.3",
			"contributes": {"iconThemes": [{"id": "t", "label": "%label%", "path": "./themes/t.json"}]}}`)},
		"package.nls.json":    {Data: []byte(`{"label": {"message": "Test Icons", "comment": ["x"]}}`)},
		"themes/t.json":       {Data: []byte(testTheme)},
		"themes/i/x.svg":      {Data: []byte(testSVG)},
		"themes/i/broken.svg": {Data: []byte("not an image")},
	}
}

func loadTest(t *testing.T) *Theme {
	t.Helper()
	ext, err := ReadExtension(testFS())
	if err != nil {
		t.Fatal(err)
	}
	if ext.ID != "pub.icons" || ext.Version != "1.2.3" || len(ext.Themes) != 1 || ext.Themes[0].Label != "Test Icons" {
		t.Fatalf("extension: %+v", ext)
	}
	th, err := ext.LoadTheme("t")
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func id(ic *Icon) string {
	if ic == nil {
		return "<nil>"
	}
	return ic.ID
}

func TestFileRules(t *testing.T) {
	th := loadTest(t)
	for name, want := range map[string]string{
		"README.md":          "name-readme", // fileNames, any case
		"readme.MD":          "name-readme",
		"a.ts":               "light-ts",    // the light section wins
		"a.test.ts":          "ext-test-ts", // the longest extension first
		"A.TEST.TS":          "ext-test-ts",
		".gitignore":         "ext-gitignore", // a dot file's name is its extension
		"main.go":            "lang-go",       // by language
		"Makefile":           "lang-makefile", // a language's file name
		"docker-compose.yml": "lang-dockercompose",
		"compose.yaml":       "lang-dockercompose", // a language's pattern
		"photo.png":          "file",
		"noext":              "file",
		"trailing.":          "file",
		"bad.txt":            "file", // its icon does not decode: the next rule
		"evil.txt":           "file", // its icon leaves the package
		"gone.ts":            "light-ts",
	} {
		if got := id(th.File(name)); got != want {
			t.Errorf("File(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestFolderRules(t *testing.T) {
	th := loadTest(t)
	for _, tc := range []struct {
		name       string
		open, root bool
		want       string
	}{
		{"src", false, false, "folder-src"},
		{"Src", true, false, "folder-src-open"},
		{"docs", true, false, "folder-docs"}, // no open icon: the closed one
		{"other", false, false, "folder"},
		{"other", true, false, "folder-open"},
		{"/", false, true, "root-named"},
		{"/", true, true, "root-named"},
		{"home", false, true, "root"},
		{"home", true, true, "root-open"},
	} {
		if got := id(th.Folder(tc.name, tc.open, tc.root)); got != tc.want {
			t.Errorf("Folder(%q, open %v, root %v) = %s, want %s", tc.name, tc.open, tc.root, got, tc.want)
		}
	}
	var none *Theme
	if none.File("a") != nil || none.Folder("a", false, false) != nil {
		t.Error("a nil theme has icons")
	}
}

func TestLanguage(t *testing.T) {
	for name, want := range map[string]string{
		"main.go":                 "go",
		"x.YML":                   "yaml",
		"Makefile":                "makefile",
		"Dockerfile":              "dockerfile",
		"Dockerfile.dev":          "dockerfile",
		"docker-compose.yml":      "dockercompose",
		"compose.prod.yaml":       "dockercompose",
		"tsconfig.base.json":      "jsonc",
		"copilot-instructions.md": "instructions",
		"server.log.1":            "log",
		"unknown.zzz":             "",
	} {
		if got := Language(name); got != want {
			t.Errorf("Language(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestJSONC(t *testing.T) {
	var v map[string]any
	in := "\xef\xbb\xbf{\n // c\n \"a\": \"x // not a comment, /* nor this */\", /* c */\n \"b\": [1, 2,],\n \"q\": \"\\\"\",\n}"
	if err := unmarshalJSONC([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if v["a"] != "x // not a comment, /* nor this */" || v["q"] != `"` || len(v["b"].([]any)) != 2 {
		t.Errorf("got %v", v)
	}
}

func TestBundledMaterial(t *testing.T) {
	list := BundledThemes()
	if len(list) != 2 || list[0].Key != KeyMaterial || list[1].Key != KeySeti {
		t.Fatalf("bundled: %+v", list)
	}
	if list[0].Label != "Material Icon Theme" || list[1].Label != "Seti (Visual Studio Code)" {
		t.Errorf("labels %q, %q", list[0].Label, list[1].Label)
	}
	th, err := list[0].Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"docker-compose.yml": "docker",
		"main.go":            "go",
		"package.json":       "nodejs",
		".gitignore":         "git",
		"README.md":          "readme",
		"Makefile":           "makefile",
		"photo.png":          "image",
		"no-such-type.zzz":   "file",
	} {
		if got := id(th.File(name)); got != want {
			t.Errorf("Material File(%q) = %s, want %s", name, got, want)
		}
	}
	if got := id(th.Folder("src", false, false)); got != "folder-src" {
		t.Errorf("src = %s", got)
	}
	if got := id(th.Folder("src", true, false)); got != "folder-src-open" {
		t.Errorf("src open = %s", got)
	}
	if got := id(th.Folder("whatever", false, false)); got != "folder" {
		t.Errorf("folder = %s", got)
	}
}

// Every icon of the bundled Material theme is an SVG MyGo draws.
func TestMaterialIconsDecode(t *testing.T) {
	th, err := BundledThemes()[0].Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(th.defs) < 1000 {
		t.Fatalf("only %d icons", len(th.defs))
	}
	for id := range th.defs {
		ic := th.Icon(id)
		if ic == nil || ic.Image == nil {
			t.Errorf("icon %s does not decode", id)
			continue
		}
		if w, h := ui.ImageSize(ic.Image); w <= 0 || h <= 0 {
			t.Errorf("icon %s is %vx%v", id, w, h)
		}
	}
}

func TestBundledSeti(t *testing.T) {
	th, err := BundledThemes()[1].Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(th.fonts) != 1 || th.fonts["seti"] == nil {
		t.Fatalf("fonts: %v", th.fonts)
	}
	// Before its font is registered, a character does not show.
	if ic := th.File("main.go"); ic != nil {
		t.Fatalf("main.go before the font: %+v", ic)
	}
	if err := th.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	ic := th.File("main.go")
	if id(ic) != "_go2_light" || ic.Glyph == 0 || ic.Font == "" || !ic.HasColor || ic.Scale != 1.5 {
		t.Fatalf("main.go: %+v", ic)
	}
	// The font is registered once, whichever theme asks.
	again, _ := BundledThemes()[1].Load()
	if err := again.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	if got := again.File("main.go"); got == nil || got.Font != ic.Font {
		t.Errorf("again: %+v", got)
	}
}

func TestFonts(t *testing.T) {
	woff2, err := os.ReadFile("testdata/seti-subset.woff2")
	if err != nil {
		t.Fatal(err)
	}
	sfnt, err := toSFNT(woff2)
	if err != nil {
		t.Fatal(err)
	}
	if tag := string(sfnt[:4]); tag != "\x00\x01\x00\x00" && tag != "OTTO" && tag != "true" {
		t.Errorf("not SFNT: % x", sfnt[:4])
	}
	if err := ui.RegisterFont(sfnt, "fileicons-test-woff2"); err != nil {
		t.Fatal(err)
	}
	if _, err := toSFNT([]byte("not a font at all")); err == nil {
		t.Error("garbage converts")
	}
}

func TestParse(t *testing.T) {
	if r, ok := parseCharacter(`\E001`); !ok || r != 0xE001 {
		t.Errorf("\\E001: %x %v", r, ok)
	}
	if r, ok := parseCharacter("★"); !ok || r != '★' {
		t.Errorf("★: %c %v", r, ok)
	}
	if _, ok := parseCharacter(`\zz`); ok {
		t.Error(`\zz parses`)
	}
	if c, ok := parseColor("#abc"); !ok || c != ui.Hex("#aabbcc") {
		t.Errorf("#abc: %v %v", c, ok)
	}
	if _, ok := parseColor("red"); ok {
		t.Error("red parses")
	}
	if s := parseSize("150%"); s != 1.5 {
		t.Errorf("150%% = %v", s)
	}
	if s := parseSize("26px"); s != 2 {
		t.Errorf("26px = %v", s)
	}
	if !newerVersion("1.10.0", "1.9.9") || newerVersion("1.0.0-beta", "1.0.0") || !newerVersion("1.0.0", "1.0.0-beta") || newerVersion("1.0.0", "1.0.0") {
		t.Error("newerVersion")
	}
}

// makeVSIX zips files as a VSIX holds them, under extension/, and
// extra as they are named.
func makeVSIX(t *testing.T, files fstest.MapFS, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, f := range files {
		w, err := zw.Create("extension/" + name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(f.Data)
	}
	for name, data := range extra {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(data))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstall(t *testing.T) {
	src := t.TempDir()
	dir := filepath.Join(t.TempDir(), "icon-themes")

	// A VSIX with entries that leave it: they are never written or read.
	files := testFS()
	files["themes/t.json"] = &fstest.MapFile{Data: []byte(strings.Replace(testTheme,
		`"escapes": {"iconPath": "../../outside.svg"}`, `"escapes": {"iconPath": "../../../evil.svg"}`, 1))}
	vsix := makeVSIX(t, files, map[string]string{"../evil.svg": testSVG, "/abs.svg": testSVG})
	ext, err := Install(writeFile(t, filepath.Join(src, "good.vsix"), vsix), dir)
	if err != nil {
		t.Fatal(err)
	}
	if ext.ID != "pub.icons" {
		t.Errorf("ID %q", ext.ID)
	}
	kept, err := os.ReadFile(InstalledPath(dir, "pub.icons"))
	if err != nil || !bytes.Equal(kept, vsix) {
		t.Fatalf("installed copy: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 1 {
		t.Errorf("install dir holds %v", left)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.svg")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("evil.svg written: %v", err)
	}
	list := Scan(dir, "")
	if len(list) != 1 || list[0].Key != "pub.icons/t" || list[0].Source != Installed || list[0].Label != "Test Icons" {
		t.Fatalf("scan: %+v", list)
	}
	th, err := list[0].Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := id(th.File("evil.txt")); got != "file" {
		t.Errorf("evil.txt = %s", got)
	}

	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"not a zip":    {[]byte("hello"), ErrNotVSIX},
		"no package":   {makeVSIX(t, fstest.MapFS{"readme.md": {Data: []byte("x")}}, nil), ErrNotVSIX},
		"no publisher": {makeVSIX(t, fstest.MapFS{"package.json": {Data: []byte(`{"name": "x", "contributes": {"iconThemes": [{"id": "t", "path": "t.json"}]}}`)}}, nil), ErrNotVSIX},
		"bad name":     {makeVSIX(t, fstest.MapFS{"package.json": {Data: []byte(`{"publisher": "../p", "name": "x"}`)}}, nil), ErrNotVSIX},
		"no themes":    {makeVSIX(t, fstest.MapFS{"package.json": {Data: []byte(`{"publisher": "p", "name": "x", "contributes": {"themes": []}}`)}}, nil), ErrNoIconThemes},
	} {
		_, err := Install(writeFile(t, filepath.Join(src, strings.ReplaceAll(name, " ", "-")+".vsix"), tc.data), dir)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	// A theme whose JSON does not read is refused before it is kept.
	broken := testFS()
	broken["themes/t.json"] = &fstest.MapFile{Data: []byte("{")}
	broken["package.json"] = &fstest.MapFile{Data: []byte(`{"publisher": "pub", "name": "broken", "contributes": {"iconThemes": [{"id": "t", "path": "themes/t.json"}]}}`)}
	if _, err := Install(writeFile(t, filepath.Join(src, "broken.vsix"), makeVSIX(t, broken, nil)), dir); err == nil {
		t.Error("a broken theme installs")
	}
	if _, err := os.Stat(InstalledPath(dir, "pub.broken")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("broken theme kept: %v", err)
	}

	// Too large: a sparse file past the limit.
	big := filepath.Join(src, "big.vsix")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(MaxVSIXSize + 1)
	f.Close()
	if _, err := Install(big, dir); !errors.Is(err, ErrTooLarge) {
		t.Errorf("big: %v", err)
	}
}

func TestScanEditors(t *testing.T) {
	home := t.TempDir()
	install := t.TempDir()
	pkg := func(publisher, name, version string) []byte {
		b, _ := json.Marshal(map[string]any{
			"publisher": publisher, "name": name, "version": version,
			"contributes": map[string]any{"iconThemes": []map[string]string{{"id": "t", "label": name + " " + version, "path": "./t.json"}}},
		})
		return b
	}
	ext := func(editorDir, folder string, data []byte) {
		writeFile(t, filepath.Join(home, editorDir, folder, "package.json"), data)
		writeFile(t, filepath.Join(home, editorDir, folder, "t.json"), []byte(`{"iconDefinitions": {}}`))
	}
	ext(".vscode/extensions", "a.one-1.0.0", pkg("a", "one", "1.0.0"))
	ext(".vscode/extensions", "a.one-1.10.0", pkg("a", "one", "1.10.0"))
	ext(".cursor/extensions", "a.one-1.2.0-linux-x64", pkg("a", "one", "1.2.0"))
	ext(".vscode/extensions", "b.two-2.0.0", pkg("b", "two", "2.0.0"))
	ext(".vscode/extensions", "b.two-3.0.0", pkg("b", "two", "3.0.0")) // uninstalled
	writeFile(t, filepath.Join(home, ".vscode/extensions/.obsolete"), []byte(`{"b.two-3.0.0": true}`))
	ext(".vscode-oss/extensions", "c.three-1.0.0", pkg("c", "three", "1.0.0"))
	// Not icon themes.
	writeFile(t, filepath.Join(home, ".vscode/extensions/d.colors-1.0.0/package.json"), []byte(`{"publisher": "d", "name": "colors", "contributes": {"themes": []}}`))
	writeFile(t, filepath.Join(home, ".vscode/extensions/e.broken-1.0.0/package.json"), []byte(`{`))
	// Installed from a VSIX too: the VSIX wins.
	writeFile(t, InstalledPath(install, "c.three"), makeVSIX(t, fstest.MapFS{
		"package.json": {Data: pkg("c", "three", "0.1.0")},
		"t.json":       {Data: []byte(`{}`)},
	}, nil))

	// A linked extension, as one in development.
	dev := filepath.Join(t.TempDir(), "dev-icons")
	writeFile(t, filepath.Join(dev, "package.json"), pkg("f", "dev", "0.0.1"))
	if err := os.Symlink(dev, filepath.Join(home, ".vscode/extensions/f.dev-0.0.1")); err != nil {
		t.Fatal(err)
	}

	got := map[string]Entry{}
	for _, e := range Scan(install, home) {
		got[e.Key] = e
	}
	if len(got) != 4 || got["f.dev/t"].Version != "0.0.1" {
		t.Fatalf("scan: %+v", got)
	}
	if e := got["a.one/t"]; e.Label != "one 1.10.0" || e.Editor != "VS Code" || e.Source != Detected {
		t.Errorf("a.one: %+v", e)
	}
	if e := got["b.two/t"]; e.Version != "2.0.0" {
		t.Errorf("b.two: %+v", e)
	}
	if e := got["c.three/t"]; e.Source != Installed || e.Version != "0.1.0" {
		t.Errorf("c.three: %+v", e)
	}
	if _, err := got["a.one/t"].Load(); err != nil {
		t.Error(err)
	}
	if e, ok := Find("a.one/t", Scan(install, home)); !ok || e.Version != "1.10.0" {
		t.Errorf("Find: %+v %v", e, ok)
	}
	if e, ok := Find(KeySeti, nil); !ok || e.Source != Bundled {
		t.Errorf("Find seti: %+v %v", e, ok)
	}
	if _, ok := Find("x.y/z", nil); ok {
		t.Error("Find x.y/z")
	}
}
