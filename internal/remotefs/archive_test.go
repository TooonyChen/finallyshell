package remotefs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/sshtest"

	"golang.org/x/crypto/ssh"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":      "'plain'",
		"it's":       `'it'\''s'`,
		"":           "''",
		"''":         `''\'''\'''`,
		"$HOME *":    "'$HOME *'",
		"a\nb":       "'a\nb'",
		`back\slash`: `'back\slash'`,
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s, want %s", in, got, want)
		}
	}
	// What sh makes of them: the same words back.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for _, in := range []string{"it's", "''", "$(touch pwned)", "`id`", "a b\tc\nd", `"q" \x`, "-n", "'; exit 1; '", "中文 名字"} {
		out, err := exec.Command(sh, "-c", "printf %s "+ShellQuote(in)).Output()
		if err != nil || string(out) != in {
			t.Errorf("sh printed %q for %q (%v)", out, in, err)
		}
	}
}

func TestScripts(t *testing.T) {
	if got, want := downloadScript("/srv/my app", []string{"it's", "-rf"}), `cd '/srv/my app' && exec tar -czf - -- 'it'\''s' '-rf'`; got != want {
		t.Errorf("download script %s", got)
	}
	if got, want := uploadScript("/tmp/x y"), `mkdir -p -- '/tmp/x y' && cd '/tmp/x y' && exec tar -xzof -`; got != want {
		t.Errorf("upload script %s", got)
	}
	if got := dirArg("-odd"); got != "'./-odd'" {
		t.Errorf("dirArg %s", got)
	}
	if _, _, err := splitRemote([]string{"/a/b", "/c/d"}); err == nil {
		t.Error("paths in two folders split")
	}
	if dir, names, err := splitRemote([]string{"/a/b", "/a/c"}); err != nil || dir != "/a" || !slices.Equal(names, []string{"b", "c"}) {
		t.Errorf("split %q %q %v", dir, names, err)
	}
}

// makeTree writes a folder with nested folders, an empty one, modes,
// links inside it and many small files under root/proj, and a file
// root/single.txt.
func makeTree(t *testing.T, root string) []string {
	t.Helper()
	proj := filepath.Join(root, "proj")
	writeFile(t, filepath.Join(proj, "a.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(proj, "deep", "x", "y", "z.txt"), []byte("zed"))
	writeFile(t, filepath.Join(proj, "bin", "run.sh"), []byte("#!/bin/sh\necho hi\n"))
	os.Chmod(filepath.Join(proj, "bin", "run.sh"), 0o755)
	writeFile(t, filepath.Join(proj, "secret"), []byte("key"))
	os.Chmod(filepath.Join(proj, "secret"), 0o600)
	writeFile(t, filepath.Join(proj, "big.bin"), randomBytes(300_000))
	writeFile(t, filepath.Join(proj, "名字 with 'quote'.txt"), []byte("unicode"))
	for i := range 120 {
		writeFile(t, filepath.Join(proj, "many", fmt.Sprintf("d%d", i%7), fmt.Sprintf("f%03d.txt", i)), []byte(strings.Repeat("x", i)))
	}
	os.MkdirAll(filepath.Join(proj, "empty"), 0o755)
	if runtime.GOOS != "windows" {
		os.Symlink("a.txt", filepath.Join(proj, "link"))
		os.Symlink("deep/x", filepath.Join(proj, "dirlink"))
	}
	writeFile(t, filepath.Join(root, "single.txt"), []byte("one file"))
	return []string{proj, filepath.Join(root, "single.txt")}
}

// treeOf describes each item under dir: its type, mode, content or
// link target.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	items := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			items[rel] = "link " + target
		case fi.IsDir():
			items[rel] = "dir"
		default:
			data, _ := os.ReadFile(p)
			mode := fi.Mode().Perm()
			if runtime.GOOS == "windows" {
				mode = 0
			}
			items[rel] = fmt.Sprintf("file %o %s %d", mode, data, fi.ModTime().Unix())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestArchiveRoundTrip(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	paths := makeTree(t, src)
	var buf bytes.Buffer
	var packed int64
	files := map[string]bool{}
	err := WriteArchive(context.Background(), &buf, paths, func(done int64, file string) { packed = done; files[file] = true })
	if err != nil {
		t.Fatal(err)
	}
	size, err := LocalArchiveSize(paths)
	if err != nil || packed != size || size < 300_000 {
		t.Errorf("packed %d bytes, measured %d (%v)", packed, size, err)
	}
	if !files["proj/deep/x/y/z.txt"] || !files["single.txt"] {
		t.Errorf("progress named %d files", len(files))
	}
	var unpacked int64
	if err := ExtractArchive(context.Background(), &buf, dst, []string{"proj", "single.txt"}, func(done int64, _ string) { unpacked = done }); err != nil {
		t.Fatal(err)
	}
	if unpacked != size {
		t.Errorf("unpacked %d bytes of %d", unpacked, size)
	}
	want, got := treeOf(t, src), treeOf(t, dst)
	delete(want, ".")
	delete(got, ".")
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("extra %s", k)
		}
	}
	if got["proj/empty"] != "dir" {
		t.Errorf("the empty folder: %q", got["proj/empty"])
	}

	// Again over what is there: tar replaces it.
	os.WriteFile(filepath.Join(src, "proj", "a.txt"), []byte("changed"), 0o644)
	buf.Reset()
	if err := WriteArchive(context.Background(), &buf, paths[:1], nil); err != nil {
		t.Fatal(err)
	}
	if err := ExtractArchive(context.Background(), &buf, dst, []string{"proj"}, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "proj", "a.txt")); string(data) != "changed" {
		t.Errorf("replaced with %q", data)
	}
}

