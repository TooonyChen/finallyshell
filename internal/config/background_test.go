package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The terminal's look reads back as it was saved, under the keys the
// file has, and a file without them reads as the defaults.
func TestTerminalLookSettings(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	want := Settings{
		TermFontFamily: "Fira Code, monospace", TermFontSize: 15.5, TermLineHeight: 1.2, TermFontWeight: 500,
		TermNoLigatures: true, TermTheme: "Dracula",
		TermBackground: "background-1.png", TermBackgroundFit: BackgroundTile, TermBackgroundOpacity: 45,
	}
	if err := SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(); s != want {
		t.Errorf("settings %+v, want %+v", s, want)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "settings.json"))
	for _, key := range []string{"termFontFamily", "termFontSize", "termLineHeight", "termFontWeight", "termNoLigatures", "termTheme", "termBackground", "termBackgroundFit", "termBackgroundOpacity"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("no %s in %s", key, data)
		}
	}
	if err := os.WriteFile(filepath.Join(Dir(), "settings.json"), []byte(`{"language":"en"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(); s != (Settings{Language: "en"}) {
		t.Errorf("a file of before reads %+v", s)
	}
}

func TestBackgroundCopies(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	data := []byte("not really a picture")
	name, err := SaveBackground(data, "/home/me/Pictures/Sea.JPG")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "background-") || !strings.HasSuffix(name, ".jpg") || filepath.Base(name) != name {
		t.Errorf("name %q", name)
	}
	path, err := BackgroundPath(name)
	if err != nil || path != filepath.Join(Dir(), name) {
		t.Fatalf("path %q, %v", path, err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) {
		t.Errorf("copy holds %q", got)
	}
	other, err := SaveBackground(data, "b.png")
	if err != nil || other == name {
		t.Fatalf("second copy %q, %v", other, err)
	}

	if err := RemoveBackground(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("copy still there: %v", err)
	}
	if err := RemoveBackground(name); err != nil {
		t.Errorf("removing it again: %v", err)
	}
	if _, err := os.Stat(filepath.Join(Dir(), other)); err != nil {
		t.Errorf("the other copy went too: %v", err)
	}

	// Names of anything but a copy, as an edited settings.json may hold,
	// are refused: nothing outside the copies is read or deleted.
	victim := filepath.Join(Dir(), "hosts.json")
	os.WriteFile(victim, []byte("{}"), 0o600)
	for _, bad := range []string{"", "hosts.json", "../background-1.png", "background-../../hosts.json", victim, `background-a\..\hosts.json`} {
		if _, err := BackgroundPath(bad); !errors.Is(err, ErrNoBackground) {
			t.Errorf("path of %q: %v", bad, err)
		}
		if err := RemoveBackground(bad); !errors.Is(err, ErrNoBackground) {
			t.Errorf("removing %q: %v", bad, err)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("hosts.json deleted: %v", err)
	}
}
