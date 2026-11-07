package remotefs

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

func TestReadText(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	small := filepath.Join(dir, "deploy.sh")
	writeFile(t, small, []byte("#!/bin/sh\necho hi\n"))
	data, fi, err := ReadText(context.Background(), c, small, nil)
	if err != nil || string(data) != "#!/bin/sh\necho hi\n" || fi.Size() != int64(len(data)) {
		t.Fatalf("read %q, %v, err %v", data, fi, err)
	}

	// A big one, past the first chunk, with progress.
	big := filepath.Join(dir, "big.log")
	text := bytes.Repeat([]byte("2026-11-07 12:00:00 INFO a line of the log\n"), 50_000)
	writeFile(t, big, text)
	var last int64
	data, _, err = ReadText(context.Background(), c, big, func(done int64) { last = done })
	if err != nil || !bytes.Equal(data, text) || last != int64(len(text)) {
		t.Fatalf("big: %d bytes, progress %d, err %v", len(data), last, err)
	}

	// An empty one.
	empty := filepath.Join(dir, "empty")
	writeFile(t, empty, nil)
	if data, _, err := ReadText(context.Background(), c, empty, nil); err != nil || len(data) != 0 {
		t.Fatalf("empty: %q %v", data, err)
	}

	// A binary one stops at its first chunk.
	bin := filepath.Join(dir, "a.out")
	writeFile(t, bin, append([]byte("\x7fELF\x02\x01\x01\x00"), randomBytes(100_000)...))
	if _, _, err := ReadText(context.Background(), c, bin, nil); !errors.Is(err, ErrBinary) {
		t.Fatalf("binary: %v", err)
	}

	// Canceled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadText(ctx, c, big, nil); !IsCanceled(err) {
		t.Fatalf("canceled: %v", err)
	}
	if _, _, err := ReadText(context.Background(), c, dir, nil); err == nil {
		t.Fatal("read a directory")
	}
}

// limitedFS takes parts of an SFTP client away, as servers and
// permissions do.
type limitedFS struct {
	FS
	noPosixRename  bool  // the server lacks the extension
	posixRenameErr error // the extension fails
	createErr      error // making the temporary file fails
	chownErr       error
	renames        []string
}

func (l *limitedFS) HasExtension(name string) (string, bool) {
	if l.noPosixRename && name == "posix-rename@openssh.com" {
		return "", false
	}
	return l.FS.HasExtension(name)
}

func (l *limitedFS) PosixRename(o, n string) error {
	l.renames = append(l.renames, "posix")
	if l.posixRenameErr != nil {
		return l.posixRenameErr
	}
	return l.FS.PosixRename(o, n)
}

func (l *limitedFS) Rename(o, n string) error {
	l.renames = append(l.renames, "rename")
	return l.FS.Rename(o, n)
}

func (l *limitedFS) OpenFile(p string, flags int) (*sftp.File, error) {
	if l.createErr != nil && flags&os.O_EXCL != 0 {
		return nil, l.createErr
	}
	return l.FS.OpenFile(p, flags)
}

func (l *limitedFS) Chown(p string, uid, gid int) error {
	if l.chownErr != nil {
		return l.chownErr
	}
	return l.FS.Chown(p, uid, gid)
}

// noTemp fails when dir has a file the save left behind.
func noTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".finallyshell-") {
			t.Errorf("left %s behind", e.Name())
		}
	}
}

func TestSaveTextReplaces(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "app.yaml")
	writeFile(t, p, []byte("port: 80\n"))
	os.Chmod(p, 0o640)
	before, _ := os.Stat(p)

	method, fi, err := SaveText(c, p, []byte("port: 8080\n"))
	if err != nil || method != SavedReplaced {
		t.Fatalf("method %v err %v", method, err)
	}
	if data, _ := os.ReadFile(p); string(data) != "port: 8080\n" {
		t.Errorf("saved %q", data)
	}
	after, _ := os.Stat(p)
	if after.Mode() != 0o640 || fi.Mode() != 0o640 || fi.Size() != 11 {
		t.Errorf("mode %v, info %v %d", after.Mode(), fi.Mode(), fi.Size())
	}
	if os.SameFile(before, after) {
		t.Error("not a new file")
	}
	noTemp(t, dir)

	// The set-ID bits stay too.
	tool := filepath.Join(dir, "tool.sh")
	writeFile(t, tool, []byte("#!/bin/sh\n"))
	os.Chmod(tool, 0o755|fs.ModeSetuid|fs.ModeSetgid)
	if _, fi, err := SaveText(c, tool, []byte("#!/bin/sh\nid\n")); err != nil || fi.Mode() != 0o755|fs.ModeSetuid|fs.ModeSetgid {
		t.Errorf("set-ID: %v %v", fi.Mode(), err)
	}

	// Without posix-rename, and with one that fails: remove, then rename.
	for _, l := range []*limitedFS{{FS: c, noPosixRename: true}, {FS: c, posixRenameErr: errors.New("unsupported")}} {
		method, _, err := SaveText(l, p, []byte("port: 9090\n"))
		if data, _ := os.ReadFile(p); err != nil || method != SavedReplaced || string(data) != "port: 9090\n" || l.renames[len(l.renames)-1] != "rename" {
			t.Errorf("fallback rename: %v %v %q %v", method, err, data, l.renames)
		}
		if st, _ := os.Stat(p); st.Mode() != 0o640 {
			t.Errorf("mode %v", st.Mode())
		}
		noTemp(t, dir)
	}
}

