package fileicons

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// File returns the icon of a file named name (its base name), or nil
// when the theme has none that shows. It looks, as VS Code does, at the
// theme's file names first, then its extensions from the longest
// ("test.ts" before "ts"), then the file's language, then its icon for
// files. A match whose icon cannot show gives way to the next.
func (t *Theme) File(name string) *Icon {
	if t == nil {
		return nil
	}
	lower := strings.ToLower(name)
	if ic := t.lookup(t.a.FileNames, lower); ic != nil {
		return ic
	}
	for i := range len(lower) {
		if lower[i] == '.' && i+1 < len(lower) {
			if ic := t.lookup(t.a.FileExtensions, lower[i+1:]); ic != nil {
				return ic
			}
		}
	}
	if lang := Language(name); lang != "" {
		if ic := t.lookup(t.a.LanguageIDs, lang); ic != nil {
			return ic
		}
	}
	return t.Icon(t.a.File)
}

// Folder returns the icon of a folder named name, open (expanded) or
// not, the root of the tree or not, or nil when the theme has none that
// shows. An open folder takes the theme's open icons, then its closed
// ones; a root takes the theme's root icons, then a folder's.
func (t *Theme) Folder(name string, open, root bool) *Icon {
	if t == nil {
		return nil
	}
	lower := strings.ToLower(name)
	if root {
		if open {
			if ic := t.lookup(t.a.RootFolderNamesExpanded, lower); ic != nil {
				return ic
			}
		}
		if ic := t.lookup(t.a.RootFolderNames, lower); ic != nil {
			return ic
		}
		if open {
			if ic := t.Icon(t.a.RootFolderExpanded); ic != nil {
				return ic
			}
		}
		if ic := t.Icon(t.a.RootFolder); ic != nil {
			return ic
		}
	}
	if open {
		if ic := t.lookup(t.a.FolderNamesExpanded, lower); ic != nil {
			return ic
		}
	}
	if ic := t.lookup(t.a.FolderNames, lower); ic != nil {
		return ic
	}
	if open {
		if ic := t.Icon(t.a.FolderExpanded); ic != nil {
			return ic
		}
	}
	return t.Icon(t.a.Folder)
}

func (t *Theme) lookup(m map[string]string, key string) *Icon {
	id, ok := m[key]
	if !ok {
		return nil
	}
	return t.Icon(id)
}

// Icon returns the theme's icon of id, as its iconDefinitions name it, or
// nil when it has none or it cannot show: an image that does not decode,
// or a character of a font that is not registered. Each icon is made
// once.
func (t *Theme) Icon(id string) *Icon {
	if t == nil || id == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ic, ok := t.icons[id]; ok {
		return ic
	}
	ic := t.makeIcon(id)
	// A character waits for its font: it is not kept until then.
	if d, ok := t.defs[id]; !ok || d.file != nil || t.fontReady(d.font) {
		t.icons[id] = ic
	}
	return ic
}

func (t *Theme) makeIcon(id string) *Icon {
	d, ok := t.defs[id]
	if !ok {
		return nil
	}
	if d.file != nil {
		img, err := ui.DecodeImage(d.file)
		if err != nil {
			return nil
		}
		return &Icon{ID: id, Image: img}
	}
	f := t.fonts[d.font]
	if f == nil || f.family == "" {
		return nil
	}
	scale := d.scale
	if scale == 0 {
		scale = f.scale
	}
	if scale == 0 {
		scale = 1
	}
	return &Icon{ID: id, Glyph: d.glyph, Font: f.family, Color: d.color, HasColor: d.hasColor, Scale: scale}
}

// fontReady tells whether the font of id is registered or failed for
// good, so that an icon of it is what it will stay.
func (t *Theme) fontReady(id string) bool {
	f := t.fonts[id]
	return f == nil || f.family != "" || f.failed
}
