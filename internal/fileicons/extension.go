package fileicons

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Limits on a VSIX: its size, as a file, and how many files it holds.
const (
	MaxVSIXSize    = 50 << 20
	maxVSIXEntries = 20000
	maxPackageJSON = 1 << 20
)

var (
	// ErrNotVSIX is the error of a file that is not a VSIX: not a zip, or
	// no extension/package.json with a publisher and a name in it.
	ErrNotVSIX = errors.New("not a VSIX package")
	// ErrNoIconThemes is the error of an extension without file icon
	// themes.
	ErrNoIconThemes = errors.New("no file icon theme in the extension")
	// ErrTooLarge is the error of a VSIX larger than MaxVSIXSize, or of
	// more files than the app reads.
	ErrTooLarge = errors.New("VSIX package too large")
)

// Extension is a VS Code extension with file icon themes.
type Extension struct {
	// ID is the extension's publisher and name, as "PKief.material-icon-theme".
	ID      string
	Version string
	// Themes are its file icon themes, in the order it lists them.
	Themes []ThemeInfo

	fsys fs.FS // the extension's folder
}

// ThemeInfo is a file icon theme as an extension lists it.
type ThemeInfo struct {
	ID    string
	Label string
	path  string // its JSON file, a path of the extension's fs.FS
}

// packageJSON is what the app reads of an extension's package.json.
type packageJSON struct {
	Name        string `json:"name"`
	Publisher   string `json:"publisher"`
	Version     string `json:"version"`
	Contributes struct {
		IconThemes []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			Path  string `json:"path"`
		} `json:"iconThemes"`
	} `json:"contributes"`
}

// ReadExtension reads the extension whose folder is fsys: its
// package.json, with the labels of its themes from package.nls.json when
// they are "%key%". An extension without file icon themes is
// ErrNoIconThemes.
func ReadExtension(fsys fs.FS) (*Extension, error) {
	data, err := readLimited(fsys, "package.json", maxPackageJSON)
	if err != nil {
		return nil, err
	}
	var pkg packageJSON
	if err := unmarshalJSONC(data, &pkg); err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	if !validName(pkg.Publisher) || !validName(pkg.Name) {
		return nil, fmt.Errorf("%w: package.json names no publisher or name", ErrNotVSIX)
	}
	e := &Extension{ID: pkg.Publisher + "." + pkg.Name, Version: pkg.Version, fsys: fsys}
	var nls map[string]json.RawMessage
	for _, t := range pkg.Contributes.IconThemes {
		p, ok := resolve(".", t.Path)
		if t.ID == "" || !ok {
			continue
		}
		label := t.Label
		if strings.HasPrefix(label, "%") && strings.HasSuffix(label, "%") && len(label) > 2 {
			if nls == nil {
				nls = map[string]json.RawMessage{}
				if b, err := readLimited(fsys, "package.nls.json", maxPackageJSON); err == nil {
					_ = unmarshalJSONC(b, &nls)
				}
			}
			label = nlsString(nls[label[1:len(label)-1]], label)
		}
		if label == "" {
			label = t.ID
		}
		e.Themes = append(e.Themes, ThemeInfo{ID: t.ID, Label: label, path: p})
	}
	if len(e.Themes) == 0 {
		return nil, ErrNoIconThemes
	}
	return e, nil
}

// nlsString is a string of package.nls.json: a string, or an object with
// its message; else def.
func nlsString(raw json.RawMessage, def string) string {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &m) == nil && m.Message != "" {
		return m.Message
	}
	return def
}

// validName tells whether s can be a publisher or an extension name as
// the app keeps it in a file name: letters, digits, '-' and '_'.
func validName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// OpenVSIX reads the extension of a VSIX, the bytes of its file, which it
// keeps.
func OpenVSIX(data []byte) (*Extension, error) {
	if len(data) > MaxVSIXSize {
		return nil, ErrTooLarge
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("%w: %v", ErrNotVSIX, err)
	}
	if len(zr.File) > maxVSIXEntries {
		return nil, ErrTooLarge
	}
	// Entries whose names leave the package (zip.ErrInsecurePath) cannot
	// be opened through fs.FS: themes only see the others.
	sub, err := fs.Sub(zr, "extension")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotVSIX, err)
	}
	e, err := ReadExtension(sub)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: no extension/package.json", ErrNotVSIX)
	}
	return e, err
}

// Theme returns the info of the extension's theme of id, and false when
// it has none.
func (e *Extension) Theme(id string) (ThemeInfo, bool) {
	for _, t := range e.Themes {
		if t.ID == id {
			return t, true
		}
	}
	return ThemeInfo{}, false
}

// LoadTheme reads the extension's theme of id, with its icons' files.
// It reads files, so it runs off the UI thread.
func (e *Extension) LoadTheme(id string) (*Theme, error) {
	t, ok := e.Theme(id)
	if !ok {
		return nil, fmt.Errorf("%s has no icon theme %q", e.ID, id)
	}
	return readTheme(e.fsys, path.Clean(t.path))
}
