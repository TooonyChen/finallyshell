package app

import (
	"cmp"
	"context"
	"errors"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"finallyshell/internal/geo"
	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// netRefresh is how often the Network tab reads the sockets while it
// shows.
const netRefresh = 2 * time.Second

// netConnLimit is the most connections a row's tab shows.
const netConnLimit = 5000

// Where the divider of the Network tab starts: the table's share above
// the tabs.
const defaultNetListPct = 55

var netListLimits = splitLimits{first: 90, second: 90, vertical: true, percent: true}

// networkAPI is what the Network tab asks of the server; tests stand in
// for it.
type networkAPI interface {
	list(ctx context.Context, client *ssh.Client, l *monitor.NetLister) (monitor.NetSnapshot, error)
	trace(ctx context.Context, client *ssh.Client, target, lang string, update func(monitor.TraceResult)) monitor.TraceResult
}

// serverNetwork is the networkAPI of a real server.
type serverNetwork struct{}

func (serverNetwork) list(ctx context.Context, client *ssh.Client, l *monitor.NetLister) (monitor.NetSnapshot, error) {
	return l.List(ctx, client)
}

func (serverNetwork) trace(ctx context.Context, client *ssh.Client, target, lang string, update func(monitor.TraceResult)) monitor.TraceResult {
	return monitor.Trace(ctx, client, target, lang, update)
}

// networkUI is the Network tab's state shared by the sessions: the size
// of its panes, the trace dialog, and what reads the server.
type networkUI struct {
	listPct float32
	dlg     *traceDialog
	api     networkAPI // serverNetwork when nil
}

func (a *App) netAPI() networkAPI {
	if a.network.api == nil {
		return serverNetwork{}
	}
	return a.network.api
}

// netState is a session's Network tab: the sockets, the table's filter,
// sort and choice, and the tabs opened below it.
type netState struct {
	poll   sessionPoll
	snap   monitor.NetSnapshot
	loaded bool
	err    error

	filter string
	table  ui.ListState
	sort   ui.SortOrder
	row    int
	key    string // the row chosen, which a refresh keeps

	view    []monitor.NetGroup
	viewOf  []monitor.NetGroup
	viewFor string
	viewBy  ui.SortOrder

	tabs []*netTab
	tab  int
}

// netTab is a tab below the table: a row's connections, or a trace.
type netTab struct {
	key   string // the row's, for connections
	title string
	trace *traceRun // for a trace

	table ui.ListState
	sort  ui.SortOrder
	row   int
	sel   netip.AddrPort // the connection chosen

	view    []monitor.Socket
	viewOf  []monitor.Socket
	viewBy  ui.SortOrder
	viewLoc bool
}

// label names the tab: the row's title, or the trace's target.
func (t *netTab) label() string {
	if t.trace != nil {
		return locales.TraceTab(t.trace.target)
	}
	return t.title
}

// pollNetwork keeps s's sockets read while the tab shows them.
func (a *App) pollNetwork(s *Session) {
	ns := &s.net
	api := a.netAPI()
	polling := a.keepPolling(&ns.poll, s, netRefresh, func() pollRead {
		var lister monitor.NetLister
		return func(ctx context.Context, client *ssh.Client) func() {
			snap, err := api.list(ctx, client, &lister)
			return func() {
				ns.err = err
				if err == nil {
					ns.snap, ns.loaded = snap, true
					a.noteIPv6(snap)
				}
			}
		}
	})
	if !polling && s.status == statusConnecting {
		ns.loaded = false
	}
}

// noteIPv6 notes an IPv6 address to locate, for the question about the
// IPv6 data.
func (a *App) noteIPv6(snap monitor.NetSnapshot) {
	if a.geo.sawV6 || !a.locationsShown() || a.geo.db[1] != nil {
		return
	}
	for _, g := range snap.Groups {
		for _, c := range g.Conns {
			if ip := c.Remote.Addr(); ip.Is6() && !ip.Is4In6() && !geo.IsLAN(ip) {
				a.geo.sawV6 = true
				return
			}
		}
	}
}

// netColumns are the table's columns; their IDs are the sort's.
func netColumns() []ui.TableColumn {
	return []ui.TableColumn{
		{ID: "pid", Title: locales.ColPID(), Width: 70, Align: ui.End, Sortable: true},
		{ID: "name", Title: locales.ColProcName(), Width: 130, Sortable: true},
		{ID: "listen", Title: locales.ColNetListen(), Width: 130, Sortable: true},
		{ID: "port", Title: locales.ColNetPort(), Width: 64, Align: ui.End, Sortable: true},
		{ID: "proto", Title: locales.ColNetProto(), Width: 70, Sortable: true},
		{ID: "ips", Title: locales.ColNetRemoteIPs(), Width: 84, Align: ui.End, Sortable: true},
		{ID: "conns", Title: locales.ColNetConns(), Width: 96, Align: ui.End, Sortable: true},
		{ID: "uprate", Title: locales.ColNetUpRate(), Width: 92, Align: ui.End, Sortable: true},
		{ID: "downrate", Title: locales.ColNetDownRate(), Width: 92, Align: ui.End, Sortable: true},
		{ID: "up", Title: locales.ColNetUp(), Width: 92, Align: ui.End, Sortable: true},
		{ID: "down", Title: locales.ColNetDown(), Width: 92, Align: ui.End, Sortable: true},
	}
}

// filterNet is the rows whose name, PID, address or port, or one of
// whose connections' remote addresses, holds query, sorted as by says.
func filterNet(groups []monitor.NetGroup, query string, by ui.SortOrder) []monitor.NetGroup {
	query = strings.ToLower(strings.TrimSpace(query))
	var out []monitor.NetGroup
	for _, g := range groups {
		if query == "" || netGroupMatches(g, query) {
			out = append(out, g)
		}
	}
	slices.SortStableFunc(out, func(x, y monitor.NetGroup) int {
		var r int
		switch by.Column {
		case "pid":
			r = cmp.Compare(x.PID, y.PID)
		case "name":
			r = cmp.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		case "listen":
			r = cmp.Compare(netListen(x), netListen(y))
		case "port":
			r = cmp.Compare(netPort(x), netPort(y))
		case "proto":
			r = cmp.Compare(x.Proto, y.Proto)
		case "ips":
			r = cmp.Compare(x.RemoteIPs, y.RemoteIPs)
		case "conns":
			r = cmp.Compare(len(x.Conns), len(y.Conns))
		case "uprate":
			r = cmp.Compare(x.SendRate, y.SendRate)
		case "downrate":
			r = cmp.Compare(x.RecvRate, y.RecvRate)
		case "up":
			r = cmp.Compare(x.Sent, y.Sent)
		case "down":
			r = cmp.Compare(x.Received, y.Received)
		}
		if by.Descending {
			r = -r
		}
		return r // equal rows keep the server's order: by port, then PID
	})
	return out
}

func netGroupMatches(g monitor.NetGroup, q string) bool {
	if strings.Contains(strings.ToLower(g.Name), q) || strings.Contains(strconv.Itoa(g.PID), q) ||
		g.Listen && strings.Contains(g.Local.String(), q) {
		return true
	}
	for _, c := range g.Conns {
		if strings.Contains(c.Remote.String(), q) {
			return true
		}
	}
	return false
}

func netListen(g monitor.NetGroup) string {
	if !g.Listen {
		return ""
	}
	return g.Local.Addr().String()
}

func netPort(g monitor.NetGroup) int {
	if !g.Listen {
		return -1
	}
	return int(g.Local.Port())
}

func netProto(p string) string { return strings.ToUpper(strings.ReplaceAll(p, "+", "/")) }

// shown is the rows the table shows, made again when the sockets, the
// filter or the sort changed.
func (ns *netState) shown() []monitor.NetGroup {
	groups := ns.snap.Groups
	same := len(ns.viewOf) == len(groups) && (len(groups) == 0 || &ns.viewOf[0] == &groups[0])
	if !same || ns.viewFor != ns.filter || ns.viewBy != ns.sort {
		ns.view = filterNet(groups, ns.filter, ns.sort)
		ns.viewOf, ns.viewFor, ns.viewBy = groups, ns.filter, ns.sort
	}
	return ns.view
}

// group is the row of key in the last read, if it is there.
func (ns *netState) group(key string) (monitor.NetGroup, bool) {
	for _, g := range ns.snap.Groups {
		if g.Key() == key {
			return g, true
		}
	}
	return monitor.NetGroup{}, false
}

// networkPanel is the "Network" tab: the active session's listening
// sockets and processes with connections, and the tabs opened below.
func (a *App) networkPanel(c *ui.Context) {
	s := a.connSession()
	if s == nil || s.isNewTab() {
		return
	}
	if s.isLocal() {
		localUnavailable(c)
		return
	}
	a.pollNetwork(s)
	a.geoTick()
	ns := &s.net
	ui.Column(c.Key(s.ID)).Grow(1).MinHeight(0).Children(func() {
		if s.status != statusConnected {
			a.statusBanner(c, s)
		}
		a.netToolbar(c, s)
		a.geoBar(c)
		if len(ns.tabs) == 0 {
			a.netList(c, s)
			return
		}
		split := ui.SplitVerticalPercent(c, &a.network.listPct, func() { a.netList(c, s) }, func() { a.netTabs(c, s) }).
			SplitReset(defaultNetListPct).Grow(1)
		a.splitMoved(c, split, &a.network.listPct, netListLimits)
	})
}

func (a *App) netToolbar(c *ui.Context, s *Session) {
	ns := &s.net
	ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
		ui.SearchField(c, &ns.filter).Placeholder(locales.NetFilter()).Label(locales.NetFilter()).Width(320).Shrink(1).MinWidth(120)
		switch {
		case !execAllowed(s.Host):
			widgets.Muted(c, locales.MonitorOff())
		case errors.Is(ns.err, monitor.ErrNotLinux):
			widgets.Muted(c, locales.ProcOnlyLinux())
		case ns.err != nil:
			msg := locales.NetListFailed(describeProcError(ns.err))
			ui.Text(c, msg).TextColor(theme.Danger).SingleLine().Shrink(1).MinWidth(0).Tooltip(msg)
		case s.status == statusConnected && !ns.loaded:
			ui.Spinner(c).Label(locales.ProcReading())
			widgets.Muted(c, locales.ProcReading())
		case ns.loaded:
			n, shown := len(ns.snap.Groups), len(ns.shown())
			text := locales.NetCount(n)
			if shown != n {
				text = locales.NetCountFiltered(shown, n)
			}
			widgets.Muted(c, text).SingleLine()
			if ns.snap.Unknown > 0 && !ns.snap.Root {
				widgets.Muted(c, "· "+locales.NetNeedsRoot()).SingleLine().Shrink(1).MinWidth(0)
			}
		}
		ui.Spacer(c)
		if execAllowed(s.Host) && widgets.IconButton(c, "route", locales.TraceRouteTo()).Clicked() && canTrace(s) {
			a.openTraceDialog(s, "")
		}
		if widgets.IconButton(c, "refresh-cw", locales.Refresh()).Clicked() {
			ns.poll.pollNow()
		}
	})
}

