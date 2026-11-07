package remotefs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"

	"github.com/pkg/sftp"
)

// FS is what reading and saving a text file asks of an SFTP client.
// *sftp.Client has it all; tests wrap one to take a part of it away, as
// a server without the posix-rename extension.
type FS interface {
	Stat(p string) (fs.FileInfo, error)
	Lstat(p string) (fs.FileInfo, error)
	Open(p string) (*sftp.File, error)
	OpenFile(p string, flags int) (*sftp.File, error)
	Chmod(p string, mode fs.FileMode) error
	Chown(p string, uid, gid int) error
	Rename(oldname, newname string) error
	PosixRename(oldname, newname string) error
	Remove(p string) error
	HasExtension(name string) (string, bool)
}

var _ FS = (*sftp.Client)(nil)

// ErrBinary is a file whose first bytes have a NUL in them, which text
// files never do: ReadText stops reading it.
var ErrBinary = errors.New("binary file")

// sniffLen is how much of a file ReadText looks at for a NUL, as Git
// does.
const sniffLen = 8000

// ReadText reads the regular file p into memory, for the editor, and
// returns it with the file's info as it was opened: its size and time,
// which a save compares with the file's then. progress is told the bytes
// read, from the reading goroutine. It stops with ErrBinary when the
// file looks binary, and with the context's error when it ends.
func ReadText(ctx context.Context, c FS, p string, progress Progress) ([]byte, fs.FileInfo, error) {
	f, err := c.Open(p)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fi, fmt.Errorf("%s: not a regular file", p)
	}
	var buf bytes.Buffer
	buf.Grow(int(min(fi.Size(), 1<<30)) + 1)
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fi, err
	}
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return nil, fi, ErrBinary
	}
	buf.Write(head[:n])
	var done int64
	cnt := &counter{ctx: ctx, done: &done, progress: progress}
	if err := cnt.add(n); err != nil {
		return nil, fi, err
	}
	if n == sniffLen {
		if _, err := f.WriteTo(&countingWriter{w: &buf, counter: cnt}); err != nil {
			return nil, fi, err
		}
	}
	return buf.Bytes(), fi, nil
}

// SaveMethod is how SaveText wrote a file.
type SaveMethod int

const (
	// SavedReplaced is a new file, written next to the old one and
	// renamed over it: the file is either the old one or the new one,
	// whatever happens on the way.
	SavedReplaced SaveMethod = iota
	// SavedInPlace is the old file truncated and written again: a
	// failure on the way leaves it cut short.
	SavedInPlace
)

func (m SaveMethod) String() string {
	if m == SavedInPlace {
		return "in place"
	}
	return "replaced"
}

// errKeepFile is a reason to write the file in place rather than
// replace it: what replacing it would lose, or a directory the user
// cannot write in.
var errKeepFile = errors.New("cannot replace the file")

// SaveText writes data as the file p, as safely as the server and the
// file allow, and returns how, with the file's info after.
//
// It writes data to a hidden file in the same directory, gives it the
// mode and the owner of p, and renames it over p: with OpenSSH's
// posix-rename extension in one step, else by removing p and renaming
// the new file, the SFTP rename of version 3 refusing to replace a file.
// A reader of p sees either the old file or the new one, never a part,
// and a connection lost on the way leaves p as it was.
//
// Replacing the file is a new file, though: it loses what the server
// keeps of the old one beyond its mode and owner (extended attributes,
// ACLs, an SELinux label, other hard links to it), and only root can give
// a file to another user. So SaveText writes p in place, truncating it
// and writing it again, where it cannot replace it as it was:
//
//   - p is a symbolic link: the write goes through it to its target,
//     which stays where it is and the link a link;
//   - the user cannot write in p's directory, but can write p;
//   - p belongs to another user (or group) that the new file cannot be
//     given to, or its mode cannot be set;
//   - the rename is refused, as in a sticky directory such as /tmp.
//
// Writing in place keeps everything of the file but makes the save
// non-atomic: a failure on the way leaves the file cut short (the editor
// still has the text, to save again). A p that is gone is created anew.
func SaveText(c FS, p string, data []byte) (SaveMethod, fs.FileInfo, error) {
	method := SavedInPlace
	lfi, err := c.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = writeInPlace(c, p, data, true)
	case err != nil:
		return method, nil, err
	case lfi.Mode()&fs.ModeSymlink != 0:
		err = writeInPlace(c, p, data, false)
	case !lfi.Mode().IsRegular():
		return method, nil, fmt.Errorf("%s: not a regular file", p)
	default:
		method, err = replace(c, p, lfi, data)
		if errors.Is(err, errKeepFile) {
			method, err = SavedInPlace, writeInPlace(c, p, data, false)
		}
	}
	if err != nil {
		return method, nil, err
	}
	fi, err := c.Stat(p)
	return method, fi, err
}