// entry is a made-up archive entry.
type entry struct {
	name string
	typ  byte
	mode int64
	body string
	link string
}

func makeArchive(t *testing.T, entries ...entry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Size: int64(len(e.body)), Linkname: e.link, ModTime: time.Unix(1_700_000_000, 0)}
		if e.typ != tar.TypeReg {
			hdr.Size = 0
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	zw.Close()
	return &buf
}

func TestExtractRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"../evil", "/abs/evil", "proj/../../evil", "proj/../proj/x", "other/file", "..", "."} {
		parent := t.TempDir()
		dst := filepath.Join(parent, "dst")
		os.Mkdir(dst, 0o755)
		ar := makeArchive(t, entry{name: "proj/", typ: tar.TypeDir, mode: 0o755}, entry{name: name, typ: tar.TypeReg, body: "pwned"})
		err := ExtractArchive(context.Background(), ar, dst, []string{"proj"}, nil)
		var unsafe *UnsafeEntryError
		if !errors.As(err, &unsafe) {
			t.Errorf("%s: %v", name, err)
		}
		for _, p := range []string{filepath.Join(parent, "evil"), filepath.Join(dst, "evil"), filepath.Join(dst, "other")} {
			if _, err := os.Lstat(p); err == nil {
				t.Errorf("%s: wrote %s", name, p)
			}
		}
	}
	// A hard link to a file outside.
	dst := t.TempDir()
	ar := makeArchive(t, entry{name: "proj/", typ: tar.TypeDir}, entry{name: "proj/h", typ: tar.TypeLink, link: "../../etc/passwd"})
	var unsafe *UnsafeEntryError
	if err := ExtractArchive(context.Background(), ar, dst, []string{"proj"}, nil); !errors.As(err, &unsafe) {
		t.Errorf("hard link outside: %v", err)
	}
}

func TestExtractNeverFollowsLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "target.txt"), []byte("keep"))
	dst := t.TempDir()
	// The user's own links in the destination: a folder of the archive's
	// name, and a file in it.
	os.Symlink(outside, filepath.Join(dst, "proj"))
	ar := makeArchive(t,
		entry{name: "proj/", typ: tar.TypeDir, mode: 0o755},
		// An absolute link and one that climbs out are not made...
		entry{name: "proj/abs", typ: tar.TypeSymlink, link: outside},
		entry{name: "proj/climb", typ: tar.TypeSymlink, link: "../../x"},
		// ...so these go into real folders of their names.
		entry{name: "proj/abs/target.txt", typ: tar.TypeReg, body: "pwned"},
		// A link inside is made, then replaced by a folder where the
		// archive writes through it.
		entry{name: "proj/up", typ: tar.TypeSymlink, link: ".."},
		entry{name: "proj/up/evil", typ: tar.TypeReg, body: "pwned"},
		entry{name: "proj/ok", typ: tar.TypeSymlink, link: "abs/target.txt"},
	)
	err := ExtractArchive(context.Background(), ar, dst, []string{"proj"}, nil)
	var skipped *SkippedLinksError
	if !errors.As(err, &skipped) || !slices.Equal(skipped.Names, []string{"proj/abs", "proj/climb"}) {
		t.Fatalf("err %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(outside, "target.txt")); string(data) != "keep" {
		t.Errorf("wrote through a link: %q", data)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 {
		t.Errorf("outside has %d items", len(entries))
	}
	if _, err := os.Lstat(filepath.Join(dst, "evil")); err == nil {
		t.Error("wrote through proj/up")
	}
	for _, d := range []string{"proj", "proj/abs", "proj/up"} {
		if fi, err := os.Lstat(filepath.Join(dst, d)); err != nil || !fi.IsDir() {
			t.Errorf("%s is not a real folder: %v %v", d, fi, err)
		}
	}
	if target, _ := os.Readlink(filepath.Join(dst, "proj", "ok")); target != "abs/target.txt" {
		t.Errorf("link inside: %q", target)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "proj", "up", "evil")); string(data) != "pwned" {
		t.Errorf("proj/up/evil: %q", data)
	}

	// A file over the user's link replaces the link, not what it points to.
	dst2 := t.TempDir()
	os.Mkdir(filepath.Join(dst2, "proj"), 0o755)
	os.Symlink(filepath.Join(outside, "target.txt"), filepath.Join(dst2, "proj", "t"))
	ar = makeArchive(t, entry{name: "proj/t", typ: tar.TypeReg, body: "new"})
	if err := ExtractArchive(context.Background(), ar, dst2, []string{"proj"}, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(outside, "target.txt")); string(data) != "keep" {
		t.Errorf("wrote through the user's link: %q", data)
	}
	if fi, _ := os.Lstat(filepath.Join(dst2, "proj", "t")); fi == nil || !fi.Mode().IsRegular() {
		t.Errorf("proj/t: %v", fi)
	}
}

func TestExtractModesAndHardLinks(t *testing.T) {
	dst := t.TempDir()
	ar := makeArchive(t,
		entry{name: "proj/", typ: tar.TypeDir, mode: 0o2775},
		entry{name: "proj/suid", typ: tar.TypeReg, mode: 0o4755, body: "#!/bin/sh"},
		entry{name: "proj/ro", typ: tar.TypeReg, mode: 0o444, body: "read only"},
		entry{name: "proj/hard", typ: tar.TypeLink, link: "proj/ro"},
	)
	if err := ExtractArchive(context.Background(), ar, dst, []string{"proj"}, nil); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dst, "proj", "suid"))
	if fi == nil || fi.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 || runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("suid: %v", fi.Mode())
	}
	if fi, _ := os.Stat(filepath.Join(dst, "proj")); fi.Mode()&fs.ModeSetgid != 0 {
		t.Errorf("folder: %v", fi.Mode())
	}
	if fi, _ := os.Stat(filepath.Join(dst, "proj", "ro")); runtime.GOOS != "windows" && fi.Mode().Perm()&0o600 != 0o600 {
		t.Errorf("read only file: %v", fi.Mode())
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "proj", "hard")); string(data) != "read only" {
		t.Errorf("hard link: %q", data)
	}
	if fi, _ := os.Stat(filepath.Join(dst, "proj", "ro")); !fi.ModTime().Equal(time.Unix(1_700_000_000, 0)) {
		t.Errorf("mtime %v", fi.ModTime())
	}
}

func TestExtractWindowsNames(t *testing.T) {
	for name, ok := range map[string]bool{
		"a.txt": true, "console": true, "COM0": true, "con.d": false, "名字": true,
		"a:b": false, "q?": false, "dot.": false, "space ": false, "CON": false, "nul.txt": false, "Com1": false, "LPT9.log": false, "tab\t": false, "": false,
	} {
		if WindowsNameOK(name) != ok {
			t.Errorf("WindowsNameOK(%q) = %v", name, !ok)
		}
	}
	defer func(old bool) { windowsNames = old }(windowsNames)
	windowsNames = true
	ar := makeArchive(t, entry{name: "proj/", typ: tar.TypeDir}, entry{name: "proj/a:b.txt", typ: tar.TypeReg, body: "x"})
	err := ExtractArchive(context.Background(), ar, t.TempDir(), []string{"proj"}, nil)
	var bad *WindowsNameError
	if !errors.As(err, &bad) || bad.Name != "proj/a:b.txt" {
		t.Errorf("err %v", err)
	}
}

