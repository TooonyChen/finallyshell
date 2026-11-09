package app

import (
	"runtime"
	"slices"
	"strings"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/icons"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The command editor is FinalShell's 命令编辑器: a pane of the Commands
// tab (quickcmds.go) to write a few lines in and send them without saving
// them as a quick command first. Its toggle in the tab's toolbar shows
// it, on the right of the buttons, or under the groups and the buttons
// where the tab is too narrow for it beside them, as in the right column
// (rightcolumn.go). Whether it shows and its sizes are kept with the pane
// sizes (splits.go), this device's own.
//
// Send, or Cmd+Enter (Ctrl+Enter elsewhere) in the text, sends the lines
// selected, whole, or all of it with none, as a quick command of those
// lines is sent (sendQuick): line by line through sendLine, to the window's
// send target (sendtarget.go), with the toolbar's and the bar's question
// before the first send to a new target. Its Options are a quick
// command's Enter, here for every send, and emptying the editor of what
// was sent.
//
// The text is ui/editor's, as the file editor's, for its line numbers
// and its rope, which edits and draws a large text as a small one: only
// the lines in view are laid out, and nothing splits the text but a send.
// It is the same for every session, and is kept in a file of its own in
// the config folder (config.CommandEditorFile), written once typing has
// stopped for cmdTextSaveDelay, and as the app quits.

// cmdEditor is the command editor's state.
type cmdEditor struct {
	shown bool
	// width is its width beside the buttons, in DIPs; pct the buttons'
	// share of the tab's height above it, where it is under them.
	width, pct float32

	// code is the text, nil until the editor first shows.
	code *editor.State
	// loadErr is why the file could not be read: then nothing is saved
	// over it this run. saveErr is why the last save failed.
	loadErr, saveErr error
	// saved is the buffer's version the file holds; seen the version
	// the last frame saw, which changed at changedAt.
	saved, seen uint64
	changedAt   time.Time

	focus bool // give the text the keyboard in the next frame
}

const (
	defaultCmdEditorWidth = 460 // DIPs
	defaultCmdEditorPct   = 40
	// cmdEditorBeside is the least width of the Commands tab, in DIPs,
	// with the editor beside the buttons: the groups, the least width of
	// the buttons and of the editor (cmdEditorLimits). A narrower tab has
	// it under them.
	cmdEditorBeside = quickGroupsWidth + 220 + 280
	// cmdTextSaveDelay is how long typing rests before the text is saved.
	cmdTextSaveDelay = time.Second
)

var (
	// Beside the buttons, the editor keeps its Send row, and the buttons
	// the toolbar's New command and a button or two.
	cmdEditorLimits = splitLimits{first: 280, second: 220}
	// Under them, the buttons keep the toolbar and a row, and the editor
	// its header, a few lines and its Send row.
	cmdEditorStackLimits = splitLimits{first: 80, second: 170, vertical: true, percent: true}
)

// cmdSendKey sends the editor's text.
var cmdSendKey = keyCombo{ui.Cmd, ui.KeyEnter}

// restoreCmdEditor takes the editor's layout from the settings.
func (a *App) restoreCmdEditor() {
	b, s := &a.quickUI.box, a.settings
	b.shown = s.CommandEditorShown
	b.width, b.pct = defaultCmdEditorWidth, defaultCmdEditorPct
	if s.CommandEditorWidth >= cmdEditorLimits.first && s.CommandEditorWidth <= maxSavedWidth {
		b.width = s.CommandEditorWidth
	}
	if s.CommandEditorPercent > 0 && s.CommandEditorPercent < 100 {
		b.pct = s.CommandEditorPercent
	}
}

// keepCmdEditorLayout puts the editor's layout in the settings, which
// saveLayout saves.
func (a *App) keepCmdEditorLayout() {
	b := &a.quickUI.box
	a.settings.CommandEditorShown = b.shown
	a.settings.CommandEditorWidth = b.width
	a.settings.CommandEditorPercent = b.pct
}

// cmdCode is the editor's text, read from its file the first time.
func (a *App) cmdCode() *editor.State {
	b := &a.quickUI.box
	if b.code == nil {
		text, err := config.LoadCommandEditorText()
		b.loadErr = err
		buf := editor.NewBuffer(text)
		b.code = &editor.State{
			Buffer: buf,
			Font:   codeFont,
			// Plain text: what is typed is what is sent.
			NoAutoClose: true,
			NoFolding:   true,
			Whitespace:  editor.WhitespaceNone,
			// Cmd+Enter is Send's, not the editor's new line below.
			Bindings: []editor.Binding{{Mods: cmdSendKey.Mods, Key: cmdSendKey.Key, Command: editor.NoCommand}},
		}
		b.saved, b.seen = buf.Version(), buf.Version()
	}
	return b.code
}

// saveCmdTextAtRest saves the editor's text once typing has stopped,
// asking for the frame that does while it waits.
func (a *App) saveCmdTextAtRest(c *ui.Context) {
	b := &a.quickUI.box
	if b.code == nil {
		return
	}
	v := b.code.Buffer.Version()
	if v == b.saved {
		return
	}
	if v != b.seen {
		b.seen, b.changedAt = v, c.Now()
	}
	if wait := cmdTextSaveDelay - c.Now().Sub(b.changedAt); wait > 0 {
		c.After(wait)
		return
	}
	a.saveCmdText()
}

// saveCmdText writes the editor's text to its file, if it changed. A
// failed save is not tried again until the text changes.
func (a *App) saveCmdText() {
	b := &a.quickUI.box
	if b.code == nil || b.loadErr != nil {
		return
	}
	v := b.code.Buffer.Version()
	if v == b.saved {
		return
	}
	b.saved = v
	b.saveErr = config.SaveCommandEditorText(b.code.Buffer.String())
}

// cmdEditorToggle is the toolbar's button that shows and hides the
// editor, pressed while it shows.
func (a *App) cmdEditorToggle(c *ui.Context) {
	b := &a.quickUI.box
	t := ui.Toggle(c, &b.shown, "").Label(locales.CmdEditor()).Shrink(0)
	if b.shown {
		t.TextColor(theme.Accent).Border(1, theme.Accent).Background(theme.Selection)
	}
	t.Children(func() {
		ui.Icon(c, icons.Get("square-terminal")).FontSize(13)
		ui.Text(c, locales.CmdEditor()).SingleLine()
	})
	if t.Changed() {
		b.focus = b.shown
		a.saveLayout()
	}
}

// withCmdEditor lays out the Commands tab, tab, with the editor when it
// shows: commands are the groups and buttons, which it calls with the
// buttons, the toolbar over them, to put beside the groups.
func (a *App) withCmdEditor(c *ui.Context, tab ui.Element, s *Session, live bool, commands func(buttons func()), buttons func()) {
	b := &a.quickUI.box
	if !b.shown {
		commands(buttons)
		return
	}
	pane := func() { a.cmdEditorPane(c, s, live) }
	// The tab's width is the last frame's: the first frame, of no width,
	// has it beside.
	if w := tab.Bounds().W; w > 0 && w < cmdEditorBeside {
		top := func() {
			ui.Column(c).Fill().Children(func() { commands(buttons) })
		}
		split := ui.SplitVerticalPercent(c, &b.pct, top, pane).SplitReset(defaultCmdEditorPct).Grow(1).MinHeight(0).FillWidth()
		a.splitMoved(c, split, &b.pct, cmdEditorStackLimits)
		return
	}
	commands(func() {
		a.endSplit(c, &b.width, defaultCmdEditorWidth, cmdEditorLimits, buttons, pane).Grow(1).MinWidth(0)
	})
}

// cmdEditorPane is the editor: its header, the text, and the row that
// sends it.
func (a *App) cmdEditorPane(c *ui.Context, s *Session, live bool) {
	b := &a.quickUI.box
	code := a.cmdCode()
	code.Theme = codeTheme()
	code.Label = locales.CmdEditor()
	code.Placeholder = locales.CmdEditorPlaceholder()
	buf := code.Buffer
	ui.Column(c.Key("command-editor")).Fill().Background(theme.Background).Children(func() {
		ui.Row(c).Padding(6, 8).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
			ui.Text(c, locales.CmdEditor()).Bold().SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			ui.Spacer(c)
			if ui.Button(c, locales.CmdEditorSaveAs()).Shrink(0).Disabled(buf.Len() == 0).Clicked() {
				a.cmdSaveAsCommand()
			}
			if ui.Button(c, locales.CmdEditorClear()).Shrink(0).Disabled(buf.Len() == 0).Clicked() {
				buf.Set("") // undone with Cmd+Z
				b.focus = true
			}
		})
		a.cmdEditorBanner(c)
		text := editor.View(c.Key("text"), code).Grow(1).MinHeight(0).FillWidth()
		if b.focus {
			b.focus = false
			text.Focus()
		}
		send := text.Shortcut(cmdSendKey.Mods, cmdSendKey.Key)
		if text.Shortcut(0, ui.KeyEscape) {
			a.cmdKeysToTerminal()
		}
		// Narrow, Send to and Send wrap to a row of their own, at its end.
		ui.Row(c).Padding(6, 8).Gap(8).Wrap().AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(colors.line).Children(func() {
			a.cmdEditorOptions(c)
			ui.Row(c).Grow(1).Gap(8).Wrap().Justify(ui.End).AlignItems(ui.Center).Children(func() {
				a.quickTarget(c, "editor")
				if a.cmdSendButton(c, live && buf.Len() > 0) {
					send = true
				}
			})
		})
		if send && live {
			a.sendCmdEditor(c, s)
		}
	})
}

