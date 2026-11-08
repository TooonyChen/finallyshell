package remotefs

// Archive transfers. Over SFTP every file costs several round trips
// (open, write or read, close, chmod), so a folder of many small files is
// slow. Packed with tar and gzip, a selection streams over one exec
// channel instead: the server runs tar, this side packs or unpacks the
// stream with archive/tar and compress/gzip, file by file, with no
// temporary archive on either side.

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ShellQuote quotes s as one word for sh: in single quotes, with each '
// written as '\”.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ArchiveProgress is told the bytes of file contents packed or unpacked
// so far, and the path in the archive of the file at work, from the
// transfer's goroutine.
type ArchiveProgress func(done int64, file string)

// ErrNoShell is an archive transfer on a session without an SSH
// connection to run tar over.
var ErrNoShell = errors.New("no SSH connection to run tar over")

// TarError is tar, or the shell running it, failing on the server.
type TarError struct {
	Status int    // the exit status; -1 when it ended without one
	Stderr string // the last lines it printed
}

func (e *TarError) Error() string {
	switch {
	case e.Stderr != "":
		return e.Stderr
	case e.Status >= 0:
		return fmt.Sprintf("tar exited with status %d", e.Status)
	}
	return "tar ended without an exit status"
}

// UnsafeEntryError is an archive entry that could write outside the
// folder it is unpacked into: an absolute name, one with "..", or one
// outside the items asked for.
type UnsafeEntryError struct{ Name string }

func (e *UnsafeEntryError) Error() string {
	return fmt.Sprintf("unsafe name in the archive: %q", e.Name)
}

// WindowsNameError is an entry whose name Windows does not allow. It is
// reported, not renamed.
type WindowsNameError struct{ Name string }

func (e *WindowsNameError) Error() string {
	return fmt.Sprintf("%q is not a valid name on Windows", e.Name)
}

// DamagedArchiveError is an archive whose gzip stream does not check out
// at its end: its checksum or size does not match what was unpacked, so
// the files unpacked from it may be wrong.
type DamagedArchiveError struct{ Err error }

func (e *DamagedArchiveError) Error() string {
	return fmt.Sprintf("the archive arrived damaged (%v); the files unpacked from it may be wrong", e.Err)
}

func (e *DamagedArchiveError) Unwrap() error { return e.Err }

// SkippedLinks are the symbolic links of an unpacked archive that were
// not made, by their names in the archive. The rest was unpacked.
type SkippedLinks struct {
	// Outside point outside the folder, or lead out of it through other
	// links.
	Outside []string
	// NotAllowed the system did not let the user make: Windows without
	// Developer Mode or an administrator.
	NotAllowed []string
}

// Len is the number of links skipped.
func (s SkippedLinks) Len() int { return len(s.Outside) + len(s.NotAllowed) }

var (
	// goos is the system the files are written on.
	goos = runtime.GOOS
	// windowsNames checks names as Windows needs them, on Windows.
	windowsNames = goos == "windows"
	// symlink makes links; tests put another in its place.
	symlink = os.Symlink
)

// errPrivilegeNotHeld is ERROR_PRIVILEGE_NOT_HELD, what Windows says to
// a user who may not make symbolic links.
const errPrivilegeNotHeld = syscall.Errno(1314)

// linkNotAllowed tells whether err is the system refusing the user to
// make a symbolic link at all, so that the link is skipped, not the
// transfer failed.
func linkNotAllowed(goos string, err error) bool {
	return goos == "windows" && errors.Is(err, errPrivilegeNotHeld)
}

// archiveCounter counts the bytes of file contents, and stops when the
// context ends.
type archiveCounter struct {
	ctx      context.Context
	done     int64
	file     string
	progress ArchiveProgress
}

func (c *archiveCounter) add(n int) error {
	c.done += int64(n)
	if c.progress != nil {
		c.progress(c.done, c.file)
	}
	return c.ctx.Err()
}

