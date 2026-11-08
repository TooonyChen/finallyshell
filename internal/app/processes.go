package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"finallyshell/internal/icons"
	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// procRefresh is how often the Processes tab reads the list while it
// shows.
const procRefresh = 2 * time.Second

// killWait is how long Kill waits after SIGTERM before it offers SIGKILL.
var killWait = 3 * time.Second // a variable, for tests

// procTimeout is how long a details read or a signal waits for the
// server.
const procTimeout = 20 * time.Second

// Where the dividers of the Processes tab start: the list's share above
// the details, and the fields' share beside the environment.
const (
	defaultProcListPct   = 55
	defaultProcFieldsPct = 50
)

var (
	// The list keeps its filter and a few rows; the details their tabs
	// and a field.
	procListLimits = splitLimits{first: 90, second: 90, vertical: true, percent: true}
	// The fields keep their labels and some of the value; the
	// environment its two columns.
	procFieldsLimits = splitLimits{first: 200, second: 200, percent: true}
)

// processAPI is what the Processes tab asks of the server; tests stand
// in for it.
type processAPI interface {
	// list reads the processes; l keeps the CPU times between reads.
	list(ctx context.Context, client *ssh.Client, l *monitor.ProcessLister) ([]monitor.Process, error)
	details(ctx context.Context, client *ssh.Client, pid int) (monitor.ProcessDetails, error)
	signal(ctx context.Context, client *ssh.Client, pid int, sig string) error
	// running tells whether pid, started at start, is still there.
	running(ctx context.Context, client *ssh.Client, pid int, start uint64) (bool, error)
}

// serverProcesses is the processAPI of a real server.
type serverProcesses struct{}

func (serverProcesses) list(ctx context.Context, client *ssh.Client, l *monitor.ProcessLister) ([]monitor.Process, error) {
	return l.List(ctx, client)
}

func (serverProcesses) details(ctx context.Context, client *ssh.Client, pid int) (monitor.ProcessDetails, error) {
	return monitor.ReadProcessDetails(ctx, client, pid)
}

func (serverProcesses) signal(ctx context.Context, client *ssh.Client, pid int, sig string) error {
	return monitor.SendSignal(ctx, client, pid, sig)
}

func (serverProcesses) running(ctx context.Context, client *ssh.Client, pid int, start uint64) (bool, error) {
	return monitor.ProcessRunning(ctx, client, pid, start)
}

// processesUI is the Processes tab's state shared by the sessions: the
// sizes of its panes, the signal dialog, and what reads the server.
type processesUI struct {
	listPct, fieldsPct float32
	dlg                *signalDialog
	api                processAPI // serverProcesses when nil
}

func (a *App) procAPI() processAPI {
	if a.processes.api == nil {
		return serverProcesses{}
	}
	return a.processes.api
}

// procState is a session's Processes tab: the list, its filter, sort
// and choice, and the processes opened in tabs below it.
type procState struct {
	poll   sessionPoll
	list   []monitor.Process
	loaded bool  // a list was read on this connection
	err    error // why the last read failed

	filter string
	table  ui.ListState
	sort   ui.SortOrder
	row    int
	pid    int // the process chosen, which a refresh keeps; 0 for none

	// The rows shown, the list filtered and sorted, and what they were
	// made from.
	view    []monitor.Process
	viewOf  []monitor.Process
	viewFor string
	viewBy  ui.SortOrder

	tabs []*procTab
	tab  int
}

// procTab is a process opened below the list.
type procTab struct {
	pid   int
	name  string
	start uint64

	loading bool
	d       monitor.ProcessDetails
	err     error
	gen     int
	cancel  context.CancelFunc

	env    ui.ListState
	envRow int
}

func (t *procTab) title() string { return strconv.Itoa(t.pid) + "-" + t.name }

// openProcesses shows the Processes tab, as a click on the side panel's
// process list does.
func (a *App) openProcesses() { a.bottomTab = tabProcesses }

