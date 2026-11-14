// Finallyshell is an SSH client laid out as FinalShell, with its session
// tabs down the side.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"finallyshell/internal/app"
	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// platform is the system, as the app asks it for windows, dialogs,
// folders and the language. Its methods run on the UI thread, apart from
// Post and the dialogs, which the app calls from goroutines.
type platform struct {
	win     *mygo.Window // the main window
	windows map[app.WindowKind]*childWindow
	tray    *mygo.Tray // while the main window is hidden in the tray
}

// childWindow is an open secondary window.
type childWindow struct {
	win     *mygo.Window
	title   func() string
	closing bool // the app is closing it
}

// Post runs fn on the UI thread and redraws every window: the state fn
// changes may show in any of them.
func (p *platform) Post(fn func()) {
	p.win.Update(func() {
		fn()
		p.redrawChildren()
	})
}

func (p *platform) Redraw() {
	p.win.Invalidate()
	p.redrawChildren()
}

func (p *platform) redrawChildren() {
	for _, w := range p.windows {
		w.win.Invalidate()
	}
}

func (p *platform) OpenWindow(spec app.WindowSpec, view func(c *ui.Context), closed func()) {
	parent := p.win
	if w := p.windows[spec.Parent]; spec.Parent != "" && w != nil {
		parent = w.win
	}
	// Centered over the parent, as dialogs are, rather than on the
	// screen. A sheet (Modal on macOS) and Wayland place it themselves.
	b := parent.Bounds()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           spec.Title(),
		Width:           spec.Width,
		Height:          spec.Height,
		UseContentSize:  true,
		MinWidth:        spec.MinWidth,
		MinHeight:       spec.MinHeight,
		X:               b.X + (b.Width-spec.Width)/2,
		Y:               b.Y + (b.Height-spec.Height)/2,
		Parent:          parent,
		Modal:           spec.Modal,
		DisableMinimize: true,
		Content:         ui.View(view),
	})
	child := &childWindow{win: win, title: spec.Title}
	p.windows[spec.Kind] = child
	// The window may ask first, as the editor does with unsaved changes,
	// when the user closes it; the app closing it does not ask.
	win.OnClose(func(e *mygo.CloseEvent) {
		if !child.closing && spec.CloseRequested != nil && !spec.CloseRequested() {
			e.PreventDefault()
		}
	})
	win.OnClosed(func() {
		if w := p.windows[spec.Kind]; w != nil && w.win == win {
			delete(p.windows, spec.Kind)
		}
		closed()
	})
}

func (p *platform) RetitleWindow(kind app.WindowKind) {
	if w := p.windows[kind]; w != nil {
		w.win.SetTitle(w.title())
	}
}

func (p *platform) FocusWindow(kind app.WindowKind) {
	if kind == app.WindowMain {
		p.raise() // out of the tray, or restored, too
		return
	}
	if w := p.windows[kind]; w != nil {
		w.win.Focus() // restores it too, and makes it the key window
	}
}

func (p *platform) CloseWindow(kind app.WindowKind) {
	if w := p.windows[kind]; w != nil {
		w.closing = true
		w.win.Close()
	}
}

// retitle sets the windows' titles again in the language shown now.
func (p *platform) retitle() {
	p.win.SetTitle(locales.AppTitle())
	for _, w := range p.windows {
		w.win.SetTitle(w.title())
	}
}

func (p *platform) OpenFiles(title string, folder bool) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: p.win, Title: title, Directory: folder, Multiple: !folder})
}

func (p *platform) ChooseFolder(parent app.WindowKind, title string) (string, error) {
	return p.openOne(parent, mygo.OpenDialogOptions{Title: title, Directory: true, CreateDirectories: true})
}

func (p *platform) OpenFile(parent app.WindowKind, title, dir string) (string, error) {
	return p.openOne(parent, mygo.OpenDialogOptions{Title: title, DefaultPath: dir, ShowHiddenFiles: true})
}

func (p *platform) OpenImage(parent app.WindowKind, title string) (string, error) {
	return p.openOne(parent, mygo.OpenDialogOptions{Title: title, Filters: []mygo.FileFilter{{
		Name:       locales.ImageFiles(),
		Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"},
	}}})
}

func (p *platform) OpenVSIX(parent app.WindowKind, title string) (string, error) {
	return p.openOne(parent, mygo.OpenDialogOptions{Title: title, Filters: []mygo.FileFilter{{
		Name:       locales.VsixFiles(),
		Extensions: []string{"vsix"},
	}}})
}

// OpenFolder shows hidden folders, as FinalShell's ~/.finalshell on Linux
// and ~/Library on macOS are.
func (p *platform) OpenFolder(parent app.WindowKind, title string) (string, error) {
	return p.openOne(parent, mygo.OpenDialogOptions{Title: title, Directory: true, ShowHiddenFiles: true})
}

// OpenFileOfType shows hidden files, as ~/.ssh/config is.
func (p *platform) OpenFileOfType(parent app.WindowKind, title, typeName string, extensions []string) (string, error) {
	opts := mygo.OpenDialogOptions{Title: title, ShowHiddenFiles: true}
	if len(extensions) > 0 {
		opts.Filters = []mygo.FileFilter{{Name: typeName, Extensions: extensions}}
	}
	return p.openOne(parent, opts)
}