// cmdEditorBanner says why the text could not be read or saved.
func (a *App) cmdEditorBanner(c *ui.Context) {
	b := &a.quickUI.box
	var msg string
	switch {
	case b.loadErr != nil:
		msg = locales.CmdEditorLoadFailed(b.loadErr.Error())
	case b.saveErr != nil:
		msg = locales.ErrSaveFailed(b.saveErr.Error())
	default:
		return
	}
	ui.Row(c).Gap(8).Padding(4, 8).AlignItems(ui.Center).Background(colors.warningBanner).Children(func() {
		ui.Icon(c, icons.Get("triangle-alert")).FontSize(13).Shrink(0)
		ui.Text(c, msg).Grow(1).MinWidth(0)
	})
}

// cmdEditorOptions is the Options menu: Enter after each line, and
// emptying the editor of what it sent, both saved.
func (a *App) cmdEditorOptions(c *ui.Context) {
	ui.MenuButton(c, locales.CmdEditorOptions(), func(m *ui.Menu) {
		st := &a.settings
		if m.Item(locales.QuickEnter()).Checked(!st.CommandEditorNoEnter).Chosen() {
			st.CommandEditorNoEnter = !st.CommandEditorNoEnter
			a.saveSettings(*st)
		}
		if m.Item(locales.CmdEditorClearAfter()).Checked(st.CommandEditorClear).Chosen() {
			st.CommandEditorClear = !st.CommandEditorClear
			a.saveSettings(*st)
		}
	}).Label(locales.CmdEditorOptionsLabel()).Shrink(0)
}

