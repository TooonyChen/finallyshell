package config

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestSettingsSyncDecided fails when a setting is in neither list: each
// new setting decides whether it is synced.
func TestSettingsSyncDecided(t *testing.T) {
	for f := range reflect.TypeFor[Settings]().Fields() {
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		synced, local := slices.Contains(syncedSettings, key), slices.Contains(localSettings, key)
		if synced == local {
			t.Errorf("setting %q (%s): add it to syncedSettings or localSettings in sync.go", key, f.Name)
		}
	}
}

func TestSyncedSettingsRoundTrip(t *testing.T) {
	s := Settings{
		Language: "zh-Hans", TermFontSize: 15, TermTheme: "Nord", SidebarWidth: 300,
		DownloadDir: "/home/me/dl", TermBackground: "bg.png",
		Sync: SyncSettings{Provider: SyncGist},
	}
	m := SyncedSettings(s)
	for _, k := range []string{"sidebarWidth", "downloadDir", "termBackground", "sync"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s is synced", k)
		}
	}
	if string(m["termTheme"]) != `"Nord"` || string(m["termFontSize"]) != "15" {
		t.Errorf("synced = %s", m)
	}

	// Another device's: the theme changed, the font size reset.
	other := map[string]json.RawMessage{"language": json.RawMessage(`"zh-Hans"`), "termTheme": json.RawMessage(`"Dracula"`)}
	got := WithSyncedSettings(s, other)
	if got.TermTheme != "Dracula" || got.TermFontSize != 0 {
		t.Errorf("synced settings not applied: %+v", got)
	}
	if got.SidebarWidth != 300 || got.DownloadDir != "/home/me/dl" || got.TermBackground != "bg.png" || got.Sync.Provider != SyncGist {
		t.Errorf("local settings changed: %+v", got)
	}
}

func TestSyncedSettingsIconTheme(t *testing.T) {
	for theme, synced := range map[string]bool{"": false, "minimal": true, "bundled:seti": true, "pkief.material/x": false} {
		_, ok := SyncedSettings(Settings{FileIconTheme: theme})["fileIconTheme"]
		if ok != synced {
			t.Errorf("%q synced = %v", theme, ok)
		}
	}
	// A theme only this device has stays, whatever the other device says.
	s := WithSyncedSettings(Settings{FileIconTheme: "pkief.material/x"}, map[string]json.RawMessage{"fileIconTheme": json.RawMessage(`"bundled:seti"`)})
	if s.FileIconTheme != "pkief.material/x" {
		t.Errorf("local theme replaced: %q", s.FileIconTheme)
	}
	s = WithSyncedSettings(Settings{FileIconTheme: "minimal"}, map[string]json.RawMessage{"fileIconTheme": json.RawMessage(`"pkief.material/x"`)})
	if s.FileIconTheme != "" {
		t.Errorf("a theme from elsewhere applied: %q", s.FileIconTheme)
	}
}

func TestSyncState(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	if st := LoadSyncState(); st.Remote != "" || st.Base != nil {
		t.Fatalf("no file: %+v", st)
	}
	want := SyncState{Remote: "gist x", Base: []byte(`{"a":1}`), Device: "laptop"}
	if err := SaveSyncState(want); err != nil {
		t.Fatal(err)
	}
	if got := LoadSyncState(); got.Remote != want.Remote || string(got.Base) != string(want.Base) || got.Device != want.Device {
		t.Fatalf("got %+v", got)
	}
}