func (a *App) netList(c *ui.Context, s *Session) {
	ns := &s.net
	view := ns.shown()
	if errors.Is(ns.err, monitor.ErrNotLinux) {
		view = nil
	}
	ns.row = slices.IndexFunc(view, func(g monitor.NetGroup) bool { return g.Key() == ns.key })
	ns.table.Selected = &ns.row
	ns.table.Sort = &ns.sort
	ns.table.KeepOffset = true
	ns.table.Key = func(i int) any { return view[i].Key() }
	ns.table.Label = func(i int) string { return view[i].Title() }
	ns.table.CellMenu = func(m *ui.Menu, row, col int) {
		if row >= 0 && row < len(view) {
			ns.key = view[row].Key()
			a.netMenu(c, m, s, view[row])
		}
	}
	connected := s.status == statusConnected
	t := ui.Table(c, &ns.table, netColumns(), len(view), func(row, col int) {
		g := view[row]
		text := ""
		switch col {
		case 0:
			text = netPID(g.PID)
		case 1:
			text = orDash(g.Name)
		case 2:
			text = "-"
			if g.Listen {
				text = g.Local.Addr().String()
			}
		case 3:
			text = "-"
			if g.Listen {
				text = strconv.Itoa(int(g.Local.Port()))
			}
		case 4:
			text = netProto(g.Proto)
		case 5:
			text = netCount(g, g.RemoteIPs)
		case 6:
			text = netCount(g, len(g.Conns))
		case 7:
			text = netRate(g.HasBytes, g.SendRate)
		case 8:
			text = netRate(g.HasBytes, g.RecvRate)
		case 9:
			text = netBytes(g.HasBytes, g.Sent)
		case 10:
			text = netBytes(g.HasBytes, g.Received)
		}
		ui.Text(c, text).SingleLine()
	}).Grow(1).Label(locales.TabNetwork())
	if !connected {
		t.Opacity(0.5) // the last read, from before the connection ended
	}
	if ns.row >= 0 && ns.row < len(view) {
		ns.key = view[ns.row].Key()
	}
	if t.Submitted() && ns.row >= 0 && ns.row < len(view) {
		a.openNetTab(s, view[ns.row])
	}
}

