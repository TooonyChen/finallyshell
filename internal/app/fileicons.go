package app

import (
	"errors"
	"os"
	"slices"

	"finallyshell/internal/config"
	"finallyshell/internal/fileicons"
	"finallyshell/internal/icons"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The file panel's icons come from a VS Code file icon theme (package
// fileicons), the one the settings name. Reading a theme, and listing
// the ones installed, reads files: it runs on a goroutine, and the theme
// shows once it is read. Until the first is read the panel shows the
// app's own two icons.

// iconThemeState is the icon theme and the File icon theme page.
type iconThemeState struct {
	theme   *fileicons.Theme // what shows; nil for Minimal or before one is read
	key     string           // the setting theme was read for, once loaded
	loaded  bool
	reading bool   // a theme is being read, for readKey
	readKey string // the setting being read
	gen     int    // counts the reads, so that a stale one is dropped
	// fallback is why the theme of key does not show, so that Material
	// shows in its place: errIconThemeMissing, or what reading it gave.
	fallback error

	// The themes besides the bundled ones (fileicons.Scan), while the
	// page lists them.
	list              []fileicons.Entry
	scanned, scanning bool
	scanGen           int
	installing        bool
	installErr        error
}

// errIconThemeMissing is why a theme the settings name does not show:
// it is installed nowhere any more.
var errIconThemeMissing = errors.New("icon theme not found")

// fileIconTheme returns the theme the file panel shows, and starts
// reading the one the settings name when it is not that one.
func (a *App) fileIconTheme() *fileicons.Theme {
	st := &a.iconThemes
	want := a.settings.FileIconTheme
	if (!st.loaded || st.key != want) && !(st.reading && st.readKey == want) {
		a.readIconTheme(want)
	}
	return st.theme
}

// readIconTheme reads the theme of key on a goroutine, then shows it.
func (a *App) readIconTheme(key string) {
	st := &a.iconThemes
	st.gen++
	gen := st.gen
	if key == fileicons.KeyMinimal {
		st.theme, st.key, st.loaded, st.reading, st.fallback = nil, key, true, false, nil
		return
	}
	st.reading, st.readKey = true, key
	installDir, home := config.IconThemesDir(), a.home()
	go func() {
		th, fallback := loadIconTheme(key, installDir, home)
		a.post(func() {
			if gen != st.gen {
				return
			}
			if th != nil {
				th.RegisterFonts()
			}
			st.theme, st.key, st.loaded, st.reading, st.fallback = th, key, true, false, fallback
		})
	}()
}

// loadIconTheme reads the theme of key: a bundled one, or one Scan
// finds. When it cannot, it reads Material instead and says why.
func loadIconTheme(key, installDir, home string) (*fileicons.Theme, error) {
	e, ok := fileicons.Find(key, nil)
	if !ok {
		e, ok = fileicons.Find(key, fileicons.Scan(installDir, home))
	}
	fallback := errIconThemeMissing
	if ok {
		th, err := e.Load()
		if err == nil {
			return th, nil
		}
		fallback = err
	}
	th, _ := fileicons.BundledThemes()[0].Load()
	return th, fallback
}

// fileIcon shows the icon of a file or folder named name, open or not,
// the tree's root or not, in a box of 16 DIPs as VS Code's explorer.
func (a *App) fileIcon(c *ui.Context, name string, dir, open, root bool) {
	var ic *fileicons.Icon
	if th := a.fileIconTheme(); th != nil {
		if dir {
			ic = th.Folder(name, open, root)
		} else {
			ic = th.File(name)
		}
	}
	drawFileIcon(c, ic, dir)
}

// drawFileIcon shows ic, or the app's own icon of a folder or a file for
// nil.
func drawFileIcon(c *ui.Context, ic *fileicons.Icon, dir bool) {
	ui.Box(c).Size(16, 16).Shrink(0).Center().Children(func() {
		switch {
		case ic == nil && dir:
			ui.Icon(c, icons.Get("folder")).FontSize(14).TextColor(ui.Hex("#eab308"))
		case ic == nil:
			ui.Icon(c, icons.Get("file")).FontSize(14).TextColor(theme.TextMuted)
		case ic.Image != nil:
			ui.Image(c, ic.Image).Size(16, 16)
		default:
			t := ui.Text(c, string(ic.Glyph)).Font(ic.Font).FontSize(13 * ic.Scale).SingleLine().LineHeight(1)
			if ic.HasColor {
				t.TextColor(ic.Color)
			}
		}
	})
}

// scanIconThemes lists the installed themes again, for the page.
func (a *App) scanIconThemes() {
	st := &a.iconThemes
	st.scanning = true
	st.scanGen++
	gen := st.scanGen
	installDir, home := config.IconThemesDir(), a.home()
	go func() {
		list := fileicons.Scan(installDir, home)
		a.post(func() {
			if gen != st.scanGen {
				return
			}
			st.list, st.scanned, st.scanning = list, true, false
		})
	}()
}

// installIconTheme asks for a VSIX, installs it and shows its first
// theme.
func (a *App) installIconTheme() {
	st := &a.iconThemes
	if st.installing {
		return
	}
	st.installing, st.installErr = true, nil
	title := locales.InstallVSIXTitle()
	dir := config.IconThemesDir()
	go func() {
		path, err := a.plat.OpenVSIX(WindowOptions, title)
		if err != nil || path == "" {
			a.post(func() { st.installing = false })
			return
		}
		ext, err := fileicons.Install(path, dir)
		a.post(func() {
			st.installing = false
			if err != nil {
				st.installErr = err
				return
			}
			key := ext.ID + "/" + ext.Themes[0].ID
			// A theme of the same key read before is read again.
			if a.iconThemes.key == key {
				a.iconThemes.loaded = false
			}
			a.changeSettings(func() { a.settings.FileIconTheme = key })
			a.scanIconThemes()
		})
	}()
}

// removeIconTheme asks, then deletes the VSIX of an installed theme. The
// default shows in place of its themes.
func (a *App) removeIconTheme(e fileicons.Entry) {
	q := Confirmation{
		Title:   locales.AppTitle(),
		Message: locales.RemoveIconThemeTitle(e.Label),
		Detail:  locales.RemoveIconThemeDetail(),
		OK:      locales.RemoveIconTheme(),
		Cancel:  locales.Cancel(),
	}
	go func() {
		if !a.plat.Confirm(q) {
			return
		}
		err := os.Remove(e.Path)
		a.post(func() {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				a.iconThemes.installErr = err
				return
			}
			gone := func(x fileicons.Entry) bool { return x.Path == e.Path }
			if i := slices.IndexFunc(a.iconThemes.list, func(x fileicons.Entry) bool {
				return x.Key == a.settings.FileIconTheme
			}); i >= 0 && gone(a.iconThemes.list[i]) {
				a.changeSettings(func() { a.settings.FileIconTheme = fileicons.KeyMaterial })
			}
			a.iconThemes.list = slices.DeleteFunc(a.iconThemes.list, gone)
			a.scanIconThemes()
		})
	}()
}

// homeDir is the user's home folder, where editors keep their
// extensions, or "".
func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
