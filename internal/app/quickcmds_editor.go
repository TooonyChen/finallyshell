package app

import (
	"fmt"
	"strings"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// WindowQuickCommand is the editor of a quick command (quickcmds.go),
// FinalShell's: its name, its group, its text and whether Enter follows
// each line. One at a time, as the connection editor.
const WindowQuickCommand WindowKind = "quickCommand"

// quickEditor is what the editor shows, until Save keeps it.
type quickEditor struct {
	id                string // the command edited; "" for a new one
	name, group, text string
	enter             bool
	checked           bool  // Save was pressed: the error shows
	saveErr           error // why saving failed
}

// newQuickCommand opens the editor on a new command, in the group shown.
func (a *App) newQuickCommand() { a.openQuickEditor(nil) }

// editQuickCommand opens the editor on cmd.
func (a *App) editQuickCommand(cmd *config.QuickCommand) { a.openQuickEditor(cmd) }

// openQuickEditor opens the editor on cmd, nil for a new one, or brings
// the open one to the front, whatever it edits.
func (a *App) openQuickEditor(cmd *config.QuickCommand) {
	if a.windowOpen(WindowQuickCommand) {
		a.plat.FocusWindow(WindowQuickCommand)
		return
	}
	ed := &quickEditor{group: a.quickUI.group, enter: true}
	title := locales.QuickNewCommand
	if cmd != nil {
		ed.id, ed.name, ed.group, ed.text, ed.enter = cmd.ID, cmd.Name, cmd.Group, cmd.Text, cmd.Enter
		title = locales.QuickEditCommand
	}
	a.quickUI.ed = ed
	a.openWindow(WindowSpec{
		Kind:  WindowQuickCommand,
		Title: title,
		Width: 560, Height: 380,
		MinWidth: 460, MinHeight: 360,
		Closed: func() {
			if a.quickUI.ed == ed {
				a.quickUI.ed = nil
			}
		},
	}, a.quickEditorView)
}

// problem is what is wrong with the fields, "" for nothing.
func (ed *quickEditor) problem() string {
	if strings.TrimSpace(ed.text) == "" {
		return locales.QuickErrCommand()
	}
	return ""
}

// saveQuickEditor keeps the editor's command and closes it, unless its
// text is empty or saving fails. A command deleted while the editor was
// open comes back.
func (a *App) saveQuickEditor() {
	ed := a.quickUI.ed
	ed.checked = true
	if ed.problem() != "" {
		return
	}
	store := a.quick()
	cmd := store.Command(ed.id)
	if cmd == nil {
		cmd = &config.QuickCommand{}
		store.AddCommand(cmd, "")
		ed.id = cmd.ID
	}
	cmd.Name = oneLine(ed.name)
	cmd.Group = ""
	if store.Group(ed.group) != nil {
		cmd.Group = ed.group
	}
	// Trailing blanks are kept: a command typed without Enter may end in
	// one, for what the user types after it.
	cmd.Text = strings.TrimRight(ed.text, "\r\n")
	cmd.Enter = ed.enter
	if ed.saveErr = a.saveQuick(); ed.saveErr != nil {
		return
	}
	a.closeWindow(WindowQuickCommand)
}

// quickGroupChoices are the editor's group choices: No group, then the
// groups, labels that two groups of one name would share told apart.
func quickGroupChoices(store *config.QuickCommands) (ids, labels []string) {
	ids, labels = []string{""}, []string{locales.QuickNoGroup()}
	seen := map[string]int{labels[0]: 1}
	for _, g := range store.Groups {
		label := g.Name
		if n := seen[label]; n > 0 {
			label = fmt.Sprintf("%s (%d)", g.Name, n+1)
		}
		seen[g.Name]++
		ids, labels = append(ids, g.ID), append(labels, label)
	}
	return ids, labels
}

// quickEditorView is the editor's window: Enter in the name saves, and
// Escape cancels.
func (a *App) quickEditorView(c *ui.Context) {
	ed := a.quickUI.ed
	if ed == nil {
		return
	}
	store := a.quick()
	submit := false
	var problem string
	if ed.checked {
		problem = ed.problem()
	}
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Grow(1).MinHeight(0).Padding(18, 20, 8, 20).Children(func() {
			ui.Form(c, func() {
				ui.Field(c, locales.FieldName(), func() {
					in := ui.TextInput(c, &ed.name).Placeholder(locales.QuickNamePlaceholder()).FillWidth()
					if ed.id == "" {
						in.AutoFocus()
					}
					submit = in.Submitted()
				})
				ui.Field(c, locales.QuickFieldGroup(), func() {
					ids, labels := quickGroupChoices(store)
					chosen := labels[0]
					for i, id := range ids {
						if id == ed.group {
							chosen = labels[i]
						}
					}
					if ui.Select(c, &chosen, labels).FillWidth().Changed() {
						for i, l := range labels {
							if l == chosen {
								ed.group = ids[i]
							}
						}
					}
				})
				ui.Field(c, locales.QuickFieldCommand(), func() {
					area := ui.TextArea(c, &ed.text).Font("monospace").Lines(5, 9).FillWidth()
					if ed.id != "" {
						area.AutoFocus()
					}
				}).Error(problem)
				hint := locales.QuickEnterOn()
				if !ed.enter {
					hint = locales.QuickEnterOff()
				}
				ui.Field(c, "", func() {
					ui.Checkbox(c, &ed.enter, locales.QuickEnter())
				}).Description(hint)
			})
		})
		ui.Row(c).Gap(8).Padding(10, 20, 16, 20).AlignItems(ui.Center).Children(func() {
			if ed.saveErr != nil {
				ui.Text(c, locales.ErrSaveFailed(ed.saveErr.Error())).TextColor(theme.Danger).Grow(1).MinWidth(0)
			} else {
				ui.Spacer(c)
			}
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.closeWindow(WindowQuickCommand)
			}
			if ui.PrimaryButton(c, locales.Save()).Clicked() {
				submit = true
			}
		})
	})
	if submit && a.quickUI.ed == ed {
		a.saveQuickEditor()
	}
}
