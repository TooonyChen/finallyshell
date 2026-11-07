package app

import (
	"fmt"
	"path"
	"strings"

	"finalshelly/internal/remotefs"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// fileState is the file panel of a session.
type fileState struct {
	cwd      string
	pathEdit string
	entries  []remotefs.Entry
	loading  bool
	err      error
	table    ui.ListState
	chosen   ui.Selection[string] // names in cwd
	row      int

	tree     ui.OutlineState[string]
	treeRow  int
	subdirs  map[string][]string // the directories in each loaded directory
	treeBusy map[string]bool
}

type sftpError struct{ err error }

func (e *sftpError) Error() string { return "sftp: " + e.err.Error() }

type dirError struct {
	dir string
	err error
}

func (e *dirError) Error() string { return e.dir + ": " + e.err.Error() }

func fileErrorText(err error) string {
	switch e := err.(type) {
	case *sftpError:
		return locales.SftpUnavailable(e.err.Error())
	case *dirError:
		return locales.CannotOpenFolder(e.dir, e.err.Error())
	}
	return err.Error()
}

func (f *fileState) init() {
	f.subdirs = map[string][]string{}
	f.treeBusy = map[string]bool{}
	f.row = -1
	f.treeRow = -1
	f.tree.Open.Add("/")
}

// chosenNames are the names of the rows chosen in the table.
func (f *fileState) chosenNames() []string {
	var names []string
	for _, e := range f.entries {
		if f.chosen.Has(e.Name) {
			names = append(names, e.Name)
		}
	}
	return names
}

// openDir lists dir in the table and makes it the current directory.
func (a *App) openDir(s *Session, dir string) {
	if s.sftp == nil {
		return
	}
	dir = path.Clean(dir)
	f := &s.files
	f.loading, f.err = true, nil
	f.pathEdit = dir
	sc := s.sftp
	go func() {
		entries, err := remotefs.ReadDir(sc, dir)
		a.post(func() {
			f.loading = false
			if err != nil {
				f.err = &dirError{dir, err}
				f.pathEdit = f.cwd
				return
			}
			if dir != f.cwd {
				f.chosen.Clear()
				f.row = -1
			}
			f.cwd, f.pathEdit, f.entries = dir, dir, entries
			f.subdirs[dir] = dirNames(dir, entries)
			// Open the tree down to the directory.
			for p := dir; ; p = path.Dir(p) {
				f.tree.Open.Add(p)
				if p == "/" {
					break
				}
			}
			for p := path.Dir(dir); p != "/" && f.subdirs[p] == nil; p = path.Dir(p) {
				a.loadSubdirs(s, p)
			}
			if dir != "/" && f.subdirs["/"] == nil {
				a.loadSubdirs(s, "/")
			}
		})
	}()
}

// refreshIfShowing lists dir again when the session shows it.
func (a *App) refreshIfShowing(s *Session, dir string) {
	if s.files.cwd == dir {
		a.openDir(s, dir)
	}
	delete(s.files.subdirs, dir)
}

// loadSubdirs reads the directories in dir, for the tree.
func (a *App) loadSubdirs(s *Session, dir string) {
	f := &s.files
	if s.sftp == nil || f.treeBusy[dir] {
		return
	}
	f.treeBusy[dir] = true
	sc := s.sftp
	go func() {
		entries, err := remotefs.ReadDir(sc, dir)
		a.post(func() {
			delete(f.treeBusy, dir)
			if err != nil {
				f.subdirs[dir] = []string{}
				return
			}
			f.subdirs[dir] = dirNames(dir, entries)
		})
	}()
}

func dirNames(dir string, entries []remotefs.Entry) []string {
	names := []string{}
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, path.Join(dir, e.Name))
		}
	}
	return names
}

func entryKind(e remotefs.Entry) string {
	switch {
	case e.IsDir():
		return locales.KindFolder()
	case e.Link:
		return locales.KindLink()
	}
	if ext := path.Ext(e.Name); len(ext) > 1 && len(ext) <= 8 && strings.Trim(ext[1:], "0123456789") != "" {
		return locales.KindExtFile(strings.ToUpper(ext[1:]))
	}
	return locales.KindFile()
}

func fileColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "name", Title: locales.ColName(), MinWidth: 160},
		{ID: "size", Title: locales.ColSize(), Width: 90, Align: ui.End},
		{ID: "type", Title: locales.ColType(), Width: 90},
		{ID: "modified", Title: locales.ColModified(), Width: 140},
		{ID: "perms", Title: locales.ColPermissions(), Width: 100},
		{ID: "owner", Title: locales.ColOwner(), Width: 96},
	}
}