func netPID(pid int) string {
	if pid == 0 {
		return "-"
	}
	return strconv.Itoa(pid)
}

// netCount is a count of a row's: "-" for a UDP socket, which has no
// connections.
func netCount(g monitor.NetGroup, n int) string {
	if g.Listen && g.Proto == "udp" {
		return "-"
	}
	return strconv.Itoa(n)
}

func netRate(known bool, r float64) string {
	if !known {
		return "-"
	}
	return formatSize(int64(r)) + "/s"
}

func netBytes(known bool, n uint64) string {
	if !known {
		return "-"
	}
	return formatSize(int64(n))
}

// netMenu is the context menu of a row of the table.
func (a *App) netMenu(c *ui.Context, m *ui.Menu, s *Session, g monitor.NetGroup) {
	if m.Item(locales.NetOpenConns()).Chosen() {
		a.openNetTab(s, g)
	}
	m.Separator()
	if g.PID != 0 && m.Item(locales.ProcCopyPID()).Chosen() {
		c.WriteClipboard(strconv.Itoa(g.PID))
	}
	if g.Listen && m.Item(locales.NetCopyAddress()).Chosen() {
		c.WriteClipboard(g.Local.String())
	}
	m.Separator()
	if m.Item(locales.Refresh()).Chosen() {
		s.net.poll.pollNow()
	}
}

