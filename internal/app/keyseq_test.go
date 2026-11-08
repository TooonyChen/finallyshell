package app

import (
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"
)

// The Terminal page shows what Backspace and Delete send in both
// languages, saves each change, and restores the defaults; the
// connection editor's "Default" names what it chose.
func TestTerminalDefaultsPage(t *testing.T) {
	a, p := testApp(t)
	a.openOptions()
	tt := p.show(t, WindowOptions)
	if err := tt.Click(locales.OptionsTerminal()); err != nil {
		t.Fatal(err)
	}
	if a.optionsNav.Page != pageTerminal {
		t.Fatalf("page %q", a.optionsNav.Page)
	}
	for _, s := range []string{locales.FieldEncoding(), "UTF-8", locales.EncodingHint(), locales.FieldBackspace(), locales.FieldDelete(),
		locales.KeySeqDEL(), locales.KeySeqVT220(), locales.KeySequencesNote(), locales.LookRestoreDefaults()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	if tt.HasText(locales.OptionsComingLater()) {
		t.Error("the Terminal page is still to come")
	}
	saveShot(t, tt, "options-terminal-en.png")

	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"字符编码", "退格键发送", "删除键发送", "ASCII 删除 (^?)", "VT220 (ESC [3~)", "恢复默认"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-terminal-zh.png")
	p.SetLocale("en")
	tt.Frame()

	for _, k := range [][2]string{
		{"UTF-8", "GBK"},
		{locales.KeySeqDEL(), locales.KeySeqCtrlH()},
		{locales.KeySeqVT220(), locales.KeySeqDEL()},
	} {
		if err := tt.Click(k[0]); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(k[1]); err != nil {
			t.Fatal(err)
		}
	}
	want := config.Settings{TermEncoding: config.EncodingGBK, TermBackspace: config.BackspaceCtrlH, TermDelete: config.DeleteDEL}
	if a.settings != want || config.LoadSettings() != want {
		t.Fatalf("settings %+v, saved %+v", a.settings, config.LoadSettings())
	}
	saveShot(t, tt, "options-terminal-changed-en.png")

	// The editor's "Default" follows the settings.
	h := &config.Host{ID: "h1", Name: "web", Addr: "203.0.113.10", User: "root", Auth: config.AuthPassword}
	a.hosts.Add(h)
	a.editConnection(h)
	ed := editorWindow(t, a, p)
	if err := ed.Click(locales.EditorPageTerminal()); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{locales.KeySeqDefault("GBK"), locales.KeySeqDefault(locales.KeySeqCtrlH()), locales.KeySeqDefault(locales.KeySeqDEL())} {
		if !ed.HasText(s) {
			t.Errorf("editor: no %q; texts %q", s, ed.Texts())
		}
	}

	// Choosing the defaults again writes them as nothing; so does
	// Restore defaults. Delete first, so that each label clicked shows
	// once.
	for _, k := range [][2]string{
		{"GBK", "UTF-8"},
		{locales.KeySeqDEL(), locales.KeySeqVT220()},
		{locales.KeySeqCtrlH(), locales.KeySeqDEL()},
	} {
		if err := tt.Click(k[0]); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(k[1]); err != nil {
			t.Fatal(err)
		}
	}
	if a.settings != (config.Settings{}) || config.LoadSettings() != (config.Settings{}) {
		t.Fatalf("chosen back: %+v, saved %+v", a.settings, config.LoadSettings())
	}
	a.changeSettings(func() { a.settings = want })
	tt.Frame()
	if err := tt.Click(locales.LookRestoreDefaults()); err != nil {
		t.Fatal(err)
	}
	if a.settings != (config.Settings{}) || config.LoadSettings() != (config.Settings{}) {
		t.Fatalf("restored %+v, saved %+v", a.settings, config.LoadSettings())
	}
}