// filePanel browses the active session's server over SFTP.
func (a *App) filePanel(c *ui.Context) {
	s := a.current()
	if s == nil || s.isNewTab() {
		return
	}
	f := &s.files
	ui.Column(c.Key(s.ID)).Grow(1).Children(func() {
		ui.Row(c).Padding(4, 8).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#e5e7eb")).Children(func() {
			if ui.TextInput(c, &f.pathEdit).Grow(1).Submitted() {
				a.openDir(s, f.pathEdit)
			}
			if widgets.IconButton(c, "arrow-up", locales.ParentFolder()).Clicked() && f.cwd != "" {
				a.openDir(s, path.Dir(f.cwd))
			}
			if widgets.IconButton(c, "refresh-cw", locales.Refresh()).Clicked() && f.cwd != "" {
				a.openDir(s, f.cwd)
			}
			if widgets.IconButton(c, "house", locales.HomeFolder()).Clicked() && s.sftp != nil {
				if home, err := s.sftp.Getwd(); err == nil {
					a.openDir(s, home)
				}
			}
			ui.Box(c).Size(1, 16).Background(ui.Hex("#e5e7eb")).Margin(0, 4)
			if widgets.IconButton(c, "upload", locales.UploadFiles()).Clicked() {
				a.pickAndUpload(s, false)
			}
			if widgets.IconButton(c, "folder-up", locales.UploadFolder()).Clicked() {
				a.pickAndUpload(s, true)
			}
			names := f.chosenNames()
			dl := widgets.IconButton(c, "download", locales.Download())
			if len(names) == 0 {
				dl.Opacity(0.4)
			}
			if dl.Clicked() && len(names) > 0 {
				a.download(s, names, "")
			}
			if f.loading {
				ui.Spinner(c)
			}
			ui.Box(c).Size(1, 16).Background(ui.Hex("#e5e7eb")).Margin(0, 4)
			a.downloadsButton(c)
		})
		if f.err != nil {
			ui.Text(c, fileErrorText(f.err)).TextColor(theme.Danger).Padding(4, 10)
		}
		split := ui.Split(c, &a.treeWidth, func() { a.dirTree(c, s) }, func() { a.fileTable(c, s) }).SplitReset(defaultTreeWidth).Grow(1)
		a.splitMoved(c, split, &a.treeWidth, treeLimits)
	})
}

// dropZone makes e take files dropped from the system's file manager, to
// upload them into dir, and outlines it while they are over it.
func (a *App) dropZone(e ui.Element, s *Session, dir string) {
	if files := e.DroppedFiles(); len(files) > 0 && dir != "" {
		a.upload(s, files, dir)
	}
	if e.FileDragOver() && dir != "" {
		e.Border(2, theme.Accent).Background(theme.Accent.Alpha(0.06))
	}
}

func (a *App) dirTree(c *ui.Context, s *Session) {
	f := &s.files
	f.tree.List.Selected = &f.treeRow
	tree := ui.Outline(c, &f.tree, []string{"/"}, func(dir string) []string {
		subs, loaded := f.subdirs[dir]
		if !loaded {
			if f.tree.Open.Has(dir) {
				a.loadSubdirs(s, dir)
			}
			return []string{}
		}
		if len(subs) == 0 {
			return nil
		}
		return subs
	}, func(dir string) {
		row := ui.Row(c).Gap(5).Grow(1).Radius(4)
		a.dropZone(row, s, dir)
		row.Children(func() {
			name := path.Base(dir)
			// "/" is a folder like any other, not a workspace's root,
			// which themes give an icon of its own.
			a.fileIcon(c, name, true, f.tree.Open.Has(dir), false)
			t := ui.Text(c, name).SingleLine()
			if dir == f.cwd {
				t.Bold()
			}
		})
	}).Fill()
	if tree.Changed() {
		if dir := f.tree.Item(f.treeRow); dir != "" && dir != f.cwd {
			a.openDir(s, dir)
		}
	}
}

func (a *App) fileTable(c *ui.Context, s *Session) {
	f := &s.files
	entries := f.entries
	f.table.Selected = &f.row
	f.table.Selection = &f.chosen
	f.table.Key = func(i int) any { return entries[i].Name }
	f.table.Label = func(i int) string { return entries[i].Name }
	zone := ui.Column(c).Fill()
	a.dropZone(zone, s, f.cwd)
	over := zone.FileDragOver() && f.cwd != ""
	zone.Children(func() {
		t := ui.Table(c, &f.table, fileColumns(), len(entries), func(row, col int) {
			e := entries[row]
			switch col {
			case 0:
				ui.Row(c).Gap(6).Children(func() {
					a.fileIcon(c, e.Name, e.IsDir(), false, false)
					ui.Text(c, e.Name).SingleLine()
				})
			case 1:
				if !e.IsDir() {
					ui.Text(c, formatSize(e.Size))
				}
			case 2:
				ui.Text(c, entryKind(e)).SingleLine()
			case 3:
				ui.Text(c, e.ModTime.Format("2006/01/02 15:04")).SingleLine()
			case 4:
				ui.Text(c, e.Mode.Perm().String()).Font("monospace")
			case 5:
				ui.Text(c, fmt.Sprintf("%d/%d", e.UID, e.GID))
			}
		}).Grow(1)
		if t.Submitted() && f.row >= 0 && f.row < len(entries) && entries[f.row].IsDir() {
			a.openDir(s, path.Join(f.cwd, entries[f.row].Name))
		}
		t.ContextMenu(func(m *ui.Menu) {
			names := f.chosenNames()
			if m.Item(locales.Download()).Disabled(len(names) == 0).Chosen() {
				a.download(s, names, "")
			}
			if m.Item(locales.DownloadTo()).Disabled(len(names) == 0).Chosen() {
				a.pickAndDownload(s, names)
			}
			m.Separator()
			if m.Item(locales.UploadFiles()).Chosen() {
				a.pickAndUpload(s, false)
			}
			if m.Item(locales.UploadFolder()).Chosen() {
				a.pickAndUpload(s, true)
			}
			m.Separator()
			if m.Item(locales.Refresh()).Chosen() {
				a.openDir(s, f.cwd)
			}
			if len(names) == 1 && m.Item(locales.CopyPath()).Chosen() {
				c.WriteClipboard(path.Join(f.cwd, names[0]))
			}
		})
		if over {
			ui.Text(c, locales.DropToUpload(f.cwd)).Padding(6, 12).Radius(6).
				Background(theme.Accent).TextColor(theme.AccentText).
				Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(12)
		}
	})
}
