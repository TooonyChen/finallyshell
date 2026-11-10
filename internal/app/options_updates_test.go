package app

import (
	"slices"
	"testing"

	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fakeUpdates stands in for MyGo's updater.
type fakeUpdates struct {
	status    UpdateStatus
	checks    int    // CheckForUpdates calls
	automatic []bool // what SetAutomaticUpdateChecks was given
}

func (p *fakeUpdates) Updates() UpdateStatus { return p.status }
func (p *fakeUpdates) CheckForUpdates()      { p.checks++ }
func (p *fakeUpdates) SetAutomaticUpdateChecks(on bool) {
	p.automatic = append(p.automatic, on)
	p.status.Automatic = on
}

// generalPageUpdates opens Options › General, tall enough to show its
// Updates row whole.
func generalPageUpdates(t *testing.T, a *App, p *fakePlatform) *ui.Tester {
	t.Helper()
	a.openOptions()
	tt := p.show(t, WindowOptions)
	tt.SetSize(optionsWindow.Width, 1060)
	tt.Frame()
	return tt
}

// A copy that updates itself shows its version, checks when asked, and
// turns the checks in the background off and on; a version a check found
// shows beside the button.
func TestUpdatesRowEnabled(t *testing.T) {
	a, p := testApp(t)
	p.status = UpdateStatus{Version: "0.2.0", Automatic: true}
	tt := generalPageUpdates(t, a, p)
	for _, s := range []string{locales.OptionsUpdates(), "Current version: 0.2.0", locales.CheckForUpdates(), locales.OptionCheckUpdatesAutomatically()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	if tt.HasText(locales.UpdatesReleasesPage()) || tt.HasText(locales.UpdatesManaged()) {
		t.Errorf("an install that updates links to the releases; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-updates-en.png")

	if err := tt.Click(locales.CheckForUpdates()); err != nil {
		t.Fatal(err)
	}
	if p.checks != 1 {
		t.Errorf("%d checks", p.checks)
	}
	for range 2 {
		if err := tt.Click(locales.OptionCheckUpdatesAutomatically()); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(p.automatic, []bool{false, true}) {
		t.Errorf("automatic checks set to %v", p.automatic)
	}

	// A check found a version: the platform redraws.
	p.status.Available = "0.3.0"
	tt.Frame()
	if !tt.HasText("Version 0.3.0 is available") {
		t.Errorf("no available version; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-updates-available-en.png")
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "options-updates-available-dark-en.png")
	tt.SetDark(false)

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"更新", "当前版本：0.2.0", "检查更新…", "自动检查更新", "新版本 0.3.0 已推出"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-updates-available-zh.png")
	p.status.Available = ""
	tt.Frame()
	saveShot(t, tt, "options-updates-zh.png")
}

// A copy that cannot update itself says why, offers no check, and links
// to the releases page.
func TestUpdatesRowDisabled(t *testing.T) {
	a, p := testApp(t)
	p.status = UpdateStatus{Version: "0.2.0", Blocked: UpdatesManaged}
	tt := generalPageUpdates(t, a, p)
	if !tt.HasText(locales.UpdatesManaged()) || !tt.HasText("Current version: 0.2.0") {
		t.Errorf("no reason; texts %q", tt.Texts())
	}
	if tt.HasText(locales.CheckForUpdates()) || tt.HasText(locales.OptionCheckUpdatesAutomatically()) {
		t.Errorf("an install that cannot update offers to; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-updates-managed-en.png")
	if err := tt.Click(locales.UpdatesReleasesPage()); err != nil {
		t.Fatal(err)
	}
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{releasesURL}) {
		t.Errorf("opened %q", got)
	}

	p.status = UpdateStatus{Blocked: UpdatesDevelopment}
	tt.Frame()
	if !tt.HasText(locales.UpdatesDevelopment()) || !tt.HasText(locales.UpdatesVersionDevelopment()) || tt.HasText(locales.UpdatesManaged()) {
		t.Errorf("no development build; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-updates-dev-en.png")

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"开发版本不会自行更新。", "当前版本：开发版本", "发布页面"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-updates-dev-zh.png")
	p.status = UpdateStatus{Version: "0.2.0", Blocked: UpdatesManaged}
	tt.Frame()
	if !tt.HasText(locales.UpdatesManaged()) {
		t.Errorf("no reason in Chinese; texts %q", tt.Texts())
	}
	saveShot(t, tt, "options-updates-managed-zh.png")
}
