package app

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"finallyshell/internal/icons"
	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// traceRun is a route traced on the server, in a tab of the Network
// tab: with NextTrace, or traceroute or tracepath when it is not there.
type traceRun struct {
	target  string
	r       monitor.TraceResult
	running bool
	gen     int
	cancel  context.CancelFunc

	table ui.ListState
	row   int
}

func (t *traceRun) stop() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.gen++
	t.running = false
}

// startTrace traces the route to target in a tab of its own, or runs
// the tab's trace again when it has one.
func (a *App) startTrace(s *Session, target string) {
	ns := &s.net
	i := slices.IndexFunc(ns.tabs, func(t *netTab) bool { return t.trace != nil && t.trace.target == target })
	if i < 0 {
		ns.tabs = append(ns.tabs, &netTab{trace: &traceRun{target: target, row: -1}})
		i = len(ns.tabs) - 1
	}
	ns.tab = i
	a.runTrace(s, ns.tabs[i].trace)
}

// runTrace traces t's route on a goroutine, the hops shown as they
// arrive.
func (a *App) runTrace(s *Session, t *traceRun) {
	t.stop()
	t.r = monitor.TraceResult{}
	if s.status != statusConnected {
		t.r = monitor.TraceResult{Done: true, Err: errNoClient}
		return
	}
	if !execAllowed(s.Host) {
		// The host asks for no extra exec channels.
		t.r = monitor.TraceResult{Done: true, Err: errMonitorOff}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	gen, client, api, lang := t.gen, s.client, a.netAPI(), traceLang()
	t.running, t.cancel = true, cancel
	go func() {
		defer cancel()
		api.trace(ctx, client, t.target, lang, func(r monitor.TraceResult) {
			r.Hops = slices.Clone(r.Hops)
			a.post(func() {
				if t.gen != gen {
					return
				}
				t.r = r
				if r.Done {
					t.running, t.cancel = false, nil
				}
			})
		})
	}()
}

// traceTools are the state of the chosen trace and its buttons, at the
// end of the tab strip: copying the route, and tracing it again.
func (a *App) traceTools(c *ui.Context, s *Session, t *netTab) {
	tr := t.trace
	ui.Row(c.Key(t)).Gap(4).AlignItems(ui.Center).PaddingX(4).Shrink(0).Children(func() {
		switch {
		case tr.running:
			ui.Spinner(c).Label(locales.Tracing())
			widgets.Muted(c, traceToolText(tr.r)).FontSize(12).SingleLine()
		case tr.r.Tool != "":
			widgets.Muted(c, traceToolText(tr.r)).FontSize(12).SingleLine()
		}
		if widgets.IconButton(c, "copy", locales.Copy()).Clicked() && len(tr.r.Hops) > 0 {
			c.WriteClipboard(a.traceText(tr))
		}
		if widgets.IconButton(c, "refresh-cw", locales.TraceAgain()).Clicked() && canTrace(s) {
			a.runTrace(s, tr)
		}
	})
}

// traceToolText names what traces the route, while it runs.
func traceToolText(r monitor.TraceResult) string {
	switch r.Tool {
	case monitor.ToolNextTrace:
		return "NextTrace"
	case monitor.ToolTraceroute, monitor.ToolTracepath:
		return r.Tool
	}
	return locales.Tracing()
}

// traceColumns are the columns of a route: the Location column when
// NextTrace locates the hops, or the location data is on; ASN with
// NextTrace.
func (a *App) traceColumns(r monitor.TraceResult) []ui.TableColumn {
	cols := []ui.TableColumn{
		{ID: "hop", Title: locales.ColTraceHop(), Width: 52, Align: ui.End},
		{ID: "ip", Title: locales.ColNetIP(), Width: 160},
		{ID: "rtt", Title: locales.ColTraceRTT(), Width: 90, Align: ui.End},
	}
	nt := r.Tool == monitor.ToolNextTrace
	if nt || a.locationsShown() {
		cols = append(cols, ui.TableColumn{ID: "loc", Title: locales.ColNetLocation(), MinWidth: 220})
	}
	if nt {
		// NextTrace looks the hops up; traceroute -n and tracepath -n
		// do not.
		cols = append(cols, ui.TableColumn{ID: "asn", Title: locales.ColTraceASN(), Width: 80},
			ui.TableColumn{ID: "host", Title: locales.ColTraceHost(), MinWidth: 160})
	}
	return cols
}

func traceRTT(h monitor.TraceHop) string {
	if len(h.RTTs) == 0 {
		return "*"
	}
	var sum time.Duration
	for _, d := range h.RTTs {
		sum += d
	}
	return fmt.Sprintf("%.2f ms", float64(sum)/float64(len(h.RTTs))/float64(time.Millisecond))
}

// hopPlace is where a hop is: NextTrace's answer, else the location
// data's.
func (a *App) hopPlace(r monitor.TraceResult, h monitor.TraceHop) string {
	if r.Tool == monitor.ToolNextTrace {
		return strings.Join(h.Where, " ")
	}
	if ip, err := netip.ParseAddr(h.IP); err == nil && a.locationsShown() {
		return a.placeOf(ip, true)
	}
	return ""
}

func (a *App) traceView(c *ui.Context, s *Session, t *netTab) {
	tr := t.trace
	r := tr.r
	ui.Column(c.Key(t)).Grow(1).MinHeight(0).Children(func() {
		if r.NoNextTrace {
			nextTraceMissing(c, r)
		}
		switch {
		case r.Done && r.Err != nil:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(8, 10).Children(func() {
				ui.Text(c, traceErrorText(r.Err)).TextColor(theme.Danger).Shrink(1).MinWidth(0)
				if canTrace(s) && !errors.Is(r.Err, monitor.ErrNoTraceTool) && ui.Button(c, locales.Retry()).Clicked() {
					a.runTrace(s, tr)
				}
			})
			if len(r.Hops) == 0 {
				return
			}
		case tr.running && len(r.Hops) == 0:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10).Children(func() {
				ui.Spinner(c).Label(locales.Tracing())
				widgets.Muted(c, locales.TracingTo(tr.target))
			})
			return
		}
		cols := a.traceColumns(r)
		hops := r.Hops
		tr.table.Selected = &tr.row
		tr.table.Label = func(i int) string { return strconv.Itoa(hops[i].TTL) + " " + hops[i].IP }
		tr.table.CellMenu = func(m *ui.Menu, row, col int) {
			if row < 0 || row >= len(hops) || hops[row].IP == "" {
				return
			}
			if m.Item(locales.NetCopyIP()).Chosen() {
				c.WriteClipboard(hops[row].IP)
			}
			m.Separator()
			if m.Item(locales.TraceRoute()).Disabled(!canTrace(s)).Chosen() && canTrace(s) {
				a.startTrace(s, hops[row].IP)
			}
		}
		ui.Table(c, &tr.table, cols, len(hops), func(row, col int) {
			h := hops[row]
			var text string
			switch cols[col].ID {
			case "hop":
				text = strconv.Itoa(h.TTL)
			case "ip":
				text = h.IP
				if text == "" {
					text = "*"
				}
			case "rtt":
				text = traceRTT(h)
			case "loc":
				text = a.hopPlace(r, h)
			case "asn":
				text = h.ASN
				if text != "" {
					text = "AS" + strings.TrimPrefix(strings.ToUpper(text), "AS")
				}
			case "host":
				text = h.Host
			}
			ui.Text(c, text).SingleLine().Tooltip(text)
		}).Grow(1).Label(tr.target)
	})
}

