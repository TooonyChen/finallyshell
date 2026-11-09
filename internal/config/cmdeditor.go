package config

import (
	"errors"
	"os"
	"path/filepath"
)

// The command editor of the Commands tab (internal/app/cmdeditor.go)
// keeps its text in commandeditor.txt in Dir(), as typed, so that it is
// there again after a restart. It is this device's own: not synced.

// CommandEditorFile is the name of the command editor's file in Dir().
const CommandEditorFile = "commandeditor.txt"

func commandEditorPath() string { return filepath.Join(Dir(), CommandEditorFile) }

// LoadCommandEditorText reads the command editor's text: "" when there is
// no file yet.
func LoadCommandEditorText() (string, error) {
	data, err := os.ReadFile(commandEditorPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveCommandEditorText writes the command editor's text, replacing the
// file at once.
func SaveCommandEditorText(text string) error {
	return writeFileAtomic(commandEditorPath(), []byte(text))
}
