package app

import (
	"slices"
	"strconv"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// matchingThemes are the names of the themes holding each word of query,
// of any case, in the order GhosttyThemes sorts them.
func matchingThemes(query string) []string {
	names := terminal.GhosttyThemes()
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return names
	}
	return slices.DeleteFunc(names, func(name string) bool {
		name = strings.ToLower(name)
		for _, w := range words {
			if !strings.Contains(name, w) {
				return true
			}
		}
		return false
	})
}

// colorsPage is the Color scheme page: Ghostty's themes to search and
// choose from, each with its colors, over a preview of the one chosen.
func (a *App) colorsPage(c *ui.Context) {
	st := &a.look
	current := themeName(a.settings)
	ui.SearchField(c, &st.themeQuery).Placeholder(locales.SearchThemes()).Label(locales.SearchThemes()).Width(280)
	names := matchingThemes(st.themeQuery)
	row := slices.Index(names, current)
	if !st.themeScrolled && row >= 0 {
		st.themeScrolled = true
		st.themeList.ScrollTo(row, ui.Center)
	}
	st.themeList.Selected = &row
	st.themeList.Key = func(i int) any { return names[i] }
	st.themeList.Label = func(i int) string { return names[i] }
	ui.Column(c).Height(184).Border(1, theme.Border).Radius(6).Clip().Children(func() {
		ui.List(c, &st.themeList, len(names), func(i int) {
			a.themeRow(c, names[i], i == row)
		}).Grow(1).Padding(4).Label(locales.OptionsColorScheme()).Children(func() {
			if len(names) == 0 {
				widgets.Muted(c, locales.NoMatchingThemes()).Padding(12, 10)
			}
		})
	})
	if row >= 0 && row < len(names) && names[row] != current {
		name := names[row]
		a.changeTerminalSettings(func() { a.settings.TermTheme = orZero(name, config.DefaultTermTheme) })
	}
	a.lookPreview(c, true)
	a.lookFooter(c, locales.ThemesSource(), func() { a.settings.TermTheme = "" }, a.settings.TermTheme == "")
}

// themeRow is the row of the theme name in the list: its name, and its
// background with its first eight colors.
func (a *App) themeRow(c *ui.Context, name string, chosen bool) {
	color := theme.Text
	if chosen {
		color = theme.AccentText
	}
	ui.Row(c).Gap(10).Padding(4, 8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).SingleLine().TextColor(color).Grow(1).MinWidth(0)
		if name == config.DefaultTermTheme {
			ui.Text(c, locales.ThemeDefault()).FontSize(11).TextColor(color.Alpha(0.75))
		}
		th := a.cachedTheme(name)
		if th == nil {
			return
		}
		ui.Row(c).Gap(2).Padding(3).Radius(4).Background(th.Background).Border(1, theme.Border).Children(func() {
			ui.Text(c, "Aa").FontSize(11).TextColor(th.Foreground).Padding(0, 4, 0, 2)
			for _, col := range th.Palette[1:7] {
				ui.Box(c).Size(10, 10).Radius(2).Background(col)
			}
		})
	})
}

// paletteSwatches are the 16 colors of th, as programs number them, in
// two rows: the normal ones over the bright ones.
func paletteSwatches(c *ui.Context, th *terminal.Theme) {
	ui.Column(c).Gap(4).Children(func() {
		for r := range 2 {
			ui.Row(c).Gap(4).Children(func() {
				for i := range 8 {
					n := r*8 + i
					ui.Box(c).Height(22).Grow(1).Basis(0).Radius(3).Background(th.Palette[n]).Center().Children(func() {
						ui.Text(c, strconv.Itoa(n)).FontSize(10).TextColor(contrastText(th.Palette[n]))
					})
				}
			})
		}
	})
}

// contrastText is black or white, whichever reads better on bg.
func contrastText(bg ui.Color) ui.Color {
	if 299*int(bg.R)+587*int(bg.G)+114*int(bg.B) < 140*1000 {
		return ui.Hex("#ffffff")
	}
	return ui.Hex("#000000")
}
