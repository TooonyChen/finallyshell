package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	t.Setenv("FINALSHELLY_DIR", t.TempDir())
	want := Settings{
		Language: "zh-Hans", SidebarWidth: 312.5, MonitorPercent: 48, TerminalPercent: 70.25, TreeWidth: 180,
		NoAutoSelectTab: true, MinimizeToTray: true, NoConfirmClose: true,
		CopyOnSelect: true, RightClick: RightClickCopyOrPaste, NoLinkClick: true, WheelLines: 5, KeepManagerOpen: true,
		DownloadDir: "/srv/incoming", AskDownloadDir: true,
	}
	if err := SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(); s != want {
		t.Errorf("settings %+v, want %+v", s, want)
	}

	// A file written before the General, Mouse and Transfers options reads
	// as their defaults: new tabs selected, no tray, a question before
	// closing, no copy on select, the context menu, links open, 3 lines a
	// notch, downloads into the system's folder without asking.
	old := `{"language": "en", "sidebarWidth": 300}`
	if err := os.WriteFile(filepath.Join(Dir(), "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(); s != (Settings{Language: "en", SidebarWidth: 300}) {
		t.Errorf("old file: %+v", s)
	}
	// The defaults are written as nothing.
	if err := SaveSettings(Settings{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(Dir(), "settings.json")); string(data) != "{}" {
		t.Errorf("defaults saved as %s", data)
	}
}

func TestHostHelpers(t *testing.T) {
	h := &Host{Addr: "example.com", Port: 2200, User: "root"}
	if h.Target() != "root@example.com:2200" || h.Title() != "example.com" {
		t.Errorf("target %q title %q", h.Target(), h.Title())
	}
	if !h.MonitorEnabled() || h.Deleted() || h.TermTypeOrDefault() != "xterm-256color" || h.Backspace != BackspaceDEL {
		t.Errorf("a new host: monitor %v deleted %v term %q backspace %q", h.MonitorEnabled(), h.Deleted(), h.TermTypeOrDefault(), h.Backspace)
	}
	h.TermType = "vt100"
	if h.TermTypeOrDefault() != "vt100" {
		t.Errorf("term %q", h.TermTypeOrDefault())
	}
}

func TestLoadNoFile(t *testing.T) {
	t.Setenv("FINALSHELLY_DIR", t.TempDir())
	s, err := LoadHosts()
	if err != nil || s == nil || s.Version != HostsVersion || len(s.Hosts) != 0 || len(s.Folders) != 0 {
		t.Fatalf("no file: %+v %v", s, err)
	}
}

func TestLoadBadFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FINALSHELLY_DIR", dir)
	at := time.Date(2026, 11, 7, 9, 5, 3, 0, time.Local)
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
	path := filepath.Join(dir, "hosts.json")
	bad := []byte(`[{"id": "h1", "addr": "example.com"},`) // cut short
	os.WriteFile(path, bad, 0o600)

	s, err := LoadHosts()
	if err == nil || s == nil || len(s.Hosts) != 0 {
		t.Fatalf("bad file: %+v %v", s, err)
	}
	copyPath := filepath.Join(dir, "hosts.json.broken-20261107-090503")
	if BrokenName(at) != filepath.Base(copyPath) {
		t.Errorf("name %q", BrokenName(at))
	}
	if got, _ := os.ReadFile(copyPath); !bytes.Equal(got, bad) {
		t.Fatalf("copy %q", got)
	}
	if !strings.Contains(err.Error(), copyPath) {
		t.Errorf("error does not name the copy: %v", err)
	}

	// Saving over it, as the app does next, leaves the copy.
	s.Add(&Host{Addr: "new.example.com"})
	if err := SaveHosts(s); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(copyPath); !bytes.Equal(got, bad) {
		t.Errorf("copy after save %q", got)
	}
	if again, err := LoadHosts(); err != nil || len(again.Hosts) != 1 {
		t.Errorf("reloaded %+v %v", again, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
}

func TestLoadMigratesV1(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FINALSHELLY_DIR", dir)
	v1, err := os.ReadFile("testdata/hosts_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hosts.json")
	os.WriteFile(path, v1, 0o600)

	s, err := LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	want := []*Host{
		{ID: "h1714000000000000001", Name: "web", Addr: "203.0.113.10", Port: 2222, User: "deploy", Auth: AuthPassword},
		{ID: "h1714000000000000002", Addr: "db.example.com", User: "root", Auth: AuthKey, KeyPath: "~/.ssh/id_ed25519"},
	}
	if s.Version != HostsVersion || len(s.Folders) != 0 || !reflect.DeepEqual(s.Hosts, want) {
		t.Fatalf("migrated: version %d folders %v hosts %+v", s.Version, s.Folders, s.Hosts)
	}
	for _, h := range s.Hosts {
		if !h.MonitorEnabled() || h.Deleted() || !h.LastConnectedAt.IsZero() || h.Folder != "" {
			t.Errorf("migrated host %+v", h)
		}
	}
	if backup, _ := os.ReadFile(filepath.Join(dir, V1BackupName)); !bytes.Equal(backup, v1) {
		t.Errorf("backup %q", backup)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, v1) {
		t.Errorf("loading changed hosts.json")
	}

	// The next save writes version 2, which loads as it was.
	s.MarkConnected(s.Hosts[0].ID, time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC))
	if err := SaveHosts(s); err != nil {
		t.Fatal(err)
	}
	var file struct {
		Version int               `json:"version"`
		Folders []json.RawMessage `json:"folders"`
		Hosts   []json.RawMessage `json:"hosts"`
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &file); err != nil || file.Version != 2 || file.Folders == nil || len(file.Hosts) != 2 {
		t.Fatalf("saved %s: %v", data, err)
	}
	again, err := LoadHosts()
	if err != nil || !reflect.DeepEqual(again, s) {
		t.Fatalf("reloaded %+v, want %+v (%v)", again, s, err)
	}

	// A later load of a version 1 file keeps the first backup.
	os.WriteFile(path, []byte(`[]`), 0o600)
	if _, err := LoadHosts(); err != nil {
		t.Fatal(err)
	}
	if backup, _ := os.ReadFile(filepath.Join(dir, V1BackupName)); !bytes.Equal(backup, v1) {
		t.Errorf("backup replaced: %q", backup)
	}
}

// fullHost has every field set, which the test checks, so that a field
// added later is round-tripped too.
func fullHost() *Host {
	return &Host{
		ID: "h1", Name: "web", Addr: "203.0.113.10", Port: 2222, User: "deploy",
		Auth: AuthKey, KeyPath: "~/.ssh/id_ed25519",
		Folder: "f2", Notes: "主库\nsecond line",
		NoMonitor:       true,
		LastConnectedAt: time.Date(2026, 11, 7, 9, 30, 15, 123456789, time.UTC),
		DeletedAt:       time.Date(2026, 11, 8, 1, 2, 3, 0, time.UTC),
		TermType:        "vt220",
		Backspace:       BackspaceCtrlH,
		Proxy:           &ProxyConfig{Kind: ProxySOCKS5, Addr: "198.51.100.7", Port: 1081, User: "proxyuser", JumpHost: "h9"},
		Tunnels: []Tunnel{
			{Kind: TunnelLocal, ListenPort: 8080, TargetHost: "10.0.0.5", TargetPort: 80},
			{Kind: TunnelRemote, ListenHost: "0.0.0.0", ListenPort: 9000, TargetHost: "localhost", TargetPort: 3000, Disabled: true},
			{Kind: TunnelDynamic, ListenPort: 1080},
		},

		NoRememberPassword: true,
		CreatedAt:          time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC),
	}
}

