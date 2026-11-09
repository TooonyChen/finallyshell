package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// demoQuick gives the app quick commands in two groups, named in the
// language of zh, as a user would.
func demoQuick(a *App, zh bool) (common, logs *config.QuickGroup) {
	q := config.NewQuickCommands()
	names := map[string]string{
		"Common": "常用", "Logs": "日志", "Docker": "Docker", "Disk": "磁盘", "Memory": "内存", "Ports": "端口",
		"Last logins": "最近登录", "Clean logs": "清理日志", "Update system": "更新系统", "Restart nginx": "重启 nginx",
		"Big files": "查找大文件", "Follow syslog": "跟踪 syslog",
	}
	n := func(s string) string {
		if zh {
			return names[s]
		}
		return s
	}
	common, logs = q.AddGroup(n("Common")), q.AddGroup(n("Logs"))
	docker := q.AddGroup(n("Docker"))
	add := func(name, group, text string, enter bool) {
		q.AddCommand(&config.QuickCommand{Name: n(name), Group: group, Text: text, Enter: enter}, "")
	}
	add("Disk", common.ID, "df -h", true)
	add("Memory", common.ID, "free -m", true)
	add("Ports", common.ID, "ss -tlnp", true)
	add("Last logins", common.ID, "last -n 20", true)
	add("Clean logs", logs.ID, "journalctl --vacuum-time=7d\nfind /var/log -name '*.gz' -mtime +30 -delete\nrm -f /var/log/*.old", true)
	add("Restart nginx", common.ID, "systemctl restart nginx", true)
	add("Update system", common.ID, "apt update\napt -y upgrade", true)
	add("Big files", "", "du -ah / | sort -rh | head", true)
	add("Follow syslog", logs.ID, "tail -f /var/log/syslog", false)
	add("", docker.ID, "docker ps -a", true)
	a.quickUI.store = q
	return common, logs
}

// quickTab shows the Commands tab.
func quickTab(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click(locales.TabCommands()); err != nil {
		t.Fatalf("no Commands tab: %v; texts %q", err, tt.Texts())
	}
}

// quickTitles are the commands' button titles, in order.
func quickTitles(cmds []*config.QuickCommand) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, c.Title())
	}
	return out
}

