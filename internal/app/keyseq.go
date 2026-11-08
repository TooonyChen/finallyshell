package app

import (
	"finallyshell/internal/config"
	"finallyshell/internal/termenc"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// choice is a choice of the Terminal page and the connection editor's
// terminal options: what Backspace or Delete sends, or the encoding.
type choice[K ~string] struct {
	value K
	label func() string
}

var backspaceChoices = []choice[config.BackspaceKey]{
	{config.BackspaceDEL, locales.KeySeqDEL},
	{config.BackspaceCtrlH, locales.KeySeqCtrlH},
}

var encodingChoices = func() []choice[config.Encoding] {
	cs := make([]choice[config.Encoding], len(config.Encodings))
	for i, e := range config.Encodings {
		cs[i] = choice[config.Encoding]{e, func() string { return termenc.Label(e) }}
	}
	return cs
}()

var deleteChoices = []choice[config.DeleteKey]{
	{config.DeleteVT220, locales.KeySeqVT220},
	{config.DeleteDEL, locales.KeySeqDEL},
	{config.DeleteCtrlH, locales.KeySeqCtrlH},
}

// choiceSelect is a select of choices showing *value, named name for
// screen readers, and reports whether the user changed *value. With def,
// the first choice is "Default: <def>", which stands for the zero value:
// a host's choice to follow the settings. A value not among the choices,
// as a newer version may have written, shows the first.
func choiceSelect[K ~string](c *ui.Context, name string, value *K, choices []choice[K], def *K) bool {
	labels := make([]string, 0, len(choices)+1)
	values := make([]K, 0, len(choices)+1)
	if def != nil {
		for _, ch := range choices {
			if ch.value == *def {
				labels = append(labels, locales.KeySeqDefault(ch.label()))
				values = append(values, "")
			}
		}
	}
	for _, ch := range choices {
		labels = append(labels, ch.label())
		values = append(values, ch.value)
	}
	chosen := labels[0]
	for i, v := range values {
		if v == *value {
			chosen = labels[i]
		}
	}
	if !ui.Select(c, &chosen, labels).Width(240).Label(name).Changed() {
		return false
	}
	for i, l := range labels {
		if l == chosen {
			*value = values[i]
		}
	}
	return true
}

// terminalDefaultsPage is the Terminal page of the options: the encoding
// of the hosts whose own choice is "Default", and what Backspace and
// Delete send to them.
func (a *App) terminalDefaultsPage(c *ui.Context) {
	ui.Form(c, func() {
		ui.Field(c, locales.FieldEncoding(), func() {
			e := a.settings.TermEncodingName()
			if choiceSelect(c, locales.FieldEncoding(), &e, encodingChoices, nil) {
				if e == config.EncodingUTF8 {
					e = "" // the default, written as nothing
				}
				a.changeSettings(func() { a.settings.TermEncoding = e })
			}
		}).Description(locales.EncodingHint())
		ui.Field(c, locales.FieldBackspace(), func() {
			bs := a.settings.TermBackspaceKey()
			if choiceSelect(c, locales.FieldBackspace(), &bs, backspaceChoices, nil) {
				if bs == config.BackspaceDEL {
					bs = ""
				}
				a.changeSettings(func() { a.settings.TermBackspace = bs })
			}
		})
		ui.Field(c, locales.FieldDelete(), func() {
			del := a.settings.TermDeleteKey()
			if choiceSelect(c, locales.FieldDelete(), &del, deleteChoices, nil) {
				if del == config.DeleteVT220 {
					del = ""
				}
				a.changeSettings(func() { a.settings.TermDelete = del })
			}
		})
	})
	defaults := a.settings.TermEncodingName() == config.EncodingUTF8 &&
		a.settings.TermBackspaceKey() == config.BackspaceDEL && a.settings.TermDeleteKey() == config.DeleteVT220
	a.lookFooter(c, locales.KeySequencesNote(), func() {
		a.settings.TermEncoding, a.settings.TermBackspace, a.settings.TermDelete = "", "", ""
	}, defaults)
}

// eraseChar is the character Backspace sends with bs, which the server's
// pseudo-terminal is told is its erase character.
func eraseChar(bs config.BackspaceKey) byte {
	if bs == config.BackspaceCtrlH {
		return 0x08
	}
	return 0x7f
}

// keySequences is the terminal's Options.OnKey for a host whose
// Backspace sends bs and Delete del, as Settings.HostBackspace and
// HostDelete give them: nil for ^? and ESC [ 3 ~, which the terminal
// sends itself. With ^H, Backspace sends it, and Alt+Backspace Esc and
// ^H, as xterm does with backarrowKey off. Delete with ^? or ^H sends
// that byte; with a modifier it is left to the terminal, which sends
// ESC [ 3 ; <mods> ~ that programs read as a modified Delete. term is
// the terminal the key goes to: the handler asks it as it runs, on the
// UI thread.
func keySequences(bs config.BackspaceKey, del config.DeleteKey, term func() *terminal.Terminal) func(ui.Modifiers, ui.Key) bool {
	if bs != config.BackspaceCtrlH && del == config.DeleteVT220 {
		return nil
	}
	return func(mods ui.Modifiers, key ui.Key) bool {
		t := term()
		if t == nil {
			return false
		}
		switch {
		case key == ui.KeyBackspace && bs == config.BackspaceCtrlH:
			switch mods {
			case 0:
				t.Send([]byte{0x08})
			case ui.Alt:
				t.Send([]byte{0x1b, 0x08})
			default:
				return false
			}
			return true
		case key == ui.KeyDelete && del != config.DeleteVT220 && mods == 0:
			b := byte(0x7f)
			if del == config.DeleteCtrlH {
				b = 0x08
			}
			t.Send([]byte{b})
			return true
		}
		return false
	}
}