// openNetTab opens g's connections in a tab below the table, or chooses
// its tab when it is open.
func (a *App) openNetTab(s *Session, g monitor.NetGroup) {
	ns := &s.net
	key := g.Key()
	if i := slices.IndexFunc(ns.tabs, func(t *netTab) bool { return t.trace == nil && t.key == key }); i >= 0 {
		ns.tab = i
		return
	}
	ns.tabs = append(ns.tabs, &netTab{key: key, title: g.Title(), row: -1})
	ns.tab = len(ns.tabs) - 1
}

func (a *App) closeNetTab(s *Session, i int) {
	ns := &s.net
	if i < 0 || i >= len(ns.tabs) {
		return
	}
	if t := ns.tabs[i]; t.trace != nil {
		t.trace.stop()
	}
	ns.tabs = slices.Delete(ns.tabs, i, i+1)
	if ns.tab > i || ns.tab >= len(ns.tabs) {
		ns.tab--
	}
	ns.tab = max(ns.tab, 0)
}

// netTabs is the strip of the tabs opened, and the chosen one.
func (a *App) netTabs(c *ui.Context, s *Session) {
	ns := &s.net
	ns.tab = min(max(ns.tab, 0), len(ns.tabs)-1)
	closed := -1
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Background(colors.sidebar).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).PaddingX(4).Shrink(0).Children(func() {
			tabs := ui.TabsBase(c, &ns.tab, len(ns.tabs))
			tabs.List.Grow(1).MinWidth(0).Clip().Children(func() {
				for i, t := range ns.tabs {
					tab := tabs.Tab(i).Row().Gap(6).AlignItems(ui.Center).Padding(5, 6, 4, 10).
						MinWidth(0).Shrink(1).Tooltip(t.label()).BorderWidth(0, 1, 0, 0).BorderColor(colors.line)
					selected := i == ns.tab
					if selected {
						tab.Background(theme.Background)
					}
					hovered := tab.Hovered()
					tab.Children(func() {
						if selected {
							ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(2).Background(theme.Accent)
						}
						name := ui.Text(c, t.label()).SingleLine().Shrink(1).MinWidth(0).FontSize(12.5)
						if !selected {
							name.TextColor(theme.TextMuted)
						}
						if procTabClose(c, hovered || selected) {
							closed = i
						}
					})
				}
			})
			if t := ns.tabs[ns.tab]; t.trace != nil {
				a.traceTools(c, s, t)
			} else {
				a.connTools(c, s, t)
			}
		})
		if t := ns.tabs[ns.tab]; t.trace != nil {
			a.traceView(c, s, t)
		} else {
			a.connView(c, s, t)
		}
	})
	if closed >= 0 {
		a.closeNetTab(s, closed)
	}
}

