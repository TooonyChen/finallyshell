package app

import (
	"slices"
	"strings"

	"finallyshell/internal/config"
	"finallyshell/internal/icons"
	"finallyshell/internal/monitor"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// netPicker is the side panel's choice of network interfaces for a
// session's host: those its graph shows, and what choosing others does.
// The zero netPicker, as the skeleton has, shows no menu.
type netPicker struct {
	chosen []string
	choose func(names []string)
}

func (a *App) netPicker(s *Session) netPicker {
	if s == nil || s.Host == nil {
		return netPicker{}
	}
	h := s.Host
	return netPicker{chosen: h.MonitorIfaces, choose: func(names []string) { a.setMonitorIfaces(h, names) }}
}

// setMonitorIfaces makes the graph of h's sessions show names, none for
// the busiest interface, and saves it with the host.
func (a *App) setMonitorIfaces(h *config.Host, names []string) {
	if len(names) == 0 {
		names = nil
	}
	h.MonitorIfaces = names
	if saved := a.hosts.Host(h.ID); saved != nil {
		if saved != h {
			saved.MonitorIfaces = slices.Clone(names)
		}
		a.saveHosts(a.hosts)
	}
}

// netGraph is the network row of the side panel, its speeds and the
// interfaces' menu, and its graph: of the interfaces p chose, summed, or
// of the busiest.
func netGraph(c *ui.Context, st *monitor.Stats, p netPicker) {
	v := st.Net(p.chosen)
	label, tip := netLabel(v)
	var menu func(m *ui.Menu)
	if p.choose != nil {
		menu = func(m *ui.Menu) { netMenu(m, st, v, p) }
	}
	netRow(c, v.TxRate, v.RxRate, label, tip, menu)
	widgets.Sparkline(c, monitor.HistoryLen, [][]float64{v.TxHist, v.RxHist}, []ui.Color{txChart, rxChart}, formatBytes)
}

// netLabel is what the graph's top right says it shows, and its tooltip:
// the interfaces, joined with "+", and those chosen that are missing.
func netLabel(v monitor.NetView) (label, tip string) {
	label = strings.Join(v.Names, " + ")
	switch {
	case v.Auto && len(v.Missing) > 0:
		if label == "" {
			label = locales.NetIfaceAuto()
		} else {
			label = locales.NetIfaceFallback(label)
		}
		tip = locales.NetIfacesMissing(strings.Join(v.Missing, ", "))
	case len(v.Missing) > 0:
		tip = locales.NetIfacesPartlyMissing(strings.Join(v.Missing, ", "))
	case label == "":
		label = locales.NetIfaceAuto()
	}
	return label, tip
}

// netMenu is the interfaces' menu: Automatic, every interface with its
// speeds, then those chosen that the server does not have, so that they
// can be unchecked.
func netMenu(m *ui.Menu, st *monitor.Stats, v monitor.NetView, p netPicker) {
	m.MaxWidth(320)
	auto := m.Item(locales.NetIfaceAutomatic()).ID("auto").Checked(len(p.chosen) == 0)
	if st.Iface != "" {
		auto.Trailing(st.Iface)
	}
	if auto.Chosen() {
		p.choose(nil)
	}
	m.Separator()
	for _, it := range st.Ifaces {
		item := m.Item(it.Name).ID("if:" + it.Name).Checked(slices.Contains(p.chosen, it.Name)).
			Trailing("↑" + formatBytes(it.TxRate) + "  ↓" + formatBytes(it.RxRate))
		if item.Chosen() {
			p.choose(toggleIface(p.chosen, it.Name))
		}
	}
	for _, name := range v.Missing {
		if m.Item(name).ID("if:" + name).Checked(true).Trailing(locales.NetIfaceNotFound()).Chosen() {
			p.choose(toggleIface(p.chosen, name))
		}
	}
}

// toggleIface is chosen with name added, or taken out when it is there.
func toggleIface(chosen []string, name string) []string {
	if slices.Contains(chosen, name) {
		return slices.DeleteFunc(slices.Clone(chosen), func(n string) bool { return n == name })
	}
	return append(slices.Clone(chosen), name)
}

// netLabelButton is the label at the graph's top right as the button
// that opens the interfaces' menu.
func netLabelButton(c *ui.Context, label, tip string, menu func(m *ui.Menu)) ui.Element {
	b := ui.ButtonBase(c).Label(locales.NetIfaces()).Gap(2).PaddingX(4).Radius(4).Shrink(1).MinWidth(0)
	if b.Hovered() {
		b.Background(colors.line)
	}
	if tip == "" {
		tip = label
	}
	b.Tooltip(tip)
	b.Children(func() {
		widgets.Muted(c, label).SingleLine().Shrink(1).MinWidth(0)
		ui.Icon(c, icons.Get("chevron-down")).FontSize(12).Shrink(0).TextColor(c.Theme().TextMuted)
	})
	b.PopoverMenuButton(menu, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
	return b
}
