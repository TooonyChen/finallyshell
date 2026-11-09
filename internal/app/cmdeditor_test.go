package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"finallyshell/internal/config"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// showCmdEditor shows the Commands tab and its command editor with the
// toggle, which gives the editor the keyboard.
func showCmdEditor(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	if a.bottomTab != tabCommands {
		quickTab(t, tt)
	}
	clickRole(t, tt, ui.RoleToggleButton, locales.CmdEditor())
	if !a.quickUI.box.shown || !tt.HasText(locales.CmdEditorSaveAs()) {
		t.Fatalf("editor not shown; texts %q", tt.Texts())
	}
}

// cmdText is the editor's text.
func cmdText(a *App) string { return a.quickUI.box.code.Buffer.String() }

// setCmdText replaces the editor's text, the caret at its end.
func setCmdText(a *App, tt *ui.Tester, text string) {
	st := a.cmdCode()
	st.Buffer.Set(text)
	st.Select(len(text), len(text))
	tt.Frame()
}

// selectLines selects lines from to to (from 0) of the editor's text,
// with their line ends.
func selectLines(a *App, tt *ui.Tester, from, to int) {
	st := a.quickUI.box.code
	start := st.Buffer.Offset(editor.Pos{Line: from})
	end := st.Buffer.Offset(editor.Pos{Line: to + 1})
	st.Select(start, end)
	tt.Frame()
}

// clickSend clicks the editor's Send button, by its name.
func clickSend(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	clickRole(t, tt, ui.RoleButton, name)
}

// clickRole clicks the element of the role and name, which must be
// enabled, and draws a frame.
func clickRole(t *testing.T, tt *ui.Tester, role ui.Role, name string) {
	t.Helper()
	b, ok := tt.AccessByRole(role, name)
	if !ok {
		t.Fatalf("no %q button; texts %q", name, tt.Texts())
	}
	if b.Disabled {
		t.Fatalf("%q is disabled", name)
	}
	tt.ClickAt(b.Bounds.X+b.Bounds.W/2, b.Bounds.Y+b.Bounds.H/2)
	tt.Frame()
}

// openCmdOptions opens the editor's Options menu.
func openCmdOptions(t *testing.T, tt *ui.Tester) {
	t.Helper()
	b, ok := tt.AccessByRole(ui.RoleMenuButton, locales.CmdEditorOptionsLabel())
	if !ok {
		t.Fatalf("no Options; texts %q", tt.Texts())
	}
	tt.Press(b.Bounds.X+b.Bounds.W/2, b.Bounds.Y+b.Bounds.H/2)
	tt.Release(b.Bounds.X+b.Bounds.W/2, b.Bounds.Y+b.Bounds.H/2)
}

