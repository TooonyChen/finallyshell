package app

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"finallyshell/internal/config"
	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The connection manager is FinalShell's 连接管理器: a window of its own
// with the saved hosts in a tree of folders, searched as the user types.
// Connecting from it opens a session in the main window, as the start
// page does; deleting moves a host to the deleted ones, which "Show
// deleted" lists to restore or to delete for good.

// WindowManager is the connection manager's window.
const WindowManager WindowKind = "manager"

// managerSpec is how the manager opens: a child of the main window that
// does not block it, and is large enough for the connection editor to be
// a sheet of it on macOS.
var managerSpec = WindowSpec{
	Kind:  WindowManager,
	Title: locales.ManagerTitle,
	Width: 900, Height: 640,
	MinWidth: 680, MinHeight: 420,
}

// managerItem is a row of the manager's tree: a folder, by its ID, or a
// host, by its.
type managerItem struct{ folder, host string }

func (it managerItem) isHost() bool { return it.host != "" }

// managerDrag is what dragging rows of the manager drags.
type managerDrag []managerItem

// managerState is what the manager keeps while the app runs.
type managerState struct {
	tree        ui.OutlineState[managerItem]
	row         int // the row chosen, -1 for none
	sort        ui.SortOrder
	query       string
	filter      string // the folder shown, "" for all
	showDeleted bool

	opened bool // once opened, with every folder open
	focus  bool // the tree takes the focus in the next frame
	// pick is the item to choose in the next frame, showing it.
	pick *managerItem
	// beforeSearch are the folders open before the search opened those
	// holding what it found, to open again as it is cleared.
	beforeSearch []managerItem
	searching    bool

	prompt   *managerPrompt
	question *managerQuestion
	saveErr  func() string
	// notice says how the last import went, until the manager closes.
	notice func() string
	over   managerItem // the row a drag was over in the last frame
}

// managerPrompt asks for a name, of a new folder or for a rename.
type managerPrompt struct {
	title    func() string
	name     string
	optional bool // an empty name will do, as a host's
	ok       func(name string)
}

// managerQuestion asks before deleting.
type managerQuestion struct {
	title, body, ok func() string
	yes             func()
}

// reveal chooses it in the manager the next time it draws, opening the
// folders it is in.
func (m *managerState) reveal(it managerItem) { m.pick = &it }

// openManager opens the connection manager, or brings it to the front,
// and the connection editor for a new host when there is no host to list.
func (a *App) openManager() { a.showManager(true) }

// showManager opens the connection manager, or brings it to the front.
// With newHost, a manager with no host to list opens the editor for a new
// host too; without, it opens alone, as the empty start page wants it, so
// that its Import is not behind the editor.
func (a *App) showManager(newHost bool) {
	m := &a.manager
	if a.windowOpen(WindowManager) {
		a.plat.FocusWindow(WindowManager)
		return
	}
	if !m.opened {
		m.opened = true
		for _, f := range a.hosts.Folders {
			m.tree.Open.Add(managerItem{folder: f.ID})
		}
	}
	m.focus, m.prompt, m.question, m.notice = true, nil, nil, nil
	if m.query != "" { // it opens without the last search
		m.query = ""
		a.searchChanged()
	}
	a.openWindow(managerSpec, a.managerView)
	if newHost && len(a.hosts.Live()) == 0 {
		a.openEditor(nil, WindowManager)
	}
}

// folderOf is the folder h shows in: "" when its folder is missing.
func (a *App) folderOf(h *config.Host) string {
	if a.hosts.Folder(h.Folder) == nil {
		return ""
	}
	return h.Folder
}

// managerTree is what the tree shows, as the search, the filter and the
// sort have it: the items at its top and those in each folder.
type managerTree struct {
	a       *App
	m       *managerState
	hosts   map[string][]*config.Host // shown, by folder
	matches map[string]bool           // the folders holding a host found
	search  bool
}

