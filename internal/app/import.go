package app

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/icons"
	"finallyshell/internal/importer"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// Importing connections: the manager's toolbar opens a window, modal to
// the manager as the editor is, with a choice of source at the top ("Import
// from": FinalShell and the others of importer.Readers, with their texts
// in import_sources.go), the one chosen last in this run. It finds the
// source's file or folder (its Find), or asks for it, reads it off the UI
// thread and lists its connections with check boxes to choose those to
// import. Hosts the store has already, the same address, port and user,
// are listed but not imported. Importing adds the hosts chosen, in folders
// named as the source's, shows them in the manager and says there how
// many were imported, skipped and failed. Passwords of other clients are
// not imported (see the importer package): the first connection asks for
// them, as for any host. Those of a list that has them go in the system's
// credential store, as a remembered password does; the table then has a
// Password column saying which rows have one, without showing it. A
// source the user writes (a server list) has "Save template…", which
// writes a file to fill in (importText.template).

// WindowImport is the import window.
const WindowImport WindowKind = "import"

var importSpec = WindowSpec{
	Kind:  WindowImport,
	Title: locales.ImportTitle,
	Width: 860, Height: 560,
	MinWidth: 640, MinHeight: 400,
	Parent: WindowManager,
	Modal:  true,
}

// importState is the import window's, while it is open.
type importState struct {
	source   importSource // the source chosen
	path     string       // the file or folder read
	busy     bool         // looking for the source's file or folder, or reading it
	notFound bool         // none was found, nor chosen
	err      error        // reading it failed
	src      *importer.Source
	rows     []importRow
	list     ui.ListState
	row      int
	reads    int // the readings started, so that an older one's result is dropped

	// The source's template (importText.template): saving it, and where
	// it was saved, or why not.
	saving  bool
	saved   string
	saveErr error
}

// importRow is a connection the window lists.
type importRow struct {
	e       *importer.Entry
	checked bool
}

// importStatus is what keeps a row from being imported: the host the
// store has already, or the row above that is the same, as present says.
type importStatus struct {
	present *config.Host // in the store
	above   *importer.Entry
}

func (s importStatus) skipped() bool { return s.present != nil || s.above != nil }

// statuses are the rows' statuses, as the store is now.
func (st *importState) statuses(hosts *config.Store) []importStatus {
	out := make([]importStatus, len(st.rows))
	for i, r := range st.rows {
		out[i].present = importer.Present(hosts, r.e.Host)
		if out[i].present != nil {
			continue
		}
		for _, above := range st.rows[:i] {
			if importer.SameTarget(above.e.Host, r.e.Host) {
				out[i].above = above.e
				break
			}
		}
	}
	return out
}

// openImport opens the import window, or brings it to the front, and
// starts looking for the source chosen last.
func (a *App) openImport() {
	if a.windowOpen(WindowImport) {
		a.plat.FocusWindow(WindowImport)
		return
	}
	st := &importState{row: -1, source: a.importSource(a.importKind)}
	a.imp = st
	a.openWindow(importSpec, a.importView)
	a.readImport(st, false)
}

// importSource is the source of kind k, or the first when none is.
func (a *App) importSource(k importer.Kind) importSource {
	for _, s := range a.importSources {
		if s.Kind == k {
			return s
		}
	}
	return a.importSources[0]
}

// chooseImportSource makes s the source of st, and of the window the next
// time it opens, and looks for it, as the window does as it opens.
func (a *App) chooseImportSource(st *importState, s importSource) {
	if s.Kind == st.source.Kind {
		return
	}
	a.importKind = s.Kind
	*st = importState{source: s, row: -1, reads: st.reads}
	a.readImport(st, false)
}

