package app

import (
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func TestLocalKeyDirectoryOption(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, p := testApp(t)
			p.SetLocale(lang)
			a.openOptions()
			tt := p.show(t, WindowOptions)
			tt.Frame()
			if err := tt.Click(locales.OptionsSync()); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(locales.LocalKeyDirectory()) || !tt.HasText(locales.LocalKeyDirectoryHint()) {
				t.Fatal("the local key directory is not visible with sync off")
			}
			below := func(label string) {
				t.Helper()
				field, ok := tt.AccessByRole(ui.RoleTextField, locales.LocalKeyDirectory())
				previous, found := tt.Access(label)
				if !ok || !found || field.Bounds.Y < previous.Bounds.Y+previous.Bounds.H {
					t.Fatalf("the local key directory must be below %q", label)
				}
			}
			below(locales.SyncService())
			dir := t.TempDir()
			typeInto(t, tt, locales.LocalKeyDirectory(), dir)
			if a.settings.SSHKeyDir != dir || config.LoadSettings().SSHKeyDir != dir {
				t.Fatal("the local directory was not saved")
			}
			h := &config.Host{ID: "h", Auth: config.AuthKey, KeyPath: "%SSH_KEYS%/host.pem"}
			jump := &config.Host{ID: "j", Auth: config.AuthKey, KeyPath: "%SSH_KEYS%/jump.pem"}
			a.hosts.Add(h)
			a.hosts.Add(jump)
			h.Proxy = &config.ProxyConfig{Kind: config.ProxyJump, JumpHost: jump.ID}
			opts, err := a.dialOptions(h, "", nil)
			if err != nil || opts.KeyDir != dir || opts.Jump == nil || opts.Jump.Options.KeyDir != dir {
				t.Fatalf("the directory did not reach the connection and jump host: %+v, %v", opts, err)
			}
			picked := t.TempDir()
			p.pickFolder(picked)
			if err := tt.Click(locales.ChooseKeyDirectory()); err != nil {
				t.Fatal(err)
			}
			p.until(t, "the selected key directory", func() bool { return a.settings.SSHKeyDir == picked })
			tt.Frame()
			if config.LoadSettings().SSHKeyDir != picked {
				t.Fatal("the picked directory was not saved")
			}
			saveShot(t, tt, "local-key-directory-"+lang+".png")
			a.settings.Sync.Provider = config.SyncGist
			tt.SetSize(760, 900)
			tt.Frame()
			below(locales.SyncReplaceRemote())
			saveShot(t, tt, "local-key-directory-sync-on-"+lang+".png")
		})
	}
}
