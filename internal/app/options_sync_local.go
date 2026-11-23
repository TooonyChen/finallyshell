package app

import (
	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func (a *App) localSyncFolderOption(c *ui.Context) {
	ui.Field(c, locales.SyncLocalDirectory(), func() {
		ui.Row(c).Gap(8).Children(func() {
			dir := a.settings.Sync.LocalDir
			if ui.TextInput(c, &dir).Label(locales.SyncLocalDirectory()).Grow(1).MinWidth(0).Changed() {
				a.changeSettings(func() { a.settings.Sync.LocalDir = dir })
			}
			if ui.Button(c, locales.SyncChooseFolder()).Clicked() {
				a.browseSyncFolder()
			}
		})
	}).Description(locales.SyncLocalHint())
}

func (a *App) browseSyncFolder() {
	before := a.settings.Sync.LocalDir
	go func() {
		dir, err := a.plat.OpenFolder(WindowOptions, locales.SyncChooseFolder())
		if err != nil || dir == "" {
			return
		}
		a.post(func() {
			if a.settings.Sync.Provider == config.SyncLocal && a.settings.Sync.LocalDir == before {
				a.changeSettings(func() { a.settings.Sync.LocalDir = dir })
			}
		})
	}()
}
