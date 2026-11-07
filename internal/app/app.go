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
	"finalshelly/internal/widgets"
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
	// OpenFile asks for one file to read, as a private key, starting in
	// dir ("" for the system's choice), over the window of parent, or the
	// main window when that is not open: "" when canceled. It blocks too.
	OpenFile(parent WindowKind, title, dir string) (string, error)
	// OpenImage asks for an image file, over the window of parent as
	// OpenFile does: "" when canceled. It blocks too.
	OpenImage(parent WindowKind, title string) (string, error)
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
	// keyboard; WindowMain the main window, out of the tray too.
	FocusWindow(kind WindowKind)
	// CloseWindow closes the window of kind, and its children.
	CloseWindow(kind WindowKind)
	// Redraw draws every open window again, the main one included.
	Redraw()

	// HideToTray hides the main window to an
	// icon in the tray (the menu bar on macOS) whose menu shows it again
	// or quits. It returns false, leaving the window as it is, when the
	// system has no tray.
	HideToTray() bool
	// Confirm asks q in a message box and tells whether the user chose OK.
	// It blocks; the app calls it on a goroutine.
	Confirm(q Confirmation) bool
	// Quit quits the app, as the tray's Quit does: the platform asks
	// QuitRequested first, then closes every window.
	Quit()

	// SecretGet returns the secret kept under key in the system's
	// credential store, with ok false when there is none. SecretSet keeps
	// value under key, replacing what it had; SecretDelete removes it,
	// and deleting a key without one is no error. Where the system has no
	// store they fail with an error for which errors.Is(err,
	// errors.ErrUnsupported) holds. They may block for long, while the
	// system asks the user: the app calls them on a goroutine
	// (secrets.go).
	SecretGet(key string) (value string, ok bool, err error)
	SecretSet(key, value string) error
	SecretDelete(key string) error
}

// App is the state of the window.
type App struct {
	plat     Platform
	settings config.Settings

	hosts     *config.Store     // deleted hosts included: lists show hosts.Live()
	passwords map[string]string // by host ID: what the host logs in with, this run
	secrets   secretStore       // the system's credential store (secrets.go)
	sessions  []*Session        // new tabs included
	active    int
	sessList  ui.ListState
	start     startState // the start page shown while no row is selected

	sideWidth   float32
	monitorPct  float32
	terminalPct float32
	treeWidth   float32
	bottomTab   int // 0 files, 1 transfers, 2 tunnels
	termFont    terminal.Font
	termTheme   *terminal.Theme
	look        appearance // the rest of the terminal's look

	// The pane sizes changed since they were saved, last at layoutAt.
	layoutDirty bool
	layoutAt    time.Time

	saveHosts    func(*config.Store) error
	saveSettings func(config.Settings) error

	transfers transferQueue

	// The connection manager's window.
	manager managerState

	// The connection editor's fields, while its window is open.
	editor *connEditor

	// The password prompt: the password a connection takes (a saved
	// host's, a jump host's or a proxy's), or a key's passphrase, and
	// what to do once it is entered (see proxy.go and secrets.go).
	pwOpen     bool
	pwNeed     *secretNeed
	pwThen     func()
	pwText     string
	pwRemember bool  // "Remember password"
	pwErr      error // why the last one did not work, or nil

	// The secondary windows that are open.
	windows map[WindowKind]bool

	// The options window's page.
	optionsNav widgets.NavState
	// Quitting: asking whether to, or told to go ahead.
	askingQuit, quitApproved bool

	// The question before replacing files.
	replace *replaceQuestion

	// The System info popover, and what reads it (readSysInfo when nil).
	sysInfo     sysInfoPopover
	readSysInfo sysInfoReader

	// The shortcuts page (shortcutspage.go), and full screen, which shows
	// the terminal alone (fullscreen.go).
	shortcutsUI shortcutsPage
	fullScreen  fullScreenState
}

// New makes the app's state, with the hosts and settings saved on disk.
func New(plat Platform) *App {
	a := &App{
		plat:         plat,
		passwords:    map[string]string{},
		windows:      map[WindowKind]bool{},
		active:       -1,
		manager:      managerState{row: -1},
		start:        startState{row: -1},
		sideWidth:    defaultSideWidth,
		monitorPct:   defaultMonitorPct,
		terminalPct:  defaultTerminalPct,
		treeWidth:    defaultTreeWidth,
		saveHosts:    config.SaveHosts,
		saveSettings: config.SaveSettings,
	}
	a.hosts, _ = config.LoadHosts()
	a.settings = config.LoadSettings()
	a.loadAppearance()
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
	if a.fullScreen.on {
		a.terminalArea(c)
		a.dialogs(c)
		return
	}
	split := ui.Split(c, &a.sideWidth, func() { a.sidebar(c) }, func() { a.mainArea(c) }).SplitReset(defaultSideWidth).Fill()
	a.splitMoved(c, split, &a.sideWidth, sidebarLimits)
	a.dialogs(c)
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
			tunnelsLabel := locales.TabTunnels()
			if s := a.current(); s != nil && s.listening() > 0 {
				tunnelsLabel = locales.TabTunnelsActive(s.listening())
			}
			ui.Tabs(c, &a.bottomTab, locales.TabFiles(), transfersLabel, tunnelsLabel)
		})
		switch a.bottomTab {
		case 0:
			a.filePanel(c)
		case 1:
			a.transferPanel(c)
		case 2:
			a.tunnelPanel(c)
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

// openSession opens a session to h: in the selected row when it is a new
// tab, else in a tab of its own.
func (a *App) openSession(h *config.Host) { a.openSessionIn(a.selectedNewTab(), h) }

// openSessionIn opens a session to h in tab, a new tab, or in a tab of
// its own for nil. It reads the passwords connecting takes from the
// system's credential store first, those the app does not know yet, and
// asks for those the store does not have either: h's own, then its
// proxy's and jump hosts', one at a time.
func (a *App) openSessionIn(tab *Session, h *config.Host) {
	if a.readChainSecrets(h, func() { a.openSessionIn(tab, h) }) {
		return
	}
	if need := a.missingSecret(h); need != nil {
		a.askSecret(need, func() { a.openSessionIn(tab, h) })
		return
	}
	pw := a.passwords[h.ID]
	if tab != nil && tab.isNewTab() && slices.Contains(a.sessions, tab) {
		a.fillTab(tab, h, pw)
		return
	}
	s := newSession(h)
	a.sessions = append(a.sessions, s)
	if !a.settings.NoAutoSelectTab || a.current() == nil {
		a.active = len(a.sessions) - 1
	}
	a.connect(s, pw)
}

func (a *App) reconnect(s *Session) {
	if s.isNewTab() || !slices.Contains(a.sessions, s) {
		return
	}
	// A jump host or a proxy with a password not known yet, as the host
	// was edited to go through it, or one forgotten as it did not work.
	if a.readChainSecrets(s.Host, func() { a.reconnect(s) }) {
		return
	}
	if need := a.missingSecret(s.Host); need != nil {
		a.askSecret(need, func() { a.reconnect(s) })
		return
	}
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
