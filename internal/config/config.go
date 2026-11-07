// Package config keeps the app's settings on disk: the saved hosts and the
// directory they live in.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
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

// AuthKind is how a host logs in.
type AuthKind string

const (
	AuthPassword AuthKind = "password"
	AuthKey      AuthKind = "key"
)

// Host is a saved connection. Passwords are never saved.
type Host struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Addr    string   `json:"addr"`
	Port    int      `json:"port,omitempty"`
	User    string   `json:"user"`
	Auth    AuthKind `json:"auth"`
	KeyPath string   `json:"keyPath,omitempty"`
}

// PortOr22 is the host's port, 22 when unset.
func (h *Host) PortOr22() int {
	if h.Port == 0 {
		return 22
	}
	return h.Port
}

// Title is the host's name, else its address.
func (h *Host) Title() string {
	if h.Name != "" {
		return h.Name
	}
	return h.Addr
}

// Target is user@addr, with the port when it is not 22.
func (h *Host) Target() string {
	s := h.User + "@" + h.Addr
	if h.PortOr22() != 22 {
		s += ":" + strconv.Itoa(h.PortOr22())
	}
	return s
}

// NewHostID returns an ID for a new host.
func NewHostID() string { return fmt.Sprintf("h%d", time.Now().UnixNano()) }

func hostsPath() string { return filepath.Join(Dir(), "hosts.json") }

// LoadHosts reads the saved hosts; none when there is no file yet.
func LoadHosts() ([]*Host, error) {
	data, err := os.ReadFile(hostsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var hosts []*Host
	if err := json.Unmarshal(data, &hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

// SaveHosts writes the hosts, replacing the file at once.
func SaveHosts(hosts []*Host) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	tmp := hostsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, hostsPath())
}

// Settings are the app's preferences.
type Settings struct {
	// Language is the language the user chose, as "en" or "zh-Hans", or ""
	// to follow the system's.
	Language string `json:"language,omitempty"`
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