func TestArchiveCancel(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "proj", "big.bin"), randomBytes(4<<20))
	writeFile(t, filepath.Join(src, "proj", "z-after.txt"), []byte("after"))
	paths := []string{filepath.Join(src, "proj")}

	ctx, cancel := context.WithCancel(context.Background())
	err := WriteArchive(ctx, io.Discard, paths, func(done int64, _ string) {
		if done > 1<<20 {
			cancel()
		}
	})
	if !IsCanceled(err) {
		t.Errorf("packing: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteArchive(context.Background(), &buf, paths, nil); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	ctx, cancel = context.WithCancel(context.Background())
	err = ExtractArchive(ctx, &buf, dst, []string{"proj"}, func(done int64, _ string) {
		if done > 1<<20 {
			cancel()
		}
	})
	if !IsCanceled(err) {
		t.Errorf("unpacking: %v", err)
	}
	// The part of the file is removed, and nothing after it is made.
	for _, n := range []string{"big.bin", "z-after.txt"} {
		if _, err := os.Lstat(filepath.Join(dst, "proj", n)); err == nil {
			t.Errorf("%s is there", n)
		}
	}
}

func TestExtractBrokenStream(t *testing.T) {
	ar := makeArchive(t, entry{name: "proj/a", typ: tar.TypeReg, body: strings.Repeat("a", 10_000)})
	cut := bytes.NewReader(ar.Bytes()[:ar.Len()/2])
	var se *streamError
	if err := ExtractArchive(context.Background(), cut, t.TempDir(), []string{"proj"}, nil); !errors.As(err, &se) {
		t.Errorf("cut stream: %v", err)
	}
	if err := ExtractArchive(context.Background(), strings.NewReader(""), t.TempDir(), []string{"proj"}, nil); !errors.As(err, &se) {
		t.Errorf("empty stream: %v", err)
	}
}

func TestRemoteArchiveSize(t *testing.T) {
	c := newTestClient(t)
	src := t.TempDir()
	paths := makeTree(t, src)
	want, err := LocalArchiveSize(paths)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RemoteArchiveSize(context.Background(), c, paths)
	if err != nil || got != want {
		t.Errorf("remote %d, local %d (%v)", got, want, err)
	}
	if _, err := RemoteArchiveSize(context.Background(), c, []string{filepath.Join(src, "missing")}); err == nil {
		t.Error("no error for a missing path")
	}
}

// The scripts with the system's tar, in place of the server's: what
// WriteArchive packs, tar unpacks, and what tar packs, ExtractArchive
// unpacks.
func TestScriptsWithLocalTar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sh")
	}
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("no tar")
	}
	src, remote, back := t.TempDir(), filepath.Join(t.TempDir(), "made by mkdir", "it's"), t.TempDir()
	paths := makeTree(t, src)

	up := exec.Command("sh", "-c", uploadScript(remote))
	stdin, _ := up.StdinPipe()
	var stderr bytes.Buffer
	up.Stderr = &stderr
	if err := up.Start(); err != nil {
		t.Fatal(err)
	}
	if err := WriteArchive(context.Background(), stdin, paths, nil); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	if err := up.Wait(); err != nil {
		t.Fatalf("tar -x: %v %s", err, stderr.Bytes())
	}

	down := exec.Command("sh", "-c", downloadScript(remote, []string{"proj", "single.txt"}))
	stdout, _ := down.StdoutPipe()
	down.Stderr = &stderr
	if err := down.Start(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractArchive(context.Background(), stdout, back, []string{"proj", "single.txt"}, nil); err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, stdout)
	if err := down.Wait(); err != nil {
		t.Fatalf("tar -c: %v %s", err, stderr.Bytes())
	}
	want, got := treeOf(t, src), treeOf(t, back)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q", k, got[k], v)
		}
	}
}

