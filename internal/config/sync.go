package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncProvider is where connections and settings are synced
// (SyncSettings.Provider).
type SyncProvider string

const (
	SyncOff    SyncProvider = ""       // not synced
	SyncGist   SyncProvider = "gist"   // a secret GitHub gist
	SyncWebDAV SyncProvider = "webdav" // a WebDAV folder
	SyncS3     SyncProvider = "s3"     // an S3-compatible bucket
)

// SyncSettings are the Sync page's settings. The provider's credential
// (a token, a password, a secret key) and the sync password are not
// here: the app keeps them in the system's credential store.
type SyncSettings struct {
	Provider SyncProvider `json:"provider,omitempty"`

	// GistID is the gist the file is in: "" to find it, by its file name,
	// or create it, and keep its ID here.
	GistID string `json:"gistId,omitempty"`

	// WebDAVURL is the folder the file is in, WebDAVUser who logs in.
	WebDAVURL  string `json:"webdavUrl,omitempty"`
	WebDAVUser string `json:"webdavUser,omitempty"`

	// The bucket the file is in: its endpoint, region ("" for
	// us-east-1), name, the file's key in it ("" for the default file
	// name) and the access key's ID.
	S3Endpoint    string `json:"s3Endpoint,omitempty"`
	S3Region      string `json:"s3Region,omitempty"`
	S3Bucket      string `json:"s3Bucket,omitempty"`
	S3Key         string `json:"s3Key,omitempty"`
	S3AccessKeyID string `json:"s3AccessKeyId,omitempty"`

	// Passwords includes the saved passwords and key passphrases.
	Passwords bool `json:"passwords,omitempty"`
	// Auto syncs at start, after local changes and every Interval
	// minutes.
	Auto bool `json:"auto,omitempty"`
	// Interval is how many minutes apart automatic syncs look for others'
	// changes: 0 for DefaultSyncInterval.
	Interval int `json:"interval,omitempty"`
}

// DefaultSyncInterval is the minutes between automatic syncs when
// SyncSettings.Interval is 0.
const DefaultSyncInterval = 15

// IntervalOrDefault is s.Interval, DefaultSyncInterval when unset.
func (s SyncSettings) IntervalOrDefault() int {
	if s.Interval <= 0 {
		return DefaultSyncInterval
	}
	return s.Interval
}

// Remote names the place the file is kept, to tell whether what was last
// synced was synced there: "" when sync is off.
func (s SyncSettings) Remote() string {
	switch s.Provider {
	case SyncGist:
		return "gist " + s.GistID
	case SyncWebDAV:
		return "webdav " + s.WebDAVUser + " " + s.WebDAVURL
	case SyncS3:
		return "s3 " + s.S3Endpoint + " " + s.S3Bucket + " " + s.S3Key
	}
	return ""
}

// The settings are synced by their key in settings.json. Those in
// localSettings stay on this device: pane sizes, folders and files of
// its own, and the Sync page itself. Every key is in one list or the
// other, as a test checks, so that each new setting is decided on.
var (
	syncedSettings = []string{
		"language", "appearance",
		"noAutoSelectTab", "minimizeToTray", "noConfirmClose",
		"copyOnSelect", "rightClick", "noLinkClick", "wheelLines",
		"shortcuts", "optionAsAlt",
		"termBackspace", "termDelete", "termEncoding",
		"termFontFamily", "termFontSize", "termLineHeight", "termFontWeight", "termNoLigatures", "termTheme",
		"termBackgroundFit", "termBackgroundOpacity",
		"keepManagerOpen",
		"fileIconTheme", // only the themes every copy has: see portable
		"noFollowTerminalDir",
		"noCommandBar", "noCommandBarPaths", "noCommandBarHistory", "noCommandBarCommands", "noServerHistory",
	}
	localSettings = []string{
		"sidebarWidth", "monitorPercent", "terminalPercent", "treeWidth",
		"termBackground", // a file in this device's config folder
		"downloadDir", "askDownloadDir",
		"sync",
	}
)

// portable says whether a synced setting's value means the same on
// another device: an icon theme installed from a VSIX or found in an
// editor's extensions is not, and stays on this device.
func portable(key string, v json.RawMessage) bool {
	if key != "fileIconTheme" {
		return true
	}
	var name string
	json.Unmarshal(v, &name)
	return name == "" || name == "minimal" || strings.HasPrefix(name, "bundled:")
}

// SyncedSettings are the settings of s that are synced, by key. A
// setting at its default has no key, as in settings.json.
func SyncedSettings(s Settings) map[string]json.RawMessage {
	all := settingsMap(s)
	out := map[string]json.RawMessage{}
	for _, k := range syncedSettings {
		if v, ok := all[k]; ok && portable(k, v) {
			out[k] = v
		}
	}
	return out
}

// WithSyncedSettings is s with its synced settings replaced by synced: a
// key missing from synced is that setting's default. A setting of s
// that is not portable is kept, as it was not synced.
func WithSyncedSettings(s Settings, synced map[string]json.RawMessage) Settings {
	all := settingsMap(s)
	for _, k := range syncedSettings {
		if v, ok := all[k]; ok && !portable(k, v) {
			continue
		}
		delete(all, k)
		if v, ok := synced[k]; ok && portable(k, v) {
			all[k] = v
		}
	}
	data, _ := json.Marshal(all)
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		return s
	}
	return out
}

func settingsMap(s Settings) map[string]json.RawMessage {
	data, _ := json.Marshal(s)
	m := map[string]json.RawMessage{}
	json.Unmarshal(data, &m)
	return m
}

// SyncState is what this device keeps of its last sync, in
// sync-state.json.
type SyncState struct {
	// Remote is SyncSettings.Remote of where it synced.
	Remote string `json:"remote,omitempty"`
	// Base is the remote file as it was after the sync, still
	// encrypted: what the next sync compares both sides with.
	Base []byte `json:"base,omitempty"`
	// At is when it synced; Device is the device that wrote the file it
	// read, "" when this one wrote it first.
	At     time.Time `json:"at,omitzero"`
	Device string    `json:"device,omitempty"`
	// Secrets says whether this device included its saved passwords in
	// that sync, so that Base's passwords are its own.
	Secrets bool `json:"secrets,omitempty"`
}

func syncStatePath() string { return filepath.Join(Dir(), "sync-state.json") }

// LoadSyncState reads sync-state.json: zero when there is none.
func LoadSyncState() SyncState {
	var st SyncState
	if data, err := os.ReadFile(syncStatePath()); err == nil {
		json.Unmarshal(data, &st)
	}
	return st
}

// SaveSyncState writes sync-state.json.
func SaveSyncState(st SyncState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(syncStatePath(), append(data, '\n'))
}

// HostsBeforeSyncName is the copy of hosts.json kept, next to it, before
// a sync changes it.
const HostsBeforeSyncName = "hosts.before-sync.json"

// BackupHostsBeforeSync copies s to HostsBeforeSyncName, replacing the
// last copy.
func BackupHostsBeforeSync(s *Store) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(Dir(), HostsBeforeSyncName), append(data, '\n'))
}