// connColumns are the columns of a row's connections; the Location
// column only while the location data is on.
func (a *App) connColumns() []ui.TableColumn {
	var cols []ui.TableColumn
	if a.locationsShown() {
		cols = append(cols, ui.TableColumn{ID: "loc", Title: locales.ColNetLocation(), Width: 220, Sortable: true})
	}
	return append(cols,
		ui.TableColumn{ID: "ip", Title: locales.ColNetIP(), Width: 170, Sortable: true},
		ui.TableColumn{ID: "port", Title: locales.ColNetPort(), Width: 64, Align: ui.End, Sortable: true},
		ui.TableColumn{ID: "state", Title: locales.ColNetState(), Width: 92, Sortable: true},
		ui.TableColumn{ID: "uprate", Title: locales.ColNetUpRate(), Width: 92, Align: ui.End, Sortable: true},
		ui.TableColumn{ID: "downrate", Title: locales.ColNetDownRate(), Width: 92, Align: ui.End, Sortable: true},
		ui.TableColumn{ID: "up", Title: locales.ColNetUp(), Width: 92, Align: ui.End, Sortable: true},
		ui.TableColumn{ID: "down", Title: locales.ColNetDown(), Width: 92, Align: ui.End, Sortable: true},
	)
}

// conns is the tab's connections, sorted, made again when they or the
// sort changed.
func (a *App) conns(t *netTab, g monitor.NetGroup) []monitor.Socket {
	loc := a.locationsShown()
	same := len(t.viewOf) == len(g.Conns) && (len(g.Conns) == 0 || &t.viewOf[0] == &g.Conns[0])
	if same && t.viewBy == t.sort && t.viewLoc == loc {
		return t.view
	}
	view := slices.Clone(g.Conns)
	by := t.sort
	places := map[netip.Addr]string{}
	place := func(ip netip.Addr) string {
		p, ok := places[ip]
		if !ok {
			p = a.placeOf(ip, false)
			places[ip] = p
		}
		return p
	}
	slices.SortStableFunc(view, func(x, y monitor.Socket) int {
		var r int
		switch by.Column {
		case "loc":
			r = cmp.Compare(place(x.Remote.Addr()), place(y.Remote.Addr()))
		case "ip":
			r = x.Remote.Addr().Compare(y.Remote.Addr())
		case "port":
			r = cmp.Compare(x.Remote.Port(), y.Remote.Port())
		case "state":
			r = cmp.Compare(x.State, y.State)
		case "uprate":
			r = cmp.Compare(x.SendRate, y.SendRate)
		case "downrate":
			r = cmp.Compare(x.RecvRate, y.RecvRate)
		case "up":
			r = cmp.Compare(x.Sent, y.Sent)
		case "down":
			r = cmp.Compare(x.Received, y.Received)
		}
		if by.Descending {
			r = -r
		}
		return r
	})
	t.view, t.viewOf, t.viewBy, t.viewLoc = view, g.Conns, by, loc
	return view
}

