package app

import (
	"slices"
	"strconv"

	"finallyshell/internal/icons"
	"finallyshell/internal/widgets"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// The send target is where the command bar and the Commands tab send:
// the current session, every session, or the sessions chosen, as
// FinalShell's 发送到. It is one for the window, shared by both, and never
// saved: every start sends to the current session, so that nobody runs
// a command on every server by mistake after a restart.
//
// Every session sent to goes through sendLine (commandbar.go), as one
// does: its encoding, its folder follower and its host's history. A
// panel row is a view of its session, not a session, and a new tab has
// none: neither is sent to, and a session is sent to once. Sessions not
// connected are skipped, the others still get the line.
//
// The first send after the target changes to anything but the current
// session asks first, naming the sessions; later sends to the same
// target don't, as this is used for many commands in a row.

// targetMode is what the target is.
type targetMode int

const (
	targetCurrent targetMode = iota
	targetAll
	targetChosen
)

// sendTarget is the window's send target.
type sendTarget struct {
	mode targetMode
	// chosen are the sessions chosen, by ID, for targetChosen; closed
	// sessions drop out of it.
	chosen map[int64]bool
	// confirmed is set once a send to the target was confirmed; a new
	// target asks again.
	confirmed bool
	// picking is the control whose Choose sessions popover is open, ""
	// for none: "quick" for the Commands tab's, "editor" for its command
	// editor's, "bar-<ID>" for a bar's.
	picking string
	ask     *sendAsk // the question shown before the first send
}

// sendAsk is the question before a send to several sessions.
type sendAsk struct {
	sessions []*Session
	send     func() // sends, once confirmed
}

// setTarget makes m the target; a change asks again before sending.
func (a *App) setTarget(m targetMode) {
	st := &a.sendTo
	if st.mode != m {
		st.mode, st.confirmed = m, false
	}
}

// targetSessions are the sessions a send target could hold: each row
// that is a session of its own, not a new tab nor a panel row.
func (a *App) targetSessions() []*Session {
	var out []*Session
	for _, s := range a.sessions {
		if !s.isNewTab() {
			out = append(out, s)
		}
	}
	return out
}

// chosenSessions are the sessions chosen, in the list's order. Those
// closed drop out of the set; with none left, the target is the current
// session again.
func (a *App) chosenSessions() []*Session {
	st := &a.sendTo
	var out []*Session
	seen := map[int64]bool{}
	for _, s := range a.targetSessions() {
		if st.chosen[s.ID] {
			out, seen[s.ID] = append(out, s), true
		}
	}
	for id := range st.chosen {
		if !seen[id] {
			delete(st.chosen, id)
		}
	}
	if st.mode == targetChosen && len(out) == 0 && st.picking == "" {
		a.setTarget(targetCurrent)
	}
	return out
}

// sendTargets are the sessions a send from cur, the session the bar or
// the Commands tab belongs to, goes to: those connected, each once.
func (a *App) sendTargets(cur *Session) []*Session {
	var from []*Session
	switch a.sendTo.mode {
	case targetAll:
		from = a.targetSessions()
	case targetChosen:
		from = a.chosenSessions()
	default:
		if cur.isPanelTab() {
			cur = cur.panelOf
		}
		if cur != nil && !cur.isNewTab() {
			from = []*Session{cur}
		}
	}
	var out []*Session
	for _, s := range from {
		if barLive(s) {
			out = append(out, s)
		}
	}
	return out
}

// sendToTargets runs send for each session of the target, asking first
// when the target is not the current session and has not been
// confirmed. done runs after the sends. It reports whether it sent now:
// false when it asks, or when there is no session to send to.
func (a *App) sendToTargets(cur *Session, send func(*Session), done func()) bool {
	targets := a.sendTargets(cur)
	if len(targets) == 0 {
		return false
	}
	st := &a.sendTo
	run := func() {
		for _, s := range targets {
			// The question may have shown a while: skip what went.
			if barLive(s) && slices.Contains(a.sessions, s) {
				send(s)
			}
		}
		if done != nil {
			done()
		}
	}
	if st.mode != targetCurrent && !st.confirmed {
		st.ask = &sendAsk{sessions: targets, send: func() {
			st.confirmed = true
			run()
		}}
		return false
	}
	run()
	return true
}

// targetLabel says what the target is: short for the bar's button.
func (a *App) targetLabel(short bool) string {
	switch a.sendTo.mode {
	case targetAll:
		n := 0
		for _, s := range a.targetSessions() {
			if barLive(s) {
				n++
			}
		}
		if short {
			return locales.SendToAllCount(n)
		}
		return locales.SendToAllSessionsCount(n)
	case targetChosen:
		return locales.SendToSessions(len(a.chosenSessions()))
	}
	return locales.SendToCurrent()
}

// targetMenu is the menu of a target control, at anchor: Current
// session, All sessions, and Choose sessions…, which opens the popover
// of check boxes there.
func (a *App) targetMenu(m *ui.Menu, anchor string) {
	mode := a.sendTo.mode
	if m.Item(locales.SendToCurrent()).Radio(mode == targetCurrent).Chosen() {
		a.setTarget(targetCurrent)
	}
	if m.Item(locales.SendToAll()).Radio(mode == targetAll).Chosen() {
		a.setTarget(targetAll)
	}
	if m.Item(locales.SendToChoose()).Radio(mode == targetChosen).Chosen() {
		a.sendTo.picking = anchor
	}
}

// warnTarget colors a target control in the warning color while the
// target is not the current session.
func (a *App) warnTarget(e ui.Element) {
	if a.sendTo.mode != targetCurrent {
		e.TextColor(theme.Warning).Border(1, theme.Warning).Background(theme.Warning.Alpha(0.1))
	}
}

// barTarget is the bar's target button, between its input and History:
// an icon for the current session, else what the target is.
func (a *App) barTarget(c *ui.Context, s *Session, live bool) {
	anchor := "bar-" + strconv.FormatInt(s.ID, 10)
	label := a.targetLabel(true)
	b := ui.Button(c, "").Label(locales.SendToTip(label)).Tooltip(locales.SendToTip(label)).
		Shrink(0).Disabled(!live).Gap(4)
	a.warnTarget(b)
	b.Children(func() {
		ui.Icon(c, icons.Get("send")).FontSize(13)
		if a.sendTo.mode != targetCurrent {
			ui.Text(c, label).SingleLine()
		}
	})
	b.Menu(func(m *ui.Menu) { a.targetMenu(m, anchor) })
	a.targetPicker(c, b, anchor)
}

// quickTarget is a Send to drop-down of the Commands tab: its toolbar's,
// anchor "quick", or its command editor's, "editor".
func (a *App) quickTarget(c *ui.Context, anchor string) {
	widgets.Muted(c, locales.SendTo()).SingleLine().Shrink(0)
	b := ui.MenuButton(c, a.targetLabel(false), func(m *ui.Menu) { a.targetMenu(m, anchor) }).Shrink(0)
	a.warnTarget(b)
	a.targetPicker(c, b, anchor)
}

// targetPicker is Choose sessions' popover below anchor, when it is the
// one open: each session with a check box, its status dot and its name.
// Checking one makes the target the sessions checked; closed with none,
// the target stays what it was.
func (a *App) targetPicker(c *ui.Context, anchor ui.Element, id string) {
	st := &a.sendTo
	if st.picking != id {
		return
	}
	open := true
	ui.Popover(c, anchor, &open, func() {
		ui.Column(c).Width(280).Gap(4).Children(func() {
			widgets.Muted(c, locales.SendTo()).FontSize(12).Padding(0, 4)
			sessions := a.targetSessions()
			if len(sessions) == 0 {
				widgets.Muted(c, locales.SendToNoSessions()).Padding(4)
				return
			}
			ui.Scroll(c).MaxHeight(300).Gap(2).Children(func() {
				for _, s := range sessions {
					a.pickerRow(c, s)
				}
			})
		})
	})
	if !open {
		st.picking = ""
		if st.mode == targetChosen && len(a.chosenSessions()) == 0 {
			a.setTarget(targetCurrent)
		}
	}
}

// pickerRow is s's row of the popover; a click on it, as on its box,
// checks it or unchecks it.
func (a *App) pickerRow(c *ui.Context, s *Session) {
	st := &a.sendTo
	on := st.mode == targetChosen && st.chosen[s.ID]
	was := on
	row := ui.Row(c.Key("pick-"+strconv.FormatInt(s.ID, 10))).Gap(8).Padding(4, 6).Radius(4).AlignItems(ui.Center).
		Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(theme.SurfaceHover)
	}
	row.Children(func() {
		ui.Checkbox(c, &on, "").Label(locales.SendToTip(s.label())).Shrink(0)
		widgets.StatusDot(c, s.statusColor(theme))
		ui.Text(c, s.label()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
	if row.Clicked() {
		on = !on
	}
	if on == was {
		return
	}
	if st.mode != targetChosen {
		st.chosen = map[int64]bool{}
	}
	if st.chosen == nil {
		st.chosen = map[int64]bool{}
	}
	if on {
		st.chosen[s.ID] = true
	} else {
		delete(st.chosen, s.ID)
	}
	st.mode, st.confirmed = targetChosen, false
}

// sendAskDialog is the question before the first send to a new target:
// the sessions it goes to, Send and Cancel.
func (a *App) sendAskDialog(c *ui.Context) {
	q := a.sendTo.ask
	if q == nil {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(420).Gap(12).Children(func() {
			ui.Text(c, locales.SendToConfirm(len(q.sessions))).FontSize(16).Bold()
			ui.Scroll(c).MaxHeight(240).Gap(4).Padding(8).Radius(6).Background(colors.note).Children(func() {
				for _, s := range q.sessions {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						widgets.StatusDot(c, s.statusColor(theme))
						ui.Text(c, s.label()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
						widgets.Muted(c, s.Host.Target()).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
					})
				}
			})
			widgets.Muted(c, locales.SendToConfirmNote()).FontSize(12)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.Cancel()).Clicked() {
					a.sendTo.ask = nil
				}
				if ui.PrimaryButton(c, locales.QuickSend()).AutoFocus().Clicked() {
					a.sendTo.ask = nil
					q.send()
				}
			})
		})
	})
	if !open {
		a.sendTo.ask = nil
	}
}
