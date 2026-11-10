package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"finallyshell/internal/icons"
	"finallyshell/locales"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
)

// The panes' limits: a divider's drag leaves each pane at least this
// many DIPs, so that none can be dragged away.
var (
	paneLimitsRight = splitLimits{first: 120, second: 120, percent: true}
	paneLimitsDown  = splitLimits{first: 60, second: 60, vertical: true, percent: true}
)

// paneDim is the veil over the panes without the focus, while a session
// shows several.
var paneDim = ui.RGBA(0, 0, 0, 0.22)

// paneBorder is how wide the focused pane's border is.
const paneBorder = 2

// connectionProbeTimeout is how long a pane whose shell ended waits to
// hear that the connection lives, before it takes the connection for
// lost; tests shorten it.
var connectionProbeTimeout = 10 * time.Second

// canSplit tells whether s's focused pane can split: a session whose
// shell runs.
func canSplit(s *Session) bool {
	return s != nil && !s.isNewTab() && !s.isPanelTab() && s.status == statusConnected && s.term() != nil
}

// splitPane opens a new pane beside s's focused pane, as dir says, and
// focuses it: another shell on the session's connection, with the
// settings of the first, or another run of a local terminal's program.
// The pane comes once the shell is there; a shell the server refuses, as
// past its MaxSessions, adds nothing, and a toast says why.
func (a *App) splitPane(s *Session, dir splitDir) {
	if !canSplit(s) {
		return
	}
	at, p := s.pane(), newPane()
	if s.isLocal() {
		term, err := a.openLocalPane(s, p)
		a.paneOpened(s, at, p, dir, nil, shellSpec{}, term, err)
		return
	}
	client, spec := s.client, a.shellSpec(s.Host)
	go func() {
		term, err := a.openPaneShell(s, p, client, spec)
		a.post(func() { a.paneOpened(s, at, p, dir, client, spec, term, err) })
	}()
}

// paneOpened puts p beside at, as splitPane asked, now that its shell on
// client opened (term) or failed (err). A session that closed or
// reconnected meanwhile does not take it.
func (a *App) paneOpened(s *Session, at, p *pane, dir splitDir, client *ssh.Client, spec shellSpec, term *terminal.Terminal, err error) {
	if err != nil {
		a.notify(paneError(err))
		return
	}
	if !slices.Contains(a.sessions, s) || s.status != statusConnected || s.client != client || s.pane() == nil {
		term.Close()
		return
	}
	if !s.panes.has(at) { // closed meanwhile
		at = s.pane()
	}
	s.panes.insert(at, p, dir, false)
	p.term = term
	s.panes.focus, s.panes.zoomed, s.panes.refocus = p, false, true
	s.panes.opened(p)
	if !s.isLocal() {
		a.startPane(s, p, spec)
	}
}

// paneError says why a new pane did not open: the server refused another
// shell, or what else went wrong.
func paneError(err error) string {
	var refused *ssh.OpenChannelError
	if errors.As(err, &refused) && refused.Reason == ssh.Prohibited {
		return locales.ErrPaneRefused()
	}
	return describeError(err)
}

// startPane follows the folder and the foreground program of p's shell,
// which just opened on s's connection, as long as both last.
func (a *App) startPane(s *Session, p *pane, spec shellSpec) {
	parent := s.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := context.WithCancel(parent)
	p.stop = stop
	a.startShellDir(ctx, s, p, s.client, p.term, spec.exec)
	a.updateTerminal(p.term, spec.topts)
}

// end ends p's shell and its follower, keeping its place in the tree.
func (p *pane) end() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	if p.term != nil {
		p.term.Close()
		p.term = nil
	}
}

