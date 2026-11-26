package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/pkg/sftp"
)

// ErrBadName is a name that cannot be a file's: empty, "." or "..", or
// one with a slash or a NUL in it.
var ErrBadName = errors.New("bad file name")

// ExistsError is a name already taken in its folder.
type ExistsError struct{ Name string }

func (e *ExistsError) Error() string { return fmt.Sprintf("%q already exists", e.Name) }

// ValidName reports whether name can be a file's name in a folder.
func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}

// free returns an error unless name is a valid name not taken in dir.
func free(c *sftp.Client, dir, name string) error {
	if !ValidName(name) {
		return ErrBadName
	}
	_, err := c.Lstat(path.Join(dir, name))
	switch {
	case err == nil:
		return &ExistsError{name}
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return err
}

// Rename gives the file, link or folder at p the name name in its folder,
// and returns its new path. It does not replace what has that name: the
// SFTP rename of OpenSSH refuses to, but other servers may not, so it
// looks first.
func Rename(c *sftp.Client, p, name string) (string, error) {
	dir := path.Dir(p)
	np := path.Join(dir, name)
	if name == path.Base(p) {
		return np, nil
	}
	if err := free(c, dir, name); err != nil {
		return "", err
	}
	return np, c.Rename(p, np)
}

// ErrSymlinkPermissions reports that SFTP cannot edit a link's own
// permissions with Chmod.
var ErrSymlinkPermissions = errors.New("cannot change a symbolic link's permissions")

// FilePermissions reads the permissions of the entry itself.
func FilePermissions(c *sftp.Client, p string) (fs.FileMode, error) {
	fi, err := c.Lstat(p)
	if err != nil {
		return 0, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return 0, ErrSymlinkPermissions
	}
	return fi.Mode() & (fs.ModePerm | specialBits), nil
}

// Chmod changes the read, write and execute bits and keeps the current
// set-ID and sticky bits.
func Chmod(c *sftp.Client, p string, mode fs.FileMode) error {
	current, err := FilePermissions(c, p)
	if err != nil {
		return err
	}
	return c.Chmod(p, current&specialBits|mode.Perm())
}

// Mkdir creates the folder name in dir, and returns its path.
func Mkdir(c *sftp.Client, dir, name string) (string, error) {
	if err := free(c, dir, name); err != nil {
		return "", err
	}
	p := path.Join(dir, name)
	return p, c.Mkdir(p)
}

// CreateFile creates the empty file name in dir, and returns its path.
// It never truncates a file that is there.
func CreateFile(c *sftp.Client, dir, name string) (string, error) {
	if err := free(c, dir, name); err != nil {
		return "", err
	}
	p := path.Join(dir, name)
	f, err := c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return "", err
	}
	return p, f.Close()
}

// removeWorkers is how many files of a folder RemoveAll removes at once:
// each is a round trip, which SFTP answers in parallel.
const removeWorkers = 16

// RemoveAll deletes the file or link at p, or the folder with all it
// holds. It never follows links: a link to a folder goes, the folder it
// points to stays. It stops at the first error, or when ctx ends, and
// removes nothing once ctx has ended.
func RemoveAll(ctx context.Context, c *sftp.Client, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fi, err := c.Lstat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return c.Remove(p)
	}
	return removeDir(ctx, c, p)
}

func removeDir(ctx context.Context, c *sftp.Client, dir string) error {
	// READDIR's attributes are the entries' own, as lstat's, so a link to
	// a folder is not a folder here.
	infos, err := c.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, fi := range infos {
		p := path.Join(dir, fi.Name())
		if !fi.IsDir() {
			files = append(files, p)
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := removeDir(ctx, c, p); err != nil {
			return err
		}
	}
	if err := removeFiles(ctx, c, files); err != nil {
		return err
	}
	return c.RemoveDirectory(dir)
}

// removeFiles removes the files at paths, several at once.
func removeFiles(ctx context.Context, c *sftp.Client, paths []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu    sync.Mutex
		first error
		wg    sync.WaitGroup
	)
	jobs := make(chan string)
	for range min(removeWorkers, len(paths)) {
		wg.Go(func() {
			for p := range jobs {
				if err := c.Remove(p); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
					cancel()
				}
			}
		})
	}
send:
	for _, p := range paths {
		select {
		case jobs <- p:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}
