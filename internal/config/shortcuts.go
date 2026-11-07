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
}
