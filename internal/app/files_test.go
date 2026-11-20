package app

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/sshtest"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// newSearchFixture is a session whose SFTP serves a folder with files and
// a subfolder, with the main window drawn.
func newSearchFixture(t *testing.T) (*App, *fakePlatform, *ui.Tester, *Session) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string]string{
		"deploy.sh":   "#!/bin/sh\n",
		"app.yaml":    "server:\n  port: 8080\n",
		"big.log":     "a log\n",
		"src/main.go": "package main\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv := sshtest.StartSFTP(t, dir)
	a, p := testApp(t)
	h := &config.Host{ID: "files", Name: "web-1", Addr: srv.Host, Port: srv.Port, User: "tester", Auth: config.AuthPassword, NoMonitor: true}
	a.hosts.Add(h)
	a.passwords[h.ID] = sshtest.Password
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openSession(h)
	s := a.current()
	// The SFTP server names the folder in its own way, as /C:/... on
	// Windows, so it is the session that says where it is.
	p.until(t, "the files", func() bool {
		if s.hostKeyQ != nil {
			s.hostKeyQ.Answer <- true
			s.hostKeyQ = nil
		}
		return s.status == statusFailed || s.status == statusConnected && s.files.cwd != "" && !s.files.loading
	})
	if s.status != statusConnected {
		t.Fatal(describeError(s.err))
	}
	t.Cleanup(a.CloseAll)
	return a, p, tt, s
}

// The table's search filters the folder's files and folders by a part of
// their names, without case, and another folder starts without it.
func TestFileSearch(t *testing.T) {
	a, p, tt, s := newSearchFixture(t)

	names := func() []string {
		var out []string
		for _, e := range s.files.shown {
			out = append(out, e.Name)
		}
		return out
	}
	all := names()
	if !slices.Contains(all, "app.yaml") || !slices.Contains(all, "src") {
		t.Fatalf("the folder: %q", all)
	}

	// A part of a name matches, wherever it is, without case.
	tt.Click(locales.SearchFiles())
	tt.Type("APP")
	p.until(t, "the filtered rows", func() bool {
		return slices.Equal(names(), []string{"app.yaml"})
	})
	saveShot(t, tt, "file-search-en.png")

	// Folders are searched too.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("SRC")
	p.until(t, "the folder's row", func() bool {
		return slices.Equal(names(), []string{"src"})
	})

	// Nothing matching shows no rows.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("nothing")
	p.until(t, "no rows", func() bool {
		return len(names()) == 0
	})

	// Escape clears the field and every row comes back.
	tt.Key(0, ui.KeyEscape)
	p.until(t, "every row", func() bool {
		return s.files.search == "" && slices.Equal(names(), all)
	})

	// Another folder starts with no search.
	tt.Click(locales.SearchFiles())
	tt.Type("APP")
	p.until(t, "the filtered rows", func() bool {
		return slices.Equal(names(), []string{"app.yaml"})
	})
	src := path.Join(s.files.cwd, "src")
	a.openDir(s, src)
	p.until(t, "the subfolder", func() bool {
		return s.files.cwd == src && !s.files.loading
	})
	if s.files.search != "" || s.files.filter != "" || !slices.Equal(names(), []string{"main.go"}) {
		t.Fatalf("the next folder: search %q, filter %q, rows %q", s.files.search, s.files.filter, names())
	}

	// The search field in Chinese.
	p.SetLocale("zh-Hans")
	tt.Frame()
	tt.Click(locales.SearchFiles())
	tt.Type("MAIN")
	p.until(t, "the Chinese rows", func() bool {
		return slices.Equal(names(), []string{"main.go"})
	})
	saveShot(t, tt, "file-search-zh.png")
}
