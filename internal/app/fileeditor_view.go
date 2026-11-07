package app

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"unicode/utf8"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The editor window's colors: FinalShell's light gray bars around the
// white page of the text.
var (
	editorBarBG  = ui.Hex("#f3f4f6")
	editorBorder = ui.Hex("#e5e7eb")
)

// codeTheme is the code's colors: the editor's light ones, with the
// app's accent, and the line with the caret a faint gray.
var codeTheme = func() *editor.Theme {
	th := editor.DefaultTheme(theme)
	th.ActiveLine = ui.Hex("#f6f7f9")
	th.LineNumber = ui.Hex("#9ca3af")
	th.Selection = theme.Accent.Alpha(0.18)
	th.InactiveSelection = theme.Accent.Alpha(0.1)
	return th
}()

// fileEditorView is the editor's window: the tabs, a toolbar, the text
// and a status bar.
func (a *App) fileEditorView(c *ui.Context) {
	fe := &a.fe
	if title := a.fileEditorTitle(); title != fe.title {
		fe.title = title
		a.plat.RetitleWindow(WindowFileEditor)
	}
	a.fileEditorKeys(c)
	t := fe.current()
	ui.Column(c).Fill().Background(theme.Background).Children(func() {
		a.editorTabStrip(c)
		a.editorToolbar(c, t)
		if t == nil {
			ui.Box(c).Grow(1)
			return
		}
		ui.Column(c.Key(t.id)).Grow(1).MinHeight(0).Children(func() {
			switch t.state {
			case tabReady:
				a.editorBanners(c, t)
				t.code.SoftWrap = fe.wrap
				t.code.Theme = codeTheme
				// Keyed, so that a banner coming or going keeps it.
				code := editor.View(c.Key("code"), t.code).Grow(1).FillWidth().AutoFocus()
				if fe.refocus {
					fe.refocus = false
					code.Focus()
				}
			default:
				a.editorNotice(c, t)
			}
		})
		a.editorStatusBar(c, t)
	})
	a.editorQuestions(c)
}

// fileEditorKeys are the window's keys: Cmd+S (Ctrl+S) saves, Cmd+W
// (Ctrl+W) and the app's key to close a tab close the tab, Ctrl+Tab and
// Ctrl+Shift+Tab go through the tabs. Escape is the editor's alone (it
// closes the find bar and the completions, and takes away extra carets):
// unlike the app's other windows, it never closes this one, as a stray
// Escape, which editing is full of, would lose the window.
func (a *App) fileEditorKeys(c *ui.Context) {
	fe := &a.fe
	t := fe.current()
	cmd := ui.Ctrl
	if macOS {
		cmd = ui.Super
	}
	if c.Shortcut(cmd, ui.KeyS) && t != nil {
		a.saveTab(t, false)
	}
	closeTab := a.shortcut(scCloseTab)
	if c.Shortcut(cmd, ui.KeyW) || !closeTab.none() && c.Shortcut(closeTab.Mods, closeTab.Key) {
		if t != nil {
			a.requestCloseTab(t)
		} else {
			a.requestCloseFileEditor()
		}
	}
	if n := len(fe.tabs); n > 0 {
		if c.Shortcut(ui.Ctrl, ui.KeyTab) {
			fe.sel = (fe.sel + 1) % n
		}
		if c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab) {
			fe.sel = (fe.sel - 1 + n) % n
		}
	}
}

// editorTabStrip is the row of tabs: a file's name, a dot while it has
// changes, and a button that closes it.
func (a *App) editorTabStrip(c *ui.Context) {
	fe := &a.fe
	ui.Row(c).Background(editorBarBG).BorderWidth(0, 0, 1, 0).BorderColor(editorBorder).PaddingX(4).Shrink(0).Children(func() {
		tabs := ui.TabsBase(c, &fe.sel, len(fe.tabs))
		tabs.List.Grow(1).MinWidth(0).Clip().Children(func() {
			for i, t := range fe.tabs {
				tab := tabs.Tab(i).Row().Gap(6).AlignItems(ui.Center).Padding(7, 6, 6, 12).
					MinWidth(0).Shrink(1).Tooltip(t.title()).BorderWidth(0, 1, 0, 0).BorderColor(editorBorder)
				selected := i == fe.sel
				if selected {
					tab.Background(theme.Background).BorderWidth(2, 1, 0, 0).BorderColor(editorBorder).
						Padding(5, 6, 6, 12)
				}
				hovered := tab.Hovered()
				tab.Children(func() {
					if selected {
						ui.Box(c).Absolute().Top(-2).Left(0).Right(0).Height(2).Background(theme.Accent)
					}
					ui.Icon(c, icons.Get("file-text")).FontSize(13).TextColor(theme.TextMuted)
					name := ui.Text(c, t.name()).SingleLine().Shrink(1).MinWidth(0)
					if !selected {
						name.TextColor(theme.TextMuted)
					}
					a.tabCloseButton(c, t, hovered || selected)
				})
			}
		})
	})
}

