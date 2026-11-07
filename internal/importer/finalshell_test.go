package importer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
)

// The samples in testdata are made up, in the shape other importers found
// FinalShell's files in: documentation addresses (192.0.2.x, example.com),
// and passwords that are base64 of "sample-not-a-real-secret".

// byName is the entry named name, failing the test without one.
func byName(t *testing.T, src *Source, name string) *Entry {
	t.Helper()
	for _, e := range src.Entries {
		if e.Host.Name == name || name == "" && e.Host.Name == "" {
			return e
		}
	}
	t.Fatalf("no entry %q", name)
	return nil
}

func TestReadFinalShell(t *testing.T) {
	src, err := Read(filepath.Join("testdata", "finalshell"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("testdata", "finalshell", "conn"); src.Conn != want {
		t.Errorf("conn %q, want %q", src.Conn, want)
	}
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	// In folder order, then by name; the top level first.
	want := []string{"|192.0.2.40", "|web-1", "|web-1 again", "Production|bastion", "Production / Databases|pg-main", "实验室 / Pi|树莓派"}
	if !slices.Equal(got, want) {
		t.Fatalf("entries %q, want %q", got, want)
	}

	// Password auth, the notes, monitoring on.
	web := byName(t, src, "web-1")
	if h := web.Host; h.Addr != "192.0.2.10" || h.Port != 0 || h.User != "root" || h.Auth != config.AuthPassword ||
		h.Notes != "front end\nrack 4" || h.NoMonitor || h.ID != "" || h.Folder != "" || web.NeedsKey || web.Proxy || web.File != "1001_connect_config.json" {
		t.Errorf("web-1 %+v %+v", web, h)
	}
	// Key auth, a port as a string, a proxy not imported.
	bastion := byName(t, src, "bastion")
	if h := bastion.Host; h.Addr != "bastion.example.com" || h.Port != 2222 || h.User != "deploy" || h.Auth != config.AuthKey ||
		h.KeyPath != "" || !bastion.NeedsKey || !bastion.Proxy || bastion.File != "f1prod/1002_connect_config.json" {
		t.Errorf("bastion %+v %+v", bastion, h)
	}
	// Nested folders, the exec channel off.
	pg := byName(t, src, "pg-main")
	if h := pg.Host; h.Port != 5022 || !h.NoMonitor || h.Notes != "primary" || !slices.Equal(pg.Folder, []string{"Production", "Databases"}) {
		t.Errorf("pg-main %+v %+v", pg, h)
	}
	// Folders nested by parent_id alone, in directories side by side.
	if pi := byName(t, src, "树莓派"); !slices.Equal(pi.Folder, []string{"实验室", "Pi"}) || pi.Host.Auth != config.AuthPassword {
		t.Errorf("pi %+v", pi)
	}
	// Missing fields: no name, port 22, no user, a password.
	bare := byName(t, src, "")
	if h := bare.Host; h.Addr != "192.0.2.40" || h.PortOr22() != 22 || h.User != "" || h.Auth != config.AuthPassword || bare.Folder != nil {
		t.Errorf("bare %+v %+v", bare, h)
	}

	problems := map[string]Reason{}
	for _, p := range src.Problems {
		if _, dup := problems[p.File]; dup {
			t.Errorf("%s twice", p.File)
		}
		problems[p.File] = p.Reason
		if p.Reason == Broken && p.Detail == "" {
			t.Errorf("%s broken without a detail", p.File)
		}
	}
	wantProblems := map[string]Reason{
		"1005_connect_config.json": Broken,
		"1006_connect_config.json": NotSSH,
		"1007_connect_config.json": DeletedThere,
		"1008_connect_config.json": NoHost,
		"1011_connect_config.json": Broken,
	}
	if !maps(problems, wantProblems) {
		t.Errorf("problems %v, want %v", problems, wantProblems)
	}
	if !Broken.Failed() || !NoHost.Failed() || NotSSH.Failed() || DeletedThere.Failed() {
		t.Error("which reasons fail")
	}

	// Nothing of the passwords, nor of the keys in config.json, is kept.
	data, err := json.Marshal(src.Entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"c2FtcGxl", "RkFLRS", "sample", "id_ed25519"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("%q in what was read: %s", secret, data)
		}
	}
}

func maps(a, b map[string]Reason) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// The conn folder itself, and a folder FinalShell exported, read too.
func TestReadConnAndExport(t *testing.T) {
	src, err := Read(filepath.Join("testdata", "finalshell", "conn"))
	if err != nil || len(src.Entries) != 6 {
		t.Fatalf("conn: %v, %v", src, err)
	}
	src, err = Read(filepath.Join("testdata", "export"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range src.Entries {
		got = append(got, e.FolderLabel()+"|"+e.Host.Title())
	}
	// No folder.json: the directories are the folders, as parent_id names
	// folders of the machine it was exported on.
	if want := []string{"Staging|stage", "Staging / Web|stage-web"}; !slices.Equal(got, want) || len(src.Problems) != 0 {
		t.Errorf("export %q, problems %v; want %q", got, src.Problems, want)
	}
}

func TestReadFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.json"), []byte("{}"), 0o600)
	if _, err := Read(dir); !errors.Is(err, ErrNoConnections) {
		t.Errorf("a folder without connections: %v", err)
	}
	if _, err := Read(filepath.Join(dir, "missing")); err == nil || errors.Is(err, ErrNoConnections) {
		t.Errorf("a missing folder: %v", err)
	}
	// A large file is not read.
	big := filepath.Join(dir, "big_connect_config.json")
	os.WriteFile(big, []byte(`{"host":"192.0.2.1","name":"`+strings.Repeat("x", maxFileSize)+`"}`), 0o600)
	src, err := Read(dir)
	if err != nil || len(src.Entries) != 0 || len(src.Problems) != 1 || src.Problems[0].Reason != TooLarge {
		t.Errorf("a large file: %+v, %v", src, err)
	}
}

func TestCandidatesAndFind(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	got := candidates("darwin", env(map[string]string{"HOME": "/Users/ann"}))
	if want := []string{filepath.Join("/Users/ann", "Library", "FinalShell"), filepath.Join("/Users/ann", "Library", "Application Support", "finalshell")}; !slices.Equal(got, want) {
		t.Errorf("darwin %q", got)
	}
	got = candidates("linux", env(map[string]string{"HOME": "/home/ann"}))
	if want := []string{filepath.Join("/home/ann", ".finalshell"), filepath.Join("/home/ann", ".local", "share", "finalshell")}; !slices.Equal(got, want) {
		t.Errorf("linux %q", got)
	}
	got = candidates("windows", env(map[string]string{"USERPROFILE": `C:\Users\ann`, "APPDATA": `C:\Users\ann\AppData\Roaming`, "ProgramFiles": `C:\Program Files`, "SystemDrive": "C:"}))
	if len(got) != 4 || !strings.Contains(got[0], "Local") || !strings.HasSuffix(got[0], "finalshell") || !strings.Contains(got[1], "Roaming") || !strings.Contains(got[2], "Program Files") {
		t.Errorf("windows %q", got)
	}
	if got := candidates("linux", env(nil)); len(got) != 0 {
		t.Errorf("no home %q", got)
	}

	// Find takes the first candidate with a conn folder.
	none, data := t.TempDir(), filepath.Join("testdata", "finalshell")
	old := Candidates
	t.Cleanup(func() { Candidates = old })
	Candidates = func() []string { return []string{none, data} }
	if got := Find(); got != data {
		t.Errorf("found %q", got)
	}
	Candidates = func() []string { return []string{none} }
	if got := Find(); got != "" {
		t.Errorf("found %q in none", got)
	}
}

func TestImport(t *testing.T) {
	src, err := Read(filepath.Join("testdata", "finalshell"))
	if err != nil {
		t.Fatal(err)
	}
	s := config.NewStore()
	// A folder of the same name is used; a deleted host is no duplicate.
	prod := s.AddFolder("Production", "")
	s.Add(&config.Host{Name: "mine", Addr: "DB.example.com", Port: 5022, User: "postgres", Auth: config.AuthKey})
	s.Add(&config.Host{Name: "gone", Addr: "192.0.2.20", User: "pi", DeletedAt: time.Now()})
	before := len(s.Hosts)

	pg := byName(t, src, "pg-main")
	if Present(s, pg.Host) == nil || Present(s, byName(t, src, "树莓派").Host) != nil {
		t.Error("present")
	}
	at := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	added := Import(s, src.Entries, at)
	var names []string
	for _, h := range added {
		names = append(names, h.Title())
		if h.ID == "" || s.Host(h.ID) != h {
			t.Errorf("%s: id %q", h.Title(), h.ID)
		}
		// Created now, for the start page to list them as new.
		if !h.CreatedAt.Equal(at) || !h.LastConnectedAt.IsZero() {
			t.Errorf("%s: created %v, connected %v", h.Title(), h.CreatedAt, h.LastConnectedAt)
		}
	}
	if !s.Hosts[0].CreatedAt.IsZero() {
		t.Error("a host there before has a creation time")
	}
	// pg-main was there, and web-1 again is web-1.
	if want := []string{"192.0.2.40", "web-1", "bastion", "树莓派"}; !slices.Equal(names, want) || len(s.Hosts) != before+4 {
		t.Fatalf("added %q, want %q", names, want)
	}
	// The entries are not the hosts added.
	if src.Entries[1].Host == added[1] || src.Entries[1].Host.ID != "" {
		t.Error("the entry's host was added")
	}
	folderOf := func(name string) string {
		for _, h := range added {
			if h.Title() == name {
				var path []string
				for _, f := range s.FolderPath(h.Folder) {
					path = append(path, f.Name)
				}
				return strings.Join(path, "/")
			}
		}
		return "?"
	}
	if folderOf("bastion") != "Production" || s.Host(added[2].ID).Folder != prod.ID {
		t.Errorf("bastion in %q", folderOf("bastion"))
	}
	if folderOf("树莓派") != "实验室/Pi" || folderOf("web-1") != "" {
		t.Errorf("pi in %q, web-1 in %q", folderOf("树莓派"), folderOf("web-1"))
	}
	// Folders made once each, and only for hosts added: Databases holds
	// only pg-main, which was there.
	if len(s.Folders) != 3 {
		t.Errorf("folders %d", len(s.Folders))
	}

	// Importing again adds nothing.
	if again := Import(s, src.Entries, at.Add(time.Hour)); len(again) != 0 {
		t.Errorf("again %d", len(again))
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "c2FtcGxl") || strings.Contains(string(data), "password\":\"") {
		t.Errorf("a password saved: %s", data)
	}
}

func TestSameTarget(t *testing.T) {
	a := &config.Host{Addr: "Host.Example.com ", User: "root"}
	for _, c := range []struct {
		b    config.Host
		same bool
	}{
		{config.Host{Addr: "host.example.com", Port: 22, User: "root"}, true},
		{config.Host{Addr: "host.example.com", Port: 2222, User: "root"}, false},
		{config.Host{Addr: "host.example.com", User: "Root"}, false},
		{config.Host{Addr: "other.example.com", User: "root"}, false},
	} {
		if SameTarget(a, &c.b) != c.same {
			t.Errorf("%+v: %v", c.b, !c.same)
		}
	}
}
