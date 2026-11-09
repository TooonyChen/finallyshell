package app

import (
	"slices"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// monitorMarks are, by part, a text only that part of the monitor shows:
// live, for the stats of demoSession, and in the skeleton.
func monitorMarks(s *Session) (live, skeleton map[string]string) {
	live = map[string]string{
		partUptime:    locales.UptimeDays(410),
		partCPU:       locales.Swap(),
		partProcesses: "mysqld",
		partNetwork:   "↑" + formatBytes(s.stats.TxRate),
		partLatency:   formatLatency(s.stats.Latency),
		partDisks:     locales.DiskPath(),
	}
	skeleton = map[string]string{
		partUptime:    locales.UptimeUnknown(),
		partCPU:       locales.Swap(),
		partProcesses: locales.Command(),
		partNetwork:   "↑" + formatBytes(0),
		partLatency:   locales.Latency(),
		partDisks:     locales.DiskPath(),
	}
	return live, skeleton
}

// monitorPartsApp is the main window in lang on a connected session, and
// that session.
func monitorPartsApp(t *testing.T, lang string) (*App, *ui.Tester, *Session) {
	t.Helper()
	a, p := testApp(t)
	p.SetLocale(lang)
	s := demoSession(a, "web-1", "45.56.78.90")
	a.active = 0
	// Menus in the window, so that their check boxes can be read, and
	// the screenshots show them.
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetMenuPresentation(ui.PopoverMenus)
		a.View(c)
	}, 1280, 820)
	tt.SetLocale(lang)
	p.tt = tt
	tt.Frame()
	return a, tt, s
}

// openMonitorMenu right-clicks the monitor panel, on its IP line.
func openMonitorMenu(t *testing.T, tt *ui.Tester, ip string) {
	t.Helper()
	if err := tt.RightClick("IP " + ip); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, p := range monitorParts {
		want = append(want, p.label())
	}
	if got := tt.Menu(); !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
}

// markCounts are how many times the window shows each of marks, by
// part, with every part shown: other panels can show the same words.
func markCounts(tt *ui.Tester, marks map[string]string) map[string]int {
	n := map[string]int{}
	for part, mark := range marks {
		n[part] = countText(tt, mark)
	}
	return n
}

// checkParts fails unless the parts in hidden are left out of the
// panel, and the others show, by marks, which the window shows base
// times with every part shown.
func checkParts(t *testing.T, tt *ui.Tester, marks map[string]string, base map[string]int, hidden ...string) {
	t.Helper()
	for _, p := range monitorParts {
		want := !slices.Contains(hidden, p.key)
		if got := countText(tt, marks[p.key]) == base[p.key]; got != want {
			t.Errorf("hidden %q: part %s (%q) shows = %v", hidden, p.key, marks[p.key], got)
		}
	}
}

func TestMonitorPartsMenu(t *testing.T) {
	a, tt, s := monitorPartsApp(t, "en")
	live, _ := monitorMarks(s)
	base := markCounts(tt, live)
	checkParts(t, tt, live, base)

	openMonitorMenu(t, tt, s.ip)
	for _, p := range monitorParts {
		if !menuChecked(t, tt, p.label()) {
			t.Errorf("%s not checked", p.key)
		}
	}
	tt.CloseMenu()

	// Hide each part in turn, then show them all again.
	var hidden []string
	for _, p := range monitorParts {
		openMonitorMenu(t, tt, s.ip)
		if err := tt.ChooseMenuItem(p.label()); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		hidden = append(hidden, p.key)
		checkParts(t, tt, live, base, hidden...)
		if got := config.LoadSettings().MonitorHidden; got != config.NewNames(hidden...) {
			t.Fatalf("saved %q, want %q", got.List(), hidden)
		}
		if len(hidden) < len(monitorParts) && tt.HasText(locales.MonitorPartsHint()) {
			t.Errorf("hint with %q hidden", hidden)
		}
	}
	if !tt.HasText(locales.MonitorPartsHint()) || !tt.HasText("IP "+s.ip) || !tt.HasText(locales.SystemInfo()) {
		t.Errorf("all hidden: texts %q", tt.Texts())
	}
	// The empty room under the hint has the menu too.
	tt.RightClickAt(120, 300)
	if menuChecked(t, tt, locales.MonitorPartNetwork()) {
		t.Error("network checked while hidden")
	}
	tt.CloseMenu()
	for _, p := range monitorParts {
		openMonitorMenu(t, tt, s.ip)
		if err := tt.ChooseMenuItem(p.label()); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	checkParts(t, tt, live, base)
	if a.settings.MonitorHidden != "" || config.LoadSettings().MonitorHidden != "" {
		t.Errorf("all shown: %q", a.settings.MonitorHidden.List())
	}

	// The interfaces' button keeps its own menu.
	if err := tt.Click(locales.NetIfaces()); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); len(got) == 0 || got[0] != locales.NetIfaceAutomatic() {
		t.Errorf("interfaces' menu %q", got)
	}
}

