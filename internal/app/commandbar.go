package app

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"finallyshell/internal/config"
	"finallyshell/internal/icons"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The command bar is FinalShell's command input box: a line under the
// terminal whose Enter sends what it holds to the shell, as typed there,
// with popups above it to complete what is typed (Terminal › Command bar
// in the options turns it, or each popup, off):
//
//   - Tab: the path picker, a folder of the server in columns, when the
//     word at the caret looks like a path (a slash in it, or starting
//     with ~ or .); else command completion for the first word; else the
//     path picker for the current folder. Typing filters it, Backspace in
//     an empty filter goes up a folder, Enter or Tab takes the entry
//     highlighted.
//   - Up, or Alt+H: the history picker, the commands sent before that
//     start with what is typed; Left and Right highlight one field (word)
//     of the command highlighted, and Enter takes the field, or the whole
//     command when none is highlighted.
//   - Esc: closes the popup, and then gives the keyboard back to the
//     terminal; the Focus command bar shortcut goes back and forth.
//
// The bar takes no key from the terminal: it has the keys only while it
// has the focus. What it needs from the server, the listings, the
// command names and the server's history, is read on goroutines
// (commandbar_data.go), which change the state through App.post.

// barPopup is the popup the bar shows.
type barPopup int

const (
	popupNone barPopup = iota
	popupPaths
	popupCommands
	popupHistory
)

// cmdBar is a session's command bar.
type cmdBar struct {
	text string
	// caret is where the caret was as the last frame was built, in runes
	// of lastText; the keys, which come between frames, work from it.
	caret    int
	lastText string
	// newCaret is where to put the caret in the next frame, after a text
	// the bar set, when caretSet.
	newCaret int
	caretSet bool
	focus    bool // give the bar the keyboard in the next frame
	toTerm   bool // give the terminal the keyboard in the next frame
	focused  bool // the bar had the keyboard in the last frame

	popup  barPopup
	sel    int  // the item highlighted
	field  int  // the history picker's field highlighted, -1 for none
	reveal bool // scroll the item highlighted into view
	// start is where the word the path picker or command completion
	// completes starts: a caret in another word closes it.
	start int
	// shown is what the popup's items were made from (the folder and
	// filter, or the text), to start the highlight over as it changes.
	shown string
	cols  int // the columns of the grid, as the last frame laid it out
	list  ui.ListState

	data barData // what the popups show, read from the server
}

// commandBarOn tells whether the bar shows (Show the command bar).
func (a *App) commandBarOn() bool { return !a.settings.NoCommandBar }

func (a *App) pathsOn() bool    { return a.commandBarOn() && !a.settings.NoCommandBarPaths }
func (a *App) commandsOn() bool { return a.commandBarOn() && !a.settings.NoCommandBarCommands }
func (a *App) historyOn() bool  { return a.commandBarOn() && !a.settings.NoCommandBarHistory }

// popupOn tells whether p is still turned on.
func (a *App) popupOn(p barPopup) bool {
	switch p {
	case popupPaths:
		return a.pathsOn()
	case popupCommands:
		return a.commandsOn()
	case popupHistory:
		return a.historyOn()
	}
	return false
}

// barLive tells whether s's bar takes input: its shell is there.
func barLive(s *Session) bool { return s.status == statusConnected && s.term != nil }

// toggleBarFocus is the Focus command bar shortcut: the keyboard goes to
// the bar of the session shown, or back to its terminal.
func (a *App) toggleBarFocus() {
	s := a.current()
	if s == nil || s.isNewTab() || !barLive(s) {
		return
	}
	if s.bar.focused {
		s.bar.popup, s.bar.toTerm = popupNone, true
	} else {
		s.bar.focus = true
	}
}

// historyKey is Alt+H, which opens the history picker.
var historyKey = keyCombo{ui.Alt, ui.KeyH}

