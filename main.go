// Finalshelly is an SSH client laid out as FinalShell, with its session
// tabs down the side.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"finalshelly/internal/app"
	"finalshelly/internal/config"
	"finalshelly/locales"

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
	win   *mygo.Window
	title func() string
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
	p.windows[spec.Kind] = &childWindow{win: win, title: spec.Title}
	win.OnClosed(func() {
		if w := p.windows[spec.Kind]; w != nil && w.win == win {
			delete(p.windows, spec.Kind)
		}
		closed()
	})
}

func (p *platform) FocusWindow(kind app.WindowKind) {
	if w := p.windows[kind]; w != nil {
		w.win.Focus() // restores it too, and makes it the key window
	}
}

func (p *platform) CloseWindow(kind app.WindowKind) {
	if w := p.windows[kind]; w != nil {
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

func (p *platform) ChooseFolder(title string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: p.win, Title: title, Directory: true, CreateDirectories: true})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (p *platform) OpenFile(parent app.WindowKind, title, dir string) (string, error) {
	// The windows are the UI thread's; the dialog waits on this one.
	owner := make(chan *mygo.Window, 1)
	p.win.Update(func() {
		w := p.win
		if c := p.windows[parent]; c != nil {
			w = c.win
		}
		owner <- w
	})
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: <-owner, Title: title, DefaultPath: dir, ShowHiddenFiles: true})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (p *platform) DownloadsDir() string {
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil && dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func (p *platform) ShowInFolder(path string) { mygo.Shell.ShowItemInFolder(path) }
func (p *platform) SetLocale(tag string)     { mygo.App.SetLocale(tag) }
func (p *platform) Languages() []string      { return mygo.App.Languages() }

// SetFullScreen and IsFullScreen put the main window in the system's full
// screen, for the app's (internal/app/fullscreen.go).
func (p *platform) SetFullScreen(on bool) { p.win.SetFullScreen(on) }
func (p *platform) IsFullScreen() bool    { return p.win.IsFullScreen() }

func main() {
	// One instance at a time: launching the app again brings the running
	// one's window back, from the tray too (raise), and two instances never
	// write hosts.json and settings.json over each other. This runs before
	// anything else, as MyGo reads the launch's Wayland activation token
	// here, before GTK starts.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	if lang := config.LoadSettings().Language; lang != "" {
		mygo.App.SetLocale(lang)
	}
	if runtime.GOOS == "darwin" {
		// MyGo's default menu bar but its File menu, whose Close Window
		// takes Cmd+W before the windows: the main one closes the tab with
		// it, a secondary one itself (openWindow in internal/app).
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleViewMenu}, {Role: mygo.RoleWindowMenu},
		}))
	}
	mygo.App.WhenReady(func() {
		plat := &platform{windows: map[app.WindowKind]*childWindow{}}
		a := app.New(plat)
		a.NewTab() // it starts on the start page
		plat.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.AppTitle(),
			Width:     1280,
			Height:    820,
			MinWidth:  900,
			MinHeight: 560,
			Content:   ui.View(a.View),
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
		mygo.App.OnLocaleChanged(func(string) { plat.retitle() })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
