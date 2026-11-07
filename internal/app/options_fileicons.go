package app

import (
	"errors"

	"finalshelly/internal/fileicons"
	"finalshelly/internal/icons"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// previewNames are the files and folders the File icon theme page shows
// its preview with: a folder open, folders, then files.
var previewNames = []struct {
	name      string
	dir, open bool
}{
	{"src", true, true},
	{".github", true, false},
	{"docs", true, false},
	{"main.go", false, false},
	{"docker-compose.yml", false, false},
	{"package.json", false, false},
	{"README.md", false, false},
	{".gitignore", false, false},
	{"Makefile", false, false},
	{"nginx.conf", false, false},
	{"deploy.sh", false, false},
	{"photo.png", false, false},
}

// fileIconsPage is the File icon theme page: a preview, and the themes to
// choose from, the bundled ones, those installed from a VSIX, and those
// of editors such as VS Code.
func (a *App) fileIconsPage(c *ui.Context) {
	st := &a.iconThemes
	if !st.scanned && !st.scanning {
		a.scanIconThemes()
	}
	chosen := a.settings.FileIconTheme

	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, locales.FileIconsPreview()).Bold()
		a.fileIconsPreview(c)
		if st.loaded && st.key == chosen && st.fallback != nil {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Get("triangle-alert")).FontSize(13).TextColor(theme.Warning)
				widgets.Muted(c, a.iconThemeFallbackText()).FontSize(12)
			})
		}
	})

	ui.Column(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Text(c, locales.FileIconsThemes()).Bold()
		ui.RadioGroup(c, func() {
			group := func(title string) {
				widgets.Muted(c, title).FontSize(12).Padding(4, 0, 0, 0)
			}
			group(locales.FileIconsBundled())
			for _, e := range fileicons.BundledThemes() {
				detail := ""
				if e.Key == fileicons.KeyMaterial {
					detail = locales.ThemeDefault()
				}
				a.iconThemeRow(c, &chosen, e.Key, e.Label, detail, nil)
			}
			a.iconThemeRow(c, &chosen, fileicons.KeyMinimal, locales.FileIconsMinimal(), locales.FileIconsMinimalNote(), nil)

			var installed, detected []fileicons.Entry
			for _, e := range st.list {
				if e.Source == fileicons.Installed {
					installed = append(installed, e)
				} else {
					detected = append(detected, e)
				}
			}
			if len(installed) > 0 {
				group(locales.FileIconsInstalled())
				for _, e := range installed {
					a.iconThemeRow(c, &chosen, e.Key, e.Label, e.Version, func() { a.removeIconTheme(e) })
				}
			}
			if len(detected) > 0 {
				group(locales.FileIconsDetected())
				for _, e := range detected {
					a.iconThemeRow(c, &chosen, e.Key, e.Label, e.Editor+" · "+e.Version, nil)
				}
			}
		}).Gap(4).Label(locales.OptionsFileIcons())
		if chosen != a.settings.FileIconTheme {
			a.changeSettings(func() { a.settings.FileIconTheme = chosen })
		}

		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(4, 0, 0, 0).Children(func() {
			if ui.Button(c, locales.InstallVSIX()).Disabled(st.installing).Clicked() {
				a.installIconTheme()
			}
			if ui.Button(c, locales.Refresh()).Disabled(st.scanning).Clicked() {
				a.scanIconThemes()
			}
			if st.installing || st.scanning {
				ui.Spinner(c)
			}
		})
		if st.installErr != nil {
			ui.Text(c, installErrorText(st.installErr)).FontSize(12).TextColor(theme.Danger)
		}
		widgets.Muted(c, locales.FileIconsNote()).FontSize(12)
	})
}

// iconThemeRow is a theme to choose: its radio, a detail after it, and a
// button that removes it when remove is not nil.
func (a *App) iconThemeRow(c *ui.Context, chosen *string, key, label, detail string, remove func()) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(24).Children(func() {
		ui.Radio(c, chosen, key, label)
		if detail != "" {
			widgets.Muted(c, detail).FontSize(12).SingleLine()
		}
		if remove != nil {
			if widgets.IconButton(c, "trash-2", locales.RemoveNamedIconTheme(label)).Clicked() {
				remove()
			}
		}
	})
}

// fileIconsPreview shows the icons of previewNames in the chosen theme,
// as the file panel does.
func (a *App) fileIconsPreview(c *ui.Context) {
	ui.Row(c).Wrap().GapX(14).GapY(6).Padding(10, 12).Border(1, theme.Border).Radius(6).Background(theme.Background).Children(func() {
		for _, p := range previewNames {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				a.fileIcon(c, p.name, p.dir, p.open, false)
				ui.Text(c, p.name).SingleLine()
			})
		}
	})
}

// iconThemeFallbackText says why the chosen theme does not show.
func (a *App) iconThemeFallbackText() string {
	if errors.Is(a.iconThemes.fallback, errIconThemeMissing) {
		return locales.IconThemeMissing()
	}
	return locales.IconThemeUnreadable(a.iconThemes.fallback.Error())
}

// installErrorText says why a VSIX did not install.
func installErrorText(err error) string {
	switch {
	case errors.Is(err, fileicons.ErrNotVSIX):
		return locales.VsixNotPackage()
	case errors.Is(err, fileicons.ErrNoIconThemes):
		return locales.VsixNoIconTheme()
	case errors.Is(err, fileicons.ErrTooLarge):
		return locales.VsixTooLarge()
	}
	return locales.VsixInstallFailed(err.Error())
}