func TestRoundTripEveryField(t *testing.T) {
	t.Setenv("FINALSHELLY_DIR", t.TempDir())
	h := fullHost()
	v := reflect.ValueOf(h).Elem()
	for i := range v.NumField() {
		if f := v.Type().Field(i); f.IsExported() && v.Field(i).IsZero() {
			t.Errorf("fullHost leaves %s unset", f.Name)
		}
	}
	s := NewStore()
	s.Folders = []*Folder{{ID: "f1", Name: "Prod"}, {ID: "f2", Name: "DB", Parent: "f1"}}
	s.Add(h)
	s.Add(&Host{Name: "plain", Addr: "example.com", User: "root", Auth: AuthPassword})
	if err := SaveHosts(s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("loaded\n%+v\nwant\n%+v", got, s)
	}
	if got.Hosts[1].ID == "" || !got.Hosts[1].MonitorEnabled() {
		t.Errorf("plain host %+v", got.Hosts[1])
	}
	// A plain host writes none of the new keys.
	data, _ := json.Marshal(got.Hosts[1])
	var keys map[string]any
	json.Unmarshal(data, &keys)
	for _, k := range []string{"folder", "notes", "noMonitor", "lastConnectedAt", "deletedAt", "termType", "backspace", "proxy", "tunnels", "noRememberPassword", "createdAt"} {
		if _, ok := keys[k]; ok {
			t.Errorf("plain host has %q: %s", k, data)
		}
	}
}

