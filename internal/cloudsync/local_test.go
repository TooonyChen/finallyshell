package cloudsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLocalFolderVersions(t *testing.T) {
	ctx := context.Background()
	r := &LocalFolder{Dir: filepath.Join(t.TempDir(), "new", "folder")}
	if _, _, err := r.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	v, err := r.Put(ctx, []byte("first"), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, readVersion, err := r.Get(ctx)
	if err != nil || string(data) != "first" || v != readVersion || v.Tag == "" {
		t.Fatalf("read %q, %+v: %v", data, readVersion, err)
	}
	if _, err := r.Put(ctx, []byte("overwrite"), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("create over existing: %v", err)
	}
	next, err := r.Put(ctx, []byte("second"), &v)
	if err != nil || next == v {
		t.Fatalf("update: %+v, %v", next, err)
	}
	if _, err := r.Put(ctx, []byte("stale"), &v); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}
	// A user's sync tool changes the file without taking our lock.
	path := filepath.Join(r.Dir, FileName)
	if err := os.WriteFile(path, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Put(ctx, []byte("stale"), &next); !errors.Is(err, ErrConflict) {
		t.Fatalf("external change: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Put(ctx, []byte("stale"), &next); !errors.Is(err, ErrConflict) {
		t.Fatalf("external deletion: %v", err)
	}
	if _, err := r.Put(ctx, []byte("recreated"), nil); err != nil {
		t.Fatal(err)
	}
	if temps, _ := filepath.Glob(filepath.Join(r.Dir, ".finallyshell-sync-*.tmp")); len(temps) != 0 {
		t.Fatalf("temporary files left behind: %v", temps)
	}
}

func TestLocalFolderConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	v, err := (&LocalFolder{Dir: dir}).Put(context.Background(), []byte("initial"), nil)
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			_, err := (&LocalFolder{Dir: dir}).Put(context.Background(), []byte{byte(i)}, &v)
			errs <- err
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("%d writers overwrote the same version", successes)
	}
}

func TestLocalFolderCancellationAndLimits(t *testing.T) {
	r := &LocalFolder{Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := r.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled get: %v", err)
	}
	if _, err := r.Put(ctx, []byte("canceled"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled put: %v", err)
	}
	ctx = context.Background()
	v, err := r.Put(ctx, []byte("original"), nil)
	if err != nil {
		t.Fatal(err)
	}
	large := bytes.Repeat([]byte{'x'}, maxResponse+1)
	if _, err := r.Put(ctx, large, &v); err == nil {
		t.Fatal("accepted an oversized write")
	}
	if data, _, err := r.Get(ctx); err != nil || string(data) != "original" {
		t.Fatalf("failed write changed the original: %q, %v", data, err)
	}
	lockPath, err := localLockPath(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if locked, err := tryLocalLock(lock); err != nil || !locked {
		t.Fatalf("hold lock: %v, %v", locked, err)
	}
	defer unlockLocalFile(lock)
	waitCtx, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if _, err := r.Put(waitCtx, []byte("blocked"), &v); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.Dir, FileName), large, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Get(ctx); err == nil {
		t.Fatal("accepted an oversized read")
	}
	for _, dir := range []string{"", " \t"} {
		if _, _, err := (&LocalFolder{Dir: dir}).Get(ctx); err == nil {
			t.Fatal("accepted an empty directory")
		}
	}
}