type archiveReader struct {
	r io.Reader
	c *archiveCounter
}

func (r archiveReader) Read(p []byte) (int, error) {
	if err := r.c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	if cerr := r.c.add(n); cerr != nil && err == nil {
		err = cerr
	}
	return n, err
}

type archiveWriter struct {
	w io.Writer
	c *archiveCounter
}

func (w archiveWriter) Write(p []byte) (int, error) {
	if err := w.c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.w.Write(p)
	if cerr := w.c.add(n); cerr != nil && err == nil {
		err = cerr
	}
	return n, err
}

// WriteArchive packs the local files and folders paths into w as a
// gzip-compressed tar, each under its own name, as tar -czf does in
// their folder: folders with what they hold, symbolic links as links,
// modes and modification times, but not owners, devices or sockets.
func WriteArchive(ctx context.Context, w io.Writer, paths []string, progress ArchiveProgress) error {
	bw := bufio.NewWriterSize(w, 64<<10)
	// The fastest level: on a fast link the compression is what waits.
	zw, err := gzip.NewWriterLevel(bw, gzip.BestSpeed)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	c := &archiveCounter{ctx: ctx, progress: progress}
	for _, p := range paths {
		root := filepath.Base(p)
		err := filepath.WalkDir(p, func(name string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			rel, err := filepath.Rel(p, name)
			if err != nil {
				return err
			}
			return addEntry(tw, name, path.Join(root, filepath.ToSlash(rel)), c)
		})
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return bw.Flush()
}

// addEntry writes the local file name to tw as arcName.
func addEntry(tw *tar.Writer, name, arcName string, c *archiveCounter) error {
	fi, err := os.Lstat(name)
	if err != nil {
		return err
	}
	link := ""
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		if link, err = os.Readlink(name); err != nil {
			return err
		}
		link = filepath.ToSlash(link)
	case fi.IsDir(), fi.Mode().IsRegular():
	default:
		return nil // a device, a socket, a pipe
	}
	hdr, err := tar.FileInfoHeader(fi, link)
	if err != nil {
		return err
	}
	hdr.Name = arcName
	if fi.IsDir() {
		hdr.Name += "/"
	}
	// Local owners mean nothing on the server, and the user's name stays
	// here.
	hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
	hdr.Mode = int64(fi.Mode().Perm())
	hdr.ModTime = fi.ModTime().Truncate(time.Second) // tar's own resolution
	c.file = arcName
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return c.add(0)
	}
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.CopyN(tw, archiveReader{f, c}, hdr.Size); err != nil {
		if err == io.EOF {
			return fmt.Errorf("%s: the file got shorter while it was packed", name)
		}
		return err
	}
	return nil
}

// streamError is the archive stream breaking off or making no sense: on
// a transfer, most likely because tar failed on the server, which then
// says why.
type streamError struct{ err error }

func (e *streamError) Error() string { return e.err.Error() }
func (e *streamError) Unwrap() error { return e.err }

// ExtractArchive unpacks the gzip-compressed tar r into the local folder
// dir, as tar -xzf does, taking only entries under roots, names in dir.
// It never writes through a symbolic link: one where a folder goes is
// replaced by the folder, as tar does. Modes are kept without setuid,
// setgid and sticky bits, and the owner can always read and write.
// Absolute names and names with ".." stop it. Links that point outside
// dir, on their own or through other links, are not made, nor those the
// system does not allow; it returns them.
func ExtractArchive(ctx context.Context, r io.Reader, dir string, roots []string, progress ArchiveProgress) (SkippedLinks, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return SkippedLinks{}, err
	}
	x := &extractor{
		dir:     dir,
		roots:   map[string]bool{},
		safe:    map[string]bool{dir: true},
		written: map[string]bool{},
		c:       &archiveCounter{ctx: ctx, progress: progress},
	}
	for _, n := range roots {
		x.roots[n] = true
	}
	err = x.unpack(r)
	// Links made so far, also when it stopped half way: those that lead
	// out through others only show now.
	if lerr := x.checkLinks(); err == nil {
		err = lerr
	}
	return x.skipped, err
}