// commandBar is s's bar, under its terminal, and its popups.
func (a *App) commandBar(c *ui.Context, s *Session) {
	b := &s.bar
	live := barLive(s)
	row := ui.Row(c.Key("command-bar")).Padding(4, 8).Gap(6).AlignItems(ui.Center).
		Background(theme.Background).BorderWidth(1, 0, 0, 0).BorderColor(colors.line)
	var input ui.Element
	var button bool // History clicked
	row.Children(func() {
		// The icon says what the bar is for: it has no placeholder.
		ui.Icon(c, icons.Get("terminal")).FontSize(13).TextColor(theme.TextMuted)
		input = ui.TextInput(c, &b.text).Grow(1).MinWidth(0).Label(locales.CmdBarLabel()).Disabled(!live)
		if a.historyOn() {
			// FinalShell's History button, at the bar's end.
			button = ui.Button(c, locales.CmdBarHistory()).Shrink(0).Disabled(!live).Clicked()
		}
	})
	if !live {
		b.popup, b.focused = popupNone, false
		return
	}
	input.OnTextKey(func(k ui.TextKey) bool { return a.barKey(s, k) })
	if b.caretSet {
		input.SetTextSelection(b.newCaret, b.newCaret)
		b.caret, b.caretSet = b.newCaret, false
	} else {
		_, b.caret = input.TextAnchorCaret()
	}
	b.lastText = b.text
	if b.focus {
		input.Focus()
		b.focus = false
	}
	b.focused = input.Focused()
	if a.historyOn() && (button || row.Shortcut(historyKey.Mods, historyKey.Key)) {
		if b.popup == popupHistory {
			b.popup = popupNone
		} else {
			a.openHistory(s)
		}
		if button {
			input.Focus() // the keys stay the bar's, for the picker
		}
	}
	a.barPopover(c, s, row)
}

// barCaret is where the caret is in text as a key comes: where the last
// frame had it, moved by what was typed since, or where the bar put it.
func (b *cmdBar) barCaret(text string) int {
	n := utf8.RuneCountInString(text)
	c := b.caret
	if b.caretSet {
		c = b.newCaret
	} else if text != b.lastText {
		c += n - utf8.RuneCountInString(b.lastText)
	}
	return min(max(c, 0), n)
}

// barEdit is the text of the bar to change, and how: through the key
// being handled, between frames, or as the view is built.
type barEdit struct {
	text string
	set  func(string)
}

// setBar replaces the bar's text with text, its caret at caret.
func (b *cmdBar) setBar(e barEdit, text string, caret int) {
	e.set(text)
	b.text, b.newCaret, b.caretSet = text, caret, true
}

// barKey takes the keys of s's bar that type no text, before the bar
// does: the popups' keys, and Enter, which sends the line.
func (a *App) barKey(s *Session, k ui.TextKey) bool {
	b := &s.bar
	text := k.Text()
	caret := b.barCaret(text)
	e := barEdit{text: text, set: k.SetText}
	if b.popup != popupNone && !a.popupOn(b.popup) {
		b.popup = popupNone
	}
	if b.popup != popupNone {
		return a.popupKey(s, k, e, caret)
	}
	switch {
	case k.Key == ui.KeyEnter && k.Mods == 0:
		a.sendBar(s, e)
		return true
	case k.Key == ui.KeyTab && k.Mods == 0:
		a.complete(s, e, caret)
		return true
	case k.Key == ui.KeyUp && k.Mods == 0 && a.historyOn():
		a.openHistory(s)
		return true
	case k.Key == ui.KeyEscape && k.Mods == 0:
		b.toTerm = true
		return true
	}
	return false
}

// sendBar sends the bar's line to the shell, with a carriage return, as
// Enter typed in the terminal, and keeps it in the host's history,
// without the blanks around it.
func (a *App) sendBar(s *Session, e barEdit) {
	if !barLive(s) {
		return
	}
	s.term.Send([]byte(e.text + "\r"))
	s.shellCommandSent()
	if cmd := strings.TrimSpace(e.text); a.historyOn() && config.Keepable(cmd) {
		a.addHistory(s.Host.ID, cmd)
	}
	s.bar.popup = popupNone
	s.bar.setBar(e, "", 0)
}