// paneExited takes the end of the shell of p, one of s's panes. While
// the session has other panes, a shell that exited closes its pane;
// one that ended as the connection dropped takes the session down with
// the others, which keep their places for Reconnect: a request on the
// connection tells the two apart. The last pane's end is the session's,
// as with a single terminal.
func (a *App) paneExited(s *Session, p *pane) {
	t := p.term
	if t == nil || !s.panes.has(p) || !slices.Contains(a.sessions, s) || s.status != statusConnected {
		return // ended by the app, or the session is down already
	}
	select {
	case <-t.Done():
	default:
		return // a terminal opened since, in its place
	}
	switch {
	case s.isLocal() && s.split():
		a.closePane(s, p)
	case s.isLocal():
		s.status, s.err = statusClosed, errShellExited
	case s.cancel == nil: // the user disconnected
	case !s.split():
		a.connectionLost(s)
	default:
		client := s.client
		go func() {
			alive := connectionAlive(client)
			a.post(func() {
				if s.client != client || s.status != statusConnected {
					return
				}
				switch {
				case !alive:
					a.connectionLost(s)
				case p.term == t && s.split():
					a.closePane(s, p)
				case p.term == t:
					a.connectionLost(s)
				}
			})
		}()
	}
}

// connectionLost marks s lost: its connection dropped.
func (a *App) connectionLost(s *Session) {
	s.status, s.err = statusClosed, errConnectionLost
	s.stopTunnels()
}

// connectionAlive asks the server for an answer on client's connection:
// any answer, even a refusal, tells that it lives. It blocks.
func connectionAlive(client *ssh.Client) bool {
	if client == nil {
		return false
	}
	answered := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		answered <- err
	}()
	select {
	case err := <-answered:
		return err == nil
	case <-time.After(connectionProbeTimeout):
		return false
	}
}

// closePane closes p, one of s's panes, ending its shell: the pane
// beside it takes its room, and the focus when p had it. The last pane
// stays: closing it is closing the session.
func (a *App) closePane(s *Session, p *pane) {
	if !s.split() || !s.panes.has(p) {
		return
	}
	focused := p == s.pane()
	s.panes.remove(p)
	s.panes.renumber()
	p.end()
	if focused {
		s.panes.refocus = true
		a.followShellDir(s)
	}
}

// closeCurrent is the Close tab key: it closes the focused pane of the
// session shown while it has several, as Ghostty does, and the session
// with its last.
func (a *App) closeCurrent() {
	s := a.current()
	switch {
	case s == nil:
	case s.split() && !s.isPanelTab():
		a.closePane(s, s.pane())
	default:
		a.closeSession(s)
	}
}

// focusPane gives p the focus among s's panes; the file panel goes to
// its shell's folder, as it follows the focused pane's. keyboard gives
// its terminal the keyboard too, which a click gives it by itself.
func (a *App) focusPane(s *Session, p *pane, keyboard bool) {
	if p == nil || p == s.pane() || !s.panes.has(p) {
		return
	}
	s.panes.focus = p
	s.panes.refocus = s.panes.refocus || keyboard
	a.followShellDir(s)
}

// movePaneFocus moves the focus of s's panes to the one next to it in
// direction d, out of zoom.
func (a *App) movePaneFocus(s *Session, d paneDir) {
	if s == nil || !s.split() {
		return
	}
	if q := s.panes.neighbor(s.pane(), d); q != nil {
		s.panes.zoomed = false
		a.focusPane(s, q, true)
	}
}

// toggleZoom shows s's focused pane alone, filling the terminal area, or
// all its panes again.
func (a *App) toggleZoom(s *Session) {
	if s == nil || !s.split() {
		return
	}
	s.panes.zoomed = !s.panes.zoomed
	s.panes.refocus = true
}

// notify shows msg in a toast as the window builds its next frame: for
// what goroutines report, which have no frame to show it in.
func (a *App) notify(msg string) {
	a.notices = append(a.notices, msg)
	a.redraw()
}

// showNotices shows the toasts notify asked for.
func (a *App) showNotices(c *ui.Context) {
	for _, msg := range a.notices {
		c.Toast(msg)
	}
	a.notices = nil
}

