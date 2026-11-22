package importer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
)

// sshEntry is an entry as the tests compare them.
type sshEntry struct {
	file, name, addr, user string
	port                   int
	auth                   config.AuthKind
	key                    string
	note                   Note
	jump                   string // the name of the jump entry's host
}

func sshEntries(t *testing.T, src *Source) []sshEntry {
	t.Helper()
	var out []sshEntry
	for _, e := range src.Entries {
		h := e.Host
		s := sshEntry{e.File, h.Name, h.Addr, h.User, h.Port, h.Auth, e.KeyFile, e.Note, ""}
		if e.Jump != nil {
			s.jump = e.Jump.Host.Name
		}
		if e.Folder != nil || h.Notes != "" || e.NeedsKey || e.Password != "" || h.ID != "" {
			t.Errorf("%s has a folder, notes, a password or an ID: %+v", h.Name, e)
		}
		out = append(out, s)
	}
	return out
}

func TestReadSSHConfig(t *testing.T) {
	path := filepath.Join("testdata", "sshconfig", "config")
	src, err := readSSHConfig(path, "me")
	if err != nil {
		t.Fatal(err)
	}
	if src.Kind != SSHConfig || src.Path != path {
		t.Errorf("source %s %s", src.Kind, src.Path)
	}
	pw, key := config.AuthPassword, config.AuthKey
	want := []sshEntry{
		// HostName with %h, the same block's first name; the later Host db
		// has its User, but its HostName comes after the first.
		{"config:15", "app", "192.0.2.20", "deploy", 0, key, "~/.ssh/keys/my key", "", "bastion"},
		// Not under "Host * !bastion": the local user.
		{"config:33", "bad-port", "192.0.2.32", "deploy", 0, pw, "", "", ""},
		{"config:10", "bastion", "bastion.example.com", "me", 2222, key, "~/.ssh/id_bastion", "", ""},
		{"config:25", "chain", "192.0.2.31", "deploy", 0, pw, "", "", "bastion"},
		{"config:7", "db", "db.example.com", "postgres", 0, pw, "", "", ""},
		{"config.d/lab.conf:1", "lab", "192.0.2.40", "deploy", 0, key, "~/.ssh/lab_%C", SSHKeyTokens, ""},
		{"config:21", "legacy", "192.0.2.30", "deploy", 0, pw, "", JumpHostNotImported, ""},
		{"config:29", "old-proxy", "192.0.2.33", "deploy", 0, pw, "", ProxyNotImported, ""},
		{"config:7", "web", "web.example.com", "deploy", 0, pw, "", "", ""},
		{"config.d/work.conf:1", "work-1", "work-1.corp.example.com", "alice", 2022, key, "~/.ssh/alice-work-1.corp.example.com", "", ""},
		{"config.d/work.conf:1", "work-2", "work-2.corp.example.com", "alice", 2022, key, "~/.ssh/alice-work-2.corp.example.com", "", ""},
	}
	if got := sshEntries(t, src); !slices.Equal(got, want) {
		t.Errorf("entries\n%+v\nwant\n%+v", got, want)
	}
	problems := []Problem{
		{File: "config:35", Reason: SSHBadPort, Detail: "port twenty-two"},
		{File: "config.d/work.conf:5", Reason: SSHPatternsOnly},
		{File: "config:37", Reason: SSHMatch},
		{File: "config:45", Reason: SSHPatternsOnly},
	}
	if !slices.Equal(src.Problems, problems) {
		t.Errorf("problems\n%+v\nwant\n%+v", src.Problems, problems)
	}
	if !SSHBadPort.Failed() || SSHMatch.Failed() || SSHPatternsOnly.Failed() || !SSHBadLine.Failed() {
		t.Error("failed or skipped")
	}
	if r := ReaderOf(SSHConfig); r == nil || r.Folder || r.Find == nil || len(r.Extensions) != 0 {
		t.Errorf("reader %+v", r)
	}
	if i := slices.Index(Readers(), ReaderOf(SSHConfig)); i < 1 {
		t.Errorf("listed at %d", i)
	}
}

