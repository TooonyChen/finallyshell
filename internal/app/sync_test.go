package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"finallyshell/internal/cloudsync"
	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// memRemote keeps the remote file in memory, as a WebDAV server would,
// with ETags that count the writes.
type memRemote struct {
	mu   sync.Mutex
	data []byte
	ver  int
	puts int
}

func (m *memRemote) Get(context.Context) ([]byte, cloudsync.Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil, cloudsync.Version{}, cloudsync.ErrNotFound
	}
	return m.data, cloudsync.Version{Tag: strconv.Itoa(m.ver)}, nil
}

func (m *memRemote) Put(_ context.Context, data []byte, prev *cloudsync.Version) (cloudsync.Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev != nil && prev.Tag != strconv.Itoa(m.ver) {
		return cloudsync.Version{}, cloudsync.ErrConflict
	}
	m.data, m.ver, m.puts = data, m.ver+1, m.puts+1
	return cloudsync.Version{Tag: strconv.Itoa(m.ver)}, nil
}

func (m *memRemote) writes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts
}

// device is an app on a computer of its own: its own config folder and
// credential store, syncing with r.
type device struct {
	a   *App
	p   *fakePlatform
	dir string
}

func newDevice(t *testing.T, r cloudsync.Remote) *device {
	dir := t.TempDir()
	t.Setenv("FINALLYSHELL_DIR", dir)
	p := newFakePlatform(t)
	a := New(p)
	a.home = func() string { return "" }
	a.newRemote = func(config.SyncSettings, string) (cloudsync.Remote, error) { return r, nil }
	a.settings.Sync = config.SyncSettings{Provider: config.SyncWebDAV, WebDAVURL: "https://dav.example/sync/", WebDAVUser: "me"}
	st := &a.sync
	st.password, st.credential, st.credFor, st.loaded = "correct horse", "app-password", config.SyncWebDAV, true
	return &device{a, p, dir}
}

// use makes d's config folder the one config reads and writes, as each
// device has its own.
func (d *device) use(t *testing.T) { t.Setenv("FINALLYSHELL_DIR", d.dir) }

// sync syncs d and waits for it to finish, failing on an error.
func (d *device) sync(t *testing.T) {
	t.Helper()
	d.syncErr(t)
	if d.a.sync.err != nil {
		t.Fatalf("sync: %v", d.a.sync.err)
	}
}

func (d *device) syncErr(t *testing.T) {
	t.Helper()
	d.use(t)
	d.a.startSync(false, time.Now())
	if !d.a.sync.running {
		t.Fatal("the sync did not start")
	}
	d.p.until(t, "the sync", func() bool { return !d.a.sync.running })
	d.p.drain()
}

func (d *device) addHost(id, name string) *config.Host {
	h := &config.Host{ID: id, Name: name, Addr: name + ".example", User: "root", Auth: config.AuthPassword}
	d.a.hosts.Add(h)
	d.a.saveHosts(d.a.hosts)
	return h
}

func hostNames(a *App) []string {
	var out []string
	for _, h := range a.hosts.Hosts {
		out = append(out, h.Name)
	}
	slices.Sort(out)
	return out
}

