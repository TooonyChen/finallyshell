package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The terminal's background image is a copy the app keeps in Dir(), so
// that it stays when the original is moved or deleted. Settings name it
// by its file name alone.

// backgroundPrefix starts the names of the copies.
const backgroundPrefix = "background-"

// ErrNoBackground is a name that is not one of the app's copies.
var ErrNoBackground = errors.New("not a background image of the app")

// BackgroundPath is where the copy name is kept.
func BackgroundPath(name string) (string, error) {
	if !isBackgroundName(name) {
		return "", ErrNoBackground
	}
	return filepath.Join(Dir(), name), nil
}

// isBackgroundName tells whether name is a file name SaveBackground
// gives, rather than a path elsewhere, as an edited settings.json might
// have.
func isBackgroundName(name string) bool {
	return strings.HasPrefix(name, backgroundPrefix) && filepath.Base(name) == name && !strings.ContainsAny(name, `/\`)
}

// SaveBackground keeps data, an image read from a file named like src,
// as a new copy in Dir(), and returns the copy's name. It writes the
// copy whole or not at all.
func SaveBackground(data []byte, src string) (string, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if len(ext) > 6 || strings.ContainsAny(ext, `/\ `) {
		ext = ""
	}
	name := fmt.Sprintf("%s%d%s", backgroundPrefix, time.Now().UnixNano(), ext)
	if err := writeFileAtomic(filepath.Join(Dir(), name), data); err != nil {
		return "", err
	}
	return name, nil
}

// RemoveBackground deletes the copy name; a copy already gone is no
// error.
func RemoveBackground(name string) error {
	path, err := BackgroundPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