func (a *App) newManagerTree() *managerTree {
	m := &a.manager
	words := strings.Fields(strings.ToLower(m.query))
	t := &managerTree{a: a, m: m, hosts: map[string][]*config.Host{}, matches: map[string]bool{}, search: len(words) > 0}
	for _, h := range a.hosts.Hosts {
		if h.Deleted() && !m.showDeleted || t.search && !hostMatches(h, words) {
			continue
		}
		folder := a.folderOf(h)
		t.hosts[folder] = append(t.hosts[folder], h)
		if t.search {
			for _, f := range a.hosts.FolderPath(folder) {
				t.matches[f.ID] = true
			}
		}
	}
	for folder, hosts := range t.hosts {
		t.hosts[folder] = sortHosts(hosts, m.sort)
	}
	return t
}

// roots are the items at the top: of the folder the filter shows, or of
// the top level.
func (t *managerTree) roots() []managerItem { return t.contents(t.m.filter) }

// children are the items in a folder; nil for a host.
func (t *managerTree) children(it managerItem) []managerItem {
	if it.isHost() {
		return nil
	}
	return t.contents(it.folder)
}

// contents are the folders in the folder, then its hosts. A folder is
// never nil, so that it shows as one when empty.
func (t *managerTree) contents(folder string) []managerItem {
	out := []managerItem{}
	folders := t.a.hosts.Subfolders(folder)
	if t.m.sort.Column == "name" {
		folders = slices.Clone(folders)
		slices.SortStableFunc(folders, func(x, y *config.Folder) int {
			return sortDir(strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)), t.m.sort)
		})
	}
	for _, f := range folders {
		if !t.search || t.matches[f.ID] {
			out = append(out, managerItem{folder: f.ID})
		}
	}
	for _, h := range t.hosts[folder] {
		out = append(out, managerItem{host: h.ID})
	}
	return out
}

// rowOf is the row showing it, as the folders open now; -1 for none.
func (t *managerTree) rowOf(it managerItem) int {
	row := 0
	var walk func(items []managerItem) bool
	walk = func(items []managerItem) bool {
		for _, x := range items {
			if x == it {
				return true
			}
			row++
			if !x.isHost() && t.m.tree.Open.Has(x) && walk(t.children(x)) {
				return true
			}
		}
		return false
	}
	if walk(t.roots()) {
		return row
	}
	return -1
}

func sortDir(n int, s ui.SortOrder) int {
	if s.Descending {
		return -n
	}
	return n
}

// sortHosts sorts hosts by the column of s, keeping the user's order
// where they are the same, and for no column.
func sortHosts(hosts []*config.Host, s ui.SortOrder) []*config.Host {
	text := func(h *config.Host) string {
		switch s.Column {
		case "name":
			return h.Title()
		case "host":
			return h.Addr
		case "user":
			return h.User
		case "notes":
			return h.Notes
		}
		return ""
	}
	if s.Column == "" {
		return hosts
	}
	slices.SortStableFunc(hosts, func(x, y *config.Host) int {
		if s.Column == "port" {
			return sortDir(cmp.Compare(x.PortOr22(), y.PortOr22()), s)
		}
		return sortDir(strings.Compare(strings.ToLower(text(x)), strings.ToLower(text(y))), s)
	})
	return hosts
}

func managerColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "name", Title: locales.HostColName(), Width: 230, MinWidth: 140, Sortable: true},
		{ID: "host", Title: locales.HostColHost(), Width: 160, MinWidth: 80, Sortable: true},
		{ID: "port", Title: locales.HostColPort(), Width: 64, MinWidth: 48, Align: ui.End, Sortable: true},
		{ID: "user", Title: locales.HostColUser(), Width: 110, MinWidth: 60, Sortable: true},
		{ID: "notes", Title: locales.HostColNotes(), MinWidth: 80, Sortable: true},
	}
}

// folderLabel is the folder's path, as the filter and the menus show it.
func (a *App) folderLabel(id string) string {
	var names []string
	for _, f := range a.hosts.FolderPath(id) {
		names = append(names, f.Name)
	}
	return strings.Join(names, " / ")
}

// allFolders are the folders, each after the one it is in.
func (a *App) allFolders() []*config.Folder {
	var out []*config.Folder
	var walk func(parent string)
	walk = func(parent string) {
		for _, f := range a.hosts.Subfolders(parent) {
			out = append(out, f)
			walk(f.ID)
		}
	}
	walk("")
	return out
}

