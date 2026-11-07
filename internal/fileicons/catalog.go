package fileicons

import (
	_ "embed"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The keys of the bundled themes, as settings keep them. Every other
// theme's is its extension's ID and its own, as
// "PKief.material-icon-theme/material-icon-theme".
const (
	KeyMaterial = ""             // Material Icon Theme, the default
	KeySeti     = "bundled:seti" // VS Code's Seti
	KeyMinimal  = "minimal"      // no theme: the app's own two icons
)

var (
	//go:embed bundled/material-icon-theme.vsix
	materialVSIX []byte
	//go:embed bundled/seti.vsix
	setiVSIX []byte
)

// Source is where a theme comes from.
type Source int

const (
	Bundled   Source = iota // with the app
	Installed               // from a VSIX the user installed
	Detected                // from an editor's extensions folder
)

// Entry is a theme the user can choose.
type Entry struct {
	// Key is what the settings keep to name it.
	Key    string
	Label  string
	Source Source
	// Editor is the editor a Detected theme is installed in, as "VS Code".
	Editor string
	// Version is its extension's version.
	Version string
	// Path is the VSIX of an Installed theme, the folder of a Detected
	// one's extension.
	Path string

	themeID string
	open    func() (*Extension, error)
}

// Load reads the entry's theme. It reads files, so it runs off the UI
// thread.
func (e Entry) Load() (*Theme, error) {
	if e.open == nil {
		return nil, nil
	}
	ext, err := e.open()
	if err != nil {
		return nil, err
	}
	return ext.LoadTheme(e.themeID)
}

var bundled = sync.OnceValue(func() []Entry {
	var out []Entry
	for _, b := range []struct {
		key, theme string
		data       []byte
	}{
		{KeyMaterial, "material-icon-theme", materialVSIX},
		{KeySeti, "vs-seti", setiVSIX},
	} {
		ext, err := OpenVSIX(b.data)
		if err != nil {
			panic("fileicons: bundled theme: " + err.Error())
		}
		t, ok := ext.Theme(b.theme)
		if !ok {
			panic("fileicons: bundled theme: no " + b.theme)
		}
		out = append(out, Entry{
			Key: b.key, Label: t.Label, Source: Bundled, Version: ext.Version,
			themeID: t.ID, open: func() (*Extension, error) { return ext, nil },
		})
	}
	return out
})

// BundledThemes are the themes that come with the app, the default first.
// Minimal, which has no theme, is not one of them.
func BundledThemes() []Entry { return bundled() }

// Editors are the editors whose extensions the app lists, with their
// extensions folders under the home folder.
var Editors = []struct{ Name, Dir string }{
	{"VS Code", ".vscode/extensions"},
	{"VS Code Insiders", ".vscode-insiders/extensions"},
	{"VSCodium", ".vscode-oss/extensions"},
	{"Cursor", ".cursor/extensions"},
	{"Windsurf", ".windsurf/extensions"},
}

// Scan lists the themes the user can choose besides the bundled ones:
// those of the VSIX files in installDir (as Install keeps them), then
// those of the extensions installed in the editors under home (any of
// them "" to leave it out). Of an extension in several places, the
// installed VSIX wins, then the newest version. It reads files, so it
// runs off the UI thread.
func Scan(installDir, home string) []Entry {
	var out []Entry
	seen := map[string]bool{}
	if installDir != "" {
		files, _ := filepath.Glob(filepath.Join(installDir, "*.vsix"))
		sort.Strings(files)
		for _, f := range files {
			ext, err := readVSIXFile(f)
			if err != nil {
				continue
			}
			seen[strings.ToLower(ext.ID)] = true
			out = append(out, entries(ext, Installed, "", f, func() (*Extension, error) { return readVSIXFile(f) })...)
		}
	}
	if home == "" {
		return out
	}
	type found struct {
		ext    *Extension
		editor string
		dir    string
	}
	newest := map[string]found{}
	var order []string
	for _, ed := range Editors {
		root := filepath.Join(home, filepath.FromSlash(ed.Dir))
		dirs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		obsolete := readObsolete(root)
		for _, d := range dirs {
			isDir := d.IsDir() || d.Type()&fs.ModeSymlink != 0 // a linked extension, as one in development
			if !isDir || obsolete[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, d.Name())
			ext, err := ReadExtension(os.DirFS(dir))
			if err != nil {
				continue
			}
			id := strings.ToLower(ext.ID)
			if seen[id] {
				continue
			}
			old, ok := newest[id]
			if !ok {
				order = append(order, id)
			}
			if !ok || newerVersion(ext.Version, old.ext.Version) {
				newest[id] = found{ext, ed.Name, dir}
			}
		}
	}
	for _, id := range order {
		f := newest[id]
		dir := f.dir
		out = append(out, entries(f.ext, Detected, f.editor, dir, func() (*Extension, error) {
			return ReadExtension(os.DirFS(dir))
		})...)
	}
	return out
}

func entries(ext *Extension, src Source, editor, path string, open func() (*Extension, error)) []Entry {
	var out []Entry
	for _, t := range ext.Themes {
		out = append(out, Entry{
			Key: ext.ID + "/" + t.ID, Label: t.Label, Source: src, Editor: editor,
			Version: ext.Version, Path: path, themeID: t.ID, open: open,
		})
	}
	return out
}

// readObsolete reads the .obsolete file of an editor's extensions
// folder: the folders of extensions it has uninstalled but not yet
// deleted.
func readObsolete(root string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(root, ".obsolete"))
	if err != nil {
		return nil
	}
	var m map[string]bool
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// newerVersion tells whether version a is after b, comparing the
// numbers of "1.2.3" one by one, a pre-release ("1.2.3-beta") before its
// release.
func newerVersion(a, b string) bool {
	split := func(v string) ([]int, bool) {
		pre := false
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			pre = v[i] == '-'
			v = v[:i]
		}
		var nums []int
		for _, p := range strings.Split(v, ".") {
			n, _ := strconv.Atoi(p)
			nums = append(nums, n)
		}
		return nums, pre
	}
	na, pa := split(a)
	nb, pb := split(b)
	for i := range max(len(na), len(nb)) {
		var x, y int
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			return x > y
		}
	}
	return pb && !pa
}

