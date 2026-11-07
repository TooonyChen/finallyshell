package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"finalshelly/internal/config"
	"finalshelly/internal/icons"
	"finalshelly/internal/monitor"
	"finalshelly/internal/widgets"
	"finalshelly/locales"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// sysInfoTimeout is how long the popover waits for the server.
const sysInfoTimeout = 20 * time.Second

// sysInfoPopover is the System info popover below the IP. There is one,
// for the session shown; each open reads the server again.
type sysInfoPopover struct {
	open    bool
	s       *Session // the session it shows
	gen     int      // counts the reads; a result of an older one is dropped
	loading bool
	info    monitor.SysInfo
	err     error
	off     bool // the host allows no exec channel
	cancel  context.CancelFunc
}

// execAllowed tells whether the app may open exec channels to h's server
// besides the shell and SFTP: the server monitor and the system info.
// It is the one place that decides, by the host's Monitor switch.
func execAllowed(h *config.Host) bool { return h.MonitorEnabled() }

var errNoClient = errors.New("not connected")

// sysInfoReader reads a server's system info over client.
type sysInfoReader func(ctx context.Context, client *ssh.Client) (monitor.SysInfo, error)

// readSysInfo reads what the popover shows, unless App.readSysInfo
// stands in for it, as in tests.
func readSysInfo(ctx context.Context, client *ssh.Client) (monitor.SysInfo, error) {
	if client == nil {
		return monitor.SysInfo{}, errNoClient
	}
	return monitor.ReadSysInfo(ctx, client)
}

// openSysInfo opens the popover for s and starts reading the server on a
// goroutine, when its host allows it.
func (a *App) openSysInfo(s *Session) {
	a.closeSysInfo()
	p := &a.sysInfo
	p.open, p.s = true, s
	p.info, p.err, p.loading, p.off = monitor.SysInfo{}, nil, false, false
	if !execAllowed(s.Host) {
		p.off = true
		return
	}
	if s.status != statusConnected {
		return
	}
	read := a.readSysInfo
	if read == nil {
		read = readSysInfo
	}
	ctx, cancel := context.WithTimeout(context.Background(), sysInfoTimeout)
	p.loading, p.cancel = true, cancel
	gen, client := p.gen, s.client
	go func() {
		info, err := read(ctx, client)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = context.DeadlineExceeded
		}
		cancel()
		a.post(func() {
			// Closed, opened again, or showing another session since.
			if p.gen != gen || p.s != s {
				return
			}
			p.loading, p.info, p.err, p.cancel = false, info, err, nil
		})
	}()
}

// closeSysInfo closes the popover, dropping the read under way.
func (a *App) closeSysInfo() {
	p := &a.sysInfo
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.open, p.loading, p.s = false, false, nil
	p.gen++
}

// sysInfoButton is the button under the IP, with its popover; it is
// disabled while s is nil or not connected.
func (a *App) sysInfoButton(c *ui.Context, s *Session) {
	p := &a.sysInfo
	// Another session shown, or this one closed: the popover was its.
	if p.open && (s == nil || p.s != s) {
		a.closeSysInfo()
	}
	enabled := s != nil && s.status == statusConnected
	b := ui.Button(c, locales.SystemInfo()).FontSize(12).Padding(2, 10).AlignSelf(ui.Start).Disabled(!enabled)
	if b.Clicked() && enabled {
		if p.open {
			a.closeSysInfo()
		} else {
			a.openSysInfo(s)
		}
	}
	ui.Popover(c, b, &p.open, func() { a.sysInfoBody(c) })
	if !p.open && p.s != nil { // dismissed by a press outside or Escape
		a.closeSysInfo()
	}
}

