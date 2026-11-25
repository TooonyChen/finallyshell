package remotefs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoveCommandUsesLiteralNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a Unix shell and filenames")
	}
	dir := filepath.Join(t.TempDir(), "folder's files")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Keep an unselected file and the target of a selected symlink.
	keep := filepath.Join(dir, "keep")
	if err := os.Mkdir(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(keep, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "nested", "child")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	literal := "-f ' ; `printf x` $(printf y) *\n!"
	if err := os.WriteFile(filepath.Join(dir, literal), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	picks := []string{literal, "nested", "link"}
	command, err := removeCommand(dir, picks)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("rm failed: %v\n%s", err, out)
	}
	for _, name := range picks {
		if _, err := os.Lstat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q still exists: %v", name, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("symlink target was removed: %v", err)
	}
}

func TestRemoveCommandRejectsNonLeafNames(t *testing.T) {
	for _, names := range [][]string{nil, {""}, {"."}, {".."}, {"/"}, {"ok", "../outside"}, {"bad\x00name"}} {
		if _, err := removeCommand("/dir", names); !errors.Is(err, ErrBadName) {
			t.Errorf("names %q: %v", names, err)
		}
	}
	if _, err := removeCommand("relative", []string{"file"}); !errors.Is(err, ErrBadName) {
		t.Errorf("relative directory: %v", err)
	}
}