func TestSyncTwoDevices(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	web := one.addHost("h1", "web")
	one.addHost("h2", "db")
	f := one.a.hosts.AddFolder("prod", "")
	web.Folder = f.ID
	web.MonitorIfaces = []string{"eth0", "wg0"}
	one.a.settings.TermTheme, one.a.settings.SidebarWidth = "Nord", 333
	one.a.hosts.MarkConnected("h1", web.CreatedAt.AddDate(1, 0, 0))
	one.sync(t)
	if r.writes() != 1 || !one.a.sync.done.Created {
		t.Fatalf("first sync: %d puts, %+v", r.writes(), one.a.sync.done)
	}
	if st := config.LoadSyncState(); st.Remote == "" || st.Base == nil {
		t.Fatalf("no sync state: %+v", st)
	}

	// A new computer receives the hosts, the folder and the theme, but
	// not the pane sizes or the recent list.
	two := newDevice(t, r)
	two.sync(t)
	if got := hostNames(two.a); !slices.Equal(got, []string{"db", "web"}) {
		t.Fatalf("hosts %v", got)
	}
	h := two.a.hosts.Host("h1")
	if h.Folder != f.ID || two.a.hosts.Folder(f.ID) == nil || !h.LastConnectedAt.IsZero() || !slices.Equal(h.MonitorIfaces, web.MonitorIfaces) {
		t.Fatalf("host %+v", h)
	}
	if two.a.settings.TermTheme != "Nord" || two.a.settings.SidebarWidth != 0 {
		t.Fatalf("settings %+v", two.a.settings)
	}
	if loaded, _ := config.LoadHosts(); len(loaded.Hosts) != 2 {
		t.Fatalf("hosts.json has %d hosts", len(loaded.Hosts))
	}
	if r.writes() != 1 {
		t.Fatalf("pulling wrote the remote: %d puts", r.writes())
	}

	// The second deletes one for good and edits the other; the first
	// gets both changes, keeping its recent list.
	two.a.hosts.Remove("h2")
	two.a.hosts.Host("h1").Name = "web-1"
	two.sync(t)
	one.sync(t)
	if got := hostNames(one.a); !slices.Equal(got, []string{"web-1"}) {
		t.Fatalf("hosts %v", got)
	}
	if one.a.hosts.Host("h1") != web || web.LastConnectedAt.IsZero() {
		t.Fatal("the host's value was replaced, or lost its last connection")
	}
	if _, err := os.Stat(filepath.Join(one.dir, config.HostsBeforeSyncName)); err != nil {
		t.Fatalf("no copy of hosts.json: %v", err)
	}

	// Nothing changed: nothing is written.
	puts := r.writes()
	one.sync(t)
	two.sync(t)
	if r.writes() != puts {
		t.Errorf("syncs with no change wrote the remote %d times", r.writes()-puts)
	}
}

func TestSyncWrongPassword(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.addHost("h1", "web")
	one.sync(t)
	two := newDevice(t, r)
	two.addHost("h2", "mine")
	two.a.sync.password = "wrong"
	two.syncErr(t)
	if !errors.Is(two.a.sync.err, cloudsync.ErrWrongPassword) {
		t.Fatalf("err = %v", two.a.sync.err)
	}
	if got := hostNames(two.a); !slices.Equal(got, []string{"mine"}) {
		t.Fatalf("hosts changed: %v", got)
	}
	if syncErrorText(two.a.sync.err) != locales.SyncErrWrongPassword() {
		t.Error("the error is not explained")
	}

	// Replacing the remote starts over with the new password.
	two.use(t)
	two.a.startSync(true, time.Now())
	two.p.until(t, "the replace", func() bool { return !two.a.sync.running })
	if two.a.sync.err != nil {
		t.Fatal(two.a.sync.err)
	}
	one.a.sync.password = "wrong"
	one.sync(t)
	if got := hostNames(one.a); !slices.Equal(got, []string{"mine", "web"}) {
		t.Fatalf("after the replace: %v", got) // no base for the new password: nothing deleted
	}
}

// A host added while a sync is away is kept, and synced next.
func TestSyncLocalChangeDuringSync(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.addHost("h1", "web")
	one.sync(t)
	two := newDevice(t, r)
	two.addHost("h2", "db")
	two.use(t)
	two.a.startSync(false, time.Now())
	two.addHost("h3", "added meanwhile")
	two.p.until(t, "the sync", func() bool { return !two.a.sync.running })
	if two.a.sync.err != nil {
		t.Fatal(two.a.sync.err)
	}
	if got := hostNames(two.a); !slices.Equal(got, []string{"added meanwhile", "db", "web"}) {
		t.Fatalf("hosts %v", got)
	}
	if !two.a.sync.nextCheck.IsZero() || cloudsync.Equal(two.a.localPayload(), two.a.sync.synced) {
		t.Fatal("the change made meanwhile is not due to sync")
	}
	two.sync(t)
	one.sync(t)
	if got := hostNames(one.a); len(got) != 3 {
		t.Fatalf("the first device has %v", got)
	}
}