// managerView is the manager's window: the toolbar, the tree, and the
// setting under it.
func (a *App) managerView(c *ui.Context) {
	m := &a.manager
	if m.filter != "" && a.hosts.Folder(m.filter) == nil {
		m.filter = ""
	}
	var later []func() // what changes the tree, once it is built
	ui.Column(c).Fill().Children(func() {
		a.managerToolbar(c)
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, 10).Children(func() {
			a.managerTable(c, &later)
		})
		ui.Row(c).Gap(12).Padding(8, 12).AlignItems(ui.Center).Children(func() {
			closeAfter := !a.settings.KeepManagerOpen
			if ui.Checkbox(c, &closeAfter, locales.CloseAfterConnect()).Changed() {
				a.changeSettings(func() { a.settings.KeepManagerOpen = !closeAfter })
			}
			ui.Spacer(c)
			if m.saveErr != nil {
				ui.Text(c, m.saveErr()).TextColor(theme.Danger).SingleLine().MinWidth(0)
			} else if m.notice != nil {
				ui.Text(c, m.notice()).TextColor(theme.TextMuted).SingleLine().MinWidth(0)
			}
		})
	})
	for _, fn := range later {
		fn()
	}
	a.managerPromptView(c)
	a.managerQuestionView(c)
}

func (a *App) managerToolbar(c *ui.Context) {
	m := &a.manager
	ui.Row(c).Gap(10).Padding(8, 10).AlignItems(ui.Center).Children(func() {
		ui.Toolbar(c, func() {
			// A tool shows its icon, and short when it is not "".
			toolText := func(icon, label, short string) ui.Element {
				b := ui.Button(c, "").Label(label).Tooltip(label)
				return b.Children(func() {
					ui.Icon(c, icons.Get(icon)).FontSize(15).TextColor(theme.TextMuted)
					if short != "" {
						ui.Text(c, short).SingleLine()
					}
				})
			}
			tool := func(icon, label string, text bool) ui.Element {
				if text {
					return toolText(icon, label, label)
				}
				return toolText(icon, label, "")
			}
			if tool("plus", locales.NewConnection(), true).Clicked() {
				a.newConnectionIn(a.managerTarget())
			}
			if tool("folder-plus", locales.NewFolder(), true).Clicked() {
				a.newFolderIn(a.managerTarget())
			}
			if tool("chevrons-down-up", locales.CollapseAll(), false).Clicked() {
				m.tree.Open.Clear()
			}
			if tool("chevrons-up-down", locales.ExpandAll(), false).Clicked() {
				for _, f := range a.hosts.Folders {
					m.tree.Open.Add(managerItem{folder: f.ID})
				}
			}
			// Short, as the toolbar has little room at the manager's size.
			if toolText("import", locales.ImportTitle(), locales.Import()).Clicked() {
				a.openImport()
			}
		}).Label(locales.ManagerTitle()).Grow(1).MinWidth(0)
		ui.Checkbox(c, &m.showDeleted, locales.ShowDeleted())
		labels, ids := []string{locales.FilterAll()}, []string{""}
		chosen := labels[0]
		for _, f := range a.allFolders() {
			label := a.folderLabel(f.ID)
			for n := 2; slices.Contains(labels, label); n++ {
				label = a.folderLabel(f.ID) + " (" + strconv.Itoa(n) + ")"
			}
			labels, ids = append(labels, label), append(ids, f.ID)
			if f.ID == m.filter {
				chosen = label
			}
		}
		if ui.Select(c, &chosen, labels).Label(locales.FolderFilter()).Width(150).Changed() {
			m.filter = ids[max(slices.Index(labels, chosen), 0)]
		}
		search := ui.SearchField(c, &m.query).Placeholder(locales.SearchHosts()).Label(locales.SearchHosts()).Width(220)
		if search.Changed() {
			a.searchChanged()
		}
		if search.Shortcut(0, ui.KeyDown) {
			m.focus = true
		}
		if search.Submitted() {
			a.connectFound()
		}
	})
}