// pollProcesses keeps s's list read while the tab shows it.
func (a *App) pollProcesses(s *Session) {
	ps := &s.procs
	api := a.procAPI()
	polling := a.keepPolling(&ps.poll, s, procRefresh, func() pollRead {
		var lister monitor.ProcessLister
		return func(ctx context.Context, client *ssh.Client) func() {
			list, err := api.list(ctx, client, &lister)
			return func() {
				ps.err = err
				if err == nil {
					ps.list, ps.loaded = list, true
				}
			}
		}
	})
	if !polling && s.status == statusConnecting {
		ps.loaded = false
	}
}

// procColumns are the list's columns; their IDs are the sort's.
func procColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "pid", Title: locales.ColPID(), Width: 72, Align: ui.End, Sortable: true},
		{ID: "user", Title: locales.ColProcUser(), Width: 96, Sortable: true},
		{ID: "mem", Title: locales.ColProcMemory(), Width: 84, Align: ui.End, Sortable: true},
		{ID: "cpu", Title: locales.ColProcCPU(), Width: 70, Align: ui.End, Sortable: true},
		{ID: "name", Title: locales.ColProcName(), Width: 140, Sortable: true},
		{ID: "args", Title: locales.ColProcCommand(), MinWidth: 220, Sortable: true},
		{ID: "path", Title: locales.ColProcPath(), Width: 200, Sortable: true},
	}
}

// filterProcs is the processes whose name, command line or user holds
// query, without regard to case, sorted as by says.
func filterProcs(list []monitor.Process, query string, by ui.SortOrder) []monitor.Process {
	query = strings.ToLower(strings.TrimSpace(query))
	var out []monitor.Process
	for _, p := range list {
		if query == "" || strings.Contains(strings.ToLower(p.Name), query) ||
			strings.Contains(strings.ToLower(p.Args), query) || strings.Contains(strings.ToLower(p.User), query) {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(x, y monitor.Process) int {
		var r int
		switch by.Column {
		case "user":
			r = cmp.Compare(x.User, y.User)
		case "mem":
			r = cmp.Compare(x.RSS, y.RSS)
		case "cpu":
			r = cmp.Compare(x.CPU, y.CPU)
		case "name":
			r = cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		case "args":
			r = cmp.Compare(x.Args, y.Args)
		case "path":
			r = cmp.Compare(x.Path, y.Path)
		}
		if by.Descending {
			r = -r
		}
		if r == 0 { // the PIDs, ascending, under equal values
			r = cmp.Compare(x.PID, y.PID)
			if by.Column == "pid" && by.Descending {
				r = -r
			}
		}
		return r
	})
	return out
}

// shown is the rows the table shows, made again when the list, the
// filter or the sort changed.
func (ps *procState) shown() []monitor.Process {
	if ps.sort.Column == "" {
		ps.sort = ui.SortOrder{Column: "cpu", Descending: true}
	}
	same := len(ps.viewOf) == len(ps.list) && (len(ps.list) == 0 || &ps.viewOf[0] == &ps.list[0])
	if !same || ps.viewFor != ps.filter || ps.viewBy != ps.sort {
		ps.view = filterProcs(ps.list, ps.filter, ps.sort)
		ps.viewOf, ps.viewFor, ps.viewBy = ps.list, ps.filter, ps.sort
	}
	return ps.view
}

// processPanel is the "Processes" tab: the active session's processes,
// and the details of those opened, below them.
func (a *App) processPanel(c *ui.Context) {
	s := a.current()
	if s == nil || s.isNewTab() {
		return
	}
	ps := &s.procs
	a.pollProcesses(s)
	if a.processes.listPct == 0 {
		a.processes.listPct, a.processes.fieldsPct = defaultProcListPct, defaultProcFieldsPct
	}
	ui.Column(c.Key(s.ID)).Grow(1).MinHeight(0).Children(func() {
		if s.status != statusConnected {
			a.statusBanner(c, s)
		}
		a.procToolbar(c, s)
		if len(ps.tabs) == 0 {
			a.procList(c, s)
			return
		}
		split := ui.SplitVerticalPercent(c, &a.processes.listPct, func() { a.procList(c, s) }, func() { a.procDetails(c, s) }).
			SplitReset(defaultProcListPct).Grow(1)
		a.splitMoved(c, split, &a.processes.listPct, procListLimits)
	})
}

func (a *App) procToolbar(c *ui.Context, s *Session) {
	ps := &s.procs
	ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
		ui.SearchField(c, &ps.filter).Placeholder(locales.ProcFilter()).Label(locales.ProcFilter()).Width(320).Shrink(1)
		switch {
		case !execAllowed(s.Host):
			widgets.Muted(c, locales.MonitorOff())
		case errors.Is(ps.err, monitor.ErrNotLinux):
			widgets.Muted(c, locales.ProcOnlyLinux())
		case ps.err != nil:
			msg := locales.ProcListFailed(describeProcError(ps.err))
			ui.Text(c, msg).TextColor(theme.Danger).SingleLine().Shrink(1).MinWidth(0).Tooltip(msg)
		case s.status == statusConnected && !ps.loaded:
			ui.Spinner(c).Label(locales.ProcReading())
			widgets.Muted(c, locales.ProcReading())
		case ps.loaded:
			n, shown := len(ps.list), len(ps.shown())
			text := locales.ProcCount(n)
			if shown != n {
				text = locales.ProcCountFiltered(shown, n)
			}
			widgets.Muted(c, text).SingleLine()
		}
		ui.Spacer(c)
		if widgets.IconButton(c, "refresh-cw", locales.Refresh()).Clicked() {
			ps.poll.pollNow()
		}
	})
}

