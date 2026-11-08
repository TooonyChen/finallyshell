package config

// BackspaceKey is what the Backspace key sends to a host
// (Settings.TermBackspace, Host.Backspace).
type BackspaceKey string

const (
	// BackspaceDefault is a host's "Default": what the settings say. In
	// the settings, where it is the value of a file without the key, it
	// sends ^?.
	BackspaceDefault BackspaceKey = ""
	// BackspaceDEL sends ^? (0x7f), as xterm, Ghostty and most others.
	BackspaceDEL BackspaceKey = "del"
	// BackspaceCtrlH sends ^H (0x08), for servers whose erase character
	// is ^H, as some old systems and serial consoles.
	BackspaceCtrlH BackspaceKey = "ctrl-h"
)

// DeleteKey is what the Delete key sends to a host (Settings.TermDelete,
// Host.Delete).
type DeleteKey string

const (
	// DeleteDefault is a host's "Default": what the settings say. In the
	// settings it sends ESC [ 3 ~.
	DeleteDefault DeleteKey = ""
	// DeleteVT220 sends ESC [ 3 ~, the VT220's Remove key, as xterm and
	// most others.
	DeleteVT220 DeleteKey = "vt220"
	// DeleteDEL sends ^? (0x7f), for servers that take it as delete.
	DeleteDEL DeleteKey = "del"
	// DeleteCtrlH sends ^H (0x08).
	DeleteCtrlH DeleteKey = "ctrl-h"
)

// TermBackspaceKey is what Backspace sends to a host whose own setting is
// "Default": BackspaceDEL or BackspaceCtrlH, never BackspaceDefault. A
// value this version does not know sends ^?.
func (s Settings) TermBackspaceKey() BackspaceKey {
	if s.TermBackspace == BackspaceCtrlH {
		return BackspaceCtrlH
	}
	return BackspaceDEL
}

// TermDeleteKey is what Delete sends to a host whose own setting is
// "Default": never DeleteDefault. A value this version does not know
// sends ESC [ 3 ~.
func (s Settings) TermDeleteKey() DeleteKey {
	switch s.TermDelete {
	case DeleteDEL, DeleteCtrlH:
		return s.TermDelete
	}
	return DeleteVT220
}

// HostBackspace is what Backspace sends to h: its own choice, else the
// settings'. It is BackspaceDEL or BackspaceCtrlH.
func (s Settings) HostBackspace(h *Host) BackspaceKey {
	switch h.Backspace {
	case BackspaceDEL, BackspaceCtrlH:
		return h.Backspace
	}
	return s.TermBackspaceKey()
}

// HostDelete is what Delete sends to h: its own choice, else the
// settings'. It is never DeleteDefault.
func (s Settings) HostDelete(h *Host) DeleteKey {
	switch h.Delete {
	case DeleteVT220, DeleteDEL, DeleteCtrlH:
		return h.Delete
	}
	return s.TermDeleteKey()
}
