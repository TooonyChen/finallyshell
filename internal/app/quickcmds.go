package app

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"finallyshell/internal/config"
	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The Commands tab of the bottom panel is FinalShell's quick command
// panel: the user's groups on the left, the commands of the group chosen
// as buttons on the right, a click on one sending it to the session
// shown. The commands are the same for every host, in quickcommands.json
// (config/quickcommands.go), read the first time the tab shows and
// written, as hosts.json is, on the UI thread as they change.
//
// A command is sent line by line through the command bar's sendLine, as
// if the bar sent each line: in the session's encoding, the folder
// follower told, and each line run kept in the host's history. With
// Enter off the last line goes without its carriage return, typed for
// the user to check; the lines before it still run.
//
// The editor of a command is a window of its own (quickcmds_editor.go).

// quickState is the Commands tab's state.
type quickState struct {
	// store is quickcommands.json's content: nil until read (quick).
	store *config.QuickCommands
	// loadErr is why the file could not be read, shown above the tab
	// until closed. noSave is set when it could not be copied aside
	// either: saving would replace it, so nothing is saved this run.
	loadErr error
	noSave  bool
	saveErr error // why the last save failed, nil once one works

	group  string // the group shown, "" for All
	query  string // the search
	naming *groupNaming
	ed     *quickEditor // the editor window's fields, while it is open
}

// groupNaming is the group whose name is being typed in place, in the
// list of groups.
type groupNaming struct {
	id    string // the group renamed; "" for a new one
	text  string
	fresh bool // focus the field, its text selected, as it shows
}

// quickDrag is a command being dragged: onto another to move it there,
// onto a group to move it into it.
type quickDrag struct{ id string }

// quick returns the quick commands, reading quickcommands.json the first
// time. A file that cannot be read gives an empty list; LoadQuickCommands
// copies a broken one aside first, else nothing is saved over it.
func (a *App) quick() *config.QuickCommands {
	q := &a.quickUI
	if q.store == nil {
		var err error
		q.store, err = config.LoadQuickCommands()
		if err != nil {
			var broken *config.BrokenFileError
			q.loadErr, q.noSave = err, !errors.As(err, &broken)
		}
	}
	return q.store
}

// saveQuick writes the quick commands, and redraws the windows that show
// them. It keeps the error for the tab to show, and returns it.
func (a *App) saveQuick() error {
	q := &a.quickUI
	a.redraw()
	if q.noSave {
		return nil // the banner says so
	}
	q.saveErr = config.SaveQuickCommands(q.store)
	return q.saveErr
}

// quickSession is the session the tab belongs to, nil in a new tab,
// and whether it can send now: a session of the send target
// (sendtarget.go) is connected, its own for Current session.
func (a *App) quickSession() (*Session, bool) {
	s := a.connSession()
	if s == nil || s.isNewTab() {
		s = nil
	}
	return s, len(a.sendTargets(s)) > 0
}

// sendQuick sends cmd to the shell of each session of the send target,
// s's for Current session, line by line, as the bar does: each line
// followed by a carriage return, but the last when cmd's Enter is off.
// In the session's row the terminal then has the keyboard, to press
// Enter there or type on.
func (a *App) sendQuick(c *ui.Context, s *Session, cmd *config.QuickCommand) {
	lines := config.QuickCommandLines(cmd.Text)
	send := func(t *Session) {
		for i, line := range lines {
			a.sendLine(t, line, cmd.Enter || i < len(lines)-1)
		}
	}
	a.sendToTargets(s, send, func() {
		if s != nil && barLive(s) && !a.current().isPanelTab() {
			s.bar.popup, s.bar.toTerm = popupNone, true
		}
	})
	c.Invalidate()
}

// barLine is the line "Put in the command bar" puts there: the command's
// only line, which the bar can hold; false for none.
func (a *App) barLine(s *Session, cmd *config.QuickCommand) (string, bool) {
	lines := config.QuickCommandLines(cmd.Text)
	if s == nil || !barLive(s) || !a.commandBarOn() || len(lines) != 1 || config.HasControl(lines[0], true) {
		return "", false
	}
	return lines[0], true
}

// putInBar puts line in s's command bar, without sending it, the caret
// at its end and the keyboard in the bar; from a panel row, the
// session's row shows again.
func (a *App) putInBar(c *ui.Context, s *Session, line string) {
	if p := a.current(); p.isPanelTab() {
		a.backToTerminal(p)
	}
	b := &s.bar
	b.popup = popupNone
	b.text, b.newCaret, b.caretSet, b.focus = line, utf8.RuneCountInString(line), true, true
	c.Invalidate()
}