func (a *App) procList(c *ui.Context, s *Session) {
	ps := &s.procs
	view := ps.shown()
	if errors.Is(ps.err, monitor.ErrNotLinux) {
		view = nil
	}
	// The choice follows its process as the rows move.
	ps.row = slices.IndexFunc(view, func(p monitor.Process) bool { return p.PID == ps.pid })
	ps.table.Selected = &ps.row
	ps.table.Sort = &ps.sort
	ps.table.KeepOffset = true
	ps.table.Key = func(i int) any { return view[i].PID }
	ps.table.Label = func(i int) string { return strconv.Itoa(view[i].PID) + " " + view[i].Name }
	ps.table.CellMenu = func(m *ui.Menu, row, col int) {
		if row >= 0 && row < len(view) {
			ps.pid = view[row].PID
			a.procMenu(c, m, s, view[row])
		}
	}
	connected := s.status == statusConnected
	t := ui.Table(c, &ps.table, procColumns(), len(view), func(row, col int) {
		p := view[row]
		switch col {
		case 0:
			ui.Text(c, strconv.Itoa(p.PID)).SingleLine()
		case 1:
			ui.Text(c, p.User).SingleLine()
		case 2:
			ui.Text(c, formatSize(int64(p.RSS))).SingleLine()
		case 3:
			ui.Text(c, fmt.Sprintf("%.1f", p.CPU)).SingleLine()
		case 4:
			ui.Text(c, p.Name).SingleLine()
		case 5:
			ui.Text(c, p.Args).SingleLine().Tooltip(p.Args)
		case 6:
			ui.Text(c, orDash(p.Path)).SingleLine()
		}
	}).Grow(1).Label(locales.TabProcesses())
	if !connected {
		t.Opacity(0.5) // the last list, from before the connection ended
	}
	if ps.row >= 0 && ps.row < len(view) {
		ps.pid = view[ps.row].PID
	}
	if t.Submitted() && ps.row >= 0 && ps.row < len(view) {
		a.openProcTab(s, view[ps.row])
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// procMenu is the context menu of p's row.
func (a *App) procMenu(c *ui.Context, m *ui.Menu, s *Session, p monitor.Process) {
	connected := s.status == statusConnected
	if m.Item(locales.ProcCopyPID()).Chosen() {
		c.WriteClipboard(strconv.Itoa(p.PID))
	}
	if m.Item(locales.ProcCopyCommand()).Chosen() {
		c.WriteClipboard(p.Args)
	}
	m.Separator()
	if m.Item(locales.ProcOpenDetails()).Disabled(!connected).Chosen() && connected {
		a.openProcTab(s, p)
	}
	m.Separator()
	m.Submenu(locales.ProcSendSignal(), func(m *ui.Menu) {
		for _, sig := range monitor.Signals {
			if m.Item("SIG"+sig).ID(sig).Subtitle(signalText(sig)).Disabled(!connected).Chosen() && connected {
				a.askSignal(s, p, sig, false)
			}
		}
	})
	if m.Item(locales.ProcKill()).Danger().Disabled(!connected).Chosen() && connected {
		a.askSignal(s, p, "TERM", true)
	}
	m.Separator()
	if m.Item(locales.Refresh()).Chosen() {
		s.procs.poll.pollNow()
	}
}

// signalText says what sig does, for the menu and the dialog.
func signalText(sig string) string {
	switch sig {
	case "TERM":
		return locales.SigTERM()
	case "KILL":
		return locales.SigKILL()
	case "HUP":
		return locales.SigHUP()
	case "INT":
		return locales.SigINT()
	case "STOP":
		return locales.SigSTOP()
	case "CONT":
		return locales.SigCONT()
	}
	return sig
}

// describeProcError says why the list or the details could not be read.
func describeProcError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return locales.SysInfoTimeout()
	case errors.Is(err, monitor.ErrScriptOutput):
		return locales.SysInfoNoShell()
	case errors.Is(err, monitor.ErrNotLinux):
		return locales.ProcOnlyLinux()
	case errors.Is(err, errNoClient):
		return locales.NotConnected()
	}
	return describeError(err)
}