// paneArea shows s's panes: its terminal alone while it has one, or
// while one is zoomed; else the tree of its splits.
func (a *App) paneArea(c *ui.Context, s *Session) {
	t := &s.panes
	if a.moving != nil && (!t.has(a.moving) || !s.split() || t.zoomed) {
		a.moving = nil // closed, or another session shows, while it dragged
	}
	if !s.split() || t.zoomed {
		a.terminalBackdrop(c, 0).Grow(1).Padding(4, 0, 0, 6).Children(func() {
			a.paneTerminal(c, s, t.focus)
		})
		return
	}
	// The dividers in the terminal's colors, a faint line between the
	// panes rather than the window's border color across a dark terminal.
	th := *c.Theme()
	th.Border = a.termColors().Foreground.Alpha(0.16)
	ui.Themed(c.Key("panes"), &th).Grow(1).Background(ui.Transparent).Children(func() {
		a.paneNode(c, s, t.root)
	})
}

// paneNode shows n of s's pane tree: a pane, or a split of its two
// children whose divider drags, and equalizes them on a double click.
func (a *App) paneNode(c *ui.Context, s *Session, n *paneNode) {
	if n.pane != nil {
		a.paneBox(c, s, n.pane)
		return
	}
	first := func() { a.paneNode(c, s, n.first) }
	second := func() { a.paneNode(c, s, n.second) }
	var split ui.Element
	limits := paneLimitsRight
	if n.dir == splitRight {
		split = ui.SplitPercent(c.Key(n.id), &n.pct, first, second)
	} else {
		split = ui.SplitVerticalPercent(c.Key(n.id), &n.pct, first, second)
		limits = paneLimitsDown
	}
	split.SplitReset(50).Fill()
	clampSplit(split, &n.pct, limits)
}

// paneBox is p, one of the panes s shows side by side: the focused one
// with a border in the accent color, the others under a veil. Both are
// painted over the terminal and take no input: a press anywhere in the
// pane reaches the terminal, selection and mouse reports included, and
// focuses the pane on its way. Its grip, at the middle of its top edge,
// shows while the pointer is near it, and drags the pane onto another,
// which shows the four sides it can go to.
func (a *App) paneBox(c *ui.Context, s *Session, p *pane) {
	focused := p == s.panes.focus
	box := a.terminalBackdrop(c.Key(p.id), 0).Fill().Padding(4, paneBorder, paneBorder, 6)
	box.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputPointerDown {
			a.focusPane(s, p, false)
		}
	})
	// Where the pointer is over the pane, which the terminal inside takes
	// the input of: for the grip's strip, and the side a pane dragged
	// over it goes to.
	bounds := box.Bounds()
	px, py, hover := box.PointerPosition()
	side := dropSide(px, py, bounds.W, bounds.H)
	var over *pane // another of s's panes, dragged over p
	if d, ok := ui.DragOver[paneDrag](box); ok && d.s == s && d.p != p && s.panes.has(d.p) {
		over = d.p
	}
	if d, ok := ui.Drop[paneDrag](box); ok && d.s == s {
		a.movePane(s, d.p, p, side)
	}
	moves := over != nil && !s.panes.stays(over, p, side)
	moving := false // p is dragged, which its grip tells below
	accent, bg, fg := theme.Accent, a.termColors().Background, a.termColors().Foreground
	box.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		switch {
		case moving:
			pt.Fill(r, paneMovingDim, 0)
		case !focused:
			pt.Fill(r, paneDim, 0)
		default:
			h := float32(paneBorder) / 2
			pt.Stroke(ui.Rect{X: r.X + h, Y: r.Y + h, W: r.W - paneBorder, H: r.H - paneBorder}, accent, 0, paneBorder)
		}
		if over != nil {
			card := ui.Rect{X: r.X + px - paneCardHotspot, Y: r.Y + py - paneCardHotspot, W: paneCardW + 2*paneCardHotspot, H: 72}
			drawDropZones(pt, r, side, moves, card, accent, bg, fg)
		}
	})
	strip := hover && py < gripStripH && abs32(px-bounds.W/2) < gripStripW/2
	show := strip && over == nil && a.moving == nil && !p.find.open
	box.Children(func() {
		a.paneTerminal(c, s, p)
		moving = a.paneGrip(c, s, p, show)
	})
}

// paneMovingDim is the veil over the pane its grip drags, darker than
// paneDim.
var paneMovingDim = ui.RGBA(0, 0, 0, 0.45)