// quickPanel is the Commands tab.
func (a *App) quickPanel(c *ui.Context) {
	q := &a.quickUI
	store := a.quick()
	if store.Group(q.group) == nil {
		q.group = ""
	}
	s, live := a.quickSession()
	ui.Column(c.Key("quick-commands")).Grow(1).MinHeight(0).Children(func() {
		a.quickBanner(c)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.quickGroups(c, store)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.quickToolbar(c, s, live)
				a.quickButtons(c, store, s, live)
			})
		})
	})
}

// quickBanner says why the file could not be read or saved.
func (a *App) quickBanner(c *ui.Context) {
	q := &a.quickUI
	var msg string
	var broken *config.BrokenFileError
	switch {
	case q.saveErr != nil:
		msg = locales.ErrSaveFailed(q.saveErr.Error())
	case errors.As(q.loadErr, &broken):
		msg = locales.QuickLoadBroken(filepath.Base(broken.Copy))
	case q.loadErr != nil:
		msg = locales.QuickLoadFailed(q.loadErr.Error())
	default:
		return
	}
	ui.Row(c).Gap(8).Padding(4, 8).AlignItems(ui.Center).Background(colors.warningBanner).Children(func() {
		ui.Icon(c, icons.Get("triangle-alert")).FontSize(13).Shrink(0)
		ui.Text(c, msg).Grow(1).MinWidth(0)
		// The file not saved over stays said, as long as it is so.
		if !(q.saveErr == nil && q.noSave) && widgets.IconButton(c, "x", locales.Close()).Size(20, 20).Clicked() {
			if q.saveErr != nil {
				q.saveErr = nil
			} else {
				q.loadErr = nil
			}
		}
	})
}

// quickGroups is the list of groups at the tab's left: All, the user's
// groups, and New group.
func (a *App) quickGroups(c *ui.Context, store *config.QuickCommands) {
	q := &a.quickUI
	ui.Column(c).Width(170).Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(colors.line).Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(6).Gap(2).Children(func() {
			a.quickGroupRow(c, store, nil)
			for _, g := range store.Groups {
				a.quickGroupRow(c, store, g)
			}
			if q.naming != nil && q.naming.id == "" {
				a.groupNameField(c)
				return
			}
			add := ui.Row(c.Key("new-group")).Gap(4).Padding(6, 8).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				TextColor(theme.Accent).Children(func() {
				ui.Icon(c, icons.Get("plus")).FontSize(13)
				ui.Text(c, locales.QuickNewGroup()).SingleLine()
			})
			if add.Clicked() {
				q.naming = &groupNaming{text: locales.QuickNewGroup(), fresh: true}
			}
		})
	})
}

// quickGroupRow is the row of g, or of All for nil: a click shows its
// commands, a command dropped on a group moves into it, and a group's
// context menu renames or deletes it.
func (a *App) quickGroupRow(c *ui.Context, store *config.QuickCommands, g *config.QuickGroup) {
	q := &a.quickUI
	id, name, count := "", locales.QuickAll(), len(store.Commands)
	if g != nil {
		id, name, count = g.ID, g.Name, len(store.InGroup(g.ID))
		if q.naming != nil && q.naming.id == id {
			a.groupNameField(c)
			return
		}
	}
	on := q.group == id
	row := ui.Row(c.Key("group-"+id)).Gap(6).Padding(5, 8).Radius(5).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Border(1, ui.Transparent).Label(name)
	if on {
		row.Background(theme.Selection).TextColor(theme.Accent)
	} else if row.Hovered() {
		row.Background(theme.SurfaceHover)
	}
	if g != nil {
		if _, over := ui.DragOver[quickDrag](row); over {
			row.Border(1, theme.Accent)
		}
		if d, ok := ui.Drop[quickDrag](row); ok {
			if cmd := store.Command(d.id); cmd != nil && cmd.Group != g.ID {
				cmd.Group = g.ID
				a.saveQuick()
			}
		}
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(locales.Rename()).Chosen() {
				q.naming = &groupNaming{id: g.ID, text: g.Name, fresh: true}
			}
			m.Separator()
			if m.Item(locales.Delete()).Danger().Chosen() {
				a.askDeleteGroup(g)
			}
		})
	}
	row.Children(func() {
		t := ui.Text(c, name).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if on {
			t.Bold()
		}
		ui.Text(c, strconv.Itoa(count)).FontSize(11).TextColor(theme.TextMuted).Shrink(0)
	})
	if row.Clicked() {
		q.group = id
	}
}