// complete is Tab: it opens the path picker or command completion for
// the word at the caret.
func (a *App) complete(s *Session, e barEdit, caret int) {
	b := &s.bar
	r := []rune(e.text)
	start, _ := wordBounds(r, caret)
	word := string(r[start:caret])
	first := strings.TrimSpace(string(r[:start])) == ""
	switch {
	case a.pathsOn() && pathLike(word):
		if word == "~" { // the home folder's entries, after its slash
			b.setBar(e, string(r[:caret])+"/"+string(r[caret:]), caret+1)
		}
		a.openPopup(s, popupPaths, start)
	case a.commandsOn() && first:
		a.openPopup(s, popupCommands, start)
	case a.pathsOn():
		a.openPopup(s, popupPaths, start)
	}
}

// openPopup opens p for the word at start.
func (a *App) openPopup(s *Session, p barPopup, start int) {
	b := &s.bar
	b.popup, b.start, b.sel, b.field, b.reveal, b.shown = p, start, 0, -1, true, ""
	b.list = ui.ListState{}
	if p == popupPaths {
		b.data.dirs = nil // listed again as the picker opens
	}
}

// openHistory opens the history picker, for what is typed.
func (a *App) openHistory(s *Session) {
	a.openPopup(s, popupHistory, 0)
}

// popupKey takes the keys of an open popup.
func (a *App) popupKey(s *Session, k ui.TextKey, e barEdit, caret int) bool {
	b := &s.bar
	view := a.popupView(s, e.text, caret)
	n := len(view.items)
	if k.Mods != 0 {
		return false
	}
	switch k.Key {
	case ui.KeyEscape:
		b.popup = popupNone
		return true
	case ui.KeyEnter, ui.KeyTab:
		if n == 0 {
			b.popup = popupNone
			if k.Key == ui.KeyEnter {
				a.sendBar(s, e)
			}
			return true
		}
		a.choose(s, e, caret, view, min(b.sel, n-1))
		return true
	case ui.KeyBackspace:
		if b.popup == popupPaths && view.path.filter == "" && caret == view.path.end {
			if up, ok := a.parentDir(s, view.path); ok {
				r := []rune(e.text)
				head := string(r[:view.path.start]) + up
				b.setBar(e, head+string(r[view.path.end:]), utf8.RuneCountInString(head))
				b.sel, b.reveal = 0, true
				return true
			}
		}
		return false
	}
	if b.popup == popupHistory {
		return a.historyKey(s, k, view)
	}
	cols := max(b.cols, 1)
	moved := b.sel
	switch k.Key {
	case ui.KeyLeft:
		moved--
	case ui.KeyRight:
		moved++
	case ui.KeyUp:
		moved -= cols
	case ui.KeyDown:
		moved += cols
	case ui.KeyHome:
		moved = 0
	case ui.KeyEnd:
		moved = n - 1
	default:
		return false
	}
	if n > 0 && moved >= 0 && moved < n {
		b.sel, b.reveal = moved, true
	}
	return true
}

// historyKey moves in the history picker: Up to older commands, which
// show above, Down to newer ones, Left and Right through the fields of
// the one highlighted.
func (a *App) historyKey(s *Session, k ui.TextKey, view popupContent) bool {
	b := &s.bar
	n := len(view.items)
	switch k.Key {
	case ui.KeyUp:
		if b.sel+1 < n {
			b.sel, b.field, b.reveal = b.sel+1, -1, true
		}
	case ui.KeyDown:
		if b.sel > 0 {
			b.sel, b.field, b.reveal = b.sel-1, -1, true
		}
	case ui.KeyLeft, ui.KeyRight:
		if n == 0 {
			return true
		}
		count := len(commandFields(view.items[min(b.sel, n-1)].name))
		if count == 0 {
			return true
		}
		// -1, the whole command, comes round between the last field and
		// the first.
		d := 1
		if k.Key == ui.KeyLeft {
			d = -1
		}
		b.field = (b.field+1+d+count+1)%(count+1) - 1
	default:
		return false
	}
	return true
}