// tabCloseButton closes the tab; a tab with changes shows a dot in its
// place until the pointer is over it.
func (a *App) tabCloseButton(c *ui.Context, t *editTab, show bool) {
	label := locales.ScCloseTab()
	b := ui.Box(c.Key("close")).Size(18, 18).Center().Radius(4).Role(ui.RoleButton).Label(label).Shrink(0)
	over := b.Hovered()
	if over {
		b.Background(theme.Text.Alpha(0.08))
	}
	b.Children(func() {
		switch {
		case t.modified() && !over:
			ui.Box(c).Size(8, 8).Radius(4).Background(theme.Text.Alpha(0.6))
		case show || over:
			ui.Icon(c, icons.Get("x")).FontSize(12).TextColor(theme.TextMuted)
		}
	})
	if b.Clicked() {
		a.requestCloseTab(t)
	}
}

// editorToolbar is FinalShell's row of buttons above the text.
func (a *App) editorToolbar(c *ui.Context, t *editTab) {
	ready := t != nil && t.state == tabReady
	button := func(icon, label string, enabled bool, act func()) {
		b := widgets.IconButton(c, icon, label)
		if !enabled {
			b.Opacity(0.35)
		}
		if b.Clicked() && enabled {
			act()
			// The keyboard back to the text, but for the bars these open.
			a.fe.refocus = icon != "search" && icon != "replace" && icon != "hash"
		}
	}
	run := func(cmd editor.Command) func() { return func() { t.code.Run(cmd) } }
	ui.Row(c).Gap(2).Padding(3, 8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(editorBorder).Shrink(0).Children(func() {
		button("save", locales.Save(), ready && !t.saving && a.sftpFor(t) != nil, func() { a.saveTab(t, false) })
		sep := func() { ui.Box(c).Size(1, 16).Background(editorBorder).Margin(0, 4) }
		sep()
		button("undo-2", locales.Undo(), ready && t.code.Buffer.CanUndo(), run(editor.Undo))
		button("redo-2", locales.Redo(), ready && t.code.Buffer.CanRedo(), run(editor.Redo))
		sep()
		button("search", locales.Find(), ready, run(editor.Find))
		button("replace", locales.EditorReplace(), ready, run(editor.FindReplace))
		button("hash", locales.EditorGoToLine(), ready, run(editor.GoToLine))
		sep()
		wrap := widgets.IconButton(c, "wrap-text", locales.EditorSoftWrap())
		if a.fe.wrap {
			wrap.Background(theme.Accent.Alpha(0.14))
		}
		if wrap.Clicked() {
			a.fe.wrap, a.fe.refocus = !a.fe.wrap, true
		}
		button("refresh-cw", locales.EditorReloadFromServer(), ready && !t.saving && a.sftpFor(t) != nil, func() {
			if t.modified() {
				t.conflict, t.conflictGone = true, false // asks: reload or overwrite
				return
			}
			a.reloadTab(t)
		})
		ui.Box(c).Grow(1)
		if t != nil && (t.saving || t.state == tabLoading) {
			ui.Spinner(c)
		}
	})
}

// editorBanners are the notes above the text: the session is not
// connected, or the file is not UTF-8.
func (a *App) editorBanners(c *ui.Context, t *editTab) {
	banner := func(bg, fg ui.Color, icon, text string, actions func()) {
		ui.Row(c).Gap(8).Padding(5, 10).AlignItems(ui.Center).Background(bg).BorderWidth(0, 0, 1, 0).BorderColor(editorBorder).Shrink(0).Children(func() {
			ui.Icon(c, icons.Get(icon)).FontSize(14).TextColor(fg)
			ui.Text(c, text).Grow(1).Shrink(1)
			if actions != nil {
				actions()
			}
		})
	}
	if a.sftpFor(t) == nil {
		banner(ui.Hex("#fef2f2"), theme.Danger, "unplug", locales.EditorDisconnected(t.host), nil)
	}
	if t.notUTF8 && t.enc == encUTF8 {
		banner(ui.Hex("#fffbeb"), theme.Warning, "triangle-alert", locales.EditorNotUTF8(), func() {
			if ui.Button(c, locales.EditorReopenAs(string(encGB18030))).Clicked() {
				a.setEncoding(t, encGB18030)
				a.fe.refocus = true
			}
			if widgets.IconButton(c, "x", locales.Close()).Clicked() {
				t.notUTF8, a.fe.refocus = false, true
			}
		})
	}
}

// editorNotice fills the tab of a file not in the editor: reading it, too
// big, binary, or failed.
func (a *App) editorNotice(c *ui.Context, t *editTab) {
	ui.Column(c).Grow(1).Center().Gap(12).Padding(24).Children(func() {
		switch t.state {
		case tabLoading:
			ui.Text(c, locales.EditorReading(t.name())).FontSize(15)
			frac := -1.0
			if t.total > 0 {
				frac = float64(t.done) / float64(t.total)
			}
			ui.Progress(c, frac).Width(320).Label(locales.EditorReading(t.name()))
			if t.total > 0 {
				widgets.Muted(c, locales.EditorProgress(formatSize(t.done), formatSize(t.total))).FontSize(12)
			}
		case tabTooLarge:
			ui.Icon(c, icons.Get("file-text")).FontSize(40).TextColor(theme.TextMuted)
			ui.Text(c, locales.EditorTooLarge(t.name(), formatSize(t.total))).FontSize(15).Bold()
			widgets.Muted(c, locales.EditorTooLargeBody(formatSize(maxEditSize)))
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, locales.ScCloseTab()).Clicked() {
					a.closeTab(t)
				}
				a.openWithAppButton(c, t, false)
				if ui.PrimaryButton(c, locales.EditorOpenAnyway()).Clicked() {
					a.loadTab(t, true)
				}
			})
		case tabBinary:
			ui.Icon(c, icons.Get("file")).FontSize(40).TextColor(theme.TextMuted)
			ui.Text(c, locales.EditorBinary(t.name())).FontSize(15).Bold()
			widgets.Muted(c, locales.EditorBinaryBody())
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, locales.ScCloseTab()).Clicked() {
					a.closeTab(t)
				}
				a.openWithAppButton(c, t, true)
			})
		case tabFailed:
			ui.Icon(c, icons.Get("triangle-alert")).FontSize(36).TextColor(theme.Danger)
			ui.Text(c, locales.EditorCannotOpen(t.name(), fileErrText(t.err, t.host))).FontSize(15).TextAlign(ui.Center)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, locales.ScCloseTab()).Clicked() {
					a.closeTab(t)
				}
				if ui.PrimaryButton(c, locales.Retry()).Clicked() {
					a.loadTab(t, false)
				}
			})
		}
		switch {
		case t.opening:
			ui.Row(c).Gap(6).Children(func() {
				ui.Spinner(c)
				widgets.Muted(c, locales.EditorOpeningWithApp())
			})
		case t.openErr != nil:
			ui.Text(c, locales.EditorOpenWithAppFailed(fileErrText(t.openErr, t.host))).TextColor(theme.Danger)
		case t.openedWith != "":
			widgets.Muted(c, locales.EditorOpenedWithApp(t.openedWith)).FontSize(12).Selectable()
		}
	})
}