// openProcTab opens p's details below the list, or chooses its tab when
// it is open, and reads them.
func (a *App) openProcTab(s *Session, p monitor.Process) {
	ps := &s.procs
	if i := slices.IndexFunc(ps.tabs, func(t *procTab) bool { return t.pid == p.PID && t.start == p.Start }); i >= 0 {
		ps.tab = i
		return
	}
	t := &procTab{pid: p.PID, name: p.Name, start: p.Start, envRow: -1}
	ps.tabs = append(ps.tabs, t)
	ps.tab = len(ps.tabs) - 1
	a.readProcTab(s, t)
}

// readProcTab reads t's details on a goroutine.
func (a *App) readProcTab(s *Session, t *procTab) {
	if t.cancel != nil {
		t.cancel()
	}
	t.gen++
	if s.status != statusConnected {
		t.loading, t.err, t.cancel = false, errNoClient, nil
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), procTimeout)
	gen, client, api := t.gen, s.client, a.procAPI()
	t.loading, t.err, t.cancel = true, nil, cancel
	go func() {
		d, err := api.details(ctx, client, t.pid)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = context.DeadlineExceeded
		}
		cancel()
		a.post(func() {
			if t.gen != gen {
				return
			}
			t.loading, t.cancel = false, nil
			// Another process with the PID since it was opened.
			if err == nil && t.start != 0 && d.Start != 0 && d.Start != t.start {
				err = monitor.ErrNoProcess
			}
			t.err = err
			if err == nil {
				t.d = d
				if d.Name != "" {
					t.name = d.Name
				}
			}
		})
	}()
}

func (a *App) closeProcTab(s *Session, i int) {
	ps := &s.procs
	if i < 0 || i >= len(ps.tabs) {
		return
	}
	if t := ps.tabs[i]; t.cancel != nil {
		t.cancel()
	}
	ps.tabs = slices.Delete(ps.tabs, i, i+1)
	if ps.tab > i || ps.tab >= len(ps.tabs) {
		ps.tab--
	}
	ps.tab = max(ps.tab, 0)
}