// choose takes item i of the popup: it goes into the bar at the word it
// completes, or a command of the history in place of the bar's text.
func (a *App) choose(s *Session, e barEdit, caret int, view popupContent, i int) {
	b := &s.bar
	r := []rune(e.text)
	it := view.items[i]
	if it.bad {
		return // shown, never inserted
	}
	switch b.popup {
	case popupPaths:
		p := view.path
		ins := p.dirRaw + escapePath(it.name)
		if it.dir {
			ins += "/"
		} else if p.end == len(r) || !isBlank(r[p.end]) {
			ins += " "
		}
		head := string(r[:p.start]) + ins
		b.setBar(e, head+string(r[p.end:]), utf8.RuneCountInString(head))
		if it.dir {
			b.sel, b.reveal = 0, true // the folder's entries, as FinalShell
		} else {
			b.popup = popupNone
		}
	case popupCommands:
		_, end := wordBounds(r, caret)
		ins := it.name
		if end == len(r) || !isBlank(r[end]) {
			ins += " "
		}
		head := string(r[:b.start]) + ins
		b.setBar(e, head+string(r[end:]), utf8.RuneCountInString(head))
		b.popup = popupNone
	case popupHistory:
		fields := commandFields(it.name)
		if b.field < 0 || b.field >= len(fields) {
			b.setBar(e, it.name, utf8.RuneCountInString(it.name))
		} else {
			// The field goes in place of the word at the caret.
			f := fields[b.field]
			field := string([]rune(it.name)[f.start:f.end])
			start, end := wordBounds(r, caret)
			head := string(r[:start]) + field
			b.setBar(e, head+string(r[end:]), utf8.RuneCountInString(head))
		}
		b.popup = popupNone
	}
}

// barItem is an entry of a popup: a file or folder, a command name, or a
// command of the history.
type barItem struct {
	name   string
	dir    bool
	hidden bool
	// bad is a name with a control character (config.HasControl): shown,
	// but never inserted, as a newline in it would run what follows as a
	// command of its own.
	bad bool
}

// shownName is how the popup writes an item's name: a bad one with its
// control characters escaped, as \n.
func (it barItem) shownName() string {
	if !it.bad {
		return it.name
	}
	q := strconv.Quote(it.name)
	return q[1 : len(q)-1]
}

// popupContent is what the open popup shows, made from the bar's text
// and caret.
type popupContent struct {
	items   []barItem
	more    int    // the matches past those listed
	title   string // above the items: the folder, or what they are
	loading bool
	err     string // why there are none to show
	path    pathWord
}

// maxPopupItems is the most entries a popup lists, the rest counted.
const maxPopupItems = 2000

// popupView is what the open popup of s shows for text with the caret
// at caret; it starts reading what it lacks, on goroutines.
func (a *App) popupView(s *Session, text string, caret int) popupContent {
	b := &s.bar
	var v popupContent
	switch b.popup {
	case popupPaths:
		v.path = parsePathWord([]rune(text), caret)
		key := a.listKey(s, v.path.dirRaw)
		v.title = key
		l := a.listing(s, key)
		v.loading, v.err = l.loading, l.err
		if l.dir != "" {
			v.title = l.dir
		}
		filter := strings.ToLower(v.path.filter)
		for _, e := range l.entries {
			if strings.HasPrefix(strings.ToLower(e.name), filter) {
				v.items = append(v.items, e)
			}
		}
	case popupCommands:
		r := []rune(text)
		start, _ := wordBounds(r, caret)
		prefix := string(r[start:caret])
		v.title = locales.CmdBarCommands()
		l := a.commandNames(s)
		v.loading, v.err = l.loading, l.err
		for _, name := range l.names {
			if strings.HasPrefix(name, prefix) {
				v.items = append(v.items, barItem{name: name})
			}
		}
	case popupHistory:
		v.title = locales.CmdBarHistory()
		var cmds []string
		cmds, v.loading = a.historyFor(s)
		prefix := strings.TrimLeft(text, " \t")
		for _, c := range cmds {
			if strings.HasPrefix(c, prefix) {
				v.items = append(v.items, barItem{name: c})
			}
		}
	}
	if len(v.items) > maxPopupItems {
		v.more = len(v.items) - maxPopupItems
		v.items = v.items[:maxPopupItems]
	}
	return v
}

