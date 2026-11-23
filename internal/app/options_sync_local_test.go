package app

import (
	"path/filepath"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"
)

func TestLocalFolderSyncOption(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			a.openOptions()
			a.optionsNav.Page = pageSync
			tt := p.show(t, WindowOptions)
			tt.SetSize(800, 1000)
			choose(t, tt, locales.SyncService(), locales.SyncLocal())
			p.until(t, "sync secrets", func() bool { return a.sync.loaded })
			tt.Frame()
			if a.settings.Sync.Provider != config.SyncLocal || !tt.HasText(locales.SyncLocalHint()) || !tt.HasText(locales.SyncPassword()) {
				t.Fatalf("local folder page: %q", tt.Texts())
			}
			for _, label := range []string{locales.SyncGistToken(), locales.SyncUser(), locales.SyncWebDAVURL(), locales.SyncS3SecretKey()} {
				if tt.HasText(label) {
					t.Fatalf("unexpected provider field %q", label)
				}
			}
			dir := filepath.Join(t.TempDir(), "My sync folder")
			typeInto(t, tt, locales.SyncLocalDirectory(), dir)
			if a.settings.Sync.LocalDir != dir || config.LoadSettings().Sync.LocalDir != dir || a.syncReady() {
				t.Fatal("folder did not persist or became ready without password")
			}
			typeInto(t, tt, locales.SyncPassword(), "same-on-every-computer")
			p.until(t, "stored password", storeSettled(a, p))
			if !a.syncReady() || p.secrets()[syncPasswordKey] != "same-on-every-computer" {
				t.Fatal("local sync password was not saved")
			}
			picked := t.TempDir()
			p.pickFolder(picked)
			if err := tt.Click(locales.SyncChooseFolder()); err != nil {
				t.Fatal(err)
			}
			p.until(t, "picked sync folder", func() bool { return a.settings.Sync.LocalDir == picked })
			tt.Frame()
			saveShot(t, tt, "options-sync-local-"+lang+".png")
			choose(t, tt, locales.SyncService(), locales.SyncOff())
			choose(t, tt, locales.SyncService(), locales.SyncLocal())
			if a.settings.Sync.LocalDir != picked || config.LoadSettings().Sync.LocalDir != picked {
				t.Fatal("switching providers lost the folder")
			}
		})
	}
}
