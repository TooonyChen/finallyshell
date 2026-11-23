package cloudsync

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func localLockPath(dir string) (string, error) {
	// Keep the persistent lock outside the transported folder: a sync
	// tool replacing a lock file would replace the inode we locked.
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(canonical))
	return filepath.Join(cache, "finallyshell", "sync-locks", hex.EncodeToString(hash[:])+".lock"), nil
}