// The strip at the middle of a pane's top edge that shows its grip while
// the pointer is in it, and the grip, in DIPs: the grip alone takes the
// pointer, so that the terminal takes it in the rest of the strip, as a
// full-screen program's mouse reports.
const (
	gripStripW, gripStripH = 160, 24
	gripW, gripH           = 40, 10
)

// paneCardW is how wide the copy of a dragged pane is (paneCard), and
// paneCardHotspot about how far its corner is from the pointer, which
// pressed the grip.
const paneCardW, paneCardHotspot = 260, 24

// paneDrag is a pane dragged by its grip onto another of its session's.
type paneDrag struct {
	s *Session
	p *pane
}

// paneGrip is the grip of p, one of s's panes, while show says or it
// is pressed (App.moving): a pill a press drags p by, which follows the pointer as a
// card of p (paneCard) until it is dropped on another pane (paneBox),
// or Esc gives it up. It reports whether it drags p.
func (a *App) paneGrip(c *ui.Context, s *Session, p *pane, show bool) bool {
	if !show && a.moving != p {
		return false
	}
	dragging := false
	ui.Row(c.Key("grip")).Absolute().Top(0).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
		g := ui.Box(c).Drag(paneDrag{s, p}).KeepFocus().Label(locales.PaneGrip())
		// The grip stays while it is pressed, after Esc too: the press
		// ends with the element, and the drag with the press.
		_, held := g.PressedButton()
		dragging = g.Dragging()
		switch {
		case (held || dragging) && a.moving != p:
			a.moving, a.movingLine = p, ""
			if p.term != nil {
				a.movingLine = lastLine(p.term.Text())
			}
		case !held && !dragging && a.moving == p:
			a.moving, a.movingLine = nil, ""
		}
		if dragging {
			// Only its copy shows, under the pointer.
			g.Opacity(0).Cursor(ui.CursorGrabbing).Children(func() { a.paneCard(c, s, p) })
			return
		}
		bg := theme.Accent.Alpha(0.6)
		if g.Hovered() {
			bg = theme.Accent
		}
		g.Size(gripW, gripH).Cursor(ui.CursorGrab).Tooltip(locales.PaneGrip()).Draw(func(pt *ui.Painter, r ui.Rect) {
			pt.Fill(r, bg, r.H/2)
			// Two rows of three dots.
			dot := ui.RGBA(255, 255, 255, 0.9)
			for i := range 3 {
				for j := range 2 {
					x := r.X + r.W/2 + float32(i-1)*4
					y := r.Y + r.H/2 + float32(j)*4 - 2
					pt.Fill(ui.Rect{X: x - 1, Y: y - 1, W: 2, H: 2}, dot, 1)
				}
			}
		})
	})
	return dragging
}

// paneCard is the copy of p, one of s's panes, that follows the pointer
// while p is dragged: its number among the panes, the title its program
// set or the session's name, and the last line on its screen.
func (a *App) paneCard(c *ui.Context, s *Session, p *pane) {
	colors := a.termColors()
	title := s.label()
	if p.term != nil {
		if t := p.term.Title(); t != "" {
			title = t
		}
	}
	n := slices.Index(s.panes.panes(), p) + 1
	ui.Column(c).Width(paneCardW).Gap(4).Padding(8, 12).Radius(6).Background(colors.Background).
		Border(2, theme.Accent).Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
		ui.Text(c, paneNumber(n)+"  "+title).FontSize(13).TextColor(colors.Foreground).SingleLine().Ellipsis("…")
		if a.movingLine != "" {
			ui.Text(c, a.movingLine).Font("monospace").FontSize(12.5).TextColor(colors.Foreground.Alpha(0.8)).SingleLine().Ellipsis("…")
		}
	})
}

// paneNumber is n as a circled digit, ① to ⑳, or the number past them.
func paneNumber(n int) string {
	if n >= 1 && n <= 20 {
		return string(rune('①' + n - 1))
	}
	return strconv.Itoa(n)
}

// lastLine is the last line of text with more than spaces, without the
// spaces at its end.
func lastLine(text string) string {
	for text != "" {
		i := strings.LastIndexByte(text, '\n')
		line := strings.TrimRight(text[i+1:], " \t\r")
		if strings.TrimSpace(line) != "" {
			return line
		}
		if i < 0 {
			break
		}
		text = text[:i]
	}
	return ""
}