// readImport reads the source's file or folder for st, off the UI thread:
// the one its Find finds, unless choose, and the one the user chooses
// when there is none. Canceling the choice keeps what st shows.
func (a *App) readImport(st *importState, choose bool) {
	st.reads++
	read := st.reads
	st.busy = true
	src := st.source
	find, title := src.Find, src.choose()
	fileTypes := ""
	if src.fileTypes != nil {
		fileTypes = src.fileTypes()
	}
	go func() {
		path := ""
		if !choose && find != nil {
			path = find()
		}
		var err error
		switch {
		case path != "":
		case src.Folder:
			path, err = a.plat.OpenFolder(WindowImport, title)
		default:
			path, err = a.plat.OpenFileOfType(WindowImport, title, fileTypes, src.Extensions)
		}
		var got *importer.Source
		if path != "" && err == nil {
			got, err = src.Read(path)
		}
		a.post(func() {
			if a.imp != st || st.reads != read {
				return
			}
			st.busy = false
			if path == "" && err == nil { // canceled
				st.notFound = st.notFound || st.src == nil && st.err == nil
				return
			}
			st.path, st.err, st.notFound = path, err, false
			st.setSource(got, a.hosts)
		})
	}()
}

// setSource lists what src has, nil for nothing, each row chosen unless it
// is skipped.
func (st *importState) setSource(src *importer.Source, hosts *config.Store) {
	st.src, st.rows, st.row = src, nil, -1
	if src == nil {
		return
	}
	for _, e := range src.Entries {
		st.rows = append(st.rows, importRow{e: e})
	}
	for i, s := range st.statuses(hosts) {
		st.rows[i].checked = !s.skipped()
	}
}

// problemCounts count the files of the problems that failed to import,
// and of those skipped on purpose.
func (st *importState) problemCounts() (failed, skipped int) {
	if st.src == nil {
		return 0, 0
	}
	for _, p := range st.src.Problems {
		if p.Reason.Failed() {
			failed++
		} else {
			skipped++
		}
	}
	return failed, skipped
}

// importColumns are the table's columns: with one of passwords for a
// source that has them, which shows which rows do.
func importColumns(passwords bool) []ui.TableColumn {
	cols := []ui.TableColumn{
		{ID: "check", Width: 34, Fixed: true},
		{ID: "name", Title: locales.HostColName(), Width: 190, MinWidth: 100},
		{ID: "host", Title: locales.HostColHost(), Width: 180, MinWidth: 80},
		{ID: "user", Title: locales.HostColUser(), Width: 100, MinWidth: 60},
		{ID: "folder", Title: locales.ImportColFolder(), Width: 150, MinWidth: 60},
		{ID: "status", Title: locales.ImportColStatus(), MinWidth: 80},
	}
	if passwords {
		cols = slices.Insert(cols, 4, ui.TableColumn{ID: "password", Title: locales.ImportColPassword(), Width: 80, MinWidth: 50})
	}
	return cols
}

// importView is the import window.
func (a *App) importView(c *ui.Context) {
	st := a.imp
	if st == nil {
		return
	}
	statuses := st.statuses(a.hosts)
	chosen := 0
	for i, r := range st.rows {
		if r.checked && !statuses[i].skipped() {
			chosen++
		}
	}
	ui.Column(c).Fill().Gap(10).Padding(12, 14).Children(func() {
		a.importSourceRow(c, st)
		switch {
		case st.src != nil:
			if st.source.note != nil {
				widgets.Muted(c, st.source.note()).FontSize(12)
			}
			a.importTable(c, st, statuses)
			a.importProblems(c, st)
		default:
			ui.Column(c).Grow(1).MinHeight(0).Justify(ui.Center).AlignItems(ui.Center).Gap(10).Children(func() {
				switch {
				case st.busy:
					ui.Spinner(c)
					widgets.Muted(c, locales.ImportLooking())
				case st.err != nil:
					ui.Text(c, a.importError(st)).TextColor(theme.Danger).TextAlign(ui.Center).MaxWidth(520)
					if st.path == "" { // the chooser failed: the top row has no button
						if ui.PrimaryButton(c, st.chooseLabel()).Clicked() {
							a.readImport(st, true)
						}
					}
				case st.notFound:
					ui.Text(c, st.source.notFound()).TextAlign(ui.Center).MaxWidth(520)
					if ui.PrimaryButton(c, st.chooseLabel()).Clicked() {
						a.readImport(st, true)
					}
				}
			})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if st.src != nil {
				if ui.Button(c, locales.SelectAll()).Clicked() {
					st.checkAll(statuses, true)
				}
				if ui.Button(c, locales.SelectNone()).Clicked() {
					st.checkAll(statuses, false)
				}
				widgets.Muted(c, locales.ImportChosen(chosen, len(st.rows))).SingleLine().MinWidth(0)
			}
			if st.source.template != nil {
				a.importTemplateButton(c, st)
			}
			ui.Spacer(c)
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.closeWindow(WindowImport)
			}
			if ui.PrimaryButton(c, locales.ImportCount(chosen)).Disabled(chosen == 0 || st.busy).Clicked() {
				a.importChosen(st, statuses)
			}
		})
	})
}

