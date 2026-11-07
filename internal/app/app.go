// Package app is the window: its state and the views that show it.
//
// The view runs on the UI thread and reads the state freely. Goroutines
// that connect, poll and transfer change the state only through App.post,
// which runs a function on the UI thread.
package app

import (
	"slices"
	"time"

	"finalshelly/internal/config"
	"finalshelly/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Platform is what the app asks of the system. The program passes the
// real one; tests pass a fake.
type Platform interface {
	// Post runs fn on the UI thread, then draws every open window again.
	// It returns without waiting.
	Post(fn func())
	// OpenFiles asks for files to upload, or one folder with folder set:
	// nil when canceled. It blocks; the app calls it on a goroutine.
	OpenFiles(title string, folder bool) ([]string, error)
	// ChooseFolder asks for a folder to download into: "" when canceled.
	// It blocks too.
	ChooseFolder(title string) (string, error)
	// DownloadsDir is where downloads go unless the user chooses.
	DownloadsDir() string
	// ShowInFolder shows a file in the system's file manager.
	ShowInFolder(path string)
	// SetLocale shows the language of tag, or the system's for "".
	SetLocale(tag string)
	// Languages are the languages the app is written in.
	Languages() []string

	// OpenWindow opens a secondary window showing view, as spec says, a
	// child of the window of spec.Parent, or of the main window for "".
	// It calls closed once the window is gone, however it went: closed by
	// the user, by CloseWindow, or with its parent. Titles follow the
	// language as it changes.
	OpenWindow(spec WindowSpec, view func(c *ui.Context), closed func())
	// FocusWindow brings the open window of kind to the front, with the
	// keyboard.
	FocusWindow(kind WindowKind)
	// CloseWindow closes the window of kind, and its children.
	CloseWindow(kind WindowKind)
	// Redraw draws every open window again, the main one included.
	Redraw()
}

// App is the state of the window.
type App struct {
	plat     Platform
	settings config.Settings

	hosts     *config.Store     // deleted hosts included: lists show hosts.Live()
	passwords map[string]string // by host ID, for this run only
	sessions  []*Session
	active    int
	sessList  ui.ListState

	sideWidth   float32
	monitorPct  float32
	terminalPct float32
	treeWidth   float32
	bottomTab   int // 0 files, 1 transfers
	termFont    terminal.Font
	termTheme   *terminal.Theme

	// The pane sizes changed since they were saved, last at layoutAt.
	layoutDirty bool
	layoutAt    time.Time

	saveHosts    func(*config.Store) error
	saveSettings func(config.Settings) error

	transfers transferQueue

	// The connection manager.
	managerOpen bool
	hostRow     int
	hostList    ui.ListState
	form        hostForm
	formOpen    bool

	// The password prompt for a saved host.
	pwOpen bool
	pwHost *config.Host
	pwText string

	// The secondary windows that are open.
	windows map[WindowKind]bool

	// The question before replacing files.
	replace *replaceQuestion

	// The System info popover, and what reads it (readSysInfo when nil).
	sysInfo     sysInfoPopover
	readSysInfo sysInfoReader
}

// New makes the app's state, with the hosts and settings saved on disk.
func New(plat Platform) *App {
	a := &App{
		plat:         plat,
		passwords:    map[string]string{},
		windows:      map[WindowKind]bool{},
		active:       -1,
		hostRow:      -1,
		sideWidth:    defaultSideWidth,
		monitorPct:   defaultMonitorPct,
		terminalPct:  defaultTerminalPct,
		treeWidth:    defaultTreeWidth,
		termFont:     terminal.Font{Family: "JetBrains Mono, Cascadia Mono, Consolas, Menlo, DejaVu Sans Mono, monospace", Size: 13.5},
		saveHosts:    config.SaveHosts,
		saveSettings: config.SaveSettings,
	}
	if t, err := terminal.GhosttyTheme("Tomorrow Night"); err == nil {
		a.termTheme = t
	}
	a.hosts, _ = config.LoadHosts()
	a.settings = config.LoadSettings()
	a.restoreLayout()
	a.transfers.init()
	return a
}

func (a *App) post(fn func()) { a.plat.Post(fn) }

func (a *App) current() *Session {
	if a.active >= 0 && a.active < len(a.sessions) {
		return a.sessions[a.active]
	}
	return nil
}

func newTheme() *ui.Theme {
	t := ui.LightTheme()
	t.Spacing = 3
	t.FontSize = 13
	t.Font = "Microsoft YaHei UI, PingFang SC, Noto Sans CJK SC, WenQuanYi Zen Hei, system-ui"
	return t
}

var theme = newTheme()

// sidebarBG is the gray of the side panel, as FinalShell's.
var sidebarBG = ui.Hex("#f3f4f6")

// View builds the window.
func (a *App) View(c *ui.Context) {
	c.SetTheme(theme)
	a.shortcuts(c)
	a.saveLayoutAtRest(c)
	split := ui.Split(c, &a.sideWidth, func() { a.sidebar(c) }, func() { a.mainArea(c) }).SplitReset(defaultSideWidth).Fill()
	a.splitMoved(c, split, &a.sideWidth, sidebarLimits)
	a.dialogs(c)
}

// shortcuts are the window's keys, which come before the terminal's: each
// must be one no shell needs. Ctrl with a letter alone is the shell's, so
// Cmd+T is Super, Command on macOS: ui.Cmd is Ctrl elsewhere, and Ctrl+T
// transposes characters in readline.
func (a *App) shortcuts(c *ui.Context) {
	switch {
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyT), c.Shortcut(ui.Super, ui.KeyT):
		a.openManager()
	case c.Shortcut(ui.Ctrl, ui.KeyTab):
		a.cycle(1)
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab):
		a.cycle(-1)
	case c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyW):
		if s := a.current(); s != nil {
			a.closeSession(s)
		}
	}
}

