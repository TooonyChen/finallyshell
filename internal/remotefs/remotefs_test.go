package remotefs

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/pkg/sftp"
)

// newTestClient serves the local file system over SFTP in the process.
func newTestClient(t *testing.T) *sftp.Client {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	server, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{sr, sw})
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve()
	c, err := sftp.NewClientPipe(cr, cw, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Hang up both ways first: each side waits for the other otherwise.
		cw.Close()
		sw.Close()
		c.Close()
	})
	return c
}

func writeFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func TestUploadDownloadDirectory(t *testing.T) {
	c := newTestClient(t)
	local, remote, back := t.TempDir(), t.TempDir(), t.TempDir()
	big := randomBytes(3 << 20)
	writeFile(t, filepath.Join(local, "site", "index.html"), []byte("<h1>hi</h1>"))
	writeFile(t, filepath.Join(local, "site", "assets", "app.bin"), big)
	os.MkdirAll(filepath.Join(local, "site", "empty"), 0o755)

	size, err := LocalSize(filepath.Join(local, "site"))
	if err != nil || size != int64(len(big))+11 {
		t.Fatalf("local size %d %v", size, err)
	}
	var last int64
	if err := Upload(context.Background(), c, filepath.Join(local, "site"), remote, func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	if last != size {
		t.Errorf("upload progress ended at %d of %d", last, size)
	}
	got, _ := os.ReadFile(filepath.Join(remote, "site", "assets", "app.bin"))
	if !bytes.Equal(got, big) {
		t.Errorf("uploaded file differs (%d bytes)", len(got))
	}
	if fi, err := os.Stat(filepath.Join(remote, "site", "empty")); err != nil || !fi.IsDir() {
		t.Errorf("empty directory not uploaded: %v", err)
	}

	rsize, err := RemoteSize(c, filepath.Join(remote, "site"))
	if err != nil || rsize != size {
		t.Fatalf("remote size %d %v", rsize, err)
	}
	dst, err := Download(context.Background(), c, filepath.Join(remote, "site"), back, func(n int64) { last = n })
	if err != nil {
		t.Fatal(err)
	}
	if dst != filepath.Join(back, "site") || last != size {
		t.Errorf("download to %s, progress %d of %d", dst, last, size)
	}
	got, _ = os.ReadFile(filepath.Join(back, "site", "assets", "app.bin"))
	if !bytes.Equal(got, big) {
		t.Errorf("downloaded file differs (%d bytes)", len(got))
	}

	entries, err := ReadDir(c, filepath.Join(remote, "site"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if len(names) != 3 || names[0] != "assets" || names[1] != "empty" || names[2] != "index.html" {
		t.Errorf("listing %v: directories first, then files", names)
	}
	if found := RemoteExisting(c, filepath.Join(remote, "site"), []string{"index.html", "nope"}); len(found) != 1 {
		t.Errorf("existing %v", found)
	}
}

func TestCancel(t *testing.T) {
	c := newTestClient(t)
	local, remote := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(local, "big.bin"), randomBytes(8<<20))

	ctx, cancel := context.WithCancel(context.Background())
	err := Upload(ctx, c, filepath.Join(local, "big.bin"), remote, func(n int64) {
		if n > 1<<20 {
			cancel()
		}
	})
	if !IsCanceled(err) {
		t.Fatalf("upload: %v, want canceled", err)
	}
	if _, err := os.Stat(filepath.Join(remote, "big.bin")); !os.IsNotExist(err) {
		t.Errorf("partial upload left behind: %v", err)
	}

	writeFile(t, filepath.Join(remote, "big.bin"), randomBytes(8<<20))
	ctx, cancel = context.WithCancel(context.Background())
	back := t.TempDir()
	_, err = Download(ctx, c, filepath.Join(remote, "big.bin"), back, func(n int64) {
		if n > 1<<20 {
			cancel()
		}
	})
	if !IsCanceled(err) {
		t.Fatalf("download: %v, want canceled", err)
	}
	if _, err := os.Stat(filepath.Join(back, "big.bin")); !os.IsNotExist(err) {
		t.Errorf("partial download left behind: %v", err)
	}
}

func TestIsInside(t *testing.T) {
	dir := filepath.FromSlash("/tmp/x")
	for p, want := range map[string]bool{"/tmp/x/a": true, "/tmp/x/a/b": true, "/tmp/x/../y": false, "/tmp/y": false, "/tmp/x/..a": true} {
		if got := isInside(dir, filepath.FromSlash(p)); got != want {
			t.Errorf("isInside(%s) = %v", p, got)
		}
	}
}

func TestReadDirLinks(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.conf"), []byte("hello, world"))
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"conf":    "real.conf",
		"abs":     filepath.Join(dir, "real.conf"),
		"subdir":  "sub",
		"gone":    "missing.txt",
		"loop":    "loop",
		"subdir2": "./sub/",
	} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ReadDir(c, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	var names []string
	for _, e := range entries {
		got[e.Name] = e
		names = append(names, e.Name)
	}
	// Links to directories sort with the directories.
	if want := []string{"sub", "subdir", "subdir2", "abs", "conf", "gone", "loop", "real.conf"}; !slices.Equal(names, want) {
		t.Errorf("order %q, want %q", names, want)
	}
	for _, tc := range []struct {
		name, target string
		size         int64
		dir, broken  bool
	}{
		{"conf", "real.conf", 12, false, false},
		{"abs", filepath.Join(dir, "real.conf"), 12, false, false},
		{"subdir", "sub", -1, true, false},
		{"subdir2", "./sub/", -1, true, false},
		{"gone", "missing.txt", -1, false, true},
		{"loop", "loop", -1, false, true},
	} {
		e := got[tc.name]
		if !e.Link || e.Target != tc.target || e.IsDir() != tc.dir || e.Broken != tc.broken {
			t.Errorf("%s: %+v", tc.name, e)
		}
		if tc.size >= 0 && e.Size != tc.size {
			t.Errorf("%s: size %d, want the target's %d", tc.name, e.Size, tc.size)
		}
		if tc.dir && e.Mode.Perm() != 0o750 {
			t.Errorf("%s: mode %v, want the directory's", tc.name, e.Mode)
		}
	}
	if e := got["real.conf"]; e.Link || e.Target != "" || e.Broken {
		t.Errorf("a file: %+v", e)
	}
}
