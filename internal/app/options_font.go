package app

import (
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// fontWeights are the weights the Font page offers, with their names.
var fontWeights = []struct {
	weight int
	name   func() string
}{
	{300, locales.FontWeightLight},
	{400, locales.FontWeightRegular},
	{500, locales.FontWeightMedium},
	{600, locales.FontWeightSemibold},
	{700, locales.FontWeightBold},
}

// fontPage is the Font page: the terminal's font family, size, line
// height, weight and ligatures, over a preview.
func (a *App) fontPage(c *ui.Context) {
	s := a.settings
	ui.Form(c, func() {
		ui.Field(c, locales.FontFamily(), func() {
			family := s.TermFontFamily
			if ui.Autocomplete(c, &family, a.monoFamilies()).Placeholder(locales.FontFamilyPlaceholder()).Width(300).Changed() {
				a.changeTerminalSettings(func() { a.settings.TermFontFamily = strings.TrimSpace(family) })
			}
		}).Description(locales.FontFamilyHint())
		ui.Field(c, locales.FontSize(), func() {
			size := float64(a.termFont.Size)
			if ui.NumberInput(c, &size, 6, 48, 0.5).Width(120).Changed() {
				a.changeTerminalSettings(func() { a.settings.TermFontSize = orZero(float32(size), config.DefaultTermFontSize) })
			}
		})
		ui.Field(c, locales.FontLineHeight(), func() {
			lh := float64(orDefault(s.TermLineHeight, 1))
			if ui.NumberInput(c, &lh, 0.8, 2, 0.05).Width(120).Changed() {
				a.changeTerminalSettings(func() { a.settings.TermLineHeight = orZero(float32(lh), 1) })
			}
		}).Description(locales.FontLineHeightHint())
		ui.Field(c, locales.FontWeight(), func() {
			names := make([]string, len(fontWeights))
			chosen := locales.FontWeightRegular()
			for i, w := range fontWeights {
				names[i] = w.name()
				if w.weight == orDefault(s.TermFontWeight, 400) {
					chosen = names[i]
				}
			}
			if ui.Select(c, &chosen, names).Width(160).Changed() {
				for _, w := range fontWeights {
					if w.name() == chosen {
						a.changeTerminalSettings(func() { a.settings.TermFontWeight = orZero(w.weight, 400) })
					}
				}
			}
		}).Description(locales.FontWeightHint())
		ui.Field(c, "", func() {
			ligatures := !s.TermNoLigatures
			if ui.Checkbox(c, &ligatures, locales.FontLigatures()).Changed() {
				a.changeTerminalSettings(func() { a.settings.TermNoLigatures = !ligatures })
			}
		}).Description(locales.FontLigaturesHint())
	})
	a.lookPreview(c, false)
	a.lookFooter(c, locales.LookAppliesNow(), func() {
		a.settings.TermFontFamily, a.settings.TermFontSize, a.settings.TermLineHeight = "", 0, 0
		a.settings.TermFontWeight, a.settings.TermNoLigatures = 0, false
	}, s.TermFontFamily == "" && s.TermFontSize == 0 && s.TermLineHeight == 0 && s.TermFontWeight == 0 && !s.TermNoLigatures)
}

// orDefault is v, or def when v is zero.
func orDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// orZero is v, or zero when it is def: a setting at its default is kept
// as zero.
func orZero[T comparable](v, def T) T {
	var zero T
	if v == def {
		return zero
	}
	return v
}

// lookFooter is the end of a page of the terminal's look: a note, and a
// button restoring the page's defaults with reset, disabled when they
// are the defaults already.
func (a *App) lookFooter(c *ui.Context, note string, reset func(), defaults bool) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
		widgets.Muted(c, note).FontSize(12).Grow(1).MinWidth(0)
		if ui.Button(c, locales.LookRestoreDefaults()).Disabled(defaults).Clicked() {
			a.changeTerminalSettings(reset)
		}
	})
}

// lookPreview is a sample of what terminals show with the look the
// settings set: a prompt, a listing in color, and code that ligatures
// join, on the background; with palette, the theme's 16 colors under it.
func (a *App) lookPreview(c *ui.Context, palette bool) {
	th := a.termColors()
	f := a.termFont
	fg, pal := th.Foreground, th.Palette
	weight := orDefault(f.Weight, 400)
	bold := min(max(weight+300, 700), 900)
	features := []string{}
	if a.settings.TermNoLigatures {
		features = []string{"calt=0", "liga=0"}
	}
	lines := [][]ui.Span{
		{{Text: "tester@web-1", Color: pal[10], Weight: bold}, {Text: ":", Color: fg}, {Text: "~/deploy", Color: pal[12], Weight: bold}, {Text: "$ ls --color", Color: fg}},
		{{Text: "logs", Color: pal[4], Weight: bold}, {Text: "  ", Color: fg}, {Text: "deploy.sh", Color: pal[2], Weight: bold}, {Text: "  README.md  ", Color: fg}, {Text: "backup.tar.gz", Color: pal[1], Weight: bold}},
		{{Text: "if (a != b && c >= d) => x -> y   ", Color: fg}, {Text: "0O 1lI", Color: pal[3]}},
		{{Text: "WARN ", Color: pal[11], Weight: bold}, {Text: "disk 81% ", Color: fg}, {Text: "ERROR ", Color: pal[9], Weight: bold}, {Text: "中文 日本語 한국어", Color: pal[6]}},
		{{Text: "tester@web-1", Color: pal[10], Weight: bold}, {Text: ":", Color: fg}, {Text: "~/deploy", Color: pal[12], Weight: bold}, {Text: "$ ", Color: fg}, {Text: "█", Color: orDefault(th.Cursor, fg)}},
	}
	ui.Text(c, locales.LookPreview()).Bold().Padding(6, 0, 0, 0)
	a.terminalBackdrop(c, 6).Padding(8, 10).Children(func() {
		ui.Column(c).Gap(10).Children(func() {
			ui.Column(c).Children(func() {
				for _, l := range lines {
					ui.RichText(c, l...).Font(f.Family).FontSize(f.Size).FontWeight(weight).
						LineHeight(1.2 * orDefault(f.LineHeight, 1)).FontFeatures(features...).SingleLine()
				}
			})
			if palette {
				paletteSwatches(c, th)
			}
		})
	})
}