func TestSaveTextInPlace(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()

	// A link: written through, the link stays a link.
	target := filepath.Join(dir, "real.conf")
	writeFile(t, target, []byte("a=1\n"))
	os.Chmod(target, 0o600)
	link := filepath.Join(dir, "link.conf")
	os.Symlink("real.conf", link)
	method, fi, err := SaveText(c, link, []byte("a=2\n"))
	if err != nil || method != SavedInPlace || fi.Size() != 4 {
		t.Fatalf("link: %v %v %v", method, fi, err)
	}
	if lfi, _ := os.Lstat(link); lfi.Mode()&fs.ModeSymlink == 0 {
		t.Error("the link was replaced")
	}
	if data, _ := os.ReadFile(target); string(data) != "a=2\n" {
		t.Errorf("target %q", data)
	}
	if st, _ := os.Stat(target); st.Mode() != 0o600 {
		t.Errorf("target mode %v", st.Mode())
	}

	// No room for a file in the directory, as without write permission
	// on it (the tests may run as root, which has it anyway).
	p := filepath.Join(dir, "hosts")
	writeFile(t, p, []byte("127.0.0.1 localhost\n"))
	os.Chmod(p, 0o644)
	before, _ := os.Stat(p)
	l := &limitedFS{FS: c, createErr: fs.ErrPermission}
	method, _, err = SaveText(l, p, []byte("127.0.0.1 localhost box\n"))
	after, _ := os.Stat(p)
	if data, _ := os.ReadFile(p); err != nil || method != SavedInPlace || string(data) != "127.0.0.1 localhost box\n" || !os.SameFile(before, after) {
		t.Errorf("unwritable directory: %v %v %q", method, err, data)
	}

	// Any other failure to make the file leaves the old one alone.
	l = &limitedFS{FS: c, createErr: errors.New("no space left")}
	if _, _, err := SaveText(l, p, []byte("gone\n")); err == nil {
		t.Error("saved without room")
	}
	if data, _ := os.ReadFile(p); string(data) != "127.0.0.1 localhost box\n" {
		t.Errorf("the failed save changed it: %q", data)
	}

	// Another user's file, which the new file cannot be given to.
	other := filepath.Join(dir, "other.txt")
	writeFile(t, other, []byte("x\n"))
	if err := os.Chown(other, 4321, 4321); err != nil {
		t.Skip("cannot give a file away:", err)
	}
	ownerOf := func() (uint32, uint32) {
		fi, err := c.Stat(other)
		if err != nil {
			t.Fatal(err)
		}
		uid, gid, _ := owner(fi)
		return uid, gid
	}
	l = &limitedFS{FS: c, chownErr: fs.ErrPermission}
	method, _, err = SaveText(l, other, []byte("y\n"))
	if uid, gid := ownerOf(); err != nil || method != SavedInPlace || uid != 4321 || gid != 4321 {
		t.Errorf("other's file: %v %v owner %d:%d", method, err, uid, gid)
	}
	// Root can give it: then it is replaced, keeping the owner.
	method, _, err = SaveText(c, other, []byte("z\n"))
	if uid, gid := ownerOf(); err != nil || method != SavedReplaced || uid != 4321 || gid != 4321 {
		t.Errorf("chown: %v %v owner %d:%d", method, err, uid, gid)
	}
	noTemp(t, dir)

	// A file removed since it was opened is made again.
	gone := filepath.Join(dir, "gone.txt")
	if _, _, err := SaveText(c, gone, []byte("back\n")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(gone); string(data) != "back\n" {
		t.Errorf("gone: %q", data)
	}
}
