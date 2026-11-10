package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
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
// focuses the pane on its way.
func (a *App) paneBox(c *ui.Context, s *Session, p *pane) {
	focused := p == s.panes.focus
	box := a.terminalBackdrop(c.Key(p.id), 0).Fill().Padding(4, paneBorder, paneBorder, 6)
	box.ObserveInput(func(ev ui.InputEvent) {
		if ev.Kind == ui.InputPointerDown {
			a.focusPane(s, p, false)
		}
	})
	accent := theme.Accent
	box.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		if !focused {
			pt.Fill(r, paneDim, 0)
			return
		}
		h := float32(paneBorder) / 2
		pt.Stroke(ui.Rect{X: r.X + h, Y: r.Y + h, W: r.W - paneBorder, H: r.H - paneBorder}, accent, 0, paneBorder)
	})
	box.Children(func() { a.paneTerminal(c, s, p) })
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
// focused pane, zooming it, equalizing them and closing it, with the
// keys that do the same.
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