// The toggle in the toolbar shows the editor beside the buttons, pressed
// while it shows, with the keyboard in the text; whether it shows and
// its width, as dragged, are saved and read back, and a double click on
// the divider puts the width back.
func TestCmdEditorToggle(t *testing.T) {
	a, p, _, _, tt := barTerm(t)
	demoQuick(a, false)
	quickTab(t, tt)
	if tt.HasText(locales.CmdEditorSaveAs()) {
		t.Fatal("the editor shows before it is asked for")
	}
	if info, ok := tt.AccessByRole(ui.RoleToggleButton, locales.CmdEditor()); !ok || info.Checked {
		t.Fatalf("toggle %+v, found %v", info, ok)
	}
	showCmdEditor(t, a, tt)
	if info, _ := tt.AccessByRole(ui.RoleToggleButton, locales.CmdEditor()); !info.Checked {
		t.Errorf("toggle not pressed: %+v", info)
	}
	if !config.LoadSettings().CommandEditorShown {
		t.Error("shown not saved")
	}
	tt.Type("uptime")
	if got := cmdText(a); got != "uptime" {
		t.Errorf("typed %q", got)
	}
	// Beside the buttons, its width from the window's right edge.
	box, ok := tt.Find(locales.CmdEditorSaveAs())
	disk, _ := tt.Find("Disk")
	if !ok || box.X < winW-defaultCmdEditorWidth || box.Y > disk.Y {
		t.Errorf("Save as command at %+v, Disk at %+v", box, disk)
	}

	const y = 700
	handle := func() float32 {
		x, _ := handleNear(t, tt, winW-a.quickUI.box.width+20, y, -0.5, 0, ui.CursorResizeEW)
		return x
	}
	drag := func(by float32) {
		t.Helper()
		x := handle()
		tt.Press(x, y)
		for i := float32(1); i <= 4; i++ {
			tt.Move(x+by*i/4, y)
		}
		tt.Release(x+by, y)
		tt.Frame()
	}
	drag(-100)
	if w := a.quickUI.box.width; w != defaultCmdEditorWidth+100 {
		t.Fatalf("width %v after the drag, want %v", w, defaultCmdEditorWidth+100)
	}
	tt.Advance(time.Second)
	if w := config.LoadSettings().CommandEditorWidth; w != defaultCmdEditorWidth+100 {
		t.Errorf("saved %v", w)
	}
	if b := New(p); !b.quickUI.box.shown || b.quickUI.box.width != defaultCmdEditorWidth+100 {
		t.Errorf("read back %+v", b.quickUI.box)
	}
	// The editor and the buttons keep their least widths.
	drag(2000)
	if w := a.quickUI.box.width; w != cmdEditorLimits.first {
		t.Errorf("width %v, want the least, %v", w, cmdEditorLimits.first)
	}
	drag(-2000)
	room := winW - a.sideWidth - 1 - quickGroupsWidth - 1
	if w := a.quickUI.box.width; w > room-cmdEditorLimits.second+0.5 {
		t.Errorf("width %v leaves the buttons less than %v", w, cmdEditorLimits.second)
	}
	x := handle()
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Frame()
	if w := a.quickUI.box.width; w != defaultCmdEditorWidth {
		t.Errorf("width %v after a double click", w)
	}

	// Hidden again: saved, and the text stays.
	clickRole(t, tt, ui.RoleToggleButton, locales.CmdEditor())
	if a.quickUI.box.shown || tt.HasText(locales.CmdEditorSaveAs()) || config.LoadSettings().CommandEditorShown {
		t.Errorf("not hidden; texts %q", tt.Texts())
	}
	if got := cmdText(a); got != "uptime" {
		t.Errorf("hidden with %q", got)
	}
}

// Send sends every line, each followed by Enter, through sendLine (the
// host's history keeps them); with text selected, it says Send
// selection and sends that alone. Cmd+Enter (Ctrl+Enter here) sends as
// the button does, Enter is a new line, Tab indents in the text, and
// Escape gives the keyboard to the terminal.
func TestCmdEditorSend(t *testing.T) {
	a, _, s, conn, tt := barTerm(t)
	demoQuick(a, false)
	showCmdEditor(t, a, tt)
	tt.Type("cd /srv/app")
	tt.Key(0, ui.KeyEnter)
	tt.Type("git pull")
	tt.Key(0, ui.KeyEnter)
	tt.Type("docker compose ps")
	tt.Frame()
	if got := cmdText(a); got != "cd /srv/app\ngit pull\ndocker compose ps" {
		t.Fatalf("typed %q", got)
	}
	if got := conn.sent(t, s.term); got != "" {
		t.Fatalf("typing sent %q", got)
	}
	clickSend(t, tt, locales.QuickSend())
	if got := conn.sent(t, s.term); got != "cd /srv/app\rgit pull\rdocker compose ps\r" {
		t.Errorf("Send sent %q", got)
	}
	if got := cmdText(a); got == "" {
		t.Error("Send emptied the editor")
	}

	// The selection alone: lines 2 and 3.
	selectLines(a, tt, 1, 1)
	if !tt.HasText(locales.CmdEditorSendSelection()) {
		t.Fatalf("no Send selection; texts %q", tt.Texts())
	}
	clickSend(t, tt, locales.CmdEditorSendSelection())
	if got := conn.sent(t, s.term); got != "git pull\r" {
		t.Errorf("Send selection sent %q", got)
	}

	// The key, in the text.
	a.quickUI.box.focus = true
	setCmdText(a, tt, "uptime\nwhoami")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if got := conn.sent(t, s.term); got != "uptime\rwhoami\r" {
		t.Errorf("%s sent %q", cmdSendKey.label("linux", false), got)
	}
	if got := cmdText(a); got != "uptime\nwhoami" {
		t.Errorf("the key changed the text to %q", got)
	}
	a.history.pending.Wait()
	if got := a.hostHistory(s.Host.ID).cmds; !slices.Equal(got, []string{"cd /srv/app", "docker compose ps", "git pull", "uptime", "whoami"}) {
		t.Errorf("history %q", got)
	}

	// Tab indents, and the text keeps the keyboard.
	setCmdText(a, tt, "if true")
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyTab)
	tt.Type("echo")
	if got := cmdText(a); !strings.HasPrefix(got, "if true\n") || !strings.HasSuffix(got, "echo") || !strings.ContainsAny(got[len("if true\n"):], "\t ") {
		t.Errorf("Tab gave %q", got)
	}

	// Escape: the terminal has the keyboard.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if got := conn.sent(t, s.term); got != "\r" {
		t.Errorf("Enter after Escape sent %q", got)
	}
}