// storeSettled reports, for fakePlatform.until, whether a's secret queue
// is idle with what its jobs posted run: a job posts what it learned (as
// applySyncedSecrets' rememberSynced) before it ends, so once the queue
// is idle its posts are queued, and until would check its condition
// again before running them. Running them may queue another job.
func storeSettled(a *App, p *fakePlatform) func() bool {
	return func() bool {
		if !a.secrets.queue.idle() {
			return false
		}
		p.drain()
		return a.secrets.queue.idle()
	}
}

func TestSyncPasswords(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.a.settings.Sync.Passwords = true
	one.addHost("h1", "web")
	one.addHost("h2", "db").NoRememberPassword = true
	one.p.stored[hostSecretKey("h1", secretPassword)] = "s3cret"
	one.p.stored[hostSecretKey("h2", secretPassword)] = "not remembered"
	one.p.stored[syncPasswordKey] = "the sync password, not synced"
	one.sync(t)

	// A device without passwords keeps the remote's as they are.
	other := newDevice(t, r)
	other.sync(t)
	if len(other.p.stored) != 0 {
		t.Fatalf("a device without passwords stored %v", other.p.stored)
	}
	other.a.hosts.Host("h1").Name = "web-renamed"
	other.sync(t)

	two := newDevice(t, r)
	two.a.settings.Sync.Passwords = true
	two.sync(t)
	two.p.until(t, "the store", storeSettled(two.a, two.p))
	if got := two.p.stored[hostSecretKey("h1", secretPassword)]; got != "s3cret" {
		t.Fatalf("password %q; store %v", got, two.p.stored)
	}
	if two.a.passwords["h1"] != "s3cret" {
		t.Error("the password is not known for this run")
	}
	for _, k := range []string{hostSecretKey("h2", secretPassword), syncPasswordKey} {
		if _, ok := two.p.stored[k]; ok {
			t.Errorf("%s synced", k)
		}
	}

	// A password forgotten on one device is forgotten on the other.
	delete(two.p.stored, hostSecretKey("h1", secretPassword))
	two.sync(t)
	one.sync(t)
	one.p.until(t, "the store", storeSettled(one.a, one.p))
	if _, ok := one.p.stored[hostSecretKey("h1", secretPassword)]; ok {
		t.Error("the forgotten password is still stored")
	}
}

// Automatic sync syncs at start, then after local changes.
func TestSyncAutomatically(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.a.settings.Sync.Auto = true
	one.addHost("h1", "web")
	tt := ui.NewTester(one.a.View, 1000, 700)
	one.p.tt = tt
	tt.Frame()
	one.p.until(t, "the first sync", func() bool { return r.writes() == 1 && !one.a.sync.running })
	tt.Advance(time.Minute)
	if r.writes() != 1 {
		t.Fatal("synced again with no change")
	}
	one.addHost("h2", "db")
	tt.Advance(time.Minute)
	one.p.until(t, "the sync of the change", func() bool { return r.writes() == 2 && !one.a.sync.running })
}