// savedQuick is what quickcommands.json holds.
func savedQuick(t *testing.T) *config.QuickCommands {
	t.Helper()
	q, err := config.LoadQuickCommands()
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// A click on a button sends its text to the session, each line followed
// by a carriage return; with Enter off, the last line goes without one.
// Each line run is kept in the host's history, as the bar's are.
func TestQuickCommandsSend(t *testing.T) {
	a, p, s, conn, tt := barTerm(t)
	demoQuick(a, false)
	quickTab(t, tt)
	if !tt.HasText("Disk") || !tt.HasText("Common") || !tt.HasText(locales.QuickLines(3)) || !tt.HasText("df -h") {
		t.Fatalf("tab: %q", tt.Texts())
	}
	saveShot(t, tt, "quick-commands-en.png")
	for _, tc := range []struct{ button, want string }{
		{"Disk", "df -h\r"},
		{"Update system", "apt update\rapt -y upgrade\r"},
		{"Follow syslog", "tail -f /var/log/syslog"},
	} {
		if err := tt.Click(tc.button); err != nil {
			t.Fatal(err)
		}
		if got := conn.sent(t, s.term); got != tc.want {
			t.Errorf("%s sent %q, want %q", tc.button, got, tc.want)
		}
	}
	// The keyboard is the terminal's again, to press Enter there.
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if got := conn.sent(t, s.term); got != "\r" {
		t.Errorf("Enter after the button sent %q", got)
	}
	a.history.pending.Wait()
	if got := a.hostHistory(s.Host.ID).cmds; !slices.Equal(got, []string{"df -h", "apt update", "apt -y upgrade"}) {
		t.Errorf("history %q", got)
	}

	// With Enter off and several lines, the lines before the last run.
	store := a.quick()
	cmd := store.InGroup("")[0]
	cmd.Text, cmd.Enter = "cd /srv\ngit pull ", false
	tt.Frame()
	if err := tt.Click("Disk"); err != nil {
		t.Fatal(err)
	}
	if got := conn.sent(t, s.term); got != "cd /srv\rgit pull " {
		t.Errorf("Enter off sent %q", got)
	}

	// The Commands tab in a tab of its own sends to its session too.
	a.openPanelTab(s, tabCommands)
	tt.Frame()
	if !a.current().isPanelTab() || !tt.HasText("Memory") {
		t.Fatalf("panel row: %q", tt.Texts())
	}
	if err := tt.Click("Memory"); err != nil {
		t.Fatal(err)
	}
	if got := conn.sent(t, s.term); got != "free -m\r" {
		t.Errorf("from the panel row %q", got)
	}
	p.SetLocale("zh-Hans")
	tt.Frame()
	if !tt.HasText("命令") || !tt.HasText(locales.QuickLines(3)) {
		t.Errorf("Chinese: %q", tt.Texts())
	}
	p.SetLocale("en")
}

// The buttons of a session that is not connected, or of a new tab, are
// shown but send nothing.
func TestQuickCommandsDisabled(t *testing.T) {
	a, p, s, conn, tt := barTerm(t)
	demoQuick(a, false)
	quickTab(t, tt)
	if info, ok := tt.Access("Disk"); !ok || info.Disabled {
		t.Errorf("connected: %+v, found %v", info, ok)
	}
	s.status = statusClosed
	tt.Frame()
	if info, ok := tt.Access("Disk"); !ok || !info.Disabled {
		t.Errorf("not connected: %+v, found %v", info, ok)
	}
	if !tt.HasText(locales.QuickOffline()) {
		t.Errorf("no reason shown: %q", tt.Texts())
	}
	tt.Click("Disk")
	if got := conn.sent(t, s.term); got != "" {
		t.Errorf("a disconnected session's button sent %q", got)
	}
	if err := tt.RightClick("Disk"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.QuickSend()); err == nil {
		t.Error("Send could be chosen while disconnected")
	}
	tt.CloseMenu()

	// A new tab: the commands can be managed, not sent.
	a.NewTab()
	tt.Frame()
	if info, ok := tt.Access("Disk"); !ok || !info.Disabled {
		t.Errorf("new tab: %+v, found %v", info, ok)
	}
	saveShot(t, tt, "quick-commands-disabled-en.png")
	_ = p
}

// New command opens the editor, which adds the command to the group
// shown; an empty name shows the first line. Edit changes it, Escape
// cancels, and an empty command is refused.
func TestQuickCommandsEditor(t *testing.T) {
	a, p, _, _, tt := barTerm(t)
	_, logs := demoQuick(a, false)
	quickTab(t, tt)
	if err := tt.Click("Logs"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("Disk") || !tt.HasText("Follow syslog") {
		t.Fatalf("Logs shows %q", tt.Texts())
	}
	if err := tt.Click(locales.QuickNewCommand()); err != nil {
		t.Fatal(err)
	}
	spec := p.windows[WindowQuickCommand].spec
	if spec.Title() != locales.QuickNewCommand() || spec.Parent != "" || spec.Modal {
		t.Errorf("spec %+v", spec)
	}
	ed := p.show(t, WindowQuickCommand)
	if !ed.Focused(locales.FieldName()) {
		t.Error("the name does not have the keyboard")
	}
	if err := ed.Click(locales.Save()); err != nil {
		t.Fatal(err)
	}
	if !ed.HasText(locales.QuickErrCommand()) || !a.windowOpen(WindowQuickCommand) {
		t.Errorf("an empty command saved: %q", ed.Texts())
	}
	if err := ed.Click(locales.QuickFieldCommand()); err != nil {
		t.Fatal(err)
	}
	ed.Type("dmesg -T")
	ed.Key(0, ui.KeyEnter)
	ed.Type("tail -n 50 /var/log/kern.log")
	ed.Click(locales.QuickEnter()) // off
	if !ed.HasText(locales.QuickEnterOff()) {
		t.Errorf("no hint for Enter off: %q", ed.Texts())
	}
	saveShot(t, ed, "quick-command-editor-en.png")
	if err := ed.Click(locales.Save()); err != nil {
		t.Fatal(err)
	}
	if a.windowOpen(WindowQuickCommand) {
		t.Fatal("the editor stayed open")
	}
	tt.Frame()
	cmds := a.quick().InGroup(logs.ID)
	last := cmds[len(cmds)-1]
	if last.Title() != "dmesg -T" || last.Name != "" || last.Text != "dmesg -T\ntail -n 50 /var/log/kern.log" || last.Enter {
		t.Fatalf("added %+v", last)
	}
	if !tt.HasText("dmesg -T") {
		t.Errorf("not shown: %q", tt.Texts())
	}
	if saved := savedQuick(t).Command(last.ID); saved == nil || saved.Text != last.Text || saved.Group != logs.ID {
		t.Errorf("file holds %+v", saved)
	}

	// Edit: the fields hold the command; Save changes it in place.
	if err := tt.RightClick("Follow syslog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.QuickEdit()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	ed = p.show(t, WindowQuickCommand)
	if p.windows[WindowQuickCommand].spec.Title() != locales.QuickEditCommand() || a.quickUI.ed.text != "tail -f /var/log/syslog" {
		t.Fatalf("editing %+v", a.quickUI.ed)
	}
	a.quickUI.ed.name = "Syslog"
	if err := ed.Click(locales.Save()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Syslog") || tt.HasText("Follow syslog") {
		t.Errorf("not renamed: %q", tt.Texts())
	}
	// Escape gives up the changes.
	tt.RightClick("Syslog")
	tt.ChooseMenuItem(locales.QuickEdit())
	ed = p.show(t, WindowQuickCommand)
	a.quickUI.ed.name = "Nope"
	ed.Key(0, ui.KeyEscape)
	if a.windowOpen(WindowQuickCommand) || a.quickUI.ed != nil {
		t.Error("Escape did not close the editor")
	}
	tt.Frame()
	if tt.HasText("Nope") {
		t.Error("Escape kept the change")
	}

	p.SetLocale("zh-Hans")
	tt.RightClick("Syslog")
	tt.ChooseMenuItem(locales.QuickEdit())
	ed = p.show(t, WindowQuickCommand)
	ed.SetLocale("zh-Hans")
	ed.Frame()
	saveShot(t, ed, "quick-command-editor-zh.png")
	ed.Key(0, ui.KeyEscape)
	p.SetLocale("en")
}

// Groups of one name, and one named as another's label would be, each
// have a label of their own in the editor's group picker, which picks
// that group.
func TestQuickCommandsEditorSameNames(t *testing.T) {
	a, p := testApp(t)
	q := config.NewQuickCommands()
	g1, g2, g3 := q.AddGroup("Logs"), q.AddGroup("Logs"), q.AddGroup("Logs (2)")
	a.quickUI.store = q
	ids, labels := quickGroupChoices(q)
	want := []string{locales.QuickNoGroup(), "Logs", "Logs (2)", "Logs (2) (2)"}
	if !slices.Equal(labels, want) || !slices.Equal(ids, []string{"", g1.ID, g2.ID, g3.ID}) {
		t.Fatalf("labels %q, ids %q", labels, ids)
	}
	for i, g := range []*config.QuickGroup{g1, g2, g3} {
		a.newQuickCommand()
		ed := p.show(t, WindowQuickCommand)
		choose(t, ed, locales.QuickFieldGroup(), want[i+1])
		if a.quickUI.ed.group != g.ID {
			t.Errorf("%q picked group %q, want %q", want[i+1], a.quickUI.ed.group, g.ID)
		}
		a.quickUI.ed.text = "ls"
		if err := ed.Click(locales.Save()); err != nil {
			t.Fatal(err)
		}
		if a.windowOpen(WindowQuickCommand) {
			t.Fatal("the editor stayed open")
		}
		if cmds := q.InGroup(g.ID); len(cmds) != 1 {
			t.Errorf("group %d holds %d commands", i+1, len(cmds))
		}
	}
}

// Groups: New group names one in place and shows it; a group's menu
// renames it and deletes it, after asking, its commands kept under All.
// A command dropped on a group moves into it.
func TestQuickCommandsGroups(t *testing.T) {
	a, p, _, _, tt := barTerm(t)
	common, _ := demoQuick(a, false)
	quickTab(t, tt)
	if err := tt.Click(locales.QuickNewGroup()); err != nil {
		t.Fatal(err)
	}
	tt.Type("Nginx") // over the name selected
	tt.Key(0, ui.KeyEnter)
	store := a.quick()
	g := store.Groups[len(store.Groups)-1]
	if g.Name != "Nginx" || a.quickUI.group != g.ID || a.quickUI.naming != nil {
		t.Fatalf("groups %+v, shown %q", store.Groups, a.quickUI.group)
	}
	if !tt.HasText(locales.QuickNoneInGroup()) {
		t.Errorf("empty group: %q", tt.Texts())
	}
	// Escape gives up a new one.
	tt.Click(locales.QuickNewGroup())
	tt.Type("Gone")
	tt.Key(0, ui.KeyEscape)
	if len(store.Groups) != 4 || a.quickUI.naming != nil {
		t.Errorf("Escape kept %+v", store.Groups)
	}

	// Drop Restart nginx on the group.
	tt.Click(locales.QuickAll())
	from, ok1 := tt.Find("Restart nginx")
	to, ok2 := tt.Find("Nginx")
	if !ok1 || !ok2 {
		t.Fatalf("not found: %q", tt.Texts())
	}
	dragTo(tt, from.X+from.W/2, from.Y+from.H/2, to.X+to.W/2, to.Y+to.H/2)
	if got := quickTitles(store.InGroup(g.ID)); !slices.Equal(got, []string{"Restart nginx"}) {
		t.Errorf("Nginx holds %q", got)
	}

	// Rename.
	if err := tt.RightClick("Nginx"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.Rename()); err != nil {
		t.Fatal(err)
	}
	tt.Type("Web")
	tt.Key(0, ui.KeyEnter)
	if g.Name != "Web" || savedQuick(t).Group(g.ID).Name != "Web" {
		t.Errorf("renamed %+v", g)
	}

	// Delete Common, after asking; its commands stay, in no group.
	p.answer = true
	if err := tt.RightClick("Common"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.Delete()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the group deleted", func() bool { return store.Group(common.ID) == nil })
	if q := p.questions(); len(q) != 1 || q[0].Message != locales.QuickDeleteGroupTitle("Common") {
		t.Errorf("asked %+v", q)
	}
	if c := store.InGroup("")[0]; c.Title() != "Disk" || c.Group != "" {
		t.Errorf("Disk is now %+v", c)
	}
	if len(store.Commands) != 10 || savedQuick(t).Group(common.ID) != nil {
		t.Errorf("after the delete: %q", quickTitles(store.Commands))
	}
}

// The search filters by name and by text; dragging a button onto another
// moves it there.
func TestQuickCommandsSearchAndReorder(t *testing.T) {
	a, _, _, _, tt := barTerm(t)
	common, _ := demoQuick(a, false)
	quickTab(t, tt)
	if err := tt.Click(locales.QuickSearch()); err != nil {
		t.Fatal(err)
	}
	tt.Type("NGINX")
	if !tt.HasText("Restart nginx") || tt.HasText("Disk") {
		t.Errorf("by name: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("upgrade")
	if !tt.HasText("Update system") || tt.HasText("Restart nginx") {
		t.Errorf("by text: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("zzz")
	if !tt.HasText(locales.QuickNoMatches()) {
		t.Errorf("no match: %q", tt.Texts())
	}
	a.quickUI.query = ""
	tt.Click("Common")

	store := a.quick()
	want := []string{"Disk", "Memory", "Ports", "Last logins", "Restart nginx", "Update system"}
	if got := quickTitles(store.InGroup(common.ID)); !slices.Equal(got, want) {
		t.Fatalf("order %q", got)
	}
	from, _ := tt.Find("Disk")
	to, _ := tt.Find("Ports")
	dragTo(tt, from.X+from.W/2, from.Y+from.H/2, to.X+to.W/2, to.Y+to.H/2)
	want = []string{"Memory", "Ports", "Disk", "Last logins", "Restart nginx", "Update system"}
	if got := quickTitles(store.InGroup(common.ID)); !slices.Equal(got, want) {
		t.Errorf("after dragging Disk onto Ports: %q", got)
	}
	from, _ = tt.Find("Update system")
	to, _ = tt.Find("Memory")
	dragTo(tt, from.X+from.W/2, from.Y+from.H/2, to.X+to.W/2, to.Y+to.H/2)
	want = []string{"Update system", "Memory", "Ports", "Disk", "Last logins", "Restart nginx"}
	if got := quickTitles(store.InGroup(common.ID)); !slices.Equal(got, want) {
		t.Errorf("after dragging Update system onto Memory: %q", got)
	}
	if got := quickTitles(savedQuick(t).InGroup(common.ID)); !slices.Equal(got, want) {
		t.Errorf("file order %q", got)
	}
}

// The context menu: Put in the command bar, Duplicate, Move to group,
// Delete after asking.
func TestQuickCommandsMenu(t *testing.T) {
	a, p, s, conn, tt := barTerm(t)
	_, logs := demoQuick(a, false)
	quickTab(t, tt)

	if err := tt.RightClick("Disk"); err != nil {
		t.Fatal(err)
	}
	want := []string{locales.QuickSend(), locales.QuickPutInBar(), "", locales.QuickEdit(), locales.QuickDuplicate(), locales.QuickMoveTo(), "", locales.Delete()}
	if got := tt.Menu(); len(got) < len(want) {
		t.Errorf("menu %q", got)
	}
	if err := tt.ChooseMenuItem(locales.QuickPutInBar()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.bar.text != "df -h" || !s.bar.focused {
		t.Errorf("bar %q, focused %v", s.bar.text, s.bar.focused)
	}
	if got := conn.sent(t, s.term); got != "" {
		t.Errorf("putting it in the bar sent %q", got)
	}
	tt.Type(" /")
	tt.Key(0, ui.KeyEnter)
	if got := conn.sent(t, s.term); got != "df -h /\r" {
		t.Errorf("the bar sent %q", got)
	}
	// Not for several lines, which the bar cannot hold.
	tt.RightClick("Update system")
	if err := tt.ChooseMenuItem(locales.QuickPutInBar()); err == nil {
		t.Error("a command of two lines went into the bar")
	}
	tt.CloseMenu()

	store := a.quick()
	n := len(store.Commands)
	tt.RightClick("Memory")
	if err := tt.ChooseMenuItem(locales.QuickDuplicate()); err != nil {
		t.Fatal(err)
	}
	copyName := locales.QuickCopyName("Memory")
	if len(store.Commands) != n+1 || store.Commands[2].Name != copyName || store.Commands[2].Text != "free -m" || store.Commands[2].ID == store.Commands[1].ID {
		t.Fatalf("duplicate: %q", quickTitles(store.Commands))
	}

	tt.RightClick(copyName)
	if err := tt.ChooseMenuItem(locales.QuickMoveTo(), "Logs"); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	if c := store.Commands[2]; c.Group != logs.ID {
		t.Errorf("moved to %q", c.Group)
	}

	// Delete asks; No keeps it.
	p.answer = false
	tt.RightClick(copyName)
	tt.ChooseMenuItem(locales.Delete())
	p.until(t, "the question", func() bool { return len(p.questions()) == 1 })
	if q := p.questions()[0]; q.Message != locales.QuickDeleteTitle(copyName) || q.OK != locales.Delete() {
		t.Errorf("asked %+v", q)
	}
	p.answer = true
	tt.RightClick(copyName)
	tt.ChooseMenuItem(locales.Delete())
	p.until(t, "the delete", func() bool { return len(store.Commands) == n })
	if tt.HasText(copyName) || len(savedQuick(t).Commands) != n {
		t.Errorf("still there: %q", tt.Texts())
	}

	// The menu drawn, for the screenshots.
	menuShots(t, a, p, s)
}

// menuShots draws the tab with a button's context menu open, drawn in
// the window, in both languages.
func menuShots(t *testing.T, a *App, p *fakePlatform, s *Session) {
	if os.Getenv("SHOT_DIR") == "" {
		return
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		p.SetLocale(lang)
		demoQuick(a, lang != "en")
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetMenuPresentation(ui.PopoverMenus)
			a.View(c)
		}, 1280, 820)
		tt.SetLocale(lang)
		p.tt = tt
		tt.Frame()
		name := "Restart nginx"
		if lang != "en" {
			name = "重启 nginx"
		}
		if err := tt.RightClick(name); err != nil {
			t.Fatalf("%v; texts %q", err, tt.Texts())
		}
		tt.Frame()
		saveShot(t, tt, "quick-commands-menu-"+lang+".png")
		tt.CloseMenu()
		tt.Frame()
		saveShot(t, tt, "quick-commands-tab-"+lang+".png")
	}
	p.SetLocale("en")
	_ = s
}

// A file that does not parse is said above the tab, and kept aside:
// saving does not lose it.
func TestQuickCommandsBrokenFile(t *testing.T) {
	a, _, _, _, tt := barTerm(t)
	bad := []byte("{not json")
	if err := os.WriteFile(filepath.Join(config.Dir(), "quickcommands.json"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	quickTab(t, tt)
	if !tt.HasText(locales.QuickNone()) || !slices.ContainsFunc(tt.Texts(), func(s string) bool {
		return strings.Contains(s, "quickcommands.json.broken-")
	}) {
		t.Fatalf("no banner: %q", tt.Texts())
	}
	saveShot(t, tt, "quick-commands-broken-en.png")
	tt.Click(locales.QuickNewGroup())
	tt.Type("Mine")
	tt.Key(0, ui.KeyEnter)
	if len(savedQuick(t).Groups) != 1 {
		t.Error("the new group was not saved")
	}
	copies, _ := filepath.Glob(filepath.Join(config.Dir(), "quickcommands.json.broken-*"))
	if len(copies) != 1 {
		t.Fatalf("copies %q", copies)
	}
	if data, _ := os.ReadFile(copies[0]); string(data) != string(bad) {
		t.Errorf("the copy holds %q", data)
	}
	// The banner closes.
	if err := tt.Click(locales.Close()); err != nil {
		t.Fatal(err)
	}
	if a.quickUI.loadErr != nil {
		t.Error("the banner stayed")
	}
}

// A file that cannot be read nor copied aside is never saved over.
func TestQuickCommandsUnreadableFile(t *testing.T) {
	a, _ := testApp(t)
	// A folder where the file goes: reading it fails, and so would the
	// copy's content.
	path := filepath.Join(config.Dir(), "quickcommands.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	store := a.quick()
	if a.quickUI.loadErr == nil || !a.quickUI.noSave {
		t.Fatalf("err %v, noSave %v", a.quickUI.loadErr, a.quickUI.noSave)
	}
	store.AddGroup("x")
	if err := a.saveQuick(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		t.Errorf("saved over: %v %v", fi, err)
	}
}

// On a real server: a command of two lines runs line by line, and the
// file panel follows the cd in it, as after Enter.
func TestSSHQuickCommands(t *testing.T) {
	a, p, s := connectTestServer(t)
	defer a.CloseAll()
	p.until(t, "the home directory", func() bool { return s.files.cwd != "" && !s.files.loading })
	p.until(t, "the shell's folder", func() bool { return s.shell.dir != "" })
	out := fmt.Sprintf("/tmp/finallyshell-quick-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if sc, err := sftpTo(); err == nil {
			sc.Remove(out)
			sc.Close()
		}
	})
	q := config.NewQuickCommands()
	q.AddCommand(&config.QuickCommand{Name: "Go and write", Text: "cd /usr/share\npwd > " + out, Enter: true}, "")
	a.quickUI.store = q
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	quickTab(t, tt)
	if err := tt.Click("Go and write"); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the panel in /usr/share", func() bool { return s.files.cwd == "/usr/share" && !s.files.loading })
	var got []byte
	p.until(t, "the file the second line writes", func() bool {
		got, _ = readRemote(s, out)
		return len(got) > 0
	})
	if strings.TrimSpace(string(got)) != "/usr/share" {
		t.Errorf("the second line wrote %q", got)
	}
}
