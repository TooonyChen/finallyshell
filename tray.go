package main

import (
	"bytes"
	_ "embed"
	"image/color"
	"image/png"
	"log"
	"runtime"

	"finallyshell/internal/app"
	"finallyshell/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// HideToTray hides the main window behind a tray icon, which is there
// only while the window is hidden: its menu shows the window again or
// quits. On macOS the Dock icon stays, and clicking it shows the window
// too (OnActivate in main).
func (p *platform) HideToTray() bool {
	if p.tray == nil {
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:             "main",
			Icon:           trayIcon(),
			IconIsTemplate: runtime.GOOS != "linux",
			ToolTip:        locales.AppTitle(),
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: locales.TrayShow(), Click: func(*mygo.MenuItem, *mygo.Window) { p.showMain() }},
				mygo.Separator(),
				{Label: locales.TrayQuit(), Click: func(*mygo.MenuItem, *mygo.Window) { p.Quit() }},
			}),
		})
		if err != nil {
			// Linux without libayatana-appindicator3: the window stays,
			// as there would be no way back to it.
			log.Println("tray:", err)
			return false
		}
		tray.OnClick(p.showMain) // a double click on Linux, where a click opens the menu
		p.tray = tray
	}
	p.win.Hide()
	return true
}

// showMain shows the main window hidden in the tray, and takes the tray
// icon away.
func (p *platform) showMain() {
	if p.tray == nil {
		return
	}
	p.tray.Destroy()
	p.tray = nil
	p.win.Show()
	mygo.App.Focus()
}

// raise brings the main window to the front, as another launch of the
// app asks: out of the tray, or out of the Dock or taskbar when it is
// minimized. MyGo runs it with the right to take the focus that the launch
// passed on: its activation token on Wayland, AllowSetForegroundWindow on
// Windows.
func (p *platform) raise() {
	if p.tray != nil {
		p.showMain()
		return
	}
	p.win.Restore()
	p.win.Focus()
}

func (p *platform) Confirm(q app.Confirmation) bool {
	var parent *mygo.Window
	if p.win.IsVisible() {
		parent = p.win // a sheet of it on macOS
	}
	r, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:        parent,
		Type:          mygo.MessageQuestion,
		Title:         q.Title,
		Message:       q.Message,
		Detail:        q.Detail,
		Buttons:       []string{q.OK, q.Cancel},
		DefaultButton: 0,
		CancelButton:  1,
	})
	if err != nil {
		// No message box: go ahead, rather than never let the app quit.
		log.Println("confirm:", err)
		return true
	}
	return r.Button == 0
}

// Quit quits as the system's Quit does: the app is asked first
// (OnBeforeQuit in main), then every window closes.
func (p *platform) Quit() { mygo.App.Quit() }

// traySVG is the app icon (docs/icon.svg) in one color, drawn in
// currentColor.
//
//go:embed tray.svg
var traySVG []byte

// trayIcon is tray.svg as a 32x32 PNG: black, for the system to tint, on
// macOS and Windows; blue on Linux, whose panels show it as it is, light
// or dark.
func trayIcon() []byte {
	var ink color.Color = color.Black
	if runtime.GOOS == "linux" {
		ink = color.RGBA{0x25, 0x63, 0xeb, 0xff}
	}
	var buf bytes.Buffer
	png.Encode(&buf, ui.MustParseSVG(traySVG).Image(32, 32, ink))
	return buf.Bytes()
}