func (a *App) openWithAppButton(c *ui.Context, t *editTab, primary bool) {
	label := locales.EditorOpenWithApp()
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, label)
	} else {
		b = ui.Button(c, label)
	}
	ok := !t.opening && a.sftpFor(t) != nil
	b.Disabled(!ok)
	if b.Clicked() && ok {
		a.openWithSystem(t)
	}
}

// fileErrText says why a file could not be read or saved.
func fileErrText(err error, host string) string {
	switch {
	case errors.Is(err, errNotConnected):
		return locales.EditorDisconnected(host)
	case errors.Is(err, fs.ErrPermission):
		return locales.EditorPermissionDenied()
	case errors.Is(err, fs.ErrNotExist):
		return locales.EditorNoSuchFile()
	}
	return err.Error()
}

// editorStatusBar shows how the save went on the left, and the language,
// the caret's line and column, the encoding and the line endings on the
// right; the last two are menus.
func (a *App) editorStatusBar(c *ui.Context, t *editTab) {
	ui.Row(c).Gap(4).Padding(2, 10).AlignItems(ui.Center).Background(editorBarBG).BorderWidth(1, 0, 0, 0).BorderColor(editorBorder).Shrink(0).Height(26).Children(func() {
		small := func(s string) ui.Element { return ui.Text(c, s).FontSize(12).SingleLine() }
		status := ui.Row(c).Gap(5).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center)
		status.Children(func() {
			switch {
			case t.saving:
				small(locales.EditorSaving()).TextColor(theme.TextMuted)
			case t.saveErr != nil:
				ui.Icon(c, icons.Get("triangle-alert")).FontSize(12).TextColor(theme.Danger)
				small(locales.EditorSaveFailed(fileErrText(t.saveErr, t.host))).TextColor(theme.Danger).Selectable()
			case t.state == tabReady && a.sftpFor(t) == nil:
				small(locales.EditorNotConnected()).TextColor(theme.Danger)
			case !t.savedAt.IsZero() && !t.modified():
				small(locales.EditorSaved()).TextColor(theme.Success)
			}
		})
		if t.state != tabReady {
			return
		}
		item := func(s, label string) ui.Element {
			return small(s).TextColor(theme.TextMuted).Padding(1, 6).Tooltip(label)
		}
		lang := locales.EditorPlainText()
		if l := t.code.Buffer.Language(); l != nil {
			lang = l.Name
		}
		item(lang, locales.EditorLanguage())
		pos := t.code.CursorPos()
		line := t.code.Buffer.Line(pos.Line)
		col := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
		if item(locales.EditorLineCol(pos.Line+1, col), locales.EditorGoToLine()).Role(ui.RoleButton).Clicked() {
			t.code.Run(editor.GoToLine)
		}
		menu := func(s, label string, build func(m *ui.Menu)) {
			b := item(s, label).Role(ui.RoleButton).Radius(4)
			if b.Hovered() {
				b.Background(theme.Text.Alpha(0.07))
			}
			b.Menu(build)
		}
		enc := string(t.enc)
		if t.bom {
			enc += " BOM"
		}
		menu(enc, locales.EditorEncoding(), func(m *ui.Menu) {
			for _, e := range textEncodings {
				if m.Item(string(e)).Checked(t.enc == e).Chosen() {
					a.setEncoding(t, e)
					a.fe.refocus = true
				}
			}
		})
		ending := "LF"
		if t.code.Buffer.LineEnding() == "\r\n" {
			ending = "CRLF"
		}
		menu(ending, locales.EditorLineEndings(), func(m *ui.Menu) {
			for _, e := range []string{"\n", "\r\n"} {
				label := strings.NewReplacer("\r", "CR", "\n", "LF").Replace(e)
				if m.Item(label).Checked(t.code.Buffer.LineEnding() == e).Chosen() {
					t.code.Buffer.SetLineEnding(e)
					a.fe.refocus = true
				}
			}
		})
	})
}