// groupNameField is the field in place of a group's name, or of New
// group: Enter, or the focus going elsewhere, keeps the name typed, and
// Escape gives it up. An empty name keeps none.
func (a *App) groupNameField(c *ui.Context) {
	n := a.quickUI.naming
	in := ui.TextInput(c.Key("group-name"), &n.text).FillWidth().MinWidth(0).Label(locales.QuickGroupName())
	switch {
	case n.fresh:
		n.fresh = false
		in.Focus().SetTextSelection(0, utf8.RuneCountInString(n.text))
	case in.Submitted():
		a.endGroupNaming(true)
	case in.Shortcut(0, ui.KeyEscape):
		a.endGroupNaming(false)
	case in.PressedOutside():
		a.endGroupNaming(true)
	case in.FocusLeft() && !in.FocusWithin():
		a.endGroupNaming(true)
	}
}

// endGroupNaming hides the group name field, adding the group or
// renaming it when keep is set.
func (a *App) endGroupNaming(keep bool) {
	q := &a.quickUI
	n := q.naming
	q.naming = nil
	name := oneLine(n.text)
	if !keep || name == "" {
		return
	}
	store := a.quick()
	if n.id == "" {
		q.group = store.AddGroup(name).ID
	} else if g := store.Group(n.id); g != nil && g.Name != name {
		g.Name = name
	} else {
		return
	}
	a.saveQuick()
}

// oneLine is s trimmed, its control characters as blanks.
func oneLine(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
}

// askDeleteGroup asks, then deletes g; its commands stay, in no group.
func (a *App) askDeleteGroup(g *config.QuickGroup) {
	q := Confirmation{
		Title:   locales.AppTitle(),
		Message: locales.QuickDeleteGroupTitle(g.Name),
		Detail:  locales.QuickDeleteGroupDetail(),
		OK:      locales.Delete(),
		Cancel:  locales.Cancel(),
	}
	id := g.ID
	go func() {
		if a.plat.Confirm(q) {
			a.post(func() {
				if a.quick().RemoveGroup(id) {
					a.saveQuick()
				}
			})
		}
	}()
}

// askDeleteQuick asks, then deletes cmd.
func (a *App) askDeleteQuick(cmd *config.QuickCommand) {
	q := Confirmation{
		Title:   locales.AppTitle(),
		Message: locales.QuickDeleteTitle(cmd.Title()),
		Detail:  locales.QuickDeleteDetail(),
		OK:      locales.Delete(),
		Cancel:  locales.Cancel(),
	}
	id := cmd.ID
	go func() {
		if a.plat.Confirm(q) {
			a.post(func() {
				if a.quick().RemoveCommand(id) {
					a.saveQuick()
				}
			})
		}
	}()
}

// quickToolbar is the row above the buttons: New command, why they are
// disabled, and the search.
func (a *App) quickToolbar(c *ui.Context, s *Session, live bool) {
	ui.Row(c).Padding(6, 8).Gap(8).AlignItems(ui.Center).Children(func() {
		add := ui.Button(c, "").Label(locales.QuickNewCommand()).Shrink(0).Children(func() {
			ui.Icon(c, icons.Get("plus")).FontSize(13)
			ui.Text(c, locales.QuickNewCommand()).SingleLine()
		})
		if add.Clicked() {
			a.newQuickCommand()
		}
		if !live && s != nil {
			widgets.Muted(c, locales.QuickOffline()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		ui.Spacer(c)
		a.quickTarget(c)
		ui.SearchField(c, &a.quickUI.query).Placeholder(locales.QuickSearch()).Label(locales.QuickSearch()).Width(220).Shrink(1).MinWidth(120)
	})
}

// shownQuick are the commands the buttons show: the group's, or every
// one under All, that the search finds in their names or their text,
// ignoring case.
func (a *App) shownQuick(store *config.QuickCommands) []*config.QuickCommand {
	cmds := store.InGroup(a.quickUI.group)
	query := strings.ToLower(strings.TrimSpace(a.quickUI.query))
	if query == "" {
		return cmds
	}
	var out []*config.QuickCommand
	for _, c := range cmds {
		if strings.Contains(strings.ToLower(c.Title()), query) || strings.Contains(strings.ToLower(c.Text), query) {
			out = append(out, c)
		}
	}
	return out
}

// quickButtons are the commands shown, as buttons in rows that wrap. A
// command dropped on another takes its place; dropped after the last, it
// goes to the end.
func (a *App) quickButtons(c *ui.Context, store *config.QuickCommands, s *Session, live bool) {
	cmds := a.shownQuick(store)
	// What the buttons ask that changes the list is done after it is
	// built: the loop goes on over the list as it was.
	var act func()
	area := ui.Scroll(c.Key("quick-buttons")).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c).Wrap().Gap(6).Padding(2, 8, 8, 8).Children(func() {
			for _, cmd := range cmds {
				a.quickButton(c, store, s, live, cmd, &act)
			}
		})
		if len(cmds) == 0 {
			msg := locales.QuickNoneInGroup()
			switch {
			case len(store.Commands) == 0:
				msg = locales.QuickNone()
			case strings.TrimSpace(a.quickUI.query) != "":
				msg = locales.QuickNoMatches()
			}
			widgets.Muted(c, msg).Padding(4, 10)
		}
	})
	if d, ok := ui.Drop[quickDrag](area); ok {
		act = func() {
			if store.MoveCommand(d.id, "") {
				a.saveQuick()
			}
		}
	}
	if act != nil {
		act()
	}
}