// searchChanged opens the folders holding what the search finds, and
// those open before it once it is cleared.
func (a *App) searchChanged() {
	m := &a.manager
	t := a.newManagerTree()
	if !t.search {
		if m.searching {
			m.tree.Open.Clear()
			for _, it := range m.beforeSearch {
				m.tree.Open.Add(it)
			}
		}
		m.searching, m.beforeSearch = false, nil
		return
	}
	if !m.searching {
		m.searching, m.beforeSearch = true, slices.Collect(m.tree.Open.All())
	}
	for id := range t.matches {
		m.tree.Open.Add(managerItem{folder: id})
	}
	for _, it := range flatItems(t) {
		if it.isHost() {
			m.reveal(it)
			break
		}
	}
}

// flatItems are the items of the rows the tree shows.
func flatItems(t *managerTree) []managerItem {
	var out []managerItem
	var walk func(items []managerItem)
	walk = func(items []managerItem) {
		for _, x := range items {
			out = append(out, x)
			if !x.isHost() && t.m.tree.Open.Has(x) {
				walk(t.children(x))
			}
		}
	}
	walk(t.roots())
	return out
}

// connectFound connects the host chosen, as Enter in the search does.
func (a *App) connectFound() {
	if it, ok := a.managerChosen(); ok && it.isHost() {
		a.connectFromManager(a.hosts.Host(it.host))
	}
}

// managerChosen is the item of the row chosen.
func (a *App) managerChosen() (managerItem, bool) {
	m := &a.manager
	if m.row < 0 || m.row >= m.tree.Rows() {
		return managerItem{}, false
	}
	return m.tree.Item(m.row), true
}

// managerTarget is the folder new hosts and folders go in: the folder
// chosen, the one of the host chosen, else the folder shown.
func (a *App) managerTarget() string {
	if it, ok := a.managerChosen(); ok {
		if !it.isHost() {
			return it.folder
		}
		if h := a.hosts.Host(it.host); h != nil {
			return a.folderOf(h)
		}
	}
	return a.manager.filter
}

