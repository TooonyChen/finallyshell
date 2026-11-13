// Package config keeps the app's files on disk: the saved hosts and
// folders (hosts.json), the settings, and the directory they live in.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Dir is where the app keeps its files: $FINALLYSHELL_DIR, else
// <user config dir>/finallyshell.
func Dir() string {
	if d := os.Getenv("FINALLYSHELL_DIR"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "finallyshell")
}

// Settings are the app's preferences.
type Settings struct {
	// Language is the language the user chose, as "en" or "zh-Hans", or ""
	// to follow the system's.
	Language string `json:"language,omitempty"`
	// Appearance is whether the app is light or dark: AppearanceLight,
	// AppearanceDark, or AppearanceSystem (or "") to follow the system's.
	Appearance Appearance `json:"appearance,omitempty"`

	// The sizes of the window's panes, as the user last dragged their
	// dividers: the sidebar's and the folder tree's widths in DIPs, the
	// server monitor's share of the sidebar and the terminal's of the main
	// area in percent. Zero is the app's default.
	SidebarWidth    float32 `json:"sidebarWidth,omitempty"`
	MonitorPercent  float32 `json:"monitorPercent,omitempty"`
	TerminalPercent float32 `json:"terminalPercent,omitempty"`
	TreeWidth       float32 `json:"treeWidth,omitempty"`

	// MonitorHidden are the parts of the server monitor the user hid, by
	// key ("uptime", "cpu", "processes", "network", "latency", "disks"),
	// the same for every host. It lists the hidden ones so that a part
	// added later shows.
	MonitorHidden Names `json:"monitorHidden,omitempty"`

	// The dividers of the Processes and Network tabs, in percent of the
	// room, shared by the sessions: the process list's share above its
	// details, the details' fields' share beside the environment, and the
	// network table's share above its tabs. Zero is the app's default.
	ProcListPercent   float32 `json:"procListPercent,omitempty"`
	ProcFieldsPercent float32 `json:"procFieldsPercent,omitempty"`
	NetListPercent    float32 `json:"netListPercent,omitempty"`

	// BottomPanelHidden collapses the bottom panel under the terminal to
	// its row of tabs, as the user left it.
	BottomPanelHidden bool `json:"bottomPanelHidden,omitempty"`
	// BottomPanelSide is where the bottom panel is: "" under the
	// terminal, PanelRight in a column on the terminal's right.
	BottomPanelSide string `json:"bottomPanelSide,omitempty"`
	// RightPanelWidth is the width of the column on the terminal's right,
	// in DIPs, as last dragged. Zero is the app's default.
	RightPanelWidth float32 `json:"rightPanelWidth,omitempty"`
	// MonitorSide is where the server monitor is: "" at the top left,
	// over the session list, PanelRight in the column on the terminal's
	// right.
	MonitorSide string `json:"monitorSide,omitempty"`
	// RightMonitorPercent is the monitor's share of the right column's
	// height above the bottom panel, when both are there. Zero is the
	// app's default.
	RightMonitorPercent float32 `json:"rightMonitorPercent,omitempty"`

	// The command editor of the Commands tab, this device's own as the
	// pane sizes; its text is in CommandEditorFile.
	//
	// CommandEditorShown shows it, as the user left it.
	CommandEditorShown bool `json:"commandEditorShown,omitempty"`
	// CommandEditorWidth is its width in DIPs beside the command
	// buttons, and CommandEditorPercent the buttons' share of the tab's
	// height above it where the tab is too narrow to have it beside
	// them, as last dragged. Zero is the app's default.
	CommandEditorWidth   float32 `json:"commandEditorWidth,omitempty"`
	CommandEditorPercent float32 `json:"commandEditorPercent,omitempty"`
	// CommandEditorNoEnter sends the last line of what the editor sends
	// without Enter, for the user to check it; by default every line
	// runs.
	CommandEditorNoEnter bool `json:"commandEditorNoEnter,omitempty"`
	// CommandEditorClear empties the editor of what it sent.
	CommandEditorClear bool `json:"commandEditorClear,omitempty"`

	// The General page of the options. Each is named so that false, as a
	// file without it reads, is the default.
	//
	// NoAutoSelectTab opens new sessions in the background, keeping the
	// selected one; by default a new session is selected.
	NoAutoSelectTab bool `json:"noAutoSelectTab,omitempty"`
	// MinimizeToTray hides the main window to a tray icon when it is
	// closed, rather than quitting.
	MinimizeToTray bool `json:"minimizeToTray,omitempty"`
	// NoConfirmClose quits without asking while sessions are open; by
	// default closing the window, or quitting, asks first.
	NoConfirmClose bool `json:"noConfirmClose,omitempty"`
	// NoConfirmCloseSession closes a connected or connecting SSH session without asking.
	NoConfirmCloseSession bool `json:"noConfirmCloseSession,omitempty"`
	// KeepAliveSeconds is the seconds between the keepalives sent on
	// each connection: 0, as a file without it reads, for
	// DefaultKeepAliveSeconds, and KeepAliveOff (any negative number)
	// for none. Read it with KeepAliveInterval.
	KeepAliveSeconds int `json:"keepAliveSeconds,omitempty"`
	// SSHAgent is the address of the SSH agent hosts that log in with
	// one use (AuthAgent): a Unix socket's path, ~ for the home folder,
	// or on Windows a named pipe, \\.\pipe\<name>. "" for the default:
	// SSH_AUTH_SOCK, or the Windows OpenSSH agent's pipe.
	SSHAgent string `json:"sshAgent,omitempty"`

	// The Mouse page of the terminal options, zero by default too.
	//
	// CopyOnSelect copies what the mouse selects in a terminal to the
	// clipboard as the selection ends.
	CopyOnSelect bool `json:"copyOnSelect,omitempty"`
	// RightClick is what a right click in a terminal does: the context
	// menu when "".
	RightClick RightClick `json:"rightClick,omitempty"`
	// NoLinkClick keeps Ctrl+click (Cmd+click on macOS) from opening the
	// links a terminal shows.
	NoLinkClick bool `json:"noLinkClick,omitempty"`
	// WheelLines is how many lines a notch of the mouse wheel scrolls a
	// terminal: 3 when 0.
	WheelLines int `json:"wheelLines,omitempty"`

	// The Shortcuts page of the options (Terminal › Shortcuts).
	//
	// Shortcuts are the keys the user rebound; an action not there has
	// its default.
	Shortcuts Shortcuts `json:"shortcuts,omitzero"`
	// OptionAsAlt makes Option the shell's Alt (Meta) key on macOS, so
	// that Option+B sends Esc b, rather than the key that types ∫ and
	// other symbols.
	OptionAsAlt bool `json:"optionAsAlt,omitempty"`

	// The Terminal page of the options: the defaults of the hosts whose
	// own setting is "Default".
	//
	// TermBackspace is what Backspace sends: "" for ^?, or
	// BackspaceCtrlH. Read it with TermBackspaceKey or HostBackspace.
	TermBackspace BackspaceKey `json:"termBackspace,omitempty"`
	// TermDelete is what Delete sends: "" for ESC [ 3 ~, DeleteDEL or
	// DeleteCtrlH. Read it with TermDeleteKey or HostDelete.
	TermDelete DeleteKey `json:"termDelete,omitempty"`
	// TermEncoding is the character encoding of the terminal: "" for
	// UTF-8, else one of Encodings. Read it with TermEncodingName or
	// HostEncoding.
	TermEncoding Encoding `json:"termEncoding,omitempty"`
	// LocalShell is the program a local terminal runs, with its
	// arguments, as a command line split into words as a shell would
	// (LocalShellCommand): "" for the terminal's default, the login
	// shell, or PowerShell on Windows. It is this device's own, as paths
	// differ between computers.
	LocalShell string `json:"localShell,omitempty"`

	// The terminal's look: the Font, Color scheme and Background image
	// pages of the options. Zero is the default of each.
	//
	// TermFontFamily is the terminal's font, a comma-separated list of
	// families of which the first installed one is used: "" for the app's
	// list of coding fonts (DefaultTermFontFamily).
	TermFontFamily string `json:"termFontFamily,omitempty"`
	// TermFontSize is the font's size in DIPs: 0 for DefaultTermFontSize.
	TermFontSize float32 `json:"termFontSize,omitempty"`
	// TermLineHeight is the height of rows as a multiple of the font's
	// own line height: 0 for 1.
	TermLineHeight float32 `json:"termLineHeight,omitempty"`
	// TermFontWeight is how heavy text is, from 100 to 900, of the
	// weights the font has: 0 for 400. Bold text is 300 heavier, and at
	// least 700.
	TermFontWeight int `json:"termFontWeight,omitempty"`
	// TermNoLigatures turns off the font's ligatures (OpenType calt and
	// liga), which are on by default.
	TermNoLigatures bool `json:"termNoLigatures,omitempty"`
	// TermTheme is the color scheme, the name of a theme of Ghostty's: ""
	// for DefaultTermTheme.
	TermTheme string `json:"termTheme,omitempty"`
	// TermBackground is the background image: the file name of the app's
	// copy of it, in Dir(); "" for none.
	TermBackground string `json:"termBackground,omitempty"`
	// TermBackgroundFit is how the image fills the terminal: "" to cover
	// it, cropped, BackgroundContain to fit inside it, BackgroundTile to
	// repeat it at its own size.
	TermBackgroundFit string `json:"termBackgroundFit,omitempty"`
	// TermBackgroundOpacity is how much of the image shows through the
	// color scheme's background, in percent from 1 to 100: 0 for
	// DefaultBackgroundOpacity.
	TermBackgroundOpacity float32 `json:"termBackgroundOpacity,omitempty"`

	// The Transfers page of the options.
	//
	// DownloadDir is the folder downloads go into: "" for the system's
	// downloads folder (~/Downloads).
	DownloadDir string `json:"downloadDir,omitempty"`
	// AskDownloadDir asks for the folder of each download, rather than
	// putting it in DownloadDir.
	AskDownloadDir bool `json:"askDownloadDir,omitempty"`

	// KeepManagerOpen leaves the connection manager open after it
	// connects; by default connecting from it closes it, as FinalShell's
	// "Close window after connecting", checked, does.
	KeepManagerOpen bool `json:"keepManagerOpen,omitempty"`

	// The panel under the server monitor, this device's own layout.
	//
	// LeftPanelPage is the page it shows: "" for the session tabs,
	// LeftPanelConnections for the tree of the saved connections.
	LeftPanelPage string `json:"leftPanelPage,omitempty"`
	// ConnectionTreeExpanded are the IDs of the folders open in that
	// tree, which stay as folders are renamed and moved: nil, before the
	// tree's folders were first opened or closed, opens every folder, and
	// an empty list none. A pointer keeps Settings comparable; it is
	// replaced, never changed through.
	ConnectionTreeExpanded *[]string `json:"connectionTreeExpanded,omitempty"`

	// FileIconTheme is the file panel's icon theme, as package fileicons
	// names it: "" for Material Icon Theme, "minimal" for the app's own
	// file and folder icons, "bundled:seti" for Seti, or
	// "<publisher>.<extension>/<theme>" for one installed from a VSIX or in
	// an editor such as VS Code.
	FileIconTheme string `json:"fileIconTheme,omitempty"`
	// NoFollowTerminalDir keeps the file panel where the user browsed it
	// when the shell's working directory changes; by default the panel
	// goes to it (the toggle in the panel's toolbar).
	NoFollowTerminalDir bool `json:"noFollowTerminalDir,omitempty"`
	// NoAgentIcons hides the logo of the coding agent running in a
	// session's shell, which its tab shows by default, and leaves out the
	// reads of the server that tell it (Options › Terminal).
	NoAgentIcons bool `json:"noAgentIcons,omitempty"`

	// Sync is the Sync page of the options: where connections and
	// settings are synced, if anywhere (sync.go).
	Sync SyncSettings `json:"sync,omitzero"`

	// The Command bar page of the options (Terminal › Command bar), all
	// on by default, so each is named for turning it off.
	//
	// NoCommandBar hides the command bar under the terminal in every
	// session, and with it everything below: no popups, no history read
	// or kept, no command run on the server for it.
	NoCommandBar bool `json:"noCommandBar,omitempty"`
	// NoCommandBarPaths turns off the path picker that Tab opens.
	NoCommandBarPaths bool `json:"noCommandBarPaths,omitempty"`
	// NoCommandBarHistory turns off the history picker (Up, Alt+H), and
	// keeping the commands the bar sends.
	NoCommandBarHistory bool `json:"noCommandBarHistory,omitempty"`
	// NoCommandBarCommands turns off completing command names with Tab,
	// which lists the commands on the server's PATH once a session.
	NoCommandBarCommands bool `json:"noCommandBarCommands,omitempty"`
	// NoServerHistory keeps the history picker from reading the server's
	// ~/.bash_history and ~/.zsh_history.
	NoServerHistory bool `json:"noServerHistory,omitempty"`

	// The Network monitor page of the options.
	//
	// GeoLocations is whether the Network tab shows where IP addresses
	// are, from ip2region's data, downloaded into GeoDir only once the
	// user agrees: "" before the user was asked, GeoOn, or GeoOff when
	// the user said "Don't ask again" or turned it off.
	GeoLocations string `json:"geoLocations,omitempty"`
	// GeoLocationsV6 is the same for the IPv6 data, which is asked for
	// apart, the first time an IPv6 address shows.
	GeoLocationsV6 string `json:"geoLocationsV6,omitempty"`
	// GeoOnline looks up online (ip-api.com) the addresses ip2region
	// leaves without a city or ISP, while locations are shown.
	GeoOnline bool `json:"geoOnline,omitempty"`
}

