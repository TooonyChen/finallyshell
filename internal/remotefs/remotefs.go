// Package remotefs lists, uploads and downloads files over SFTP.
package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// NewClient starts SFTP on an SSH connection, with transfers that send and
// fetch several chunks at once, which is what makes them fast on links
// with a long round trip.
func NewClient(client *ssh.Client) (*sftp.Client, error) {
	return sftp.NewClient(client, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
}

// Entry is a file or directory on the server.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode // of the target, for a link to a directory
	ModTime time.Time
	UID     uint32
	GID     uint32
	Link    bool
}

func (e Entry) IsDir() bool { return e.Mode.IsDir() }

// ReadDir lists dir, directories first, then by name.
func ReadDir(c *sftp.Client, dir string) ([]Entry, error) {
	infos, err := c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		e := Entry{Name: fi.Name(), Size: fi.Size(), Mode: fi.Mode(), ModTime: fi.ModTime()}
		if st, ok := fi.Sys().(*sftp.FileStat); ok {
			e.UID, e.GID = st.UID, st.GID
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			e.Link = true
			if target, err := c.Stat(path.Join(dir, fi.Name())); err == nil && target.IsDir() {
				e.Mode = target.Mode()
			}
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(x, y Entry) int {
		if x.IsDir() != y.IsDir() {
			if x.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return entries, nil
}

// RemoteExisting returns which of names are in the remote directory dir.
func RemoteExisting(c *sftp.Client, dir string, names []string) []string {
	var found []string
	for _, n := range names {
		if _, err := c.Lstat(path.Join(dir, n)); err == nil {
			found = append(found, n)
		}
	}
	return found
}

// LocalExisting returns which of names are in the local directory dir.
func LocalExisting(dir string, names []string) []string {
	var found []string
	for _, n := range names {
		if _, err := os.Lstat(filepath.Join(dir, n)); err == nil {
			found = append(found, n)
		}
	}
	return found
}

// LocalSize is the number of bytes of the files in p, a file or a
// directory. Links to directories are not followed, as Upload skips them.
func LocalSize(p string) (int64, error) {
	var total int64
	err := filepath.WalkDir(p, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := os.Stat(name)
		if err == nil && fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

// RemoteSize is the number of bytes of the files in p, a file or a
// directory, as Download would fetch them.
func RemoteSize(c *sftp.Client, p string) (int64, error) {
	fi, err := c.Stat(p)
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() {
		return fi.Size(), nil
	}
	var total int64
	err = walkRemote(c, p, func(_ string, fi fs.FileInfo) error {
		total += fi.Size()
		return nil
	})
	return total, err
}

// walkRemote calls file for each regular file under the directory dir,
// with its path. Links to directories are not followed, which keeps a
// link to a parent from looping.
func walkRemote(c *sftp.Client, dir string, file func(p string, fi fs.FileInfo) error) error {
	infos, err := c.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, fi := range infos {
		p := path.Join(dir, fi.Name())
		if fi.Mode()&fs.ModeSymlink != 0 {
			target, err := c.Stat(p)
			if err != nil || target.IsDir() {
				continue
			}
			fi = target
		}
		switch {
		case fi.IsDir():
			if err := walkRemote(c, p, file); err != nil {
				return err
			}
		case fi.Mode().IsRegular():
			if err := file(p, fi); err != nil {
				return err
			}
		}
	}
	return nil
}

// Progress is told the bytes copied so far, from the transfer's goroutine.
type Progress func(done int64)

type counter struct {
	ctx      context.Context
	done     *int64
	progress Progress
}

func (c *counter) add(n int) error {
	*c.done += int64(n)
	if c.progress != nil {
		c.progress(*c.done)
	}
	return c.ctx.Err()
}

// countingReader counts what is read, and stops when the context ends.
// Its Size lets sftp write several chunks at once.
type countingReader struct {
	r    io.Reader
	size int64
	*counter
}

func (r *countingReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	if cerr := r.add(n); cerr != nil && err == nil {
		err = cerr
	}
	return n, err
}

func (r *countingReader) Size() int64 { return r.size }

type countingWriter struct {
	w io.Writer
	*counter
}

func (w *countingWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.w.Write(p)
	if cerr := w.add(n); cerr != nil && err == nil {
		err = cerr
	}
	return n, err
}

// Upload copies the local file or directory src into the remote directory
// dstDir, as dstDir/<name of src>, replacing files that are there.
func Upload(ctx context.Context, c *sftp.Client, src, dstDir string, progress Progress) error {
	var done int64
	cnt := &counter{ctx: ctx, done: &done, progress: progress}
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	dst := path.Join(dstDir, filepath.Base(src))
	if !fi.IsDir() {
		return uploadFile(c, src, dst, fi, cnt)
	}
	return filepath.WalkDir(src, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, name)
		if err != nil {
			return err
		}
		target := path.Join(dst, filepath.ToSlash(rel))
		if d.IsDir() {
			return c.MkdirAll(target)
		}
		fi, err := os.Stat(name)
		if err != nil || !fi.Mode().IsRegular() {
			return nil // a broken link, a device, a link to a directory
		}
		return uploadFile(c, name, target, fi, cnt)
	})
}

func uploadFile(c *sftp.Client, src, dst string, fi fs.FileInfo, cnt *counter) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := c.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("%s: %w", dst, err)
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			c.Remove(dst) // what was written is a part of the file
		}
	}()
	if _, err = out.ReadFrom(&countingReader{r: in, size: fi.Size(), counter: cnt}); err != nil {
		return err
	}
	c.Chmod(dst, fi.Mode().Perm()) // best effort: some servers refuse it
	return nil
}

// Download copies the remote file or directory src into the local
// directory dstDir, as dstDir/<name of src>, replacing files that are
// there, and returns the local path.
func Download(ctx context.Context, c *sftp.Client, src, dstDir string, progress Progress) (string, error) {
	var done int64
	cnt := &counter{ctx: ctx, done: &done, progress: progress}
	fi, err := c.Stat(src)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, path.Base(src))
	if !fi.IsDir() {
		return dst, downloadFile(c, src, dst, fi, cnt)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return dst, err
	}
	return dst, walkRemote(c, src, func(p string, fi fs.FileInfo) error {
		rel := strings.TrimPrefix(p, strings.TrimSuffix(src, "/")+"/")
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if !isInside(dst, target) {
			return fmt.Errorf("unsafe name %q", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return downloadFile(c, p, target, fi, cnt)
	})
}

// isInside reports whether p is in dir, as a name the server sends must
// be, so that it cannot write elsewhere.
func isInside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func downloadFile(c *sftp.Client, src, dst string, fi fs.FileInfo, cnt *counter) (err error) {
	in, err := c.Open(src)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst)
		}
	}()
	if _, err = in.WriteTo(&countingWriter{w: out, counter: cnt}); err != nil {
		return err
	}
	if perm := fi.Mode().Perm(); perm&0o111 != 0 {
		os.Chmod(dst, 0o755) // keep scripts and programs runnable
	}
	return nil
}

// IsCanceled reports whether err is a transfer stopped by its context.
func IsCanceled(err error) bool { return errors.Is(err, context.Canceled) }
