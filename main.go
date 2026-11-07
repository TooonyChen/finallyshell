// Finalshelly is an SSH client laid out as FinalShell, with its session
// tabs down the side.
package main

import (
	"log"
	"os"
	"path/filepath"

	"finalshelly/internal/app"
	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// platform is the system, as the app asks it for dialogs, folders and
// the language.
type platform struct{ win *mygo.Window }

func (p *platform) Post(fn func()) { p.win.Update(fn) }

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

func main() {
	if lang := config.LoadSettings().Language; lang != "" {
		mygo.App.SetLocale(lang)
	}
	mygo.App.WhenReady(func() {
		plat := &platform{}
		a := app.New(plat)
		plat.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.AppTitle(),
			Width:     1280,
			Height:    820,
			MinWidth:  900,
			MinHeight: 560,
			Content:   ui.View(a.View),
		})
		plat.win.OnClosed(a.CloseAll)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