// writeFiles writes files, by their names in dir, and returns dir.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readSSHFiles(t *testing.T, files map[string]string) *Source {
	t.Helper()
	src, err := readSSHConfig(filepath.Join(writeFiles(t, files), "config"), "me")
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// The settings of a host come from the first line that applies; an
// Include in a Host block applies under it; Match blocks are skipped up
// to the next Host.
func TestSSHConfigBlocks(t *testing.T) {
	src := readSSHFiles(t, map[string]string{
		"config": "User early\n" +
			"Host a b\n" +
			"  Include inc/a.conf\n" +
			"Host c\n" +
			"  HostName 192.0.2.3\n" +
			"Match all\n" +
			"  HostName 192.0.2.99\n" +
			"  Include inc/c.conf\n" +
			"Host C\n" +
			"  Port 2003\n" +
			"Host 'd e' \"f\"\n" +
			"  HostName \"192.0.2.4\"\n" +
			"Host = g\n" +
			"  HostName = 192.0.2.7 # comment\n" +
			"  User = \"x y\" z\n" +
			"Host ?\n" +
			"  Port 2000\n",
		// Under Host a b: its Host line is only for b.
		"inc/a.conf": "Port 2001\nHost b\nUser bee\n",
		"inc/c.conf": "Host c\nUser never\n",
	})
	got := map[string]string{}
	for _, e := range src.Entries {
		h := e.Host
		got[h.Name] = h.Addr + " " + h.User + " " + strconv.Itoa(h.Port)
	}
	want := map[string]string{
		"a":   "a early 2001",
		"b":   "b early 2001", // User early comes first
		"c":   "192.0.2.3 early 2003",
		"d e": "192.0.2.4 early 0",
		"f":   "192.0.2.4 early 2000", // one character: Host ?
		"g":   "192.0.2.7 early 2000",
	}
	if len(got) != len(want) {
		t.Errorf("hosts %q", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	if len(src.Problems) != 2 || src.Problems[0] != (Problem{File: "config:6", Reason: SSHMatch}) ||
		src.Problems[1] != (Problem{File: "config:16", Reason: SSHPatternsOnly}) {
		t.Errorf("problems %+v", src.Problems)
	}
	// No User at all: the local user's name.
	src = readSSHFiles(t, map[string]string{"config": "Host x\n  User \"\"\n"})
	if src.Entries[0].Host.User != "me" {
		t.Errorf("user %q", src.Entries[0].Host.User)
	}
}

func TestSSHConfigProblems(t *testing.T) {
	src := readSSHFiles(t, map[string]string{
		"config": "Host ok\n" +
			"  IdentityFile \"~/.ssh/not closed\n" +
			"  Port 0\n" +
			"  Port 65536\n" +
			"Host\n" +
			"Include folder\n" +
			"Include loop\n" +
			"Include missing nothing-*\n" +
			"Include [\n",
		"folder/x": "",
		"loop":     "Include loop\n",
	})
	if len(src.Entries) != 1 || src.Entries[0].KeyFile != "" || src.Entries[0].Host.Port != 0 {
		t.Errorf("entries %+v", src.Entries)
	}
	var got []string
	for _, p := range src.Problems {
		got = append(got, p.File+" "+string(p.Reason))
	}
	want := []string{
		"config:2 ssh-line", "config:3 ssh-port", "config:4 ssh-port", "config:5 ssh-line",
		"folder unreadable", "loop:1 ssh-include-limit", "config:9 ssh-line",
	}
	if !slices.Equal(got, want) {
		t.Errorf("problems %q, want %q", got, want)
	}
	if !strings.Contains(src.Problems[0].Detail, "quote") {
		t.Errorf("detail %q", src.Problems[0].Detail)
	}

	// Too many files: one includes itself twice, said once.
	src = readSSHFiles(t, map[string]string{"config": "Host x\nInclude twice\n", "twice": "Include twice twice\n"})
	if len(src.Entries) != 1 || !slices.Equal(src.Problems, []Problem{{File: "twice:1", Reason: SSHIncludeLimit}}) {
		t.Errorf("entries %d, problems %+v", len(src.Entries), src.Problems)
	}

	// No host, or no file.
	dir := writeFiles(t, map[string]string{"config": "Host *\n  User x\n", "empty": ""})
	for _, name := range []string{"config", "empty"} {
		if _, err := readSSHConfig(filepath.Join(dir, name), "me"); !errors.Is(err, ErrNoConnections) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ReadSSHConfig(filepath.Join(dir, "nothing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	if _, err := ReadSSHConfig(dir); err == nil {
		t.Error("read a folder")
	}
}

func TestSSHFields(t *testing.T) {
	for _, c := range []struct {
		line string
		key  string
		args []string
		err  bool
	}{
		{"", "", nil, false},
		{"   # comment", "", nil, false},
		{"Host web db", "host", []string{"web", "db"}, false},
		{"\tHostName\t192.0.2.1\r", "hostname", []string{"192.0.2.1"}, false},
		{"Port=22", "port", []string{"22"}, false},
		{"Port = 22", "port", []string{"22"}, false},
		{"User =x", "user", []string{"x"}, false},
		{`IdentityFile "~/my key" 'b c'`, "identityfile", []string{"~/my key", "b c"}, false},
		{`IdentityFile "a \" b"`, "identityfile", []string{`a " b`}, false},
		{`IdentityFile a\ b`, "identityfile", []string{"a b"}, false},
		{"HostName x#y # z", "hostname", []string{"x#y"}, false},
		{`IdentityFile ""`, "identityfile", []string{""}, false},
		{"Compression", "compression", nil, false},
		{`User "x`, "", nil, true},
	} {
		key, args, err := sshFields(c.line)
		if key != c.key || !slices.Equal(args, c.args) || (err != nil) != c.err {
			t.Errorf("%q: %q %q %v", c.line, key, args, err)
		}
	}
}

func TestSSHMatchHost(t *testing.T) {
	for _, c := range []struct {
		name     string
		patterns []string
		want     bool
	}{
		{"web", []string{"web"}, true},
		{"Web", []string{"wEb"}, true},
		{"web", []string{"db", "web"}, true},
		{"web", []string{"*"}, true},
		{"web", []string{"w*"}, true},
		{"web", []string{"*b"}, true},
		{"web", []string{"*e*"}, true},
		{"web", []string{"w?b"}, true},
		{"web", []string{"w?"}, false},
		{"web-1.example.com", []string{"*.example.com"}, true},
		{"example.com", []string{"*.example.com"}, false},
		{"aaa", []string{"a*a*a"}, true},
		{"aab", []string{"a*a*a"}, false},
		{"web", []string{"* !web"}, false},
		{"web", []string{"*", "!web"}, false},
		{"db", []string{"*", "!web"}, true},
		{"web", []string{"!db"}, false}, // a negation alone matches nothing
		{"web", nil, false},
		{"服务器", []string{"服务?"}, true},
	} {
		if got := sshMatchHost(c.name, c.patterns); got != c.want {
			t.Errorf("%q %q: %v", c.name, c.patterns, got)
		}
	}
}

func TestSSHExpand(t *testing.T) {
	tokens := map[byte]string{'h': "web.example.com", 'r': "ops", 'u': ""}
	for _, c := range []struct {
		in, want string
		ok       bool
	}{
		{"~/.ssh/id", "~/.ssh/id", true},
		{"~/.ssh/%r@%h", "~/.ssh/ops@web.example.com", true},
		{"100%%", "100%", true},
		{"%u", "%u", false}, // not known
		{"a_%C", "a_%C", false},
		{"%", "%", false},
	} {
		if got, ok := sshExpand(c.in, tokens); got != c.want || ok != c.ok {
			t.Errorf("%q: %q %v", c.in, got, ok)
		}
	}
}

func TestFindSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := FindSSHConfig(); got != "" {
		t.Errorf("found %q", got)
	}
	os.MkdirAll(filepath.Join(home, ".ssh", "config"), 0o700) // a folder
	if got := FindSSHConfig(); got != "" {
		t.Errorf("found the folder %q", got)
	}
	os.Remove(filepath.Join(home, ".ssh", "config"))
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := FindSSHConfig(); got != filepath.Join(home, ".ssh", "config") {
		t.Errorf("found %q", got)
	}
}

// Importing an SSH config's hosts makes the jump host of each the host of
// its Jump: the one imported, or the one present already; with neither,
// the host connects directly.
func TestImportJumpHosts(t *testing.T) {
	src, err := readSSHConfig(filepath.Join("testdata", "sshconfig", "config"), "me")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*Entry{}
	for _, e := range src.Entries {
		byName[e.Host.Name] = e
	}
	s := &config.Store{}
	added := Import(s, []*Entry{byName["app"], byName["bastion"], byName["legacy"]}, time.Now())
	if len(added) != 3 {
		t.Fatalf("added %d", len(added))
	}
	app, bastion, legacy := added[0], added[1], added[2]
	if p := config.ProxyOf(app); p == nil || p.Kind != config.ProxyJump || p.JumpHost != bastion.ID {
		t.Errorf("app's proxy %+v", app.Proxy)
	}
	if app.Auth != config.AuthKey || app.KeyPath != "~/.ssh/keys/my key" {
		t.Errorf("app %+v", app)
	}
	if bastion.Proxy != nil || legacy.Proxy != nil {
		t.Errorf("proxies %+v %+v", bastion.Proxy, legacy.Proxy)
	}
	if jumps, err := s.JumpHosts(app); err != nil || len(jumps) != 1 || jumps[0] != bastion {
		t.Errorf("jumps %v %v", jumps, err)
	}
	// bastion present already: chain goes through it.
	added = Import(s, []*Entry{byName["chain"], byName["bastion"]}, time.Now())
	if len(added) != 1 || config.ProxyOf(added[0]) == nil || added[0].Proxy.JumpHost != bastion.ID {
		t.Errorf("chain %+v", added)
	}
	// Neither imported nor present: directly.
	s = &config.Store{}
	added = Import(s, []*Entry{byName["app"]}, time.Now())
	if len(added) != 1 || added[0].Proxy != nil {
		t.Errorf("app alone %+v", added[0].Proxy)
	}
	// A jump to itself, as through another name of the same target, is
	// not one.
	e := &Entry{Host: &config.Host{Name: "x", Addr: "192.0.2.1", User: "u"}}
	e.Jump = &Entry{Host: &config.Host{Name: "y", Addr: "192.0.2.1", User: "u"}}
	if added := Import(&config.Store{}, []*Entry{e}, time.Now()); added[0].Proxy != nil {
		t.Errorf("self %+v", added[0].Proxy)
	}
}