// tempName is a hidden name in the directory of p, for the new file.
func tempName(p string) string {
	base := path.Base(p)
	if len(base) > 120 {
		base = base[:120]
	}
	var r [6]byte
	rand.Read(r[:])
	return path.Join(path.Dir(p), "."+base+".finallyshell-"+hex.EncodeToString(r[:])+".tmp")
}

// specialBits are the mode bits beyond the permissions that Chmod sets.
const specialBits = fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// replace writes data to a new file and renames it over p, which lfi
// describes. It fails with errKeepFile when it cannot keep p's owner and
// mode, or the user cannot write in the directory, having left p as it
// was.
func replace(c FS, p string, lfi fs.FileInfo, data []byte) (method SaveMethod, err error) {
	tmp := tempName(p)
	f, err := c.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if errors.Is(err, fs.ErrPermission) {
		return SavedReplaced, fmt.Errorf("%w: %v", errKeepFile, err)
	}
	if err != nil {
		return SavedReplaced, err
	}
	renamed := false
	defer func() {
		if f != nil {
			f.Close()
		}
		if !renamed {
			c.Remove(tmp)
		}
	}()
	// The owner first, as giving a file away clears its set-ID bits, and
	// the mode before the data, so that a private file's text is never in
	// a file others can read.
	if uid, gid, ok := owner(lfi); ok {
		if tfi, err := c.Lstat(tmp); err == nil {
			if tuid, tgid, ok := owner(tfi); ok && (tuid != uid || tgid != gid) {
				if err := c.Chown(tmp, int(uid), int(gid)); err != nil {
					return SavedReplaced, fmt.Errorf("%w: owner: %v", errKeepFile, err)
				}
			}
		}
	}
	mode := lfi.Mode() & (fs.ModePerm | specialBits)
	if err := c.Chmod(tmp, mode); err != nil {
		return SavedReplaced, fmt.Errorf("%w: mode: %v", errKeepFile, err)
	}
	if _, err := f.ReadFrom(bytes.NewReader(data)); err != nil {
		return SavedReplaced, err
	}
	err = f.Close()
	f = nil
	if err != nil {
		return SavedReplaced, err
	}
	if mode&specialBits != 0 {
		c.Chmod(tmp, mode) // writing cleared the set-ID bits
	}
	if err := renameOver(c, tmp, p); err != nil {
		return SavedReplaced, err
	}
	renamed = true
	return SavedReplaced, nil
}

// renameOver renames tmp to p, replacing it.
func renameOver(c FS, tmp, p string) error {
	if _, ok := c.HasExtension("posix-rename@openssh.com"); ok {
		err := c.PosixRename(tmp, p)
		if err == nil {
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("%w: rename: %v", errKeepFile, err)
		}
		// A server that lists the extension but fails it: as without.
	}
	// SFTP version 3's rename does not replace a file, so p goes first;
	// between the two p is missing, which posix-rename avoids.
	if err := c.Remove(p); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("%w: remove: %v", errKeepFile, err)
		}
		return err
	}
	if err := c.Rename(tmp, p); err != nil {
		// p is gone now: the caller writes it anew from data, which it
		// still has, and then drops tmp.
		return fmt.Errorf("%w: rename: %v", errKeepFile, err)
	}
	return nil
}

// owner returns the user and group of a file the server told, with ok
// false when it did not.
func owner(fi fs.FileInfo) (uid, gid uint32, ok bool) {
	st, ok := fi.Sys().(*sftp.FileStat)
	if !ok {
		return 0, 0, false
	}
	return st.UID, st.GID, true
}

// writeInPlace truncates p and writes data to it, through a link to its
// target, creating it when create is set or it is gone.
func writeInPlace(c FS, p string, data []byte, create bool) (err error) {
	flags := os.O_WRONLY | os.O_TRUNC
	if create {
		flags |= os.O_CREATE
	}
	f, err := c.OpenFile(p, flags)
	if errors.Is(err, fs.ErrNotExist) && !create {
		f, err = c.OpenFile(p, flags|os.O_CREATE) // removed by renameOver
	}
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = f.ReadFrom(bytes.NewReader(data))
	return err
}