func (a *App) managerTable(c *ui.Context, later *[]func()) {
	m := &a.manager
	t := a.newManagerTree()
	scroll, choose := -1, -1
	if m.pick != nil {
		it := *m.pick
		m.pick = nil
		folder := it.folder
		if h := a.hosts.Host(it.host); it.isHost() && h != nil {
			folder = a.folderOf(h)
		} else if f := a.hosts.Folder(it.folder); f != nil {
			folder = f.Parent
		}
		path := a.hosts.FolderPath(folder)
		for _, f := range path {
			m.tree.Open.Add(managerItem{folder: f.ID})
		}
		if m.filter != "" && !slices.ContainsFunc(path, func(f *config.Folder) bool { return f.ID == m.filter }) {
			m.filter = ""
		}
		if row := t.rowOf(it); row >= 0 {
			// The list keeps the item it had chosen, where it goes, unless
			// the app chooses another row: none for this frame, then the
			// row, so that its choice is the app's however the rows moved.
			m.row, choose, scroll = -1, row, row
			c.Invalidate()
		}
	}
	over := managerItem{}
	m.tree.List.Selected = &m.row
	m.tree.List.Sort = &m.sort
	m.tree.List.Label = func(row int) string { return a.managerTitle(m.tree.Item(row)) }
	m.tree.List.DragRows = func(rows []int) any {
		var items managerDrag
		for _, r := range rows {
			items = append(items, m.tree.Item(r))
		}
		return items
	}
	m.tree.List.CellMenu = func(menu *ui.Menu, row, _ int) {
		if row < 0 || row >= m.tree.Rows() {
			return
		}
		*later = append(*later, func() { m.row = row })
		a.managerItemMenu(c, menu, m.tree.Item(row), later)
	}
	// A drop on a folder's row goes into the folder, on a host's into its
	// folder, and past the rows to the top of what the tree shows.
	dropZone := func(e ui.Element, it managerItem, folder string) {
		if items, ok := ui.Drop[managerDrag](e); ok {
			*later = append(*later, func() { a.moveItems(items, folder) })
		}
		if _, ok := ui.DragOver[managerDrag](e); ok {
			over = it
		}
	}
	table := ui.OutlineTable(c, &m.tree, managerColumns(), t.roots(), t.children, func(it managerItem, col int) {
		chosen := m.row >= 0 && m.row < m.tree.Rows() && m.tree.Item(m.row) == it
		// The cell's padding included, for drops to land on the row.
		padY, padX := theme.Space(1.5), theme.Space(2.5)
		cell := ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).AlignSelf(ui.Stretch).
			Margin(-padY, -padX, -padY, 0).Padding(padY, padX, padY, 0)
		if col > 0 {
			cell.Margin(-padY, -padX).Padding(padY, padX)
		}
		if it == m.over && it != (managerItem{}) {
			cell.Background(theme.Accent.Alpha(0.14))
		}
		if !it.isHost() {
			f := a.hosts.Folder(it.folder)
			if f == nil {
				return
			}
			dropZone(cell, it, f.ID)
			cell.Children(func() {
				if col == 0 {
					ui.Icon(c, icons.Get("folder")).FontSize(15).TextColor(folderColor)
					ui.Text(c, f.Name).SingleLine().MinWidth(0)
				}
			})
			return
		}
		h := a.hosts.Host(it.host)
		if h == nil {
			return
		}
		dropZone(cell, it, a.folderOf(h))
		color := theme.Text
		if h.Deleted() {
			color = theme.TextMuted
			if chosen {
				color = theme.AccentText.Alpha(0.75)
			}
		} else if chosen {
			color = theme.AccentText
		}
		cell.Children(func() {
			text := func(s string) {
				ui.Text(c, s).SingleLine().TextColor(color).MinWidth(0)
			}
			switch col {
			case 0:
				icon := "server"
				if h.Deleted() {
					icon = "trash-2"
				}
				ui.Icon(c, icons.Get(icon)).FontSize(14).TextColor(color.Alpha(0.75))
				text(h.Title())
			case 1:
				text(h.Addr)
			case 2:
				ui.Spacer(c)
				text(strconv.Itoa(h.PortOr22()))
			case 3:
				text(h.User)
			case 4:
				notes, _, _ := strings.Cut(h.Notes, "\n")
				text(notes)
			}
		})
		if h.Deleted() {
			cell.Tooltip(locales.DeletedAt(h.DeletedAt.Local().Format("2006-01-02 15:04")))
		}
	}).Grow(1).Border(1, theme.Border).Radius(4)
	dropZone(table, managerItem{}, m.filter)
	m.over = over
	table.Children(func() {
		if t.search && len(t.roots()) == 0 {
			widgets.Muted(c, locales.NoMatchingHosts()).Padding(12)
		} else if len(a.hosts.Hosts) == 0 && len(a.hosts.Folders) == 0 {
			widgets.Muted(c, locales.ManagerEmpty()).Padding(12)
		}
	})
	table.ContextMenu(func(menu *ui.Menu) {
		if menu.Item(locales.NewConnection()).Chosen() {
			*later = append(*later, func() { a.newConnectionIn(m.filter) })
		}
		if menu.Item(locales.NewFolder()).Chosen() {
			*later = append(*later, func() { a.newFolderIn(m.filter) })
		}
	})
	if scroll >= 0 {
		m.row = choose
		m.tree.List.ScrollIntoView(scroll)
	}
	if m.focus && m.tree.List.Focus(c) {
		m.focus = false
	}
	it, ok := a.managerChosen()
	if !ok {
		return
	}
	switch {
	case table.Submitted():
		if it.isHost() {
			h := a.hosts.Host(it.host)
			*later = append(*later, func() { a.connectFromManager(h) })
		} else if m.tree.Open.Has(it) {
			m.tree.Open.Remove(it)
		} else {
			m.tree.Open.Add(it)
		}
	case m.tree.List.Shortcut(c, 0, ui.KeyDelete), m.tree.List.Shortcut(c, ui.Cmd, ui.KeyBackspace):
		*later = append(*later, func() { a.deleteItem(it) })
	case m.tree.List.Shortcut(c, 0, ui.KeyF2):
		*later = append(*later, func() { a.renameItem(it) })
	}
}

// managerTitle is the name the row of it shows.
func (a *App) managerTitle(it managerItem) string {
	if h := a.hosts.Host(it.host); it.isHost() && h != nil {
		return h.Title()
	}
	if f := a.hosts.Folder(it.folder); f != nil {
		return f.Name
	}
	return ""
}