func (x *extractor) unpack(r io.Reader) error {
	zr, err := gzip.NewReader(bufio.NewReaderSize(r, 64<<10))
	if err != nil {
		return &streamError{err}
	}
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// The rest of the gzip stream, to its trailer: only reading it
			// checks the CRC and the size of all that was unpacked.
			if _, err := io.Copy(io.Discard, zr); err != nil {
				return &DamagedArchiveError{err}
			}
			return nil
		}
		if err != nil {
			return &streamError{err}
		}
		if err := x.c.ctx.Err(); err != nil {
			return err
		}
		if err := x.entry(hdr, tr); err != nil {
			return err
		}
	}
}

// checkLinks removes the links made that lead outside x.dir on the disk
// as it is now: following the links of their targets, and, for a part
// that is not there, by its name. All are judged before any is removed,
// so that a link to a link that leads out goes too; removing them can
// change where others lead, so it goes round until none is removed.
func (x *extractor) checkLinks() error {
	dir, err := physical(x.dir)
	if err != nil {
		return err
	}
	for {
		var kept, out []madeLink
		for _, l := range x.links {
			fi, err := os.Lstat(l.path)
			if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
				continue // replaced since by a folder or a file
			}
			if to, err := physical(l.path); err == nil && isInside(dir, to) {
				kept = append(kept, l)
			} else {
				out = append(out, l)
			}
		}
		x.links = kept
		if len(out) == 0 {
			return nil
		}
		for _, l := range out {
			if err := os.Remove(l.path); err != nil {
				return err
			}
			x.skipped.Outside = append(x.skipped.Outside, l.name)
		}
	}
}

// maxLinks is how many links physical follows before it gives up, as
// the system does for a loop.
const maxLinks = 255

// physical is where the absolute path p leads on the disk: through every
// link of the part that is there, and by the names of the part that is
// not.
func physical(p string) (string, error) {
	sep := string(filepath.Separator)
	vol := filepath.VolumeName(p)
	done, rest := vol+sep, p[len(vol):]
	links := 0
	for rest != "" {
		rest = strings.TrimLeft(rest, `/`+sep)
		name := rest
		if i := strings.IndexAny(rest, `/`+sep); i >= 0 {
			name, rest = rest[:i], rest[i:]
		} else {
			rest = ""
		}
		switch name {
		case "", ".":
			continue
		case "..":
			done = filepath.Dir(done)
			continue
		}
		next := filepath.Join(done, name)
		fi, err := os.Lstat(next)
		if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			done = next
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("%s: too many links", p)
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
			v := filepath.VolumeName(target)
			done, rest = v+sep, target[len(v):]+sep+rest
		} else {
			rest = target + sep + rest
		}
	}
	return done, nil
}

type extractor struct {
	dir   string
	roots map[string]bool
	// safe are the folders known to be real folders, not links: dir, and
	// those made or checked since.
	safe map[string]bool
	// written are the regular files this extraction wrote, the only ones
	// a hard link may point to: a file that was there before has contents
	// the server never sent.
	written map[string]bool
	links   []madeLink
	skipped SkippedLinks
	c       *archiveCounter
}

// madeLink is a symbolic link the extractor made.
type madeLink struct{ name, path string }

// target checks the archive name of an entry, and returns it cleaned and
// where it goes.
func (x *extractor) target(name string) (string, string, error) {
	clean := path.Clean(name)
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	bad := name == "" || path.IsAbs(name) || clean == "." || !x.roots[strings.Split(clean, "/")[0]] ||
		filepath.VolumeName(filepath.FromSlash(name)) != ""
	for _, p := range parts {
		if p == ".." {
			bad = true
		}
	}
	if bad {
		return "", "", &UnsafeEntryError{name}
	}
	if windowsNames {
		for _, p := range strings.Split(clean, "/") {
			if !WindowsNameOK(p) {
				return "", "", &WindowsNameError{clean}
			}
		}
	}
	abs := filepath.Join(x.dir, filepath.FromSlash(clean))
	if !isInside(x.dir, abs) || abs == x.dir {
		return "", "", &UnsafeEntryError{name}
	}
	return clean, abs, nil
}