// importSourceRow is the choice of source, the file or folder read, once
// one was found or chosen, and the button choosing another.
func (a *App) importSourceRow(c *ui.Context, st *importState) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.ImportFrom()).SingleLine().Shrink(0)
		names := make([]string, len(a.importSources))
		for i, s := range a.importSources {
			names[i] = s.name()
		}
		chosen := st.source.name()
		if ui.Select(c, &chosen, names).Label(locales.ImportFrom()).Width(200).Changed() {
			if i := slices.Index(names, chosen); i >= 0 {
				a.chooseImportSource(st, a.importSources[i])
			}
		}
		path := st.path
		if st.src != nil {
			path = st.src.Path
		}
		if path == "" { // the button is in the middle of the window
			return
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Grow(1).MinWidth(0).Padding(0, 0, 0, 4).Children(func() {
			if st.source.Folder {
				ui.Icon(c, icons.Get("folder")).FontSize(15).TextColor(folderColor)
			} else {
				ui.Icon(c, icons.Get("file-text")).FontSize(15).TextColor(theme.TextMuted)
			}
			ui.Text(c, tildePath(path, a.home(), runtime.GOOS)).SingleLine().MinWidth(0).Grow(1).Tooltip(path)
		})
		if st.busy && st.src != nil {
			ui.Spinner(c)
		}
		if ui.Button(c, st.chooseLabel()).Disabled(st.busy).Clicked() {
			a.readImport(st, true)
		}
	})
}

// tildePath is path with the home folder home written "~", as shells
// write it, on goos where they do: path itself on Windows, or out of
// home.
func tildePath(path, home, goos string) string {
	if goos == "windows" || home == "" || home == "/" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(home, "/")+"/"); ok {
		return "~/" + rest
	}
	return path
}

// chooseLabel is the label of the button choosing the source's file or
// folder.
func (st *importState) chooseLabel() string {
	if st.source.Folder {
		return locales.ChooseFolder()
	}
	return locales.ChooseFile()
}

// importError says why reading the file or folder failed.
func (a *App) importError(st *importState) string {
	if st.source.errorText != nil {
		if s := st.source.errorText(st.err); s != "" {
			return s
		}
	}
	if errors.Is(st.err, importer.ErrNoConnections) {
		return locales.ImportNoConnections(st.path)
	}
	return locales.ImportReadFailed(st.err.Error())
}

// checkAll chooses every row that is not skipped, or none.
func (st *importState) checkAll(statuses []importStatus, on bool) {
	for i := range st.rows {
		st.rows[i].checked = on && !statuses[i].skipped()
	}
}