// stillOpen tells whether the popup goes on with the caret where it is:
// the path picker and command completion close as the caret leaves the
// word they complete.
func (b *cmdBar) stillOpen(text string, caret int) bool {
	r := []rune(text)
	start, _ := wordBounds(r, caret)
	switch b.popup {
	case popupPaths:
		return start == b.start
	case popupCommands:
		return start == b.start && strings.TrimSpace(string(r[:start])) == ""
	}
	return b.popup != popupNone
}

// The sizes of the popups.
const (
	popupCellWidth  = 200 // a column of the path picker and of commands
	popupMaxHeight  = 280
	historyMaxWidth = 720
	popupInset      = 4 // from the bar's ends
)

// barPopover shows the open popup above the bar, row.
func (a *App) barPopover(c *ui.Context, s *Session, row ui.Element) {
	b := &s.bar
	if b.popup != popupNone && (!a.popupOn(b.popup) || !b.stillOpen(b.text, b.caret)) {
		b.popup = popupNone
	}
	if b.popup == popupNone {
		return
	}
	view := a.popupView(s, b.text, b.caret)
	shown := view.title + "\x00" + view.path.filter
	if b.popup == popupHistory {
		shown = b.text
	}
	if shown != b.shown {
		if b.shown != "" {
			b.sel, b.field = 0, -1
		}
		b.shown, b.reveal = shown, true
	}
	if n := len(view.items); b.sel >= n {
		b.sel = max(n-1, 0)
	}
	width := row.Bounds().W
	if width <= 0 {
		width = 600
	}
	panelW := width - 2*popupInset // inside the window's edge
	if b.popup == popupHistory {
		panelW = min(width, historyMaxWidth)
	}
	b.cols = max(int((panelW-10)/popupCellWidth), 1)
	if b.popup == popupHistory {
		b.cols = 1
	}
	open := true
	ui.PopoverBase(c, row, &open, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(0, 0, 2, 0).Left(popupInset).
			Width(panelW).Padding(4).Gap(2).
			Background(theme.Background).Border(1, theme.Border).Radius(6).
			Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.25)).Label(view.title)
		a.popupHeader(c, s, view)
		switch {
		case len(view.items) == 0 && view.err != "":
			ui.Text(c, view.err).TextColor(theme.Danger).Padding(6, 8)
		case len(view.items) == 0 && !view.loading:
			ui.Text(c, locales.CmdBarNoMatches()).TextColor(theme.TextMuted).Padding(6, 8)
		case b.popup == popupHistory:
			a.historyRows(c, s, view)
		case len(view.items) > 0:
			a.gridRows(c, s, view)
		}
	})
	if !open {
		b.popup = popupNone
	}
	b.reveal = false
}