// LeftPanelConnections is Settings.LeftPanelPage showing the
// connections.
const LeftPanelConnections = "connections"

// PanelRight is Settings.BottomPanelSide with the panel, and
// Settings.MonitorSide with the monitor, in the column on the terminal's
// right.
const PanelRight = "right"

// The keepalive of Settings.KeepAliveSeconds.
const (
	DefaultKeepAliveSeconds = 30
	MaxKeepAliveSeconds     = 3600
	// KeepAliveOff is KeepAliveSeconds with keepalives off, which the
	// options show as 0.
	KeepAliveOff = -1
)

// KeepAliveInterval is the seconds between keepalives, 0 when they are
// off.
func (s Settings) KeepAliveInterval() int {
	switch {
	case s.KeepAliveSeconds < 0:
		return 0
	case s.KeepAliveSeconds == 0:
		return DefaultKeepAliveSeconds
	}
	return min(s.KeepAliveSeconds, MaxKeepAliveSeconds)
}

// SetKeepAliveInterval sets KeepAliveSeconds to n seconds, 0 for off.
func (s *Settings) SetKeepAliveInterval(n int) {
	switch {
	case n <= 0:
		s.KeepAliveSeconds = KeepAliveOff
	case n == DefaultKeepAliveSeconds:
		s.KeepAliveSeconds = 0 // the default, written as nothing
	default:
		s.KeepAliveSeconds = min(n, MaxKeepAliveSeconds)
	}
}

