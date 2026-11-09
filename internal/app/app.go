// Package app is the window: its state and the views that show it.
//
// The view runs on the UI thread and reads the state freely. Goroutines
// that connect, poll and transfer change the state only through App.post,
// which runs a function on the UI thread.
package app

import (
	"slices"
	"time"

	"finallyshell/internal/cloudsync"
	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

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
	// ChooseFolder asks for a folder to download into, over the window of
	// parent as OpenFile does: "" when canceled. It blocks too.
	ChooseFolder(parent WindowKind, title string) (string, error)
	// OpenFile asks for one file to read, as a private key, starting in
	// dir ("" for the system's choice), over the window of parent, or the
	// main window when that is not open: "" when canceled. It blocks too.
	OpenFile(parent WindowKind, title, dir string) (string, error)
	// OpenImage asks for an image file, over the window of parent as
	// OpenFile does: "" when canceled. It blocks too.
	OpenImage(parent WindowKind, title string) (string, error)
	// OpenVSIX asks for a VSIX file (a VS Code extension), over the window
	// of parent as OpenFile does: "" when canceled. It blocks too.
	OpenVSIX(parent WindowKind, title string) (string, error)
	// OpenFolder asks for a folder to read, as FinalShell's, over the
	// window of parent as OpenFile does, hidden folders shown: "" when
	// canceled. It blocks too.
	OpenFolder(parent WindowKind, title string) (string, error)
	// OpenFileOfType asks for a file to read, as a list of connections to
	// import, with one of extensions (without the dot; any file for none),
	// files of which typeName names, over the window of parent as
	// OpenFile does, hidden files shown: "" when canceled. It blocks too.
	OpenFileOfType(parent WindowKind, title, typeName string, extensions []string) (string, error)
	// SaveFile asks where to save a file, as a template to fill in,
	// starting at path (its folder and name), with one of extensions,
	// files of which typeName names, over the window of parent as
	// OpenFile does: "" when canceled. It blocks too.
	SaveFile(parent WindowKind, title, path, typeName string, extensions []string) (string, error)
	// DownloadsDir is where downloads go unless the user chooses.
	DownloadsDir() string
	// ShowInFolder shows a file in the system's file manager.
	ShowInFolder(path string)
	// OpenPath opens a file with the system's default app for it, or a
	// folder in the file manager.
	OpenPath(path string) error
	// ShowTransfers shows on the app's icon (the Dock on macOS, the
	// taskbar button on Windows) how many transfers are under way, and
	// the fraction of their bytes done: -1 when none knows its size yet.
	// Zero transfers clear both. See downloads.go.
	ShowTransfers(count int, progress float64)
	// TempDir is a folder of the app's for the copies of remote files it
	// opens with other apps.
	TempDir() string
	// SetLocale shows the language of tag, or the system's for "".
	SetLocale(tag string)
	// Languages are the languages the app is written in.
	Languages() []string
	// SetAppearance makes every window light or dark, native title bars
	// included, or follow the system's appearance.
	SetAppearance(a config.Appearance)

	// OpenWindow opens a secondary window showing view, as spec says, a
	// child of the window of spec.Parent, or of the main window for "".
	// It calls closed once the window is gone, however it went: closed by
	// the user, by CloseWindow, or with its parent. Titles follow the
	// language as it changes.
	OpenWindow(spec WindowSpec, view func(c *ui.Context), closed func())
	// RetitleWindow sets the title of the open window of kind again, from
	// its spec's Title, as it changed.
	RetitleWindow(kind WindowKind)
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

	// Updates reports what the updater knows now (options_updates.go). It
	// is cheap: views call it as they draw. When it changes, as a check
	// finds a version, the platform calls Redraw.
	Updates() UpdateStatus
	// CheckForUpdates checks as the user asked: the update window shows
	// at once. It returns without waiting.
	CheckForUpdates()
	// SetAutomaticUpdateChecks turns checking in the background on or off,
	// and keeps the choice.
	SetAutomaticUpdateChecks(on bool)
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

	rename renameState // the session row being renamed (rename.go)

	sideWidth   float32
	monitorPct  float32
	terminalPct float32
	treeWidth   float32
	bottomTab   int // tabFiles, tabTransfers, …
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

	// The left panel's page, and the tree of the Connections page
	// (conntree.go).
	left     leftPanel
	connTree managerState

	// The connection editor's fields, while its window is open.
	editor *connEditor

	// The import window's state (import.go); its sources,
	// allImportSources() unless tests replace them, and the kind of the
	// one chosen last, "" for the first.
	imp           *importState
	importSources []importSource
	importKind    importer.Kind

	// The password prompt: the password a connection takes (a saved
	// host's, a jump host's or a proxy's), or a key's passphrase, and
	// what to do once it is entered (see proxy.go and secrets.go).
	pwOpen     bool
	pwNeed     *secretNeed
	pwThen     func()
	pwCancel   func() // on Cancel, for "Connect all" (connectall.go)
	pwText     string
	pwRemember bool  // "Remember password"
	pwErr      error // why the last one did not work, or nil

	// The secondary windows that are open.
	windows map[WindowKind]bool

	// The editor of remote files (fileeditor.go).
	fe fileEditor

	// The options window's page.
	optionsNav widgets.NavState
	// Quitting: asking whether to, or told to go ahead.
	askingQuit, quitApproved bool

	// The question before replacing files.
	replace *replaceQuestion
	// The file panel's dialog: a rename, a new folder or file, a delete.
	fileDlg *fileDialog

	// The System info popover, and what reads it (readSysInfo when nil).
	sysInfo     sysInfoPopover
	readSysInfo sysInfoReader

	// The shortcuts page (shortcutspage.go), and full screen, which shows
	// the terminal alone (fullscreen.go).
	shortcutsUI shortcutsPage
	fullScreen  fullScreenState

	// The file panel's icon theme (fileicons.go), and the home folder,
	// where editors keep the extensions it lists: homeDir, which tests
	// replace.
	iconThemes iconThemeState
	home       func() string

	// Sync (sync.go), and how it makes its remote: syncRemote, which
	// tests replace.
	sync      syncState
	newRemote func(config.SyncSettings, string) (cloudsync.Remote, error)

	// The command bar's history of each host (commandbar_data.go).
	history cmdHistories

	// The polls of the bottom panel's tabs (polling.go), and the
	// Processes and Network tabs' shared state (processes.go,
	// network.go), with where IP addresses are (geo.go).
	polls     pollSet
	processes processesUI
	network   networkUI
	geo       geoState

	// The Commands tab's quick commands (quickcmds.go).
	quickUI quickState
}

