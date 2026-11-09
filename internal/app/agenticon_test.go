package app

import (
	"os"
	"testing"
	"time"

	"finallyshell/internal/monitor"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// claudeArgs is Claude Code's command line as node runs it.
var claudeArgs = []string{"node", "/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js"}

// agentServer is followServer with the server's answers from a fake,
// and the reads of an agent's tab every 30 ms.
func agentServer(t *testing.T) (*App, *fakePlatform, *Session, *ui.Tester, *fakeShellDir) {
	t.Helper()
	poll := agentPoll
	agentPoll = 30 * time.Millisecond
	t.Cleanup(func() { agentPoll = poll })
	home := followHome(t)
	fd := &fakeShellDir{dir: home}
	standInShell(t, fd)
	a, p, s, tt := followServer(t, home, true)
	settle(p, 100*time.Millisecond) // the reads as the shell starts
	return a, p, s, tt, fd
}

// agentShown is the agent the row of s shows, as assistive technology
// sees its logo: "" for none.
func agentShown(tt *ui.Tester, s *Session) string {
	tt.Frame()
	for i := range monitor.Agents {
		if info, ok := tt.Access(monitor.Agents[i].Name); ok && info.Bounds.W > 0 {
			return monitor.Agents[i].Name
		}
	}
	return ""
}

// An agent started with Enter shows its logo, with its name as the
// tooltip, next to the tab's name, a custom one too; Ctrl+C ending it
// takes it away. While the logo shows, the tab reads again now and then,
// and sees another agent, or none, without a key; then the reads stop.
func TestAgentIcon(t *testing.T) {
	a, p, s, tt, fd := agentServer(t)
	if agentShown(tt, s) != "" {
		t.Fatal("an agent at the prompt")
	}
	fd.run(claudeArgs...)
	tt.Key(0, ui.KeyEnter)
	p.until(t, "Claude Code", func() bool { return agentShown(tt, s) == "Claude Code" })
	info, _ := tt.Access("Claude Code")
	name, ok := tt.Find(s.label())
	if !ok || info.Bounds.X < name.X+30 || info.Bounds.X+info.Bounds.W > name.X+name.W-20 || info.Bounds.Y > name.Y+name.H || info.Bounds.Y+info.Bounds.H < name.Y {
		t.Errorf("logo at %+v, name at %+v", info.Bounds, name)
	}
	if info.Bounds.H < 13 || info.Bounds.H > 15 {
		t.Errorf("logo %v high", info.Bounds.H)
	}
	tt.Move(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Advance(2 * time.Second)
	tt.Frame()
	if !tt.HasText("Claude Code") {
		t.Errorf("no tooltip; texts %q", tt.Texts())
	}
	tt.Move(760, 200)
	tt.Frame()

	// A custom name keeps the logo.
	s.name = "build"
	if agentShown(tt, s) != "Claude Code" || !tt.HasText("build") {
		t.Errorf("renamed: agent %q; texts %q", agentShown(tt, s), tt.Texts())
	}

	// Ctrl+C ends it.
	fd.run()
	tt.Key(ui.Ctrl, ui.KeyC)
	p.until(t, "no agent", func() bool { return agentShown(tt, s) == "" })

	// Another starts; while it shows, the tab sees it change by itself.
	fd.run("codex")
	tt.Key(ui.Ctrl, ui.KeyZ) // as after fg
	p.until(t, "Codex", func() bool { return agentShown(tt, s) == "Codex" })
	fd.run("node", "/usr/bin/gemini")
	p.until(t, "Gemini CLI without a key", func() bool { return agentShown(tt, s) == "Gemini CLI" })
	fd.run()
	p.until(t, "the agent gone by itself", func() bool { return agentShown(tt, s) == "" })
	settle(p, 100*time.Millisecond)
	n := fd.count()
	settle(p, 200*time.Millisecond)
	if fd.count() != n {
		t.Errorf("%d reads without an agent or a key", fd.count()-n)
	}
	// Ctrl+D reads too.
	fd.run("aider")
	tt.Key(ui.Ctrl, ui.KeyD)
	p.until(t, "Aider", func() bool { return agentShown(tt, s) == "Aider" })
	if !a.settings.NoFollowTerminalDir && s.files.cwd != fd.dir {
		t.Errorf("the panel moved to %q", s.files.cwd)
	}
}

// Turned off in Options › Terminal, no tab shows an agent and none is
// read: Ctrl+C reads nothing, nor does Enter in a shell that reports its
// folder, and a tab showing one stops reading. Turned on, the tab reads
// at once.
func TestAgentIconsOff(t *testing.T) {
	a, p, s, tt, fd := agentServer(t)
	fd.run(claudeArgs...)
	tt.Key(0, ui.KeyEnter)
	p.until(t, "Claude Code", func() bool { return agentShown(tt, s) == "Claude Code" })

	a.openOptions()
	ot := p.show(t, WindowOptions)
	if err := ot.Click(locales.OptionsTerminal()); err != nil {
		t.Fatal(err)
	}
	if !ot.HasText(locales.OptionAgentIconsNote()) {
		t.Errorf("no note; texts %q", ot.Texts())
	}
	saveShot(t, ot, "options-agent-icons-en.png")
	if err := ot.Click(locales.OptionAgentIcons()); err != nil {
		t.Fatal(err)
	}
	if !a.settings.NoAgentIcons {
		t.Fatal("the checkbox did not turn the logos off")
	}
	if agentShown(tt, s) != "" {
		t.Error("a logo with the option off")
	}
	settle(p, 100*time.Millisecond)
	n := fd.count()
	settle(p, 200*time.Millisecond)
	tt.Key(ui.Ctrl, ui.KeyC)
	reportDir(s, fd.dir)
	tt.Key(0, ui.KeyEnter)
	settle(p, 200*time.Millisecond)
	if fd.count() != n || agentShown(tt, s) != "" {
		t.Errorf("off: %d reads, agent %q", fd.count()-n, agentShown(tt, s))
	}

	if err := ot.Click(locales.OptionAgentIcons()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "Claude Code again", func() bool { return agentShown(tt, s) == "Claude Code" })
}

// With the file panel not following the terminal, the reads still tell
// the agent, and the folder they read leaves the panel where it is.
func TestAgentIconNoFollow(t *testing.T) {
	a, p, s, tt, fd := agentServer(t)
	a.settings.NoFollowTerminalDir = true
	home := fd.dir
	fd.set(home + "/a")
	fd.run("claude")
	tt.Key(0, ui.KeyEnter)
	p.until(t, "Claude Code", func() bool { return agentShown(tt, s) == "Claude Code" })
	settle(p, 100*time.Millisecond)
	if s.files.cwd != home {
		t.Errorf("the panel went to %q", s.files.cwd)
	}
}

// The follower, and its reads, end with the session.
func TestAgentFollowerEnds(t *testing.T) {
	before := shellFollowers.Load()
	a, p, s, tt, fd := agentServer(t)
	fd.run("claude")
	tt.Key(0, ui.KeyEnter)
	p.until(t, "Claude Code", func() bool { return agentShown(tt, s) == "Claude Code" })
	if shellFollowers.Load() <= before {
		t.Fatal("no follower")
	}
	a.closeSession(s)
	p.until(t, "the follower ended", func() bool { return shellFollowers.Load() <= before })
	n := fd.count()
	settle(p, 150*time.Millisecond)
	if fd.count() != n {
		t.Errorf("%d reads after closing", fd.count()-n)
	}
}

// A disconnected session's tab shows no agent; a panel row shows none,
// its session's row does; and every agent has its logo.
func TestAgentIconRows(t *testing.T) {
	a, p := testApp(t)
	s := demoSession(a, "web-1", "10.0.0.7")
	s.shell.agent = &monitor.Agents[0]
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	a.openPanelTab(s, tabFiles)
	// The panel row first, so that a logo it showed would be the first.
	a.sessions[0], a.sessions[1] = a.sessions[1], a.sessions[0]
	tt.Frame()
	info, ok := tt.Access("Claude Code")
	name, _ := tt.Find("web-1")
	if !ok || info.Bounds.Y > name.Y+name.H || info.Bounds.Y+info.Bounds.H < name.Y {
		t.Errorf("the first logo at %+v, the session's name at %+v", info.Bounds, name)
	}
	a.sessions[0], a.sessions[1] = a.sessions[1], a.sessions[0]
	s.status = statusClosed
	if agentShown(tt, s) != "" {
		t.Error("a logo on a closed session")
	}
	for i := range monitor.Agents {
		if agentIcon(&monitor.Agents[i]) == nil {
			t.Errorf("no logo for %s", monitor.Agents[i].Name)
		}
	}
}

// TestAgentIconScreenshots draws a session list of tabs running
// different agents, light and dark, in both languages, with SHOT_DIR
// set.
func TestAgentIconScreenshots(t *testing.T) {
	if os.Getenv("SHOT_DIR") == "" {
		t.Skip("SHOT_DIR not set")
	}
	for _, lang := range []string{"en", "zh-Hans"} {
		for _, dark := range []bool{false, true} {
			a, p := testApp(t)
			p.SetLocale(lang)
			agents := []string{"Claude Code", "Codex", "", "Gemini CLI", "pi", "Aider"}
			for i, name := range agents {
				s := demoSession(a, "server1", "203.0.113.10")
				if i == 2 {
					s.Host.Name = "db-1"
				}
				if i == 4 {
					s.name = "a rather long name for a tab"
				}
				for j := range monitor.Agents {
					if monitor.Agents[j].Name == name {
						s.shell.agent = &monitor.Agents[j]
					}
				}
			}
			a.active = 0
			tt := ui.NewTester(a.View, 1280, 820)
			tt.SetScale(2)
			tt.SetLocale(lang)
			tt.SetDark(dark)
			p.tt = tt
			tt.Frame()
			mode := "light"
			if dark {
				mode = "dark"
			}
			saveShot(t, tt, "agent-icons-"+mode+"-"+lang+".png")
			a.openOptions()
			ot := p.show(t, WindowOptions)
			ot.SetDark(dark)
			if err := ot.Click(locales.OptionsTerminal()); err != nil {
				t.Fatal(err)
			}
			saveShot(t, ot, "options-terminal-"+mode+"-"+lang+".png")
		}
	}
}
