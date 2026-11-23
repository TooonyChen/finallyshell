package cloudsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// External sync tools can atomically replace files in the sync folder.
// A replaced lock inode must not let another app writer bypass our lock.
func TestLocalFolderSyncToolCannotReplaceActiveLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permits replacing an open lock file")
	}
	r := &LocalFolder{Dir: t.TempDir()}
	v, err := r.Put(context.Background(), []byte("original"), nil)
	if err != nil {
		t.Fatal(err)
	}
	path, err := localLockPath(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if locked, err := tryLocalLock(lock); err != nil || !locked {
		t.Fatalf("hold lock: %v, %v", locked, err)
	}
	defer unlockLocalFile(lock)
	external := filepath.Join(r.Dir, "external-lock")
	if err := os.WriteFile(external, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(external, filepath.Join(r.Dir, ".finallyshell-sync.lock")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := r.Put(ctx, []byte("bypassed lock"), &v); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an external sync tool bypassed an active app lock: %v", err)
	}
	data, _, err := r.Get(context.Background())
	if err != nil || string(data) != "original" {
		t.Fatalf("blocked write changed file: %q, %v", data, err)
	}
}
