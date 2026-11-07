package fileicons

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/tdewolff/font"
)

// toSFNT returns a font as TrueType or OpenType, which ui.RegisterFont
// takes: WOFF and WOFF2, which themes use, are unpacked.
func toSFNT(b []byte) ([]byte, error) { return font.ToSFNT(b) }

var (
	registerMu sync.Mutex
	registered = map[string]error{} // by family: what registering it gave
)

// RegisterFonts registers the theme's fonts with ui.RegisterFont, each
// under a family of its own made of its bytes, so that the icons that
// are characters of them show. A font registered before, by this theme or
// another, is not registered again. Call it on the UI thread, before
// showing the theme's icons; a font that fails leaves its icons out. It
// returns the first error.
func (t *Theme) RegisterFonts() error {
	if t == nil {
		return nil
	}
	var first error
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, f := range t.fonts {
		if f.family != "" || f.failed {
			continue
		}
		sum := sha256.Sum256(f.data)
		family := "fileicons-" + hex.EncodeToString(sum[:8])
		registerMu.Lock()
		err, done := registered[family]
		if !done {
			err = ui.RegisterFont(f.data, family)
			registered[family] = err
		}
		registerMu.Unlock()
		if err != nil {
			f.failed = true
			if first == nil {
				first = err
			}
			continue
		}
		f.family = family
	}
	return first
}