func (x *extractor) entry(hdr *tar.Header, r io.Reader) error {
	if hdr.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	clean, abs, err := x.target(hdr.Name)
	if err != nil {
		return err
	}
	x.c.file = clean
	mode := fs.FileMode(hdr.Mode).Perm()
	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := x.ensureDir(abs); err != nil {
			return err
		}
		os.Chmod(abs, mode|0o700) // best effort, as on Windows
		return x.c.add(0)
	case tar.TypeSymlink:
		return x.symlink(clean, abs, hdr.Linkname)
	case tar.TypeLink:
		return x.hardLink(abs, hdr.Linkname)
	}
	if !hdr.FileInfo().Mode().IsRegular() {
		return nil // a device or a pipe
	}
	return x.file(abs, mode|0o600, hdr.ModTime, r)
}

// ensureDir makes d a real folder inside x.dir, and the folders it is
// in, replacing a link or a file that is in the way.
func (x *extractor) ensureDir(d string) error {
	if x.safe[d] {
		return nil
	}
	if !isInside(x.dir, d) {
		return &UnsafeEntryError{d}
	}
	if err := x.ensureDir(filepath.Dir(d)); err != nil {
		return err
	}
	fi, err := os.Lstat(d)
	switch {
	case err == nil && fi.IsDir():
	case err == nil || errors.Is(err, fs.ErrNotExist):
		if err == nil {
			if err := os.Remove(d); err != nil {
				return err
			}
			delete(x.written, d)
		}
		if err := os.Mkdir(d, 0o755); err != nil {
			return err
		}
	default:
		return err
	}
	x.safe[d] = true
	return nil
}

// clear removes what is at p, unless it is a folder with something in
// it, for an entry that is not a folder to take its place.
func (x *extractor) clear(p string) error {
	if err := x.ensureDir(filepath.Dir(p)); err != nil {
		return err
	}
	delete(x.safe, p)
	delete(x.written, p)
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (x *extractor) file(p string, mode fs.FileMode, mtime time.Time, r io.Reader) (err error) {
	if err := x.clear(p); err != nil {
		return err
	}
	// O_EXCL: what is there now was just removed, so nothing, not even a
	// link made meanwhile, is followed.
	out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(p) // what was written is a part of the file
		}
	}()
	if err := x.c.add(0); err != nil {
		return err
	}
	if _, err := io.Copy(archiveWriter{out, x.c}, r); err != nil {
		return err
	}
	if !mtime.IsZero() {
		os.Chtimes(p, mtime, mtime)
	}
	x.written[p] = true
	return nil
}

func (x *extractor) symlink(clean, p, target string) error {
	local := filepath.FromSlash(target)
	if target == "" || path.IsAbs(target) || filepath.IsAbs(local) || filepath.VolumeName(local) != "" ||
		!isInside(x.dir, filepath.Join(filepath.Dir(p), local)) {
		x.skipped.Outside = append(x.skipped.Outside, clean)
		return nil
	}
	if err := x.clear(p); err != nil {
		return err
	}
	if err := symlink(local, p); err != nil {
		if linkNotAllowed(goos, err) {
			x.skipped.NotAllowed = append(x.skipped.NotAllowed, clean)
			return nil
		}
		return err
	}
	x.links = append(x.links, madeLink{clean, p})
	return x.c.add(0)
}

