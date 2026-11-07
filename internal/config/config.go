// Package config keeps the app's files on disk: the saved hosts and
// folders (hosts.json), the settings, and the directory they live in.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Dir is where the app keeps its files: $FINALSHELLY_DIR, else
// <user config dir>/finalshelly.
func Dir() string {
	if d := os.Getenv("FINALSHELLY_DIR"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "finalshelly")
}

// Settings are the app's preferences.
type Settings struct {
	// Language is the language the user chose, as "en" or "zh-Hans", or ""
	// to follow the system's.
	Language string `json:"language,omitempty"`

	// The sizes of the window's panes, as the user last dragged their
	// dividers: the sidebar's and the folder tree's widths in DIPs, the
	// server monitor's share of the sidebar and the terminal's of the main
	// area in percent. Zero is the app's default.
	SidebarWidth    float32 `json:"sidebarWidth,omitempty"`
	MonitorPercent  float32 `json:"monitorPercent,omitempty"`
	TerminalPercent float32 `json:"terminalPercent,omitempty"`
	TreeWidth       float32 `json:"treeWidth,omitempty"`
}

func settingsPath() string { return filepath.Join(Dir(), "settings.json") }

// LoadSettings reads the settings; the defaults when there is no file.
func LoadSettings() Settings {
	var s Settings
	if data, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

// SaveSettings writes the settings.
func SaveSettings(s Settings) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0o600)
}