// managerItemMenu is the context menu of a row.
func (a *App) managerItemMenu(c *ui.Context, menu *ui.Menu, it managerItem, later *[]func()) {
	do := func(fn func()) { *later = append(*later, fn) }
	if !it.isHost() {
		f := a.hosts.Folder(it.folder)
		if f == nil {
			return
		}
		if menu.Item(locales.NewConnection()).Chosen() {
			do(func() { a.newConnectionIn(f.ID) })
		}
		if menu.Item(locales.NewSubfolder()).Chosen() {
			do(func() { a.newFolderIn(f.ID) })
		}
		menu.Separator()
		if menu.Item(locales.Rename()).Shortcut(0, ui.KeyF2).Chosen() {
			do(func() { a.renameItem(it) })
		}
		a.moveMenu(menu, it, do)
		menu.Separator()
		if menu.Item(locales.Delete()).Shortcut(0, ui.KeyDelete).Chosen() {
			do(func() { a.deleteItem(it) })
		}
		return
	}
	h := a.hosts.Host(it.host)
	if h == nil {
		return
	}
	if h.Deleted() {
		if menu.Item(locales.Restore()).Chosen() {
			do(func() { a.restoreHost(h) })
		}
		if menu.Item(locales.DeleteForever()).Chosen() {
			do(func() { a.deleteItem(it) })
		}
		menu.Separator()
		if menu.Item(locales.CopyHost()).Chosen() {
			c.WriteClipboard(h.Addr)
		}
		return
	}
	if menu.Item(locales.Connect()).Chosen() {
		do(func() { a.connectFromManager(h) })
	}
	if menu.Item(locales.Edit()).Chosen() {
		do(func() { a.openEditor(h, WindowManager) })
	}
	if menu.Item(locales.Duplicate()).Chosen() {
		do(func() { a.duplicateHost(h) })
	}
	if menu.Item(locales.Rename()).Shortcut(0, ui.KeyF2).Chosen() {
		do(func() { a.renameItem(it) })
	}
	a.moveMenu(menu, it, do)
	menu.Separator()
	if menu.Item(locales.CopyHost()).Chosen() {
		c.WriteClipboard(h.Addr)
	}
	menu.Separator()
	if menu.Item(locales.Delete()).Shortcut(0, ui.KeyDelete).Chosen() {
		do(func() { a.deleteItem(it) })
	}
}

// moveMenu is the submenu moving it to another folder.
func (a *App) moveMenu(menu *ui.Menu, it managerItem, do func(func())) {
	here := ""
	if h := a.hosts.Host(it.host); it.isHost() && h != nil {
		here = a.folderOf(h)
	} else if f := a.hosts.Folder(it.folder); f != nil {
		here = f.Parent
		if a.hosts.Folder(here) == nil {
			here = ""
		}
	}
	menu.Submenu(locales.MoveToFolder(), func(menu *ui.Menu) {
		if menu.Item(locales.TopLevel()).ID("").Disabled(here == "").Chosen() {
			do(func() { a.moveItems(managerDrag{it}, "") })
		}
		for _, f := range a.allFolders() {
			inside := !it.isHost() && slices.ContainsFunc(a.hosts.FolderPath(f.ID), func(g *config.Folder) bool { return g.ID == it.folder })
			if menu.Item(a.folderLabel(f.ID)).ID(f.ID).Disabled(f.ID == here || inside).Chosen() {
				do(func() { a.moveItems(managerDrag{it}, f.ID) })
			}
		}
	})
}

// saveFromManager saves the hosts the manager changed, saying so in the
// manager when it fails, and shows the change in every window.
func (a *App) saveFromManager() {
	a.manager.saveErr = nil
	if err := a.saveHosts(a.hosts); err != nil {
		a.manager.saveErr = func() string { return locales.ErrSaveFailed(err.Error()) }
	}
	a.redraw()
}

