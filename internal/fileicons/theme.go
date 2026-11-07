package fileicons

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Limits on what a theme reads, against broken or hostile packages.
const (
	maxThemeJSON = 16 << 20 // the theme's JSON file
	maxIconFile  = 4 << 20  // each image or font
	maxIconTotal = 96 << 20 // all of them
)

// associations are a theme's maps from names to icons, as in its JSON
// file and in its light section.
type associations struct {
	File                    string            `json:"file"`
	Folder                  string            `json:"folder"`
	FolderExpanded          string            `json:"folderExpanded"`
	RootFolder              string            `json:"rootFolder"`
	RootFolderExpanded      string            `json:"rootFolderExpanded"`
	FileNames               map[string]string `json:"fileNames"`
	FileExtensions          map[string]string `json:"fileExtensions"`
	LanguageIDs             map[string]string `json:"languageIds"`
	FolderNames             map[string]string `json:"folderNames"`
	FolderNamesExpanded     map[string]string `json:"folderNamesExpanded"`
	RootFolderNames         map[string]string `json:"rootFolderNames"`
	RootFolderNamesExpanded map[string]string `json:"rootFolderNamesExpanded"`
}

// themeJSON is a theme's JSON file.
type themeJSON struct {
	IconDefinitions map[string]iconJSON `json:"iconDefinitions"`
	Fonts           []fontJSON          `json:"fonts"`
	associations
	Light *associations `json:"light"`
}

type iconJSON struct {
	IconPath      string `json:"iconPath"`
	FontCharacter string `json:"fontCharacter"`
	FontColor     string `json:"fontColor"`
	FontSize      string `json:"fontSize"`
	FontID        string `json:"fontId"`
}

type fontJSON struct {
	ID  string `json:"id"`
	Src []struct {
		Path   string `json:"path"`
		Format string `json:"format"`
	} `json:"src"`
	Size string `json:"size"`
}

// iconDef is an icon as the theme defines it, its file read.
type iconDef struct {
	file     []byte // an image's file, nil for a character
	glyph    rune
	font     string // the font's id
	color    ui.Color
	hasColor bool
	scale    float32 // 0 for the font's
}

// Theme is a file icon theme, read whole: what Theme.File and
// Theme.Folder find the icons with. Its methods may be called from any
// goroutine, apart from RegisterFonts.
type Theme struct {
	defs  map[string]iconDef
	a     associations
	fonts map[string]*themeFont
	first string // the id of the font icons take when they name none

	mu    sync.Mutex
	icons map[string]*Icon // made so far, nil for one that cannot show
}

// themeFont is a font of a theme, as TrueType or OpenType.
type themeFont struct {
	data   []byte
	family string  // registered as, "" before RegisterFonts or when it failed
	scale  float32 // the size its icons take, as a share
	failed bool
}

// readTheme reads the theme in fsys whose JSON file is at file, a path
// of fsys, with the files of its icons, for a light background (its
// light section, which VS Code shows on light themes, replaces what it
// names).
func readTheme(fsys fs.FS, file string) (*Theme, error) {
	data, err := readLimited(fsys, file, maxThemeJSON)
	if err != nil {
		return nil, err
	}
	var j themeJSON
	if err := unmarshalJSONC(data, &j); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	dir := path.Dir(file)
	t := &Theme{
		defs:  map[string]iconDef{},
		a:     merge(j.associations, j.Light),
		fonts: map[string]*themeFont{},
		icons: map[string]*Icon{},
	}

	total := 0
	files := map[string][]byte{} // read so far, nil for one that failed
	read := func(rel string) []byte {
		p, ok := resolve(dir, rel)
		if !ok {
			return nil
		}
		if b, ok := files[p]; ok {
			return b
		}
		var b []byte
		if total < maxIconTotal {
			b, _ = readLimited(fsys, p, maxIconFile)
			total += len(b)
		}
		files[p] = b
		return b
	}

	for _, f := range j.Fonts {
		for _, src := range f.Src {
			b := read(src.Path)
			if b == nil {
				continue
			}
			sfnt, err := toSFNT(b)
			if err != nil {
				continue
			}
			t.fonts[f.ID] = &themeFont{data: sfnt, scale: parseSize(f.Size)}
			break
		}
		if t.first == "" {
			t.first = f.ID
		}
	}

	for id, d := range j.IconDefinitions {
		switch {
		case d.IconPath != "":
			if b := read(d.IconPath); b != nil {
				t.defs[id] = iconDef{file: b}
			}
		case d.FontCharacter != "":
			r, ok := parseCharacter(d.FontCharacter)
			if !ok {
				continue
			}
			def := iconDef{glyph: r, font: d.FontID, scale: parseSize(d.FontSize)}
			if def.font == "" {
				def.font = t.first
			}
			if c, ok := parseColor(d.FontColor); ok {
				def.color, def.hasColor = c, true
			}
			t.defs[id] = def
		}
	}
	return t, nil
}