// popupHeader is the line above a popup's items: the folder or what it
// lists, and the keys, or Clear list in the history.
func (a *App) popupHeader(c *ui.Context, s *Session, view popupContent) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(2, 6).Children(func() {
		ui.Text(c, view.title).FontSize(12).TextColor(theme.TextMuted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		if view.loading {
			ui.Spinner(c).FontSize(11)
		}
		ui.Box(c).Grow(1)
		hint := locales.CmdBarPickHint()
		if s.bar.popup == popupHistory {
			hint = locales.CmdBarHistoryHint()
		}
		ui.Text(c, hint).FontSize(11).TextColor(theme.TextMuted).SingleLine().Shrink(0)
		if s.bar.popup == popupHistory && len(view.items) > 0 {
			clear := ui.Text(c, locales.CmdBarClearList()).FontSize(12).TextColor(theme.Accent).Cursor(ui.CursorPointer).Shrink(0)
			if clear.Clicked() {
				a.clearHostHistory(s)
			}
		}
	})
}

// gridRows lists the path picker's entries, or command names, in rows of
// b.cols columns, left to right.
func (a *App) gridRows(c *ui.Context, s *Session, view popupContent) {
	b := &s.bar
	cols := b.cols
	items := view.items
	rows := (len(items) + cols - 1) / cols
	n := rows
	if view.more > 0 {
		n++
	}
	if b.reveal {
		b.list.ScrollIntoView(b.sel / cols)
	}
	ui.List(c, &b.list, n, func(r int) {
		if r == rows {
			ui.Text(c, locales.CmdBarMore(view.more)).FontSize(12).TextColor(theme.TextMuted).Padding(4, 8)
			return
		}
		ui.Row(c).Children(func() {
			for i := r * cols; i < min((r+1)*cols, len(items)); i++ {
				a.gridCell(c, s, view, i)
			}
		})
	}).MaxHeight(popupMaxHeight)
}

