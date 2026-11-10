package config

// Shortcuts are the keys the user rebound on the Shortcuts page of the
// options, one field an action. Each holds a key as "Ctrl+Shift+C" or
// "Super+Enter" (Super is Command on macOS), "none" for an action the
// user left without a key, or "" for the action's default on the
// platform: only what the user changed is written.
type Shortcuts struct {
	Copy              string `json:"copy,omitempty"`
	Paste             string `json:"paste,omitempty"`
	Find              string `json:"find,omitempty"`
	FullScreen        string `json:"fullScreen,omitempty"`
	Reconnect         string `json:"reconnect,omitempty"`
	PreviousTab       string `json:"previousTab,omitempty"`
	NextTab           string `json:"nextTab,omitempty"`
	CloseTab          string `json:"closeTab,omitempty"`
	ConnectionManager string `json:"connectionManager,omitempty"`
	NewTab            string `json:"newTab,omitempty"`
	// SwitchTab is the modifiers that go with the digits 1 to 9, which
	// switch to the tab of their number, as "Alt+1-9".
	SwitchTab string `json:"switchTab,omitempty"`
	// FocusCommandBar moves the keyboard to the command bar under the
	// terminal, and back.
	FocusCommandBar string `json:"focusCommandBar,omitempty"`
	// SwitchLeftPanel switches the panel under the server monitor between
	// the session tabs and the connections.
	SwitchLeftPanel string `json:"switchLeftPanel,omitempty"`
	// ToggleBottomPanel collapses the bottom panel under the terminal to
	// its row of tabs, and opens it again.
	ToggleBottomPanel string `json:"toggleBottomPanel,omitempty"`
	// SplitRight and SplitDown open a pane beside the focused one of a
	// session, or under it; ZoomPane shows the focused pane alone, and
	// all again; the FocusPane keys move the focus to the pane on that
	// side, and the MovePane keys trade the focused pane's place with
	// that pane's.
	SplitRight     string `json:"splitRight,omitempty"`
	SplitDown      string `json:"splitDown,omitempty"`
	ZoomPane       string `json:"zoomPane,omitempty"`
	FocusPaneLeft  string `json:"focusPaneLeft,omitempty"`
	FocusPaneRight string `json:"focusPaneRight,omitempty"`
	FocusPaneUp    string `json:"focusPaneUp,omitempty"`
	FocusPaneDown  string `json:"focusPaneDown,omitempty"`
	MovePaneLeft   string `json:"movePaneLeft,omitempty"`
	MovePaneRight  string `json:"movePaneRight,omitempty"`
	MovePaneUp     string `json:"movePaneUp,omitempty"`
	MovePaneDown   string `json:"movePaneDown,omitempty"`
}