// connTools are the count of a row's connections, at the end of the tab
// strip.
func (a *App) connTools(c *ui.Context, s *Session, t *netTab) {
	g, _ := s.net.group(t.key)
	ui.Row(c.Key(t)).Gap(4).AlignItems(ui.Center).PaddingX(6).Shrink(0).Children(func() {
		n := len(g.Conns)
		text := locales.NetConnCount(n)
		if n > netConnLimit {
			text = locales.NetConnCountLimited(netConnLimit, n)
		}
		widgets.Muted(c, text).FontSize(12).SingleLine()
	})
}

// connView is the connections of a row: those to a listening socket, or
// those a process opened.
func (a *App) connView(c *ui.Context, s *Session, t *netTab) {
	g, _ := s.net.group(t.key)
	view := a.conns(t, g)
	if len(view) > netConnLimit {
		view = view[:netConnLimit]
	}
	cols := a.connColumns()
	t.row = slices.IndexFunc(view, func(x monitor.Socket) bool { return x.Remote == t.sel })
	t.table.Selected = &t.row
	t.table.Sort = &t.sort
	t.table.KeepOffset = true
	t.table.Key = func(i int) any { return view[i].Local.String() + " " + view[i].Remote.String() }
	t.table.Label = func(i int) string { return view[i].Remote.String() }
	t.table.CellMenu = func(m *ui.Menu, row, col int) {
		if row >= 0 && row < len(view) {
			t.sel = view[row].Remote
			a.connMenu(c, m, s, view[row])
		}
	}
	tbl := ui.Table(c.Key(t), &t.table, cols, len(view), func(row, col int) {
		x := view[row]
		var text string
		switch cols[col].ID {
		case "loc":
			text = orDash(a.placeOf(x.Remote.Addr(), true))
		case "ip":
			text = x.Remote.Addr().String()
		case "port":
			text = strconv.Itoa(int(x.Remote.Port()))
		case "state":
			text = x.State
		case "uprate":
			text = netRate(x.HasBytes, x.SendRate)
		case "downrate":
			text = netRate(x.HasBytes, x.RecvRate)
		case "up":
			text = netBytes(x.HasBytes, x.Sent)
		case "down":
			text = netBytes(x.HasBytes, x.Received)
		}
		ui.Text(c, text).SingleLine().Tooltip(text)
	}).Grow(1).Label(t.title)
	if s.status != statusConnected {
		tbl.Opacity(0.5)
	}
	if t.row >= 0 && t.row < len(view) {
		t.sel = view[t.row].Remote
	}
}

// connMenu is the context menu of a connection.
func (a *App) connMenu(c *ui.Context, m *ui.Menu, s *Session, x monitor.Socket) {
	ip := x.Remote.Addr()
	if m.Item(locales.NetCopyIP()).Chosen() {
		c.WriteClipboard(ip.String())
	}
	if m.Item(locales.NetCopyAddress()).Chosen() {
		c.WriteClipboard(x.Remote.String())
	}
	m.Separator()
	if m.Item(locales.TraceRoute()).Disabled(!canTrace(s)).Chosen() && canTrace(s) {
		a.startTrace(s, ip.String())
	}
	if a.settings.GeoOnline && a.locationsShown() && !geo.IsLAN(ip) && m.Item(locales.GeoLookUpOnline()).Chosen() {
		a.lookUpOnline(ip)
	}
}

// traceLang is the language of NextTrace's locations for the app's.
func traceLang() string {
	if strings.HasPrefix(i18n.Locale(), "zh") {
		return "cn"
	}
	return "en"
}