// merge returns a with what light names in its place, and the keys of
// names in lower case, as VS Code matches them. Language ids keep their
// case.
func merge(a associations, light *associations) associations {
	join := func(lower bool, base, over map[string]string) map[string]string {
		out := map[string]string{}
		for _, m := range []map[string]string{base, over} {
			for k, v := range m {
				if lower {
					k = strings.ToLower(k)
				}
				out[k] = v
			}
		}
		return out
	}
	lowerKeys := func(base, over map[string]string) map[string]string { return join(true, base, over) }
	var l associations
	if light != nil {
		l = *light
	}
	str := func(base, over string) string {
		if over != "" {
			return over
		}
		return base
	}
	langs := join(false, a.LanguageIDs, l.LanguageIDs)
	return associations{
		File:                    str(a.File, l.File),
		Folder:                  str(a.Folder, l.Folder),
		FolderExpanded:          str(a.FolderExpanded, l.FolderExpanded),
		RootFolder:              str(a.RootFolder, l.RootFolder),
		RootFolderExpanded:      str(a.RootFolderExpanded, l.RootFolderExpanded),
		FileNames:               lowerKeys(a.FileNames, l.FileNames),
		FileExtensions:          lowerKeys(a.FileExtensions, l.FileExtensions),
		LanguageIDs:             langs,
		FolderNames:             lowerKeys(a.FolderNames, l.FolderNames),
		FolderNamesExpanded:     lowerKeys(a.FolderNamesExpanded, l.FolderNamesExpanded),
		RootFolderNames:         lowerKeys(a.RootFolderNames, l.RootFolderNames),
		RootFolderNamesExpanded: lowerKeys(a.RootFolderNamesExpanded, l.RootFolderNamesExpanded),
	}
}

// resolve returns the path in the package of rel, a path relative to the
// theme's folder dir, and false when it leaves the package or is not a
// path of it.
func resolve(dir, rel string) (string, bool) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return "", false
	}
	p := path.Join(dir, rel)
	if !fs.ValidPath(p) || p == "." {
		return "", false
	}
	return p, true
}

var errTooLarge = errors.New("file too large")

// readLimited reads the file at name of fsys, of at most limit bytes.
func readLimited(fsys fs.FS, name string, limit int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: %w", name, errTooLarge)
	}
	return b, nil
}

// parseCharacter reads a fontCharacter: "\E001" (hexadecimal, as CSS
// writes it) or the character itself.
func parseCharacter(s string) (rune, bool) {
	if strings.HasPrefix(s, "\\") {
		n, err := strconv.ParseUint(s[1:], 16, 32)
		if err != nil || n == 0 {
			return 0, false
		}
		return rune(n), true
	}
	r := []rune(s)
	if len(r) != 1 {
		return 0, false
	}
	return r[0], true
}

// parseColor reads a CSS color as "#rgb", "#rrggbb" or "#rrggbbaa".
func parseColor(s string) (ui.Color, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return ui.Color{}, false
	}
	h := s[1:]
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, c := range h {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		h = b.String()
	}
	if len(h) != 6 && len(h) != 8 {
		return ui.Color{}, false
	}
	if _, err := strconv.ParseUint(h, 16, 32); err != nil {
		return ui.Color{}, false
	}
	return ui.Hex("#" + h), true
}

// parseSize reads a CSS font size as a share of the explorer's: "150%",
// or "16px" of its 13 pixels. It is 0 for none.
func parseSize(s string) float32 {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "%"):
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 32); err == nil && v > 0 {
			return float32(v / 100)
		}
	case strings.HasSuffix(s, "px"):
		if v, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32); err == nil && v > 0 {
			return float32(v / 13)
		}
	}
	return 0
}