func (a *App) importTable(c *ui.Context, st *importState, statuses []importStatus) {
	st.list.Selected = &st.row
	st.list.Label = func(row int) string { return st.rows[row].e.Host.Title() }
	cols := importColumns(slices.ContainsFunc(st.rows, func(r importRow) bool { return r.e.Password != "" }))
	table := ui.Table(c, &st.list, cols, len(st.rows), func(row, col int) {
		r, s := &st.rows[row], statuses[row]
		h := r.e.Host
		color := theme.Text
		if s.skipped() {
			color = theme.TextMuted
		}
		if row == st.row {
			color = theme.AccentText
		}
		text := func(v string) ui.Element { return ui.Text(c, v).SingleLine().TextColor(color).MinWidth(0) }
		switch cols[col].ID {
		case "check":
			checked := r.checked && !s.skipped()
			if ui.Checkbox(c, &checked, "").Label(h.Title()).Disabled(s.skipped()).Changed() {
				r.checked = checked
			}
		case "name":
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				ui.Icon(c, icons.Get("server")).FontSize(14).TextColor(color.Alpha(0.75))
				text(h.Title())
			})
		case "host":
			addr := h.Addr
			if h.PortOr22() != 22 {
				addr = net.JoinHostPort(addr, strconv.Itoa(h.PortOr22())) // [2001:db8::1]:2200
			}
			text(addr)
		case "user":
			text(h.User)
		case "password": // never the password itself
			if r.e.Password != "" {
				text("••••")
			}
		case "folder":
			text(r.e.FolderLabel())
		case "status":
			var notes []string
			switch {
			case s.present != nil:
				notes = append(notes, locales.ImportPresent(s.present.Title()))
			case s.above != nil:
				notes = append(notes, locales.ImportSameAs(s.above.Host.Title()))
			}
			if r.e.NeedsKey {
				notes = append(notes, locales.ImportNeedsKey())
			}
			if j := r.e.Jump; j != nil {
				if st.jumpImported(j, statuses, a.hosts) {
					notes = append(notes, locales.ImportJumpHost(j.Host.Title()))
				} else {
					notes = append(notes, importNoteText(importer.JumpHostNotImported))
				}
			}
			if r.e.Note != "" {
				notes = append(notes, importNoteText(r.e.Note))
			}
			text(strings.Join(notes, locales.ListSeparator())).Tooltip(strings.Join(notes, "\n"))
		}
	}).Grow(1).MinHeight(0).Border(1, theme.Border).Radius(4)
	table.Children(func() {
		if len(st.rows) == 0 {
			widgets.Muted(c, locales.ImportNothing()).Padding(12)
		}
	})
	// Enter, a double click or Space checks the row chosen, or unchecks it.
	if st.row >= 0 && st.row < len(st.rows) && !statuses[st.row].skipped() &&
		(table.Submitted() || st.list.Shortcut(c, 0, ui.KeySpace)) {
		st.rows[st.row].checked = !st.rows[st.row].checked
	}
}

// jumpImported tells whether j, the Jump of an entry, will be a host of
// hosts once the rows chosen are imported, as importer.Import has it: one
// present already, or a row chosen that reaches the same target.
func (st *importState) jumpImported(j *importer.Entry, statuses []importStatus, hosts *config.Store) bool {
	if importer.Present(hosts, j.Host) != nil {
		return true
	}
	for i, r := range st.rows {
		if r.checked && !statuses[i].skipped() && importer.SameTarget(r.e.Host, j.Host) {
			return true
		}
	}
	return false
}

// importProblems lists the files, or lines, not imported from, and why.
func (a *App) importProblems(c *ui.Context, st *importState) {
	failed, skipped := st.problemCounts()
	if failed+skipped == 0 {
		return
	}
	ui.Column(c).Gap(4).Shrink(0).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Get("triangle-alert")).FontSize(13).TextColor(theme.TextMuted)
			widgets.Muted(c, locales.ImportProblems(failed, skipped)).SingleLine().MinWidth(0)
		})
		ui.Scroll(c).MaxHeight(78).Children(func() {
			ui.Column(c).Gap(2).Padding(0, 0, 0, 19).Children(func() {
				for _, p := range st.src.Problems {
					widgets.Muted(c, locales.ProblemLine(p.File, importProblemText(p, st.source.name()))).FontSize(12).SingleLine().MinWidth(0).Tooltip(p.Detail)
				}
			})
		})
	})
}