// connectFromManager opens a session to h in the main window, in its new
// tab when one is selected, and brings the main window forward, closing
// the manager unless the user keeps it open.
func (a *App) connectFromManager(h *config.Host) {
	if h == nil || h.Deleted() {
		return
	}
	if !a.settings.KeepManagerOpen {
		a.closeWindow(WindowManager)
	}
	a.openSession(h)
	a.plat.FocusWindow(WindowMain)
	a.redraw()
}

// newConnectionIn opens the editor for a new host in folder, modal to the
// manager.
func (a *App) newConnectionIn(folder string) {
	open := a.windowOpen(WindowEditor)
	a.openEditor(nil, WindowManager)
	if !open && a.editor != nil {
		a.editor.folder = folder
	}
}

// newFolderIn asks for the name of a folder to make in parent.
func (a *App) newFolderIn(parent string) {
	a.manager.prompt = &managerPrompt{title: locales.NewFolder, name: locales.NewFolder(), ok: func(name string) {
		f := a.hosts.AddFolder(name, parent)
		a.manager.reveal(managerItem{folder: f.ID})
		a.saveFromManager()
	}}
}

// renameItem asks for a new name of the folder or host.
func (a *App) renameItem(it managerItem) {
	if h := a.hosts.Host(it.host); it.isHost() && h != nil {
		a.manager.prompt = &managerPrompt{title: locales.RenameConnection, name: h.Name, optional: true, ok: func(name string) {
			h.Name = name
			a.saveFromManager()
		}}
		return
	}
	if f := a.hosts.Folder(it.folder); f != nil {
		a.manager.prompt = &managerPrompt{title: locales.RenameFolder, name: f.Name, ok: func(name string) {
			f.Name = name
			a.saveFromManager()
		}}
	}
}

// deleteItem deletes a folder, asking first, as its contents move to the
// folder it is in; moves a host to the deleted ones; and deletes a
// deleted host for good, asking first.
func (a *App) deleteItem(it managerItem) {
	m := &a.manager
	if !it.isHost() {
		f := a.hosts.Folder(it.folder)
		if f == nil {
			return
		}
		body := locales.DeleteFolderBodyTop
		if path := a.hosts.FolderPath(f.ID); len(path) > 1 {
			parent := path[len(path)-2]
			body = func() string { return locales.DeleteFolderBody(parent.Name) }
		}
		title := func() string { return locales.DeleteFolderTitle(f.Name) }
		m.question = &managerQuestion{title: title, body: body, ok: locales.Delete, yes: func() {
			a.hosts.RemoveFolder(f.ID)
			a.saveFromManager()
		}}
		return
	}
	h := a.hosts.Host(it.host)
	if h == nil {
		return
	}
	// The hosts h is the jump host of cannot connect without it.
	users := a.jumpUsers(h)
	switch {
	case h.Deleted():
		title := func() string { return locales.DeleteForeverTitle(h.Title()) }
		body := func() string {
			if len(users) > 0 {
				return locales.DeleteForeverBody() + " " + locales.JumpHostInUseForever(a.hostTitles(users))
			}
			return locales.DeleteForeverBody()
		}
		m.question = &managerQuestion{title: title, body: body, ok: locales.DeleteForever, yes: func() {
			a.deleteHostForever(h)
		}}
	case len(users) > 0:
		title := func() string { return locales.DeleteHostTitle(h.Title()) }
		body := func() string { return locales.JumpHostInUse(a.hostTitles(users)) }
		m.question = &managerQuestion{title: title, body: body, ok: locales.Delete, yes: func() {
			a.hosts.SoftDelete(h.ID, time.Now())
			a.saveFromManager()
		}}
	default:
		a.hosts.SoftDelete(h.ID, time.Now())
		a.saveFromManager()
	}
}

// jumpUsers are the hosts, not deleted, whose jump host is h, as the
// Proxy page of the editor sets it.
func (a *App) jumpUsers(h *config.Host) []*config.Host {
	var out []*config.Host
	for _, x := range a.hosts.Live() {
		if p := config.ProxyOf(x); x != h && p != nil && p.Kind == config.ProxyJump && p.JumpHost == h.ID {
			out = append(out, x)
		}
	}
	return out
}