func TestSyncPage(t *testing.T) {
	r := &memRemote{}
	d := newDevice(t, r)
	a, p := d.a, d.p
	a.settings.Sync = config.SyncSettings{}
	a.sync = syncState{}
	a.openOptions()
	a.optionsNav.Page = pageSync
	tt := p.show(t, WindowOptions)
	if !tt.HasText(locales.SyncIntro()) || tt.HasText(locales.SyncPassword()) {
		t.Fatalf("sync off: %q", tt.Texts())
	}
	a.settings.Sync = config.SyncSettings{Provider: config.SyncGist}
	tt.Frame()
	p.until(t, "the secrets", func() bool { return a.sync.loaded })
	for _, s := range []string{locales.SyncGistToken(), locales.SyncGistNewToken(), locales.SyncPassword(), locales.SyncIncludePasswords(), locales.SyncNow()} {
		if !tt.HasText(s) {
			t.Errorf("no %q; texts %q", s, tt.Texts())
		}
	}
	a.setSyncCredential("ghp_x")
	a.setSyncPassword("pw")
	p.until(t, "the store", storeSettled(a, p))
	if p.stored[syncCredentialKey(config.SyncGist)] != "ghp_x" || p.stored[syncPasswordKey] != "pw" {
		t.Fatalf("store %v", p.stored)
	}
	a.settings.Sync = config.SyncSettings{Provider: config.SyncWebDAV, WebDAVURL: "https://dav.jianguoyun.com/dav/finallyshell/", WebDAVUser: "me@example.com", Auto: true, Passwords: true}
	a.sync.loaded = false
	tt.Frame()
	p.until(t, "the secrets", func() bool { return a.sync.loaded })
	a.setSyncCredential("app-password")
	tt.Frame()
	saveShot(t, tt, "options-sync-en.png")
	d.sync(t)
	tt.Frame()
	if !tt.HasText(locales.SyncLastSynced(a.sync.state.At.Local().Format("2006-01-02 15:04")) + " · " + locales.SyncCreated()) {
		t.Errorf("no status; texts %q", tt.Texts())
	}
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"同步到", "同步密码", "包括已保存的密码", "自动同步", "立即同步"} {
		if !tt.HasText(s) {
			t.Errorf("no %q in Chinese; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "options-sync-zh.png")
}

// A device that synced without passwords, then includes them, receives
// them rather than deleting them everywhere.
func TestSyncPasswordsTurnedOnLater(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.a.settings.Sync.Passwords = true
	one.addHost("h1", "web")
	one.p.stored[hostSecretKey("h1", secretPassword)] = "s3cret"
	one.sync(t)

	two := newDevice(t, r)
	two.sync(t)
	two.a.settings.Sync.Passwords = true
	two.sync(t)
	two.p.until(t, "the store", storeSettled(two.a, two.p))
	if got := two.p.stored[hostSecretKey("h1", secretPassword)]; got != "s3cret" {
		t.Fatalf("password %q", got)
	}
	one.sync(t)
	one.p.until(t, "the store", storeSettled(one.a, one.p))
	if got := one.p.stored[hostSecretKey("h1", secretPassword)]; got != "s3cret" {
		t.Fatalf("the first device lost its password: %q", got)
	}
}

// What a sync brings is dropped when the destination changed meanwhile.
func TestSyncDestinationChangedDuringSync(t *testing.T) {
	r := &memRemote{}
	one := newDevice(t, r)
	one.addHost("h1", "web")
	one.sync(t)
	two := newDevice(t, r)
	two.use(t)
	two.a.startSync(false, time.Now())
	two.a.settings.Sync.WebDAVURL = "https://elsewhere.example/dav/"
	two.p.until(t, "the sync", func() bool { return !two.a.sync.running })
	if len(two.a.hosts.Hosts) != 0 || two.a.sync.done != nil {
		t.Fatalf("applied the old destination's result: %v", hostNames(two.a))
	}
	if st := config.LoadSyncState(); st.Base != nil {
		t.Fatalf("saved a base: %+v", st)
	}
}

// A password saved during a sync is kept, and synced next.
func TestSyncPasswordSavedDuringSync(t *testing.T) {
	r := &memRemote{}
	key := hostSecretKey("h1", secretPassword)
	one := newDevice(t, r)
	one.a.settings.Sync.Passwords = true
	one.addHost("h1", "web")
	one.p.stored[key] = "old"
	one.sync(t)
	two := newDevice(t, r)
	two.a.settings.Sync.Passwords = true
	two.sync(t)
	two.p.until(t, "the store", storeSettled(two.a, two.p))

	one.p.stored[key] = "new"
	one.sync(t)
	two.use(t)
	two.a.startSync(false, time.Now())
	two.a.setSecret(key, "mine") // after the sync read the passwords
	two.p.until(t, "the sync", func() bool { return !two.a.sync.running && two.a.secrets.queue.idle() })
	two.p.until(t, "the store", storeSettled(two.a, two.p))
	if got := two.p.stored[key]; got != "mine" {
		t.Fatalf("the password saved meanwhile became %q", got)
	}
	if !two.a.sync.again {
		t.Fatal("not due to sync again")
	}
	two.sync(t)
	one.sync(t)
	one.p.until(t, "the store", storeSettled(one.a, one.p))
	if got := one.p.stored[key]; got != "mine" {
		t.Fatalf("the other device has %q", got)
	}
}
