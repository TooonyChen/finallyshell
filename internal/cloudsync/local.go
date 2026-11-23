package cloudsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalFolder keeps the encrypted envelope in a folder transported by
// the user's own sync tool. That tool's cross-device propagation is not
// coordinated by this process or by the local file lock.
type LocalFolder struct {
	Dir string
}

func (f *LocalFolder) path() (string, error) {
	if strings.TrimSpace(f.Dir) == "" {
		return "", errors.New("cloudsync: no local sync folder")
	}
	return filepath.Abs(filepath.Join(f.Dir, FileName))
}

func localVersion(data []byte) Version {
	hash := sha256.Sum256(data)
	return Version{Tag: hex.EncodeToString(hash[:])}
}

func readLocalFile(path string) ([]byte, Version, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, Version{}, ErrNotFound
	}
	if err != nil {
		return nil, Version{}, err
	}
	defer f.Close()
	data, err := readBody(f)
	if err != nil {
		return nil, Version{}, err
	}
	return data, localVersion(data), nil
}

func (f *LocalFolder) Get(ctx context.Context) ([]byte, Version, error) {
	if err := ctx.Err(); err != nil {
		return nil, Version{}, err
	}
	path, err := f.path()
	if err != nil {
		return nil, Version{}, err
	}
	return readLocalFile(path)
}

// Put compares the content hash while holding a local OS lock, then
// atomically replaces the envelope. Sync tools may still replace the
// file themselves; they do not participate in this conditional write.
func (f *LocalFolder) Put(ctx context.Context, data []byte, prev *Version) (Version, error) {
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	if len(data) > maxResponse {
		return Version{}, fmt.Errorf("cloudsync: local file is over %d bytes", maxResponse)
	}
	path, err := f.path()
	if err != nil {
		return Version{}, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Version{}, err
	}
	lockPath, err := localLockPath(dir)
	if err != nil {
		return Version{}, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return Version{}, err
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Version{}, err
	}
	defer lock.Close()
	for {
		if err := ctx.Err(); err != nil {
			return Version{}, err
		}
		locked, err := tryLocalLock(lock)
		if err != nil {
			return Version{}, err
		}
		if locked {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Version{}, ctx.Err()
		case <-timer.C:
		}
	}
	defer unlockLocalFile(lock)
	_, current, err := readLocalFile(path)
	switch {
	case errors.Is(err, ErrNotFound):
		if prev != nil {
			return Version{}, ErrConflict
		}
	case err != nil:
		return Version{}, err
	case prev == nil || (prev.Tag != "" && prev.Tag != current.Tag):
		return Version{}, ErrConflict
	}
	tmp, err := os.CreateTemp(dir, ".finallyshell-sync-*.tmp")
	if err != nil {
		return Version{}, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Version{}, err
	}
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Version{}, err
	}
	return localVersion(data), nil
}
