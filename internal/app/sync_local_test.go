package app

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"finallyshell/internal/cloudsync"
	"finallyshell/internal/config"
)

func localDevice(t *testing.T) *device {
	t.Helper()
	d := newDevice(t, nil)
	d.a.newRemote = syncRemote
	d.a.settings.Sync = config.SyncSettings{Provider: config.SyncLocal, LocalDir: t.TempDir()}
	d.a.sync.credential, d.a.sync.credFor = "", config.SyncLocal
	if err := config.SaveSettings(d.a.settings); err != nil {
		t.Fatal(err)
	}
	return d
}

// transferSyncFile simulates the user's external sync tool copying the
// envelope between two machines with different local folder paths.
func transferSyncFile(t *testing.T, from, to *device) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(from.a.settings.Sync.LocalDir, cloudsync.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(to.a.settings.Sync.LocalDir, cloudsync.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocalFolderSyncTwoDevices(t *testing.T) {
	one := localDevice(t)
	one.addHost("web", "web")
	one.addHost("db", "db")
	one.a.settings.TermTheme = "Nord"
	one.sync(t)
	data, err := os.ReadFile(filepath.Join(one.a.settings.Sync.LocalDir, cloudsync.FileName))
	if err != nil || bytes.Contains(data, []byte("web.example")) || bytes.Contains(data, []byte(one.a.settings.Sync.LocalDir)) {
		t.Fatalf("envelope exposed local data: %v", err)
	}
	two := localDevice(t)
	dir := two.a.settings.Sync.LocalDir
	transferSyncFile(t, one, two)
	two.sync(t)
	if !slices.Equal(hostNames(two.a), []string{"db", "web"}) || two.a.settings.TermTheme != "Nord" {
		t.Fatal("hosts or theme did not arrive")
	}
	if two.a.settings.Sync.LocalDir != dir || config.LoadSettings().Sync.LocalDir != dir {
		t.Fatal("sync replaced the local folder")
	}
	two.a.hosts.Host("web").Name = "web-edited"
	two.sync(t)
	one.a.hosts.Host("db").Name = "db-edited"
	transferSyncFile(t, two, one)
	one.sync(t)
	transferSyncFile(t, one, two)
	two.sync(t)
	if two.a.hosts.Host("web").Name != "web-edited" || two.a.hosts.Host("db").Name != "db-edited" {
		t.Fatal("independent host edits were lost")
	}
	two.a.hosts.Hosts = two.a.hosts.Hosts[:1]
	two.sync(t)
	transferSyncFile(t, two, one)
	one.sync(t)
	if len(one.a.hosts.Hosts) != 1 {
		t.Fatal("host deletion did not arrive")
	}
}

func TestLocalFolderSyncDestinationChangedDuringSync(t *testing.T) {
	one := localDevice(t)
	one.addHost("web", "web")
	one.sync(t)
	two := localDevice(t)
	transferSyncFile(t, one, two)
	two.a.startSync(false, time.Now())
	two.a.settings.Sync.LocalDir = t.TempDir()
	two.p.until(t, "the sync", func() bool { return !two.a.sync.running })
	if len(two.a.hosts.Hosts) != 0 || two.a.sync.done != nil || config.LoadSyncState().Base != nil {
		t.Fatal("applied the previous folder's result")
	}
}

func TestLocalFolderSyncSecrets(t *testing.T) {
	a, p := testApp(t)
	a.settings.Sync = config.SyncSettings{Provider: config.SyncLocal, LocalDir: t.TempDir()}
	p.put(syncPasswordKey, "sync-password")
	p.put(syncCredentialKey(config.SyncGist), "gist-token")
	calls := p.callCount()
	a.loadSyncSecrets()
	p.until(t, "local sync password", func() bool { return a.sync.loaded && !a.sync.loading })
	if p.callCount()-calls != 1 || a.sync.password != "sync-password" || a.sync.credential != "" || !a.syncReady() {
		t.Fatal("local sync should read only the encryption password")
	}
	a.settings.Sync.Provider = config.SyncGist
	a.loadSyncSecrets()
	p.until(t, "gist credential", func() bool { return !a.sync.loading })
	if a.sync.credential != "gist-token" || !a.syncReady() {
		t.Fatal("switching to gist did not load its credential")
	}
	calls = p.callCount()
	a.settings.Sync.Provider = config.SyncLocal
	a.loadSyncSecrets()
	if a.sync.loading || p.callCount() != calls || a.sync.credential != "" || !a.syncReady() {
		t.Fatal("switching back to local kept or requested a credential")
	}
	a.settings.Sync.LocalDir = " \t"
	if a.syncReady() {
		t.Fatal("sync ready without a folder")
	}
	a.settings.Sync.LocalDir = t.TempDir()
	a.sync.password = ""
	if a.syncReady() {
		t.Fatal("sync ready without encryption password")
	}
}