// hostTitles names hosts in a list, in the language shown.
func (a *App) hostTitles(hosts []*config.Host) string {
	var names []string
	for _, h := range hosts {
		names = append(names, "“"+h.Title()+"”")
	}
	return strings.Join(names, locales.ListSeparator())
}

// restoreHost brings a deleted host back, and chooses it.
func (a *App) restoreHost(h *config.Host) {
	a.hosts.Restore(h.ID)
	a.manager.reveal(managerItem{host: h.ID})
	a.saveFromManager()
}

// deleteHostForever deletes h for good, with what the app keeps for it:
// every deletion for good goes through here, so that what is kept
// elsewhere for the host, as its password in the system's keychain, goes
// with it. Sessions open to h keep it until they close.
func (a *App) deleteHostForever(h *config.Host) {
	a.hosts.Remove(h.ID)
	delete(a.passwords, h.ID)
	delete(a.passwords, proxySecretKey(h.ID))
	a.deleteHostSecrets(h.ID)
	a.saveFromManager()
}

// duplicateHost adds a copy of h after it, with its proxy or jump host,
// its tunnels and the passwords remembered for it, and chooses it.
func (a *App) duplicateHost(h *config.Host) {
	dup := h.Clone()
	dup.ID = config.NewHostID()
	for a.hosts.Host(dup.ID) != nil {
		dup.ID += "-"
	}
	dup.Name = locales.CopyOfName(h.Title())
	dup.LastConnectedAt, dup.DeletedAt, dup.CreatedAt = time.Time{}, time.Time{}, time.Now()
	i := slices.Index(a.hosts.Hosts, h)
	a.hosts.Hosts = slices.Insert(a.hosts.Hosts, i+1, dup)
	a.copyHostSecrets(h, dup)
	for from, to := range map[string]string{h.ID: dup.ID, proxySecretKey(h.ID): proxySecretKey(dup.ID)} {
		if pw, ok := a.passwords[from]; ok {
			a.passwords[to] = pw
		}
	}
	a.manager.reveal(managerItem{host: dup.ID})
	a.saveFromManager()
}

// moveItems moves hosts and folders into folder, "" for the top level. A
// folder does not go into itself, or into a folder inside it.
func (a *App) moveItems(items managerDrag, folder string) {
	if folder != "" && a.hosts.Folder(folder) == nil {
		return
	}
	path := a.hosts.FolderPath(folder)
	moved := false
	for _, it := range items {
		if h := a.hosts.Host(it.host); it.isHost() {
			if h != nil && a.folderOf(h) != folder {
				h.Folder, moved = folder, true
			}
			continue
		}
		f := a.hosts.Folder(it.folder)
		if f == nil || f.Parent == folder || slices.Contains(path, f) {
			continue
		}
		f.Parent, moved = folder, true
	}
	if !moved {
		return
	}
	if folder != "" {
		a.manager.tree.Open.Add(managerItem{folder: folder})
	}
	if len(items) > 0 {
		a.manager.reveal(items[0])
	}
	a.saveFromManager()
}

// managerPromptView asks for a name in the manager's window.
func (a *App) managerPromptView(c *ui.Context) {
	p := a.manager.prompt
	if p == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(360).Gap(12).Children(func() {
			ui.Text(c, p.title()).FontSize(15).Bold()
			name := strings.TrimSpace(p.name)
			submit := ui.TextInput(c, &p.name).Label(locales.FolderName()).AutoFocus().Submitted()
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.Cancel()).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c, locales.Ok()).Disabled(name == "" && !p.optional).Clicked() {
					submit = true
				}
			})
			if submit && (name != "" || p.optional) {
				open = false
				p.ok(name)
			}
		})
	})
	if !open && a.manager.prompt == p {
		a.manager.prompt = nil
	}
}

// managerQuestionView asks before deleting, in the manager's window.
func (a *App) managerQuestionView(c *ui.Context) {
	q := a.manager.question
	if q == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(400).Gap(12).Children(func() {
			ui.Text(c, q.title()).FontSize(15).Bold()
			ui.Text(c, q.body())
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.Cancel()).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c, q.ok()).Clicked() {
					open = false
					q.yes()
				}
			})
		})
	})
	if !open && a.manager.question == q {
		a.manager.question = nil
	}
}