func (a *App) cycle(d int) {
	if n := len(a.sessions); n > 0 {
		a.active = ((a.active+d)%n + n) % n
	}
}

func (a *App) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Background(sidebarBG).Children(func() {
		split := ui.SplitVerticalPercent(c, &a.monitorPct, func() { a.monitorPanel(c) }, func() { a.sessionPanel(c) }).SplitReset(defaultMonitorPct).Fill()
		a.splitMoved(c, split, &a.monitorPct, monitorLimits)
	})
}

func (a *App) mainArea(c *ui.Context) {
	split := ui.SplitVerticalPercent(c, &a.terminalPct, func() { a.terminalArea(c) }, func() { a.bottomPanel(c) }).SplitReset(defaultTerminalPct).Fill()
	a.splitMoved(c, split, &a.terminalPct, terminalLimits)
}

// bottomPanel is the files of the active session, and the transfers.
func (a *App) bottomPanel(c *ui.Context) {
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		ui.Row(c).PaddingX(6).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#e5e7eb")).Children(func() {
			transfersLabel := locales.TabTransfers()
			if n := a.transfers.active(); n > 0 {
				transfersLabel = locales.TabTransfersActive(n)
			}
			ui.Tabs(c, &a.bottomTab, locales.TabFiles(), transfersLabel)
		})
		switch a.bottomTab {
		case 0:
			a.filePanel(c)
		case 1:
			a.transferPanel(c)
		}
	})
}

// setLanguage shows tag, "" for the system's language, and keeps the
// choice.
func (a *App) setLanguage(tag string) {
	a.settings.Language = tag
	a.plat.SetLocale(tag)
	a.saveSettings(a.settings)
	a.redraw()
}

// openSession opens a tab for h, asking for its password first when the
// app does not know it.
func (a *App) openSession(h *config.Host) {
	pw, known := a.passwords[h.ID]
	if h.Auth == config.AuthPassword && !known {
		a.pwOpen, a.pwHost, a.pwText = true, h, ""
		return
	}
	s := newSession(h)
	a.sessions = append(a.sessions, s)
	a.active = len(a.sessions) - 1
	a.connect(s, pw)
}

func (a *App) reconnect(s *Session) {
	s.disconnect()
	a.connect(s, a.passwords[s.Host.ID])
}

func (a *App) closeSession(s *Session) {
	i := slices.Index(a.sessions, s)
	if i < 0 {
		return
	}
	a.transfers.cancelSession(s)
	s.disconnect()
	a.sessions = slices.Delete(a.sessions, i, i+1)
	if a.active >= len(a.sessions) || a.active > i {
		a.active--
	}
	if a.active < 0 && len(a.sessions) > 0 {
		a.active = 0
	}
}

// CloseAll ends every connection, as the main window closes, closes the
// secondary windows, and saves the pane sizes the user changed.
func (a *App) CloseAll() {
	a.closeWindows()
	if a.layoutDirty {
		a.saveLayout()
	}
	for _, s := range a.sessions {
		a.transfers.cancelSession(s)
		s.disconnect()
	}
}

func (a *App) moveSessions(rows []int, to int) {
	cur := a.current()
	moved := make([]*Session, 0, len(rows))
	for _, r := range rows {
		moved = append(moved, a.sessions[r])
	}
	var rest []*Session
	insert := 0
	for i, s := range a.sessions {
		if slices.Contains(moved, s) {
			continue
		}
		if i < to {
			insert++
		}
		rest = append(rest, s)
	}
	a.sessions = slices.Insert(rest, insert, moved...)
	a.active = slices.Index(a.sessions, cur)
}