// openOne asks for one file as opts say, over the window of parent, or
// the main window when that is not open: "" when canceled.
func (p *platform) openOne(parent app.WindowKind, opts mygo.OpenDialogOptions) (string, error) {
	opts.Parent = p.owner(parent)
	paths, err := mygo.Dialog.Open(opts)
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (p *platform) SaveFile(parent app.WindowKind, title, path, typeName string, extensions []string) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: p.owner(parent), Title: title, DefaultPath: path, CreateDirectories: true,
		Filters: []mygo.FileFilter{{Name: typeName, Extensions: extensions}},
	})
}

// owner is the window of parent, or the main window when that is not
// open, for a dialog over it. It is called off the UI thread.
func (p *platform) owner(parent app.WindowKind) *mygo.Window {
	// The windows are the UI thread's; the dialog waits on this one.
	owner := make(chan *mygo.Window, 1)
	p.win.Update(func() {
		w := p.win
		if c := p.windows[parent]; c != nil {
			w = c.win
		}
		owner <- w
	})
	return <-owner
}

func (p *platform) DownloadsDir() string {
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil && dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (p *platform) ShowInFolder(path string)   { mygo.Shell.ShowItemInFolder(path) }
func (p *platform) OpenPath(path string) error { return mygo.Shell.OpenPath(path) }
func (p *platform) SetLocale(tag string)       { mygo.App.SetLocale(tag) }
func (p *platform) Languages() []string        { return mygo.App.Languages() }

// SetAppearance makes every window light or dark, or follow the system.
func (p *platform) SetAppearance(a config.Appearance) { mygo.Theme.SetSource(themeSource(a)) }

// themeSource is the appearance a as MyGo names it.
func themeSource(a config.Appearance) mygo.ThemeSource {
	switch a.Normal() {
	case config.AppearanceLight:
		return mygo.ThemeLight
	case config.AppearanceDark:
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// ShowTransfers puts the count on the Dock icon (macOS; the launcher
// entry on Linux docks that show one) and the progress under it, or on
// the taskbar button (Windows), where MyGo shows SetProgressBar.
func (p *platform) ShowTransfers(count int, progress float64) {
	mygo.App.SetBadgeCount(count)
	switch {
	case count == 0:
		p.win.SetProgressBar(mygo.ProgressBar{})
	case progress < 0:
		p.win.SetProgressBar(mygo.ProgressBar{State: mygo.ProgressIndeterminate})
	default:
		p.win.SetProgressBar(mygo.ProgressBar{State: mygo.ProgressNormal, Value: progress})
	}
}

// TempDir is in the app's cache folder: the copies of remote files
// opened with other apps, which the app clears out after a day.
func (p *platform) TempDir() string {
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil || dir == "" {
		dir = filepath.Join(os.TempDir(), "finallyshell")
	}
	return filepath.Join(dir, "opened")
}

// SetFullScreen and IsFullScreen put the main window in the system's full
// screen, for the app's (internal/app/fullscreen.go).
func (p *platform) SetFullScreen(on bool) { p.win.SetFullScreen(on) }
func (p *platform) IsFullScreen() bool    { return p.win.IsFullScreen() }

func main() {
	// Before the first window: MyGo reads MYGO_GPU when it makes one.
	useMemoryRendering()
	// One instance at a time: launching the app again brings the running
	// one's window back, from the tray too (raise), and two instances never
	// write hosts.json and settings.json over each other. This runs before
	// anything else, as MyGo reads the launch's Wayland activation token
	// here, before GTK starts.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	settings := config.LoadSettings()
	if lang := settings.Language; lang != "" {
		mygo.App.SetLocale(lang)
	}
	// Light, dark or the system's, native title bars included, from the
	// first frame.
	mygo.Theme.SetSource(themeSource(settings.Appearance))
	useUpdater() // updates.go
	mygo.App.WhenReady(func() {
		if runtime.GOOS == "darwin" {
			setAppMenu() // once the app knows its language
		}
		plat := &platform{windows: map[app.WindowKind]*childWindow{}}
		a := app.New(plat)
		a.NewTab() // it starts on the start page
		plat.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         locales.AppTitle(),
			Width:         1280,
			Height:        820,
			MinWidth:      900,
			MinHeight:     560,
			TitleBarStyle: mygo.TitleBarHidden,
			Content:       ui.View(a.View),
		})
		plat.win.OnLeaveFullScreen(a.WindowLeftFullScreen)
		// The close button may hide the window to the tray, or ask first;
		// a quit may ask too (closing.go in internal/app).
		plat.win.OnClose(func(e *mygo.CloseEvent) {
			if !a.CloseRequested() {
				e.PreventDefault()
			}
		})
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
			if !a.QuitRequested() {
				e.PreventDefault()
			}
		})
		// Its children close with it, before this runs.
		plat.win.OnClosed(func() {
			if plat.tray != nil {
				plat.tray.Destroy()
				plat.tray = nil
			}
			a.CloseAll()
		})
		// The Dock icon (macOS) shows the window hidden in the tray.
		mygo.App.OnActivate(func(hasVisibleWindows bool) {
			if !hasVisibleWindows {
				plat.showMain()
			}
		})
		mygo.App.OnSecondInstance(func([]string, string) { plat.raise() })
		mygo.App.OnLocaleChanged(func(string) {
			plat.retitle()
			if runtime.GOOS == "darwin" {
				setAppMenu() // its Check for Updates… in the new language
			}
		})
		plat.watchUpdates()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