// Without exec on the server, or without a connection, an archive
// transfer fails at once.
func TestArchiveWithoutExec(t *testing.T) {
	if _, err := DownloadArchive(context.Background(), nil, []string{"/a/b"}, t.TempDir(), nil); !errors.Is(err, ErrNoShell) {
		t.Errorf("no client: %v", err)
	}
	client := sshtest.Start(t).Dial(t)
	err := UploadArchive(context.Background(), client, []string{t.TempDir()}, "/tmp", nil)
	if err == nil || !strings.Contains(err.Error(), "cannot run tar") {
		t.Errorf("exec refused: %v", err)
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{max: 40}
	fmt.Fprintf(b, "first line that is long enough to go\n\n")
	fmt.Fprintf(b, "tar: a: Cannot stat\ntar: exiting\n")
	if got := b.lines(2); got != "tar: a: Cannot stat\ntar: exiting" {
		t.Errorf("%q", got)
	}
}

// TestArchiveLive runs archive transfers against a real server with tar,
// named as user:password@host:port by FINALLYSHELL_TEST_SSH, in a
// folder of its own under the user's home.
func TestArchiveLive(t *testing.T) {
	spec := os.Getenv("FINALLYSHELL_TEST_SSH")
	if spec == "" {
		t.Skip("FINALLYSHELL_TEST_SSH not set")
	}
	cred, addr, _ := strings.Cut(spec, "@")
	user, pass, _ := strings.Cut(cred, ":")
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.Password(pass)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	sc, err := NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sc.Close() })
	home, err := sc.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	remote := path.Join(home, fmt.Sprintf("fs-archive-test-%d", time.Now().UnixNano()), "it's here")
	t.Cleanup(func() {
		if err := RemoveAll(context.Background(), sc, path.Dir(remote)); err != nil {
			t.Errorf("cleaning up: %v", err)
		}
	})

	src, back := t.TempDir(), t.TempDir()
	paths := makeTree(t, src)
	if err := UploadArchive(context.Background(), client, paths, remote, nil); err != nil {
		t.Fatal(err)
	}
	if target, err := sc.ReadLink(path.Join(remote, "proj", "link")); err != nil || target != "a.txt" {
		t.Errorf("link on the server: %q %v", target, err)
	}
	if fi, err := sc.Stat(path.Join(remote, "proj", "bin", "run.sh")); err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh on the server: %v %v", fi, err)
	}
	want, err := LocalArchiveSize(paths)
	if err != nil {
		t.Fatal(err)
	}
	remotePaths := []string{path.Join(remote, "proj"), path.Join(remote, "single.txt")}
	if got, err := RemoteArchiveSize(context.Background(), sc, remotePaths); err != nil || got != want {
		t.Errorf("remote size %d, local %d (%v)", got, want, err)
	}
	var done int64
	local, err := DownloadArchive(context.Background(), client, remotePaths, back, func(n int64, _ string) { done = n })
	if err != nil {
		t.Fatal(err)
	}
	if local != filepath.Join(back, "proj") || done != want {
		t.Errorf("local %s, %d bytes of %d", local, done, want)
	}
	wantTree, gotTree := treeOf(t, src), treeOf(t, back)
	for k, v := range wantTree {
		if gotTree[k] != v {
			t.Errorf("%s: %q, want %q", k, gotTree[k], v)
		}
	}

	// tar fails on a missing name, and says so.
	_, err = DownloadArchive(context.Background(), client, []string{path.Join(remote, "missing")}, t.TempDir(), nil)
	var te *TarError
	if !errors.As(err, &te) || te.Status == 0 || !strings.Contains(te.Stderr, "missing") {
		t.Errorf("missing: %v", err)
	}
	t.Logf("missing: %v", err)
	// A folder that cannot be made.
	err = UploadArchive(context.Background(), client, paths[:1], "/proc/no-such/dir", nil)
	if !errors.As(err, &te) || !strings.Contains(te.Stderr, "mkdir") {
		t.Errorf("mkdir: %v", err)
	}
	t.Logf("mkdir: %v", err)

	// Canceling stops both ways mid-stream.
	writeFile(t, filepath.Join(src, "huge", "big.bin"), randomBytes(24<<20))
	ctx, cancel := context.WithCancel(context.Background())
	err = UploadArchive(ctx, client, []string{filepath.Join(src, "huge")}, remote, func(n int64, _ string) {
		if n > 2<<20 {
			cancel()
		}
	})
	if !IsCanceled(err) {
		t.Errorf("upload: %v", err)
	}
	if err := Upload(context.Background(), sc, filepath.Join(src, "huge"), remote, nil); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	ctx, cancel = context.WithCancel(context.Background())
	_, err = DownloadArchive(ctx, client, []string{path.Join(remote, "huge")}, dst, func(n int64, _ string) {
		if n > 2<<20 {
			cancel()
		}
	})
	if !IsCanceled(err) {
		t.Errorf("download: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "huge", "big.bin")); err == nil {
		t.Error("the part of big.bin is left")
	}
	// The connection is still good for the next one.
	if _, err := DownloadArchive(context.Background(), client, []string{path.Join(remote, "single.txt")}, t.TempDir(), nil); err != nil {
		t.Errorf("after cancel: %v", err)
	}
}