// The values of GeoLocations and GeoLocationsV6.
const (
	GeoOn  = "on"
	GeoOff = "off"
)

// GeoDir is where the IP location data the user downloads is kept, with
// the cache of the online lookups.
func GeoDir() string { return filepath.Join(Dir(), "geo") }

// IconThemesDir is where the VSIX files of the icon themes the user
// installs are kept.
func IconThemesDir() string { return filepath.Join(Dir(), "icon-themes") }

// Appearance is whether the app is light or dark (Settings.Appearance).
type Appearance string

const (
	AppearanceSystem Appearance = "system" // the system's appearance
	AppearanceLight  Appearance = "light"
	AppearanceDark   Appearance = "dark"
)

// Normal returns a as one of the three appearances: a value this version
// does not know, "" included, follows the system.
func (a Appearance) Normal() Appearance {
	if a == AppearanceLight || a == AppearanceDark {
		return a
	}
	return AppearanceSystem
}

// RightClick is what a right click in a terminal does (Settings.RightClick).
// A value this version does not know shows the context menu.
type RightClick string

const (
	RightClickMenu        RightClick = ""              // the context menu
	RightClickPaste       RightClick = "paste"         // paste the clipboard
	RightClickCopy        RightClick = "copy"          // copy the selection
	RightClickCopyOrPaste RightClick = "copy-or-paste" // copy, or paste with no selection
	RightClickIgnore      RightClick = "ignore"        // nothing
)

// The defaults of the terminal's look, which Settings leaves zero.
const (
	DefaultTermFontFamily    = "JetBrains Mono, Cascadia Mono, Consolas, Menlo, DejaVu Sans Mono, monospace"
	DefaultTermFontSize      = 13.5
	DefaultTermTheme         = "Tomorrow Night"
	DefaultBackgroundOpacity = 30
)

// The values of Settings.TermBackgroundFit besides "", which covers the
// terminal.
const (
	BackgroundContain = "contain"
	BackgroundTile    = "tile"
)

func settingsPath() string { return filepath.Join(Dir(), "settings.json") }

// LoadSettings reads the settings; the defaults when there is no file.
func LoadSettings() Settings {
	var s Settings
	if data, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

// SaveSettings writes the settings.
func SaveSettings(s Settings) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0o600)
}