// hardLink links p to the file the archive already unpacked as name, or
// copies it where the file system has no hard links.
func (x *extractor) hardLink(p, name string) error {
	_, src, err := x.target(name)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(src)
	if err != nil || !fi.Mode().IsRegular() || !x.written[src] || !x.safe[filepath.Dir(src)] {
		return &UnsafeEntryError{name}
	}
	if err := x.clear(p); err != nil {
		return err
	}
	if os.Link(src, p) == nil {
		x.written[p] = true
		return x.c.add(0)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return x.file(p, fi.Mode().Perm(), fi.ModTime(), in)
}

// WindowsNameOK reports whether Windows allows name as a file name: none
// of <>:"/\|?* or control characters, no dot or space at the end, not a
// device such as CON or COM1.
func WindowsNameOK(name string) bool {
	if name == "" || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return false
		}
	}
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return false
	}
	return true
}

// LocalArchiveSize is the number of bytes of the regular files in paths,
// as WriteArchive packs them: links are not followed.
func LocalArchiveSize(paths []string) (int64, error) {
	var total int64
	for _, p := range paths {
		err := filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				if fi, err := d.Info(); err == nil {
					total += fi.Size()
				}
			}
			return nil
		})
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// RemoteArchiveSize is the number of bytes of the regular files in the
// remote paths, as tar packs them: links are not followed. It reads up
// to linkWorkers folders at a time, so many folders don't wait for each
// round trip.
func RemoteArchiveSize(ctx context.Context, c *sftp.Client, paths []string) (int64, error) {
	var total atomic.Int64
	var dirs []string
	for _, p := range paths {
		fi, err := c.Lstat(p)
		if err != nil {
			return 0, err
		}
		switch {
		case fi.IsDir():
			dirs = append(dirs, p)
		case fi.Mode().IsRegular():
			total.Add(fi.Size())
		}
	}
	// A level of the tree at a time.
	for len(dirs) > 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		var mu sync.Mutex
		var next []string
		var first error
		jobs := make(chan string)
		var wg sync.WaitGroup
		for range min(linkWorkers, len(dirs)) {
			wg.Go(func() {
				for d := range jobs {
					infos, err := c.ReadDir(d)
					mu.Lock()
					if err != nil && first == nil {
						first = err
					}
					for _, fi := range infos {
						switch {
						case fi.IsDir():
							next = append(next, path.Join(d, fi.Name()))
						case fi.Mode().IsRegular():
							total.Add(fi.Size())
						}
					}
					mu.Unlock()
				}
			})
		}
		for _, d := range dirs {
			jobs <- d
		}
		close(jobs)
		wg.Wait()
		if first != nil {
			return 0, first
		}
		dirs = next
	}
	return total.Load(), nil
}

// splitRemote returns the folder the remote paths are in, and their
// names; they must all be in the same one.
func splitRemote(paths []string) (string, []string, error) {
	if len(paths) == 0 {
		return "", nil, errors.New("nothing to transfer")
	}
	dir := path.Dir(paths[0])
	names := make([]string, len(paths))
	for i, p := range paths {
		if path.Dir(p) != dir {
			return "", nil, fmt.Errorf("%s is not in %s", p, dir)
		}
		names[i] = path.Base(p)
	}
	return dir, names, nil
}

// dirArg is the folder p as an argument of cd, which takes no "--".
func dirArg(p string) string {
	if strings.HasPrefix(p, "-") {
		p = "./" + p
	}
	return ShellQuote(p)
}

// downloadScript is what the server runs to pack names in dir.
func downloadScript(dir string, names []string) string {
	var b strings.Builder
	b.WriteString("cd " + dirArg(dir) + " && exec tar -czf - --")
	for _, n := range names {
		b.WriteString(" " + ShellQuote(n))
	}
	return b.String()
}

// uploadScript is what the server runs to unpack into dir, which it makes
// first. -o: the files belong to the user, not to the owners in the
// archive, even for root.
func uploadScript(dir string) string {
	return "mkdir -p -- " + ShellQuote(dir) + " && cd " + dirArg(dir) + " && exec tar -xzof -"
}