// dropSide is the side of a pane w by h DIPs that (x, y) in it is
// nearest to: the pane split by its diagonals, so that every point picks
// one side.
func dropSide(x, y, w, h float32) paneDir {
	if w <= 0 || h <= 0 {
		return paneRight
	}
	u, v := x/w, y/h
	switch m := min(u, 1-u, v, 1-v); m {
	case u:
		return paneLeft
	case 1 - u:
		return paneRight
	case v:
		return paneUp
	}
	return paneDown
}

// drawDropZones paints the four sides of r, a pane's box, that a pane
// dragged over it can go to, over its terminal, muted: side, the one
// under the pointer, lit up with what dropping does, unless it would
// change nothing (moves false); the others outlined, with their names.
// The names keep out of card, where the dragged pane's copy is.
func drawDropZones(pt *ui.Painter, r ui.Rect, side paneDir, moves bool, card ui.Rect, accent, bg, fg ui.Color) {
	const gap = 8
	in := ui.Rect{X: r.X + gap, Y: r.Y + gap, W: r.W - 2*gap, H: r.H - 2*gap}
	sw, th := in.W*0.22, in.H*0.22
	mid := ui.Rect{X: in.X + sw + gap, W: in.W - 2*sw - 2*gap, H: th}
	zones := [...]ui.Rect{
		paneLeft:  {X: in.X, Y: in.Y, W: sw, H: in.H},
		paneRight: {X: in.X + in.W - sw, Y: in.Y, W: sw, H: in.H},
		paneUp:    {X: mid.X, Y: in.Y, W: mid.W, H: th},
		paneDown:  {X: mid.X, Y: in.Y + in.H - th, W: mid.W, H: th},
	}
	names := [...]func() string{paneLeft: locales.DropZoneLeft, paneRight: locales.DropZoneRight, paneUp: locales.DropZoneUp, paneDown: locales.DropZoneDown}
	drops := [...]func() string{paneLeft: locales.DropOnLeft, paneRight: locales.DropOnRight, paneUp: locales.DropOnUp, paneDown: locales.DropOnDown}
	pt.Fill(r, bg.Alpha(0.8), 0)
	pt.Fill(r, accent.Alpha(0.06), 0)
	for d, z := range zones {
		if z.W <= 0 || z.H <= 0 {
			continue
		}
		label, color := names[d](), fg.Alpha(0.75)
		if paneDir(d) == side && moves {
			pt.Fill(z, accent.Alpha(0.7), 6)
			pt.Stroke(z, accent, 6, 2)
			label, color = drops[d](), ui.RGB(255, 255, 255)
		} else {
			pt.Fill(z, accent.Alpha(0.12), 6)
			pt.StrokeDashed(z, accent.Alpha(0.9), 6, 1.5)
		}
		span := ui.Span{Text: label, Size: 13, Color: color}
		w, h := pt.MeasureText(z.W-12, span)
		at := labelSpot(z, w, h, card)
		pt.RichText(at.X, at.Y, z.W-12, span)
	}
}

// labelSpot is where a label w by h goes in zone z: its middle, else a
// quarter of the way from an end, the first spot clear of card.
func labelSpot(z ui.Rect, w, h float32, card ui.Rect) ui.Rect {
	place := func(fx, fy float32) ui.Rect {
		return ui.Rect{X: z.X + z.W*fx - w/2, Y: z.Y + z.H*fy - h/2, W: w, H: h}
	}
	spots := []ui.Rect{place(0.5, 0.5)}
	if z.H > z.W {
		spots = append(spots, place(0.5, 0.2), place(0.5, 0.8))
	} else {
		spots = append(spots, place(0.2, 0.5), place(0.8, 0.5))
	}
	for _, s := range spots {
		if s.X+s.W <= card.X || card.X+card.W <= s.X || s.Y+s.H <= card.Y || card.Y+card.H <= s.Y {
			return s
		}
	}
	return spots[0]
}

// movePane moves p, one of s's panes, to side d of at, another, as
// dropping its grip there does: p takes the focus, and the file panel
// goes to its shell's folder.
func (a *App) movePane(s *Session, p, at *pane, d paneDir) {
	if s.split() && s.panes.move(p, at, d) {
		a.followShellDir(s)
	}
}