// The Options: Enter off sends the last line without it, the lines
// before still run, and the terminal takes the keyboard to press it;
// Clear after sending empties the editor of what was sent, the
// selection alone when one was. Both are saved.
func TestCmdEditorOptions(t *testing.T) {
	a, p, s, conn, tt := barTerm(t)
	showCmdEditor(t, a, tt)
	setCmdText(a, tt, "cd /srv\ngit pull ")
	openCmdOptions(t, tt)
	if m := tt.Menu(); !slices.Equal(m, []string{locales.QuickEnter(), locales.CmdEditorClearAfter()}) {
		t.Fatalf("menu %q", m)
	}
	if err := tt.ChooseMenuItem(locales.QuickEnter()); err != nil {
		t.Fatal(err)
	}
	if !config.LoadSettings().CommandEditorNoEnter {
		t.Fatal("Enter off not saved")
	}
	clickSend(t, tt, locales.QuickSend())
	if got := conn.sent(t, s.term); got != "cd /srv\rgit pull " {
		t.Errorf("Enter off sent %q", got)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if got := conn.sent(t, s.term); got != "\r" {
		t.Errorf("Enter after the send sent %q, not in the terminal", got)
	}

	// Clear after sending: the selection goes, the rest stays.
	openCmdOptions(t, tt)
	tt.ChooseMenuItem(locales.QuickEnter()) // on again
	openCmdOptions(t, tt)
	if err := tt.ChooseMenuItem(locales.CmdEditorClearAfter()); err != nil {
		t.Fatal(err)
	}
	if st := config.LoadSettings(); st.CommandEditorNoEnter || !st.CommandEditorClear {
		t.Fatalf("saved %+v", st)
	}
	if b := New(p); b.settings.CommandEditorNoEnter || !b.settings.CommandEditorClear {
		t.Error("options not read back")
	}
	setCmdText(a, tt, "one\ntwo\nthree")
	selectLines(a, tt, 1, 1)
	clickSend(t, tt, locales.CmdEditorSendSelection())
	if got := conn.sent(t, s.term); got != "two\r" {
		t.Errorf("sent %q", got)
	}
	if got := cmdText(a); got != "one\nthree" {
		t.Errorf("left %q", got)
	}
	clickSend(t, tt, locales.QuickSend())
	if got := conn.sent(t, s.term); got != "one\rthree\r" {
		t.Errorf("sent %q", got)
	}
	if got := cmdText(a); got != "" {
		t.Errorf("left %q", got)
	}
	// Cmd+Z brings it back.
	a.quickUI.box.focus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := cmdText(a); got != "one\nthree" {
		t.Errorf("undo gave %q", got)
	}
	// Clear empties it at once.
	if err := tt.Click(locales.CmdEditorClear()); err != nil {
		t.Fatal(err)
	}
	if got := cmdText(a); got != "" {
		t.Errorf("Clear left %q", got)
	}
}

// With All sessions as the target, chosen with the editor's Send to,
// Send asks first, then sends each line to each connected session once,
// the panel row not doubling its session; the toolbar's Send to gives
// way to the editor's.
func TestCmdEditorSendAll(t *testing.T) {
	f := newSendFarm(t)
	a, tt := f.a, f.tt
	showCmdEditor(t, a, tt)
	setCmdText(a, tt, "uptime\ndf -h")
	if err := tt.Click(locales.SendToCurrent()); err != nil {
		t.Fatalf("no Send to: %v; texts %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem(locales.SendToAll()); err != nil {
		t.Fatalf("%v; menu %q", err, tt.Menu())
	}
	tt.Frame()
	if a.sendTo.mode != targetAll || !tt.HasText(locales.SendToAllSessionsCount(2)) {
		t.Fatalf("target %d; texts %q", a.sendTo.mode, tt.Texts())
	}
	if n := strings.Count(strings.Join(tt.Texts(), "\n"), locales.SendToAllSessionsCount(2)); n != 1 {
		t.Errorf("%d Send to controls in the tab", n)
	}
	clickSend(t, tt, locales.QuickSend())
	if a.sendTo.ask == nil || !tt.HasText(locales.SendToConfirm(2)) {
		t.Fatalf("no question: %q", tt.Texts())
	}
	f.want(t, "before the answer", "", "", "")
	tt.Key(0, ui.KeyEnter) // its Send has the keyboard
	tt.Frame()
	if a.sendTo.ask != nil {
		t.Fatal("the question stayed")
	}
	f.want(t, "Send", "uptime\rdf -h\r", "uptime\rdf -h\r", "")

	// From the panel row, which shows the editor too: no question now.
	a.active = slices.IndexFunc(a.sessions, (*Session).isPanelTab)
	tt.Frame()
	if !tt.HasText(locales.CmdEditorSaveAs()) {
		t.Fatalf("no editor in the panel row: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape) // nothing to give the keyboard to
	clickSend(t, tt, locales.QuickSend())
	if a.sendTo.ask != nil {
		t.Fatal("asked again")
	}
	f.want(t, "from the panel row", "uptime\rdf -h\r", "uptime\rdf -h\r", "")
}

// Send is disabled while no session of the target is connected, and in
// a new tab, as the buttons are, and while the text is empty.
func TestCmdEditorDisabled(t *testing.T) {
	a, _, s, conn, tt := barTerm(t)
	showCmdEditor(t, a, tt)
	disabled := func() bool {
		t.Helper()
		b, ok := tt.AccessByRole(ui.RoleButton, locales.QuickSend())
		if !ok {
			t.Fatalf("no Send; texts %q", tt.Texts())
		}
		return b.Disabled
	}
	if !disabled() {
		t.Error("Send enabled with no text")
	}
	setCmdText(a, tt, "uptime")
	if disabled() {
		t.Error("Send disabled with text")
	}
	s.status = statusClosed
	tt.Frame()
	if !disabled() {
		t.Error("Send enabled while not connected")
	}
	a.quickUI.box.focus = true
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	if got := conn.sent(t, s.term); got != "" {
		t.Errorf("the key sent %q while not connected", got)
	}
	s.status = statusConnected
	a.NewTab()
	tt.Frame()
	if !disabled() {
		t.Error("Send enabled in a new tab")
	}
}

// The text is saved to its file a moment after typing stops, and as the
// app closes, and a new App reads it back.
func TestCmdEditorTextKept(t *testing.T) {
	a, p, _, _, tt := barTerm(t)
	showCmdEditor(t, a, tt)
	tt.Type("tail -f /var/log/syslog")
	tt.Key(0, ui.KeyEnter)
	tt.Type("# 中文 too")
	file := filepath.Join(config.Dir(), config.CommandEditorFile)
	if _, err := os.Stat(file); err == nil {
		t.Error("saved while typing")
	}
	tt.Advance(2 * cmdTextSaveDelay)
	tt.Frame()
	want := "tail -f /var/log/syslog\n# 中文 too"
	if data, err := os.ReadFile(file); err != nil || string(data) != want {
		t.Fatalf("file %q, %v", data, err)
	}
	b := New(p)
	if got := b.cmdCode().Buffer.String(); got != want {
		t.Errorf("read back %q", got)
	}

	// Closing saves what was typed since, at once.
	tt.Type("!")
	a.CloseAll()
	if data, _ := os.ReadFile(file); string(data) != want+"!" {
		t.Errorf("after closing the file holds %q", data)
	}
}

// A file that cannot be read is said in the editor, and not saved over.
func TestCmdEditorUnreadable(t *testing.T) {
	a, _, _, _, tt := barTerm(t)
	file := filepath.Join(config.Dir(), config.CommandEditorFile)
	if err := os.MkdirAll(file, 0o700); err != nil { // a folder: no file to read
		t.Fatal(err)
	}
	showCmdEditor(t, a, tt)
	if a.quickUI.box.loadErr == nil || !strings.Contains(strings.Join(tt.Texts(), "\n"), config.CommandEditorFile) {
		t.Fatalf("no error shown: %q", tt.Texts())
	}
	tt.Type("ls")
	tt.Advance(2 * cmdTextSaveDelay)
	tt.Frame()
	if fi, err := os.Stat(file); err != nil || !fi.IsDir() {
		t.Errorf("replaced: %v, %v", fi, err)
	}
}

// Save as command opens the quick command editor on a new command of
// the selection, or of all the text, its Enter the editor's.
func TestCmdEditorSaveAsCommand(t *testing.T) {
	a, p, _, _, tt := barTerm(t)
	showCmdEditor(t, a, tt)
	setCmdText(a, tt, "apt update\napt -y upgrade\nreboot")
	if err := tt.Click(locales.CmdEditorSaveAs()); err != nil {
		t.Fatal(err)
	}
	if !a.windowOpen(WindowQuickCommand) || a.quickUI.ed == nil {
		t.Fatal("no editor window")
	}
	if ed := a.quickUI.ed; ed.id != "" || ed.text != "apt update\napt -y upgrade\nreboot" || !ed.enter {
		t.Errorf("window holds %+v", ed)
	}
	w := p.show(t, WindowQuickCommand)
	saveShot(t, w, "cmdeditor-save-as-en.png")
	if err := w.Click(locales.Save()); err != nil {
		t.Fatal(err)
	}
	if cmds := a.quick().Commands; len(cmds) != 1 || cmds[0].Text != "apt update\napt -y upgrade\nreboot" {
		t.Fatalf("saved %+v", cmds)
	}

	// The selection, with Enter off.
	a.settings.CommandEditorNoEnter = true
	selectLines(a, tt, 2, 2)
	tt.Click(locales.CmdEditorSaveAs())
	if ed := a.quickUI.ed; ed == nil || ed.text != "reboot" || ed.enter {
		t.Errorf("window holds %+v", ed)
	}
}

// Where the Commands tab is narrow, as in the right column, the editor
// goes under the groups and the buttons, the tab's whole width, its
// share of the height dragged and saved.
func TestCmdEditorRightColumn(t *testing.T) {
	a, p, _, _, tt := barTerm(t)
	demoQuick(a, false)
	showCmdEditor(t, a, tt)
	a.setPanelOnRight(true)
	tt.Frame()
	tt.Frame()
	left := winW - a.rightWidth
	head, ok := tt.Find(locales.CmdEditorSaveAs())
	groups, _ := tt.Find(locales.QuickNewGroup())
	if !ok || head.Y < groups.Y {
		t.Fatalf("Save as command at %+v, New group at %+v", head, groups)
	}
	all, _ := tt.Find(locales.QuickAll())
	if all.X < left || all.X > left+20 {
		t.Errorf("All at %+v; the column starts at %v", all, left)
	}
	// Send keeps its row, at the end.
	send, ok := tt.AccessByRole(ui.RoleButton, locales.QuickSend())
	if !ok || send.Bounds.X+send.Bounds.W > winW || send.Bounds.Y < head.Y {
		t.Errorf("Send at %+v, %v", send.Bounds, ok)
	}

	// The divider: dragged down, saved, read back.
	x := left + a.rightWidth/2
	_, y := handleNear(t, tt, x, head.Y-6, 0, -0.5, ui.CursorResizeNS)
	tt.Press(x, y)
	for i := float32(1); i <= 4; i++ {
		tt.Move(x, y+50*i/4)
	}
	tt.Release(x, y+50)
	tt.Frame()
	if a.quickUI.box.pct <= defaultCmdEditorPct {
		t.Fatalf("share %v after the drag", a.quickUI.box.pct)
	}
	tt.Advance(time.Second)
	if pct := config.LoadSettings().CommandEditorPercent; pct != a.quickUI.box.pct {
		t.Errorf("saved %v, want %v", pct, a.quickUI.box.pct)
	}
	if b := New(p); b.quickUI.box.pct != a.quickUI.box.pct {
		t.Error("share not read back")
	}

	// Wide enough, it is beside them again.
	a.rightWidth = 900
	tt.Frame()
	tt.Frame()
	head, _ = tt.Find(locales.CmdEditorSaveAs())
	groups, _ = tt.Find(locales.QuickNewGroup())
	if head.Y > groups.Y || head.X < winW-a.quickUI.box.width {
		t.Errorf("Save as command at %+v in a wide column", head)
	}
}

// cmdDemo is the editor's text in the screenshots, lines 3 and 4
// selected by TestCmdEditorShots.
var cmdDemo = map[string]string{
	"en":      "cd /srv/app\ngit pull\ndocker compose up -d --build\ndocker compose ps\n# lines 3 and 4 are selected: only they are sent\ntail -n 50 logs/app.log",
	"zh-Hans": "cd /srv/app\ngit pull\ndocker compose up -d --build\ndocker compose ps\n# 选中了第 3、4 行：只发送这两行\ntail -n 50 logs/app.log",
}

// The editor in both languages, for the screenshots: open with a
// selection, its Options menu, the target All sessions and its
// question, and the tab in the right column.
func TestCmdEditorShots(t *testing.T) {
	if os.Getenv("SHOT_DIR") == "" {
		t.Skip("SHOT_DIR is not set")
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			f := newSendFarm(t)
			a, p := f.a, f.p
			p.SetLocale(lang)
			demoQuick(a, lang != "en")
			a.closeSession(a.sessions[slices.IndexFunc(a.sessions, (*Session).isPanelTab)])
			a.active, a.bottomTab = 0, tabCommands
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetMenuPresentation(ui.PopoverMenus)
				a.View(c)
			}, winW, winH)
			tt.SetLocale(lang)
			p.tt = tt
			tt.Frame()
			showCmdEditor(t, a, tt)
			setCmdText(a, tt, cmdDemo[lang])
			selectLines(a, tt, 2, 3)
			short := strings.TrimSuffix(lang, "-Hans")
			saveShot(t, tt, "cmdeditor-selection-"+short+".png")

			openCmdOptions(t, tt)
			tt.Frame()
			saveShot(t, tt, "cmdeditor-options-"+short+".png")
			tt.CloseMenu()

			a.setTarget(targetAll)
			tt.Frame()
			saveShot(t, tt, "cmdeditor-target-all-"+short+".png")
			clickSend(t, tt, locales.CmdEditorSendSelection())
			saveShot(t, tt, "cmdeditor-confirm-"+short+".png")
			tt.Key(0, ui.KeyEscape)
			tt.Frame()

			a.setPanelOnRight(true)
			tt.Frame()
			tt.Frame()
			saveShot(t, tt, "cmdeditor-right-column-"+short+".png")
			a.setTarget(targetCurrent)
			a.quickUI.box.shown = false
			tt.Frame()
			tt.Frame()
			saveShot(t, tt, "cmdeditor-right-column-hidden-"+short+".png")
			a.setPanelOnRight(false)
			tt.Frame()
			saveShot(t, tt, "cmdeditor-hidden-"+short+".png")
		})
	}
}