// DownloadArchive fetches the remote files and folders paths, all in one
// folder, into the local folder dstDir, packed by tar on the server, and
// returns the local path of the first, and the links it did not make
// (see ExtractArchive).
func DownloadArchive(ctx context.Context, client *ssh.Client, paths []string, dstDir string, progress ArchiveProgress) (string, SkippedLinks, error) {
	dir, names, err := splitRemote(paths)
	if err != nil {
		return "", SkippedLinks{}, err
	}
	local := filepath.Join(dstDir, names[0])
	var skipped SkippedLinks
	err = runTar(ctx, client, downloadScript(dir, names), func(_ io.Writer, stdout io.Reader) error {
		var err error
		skipped, err = ExtractArchive(ctx, stdout, dstDir, names, progress)
		return err
	})
	return local, skipped, err
}

// UploadArchive sends the local files and folders paths into the remote
// folder dstDir, which is made when it is not there, packed here and
// unpacked by tar on the server.
func UploadArchive(ctx context.Context, client *ssh.Client, paths []string, dstDir string, progress ArchiveProgress) error {
	if len(paths) == 0 {
		return errors.New("nothing to transfer")
	}
	return runTar(ctx, client, uploadScript(dstDir), func(stdin io.Writer, _ io.Reader) error {
		return WriteArchive(ctx, streamWriter{stdin}, paths, progress)
	})
}

// streamWriter marks the errors of writing to tar's input as the stream
// breaking off.
type streamWriter struct{ w io.Writer }

func (w streamWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		err = &streamError{err}
	}
	return n, err
}

// tarGrace is how long tar has to end by itself after the stream broke
// off, before it is stopped.
const tarGrace = 5 * time.Second

// runTar runs script with sh on the server, over an exec channel of its
// own, while work writes its input or reads its output. When ctx ends it
// stops the command, with a signal and by closing the channel. Errors of
// work's own (a local file, an unsafe name) come first; when the stream
// broke off and tar failed, tar's error says why.
func runTar(ctx context.Context, client *ssh.Client, script string, work func(stdin io.Writer, stdout io.Reader) error) error {
	if client == nil {
		return ErrNoShell
	}
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tailBuffer{max: 4 << 10}
	sess.Stderr = stderr
	// Through sh whatever the login shell is, which runs the command.
	if err := sess.Start("sh -c " + ShellQuote(script)); err != nil {
		return fmt.Errorf("cannot run tar: %w", err)
	}
	kill := func() {
		sess.Signal(ssh.SIGKILL)
		sess.Close()
	}
	defer context.AfterFunc(ctx, kill)()

	workErr := work(stdin, stdout)
	stdin.Close()
	go io.Copy(io.Discard, stdout) // what is left: padding, or the rest after an error
	var se *streamError
	switch {
	case workErr == nil:
	case errors.As(workErr, &se):
		defer time.AfterFunc(tarGrace, kill).Stop()
	default:
		kill()
	}
	waitErr := sess.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if workErr != nil && se == nil {
		return workErr
	}
	var exit *ssh.ExitError
	var missing *ssh.ExitMissingError
	switch {
	case errors.As(waitErr, &exit):
		return &TarError{Status: exit.ExitStatus(), Stderr: stderr.lines(3)}
	case errors.As(waitErr, &missing):
		return &TarError{Status: -1, Stderr: stderr.lines(3)}
	case workErr != nil:
		return workErr
	}
	return waitErr
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if over := len(t.b) - t.max; over > 0 {
		t.b = t.b[over:]
	}
	return len(p), nil
}

// lines is the last n lines that are not empty.
func (t *tailBuffer) lines(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, l := range bytes.Split(t.b, []byte("\n")) {
		if l := strings.TrimSpace(string(l)); l != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return strings.Join(out, "\n")
}