// TestMonitorPartsSkeleton: the skeleton, of a new tab or a session not
// connected yet, leaves out the same parts as the live view.
func TestMonitorPartsSkeleton(t *testing.T) {
	for _, newTab := range []bool{false, true} {
		a, tt, s := monitorPartsApp(t, "en")
		if newTab {
			a.NewTab()
		} else {
			s.status = statusConnecting
		}
		tt.Frame()
		_, skeleton := monitorMarks(s)
		base := markCounts(tt, skeleton)
		for part, n := range base {
			if n == 0 {
				t.Fatalf("new tab %v: no %s in the skeleton; texts %q", newTab, part, tt.Texts())
			}
		}
		for _, hidden := range [][]string{
			{partCPU, partNetwork, partDisks},
			{partUptime, partProcesses, partLatency},
			{partUptime, partCPU, partProcesses, partNetwork, partLatency, partDisks},
		} {
			a.settings.MonitorHidden = config.NewNames(hidden...)
			tt.Frame()
			checkParts(t, tt, skeleton, base, hidden...)
		}
		if !tt.HasText(locales.MonitorPartsHint()) || !tt.HasText(locales.SystemInfo()) {
			t.Errorf("new tab %v, all hidden: texts %q", newTab, tt.Texts())
		}
	}
}

// TestMonitorPartsUnknownKept: a part the menu does not know, as a later
// version's, stays hidden through the menu's changes.
func TestMonitorPartsUnknownKept(t *testing.T) {
	a, tt, s := monitorPartsApp(t, "en")
	a.settings.MonitorHidden = config.NewNames("gpu")
	tt.Frame()
	if tt.HasText(locales.MonitorPartsHint()) {
		t.Error("hint with only an unknown part hidden")
	}
	openMonitorMenu(t, tt, s.ip)
	if err := tt.ChooseMenuItem(locales.MonitorPartDisks()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := config.LoadSettings().MonitorHidden; got != config.NewNames("gpu", partDisks) {
		t.Errorf("saved %q", got.List())
	}
}

// menuChecked says whether the item label of the menu shown is checked.
func menuChecked(t *testing.T, tt *ui.Tester, label string) bool {
	t.Helper()
	item, ok := tt.AccessByRole(ui.RoleMenuItemCheckBox, label)
	if !ok {
		t.Fatalf("no check box %q in the menu %q", label, tt.Menu())
	}
	return item.Checked
}

// TestMonitorPartsShots saves the menu, a trimmed panel and the panel
// with every part hidden, in English and Simplified Chinese.
func TestMonitorPartsShots(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			a, tt, s := monitorPartsApp(t, lang)
			suffix := "-" + lang + ".png"
			openMonitorMenu(t, tt, s.ip)
			saveShot(t, tt, "monitor-parts-menu"+suffix)
			tt.CloseMenu()
			a.settings.MonitorHidden = config.NewNames(partProcesses, partDisks)
			tt.Frame()
			saveShot(t, tt, "monitor-parts-trimmed"+suffix)
			var all []string
			for _, p := range monitorParts {
				all = append(all, p.key)
			}
			a.settings.MonitorHidden = config.NewNames(all...)
			tt.Frame()
			if !tt.HasText(locales.MonitorPartsHint()) {
				t.Errorf("no hint; texts %q", tt.Texts())
			}
			saveShot(t, tt, "monitor-parts-none"+suffix)
		})
	}
}