// gridCell is entry i: its icon and name.
func (a *App) gridCell(c *ui.Context, s *Session, view popupContent, i int) {
	b := &s.bar
	it := view.items[i]
	on := i == b.sel
	cell := ui.Row(c).Width(popupCellWidth).Gap(6).AlignItems(ui.Center).Padding(3, 6).Radius(4)
	text := theme.Text
	if it.hidden {
		text = theme.TextMuted
	}
	if it.bad {
		cell.Opacity(0.5).Tooltip(locales.CmdBarControlName())
	} else {
		cell.Cursor(ui.CursorPointer)
	}
	if on {
		cell.Background(theme.Accent)
		text = theme.AccentText
	}
	cell.Children(func() {
		if b.popup == popupPaths {
			a.fileIcon(c, it.name, it.dir, false, false)
		}
		name := it.shownName()
		if it.dir {
			name += "/"
		}
		ui.Text(c, name).TextColor(text).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
	if cell.Clicked() && !it.bad {
		b.sel = i
		a.chooseInView(s, view, i)
	}
}

// historyRows lists the history picker's commands, the newest at the
// bottom, next to the bar, and the fields of the one highlighted.
func (a *App) historyRows(c *ui.Context, s *Session, view popupContent) {
	b := &s.bar
	n := len(view.items)
	if b.reveal {
		b.list.ScrollIntoView(n - 1 - b.sel)
	}
	ui.List(c, &b.list, n, func(r int) {
		i := n - 1 - r
		it := view.items[i]
		on := i == b.sel
		row := ui.Row(c).Padding(3, 8).Radius(4).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		if on && b.field < 0 {
			row.Background(theme.Accent)
		} else if on {
			row.Background(theme.Selection)
		}
		field := -1 // a field clicked
		row.Children(func() {
			if !on || b.field < 0 {
				t := ui.Text(c, it.name).SingleLine().Ellipsis("…").MinWidth(0).Padding(0, 3)
				if on {
					t.TextColor(theme.AccentText)
				}
				return
			}
			// One text a field, the one highlighted in the accent color.
			rs := []rune(it.name)
			for fi, f := range commandFields(it.name) {
				t := ui.Text(c, string(rs[f.start:f.end])).SingleLine().Padding(0, 3).Radius(3)
				if fi == b.field {
					t.Background(theme.Accent).TextColor(theme.AccentText).Shrink(0)
				} else {
					t.Ellipsis("…").MinWidth(0)
				}
				if t.Clicked() {
					field = fi
				}
			}
		})
		if row.Clicked() || field >= 0 {
			b.sel, b.field = i, field
			a.chooseInView(s, view, i)
		}
	}).MaxHeight(popupMaxHeight)
}

// chooseInView takes item i of the popup as a click does, the view
// setting the bar's text.
func (a *App) chooseInView(s *Session, view popupContent, i int) {
	b := &s.bar
	e := barEdit{text: b.text, set: func(string) {}}
	a.choose(s, e, b.caret, view, i)
	b.focus = true
}

// barFocus gives the terminal, view, the keyboard as the bar asked.
func (a *App) barFocus(s *Session, view ui.Element) {
	if s.bar.toTerm {
		view.Focus()
		s.bar.toTerm = false
	}
}

// The words of the bar: separated by blanks, a blank escaped with a
// backslash belonging to its word, as the shell reads it.

func isBlank(r rune) bool { return r == ' ' || r == '\t' }

// escapedAt tells whether r[i] follows an odd number of backslashes.
func escapedAt(r []rune, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && r[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// wordBounds returns where the word at caret starts and ends, in runes.
func wordBounds(r []rune, caret int) (start, end int) {
	start = min(max(caret, 0), len(r))
	for start > 0 && !(isBlank(r[start-1]) && !escapedAt(r, start-1)) {
		start--
	}
	end = start
	for end < len(r) && !(isBlank(r[end]) && !escapedAt(r, end)) {
		end++
	}
	return start, max(end, caret)
}

// pathLike tells whether a word looks like a path, for Tab.
func pathLike(word string) bool {
	return strings.Contains(word, "/") || strings.HasPrefix(word, "~") || strings.HasPrefix(word, ".")
}

// pathWord is the word the path picker completes.
type pathWord struct {
	start, end int // where the word is, in runes
	// dirRaw is the word up to its last slash, as typed; "" for the
	// current folder.
	dirRaw string
	// filter is what follows it up to the caret, its backslashes taken
	// out.
	filter string
}

// parsePathWord reads the word at caret as a folder and a filter.
func parsePathWord(r []rune, caret int) pathWord {
	start, end := wordBounds(r, caret)
	w := r[start:min(caret, end)]
	slash := -1
	for i := len(w) - 1; i >= 0; i-- {
		if w[i] == '/' && !escapedAt(w, i) {
			slash = i
			break
		}
	}
	return pathWord{start: start, end: end, dirRaw: string(w[:slash+1]), filter: unescape(string(w[slash+1:]))}
}

// unescape takes the backslashes out of a word: "\ " is a blank.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	esc := false
	for _, c := range s {
		if c == '\\' && !esc {
			esc = true
			continue
		}
		esc = false
		b.WriteRune(c)
	}
	return b.String()
}

// escapePath puts a backslash before the characters of a file name that
// the shell would read as something else.
func escapePath(name string) string {
	var b strings.Builder
	for _, c := range name {
		if strings.ContainsRune(" \t\\'\"`$&;|<>()*?[]{}!", c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// field is a field of a command, in runes.
type field struct{ start, end int }

// commandFields splits a command into its fields, as the shell would:
// at blanks, but not in quotes or after a backslash.
func commandFields(cmd string) []field {
	r := []rune(cmd)
	var out []field
	start := -1
	var quote rune
	for i := 0; i <= len(r); i++ {
		if i == len(r) {
			if start >= 0 {
				out = append(out, field{start, i})
			}
			break
		}
		ch := r[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\\':
			if start < 0 {
				start = i
			}
			if i+1 < len(r) {
				i++ // the next character is the field's
			}
		case ch == '"' || ch == '\'':
			quote = ch
			if start < 0 {
				start = i
			}
		case isBlank(ch):
			if start >= 0 {
				out = append(out, field{start, i})
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	return out
}