// procDetails is the tabs of the processes opened, and the details of
// the one chosen.
func (a *App) procDetails(c *ui.Context, s *Session) {
	ps := &s.procs
	ps.tab = min(max(ps.tab, 0), len(ps.tabs)-1)
	closed := -1
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Background(colors.sidebar).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).PaddingX(4).Shrink(0).Children(func() {
			tabs := ui.TabsBase(c, &ps.tab, len(ps.tabs))
			tabs.List.Grow(1).MinWidth(0).Clip().Children(func() {
				for i, t := range ps.tabs {
					tab := tabs.Tab(i).Row().Gap(6).AlignItems(ui.Center).Padding(5, 6, 4, 10).
						MinWidth(0).Shrink(1).Tooltip(t.title()).BorderWidth(0, 1, 0, 0).BorderColor(colors.line)
					selected := i == ps.tab
					if selected {
						tab.Background(theme.Background)
					}
					hovered := tab.Hovered()
					tab.Children(func() {
						if selected {
							ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(2).Background(theme.Accent)
						}
						name := ui.Text(c, t.title()).SingleLine().Shrink(1).MinWidth(0).FontSize(12.5)
						if !selected {
							name.TextColor(theme.TextMuted)
						}
						if procTabClose(c, hovered || selected) {
							closed = i
						}
					})
				}
			})
			if t := ps.tabs[ps.tab]; t != nil {
				a.procTabTools(c, s, t)
			}
		})
		if t := ps.tabs[ps.tab]; t != nil {
			a.procTabView(c, s, t)
		}
	})
	if closed >= 0 {
		a.closeProcTab(s, closed)
	}
}

// procTabClose is the × of a process's tab, shown while the pointer is
// over the tab or it is chosen; it tells whether it was clicked.
func procTabClose(c *ui.Context, show bool) bool {
	label := locales.Close()
	b := ui.Box(c.Key("close")).Size(16, 16).Center().Radius(4).Role(ui.RoleButton).Label(label).Tooltip(label).Shrink(0)
	over := b.Hovered()
	if over {
		b.Background(theme.Text.Alpha(0.08))
	}
	b.Children(func() {
		if show || over {
			ui.Icon(c, icons.Get("x")).FontSize(12).TextColor(theme.TextMuted)
		}
	})
	return b.Clicked()
}

func (a *App) procTabView(c *ui.Context, s *Session, t *procTab) {
	connected := s.status == statusConnected
	ui.Column(c.Key(t)).Grow(1).MinHeight(0).Children(func() {
		switch {
		case t.loading && t.d.PID == 0:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10).Children(func() {
				ui.Spinner(c).Label(locales.ReadingServerInfo())
				widgets.Muted(c, locales.ReadingServerInfo())
			})
			return
		case t.err != nil && t.d.PID == 0:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 10).Children(func() {
				ui.Text(c, procDetailsError(t)).TextColor(theme.Danger).Shrink(1).MinWidth(0)
				if connected && ui.Button(c, locales.Retry()).Clicked() {
					a.readProcTab(s, t)
				}
			})
			return
		}
		split := ui.SplitPercent(c, &a.processes.fieldsPct, func() { a.procFields(c, t) }, func() { a.procEnv(c, t) }).
			SplitReset(defaultProcFieldsPct).Grow(1)
		a.splitMoved(c, split, &a.processes.fieldsPct, procFieldsLimits)
	})
}

func procDetailsError(t *procTab) string {
	if errors.Is(t.err, monitor.ErrNoProcess) {
		return locales.ProcGone(strconv.Itoa(t.pid))
	}
	return locales.ProcDetailsFailed(describeProcError(t.err))
}

// procField is a line of a process's details.
type procField struct {
	label, value string
	denied       bool // needs root to read
}

func procFields(t *procTab) []procField {
	d := t.d
	return []procField{
		{label: locales.ColPID(), value: strconv.Itoa(t.pid)},
		{label: locales.ColProcName(), value: orDash(d.Name)},
		{label: locales.ColProcUser(), value: orDash(d.User)},
		{label: locales.ColProcCommand(), value: orDash(strings.Join(d.Args, " "))},
		{label: locales.ColProcPath(), value: orDash(d.Path), denied: d.PathDenied},
		{label: locales.ProcCwd(), value: orDash(d.Cwd), denied: d.CwdDenied},
	}
}

// procTabTools are the buttons of the chosen process's tab, at the end
// of the tab strip: copying its details, and reading them again.
func (a *App) procTabTools(c *ui.Context, s *Session, t *procTab) {
	ui.Row(c.Key(t)).Gap(4).AlignItems(ui.Center).PaddingX(4).Shrink(0).Children(func() {
		if t.loading {
			ui.Spinner(c).Label(locales.ReadingServerInfo())
		} else if t.err != nil && t.d.PID != 0 {
			// Read before: the details stay, with why they are not new.
			msg := procDetailsError(t)
			ui.Text(c, msg).TextColor(theme.Danger).FontSize(12).SingleLine().MaxWidth(320).Tooltip(msg)
		}
		if widgets.IconButton(c, "copy", locales.Copy()).Clicked() && t.d.PID != 0 {
			c.WriteClipboard(procDetailsSummary(t))
		}
		if widgets.IconButton(c, "refresh-cw", locales.Refresh()).Clicked() && s.status == statusConnected {
			a.readProcTab(s, t)
		}
	})
}

