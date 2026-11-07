package app

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// WindowKind names a secondary window: a dialog such as the settings,
// which FinalShell opens in a window of its own. The app keeps at most one
// window of each kind open; opening it again brings it to the front.
type WindowKind string

// The secondary windows.
const (
	WindowSettings WindowKind = "settings"
)

// WindowSpec says how a secondary window opens.
type WindowSpec struct {
	Kind WindowKind
	// Title is the text of the title bar, from locales: the platform asks
	// it again when the language changes.
	Title func() string
	// Width and Height are the size the window's view opens at, without
	// the title bar; MinWidth and MinHeight the least the user can make
	// the window, 0 for no limit.
	Width, Height       int
	MinWidth, MinHeight int
	// Parent is the window this one belongs to: it stays above it and
	// closes with it. "" is the main window, as is a kind that is not
	// open.
	Parent WindowKind
	// Modal blocks the parent until this window closes. On macOS the
	// window is then a sheet of its parent.
	Modal bool
}

// openWindow opens the window of spec.Kind showing view, or brings it to
// the front when it is open already. The view is the window's alone: it
// has the app's theme, and Escape closes the window unless something in
// it takes the key first.
func (a *App) openWindow(spec WindowSpec, view func(c *ui.Context)) {
	if a.windows[spec.Kind] {
		a.plat.FocusWindow(spec.Kind)
		return
	}
	if spec.Parent != "" && !a.windows[spec.Parent] {
		spec.Parent = ""
	}
	a.windows[spec.Kind] = true
	kind := spec.Kind
	a.plat.OpenWindow(spec, func(c *ui.Context) {
		c.SetTheme(theme)
		if c.Shortcut(0, ui.KeyEscape) {
			a.closeWindow(kind)
		}
		view(c)
	}, func() { delete(a.windows, kind) })
}

// closeWindow closes the window of kind, as its Close button does.
func (a *App) closeWindow(kind WindowKind) {
	if a.windows[kind] {
		a.plat.CloseWindow(kind)
	}
}

// windowOpen tells whether the window of kind is open.
func (a *App) windowOpen(kind WindowKind) bool { return a.windows[kind] }

// closeWindows closes every secondary window. A parent takes its
// children with it, so the order does not matter.
func (a *App) closeWindows() {
	kinds := make([]WindowKind, 0, len(a.windows))
	for k := range a.windows {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	for _, k := range kinds {
		a.closeWindow(k)
	}
}

// redraw builds every open window again. A window's view redraws itself
// after its own input; a change it makes to state that other windows
// show, as a saved host or a setting, calls redraw so they show it too.
// Work posted with App.post redraws them all on its own.
func (a *App) redraw() { a.plat.Redraw() }
