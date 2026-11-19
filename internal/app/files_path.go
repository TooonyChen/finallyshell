package app

import (
	"path"
	"strings"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

func (a *App) filePathBar(c *ui.Context, s *Session) {
	f := &s.files
	if !a.settings.FilePathBreadcrumbs {
		if ui.TextInput(c.Key("file-path-input"), &f.pathEdit).Grow(1).MinWidth(0).Label(locales.EditRemotePath()).Submitted() {
			a.openDir(s, f.pathEdit)
		}
		return
	}

	editing := f.pathEditing
	border := theme.Border
	if editing {
		border = theme.Accent
	}
	bar := ui.Row(c.Key("file-path")).Grow(1).MinWidth(0).Height(30).AlignItems(ui.Center).
		PaddingX(3).Radius(5).Background(theme.Background).Border(1, border)
	bar.Children(func() {
		var restoreFocus bool
		if editing {
			in := ui.TextInputBase(c.Key("path-input"), &f.pathEdit).Grow(1).MinWidth(0).
				PaddingX(5).TextColor(theme.Text).Label(locales.EditRemotePath()).AutoFocus()
			switch {
			case in.Submitted():
				dir := f.pathEdit
				f.pathEditing, f.pathEdit = false, f.cwd
				if dir != "" {
					a.openDir(s, dir)
				}
				restoreFocus = true
			case in.Shortcut(0, ui.KeyEscape):
				f.pathEditing, f.pathEdit = false, f.cwd
				restoreFocus = true
			case bar.PressedOutside() || in.FocusLeft() && !bar.FocusWithin():
				f.pathEditing, f.pathEdit = false, f.cwd
			}
		} else {
			a.fileBreadcrumbs(c, s)
		}
		edit := widgets.ToggleIconButton(c, "pencil", locales.EditRemotePath(), editing).
			Focusable().Disabled(s.sftp == nil && f.cwd == "")
		if edit.Clicked() {
			f.pathEditing, f.pathEdit = !editing, f.cwd
		}
		if restoreFocus {
			edit.Focus()
		}
		if f.pathEditing != editing {
			c.Invalidate()
		}
	})
}

// resetFilePathEdits keeps a draft from one style from appearing in the other.
func (a *App) resetFilePathEdits() {
	for _, s := range a.sessions {
		s.files.pathEditing, s.files.pathEdit = false, s.files.cwd
	}
}

func (a *App) fileBreadcrumbs(c *ui.Context, s *Session) {
	const (
		fontSize = 12.5
		pad      = 3
		maxWidth = 156
		chevron  = 12
		overflow = 27
	)
	row := ui.Row(c.Key("breadcrumbs")).Grow(1).MinWidth(0).Height(26).AlignItems(ui.Center).Clip()
	if s.files.cwd == "" {
		return
	}
	type crumb struct {
		name, dir string
		width     float32
	}
	crumbs := []crumb{{name: "/", dir: "/"}}
	dir := "/"
	for name := range strings.SplitSeq(strings.TrimPrefix(s.files.cwd, "/"), "/") {
		if name != "" {
			dir = path.Join(dir, name)
			crumbs = append(crumbs, crumb{name: name, dir: dir})
		}
	}
	width := float32(chevron * (len(crumbs) - 1))
	for i := range crumbs {
		w, _ := c.MeasureText(0, ui.Span{Text: crumbs[i].name, Size: fontSize})
		crumbs[i].width = min(maxWidth, w+2*pad+1)
		width += crumbs[i].width
	}
	first := 0
	available := row.LayoutBounds().W
	// Build the overflow menu again when this frame lays out a new width.
	row.DrawOver(func(_ *ui.Painter, r ui.Rect) {
		if r.W != available {
			c.Invalidate()
		}
	})
	if available > 0 && width > available {
		width += overflow + chevron
		for first < len(crumbs)-1 && width > available {
			width -= crumbs[first].width + chevron
			first++
		}
	}
	row.Children(func() {
		separator := func() {
			ui.Icon(c, icons.Get("chevron-right")).FontSize(chevron).Width(chevron).
				Shrink(0).TextColor(theme.TextMuted).AccessHidden(true)
		}
		if first > 0 {
			more := ui.ButtonBase(c.Key("more-folders")).Size(overflow, 24).Shrink(0).Center().Radius(4).
				Label(locales.MoreParentFolders()).Tooltip(locales.MoreParentFolders())
			if more.Hovered() {
				more.Background(theme.Text.Alpha(0.07))
			}
			more.Children(func() { widgets.Muted(c, "…").FontSize(fontSize) })
			more.PopoverMenuButton(func(m *ui.Menu) {
				m.MaxWidth(360)
				for _, item := range crumbs[:first] {
					if m.Item(item.dir).ID(item.dir).Tooltip(item.dir).Chosen() {
						a.openDir(s, item.dir)
					}
				}
			})
			separator()
		}
		for i, item := range crumbs[first:] {
			if i > 0 {
				separator()
			}
			label := func() ui.Element {
				return ui.Text(c, item.name).FontSize(fontSize).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			}
			if item.dir == s.files.cwd {
				ui.Row(c.Key(item.dir)).Width(item.width).MinWidth(0).Shrink(1).PaddingX(pad).
					Children(func() { label().TextColor(theme.Text) })
				continue
			}
			b := ui.ButtonBase(c.Key(item.dir)).Size(item.width, 24).Shrink(0).PaddingX(pad).Center().Radius(4).
				Label(item.dir)
			if b.Hovered() {
				b.Background(theme.Text.Alpha(0.07))
			}
			b.Children(func() { label().TextColor(theme.TextMuted) })
			if b.Clicked() {
				a.openDir(s, item.dir)
			}
		}
	})
}