// nextTraceMissing says that NextTrace is not on the server, with the
// command of its README that installs it, which the app never runs.
func nextTraceMissing(c *ui.Context, r monitor.TraceResult) {
	ui.Row(c.Key("nexttrace")).Padding(5, 8, 5, 10).Gap(8).AlignItems(ui.Center).Background(colors.note).
		BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
		ui.Icon(c, icons.Get("triangle-alert")).FontSize(13).TextColor(theme.TextMuted).Shrink(0)
		text := locales.NextTraceMissing()
		if r.Tool != "" {
			text = locales.NextTraceMissingFallback(r.Tool)
		}
		ui.Text(c, text).SingleLine().Shrink(1).MinWidth(0).Tooltip(text)
		ui.Text(c, monitor.NextTraceInstall).Font("monospace").FontSize(12).Selectable().SingleLine().
			Padding(2, 6).Radius(4).Background(theme.Background).Shrink(1).MinWidth(0)
		if widgets.IconButton(c, "copy", locales.Copy()).Clicked() {
			c.WriteClipboard(monitor.NextTraceInstall)
		}
	})
}

func traceErrorText(err error) string {
	var te *monitor.TraceError
	switch {
	case errors.Is(err, monitor.ErrNoTraceTool):
		return locales.TraceNoTool()
	case errors.As(err, &te):
		return locales.TraceFailed(te.Msg)
	case errors.Is(err, errMonitorOff):
		return locales.MonitorOff()
	}
	return locales.TraceFailed(describeProcError(err))
}

// traceText is the route as text, for Copy.
func (a *App) traceText(t *traceRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", locales.TraceTab(t.target), traceToolText(t.r))
	for _, h := range t.r.Hops {
		ip := h.IP
		if ip == "" {
			ip = "*"
		}
		line := []string{strconv.Itoa(h.TTL), ip, traceRTT(h)}
		for _, s := range []string{a.hopPlace(t.r, h), h.ASN, h.Host} {
			if s != "" {
				line = append(line, s)
			}
		}
		b.WriteString(strings.Join(line, "\t") + "\n")
	}
	return b.String()
}

// errMonitorOff is a trace on a host whose server monitor is off, which
// opens no exec channels.
var errMonitorOff = errors.New("the server monitor is off for this host")

// canTrace says whether s can trace a route now.
func canTrace(s *Session) bool { return s.status == statusConnected && execAllowed(s.Host) }

// traceDialog asks for the address or host to trace the route to.
type traceDialog struct {
	s      *Session
	target string
	bad    bool // what was typed cannot be traced
}

func (a *App) openTraceDialog(s *Session, target string) {
	a.network.dlg = &traceDialog{s: s, target: target}
}

// traceDialogs draws the trace dialog while it is open, as a modal of
// the main window: dialogs calls it.
func (a *App) traceDialogs(c *ui.Context) {
	d := a.network.dlg
	if d == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(380).Gap(12).Children(func() {
			ui.Text(c, locales.TraceRouteTo()).FontSize(16).Bold()
			go1 := false
			f := ui.TextInput(c, &d.target).Placeholder(locales.TracePlaceholder()).Label(locales.TracePlaceholder()).AutoFocus()
			if f.Changed() {
				d.bad = false
			}
			if f.Submitted() {
				go1 = true
			}
			if d.bad {
				ui.Text(c, locales.TraceInvalid()).TextColor(theme.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.Cancel()).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c, locales.TraceRoute()).Clicked() {
					go1 = true
				}
			})
			if go1 {
				target := strings.TrimSpace(d.target)
				if !monitor.ValidTraceTarget(target) {
					d.bad = true
					return
				}
				open = false
				if canTrace(d.s) && slices.Contains(a.sessions, d.s) {
					a.startTrace(d.s, target)
				}
			}
		})
	})
	if !open || !slices.Contains(a.sessions, d.s) {
		if a.network.dlg == d {
			a.network.dlg = nil
		}
	}
}
