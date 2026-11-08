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

// Encoding is the character encoding of a host's terminal: what the
// server's programs read and write (Settings.TermEncoding,
// Host.Encoding). Package termenc converts it to and from UTF-8.
type Encoding string

const (
	// EncodingDefault is a host's "Default": what the settings say. In
	// the settings it is UTF-8.
	EncodingDefault Encoding = ""
	EncodingUTF8    Encoding = "utf-8"
	EncodingGBK     Encoding = "gbk"
	EncodingGB18030 Encoding = "gb18030"
	EncodingBig5    Encoding = "big5"
	EncodingSJIS    Encoding = "shift_jis"
	EncodingEUCJP   Encoding = "euc-jp"
	EncodingEUCKR   Encoding = "euc-kr"
	EncodingLatin1  Encoding = "iso-8859-1"
	EncodingWin1252 Encoding = "windows-1252"
)

// Encodings are the encodings a host can have, in the order the Terminal
// page and the connection editor list them.
var Encodings = []Encoding{
	EncodingUTF8, EncodingGBK, EncodingGB18030, EncodingBig5,
	EncodingSJIS, EncodingEUCJP, EncodingEUCKR, EncodingLatin1, EncodingWin1252,
}

// known says whether e is one of Encodings.
func (e Encoding) known() bool {
	for _, k := range Encodings {
		if e == k {
			return true
		}
	}
	return false
}

// TermEncodingName is the encoding of a host whose own setting is
// "Default": never EncodingDefault. A name this version does not know
// is UTF-8.
func (s Settings) TermEncodingName() Encoding {
	if s.TermEncoding.known() {
		return s.TermEncoding
	}
	return EncodingUTF8
}

// HostEncoding is the encoding of h's terminal: its own choice, else the
// settings'. It is never EncodingDefault.
func (s Settings) HostEncoding(h *Host) Encoding {
	if h.Encoding.known() {
		return h.Encoding
	}
	return s.TermEncodingName()
}