// editorQuestions ask before closing a tab or the window with changes,
// and before a save replaces a file that changed on the server.
func (a *App) editorQuestions(c *ui.Context) {
	fe := &a.fe
	if t := fe.askTab; t != nil {
		open := true
		buttons := []string{locales.DontSave(), locales.Cancel()}
		canSave := a.sftpFor(t) != nil
		if canSave {
			buttons = append(buttons, locales.Save())
		}
		body := locales.EditorUnsavedBody()
		if !canSave {
			body += "\n" + locales.EditorDisconnected(t.host)
		}
		switch ui.AlertDialog(c, &open, locales.EditorUnsavedTitle(t.name()), body, buttons...) {
		case 0:
			a.closeTab(t)
		case 2:
			t.closeAfterSave = true
			a.saveTab(t, false)
		}
		if !open {
			fe.askTab, fe.refocus = nil, true
		}
	}
	if fe.askWindow {
		var names []string
		for _, t := range fe.tabs {
			if t.modified() {
				names = append(names, t.title())
			}
		}
		if len(names) == 0 {
			fe.askWindow, fe.refocus = false, true
			a.fe.discard = true
			a.closeWindow(WindowFileEditor)
			return
		}
		open := true
		switch ui.AlertDialog(c, &open, locales.EditorUnsavedFiles(len(names)), strings.Join(names, "\n")+"\n\n"+locales.EditorUnsavedBody(),
			locales.DontSave(), locales.Cancel(), locales.SaveAll()) {
		case 0:
			fe.discard = true
			a.closeWindow(WindowFileEditor)
		case 2:
			a.saveAllAndClose()
		}
		if !open {
			fe.askWindow, fe.refocus = false, true
		}
	}
	i := slices.IndexFunc(fe.tabs, func(t *editTab) bool { return t.conflict })
	if i < 0 {
		return
	}
	t := fe.tabs[i]
	open := true
	title := locales.EditorConflict(t.name())
	if t.conflictGone {
		title = locales.EditorConflictGone(t.name())
	}
	switch ui.AlertDialog(c, &open, title, locales.EditorConflictBody(), locales.Cancel(), locales.EditorReload(), locales.Overwrite()) {
	case 1:
		a.reloadTab(t)
	case 2:
		t.conflict, fe.refocus = false, true
		a.saveTab(t, true)
	}
	if !open {
		t.conflict, fe.refocus = false, true
		if !t.saving { // canceled: the tab stays
			t.closeAfterSave = false
		}
	}
}
