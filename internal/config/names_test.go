package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	var n Names
	if n.List() != nil || n.Has("cpu") {
		t.Fatalf("zero Names: %q", n.List())
	}
	n = n.Toggle("cpu").Toggle("disks").Toggle("later")
	if !slices.Equal(n.List(), []string{"cpu", "disks", "later"}) || !n.Has("disks") {
		t.Fatalf("after toggles: %q", n.List())
	}
	if n.Toggle("disks") != NewNames("cpu", "later") {
		t.Errorf("disks not taken out: %q", n.Toggle("disks").List())
	}
	if NewNames("cpu").Toggle("cpu") != "" {
		t.Error("the last name taken out is not the zero Names")
	}

	var back Names
	if err := json.Unmarshal([]byte(`["uptime", "", "network"]`), &back); err != nil {
		t.Fatal(err)
	}
	if back != NewNames("uptime", "network") {
		t.Errorf("read %q", back.List())
	}
	if data, _ := json.Marshal(back); string(data) != `["uptime","network"]` {
		t.Errorf("wrote %s", data)
	}
}

func TestMonitorHiddenSaved(t *testing.T) {
	t.Setenv("FINALLYSHELL_DIR", t.TempDir())
	if err := SaveSettings(Settings{SidebarWidth: 250}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(Dir(), "settings.json"))
	if strings.Contains(string(data), "monitorHidden") {
		t.Errorf("monitorHidden written with nothing hidden: %s", data)
	}

	want := Settings{MonitorHidden: NewNames("processes", "disks")}
	if err := SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(Dir(), "settings.json"))
	if !strings.Contains(string(data), `"monitorHidden": [`) {
		t.Errorf("no monitorHidden list in %s", data)
	}
	if got := LoadSettings(); got != want {
		t.Errorf("read back %q", got.MonitorHidden.List())
	}
	if _, ok := SyncedSettings(want)["monitorHidden"]; ok {
		t.Error("monitorHidden is synced")
	}
}