// cmdSendButton is Send, Send selection while text is selected, with
// its key; it reports a click.
func (a *App) cmdSendButton(c *ui.Context, enabled bool) bool {
	label := locales.QuickSend()
	if cmdSelected(a.quickUI.box.code) {
		label = locales.CmdEditorSendSelection()
	}
	key := cmdSendKey.label(runtime.GOOS, false)
	btn := ui.PrimaryButton(c, "").Label(label).Tooltip(locales.CmdEditorSendTip(key)).Disabled(!enabled).Shrink(0).Gap(6)
	btn.Children(func() {
		ui.Text(c, label).SingleLine()
		ui.Text(c, key).FontSize(11).Opacity(0.75).SingleLine()
	})
	return btn.Clicked()
}

// cmdSelected tells whether text is selected in st.
func cmdSelected(st *editor.State) bool {
	for _, sel := range st.Selections() {
		if !sel.Empty() {
			return true
		}
	}
	return false
}

// cmdLines are lines first to last of the editor's text, from 0.
type cmdLines struct{ first, last int }

// cmdSelectedLines are the lines the selections touch, whole, so that a
// sloppy drag sends no half a command: in order, the runs of selections
// on the same or next lines joined. A selection that ends at the start
// of a line, as a drag to the start of the next line does, leaves that
// line out. nil without a selection.
func cmdSelectedLines(snap editor.Snapshot, sels []editor.Selection) []cmdLines {
	sels = slices.DeleteFunc(slices.Clone(sels), editor.Selection.Empty)
	slices.SortFunc(sels, func(x, y editor.Selection) int { return x.Start() - y.Start() })
	var runs []cmdLines
	for _, sel := range sels {
		first, last := snap.LineOf(sel.Start()), snap.LineOf(sel.End())
		if last > first && snap.LineStart(last) == sel.End() {
			last--
		}
		if n := len(runs); n > 0 && first <= runs[n-1].last+1 {
			runs[n-1].last = max(runs[n-1].last, last)
			continue
		}
		runs = append(runs, cmdLines{first, last})
	}
	return runs
}