// readVSIXFile reads the extension of the VSIX file at path.
func readVSIXFile(path string) (*Extension, error) {
	_, ext, err := readVSIX(path)
	return ext, err
}

// readVSIX reads the VSIX file at path, and its extension.
func readVSIX(path string) ([]byte, *Extension, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxVSIXSize+1))
	if err != nil {
		return nil, nil, err
	}
	ext, err := OpenVSIX(data)
	if err != nil {
		return nil, nil, err
	}
	return data, ext, nil
}

// Find returns the theme of key among the bundled ones and list (as Scan
// returns it), and false when none has it. KeyMinimal has none.
func Find(key string, list []Entry) (Entry, bool) {
	for _, e := range BundledThemes() {
		if e.Key == key {
			return e, true
		}
	}
	for _, e := range list {
		if e.Key == key {
			return e, true
		}
	}
	return Entry{}, false
}

// Install checks that the file at src is a VSIX with file icon themes
// that read, and copies it into dir as <publisher>.<name>.vsix, in place
// of an earlier version. It returns the extension. It reads and writes
// files, so it runs off the UI thread.
func Install(src, dir string) (*Extension, error) {
	data, ext, err := readVSIX(src)
	if err != nil {
		return nil, err
	}
	for _, t := range ext.Themes {
		if _, err := ext.LoadTheme(t.ID); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, ".install-*.vsix")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), InstalledPath(dir, ext.ID)); err != nil {
		return nil, err
	}
	return ext, nil
}

// InstalledPath is where Install keeps the VSIX of the extension of id in
// dir.
func InstalledPath(dir, id string) string { return filepath.Join(dir, id+".vsix") }
