// Package fileicons reads VS Code file icon themes and finds the icon of
// a file or folder by its name, as VS Code's explorer does.
//
// A theme comes in an extension, a folder or a VSIX (a zip) with a
// package.json that lists its themes under contributes.iconThemes. The
// theme is a JSON file that defines icons (an SVG or PNG file, or a
// character of a font) and maps file names, extensions, languages and
// folder names to them; see
// https://code.visualstudio.com/api/extension-guides/file-icon-theme.
// Nothing an extension holds runs: only its JSON, images and fonts are
// read.
//
// Two themes come with the app (bundled/): Material Icon Theme (MIT
// License, Copyright (c) 2025 Material Extensions) and VS Code's Seti
// (MIT License, Copyright (c) Microsoft Corporation, with Seti UI's
// icons, MIT License, Copyright (c) 2014 Jesse Weed). Each VSIX holds its
// license.
package fileicons

//go:generate go run ./internal/gen

import "github.com/egoist/mygo/ui"

// Icon is a file icon: an image, or a character of a font.
type Icon struct {
	// ID is the theme's name for the icon, from its iconDefinitions.
	ID string
	// Image is the icon's SVG or bitmap, shown in its own colors; nil for
	// a character.
	Image ui.ImageSource
	// Glyph is the character of the font registered as Font that shows
	// the icon, when Image is nil.
	Glyph rune
	Font  string
	// Color is the character's color, when HasColor; else it takes the
	// text's.
	Color    ui.Color
	HasColor bool
	// Scale is the character's size as a share of the explorer's font
	// size, 1 for the same size.
	Scale float32
}
