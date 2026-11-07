package app

import (
	"errors"
	"io/fs"
	"math"
	"slices"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// The terminal's look comes from the settings of the Font, Color scheme
// and Background image pages: App keeps the font and the theme they make
// (termFont, termTheme), which every terminal has. Terminals are
// transparent: the app draws their background under them, the theme's
// color with the background image showing through it (terminalBackdrop).

// appearance is the state of the terminal's look beyond termFont and
// termTheme, and of the pages that set it.
type appearance struct {
	themeName string // the theme termTheme is, "" before it is loaded

	// The background image: the copy it is, or is to be, read from, and
	// once read, its pixels or why it does not show. It is read on a
	// goroutine once something shows it (backgroundImage); until then, as
	// when image is nil, terminals show the theme's background alone.
	background string
	image      *ui.Bitmap
	imageErr   error
	requested  bool // background's read has started, or image is it
	gen        int  // counts the reads, so that a stale one is dropped
	// read reads an image file and decodes it, off the UI thread:
	// readImage when nil. Tests hold it up.
	read     func(path string) (*ui.Bitmap, error)
	choosing bool // the dialog choosing an image is open

	families []string                   // the monospaced font families, once listed
	themes   map[string]*terminal.Theme // the themes the color page showed

	themeQuery    string
	themeList     ui.ListState
	themeScrolled bool // the list scrolled to the theme chosen
}

// The errors of a background image, besides reading and copying it.
var (
	errImageFormat   = errors.New("not an image the app can show")
	errImageTooLarge = errors.New("image file too large")
	errImageMissing  = errors.New("the copy of the background image is missing")
)

// termFontOf is the terminal's font as the settings s set it.
func termFontOf(s config.Settings) terminal.Font {
	f := terminal.Font{
		Family:     strings.TrimSpace(s.TermFontFamily),
		Size:       s.TermFontSize,
		Weight:     s.TermFontWeight,
		LineHeight: s.TermLineHeight,
	}
	if f.Family == "" {
		f.Family = config.DefaultTermFontFamily
	}
	if !(f.Size > 0) {
		f.Size = config.DefaultTermFontSize
	}
	if s.TermNoLigatures {
		f.Features = []string{"-calt", "-liga"}
	}
	return f
}

// sameFont tells whether two fonts are the same.
func sameFont(a, b terminal.Font) bool {
	return a.Family == b.Family && a.Size == b.Size && a.Weight == b.Weight &&
		slices.Equal(a.Features, b.Features) && a.CellWidth == b.CellWidth &&
		a.LineHeight == b.LineHeight && a.Thicken == b.Thicken
}

// themeName is the name of the color scheme the settings choose.
func themeName(s config.Settings) string {
	if s.TermTheme == "" {
		return config.DefaultTermTheme
	}
	return s.TermTheme
}

// loadTheme is the theme of Ghostty's called name, else the default
// one, else nil, for which the terminal follows the window.
func loadTheme(name string) *terminal.Theme {
	if t, err := terminal.GhosttyTheme(name); err == nil {
		return t
	}
	if t, err := terminal.GhosttyTheme(config.DefaultTermTheme); err == nil {
		return t
	}
	return nil
}

// termColors is the terminal's theme, never nil: the colors the area
// around a terminal shows.
func (a *App) termColors() *terminal.Theme {
	if a.termTheme != nil {
		return a.termTheme
	}
	return terminal.DarkTheme()
}

// appearanceOptions sets the options of a new terminal that the Font,
// Color scheme and Background image pages set: its font, its colors, and
// no background of its own, as the app draws it under the terminal.
func (a *App) appearanceOptions(o *terminal.Options) {
	o.Font = a.termFont
	o.Theme, o.DarkTheme = a.termTheme, a.termTheme
	o.Transparent = true
}

// lookChange is what loadAppearance changed that open terminals show.
type lookChange struct{ font, theme bool }

// loadAppearance makes the terminal's look from the settings: termFont,
// termTheme and the background image, read again only as they change.
func (a *App) loadAppearance() lookChange {
	var ch lookChange
	font := termFontOf(a.settings)
	ch.font = !sameFont(font, a.termFont)
	a.termFont = font
	if name := themeName(a.settings); name != a.look.themeName {
		a.termTheme, a.look.themeName, ch.theme = loadTheme(name), name, true
	}
	if name := a.settings.TermBackground; name != a.look.background {
		// Read once something shows it, as startup reads nothing.
		st := &a.look
		st.background, st.image, st.imageErr, st.requested = name, nil, nil, false
		st.gen++
	}
	return ch
}

// applyAppearance gives an open terminal what ch says changed of its
// look. The background needs nothing: the app draws it.
func (a *App) applyAppearance(t *terminal.Terminal, ch lookChange) {
	// SetFont also ends a zoom, so only when the font changed.
	if ch.font {
		t.SetFont(a.termFont)
	}
	if ch.theme {
		t.SetTheme(a.termTheme, a.termTheme)
	}
}

// updateTerminal gives term, made with opts, the look the settings have
// now: they may have changed while it connected.
func (a *App) updateTerminal(term *terminal.Terminal, opts terminal.Options) {
	if !sameFont(opts.Font, a.termFont) {
		term.SetFont(a.termFont)
	}
	if opts.Theme != a.termTheme {
		term.SetTheme(a.termTheme, a.termTheme)
	}
}

// readImage reads the image file at path and decodes it, scaled down to
// at most maxBackgroundSide pixels across and down.
func readImage(path string) (*ui.Bitmap, error) {
	data, err := readImageFile(path)
	if err != nil {
		return nil, err
	}
	return decodeBackground(data, maxBackgroundSide)
}

// loadBackground reads the app's copy name of the background image with
// read.
func loadBackground(name string, read func(string) (*ui.Bitmap, error)) (*ui.Bitmap, error) {
	path, err := config.BackgroundPath(name)
	if err != nil {
		return nil, err
	}
	img, err := read(path)
	if errors.Is(err, fs.ErrNotExist) {
		err = errImageMissing
	}
	return img, err
}

// backgroundImage is the background image, nil while there is none or
// it is being read. The first call after it changes starts reading it on
// a goroutine, which shows it once read, unless it changed meanwhile.
func (a *App) backgroundImage() *ui.Bitmap {
	st := &a.look
	if st.background != "" && !st.requested {
		st.requested = true
		st.gen++
		name, gen, read := st.background, st.gen, st.read
		if read == nil {
			read = readImage
		}
		go func() {
			img, err := loadBackground(name, read)
			a.post(func() {
				if st.gen == gen {
					st.image, st.imageErr = img, err
				}
			})
		}()
	}
	return st.image
}

// importBackground reads the image at path and keeps a copy of it,
// returning the copy's name and the image as the app keeps it; nothing
// is copied unless it is an image the app can show. It runs off the UI
// thread.
func importBackground(path string) (string, *ui.Bitmap, error) {
	data, err := readImageFile(path)
	if err != nil {
		return "", nil, err
	}
	img, err := decodeBackground(data, maxBackgroundSide)
	if err != nil {
		return "", nil, err
	}
	name, err := config.SaveBackground(data, path)
	if err != nil {
		return "", nil, &saveImageError{err}
	}
	return name, img, nil
}

// saveImageError is an image that could not be copied.
type saveImageError struct{ err error }

func (e *saveImageError) Error() string { return "copy the image: " + e.err.Error() }
func (e *saveImageError) Unwrap() error { return e.err }

// chooseBackground asks for an image and makes a copy of it the
// background, deleting the copy it replaces. It does nothing while the
// dialog is open.
func (a *App) chooseBackground() {
	if a.look.choosing {
		return
	}
	a.look.choosing = true
	title := locales.ChooseImageTitle()
	go func() {
		path, err := a.plat.OpenImage(WindowOptions, title)
		if err != nil || path == "" {
			a.post(func() { a.look.choosing = false })
			return
		}
		name, img, err := importBackground(path)
		a.post(func() {
			a.look.choosing = false
			if err != nil {
				a.look.imageErr = err
				return
			}
			a.setBackground(name, img)
		})
	}()
}

// setBackground makes the copy name, of img, the background, deleting
// the copy it replaces.
func (a *App) setBackground(name string, img *ui.Bitmap) {
	old := a.settings.TermBackground
	a.changeTerminalSettings(func() {
		a.settings.TermBackground = name
		st := &a.look
		st.background, st.image, st.imageErr, st.requested = name, img, nil, true
		st.gen++ // a read of the image before is stale
	})
	if old != "" && old != name {
		config.RemoveBackground(old)
	}
}

// removeBackground takes the background image away and deletes its copy.
func (a *App) removeBackground() {
	old := a.settings.TermBackground
	a.changeTerminalSettings(func() { a.settings.TermBackground = "" })
	a.look.imageErr = nil
	if old != "" {
		config.RemoveBackground(old)
	}
}

// describeImageError says why the background image does not show.
func describeImageError(err error) string {
	var saveErr *saveImageError
	switch {
	case errors.As(err, &saveErr):
		return locales.ErrImageSave(saveErr.err.Error())
	case errors.Is(err, errImageFormat):
		return locales.ErrImageFormat()
	case errors.Is(err, errImageTooLarge):
		return locales.ErrImageTooLarge(maxBackgroundPixels, formatSize(maxBackgroundSize))
	case errors.Is(err, errImageMissing), errors.Is(err, config.ErrNoBackground):
		return locales.ErrImageMissing()
	}
	return locales.ErrImageRead(err.Error())
}

// backgroundOpacity is how much of the image shows, from 0.01 to 1.
func (a *App) backgroundOpacity() float32 {
	o := a.settings.TermBackgroundOpacity
	if !(o > 0) {
		o = config.DefaultBackgroundOpacity
	}
	return min(max(o, 1), 100) / 100
}

// terminalBackdrop is the box a terminal shows in: the theme's
// background, and the background image showing through it, its corners
// rounded by radius.
func (a *App) terminalBackdrop(c *ui.Context, radius float32) ui.Element {
	bg := a.termColors().Background
	e := ui.Box(c).Background(bg).Radius(radius)
	if img := a.backgroundImage(); img != nil {
		fit, opacity := a.settings.TermBackgroundFit, a.backgroundOpacity()
		e.Draw(func(p *ui.Painter, r ui.Rect) { drawBackdrop(p, r, radius, img, fit, opacity, bg) })
	}
	return e
}

// maxTiles is about the most copies of a tiled image drawn across or
// down: a smaller image is drawn larger.
const maxTiles = 64

// drawBackdrop draws img into r as fit says, with bg over it so that
// opacity of the image shows, within corners rounded by radius.
func drawBackdrop(p *ui.Painter, r ui.Rect, radius float32, img *ui.Bitmap, fit string, opacity float32, bg ui.Color) {
	p.Clip(r, radius, func() {
		switch fit {
		case config.BackgroundTile:
			w, h := ui.ImageSize(img)
			if w <= 0 || h <= 0 {
				break
			}
			k := float32(math.Ceil(float64(max(r.W/w, r.H/h) / maxTiles)))
			w, h = w*max(k, 1), h*max(k, 1)
			for y := r.Y; y < r.Y+r.H; y += h {
				for x := r.X; x < r.X+r.W; x += w {
					p.ImageRegion(img, ui.Rect{}, ui.Rect{X: x, Y: y, W: w, H: h}, ui.ImageOptions{})
				}
			}
		case config.BackgroundContain:
			p.Image(img, r, ui.Contain)
		default:
			p.Image(img, r, ui.Cover)
		}
		if opacity < 1 {
			p.Fill(r, bg.Alpha(1-opacity), 0)
		}
	})
}

// monoFamilies are the monospaced font families installed, which the
// font page suggests; listed once.
func (a *App) monoFamilies() []string {
	if a.look.families == nil {
		a.look.families = []string{}
		for _, f := range ui.FontFamilies() {
			if f.Monospaced {
				a.look.families = append(a.look.families, f.Name)
			}
		}
	}
	return a.look.families
}

// cachedTheme is the theme name, read once.
func (a *App) cachedTheme(name string) *terminal.Theme {
	if t, ok := a.look.themes[name]; ok {
		return t
	}
	if a.look.themes == nil {
		a.look.themes = map[string]*terminal.Theme{}
	}
	t, _ := terminal.GhosttyTheme(name)
	a.look.themes[name] = t
	return t
}