func (a *App) sysInfoBody(c *ui.Context) {
	p := &a.sysInfo
	ui.Column(c).Width(400).Padding(8, 10, 10, 10).Gap(4).Children(func() {
		ui.Row(c).Gap(6).Margin(0, 0, 4, 0).Children(func() {
			ui.Text(c, locales.SystemInfo()).Bold().Grow(1)
			ready := !p.off && !p.loading && p.err == nil && p.s != nil && p.s.status == statusConnected
			cp := ui.Button(c, "").FontSize(12).Padding(2, 8).Disabled(!ready).Children(func() {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.Get("copy")).FontSize(12)
					ui.Text(c, locales.Copy()).FontSize(12)
				})
			}).Label(locales.Copy())
			if cp.Clicked() && ready {
				c.WriteClipboard(sysInfoSummary(p.info))
			}
		})
		switch {
		case p.off:
			widgets.Muted(c, locales.SysInfoOff())
		case p.loading:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(6, 0).Children(func() {
				ui.Spinner(c).Label(locales.ReadingServerInfo())
				widgets.Muted(c, locales.ReadingServerInfo())
			})
		case p.err != nil:
			ui.Text(c, locales.SysInfoFailed(describeSysInfoError(p.err))).TextColor(theme.Danger)
			if p.s != nil && p.s.status == statusConnected && ui.Button(c, locales.Retry()).AlignSelf(ui.Start).Clicked() {
				a.openSysInfo(p.s)
			}
		case p.s == nil || p.s.status != statusConnected:
			widgets.Muted(c, locales.NotConnected())
		default:
			for i, r := range sysInfoRows(p.info) {
				ui.Row(c.Key(i)).Gap(10).AlignItems(ui.Start).Children(func() {
					widgets.Muted(c, r.label).Width(86).Shrink(0)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						for j, v := range r.values {
							ui.Text(c.Key(j), v).Selectable()
						}
					})
				})
			}
		}
	})
}

func describeSysInfoError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return locales.SysInfoTimeout()
	case errors.Is(err, monitor.ErrSysInfoOutput):
		return locales.SysInfoNoShell()
	case errors.Is(err, errNoClient):
		return locales.NotConnected()
	}
	return describeError(err)
}

// sysInfoRow is a line of the popover: a label and one or more values.
type sysInfoRow struct {
	label  string
	values []string
}

func sysInfoRows(info monitor.SysInfo) []sysInfoRow {
	or := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	cpu := []string{or(info.CPUModel)}
	switch {
	case info.Cores > 0 && info.Cores != info.CPUs && info.CPUs > 0:
		cpu = append(cpu, locales.SysCoresThreads(info.Cores, info.CPUs))
	case info.CPUs > 0:
		cpu = append(cpu, locales.SysCores(info.CPUs))
	}
	mem := "-"
	if info.MemTotal > 0 {
		mem = formatSize(int64(info.MemTotal))
	}
	swap := "-"
	if info.SwapTotal > 0 {
		swap = formatSize(int64(info.SwapTotal))
	}
	uptime := "-"
	if info.Uptime > 0 {
		days := int(info.Uptime / (24 * time.Hour))
		rest := info.Uptime % (24 * time.Hour)
		uptime = locales.SysUptimeValue(days, fmt.Sprintf("%d:%02d", int(rest/time.Hour), int(rest%time.Hour/time.Minute)))
	}
	var addrs []string
	for _, ad := range info.Addrs {
		addrs = append(addrs, ad.Iface+"  "+ad.Addr)
	}
	var disks []string
	for _, d := range info.Disks {
		line := d.Name + "  " + formatSize(int64(d.Size))
		if d.Model != "" {
			line += "  " + d.Model
		}
		disks = append(disks, line)
	}
	list := func(l []string) []string {
		if len(l) == 0 {
			return []string{"-"}
		}
		return l
	}
	return []sysInfoRow{
		{locales.SysHostname(), []string{or(info.Hostname)}},
		{locales.SysOS(), []string{or(info.OS)}},
		{locales.SysKernel(), []string{or(info.Kernel)}},
		{locales.SysArch(), []string{or(info.Arch)}},
		{locales.SysCPU(), cpu},
		{locales.SysMemory(), []string{mem}},
		{locales.Swap(), []string{swap}},
		{locales.SysUptime(), []string{uptime}},
		{locales.SysAddresses(), list(addrs)},
		{locales.SysDisks(), list(disks)},
	}
}

// sysInfoSummary is what Copy puts on the clipboard: the popover's rows
// as plain text, a list's items on lines of their own.
func sysInfoSummary(info monitor.SysInfo) string {
	var b strings.Builder
	for _, r := range sysInfoRows(info) {
		if len(r.values) == 1 {
			fmt.Fprintf(&b, "%s: %s\n", r.label, r.values[0])
			continue
		}
		fmt.Fprintf(&b, "%s:\n", r.label)
		for _, v := range r.values {
			fmt.Fprintf(&b, "  %s\n", v)
		}
	}
	return b.String()
}