func (a *App) procFields(c *ui.Context, t *procTab) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(8, 10, 8, 10).Gap(6).Children(func() {
			for i, f := range procFields(t) {
				ui.Row(c.Key(i)).Gap(10).AlignItems(ui.Start).Children(func() {
					widgets.Muted(c, f.label).Width(120).Shrink(0)
					ui.Row(c).Gap(8).Grow(1).MinWidth(0).Wrap().Children(func() {
						ui.Text(c, f.value).Selectable()
						if f.denied {
							widgets.Muted(c, locales.ProcNeedsRoot()).FontSize(11.5)
						}
					})
				})
			}
		})
	})
}

// procDetailsSummary is what Copy puts on the clipboard: the fields and
// the environment, as text.
func procDetailsSummary(t *procTab) string {
	var b strings.Builder
	for _, f := range procFields(t) {
		fmt.Fprintf(&b, "%s: %s\n", f.label, f.value)
	}
	if len(t.d.Env) > 0 {
		fmt.Fprintf(&b, "%s:\n", locales.ProcEnvironment())
		for _, v := range t.d.Env {
			fmt.Fprintf(&b, "  %s=%s\n", v.Name, v.Value)
		}
	}
	return b.String()
}

func envColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "name", Title: locales.EnvName(), Width: 170},
		{ID: "value", Title: locales.EnvValue(), MinWidth: 160},
	}
}

func (a *App) procEnv(c *ui.Context, t *procTab) {
	env := t.d.Env
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(4, 8, 4, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.ProcEnvironment()).Bold()
			if t.d.EnvDenied {
				widgets.Muted(c, "- "+locales.ProcNeedsRoot())
			} else {
				widgets.Muted(c, strconv.Itoa(len(env)))
			}
			ui.Spacer(c)
			cp := widgets.IconButton(c, "copy", locales.EnvCopyAll())
			if cp.Clicked() && len(env) > 0 {
				c.WriteClipboard(envText(env))
			}
		})
		t.env.Selected = &t.envRow
		t.env.Label = func(i int) string { return env[i].Name }
		t.env.CellMenu = func(m *ui.Menu, row, col int) {
			if row < 0 || row >= len(env) {
				return
			}
			v := env[row]
			if m.Item(locales.EnvCopyValue()).Chosen() {
				c.WriteClipboard(v.Value)
			}
			if m.Item(locales.EnvCopyPair()).Chosen() {
				c.WriteClipboard(v.Name + "=" + v.Value)
			}
			m.Separator()
			if m.Item(locales.EnvCopyAll()).Chosen() {
				c.WriteClipboard(envText(env))
			}
		}
		ui.Table(c, &t.env, envColumns(), len(env), func(row, col int) {
			v := env[row]
			if col == 0 {
				ui.Text(c, v.Name).SingleLine()
			} else {
				ui.Text(c, v.Value).SingleLine().Tooltip(v.Value)
			}
		}).Grow(1).Label(locales.ProcEnvironment())
	})
}

func envText(env []monitor.EnvVar) string {
	var b strings.Builder
	for _, v := range env {
		b.WriteString(v.Name + "=" + v.Value + "\n")
	}
	return b.String()
}

// signalDialog asks before sending a signal, sends it, and shows how it
// went. Kill sends TERM, waits, and offers KILL when the process is
// still there.
type signalDialog struct {
	s     *Session
	pid   int
	name  string
	start uint64
	sig   string
	kill  bool // the Kill command: TERM, then the offer of KILL

	busy    bool
	waiting bool // after Kill's TERM, for the process to go
	still   bool // still running after Kill's TERM: offering KILL
	err     error
	cancel  context.CancelFunc
}

