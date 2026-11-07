package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// keyCombo is a key with the modifiers held with it, as a shortcut takes
// it. The zero value is no key.
type keyCombo struct {
	Mods ui.Modifiers
	Key  ui.Key
}

func (k keyCombo) none() bool { return k.Key == ui.KeyUnknown }

// digitKeys are the keys of the digits 1 to 9, which switch tabs.
var digitKeys = [...]ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// digitsName is the key of a combo that stands for the digits 1 to 9, as
// the settings write it.
const digitsName = "1-9"

// keyNames name the keys a shortcut can take, as the settings write them.
var keyNames = func() map[ui.Key]string {
	m := map[ui.Key]string{
		ui.KeyEnter: "Enter", ui.KeyEscape: "Escape", ui.KeyBackspace: "Backspace",
		ui.KeyTab: "Tab", ui.KeySpace: "Space", ui.KeyDelete: "Delete", ui.KeyInsert: "Insert",
		ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
		ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
		ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/",
		ui.KeySemicolon: ";", ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]",
		ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
	}
	for i, k := range [...]ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12} {
		m[k] = "F" + string(rune('1'+i))
		if i >= 9 {
			m[k] = "F1" + string(rune('0'+i-9))
		}
	}
	m[ui.Key0] = "0"
	for i, k := range digitKeys {
		m[k] = string(rune('1' + i))
	}
	for i, k := range [...]ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK, ui.KeyL, ui.KeyM, ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ} {
		m[k] = string(rune('A' + i))
	}
	return m
}()

var keysByName = func() map[string]ui.Key {
	m := make(map[string]ui.Key, len(keyNames))
	for k, n := range keyNames {
		m[strings.ToLower(n)] = k
	}
	return m
}()

// modNames are the modifiers in the order the settings write them.
var modNames = [...]struct {
	mod  ui.Modifiers
	name string
}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Super, "Super"}}

// format writes k as the settings keep it, as "Ctrl+Shift+C"; digits
// writes a combo of the digits as "Alt+1-9".
func (k keyCombo) format(digits bool) string {
	if k.none() {
		return "none"
	}
	var b strings.Builder
	for _, m := range modNames {
		if k.Mods&m.mod != 0 {
			b.WriteString(m.name + "+")
		}
	}
	if digits {
		b.WriteString(digitsName)
	} else {
		b.WriteString(keyNames[k.Key])
	}
	return b.String()
}

// parseCombo reads a combo as format writes it; ok is false for text it
// does not know. "none" is no key, with ok. With digits, the key is the
// digit 1, standing for the nine.
func parseCombo(s string, digits bool) (k keyCombo, ok bool) {
	if strings.EqualFold(s, "none") {
		return keyCombo{}, true
	}
	parts := strings.Split(s, "+")
	key := strings.ToLower(parts[len(parts)-1])
	for _, p := range parts[:len(parts)-1] {
		found := false
		for _, m := range modNames {
			if strings.EqualFold(p, m.name) {
				k.Mods |= m.mod
				found = true
			}
		}
		if !found {
			return keyCombo{}, false
		}
	}
	if digits {
		if key != digitsName {
			return keyCombo{}, false
		}
		k.Key = ui.Key1
		return k, true
	}
	if k.Key, ok = keysByName[key]; !ok {
		return keyCombo{}, false
	}
	return k, true
}

// label is k as the shortcuts page shows it on goos: with macOS's
// symbols there, as "⇧⌘F", and as "Ctrl+Shift+F" elsewhere; digits shows
// the nine digits.
func (k keyCombo) label(goos string, digits bool) string {
	if k.none() {
		return "—"
	}
	key := keyNames[k.Key]
	if digits {
		key = "1…9"
	}
	if goos == "darwin" {
		var b strings.Builder
		// In the order of macOS's menus.
		for _, m := range [...]struct {
			mod ui.Modifiers
			sym string
		}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Super, "⌘"}} {
			if k.Mods&m.mod != 0 {
				b.WriteString(m.sym)
			}
		}
		switch k.Key {
		case ui.KeyEnter:
			key = "↩"
		case ui.KeyEscape:
			key = "⎋"
		case ui.KeyBackspace:
			key = "⌫"
		case ui.KeyDelete:
			key = "⌦"
		case ui.KeyTab:
			key = "⇥"
		case ui.KeyLeft:
			key = "←"
		case ui.KeyRight:
			key = "→"
		case ui.KeyUp:
			key = "↑"
		case ui.KeyDown:
			key = "↓"
		}
		b.WriteString(key)
		return b.String()
	}
	var b strings.Builder
	for _, m := range modNames {
		if k.Mods&m.mod == 0 {
			continue
		}
		name := m.name
		if m.mod == ui.Super && goos == "windows" {
			name = "Win"
		}
		b.WriteString(name + "+")
	}
	b.WriteString(key)
	return b.String()
}

// typesText tells whether key types a character: a letter, a digit, a
// punctuation key or Space.
func typesText(key ui.Key) bool {
	n, ok := keyNames[key]
	return ok && (len(n) == 1 || key == ui.KeySpace)
}

// editsLine tells whether key moves or edits in a shell's line: the
// arrows, Home, End, Backspace and Delete, whose modified forms readline
// and other shells take for words.
func editsLine(key ui.Key) bool {
	switch key {
	case ui.KeyLeft, ui.KeyRight, ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd, ui.KeyBackspace, ui.KeyDelete:
		return true
	}
	return false
}

// isDigit tells whether key is one of the digits 1 to 9.
func isDigit(key ui.Key) bool {
	for _, d := range digitKeys {
		if d == key {
			return true
		}
	}
	return false
}