func TestUnknownKeysKept(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FINALSHELLY_DIR", dir)
	newer, err := os.ReadFile("testdata/hosts_newer.json")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "hosts.json")
	os.WriteFile(path, newer, 0o600)
	s, err := LoadHosts()
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 3 || len(s.Hosts) != 1 || s.Hosts[0].Folder != "f1" || s.Folder("f1").Name != "Prod" {
		t.Fatalf("loaded %+v", s)
	}
	// Edit known fields, as the app does, and save.
	s.Hosts[0].Name = "web-1"
	s.Folders[0].Name = "Production"
	s.MarkConnected("h1", time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC))
	if _, err := os.Stat(filepath.Join(dir, V1BackupName)); !os.IsNotExist(err) {
		t.Errorf("a version 2 file was backed up")
	}
	if err := SaveHosts(s); err != nil {
		t.Fatal(err)
	}

	var before, after map[string]any
	json.Unmarshal(newer, &before)
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatalf("saved %s: %v", data, err)
	}
	same := func(what string, a, b any) {
		t.Helper()
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: %v, want %v", what, b, a)
		}
	}
	same("version", before["version"], after["version"])
	same("theme", before["theme"], after["theme"])
	folderBefore := before["folders"].([]any)[0].(map[string]any)
	folderAfter := after["folders"].([]any)[0].(map[string]any)
	same("folder color", folderBefore["color"], folderAfter["color"])
	same("folder name", "Production", folderAfter["name"])
	hostBefore := before["hosts"].([]any)[0].(map[string]any)
	hostAfter := after["hosts"].([]any)[0].(map[string]any)
	same("proxy", hostBefore["proxy"], hostAfter["proxy"])
	same("tunnels", hostBefore["tunnels"], hostAfter["tunnels"])
	same("host name", "web-1", hostAfter["name"])
	same("lastConnectedAt", "2026-11-07T10:00:00Z", hostAfter["lastConnectedAt"])

	// And again: what was kept is kept on the next load and save.
	again, err := LoadHosts()
	if err != nil || !reflect.DeepEqual(again, s) {
		t.Fatalf("reloaded %+v (%v)", again, err)
	}

}

// A copy keeps every field and unknown key, the proxy's and the tunnels'
// too, and shares none of them.
func TestHostClone(t *testing.T) {
	var h Host
	err := json.Unmarshal([]byte(`{"id": "h1", "addr": "a", "color": "red",
		"proxy": {"kind": "socks5", "addr": "p", "chain": [1]},
		"tunnels": [{"kind": "local", "listenPort": 8080, "targetHost": "t", "targetPort": 80, "note": "admin"}, {"kind": "dynamic", "listenPort": 1080}]}`), &h)
	if err != nil {
		t.Fatal(err)
	}
	c := h.Clone()
	if c == &h || !reflect.DeepEqual(*c, h) || len(c.extra) == 0 || len(c.Proxy.extra) == 0 || len(c.Tunnels) != 2 || len(c.Tunnels[0].extra) == 0 {
		t.Fatalf("clone %+v", c)
	}
	c.extra["color"] = []byte(`"blue"`)
	c.Proxy.Addr = "changed"
	c.Proxy.extra["chain"] = []byte(`[]`)
	c.Tunnels[0].ListenPort = 9
	c.Tunnels[0].extra["note"] = []byte(`"other"`)
	c.Tunnels = append(c.Tunnels[:1], Tunnel{Kind: TunnelLocal})
	if string(h.extra["color"]) != `"red"` || h.Proxy.Addr != "p" || string(h.Proxy.extra["chain"]) != `[1]` ||
		h.Tunnels[0].ListenPort != 8080 || string(h.Tunnels[0].extra["note"]) != `"admin"` || h.Tunnels[1].Kind != TunnelDynamic {
		t.Errorf("the clone shares with the host: %+v", h)
	}
	if (&Host{}).Clone().Tunnels != nil {
		t.Error("a host without tunnels copied to an empty list")
	}
}

// A known key in another case is the known field, not an extra one.
func TestKnownKeysAnyCase(t *testing.T) {
	var h Host
	if err := json.Unmarshal([]byte(`{"ID": "h1", "Addr": "x", "extraKey": 1}`), &h); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(h)
	var keys map[string]any
	json.Unmarshal(data, &keys)
	if h.ID != "h1" || keys["ID"] != nil || keys["Addr"] != nil || keys["extraKey"] != 1.0 {
		t.Errorf("encoded %s", data)
	}
}

func ids[T any](items []T, id func(T) string) []string {
	var out []string
	for _, it := range items {
		out = append(out, id(it))
	}
	return out
}

func hostIDs(hosts []*Host) []string { return ids(hosts, func(h *Host) string { return h.ID }) }
func folderIDs(folders []*Folder) []string {
	return ids(folders, func(f *Folder) string { return f.ID })
}