// askSignal opens the dialog asking whether to send sig to p; kill for
// the Kill command.
func (a *App) askSignal(s *Session, p monitor.Process, sig string, kill bool) {
	a.closeSignalDialog()
	a.processes.dlg = &signalDialog{s: s, pid: p.PID, name: p.Name, start: p.Start, sig: sig, kill: kill}
}

func (a *App) closeSignalDialog() {
	if d := a.processes.dlg; d != nil && d.cancel != nil {
		d.cancel()
	}
	a.processes.dlg = nil
}

// sendSignal sends d's signal on a goroutine.
func (a *App) sendSignal(d *signalDialog) {
	s := d.s
	if d.busy || s.status != statusConnected {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.busy, d.err, d.cancel = true, nil, cancel
	client, api, sig, kill := s.client, a.procAPI(), d.sig, d.kill && !d.still
	if d.still {
		sig = "KILL"
	}
	go func() {
		defer cancel()
		sctx, scancel := context.WithTimeout(ctx, procTimeout)
		err := api.signal(sctx, client, d.pid, sig)
		scancel()
		if err != nil || !kill {
			a.post(func() { a.signalSent(d, err, false) })
			return
		}
		a.post(func() {
			if a.processes.dlg == d {
				d.waiting = true
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(killWait):
		}
		rctx, rcancel := context.WithTimeout(ctx, procTimeout)
		running, err := api.running(rctx, client, d.pid, d.start)
		rcancel()
		a.post(func() { a.signalSent(d, err, running) })
	}()
}

// signalSent is the end of sendSignal: the dialog closes, or shows the
// error, or offers KILL for a process TERM did not end.
func (a *App) signalSent(d *signalDialog, err error, running bool) {
	if a.processes.dlg != d {
		return
	}
	d.busy, d.waiting, d.cancel = false, false, nil
	if err != nil && !errors.Is(err, context.Canceled) {
		d.err = err
		return
	}
	d.s.procs.poll.pollNow()
	if running {
		d.still = true
		return
	}
	a.processes.dlg = nil
}

func signalErrorText(err error) string {
	var se *monitor.SignalError
	if errors.As(err, &se) {
		return locales.SignalRefused(se.Msg)
	}
	return locales.SignalFailed(describeProcError(err))
}

// signalDialogs draws the signal dialog while it is open, as a modal of
// the main window: dialogs calls it.
func (a *App) signalDialogs(c *ui.Context) {
	d := a.processes.dlg
	if d == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() { a.signalDialogView(c, d) })
	// Closed, or its session is gone.
	if !open || d.s.status != statusConnected || !slices.Contains(a.sessions, d.s) {
		if a.processes.dlg == d {
			a.closeSignalDialog()
		}
	}
}

func (a *App) signalDialogView(c *ui.Context, d *signalDialog) {
	ui.Column(c).Width(420).Gap(12).Children(func() {
		var title, body, ok string
		switch {
		case d.still:
			title, body, ok = locales.KillStillTitle(d.name, strconv.Itoa(d.pid)), locales.KillStillBody(), locales.ForceKill()
		case d.kill:
			title, body, ok = locales.KillTitle(d.name, strconv.Itoa(d.pid)), locales.KillBody(int(killWait/time.Second)), locales.ProcKillButton()
		default:
			title, body, ok = locales.SignalTitle("SIG"+d.sig, d.name, strconv.Itoa(d.pid)), signalText(d.sig), locales.SignalSend()
		}
		ui.Text(c, title).FontSize(16).Bold()
		ui.Text(c, body)
		if d.err != nil {
			ui.Text(c, signalErrorText(d.err)).TextColor(theme.Danger).MaxLines(3)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if d.busy {
				ui.Spinner(c)
				if d.waiting {
					widgets.Muted(c, locales.KillWaiting())
				}
			}
			ui.Spacer(c)
			if ui.Button(c, locales.Cancel()).Clicked() {
				a.closeSignalDialog()
			}
			if ui.PrimaryButton(c, ok).Disabled(d.busy).Clicked() && a.processes.dlg == d {
				a.sendSignal(d)
			}
		})
	})
}
