package app

import (
	"errors"
	"strconv"
	"strings"

	"finalshelly/internal/config"
	"finalshelly/internal/icons"
	"finalshelly/internal/importer"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
)

// Importing from FinalShell: the manager's toolbar opens a window, modal
// to the manager as the editor is, that finds FinalShell's data folder
// (importer.Find), or asks for it, reads it off the UI thread and lists
// its connections with check boxes to choose those to import. Hosts the
// store has already, the same address, port and user, are listed but not
// imported. Importing adds the hosts chosen, in folders named as
// FinalShell's, shows them in the manager and says there how many were
// imported, skipped and failed. Passwords are not imported (see the
// importer package): the first connection asks for them, as for any host.

// WindowImport is the window importing from FinalShell.
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
	dir      string // the folder read
	busy     bool   // looking for FinalShell's folder, or reading it
	notFound bool   // none was found, nor chosen
	err      error  // reading it failed
	src      *importer.Source
	rows     []importRow
	list     ui.ListState
	row      int
	reads    int // the readings started, so that an older one's result is dropped
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

// importFinalShell opens the import window, or brings it to the front,
// and starts looking for FinalShell's folder.
func (a *App) importFinalShell() {
	if a.windowOpen(WindowImport) {
		a.plat.FocusWindow(WindowImport)
		return
	}
	st := &importState{row: -1}
	a.imp = st
	a.openWindow(importSpec, a.importView)
	a.readFinalShell(st, false)
}

// readFinalShell reads FinalShell's folder for st, off the UI thread: the
// one importer.Find finds, unless choose, and the one the user chooses
// when there is none. Canceling the choice keeps what st shows.
func (a *App) readFinalShell(st *importState, choose bool) {
	st.reads++
	read := st.reads
	st.busy = true
	find, title := a.findFinalShell, locales.ChooseFinalShellFolder()
	go func() {
		dir := ""
		if !choose {
			dir = find()
		}
		var err error
		if dir == "" {
			dir, err = a.plat.OpenFolder(WindowImport, title)
		}
		var src *importer.Source
		if dir != "" && err == nil {
			src, err = importer.Read(dir)
		}
		a.post(func() {
			if a.imp != st || st.reads != read {
				return
			}
			st.busy = false
			if dir == "" && err == nil { // canceled
				st.notFound = st.notFound || st.src == nil && st.err == nil
				return
			}
			st.dir, st.err, st.notFound = dir, err, false
			st.setSource(src, a.hosts)
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

func importColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "check", Width: 34, Fixed: true},
		{ID: "name", Title: locales.HostColName(), Width: 190, MinWidth: 100},
		{ID: "host", Title: locales.HostColHost(), Width: 180, MinWidth: 80},
		{ID: "user", Title: locales.HostColUser(), Width: 100, MinWidth: 60},
		{ID: "folder", Title: locales.ImportColFolder(), Width: 150, MinWidth: 60},
		{ID: "status", Title: locales.ImportColStatus(), MinWidth: 80},
	}
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
		a.importFolderRow(c, st)
		switch {
		case st.src != nil:
			widgets.Muted(c, locales.ImportPasswordsNote()).FontSize(12)
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
					if st.dir == "" { // the folder row has none
						if ui.PrimaryButton(c, locales.ChooseFolder()).Clicked() {
							a.readFinalShell(st, true)
						}
					}
				case st.notFound:
					ui.Text(c, locales.FinalShellNotFound()).TextAlign(ui.Center).MaxWidth(520)
					if ui.PrimaryButton(c, locales.ChooseFolder()).Clicked() {
						a.readFinalShell(st, true)
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

// importFolderRow is the folder read, and the button choosing another:
// nothing before a folder was found or chosen.
func (a *App) importFolderRow(c *ui.Context, st *importState) {
	dir := st.dir
	if st.src != nil {
		dir = st.src.Conn
	}
	if dir == "" {
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Get("folder")).FontSize(15).TextColor(ui.Hex("#eab308"))
		ui.Text(c, locales.FinalShellFolderAt(dir)).SingleLine().MinWidth(0).Grow(1).Tooltip(dir)
		if st.busy && st.src != nil {
			ui.Spinner(c)
		}
		if ui.Button(c, locales.ChooseFolder()).Disabled(st.busy).Clicked() {
			a.readFinalShell(st, true)
		}
	})
}

// importError says why reading the folder failed.
func (a *App) importError(st *importState) string {
	if errors.Is(st.err, importer.ErrNoConnections) {
		return locales.FinalShellNoConnections(st.dir)
	}
	return locales.FinalShellReadFailed(st.err.Error())
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
	table := ui.Table(c, &st.list, importColumns(), len(st.rows), func(row, col int) {
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
		switch col {
		case 0:
			checked := r.checked && !s.skipped()
			if ui.Checkbox(c, &checked, "").Label(h.Title()).Disabled(s.skipped()).Changed() {
				r.checked = checked
			}
		case 1:
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() {
				ui.Icon(c, icons.Get("server")).FontSize(14).TextColor(color.Alpha(0.75))
				text(h.Title())
			})
		case 2:
			addr := h.Addr
			if h.PortOr22() != 22 {
				addr += ":" + strconv.Itoa(h.PortOr22())
			}
			text(addr)
		case 3:
			text(h.User)
		case 4:
			text(r.e.FolderLabel())
		case 5:
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
			if r.e.Proxy {
				notes = append(notes, locales.ImportNoProxy())
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

// importProblems lists the files not imported from, and why.
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
					widgets.Muted(c, locales.ProblemLine(p.File, importProblemText(p))).FontSize(12).SingleLine().MinWidth(0).Tooltip(p.Detail)
				}
			})
		})
	})
}

// importProblemText says why a file was not imported from.
func importProblemText(p importer.Problem) string {
	switch p.Reason {
	case importer.Broken:
		return locales.ProblemBroken()
	case importer.Unreadable:
		return locales.ProblemUnreadable()
	case importer.TooLarge:
		return locales.ProblemTooLarge()
	case importer.NoHost:
		return locales.ProblemNoHost()
	case importer.NotSSH:
		return locales.ProblemNotSSH()
	case importer.DeletedThere:
		return locales.ProblemDeleted()
	}
	return ""
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
	added := importer.Import(a.hosts, entries)
	failed, skipped := st.problemCounts()
	imported, skipped := len(added), skipped+len(st.rows)-len(added)
	keys := 0
	m := &a.manager
	for _, h := range added {
		if h.Auth == config.AuthKey {
			keys++
		}
		for _, f := range a.hosts.FolderPath(h.Folder) {
			m.tree.Open.Add(managerItem{folder: f.ID})
		}
	}
	if m.query != "" {
		m.query = ""
		a.searchChanged()
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