// importTemplateButton is "Save template…", for a source with a template,
// and where it was saved, or why not.
func (a *App) importTemplateButton(c *ui.Context, st *importState) {
	if ui.Button(c, locales.SaveTemplate()).Disabled(st.saving).Clicked() {
		a.saveImportTemplate(st)
	}
	switch {
	case st.saveErr != nil:
		ui.Text(c, locales.TemplateSaveFailed(st.saveErr.Error())).TextColor(theme.Danger).FontSize(12).SingleLine().MinWidth(0).Tooltip(st.saveErr.Error())
	case st.saved != "":
		widgets.Muted(c, locales.TemplateSaved(tildePath(st.saved, a.home(), runtime.GOOS))).FontSize(12).SingleLine().MinWidth(0).Tooltip(st.saved)
	}
}

// saveImportTemplate asks where to save the template of st's source, in
// the downloads folder to begin with, and writes it there, off the UI
// thread. The window says where it went.
func (a *App) saveImportTemplate(st *importState) {
	tmpl, kind := st.source.template(), st.source.Kind
	st.saving, st.saved, st.saveErr = true, "", nil
	title, start := locales.SaveTemplateTitle(), filepath.Join(a.plat.DownloadsDir(), tmpl.name)
	go func() {
		path, err := a.plat.SaveFile(WindowImport, title, start, tmpl.typeName, []string{strings.TrimPrefix(filepath.Ext(tmpl.name), ".")})
		if path != "" && err == nil {
			err = os.WriteFile(path, tmpl.data, 0o644)
		}
		a.post(func() {
			if a.imp != st || st.source.Kind != kind {
				return
			}
			st.saving = false
			if err != nil {
				st.saveErr = err
			} else {
				st.saved = path
			}
		})
	}()
}

// importChosen imports the rows chosen, closes the window and shows the
// hosts imported in the manager, saying there how many were imported,
// skipped and failed.
func (a *App) importChosen(st *importState, statuses []importStatus) {
	var entries []*importer.Entry
	for i, r := range st.rows {
		if r.checked && !statuses[i].skipped() {
			entries = append(entries, r.e)
		}
	}
	added := importer.Import(a.hosts, entries, time.Now())
	failed, skipped := st.problemCounts()
	imported, skipped := len(added), skipped+len(st.rows)-len(added)
	keys := 0
	m := &a.manager
	for _, h := range added {
		if h.Auth == config.AuthKey && h.KeyPath == "" {
			keys++
		}
		// A list's password, as the editor keeps a remembered one.
		if e := entryOf(entries, h); e != nil && e.Password != "" && h.Auth == config.AuthPassword {
			a.keepLogin(h, e.Password, true, false)
		}
		for _, f := range a.hosts.FolderPath(h.Folder) {
			m.tree.Open.Add(managerItem{folder: f.ID})
		}
	}
	if m.query != "" {
		m.query = ""
		a.searchChanged(m)
	}
	if len(added) > 0 {
		m.reveal(managerItem{host: added[0].ID})
	}
	m.notice = func() string {
		s := locales.ImportResult(imported, skipped, failed)
		if keys > 0 {
			s += " " + locales.ImportKeysNote(keys)
		}
		return s
	}
	a.imp = nil
	a.closeWindow(WindowImport)
	a.saveFromManager()
}

// entryOf is the entry of entries that Import added as h: the first to
// reach the same target, as Import skips the others.
func entryOf(entries []*importer.Entry, h *config.Host) *importer.Entry {
	for _, e := range entries {
		if importer.SameTarget(e.Host, h) {
			return e
		}
	}
	return nil
}