// New makes the app's state, with the hosts and settings saved on disk.
func New(plat Platform) *App {
	a := &App{
		plat:         plat,
		passwords:    map[string]string{},
		windows:      map[WindowKind]bool{},
		active:       -1,
		manager:      managerState{row: -1, win: WindowManager},
		connTree:     managerState{row: -1, win: WindowMain},
		start:        startState{row: -1},
		sideWidth:    defaultSideWidth,
		monitorPct:   defaultMonitorPct,
		terminalPct:  defaultTerminalPct,
		treeWidth:    defaultTreeWidth,
		processes:    processesUI{listPct: defaultProcListPct, fieldsPct: defaultProcFieldsPct},
		network:      networkUI{listPct: defaultNetListPct},
		saveHosts:    config.SaveHosts,
		saveSettings: config.SaveSettings,

		importSources: allImportSources(),
		home:          homeDir,
		newRemote:     syncRemote,
	}
	a.hosts, _ = config.LoadHosts()
	a.settings = config.LoadSettings()
	a.loadAppearance()
	a.restoreLayout()
	a.restoreLeftPanel()
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

// View builds the window.
func (a *App) View(c *ui.Context) {
	defer a.stopUnseenPolls()
	useTheme(c)
	a.shortcuts(c)
	a.saveLayoutAtRest(c)
	a.syncTick(c)
	if a.fullScreen.on {
		if s := a.current(); s.isPanelTab() {
			a.panelArea(c, s)
		} else {
			a.terminalArea(c)
		}
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
	ui.Column(c).Fill().Background(colors.sidebar).Children(func() {
		split := ui.SplitVerticalPercent(c, &a.monitorPct, func() { a.monitorPanel(c) }, func() { a.sessionPanel(c) }).SplitReset(defaultMonitorPct).Fill()
		a.splitMoved(c, split, &a.monitorPct, monitorLimits)
	})
}

// mainArea is the terminal over the bottom panel, or a panel row's panel
// (paneltab.go).
func (a *App) mainArea(c *ui.Context) {
	if s := a.current(); s.isPanelTab() {
		a.panelArea(c, s)
		return
	}
	split := ui.SplitVerticalPercent(c, &a.terminalPct, func() { a.terminalArea(c) }, func() { a.bottomPanel(c, &a.bottomTab) }).SplitReset(defaultTerminalPct).Fill()
	a.splitMoved(c, split, &a.terminalPct, terminalLimits)
}

// The bottom panel's tabs, in order: their index in a.bottomTab, a panel
// row's panelTab and bottomTabs.
const (
	tabFiles = iota
	tabTransfers
	tabTunnels
	tabProcesses
	tabNetwork
	tabCommands
)

// bottomTab is a tab of the bottom panel: its label, and its page.
type bottomTab struct {
	label string
	page  func(c *ui.Context)
}

// bottomTabs are the bottom panel's tabs, by their constants.
func (a *App) bottomTabs() []bottomTab {
	transfersLabel := locales.TabTransfers()
	if n := a.transfers.active(); n > 0 {
		transfersLabel = locales.TabTransfersActive(n)
	}
	tunnelsLabel := locales.TabTunnels()
	if s := a.connSession(); s != nil && s.listening() > 0 {
		tunnelsLabel = locales.TabTunnelsActive(s.listening())
	}
	return []bottomTab{
		tabFiles:     {locales.TabFiles(), a.filePanel},
		tabTransfers: {transfersLabel, a.transferPanel},
		tabTunnels:   {tunnelsLabel, a.tunnelPanel},
		tabProcesses: {locales.TabProcesses(), a.processPanel},
		tabNetwork:   {locales.TabNetwork(), a.networkPanel},
		tabCommands:  {locales.TabCommands(), a.quickPanel},
	}
}

// bottomPanel is the files of the active session, the transfers, and
// the session's other tabs, on the tab *tab: a.bottomTab under the
// terminal, a panel row's own in it.
func (a *App) bottomPanel(c *ui.Context, tab *int) {
	tabs := a.bottomTabs()
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		ui.Row(c).PaddingX(6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
			labels := make([]string, len(tabs))
			for i, t := range tabs {
				labels[i] = t.label
			}
			ui.Tabs(c, tab, labels...)
			ui.Spacer(c)
			a.panelCornerButton(c, *tab)
		})
		if *tab >= 0 && *tab < len(tabs) {
			tabs[*tab].page(c)
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
	if tab != nil && tab.isNewTab() && !tab.isPanelTab() && slices.Contains(a.sessions, tab) {
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

// closeSession closes the row of s: its session, with its panel row, or
// a panel row alone, which ends nothing.
func (a *App) closeSession(s *Session) {
	i := slices.Index(a.sessions, s)
	if i < 0 {
		return
	}
	if !s.isPanelTab() {
		if p := a.panelRowOf(s); p != nil {
			a.closeSession(p)
			i = slices.Index(a.sessions, s)
		}
		a.transfers.cancelSession(s)
		s.disconnect()
	}
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
	a.fe.discard = true // the editor's changes go with the app
	a.closeWindows()
	if a.layoutDirty {
		a.saveLayout()
	}
	for _, s := range a.sessions {
		if s.isPanelTab() {
			continue // its session's
		}
		a.transfers.cancelSession(s)
		s.disconnect()
	}
}

// moveSessions moves the rows dragged before the row to, as the list
// asks. A session takes its panel row along, right below it; a panel row
// does not move alone, and a drop between a session and its panel row
// goes below the panel row.
func (a *App) moveSessions(rows []int, to int) {
	cur := a.current()
	moved := make([]*Session, 0, len(rows))
	for _, r := range rows {
		s := a.sessions[r]
		if s.isPanelTab() || slices.Contains(moved, s) {
			continue
		}
		moved = append(moved, s)
		if p := a.panelRowOf(s); p != nil {
			moved = append(moved, p)
		}
	}
	if len(moved) == 0 {
		return
	}
	if to < len(a.sessions) && a.sessions[to].isPanelTab() {
		to++
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
