package app

import (
	"strings"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// keyDirectoryOption makes portable key paths configurable without
// requiring the user to create an operating-system environment variable.
func (a *App) keyDirectoryOption(c *ui.Context) {
	ui.Column(c).Gap(6).Padding(8, 0).Children(func() {
		ui.Text(c, locales.LocalKeyDirectory()).Bold()
		ui.Row(c).Gap(8).Children(func() {
			dir := a.settings.SSHKeyDir
			if ui.TextInput(c, &dir).Label(locales.LocalKeyDirectory()).Placeholder(locales.LocalKeyDirectoryPlaceholder()).Grow(1).MinWidth(0).Changed() {
				a.changeSettings(func() { a.settings.SSHKeyDir = strings.TrimSpace(dir) })
			}
			if ui.Button(c, locales.ChooseKeyDirectory()).Clicked() {
				a.browseKeyDirectory()
			}
		})
		ui.Text(c, locales.LocalKeyDirectoryHint()).FontSize(12).TextColor(theme.TextMuted)
	})
}

func (a *App) browseKeyDirectory() {
	before := a.settings.SSHKeyDir
	go func() {
		dir, err := a.plat.OpenFolder(WindowOptions, locales.ChooseKeyDirectory())
		if err != nil || dir == "" {
			return
		}
		a.post(func() {
			if a.settings.SSHKeyDir == before {
				a.changeSettings(func() { a.settings.SSHKeyDir = dir })
			}
		})
	}()
}