func TestRecentAndDelete(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 11, 7, 10, min, 0, 0, time.UTC) }
	s := NewStore()
	for _, id := range []string{"a", "b", "c", "d"} {
		s.Add(&Host{ID: id, Addr: id + ".example.com"})
	}
	if s.Host("b") != s.Hosts[1] || s.Host("zz") != nil {
		t.Errorf("Host lookup")
	}
	s.MarkConnected("a", at(1))
	s.MarkConnected("c", at(3))
	s.MarkConnected("b", at(2))
	if s.MarkConnected("zz", at(4)) {
		t.Errorf("marked a missing host")
	}
	if got := hostIDs(s.Recent(0)); !slices.Equal(got, []string{"c", "b", "a"}) {
		t.Errorf("recent %v", got)
	}
	if got := hostIDs(s.Recent(2)); !slices.Equal(got, []string{"c", "b"}) {
		t.Errorf("recent 2 %v", got)
	}

	if !s.SoftDelete("c", at(10)) || !s.SoftDelete("d", at(11)) || s.SoftDelete("zz", at(12)) {
		t.Fatal("soft delete")
	}
	s.SoftDelete("c", at(20)) // already deleted: keeps its time
	if !s.Host("c").DeletedAt.Equal(at(10)) {
		t.Errorf("deleted again at %v", s.Host("c").DeletedAt)
	}
	if got := hostIDs(s.Live()); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("live %v", got)
	}
	if got := hostIDs(s.Deleted()); !slices.Equal(got, []string{"d", "c"}) {
		t.Errorf("deleted %v", got)
	}
	if got := hostIDs(s.Recent(0)); !slices.Equal(got, []string{"b", "a"}) {
		t.Errorf("recent without deleted %v", got)
	}

	if !s.Restore("c") || s.Host("c").Deleted() || s.Restore("zz") {
		t.Error("restore")
	}
	if got := hostIDs(s.Recent(1)); !slices.Equal(got, []string{"c"}) {
		t.Errorf("restored host not recent: %v", got)
	}

	if !s.Remove("d") || s.Remove("d") || s.Host("d") != nil || len(s.Hosts) != 3 {
		t.Errorf("remove: %v", hostIDs(s.Hosts))
	}

	s.ClearRecents()
	if len(s.Recent(0)) != 0 || len(s.Live()) != 3 {
		t.Errorf("after clearing: recent %v live %v", hostIDs(s.Recent(0)), hostIDs(s.Live()))
	}
}

func TestFolders(t *testing.T) {
	s := NewStore()
	prod := s.AddFolder("Prod", "")
	db := s.AddFolder("DB", prod.ID)
	s.Folders = append(s.Folders,
		&Folder{ID: "orphan", Name: "Orphan", Parent: "gone"},
		&Folder{ID: "x", Name: "X", Parent: "y"},
		&Folder{ID: "y", Name: "Y", Parent: "x"},
	)
	if prod.ID == "" || prod.ID == db.ID || s.Folder(db.ID) != db || s.Folder("") != nil || s.Folder("gone") != nil {
		t.Fatalf("folders %v", folderIDs(s.Folders))
	}
	s.Add(&Host{ID: "top"})
	s.Add(&Host{ID: "lost", Folder: "gone"})
	s.Add(&Host{ID: "web", Folder: prod.ID})
	s.Add(&Host{ID: "pg", Folder: db.ID})
	s.Add(&Host{ID: "old", Folder: db.ID, DeletedAt: time.Now()})

	if got := folderIDs(s.Subfolders("")); !slices.Equal(got, []string{prod.ID, "orphan", "x", "y"}) {
		t.Errorf("top folders %v", got)
	}
	if got := folderIDs(s.Subfolders(prod.ID)); !slices.Equal(got, []string{db.ID}) {
		t.Errorf("in prod %v", got)
	}
	if got := hostIDs(s.HostsIn("")); !slices.Equal(got, []string{"top", "lost"}) {
		t.Errorf("top hosts %v", got)
	}
	if got := hostIDs(s.HostsIn(db.ID)); !slices.Equal(got, []string{"pg"}) {
		t.Errorf("in db %v", got)
	}
	if got := folderIDs(s.FolderPath(db.ID)); !slices.Equal(got, []string{prod.ID, db.ID}) {
		t.Errorf("path %v", got)
	}
	if got := folderIDs(s.FolderPath("x")); !slices.Equal(got, []string{"x"}) {
		t.Errorf("path in a cycle %v", got)
	}
	if s.FolderPath("gone") != nil {
		t.Errorf("path of a missing folder")
	}

	if !s.RemoveFolder(prod.ID) || s.RemoveFolder(prod.ID) {
		t.Fatal("remove folder")
	}
	if db.Parent != "" || s.Host("web").Folder != "" || s.Host("pg").Folder != db.ID {
		t.Errorf("after removing prod: db in %q, web in %q", db.Parent, s.Host("web").Folder)
	}
	s.RemoveFolder(db.ID)
	if s.Host("old").Folder != "" || s.Host("pg").Folder != "" {
		t.Errorf("hosts of a removed folder not moved up")
	}
}