// swapPane trades the place of s's focused pane with the pane next to it
// in direction d, as the Move pane keys do.
func (a *App) swapPane(s *Session, d paneDir) {
	if s != nil && s.split() {
		s.panes.swap(s.pane(), d)
	}
}

// paneTerminal is the terminal of p, one of s's panes, with its find
// bar; the focused one's takes the keyboard as the panes or the command
// bar ask.
func (a *App) paneTerminal(c *ui.Context, s *Session, p *pane) {
	if p == nil || p.term == nil {
		return
	}
	view := terminal.View(c, p.term).Fill()
	focused := p == s.panes.focus
	if focused {
		view.AutoFocus()
		if s.panes.refocus {
			view.Focus()
			s.panes.refocus = false
		}
	}
	a.findBar(c, p, view)
	if focused {
		a.barFocus(s, view)
	}
}

// paneMenu is the panes' part of the menu of s's row: splitting the
// focused pane, zooming it, equalizing them, trading its place with a
// neighbor's and closing it, with the keys that do the same.
//
// MyGo's terminal view builds its own context menu (Copy, Paste, Select
// All, Clear), which is the one a right-click in the terminal opens, the
// innermost element's: an app cannot add to it, so these items are on
// the session's row.
func (a *App) paneMenu(m *ui.Menu, s *Session) {
	item := func(label string, id shortcutID) *ui.MenuItem {
		it := m.Item(label)
		if k := a.shortcut(id); !k.none() {
			it.Shortcut(k.Mods, k.Key)
		}
		return it
	}
	can, split := canSplit(s), s.split()
	if item(locales.SplitRight(), scSplitRight).Disabled(!can).Chosen() {
		a.splitPane(s, splitRight)
	}
	if item(locales.SplitDown(), scSplitDown).Disabled(!can).Chosen() {
		a.splitPane(s, splitDown)
	}
	m.Separator()
	if item(locales.ZoomPane(), scZoomPane).Checked(split && s.panes.zoomed).Disabled(!split).Chosen() {
		a.toggleZoom(s)
	}
	if m.Item(locales.EqualizePanes()).Disabled(!split).Chosen() {
		s.panes.equalize()
	}
	m.Submenu(locales.MovePane(), func(m *ui.Menu) {
		for _, mv := range []struct {
			label func() string
			id    shortcutID
			d     paneDir
		}{
			{locales.MovePaneLeft, scMovePaneLeft, paneLeft},
			{locales.MovePaneRight, scMovePaneRight, paneRight},
			{locales.MovePaneUp, scMovePaneUp, paneUp},
			{locales.MovePaneDown, scMovePaneDown, paneDown},
		} {
			it := m.Item(mv.label())
			if k := a.shortcut(mv.id); !k.none() {
				it.Shortcut(k.Mods, k.Key)
			}
			if it.Disabled(!split || s.panes.neighbor(s.pane(), mv.d) == nil).Chosen() {
				a.swapPane(s, mv.d)
			}
		}
	}).Disabled(!split)
	if item(locales.ClosePane(), scCloseTab).Disabled(!split).Chosen() {
		a.closePane(s, s.pane())
	}
}

// paneBadge is the mark on s's row of how many panes it has, after its
// name, while it has more than one.
func paneBadge(c *ui.Context, s *Session, active bool) {
	if !s.split() {
		return
	}
	n := s.panes.count()
	fg, bg := theme.TextMuted, theme.Border.Alpha(0.6)
	if active {
		fg, bg = theme.AccentText, theme.AccentText.Alpha(0.22)
	}
	label := locales.PaneCount(n)
	ui.Row(c.Key("panes")).Gap(3).AlignItems(ui.Center).Padding(1, 5).Radius(8).Background(bg).Shrink(0).
		Label(label).Tooltip(label).Children(func() {
		ui.Icon(c, icons.Get("layout-grid")).FontSize(11).TextColor(fg)
		ui.Text(c, strconv.Itoa(n)).FontSize(11).TextColor(fg)
	})
}