// quickButton is cmd's button: its name and, in gray, its one line, or
// how many lines it has. A click sends it; it drags, and takes a command
// dropped on it; its context menu has the rest.
func (a *App) quickButton(c *ui.Context, store *config.QuickCommands, s *Session, live bool, cmd *config.QuickCommand, act *func()) {
	lines := config.QuickCommandLines(cmd.Text)
	title := cmd.Title()
	btn := ui.Button(c.Key(cmd.ID), "").Label(title).Tooltip(cmd.Text).AccessDisabled(!live).
		MaxWidth(420).MinWidth(0).Border(1, theme.Border).Drag(quickDrag{cmd.ID})
	switch {
	case btn.Dragging():
		btn.Opacity(0.4)
	case !live:
		btn.Opacity(0.5)
	}
	if _, over := ui.DragOver[quickDrag](btn); over {
		btn.Border(1, theme.Accent)
	}
	btn.Children(func() {
		ui.Text(c, title).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		switch {
		case len(lines) > 1:
			ui.Text(c, locales.QuickLines(len(lines))).FontSize(11).TextColor(theme.TextMuted).
				Padding(0, 4).Border(1, theme.Border).Radius(3).SingleLine().Shrink(0)
		case len(lines) == 1 && strings.TrimSpace(cmd.Name) != "":
			ui.Text(c, strings.TrimSpace(lines[0])).Font("monospace").FontSize(12).TextColor(theme.TextMuted).
				SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
	})
	btn.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.QuickSend()).Disabled(!live).Chosen() {
			*act = func() { a.sendQuick(c, s, cmd) }
		}
		line, ok := a.barLine(s, cmd)
		if m.Item(locales.QuickPutInBar()).Disabled(!ok).Chosen() {
			*act = func() { a.putInBar(c, s, line) }
		}
		m.Separator()
		if m.Item(locales.QuickEdit()).Chosen() {
			a.editQuickCommand(cmd)
		}
		if m.Item(locales.QuickDuplicate()).Chosen() {
			*act = func() {
				dup := *cmd
				if n := strings.TrimSpace(cmd.Name); n != "" {
					dup.Name = locales.QuickCopyName(n)
				}
				store.AddCommand(&dup, cmd.ID)
				a.saveQuick()
			}
		}
		m.Submenu(locales.QuickMoveTo(), func(m *ui.Menu) {
			to := func(group string) {
				*act = func() {
					if cmd.Group != group {
						cmd.Group = group
						a.saveQuick()
					}
				}
			}
			if m.Item(locales.QuickNoGroup()).ID("no-group").Radio(store.Group(cmd.Group) == nil).Chosen() {
				to("")
			}
			for _, g := range store.Groups {
				if m.Item(g.Name).ID("group-" + g.ID).Radio(cmd.Group == g.ID).Chosen() {
					to(g.ID)
				}
			}
		})
		m.Separator()
		if m.Item(locales.Delete()).Danger().Chosen() {
			a.askDeleteQuick(cmd)
		}
	})
	if d, ok := ui.Drop[quickDrag](btn); ok {
		*act = func() {
			if store.MoveCommand(d.id, cmd.ID) {
				a.saveQuick()
			}
		}
	}
	if btn.Clicked() && live {
		*act = func() { a.sendQuick(c, s, cmd) }
	}
}