// cmdSendText is what Send sends, and what Save as command saves, of
// snap: the lines selected, whole (cmdSelectedLines), the runs joined by
// newlines, or all of it.
func cmdSendText(snap editor.Snapshot, runs []cmdLines) string {
	if len(runs) == 0 {
		return snap.String()
	}
	parts := make([]string, len(runs))
	for i, r := range runs {
		parts[i] = snap.Slice(snap.LineStart(r.first), snap.LineEnd(r.last))
	}
	return strings.Join(parts, "\n")
}

// cmdClearLines are the edits that take the lines of runs out of snap,
// each with its newline: the one after it, or before it for the last
// line.
func cmdClearLines(snap editor.Snapshot, runs []cmdLines) []editor.Edit {
	edits := make([]editor.Edit, len(runs))
	for i, r := range runs {
		start, end := snap.LineStart(r.first), snap.LineEnd(r.last)
		if r.last+1 < snap.LineCount() {
			end = snap.LineStart(r.last + 1)
		} else if r.first > 0 {
			start = snap.LineEnd(r.first - 1)
		}
		edits[i] = editor.Edit{Start: start, End: end}
	}
	return edits
}

// sendCmdEditor sends the text, or the lines selected, as a quick
// command of its lines: to s, or to each session of the send target.
// With Clear after sending, the lines sent go from the editor once sent,
// unless it was edited since; Cmd+Z brings them back.
// The text keeps the keyboard, for the next lines; with Enter off, the
// terminal of s's row takes it, to press Enter there.
func (a *App) sendCmdEditor(c *ui.Context, s *Session) {
	st := a.quickUI.box.code
	buf := st.Buffer
	snap := buf.Snapshot()
	runs := cmdSelectedLines(snap, st.Selections())
	lines := config.QuickCommandLines(cmdSendText(snap, runs))
	if len(lines) == 0 {
		return
	}
	enter := !a.settings.CommandEditorNoEnter
	version := snap.Version()
	send := func(t *Session) {
		for i, line := range lines {
			a.sendLine(t, line, enter || i < len(lines)-1)
		}
	}
	a.sendToTargets(s, send, func() {
		if a.settings.CommandEditorClear && buf.Version() == version {
			if len(runs) == 0 {
				buf.Set("")
			} else {
				buf.Edit(cmdClearLines(snap, runs)...)
			}
		}
		if enter {
			a.quickUI.box.focus = true
		} else {
			a.cmdKeysToTerminal()
		}
	})
	c.Invalidate()
}

// cmdKeysToTerminal gives the keyboard to the terminal of the session
// shown, as Escape in the text does; a panel row has none.
func (a *App) cmdKeysToTerminal() {
	if s := a.current(); s != nil && !s.isPanelTab() && barLive(s) {
		s.bar.popup, s.bar.toTerm = popupNone, true
	}
}

// cmdSaveAsCommand opens the quick command editor on a new command of
// the lines selected, whole, or all of the text, its Enter the editor's.
func (a *App) cmdSaveAsCommand() {
	if a.windowOpen(WindowQuickCommand) {
		a.plat.FocusWindow(WindowQuickCommand) // the one open, as New command
		return
	}
	st := a.quickUI.box.code
	snap := st.Buffer.Snapshot()
	text := cmdSendText(snap, cmdSelectedLines(snap, st.Selections()))
	a.newQuickCommand()
	if ed := a.quickUI.ed; ed != nil {
		ed.text, ed.enter = text, !a.settings.CommandEditorNoEnter
	}
}
