package app

import (
	"context"
	"sync/atomic"

	"finallyshell/internal/monitor"

	"github.com/egoist/mygo/plugins/terminal"
)

// A session's terminal splits into panes, as a tab of a terminal app
// does: each pane is a shell of its own, on the session's connection (a
// channel more on its SSH client: no new login), or for a local terminal
// another run of its program. The panes are the leaves of a binary tree,
// paneTree, whose inner nodes split their room between two children,
// side by side or one above the other, at the share their divider sets.
//
// What is per terminal is the pane's: its terminal, its find bar, the
// follower of its shell's folder and foreground program (shelldir.go),
// a local program's title. What is per connection stays the session's:
// the client, SFTP, the monitor, the tunnels, the command bar. The
// focused pane is the session's terminal (Session.term): the command
// bar, the Commands tab and the command editor send to it, the file
// panel follows its folder, the row shows its agent.
//
// The tree is kept as panes close and come back: a pane can be taken
// out (paneTree.remove) and put back beside another (paneTree.insert),
// which is how dragging a pane will move it. Nothing of it is saved
// across runs.

// splitDir is how an inner node of the tree splits its room.
type splitDir uint8

const (
	splitRight splitDir = iota // side by side, the first on the left
	splitDown                  // one above the other, the first on top
)

// paneDir is a direction focus moves in, from the focused pane.
type paneDir uint8

const (
	paneLeft paneDir = iota
	paneRight
	paneUp
	paneDown
)

var nextPaneID atomic.Int64

// pane is one terminal of a session.
type pane struct {
	id    int64
	term  *terminal.Terminal // nil until its shell opens, and once disconnected
	find  findState          // the find bar over its terminal
	shell shellDirState      // its shell's folder and foreground program
	title string             // the title a local terminal's program set
	// stop ends its shell's follower: nil while none runs.
	stop context.CancelFunc
	// pick is which of the connection's shells this pane's is, as its
	// follower reads it from the server (monitor.ShellPick): its place,
	// from 1, among the session's open shells by the order they opened in,
	// in the high 32 bits, and their number in the low. The UI thread
	// sets it as panes open and close; the follower reads it.
	pick atomic.Uint64
}

func newPane() *pane { return &pane{id: nextPaneID.Add(1)} }

// shellPick is the pick the pane's follower reads its shell by.
func (p *pane) shellPick() monitor.ShellPick {
	v := p.pick.Load()
	return monitor.ShellPick{Index: int(v >> 32), Count: int(v & 0xffffffff)}
}

// paneNode is a node of a pane tree: a leaf holding a pane, or an inner
// node splitting its room between first and second.
type paneNode struct {
	id     int64 // the key of its split's element
	parent *paneNode
	pane   *pane // a leaf's
	dir    splitDir
	// pct is first's share of the room, in percent, as the divider sets
	// it.
	pct           float32
	first, second *paneNode
}

// paneTree is a session's panes.
type paneTree struct {
	root  *paneNode
	focus *pane // nil while the tree is empty
	// zoomed shows the focused pane alone, filling the terminal area.
	zoomed bool
	// refocus has the focused pane's terminal take the keyboard in the
	// next frame, after the tree or its focus changed.
	refocus bool
	// order are the panes whose shells are open, by the order they
	// opened in: the order of the server's shells (pane.pick).
	order []*pane
}

// reset makes p the tree's only pane, focused.
func (t *paneTree) reset(p *pane) {
	*t = paneTree{root: &paneNode{id: p.id, pane: p}, focus: p}
}

// panes are the tree's panes in order: left to right, top to bottom.
func (t *paneTree) panes() []*pane {
	var ps []*pane
	var walk func(n *paneNode)
	walk = func(n *paneNode) {
		switch {
		case n == nil:
		case n.pane != nil:
			ps = append(ps, n.pane)
		default:
			walk(n.first)
			walk(n.second)
		}
	}
	walk(t.root)
	return ps
}

// count is how many panes the tree has.
func (t *paneTree) count() int { return len(t.panes()) }

// leaf is the node holding p, or nil when p is not in the tree.
func (t *paneTree) leaf(p *pane) *paneNode {
	var found *paneNode
	var walk func(n *paneNode)
	walk = func(n *paneNode) {
		switch {
		case n == nil || found != nil:
		case n.pane == p:
			found = n
		default:
			walk(n.first)
			walk(n.second)
		}
	}
	if p != nil {
		walk(t.root)
	}
	return found
}

// has tells whether p is one of the tree's panes.
func (t *paneTree) has(p *pane) bool { return t.leaf(p) != nil }

// replace puts n where old is in the tree.
func (t *paneTree) replace(old, n *paneNode) {
	parent := old.parent
	n.parent = parent
	switch {
	case parent == nil:
		t.root = n
	case parent.first == old:
		parent.first = n
	default:
		parent.second = n
	}
}

// insert puts p beside at, splitting at's room in two as dir says, half
// each: p after at (right of it, or under it), or before it. It reports
// false, changing nothing, when at is not in the tree or p already is.
// Splitting inserts the new pane; moving one removes it, then inserts it
// beside another.
func (t *paneTree) insert(at, p *pane, dir splitDir, before bool) bool {
	n := t.leaf(at)
	if n == nil || p == nil || t.has(p) {
		return false
	}
	leaf := &paneNode{id: p.id, pane: p}
	inner := &paneNode{id: nextPaneID.Add(1), dir: dir, pct: 50, first: n, second: leaf}
	if before {
		inner.first, inner.second = leaf, n
	}
	t.replace(n, inner)
	n.parent, leaf.parent = inner, inner
	return true
}

// remove takes p out of the tree: its sibling takes the room they
// shared. It returns the pane that takes the focus, when p had it: the
// sibling's pane nearest to where p was. Removing the last pane leaves
// the tree empty. It reports nil, changing nothing, when p is not in
// the tree.
func (t *paneTree) remove(p *pane) *pane {
	n := t.leaf(p)
	if n == nil {
		return nil
	}
	t.order = deletePane(t.order, p)
	parent := n.parent
	if parent == nil {
		*t = paneTree{}
		return nil
	}
	sibling, wasFirst := parent.second, true
	if parent.second == n {
		sibling, wasFirst = parent.first, false
	}
	t.replace(parent, sibling)
	if t.focus == p {
		// The sibling's edge that touched p: its first pane when p was
		// before it, else its last.
		ps := (&paneTree{root: sibling}).panes()
		t.focus = ps[len(ps)-1]
		if wasFirst {
			t.focus = ps[0]
		}
		t.zoomed = false
	}
	return t.focus
}

// deletePane is ps without p.
func deletePane(ps []*pane, p *pane) []*pane {
	out := ps[:0]
	for _, q := range ps {
		if q != p {
			out = append(out, q)
		}
	}
	return out
}

// equalize gives the panes the same room: each divider splits its room
// by how many panes lie along its direction on each side, so that three
// panes side by side get a third each.
func (t *paneTree) equalize() {
	// weight is how many panes n lays out along dir.
	var weight func(n *paneNode, dir splitDir) float32
	weight = func(n *paneNode, dir splitDir) float32 {
		if n.pane != nil || n.dir != dir {
			return 1
		}
		return weight(n.first, dir) + weight(n.second, dir)
	}
	var walk func(n *paneNode)
	walk = func(n *paneNode) {
		if n == nil || n.pane != nil {
			return
		}
		a, b := weight(n.first, n.dir), weight(n.second, n.dir)
		n.pct = 100 * a / (a + b)
		walk(n.first)
		walk(n.second)
	}
	walk(t.root)
}

// paneRect is where a pane is in the terminal area, which is the unit
// square.
type paneRect struct{ x, y, w, h float32 }

// rects are where each pane is, as the dividers have them.
func (t *paneTree) rects() map[*pane]paneRect {
	m := map[*pane]paneRect{}
	var walk func(n *paneNode, r paneRect)
	walk = func(n *paneNode, r paneRect) {
		if n == nil {
			return
		}
		if n.pane != nil {
			m[n.pane] = r
			return
		}
		f := n.pct / 100
		if n.dir == splitRight {
			w := r.w * f
			walk(n.first, paneRect{r.x, r.y, w, r.h})
			walk(n.second, paneRect{r.x + w, r.y, r.w - w, r.h})
			return
		}
		h := r.h * f
		walk(n.first, paneRect{r.x, r.y, r.w, h})
		walk(n.second, paneRect{r.x, r.y + h, r.w, r.h - h})
	}
	walk(t.root, paneRect{0, 0, 1, 1})
	return m
}

// neighbor is the pane next to p in direction d, or nil at the edge:
// of the panes beside p on that side, sharing some of its edge, the
// nearest; among those as near, the one most in line with p's middle.
func (t *paneTree) neighbor(p *pane, d paneDir) *pane {
	const eps = 1e-4
	rects := t.rects()
	cur, ok := rects[p]
	if !ok {
		return nil
	}
	var best *pane
	bestGap, bestOff := float32(2), float32(2)
	for _, q := range t.panes() { // in order, for the same choice each time
		if q == p {
			continue
		}
		r := rects[q]
		var gap, overlap, off float32
		switch d {
		case paneLeft:
			gap = cur.x - (r.x + r.w)
		case paneRight:
			gap = r.x - (cur.x + cur.w)
		case paneUp:
			gap = cur.y - (r.y + r.h)
		case paneDown:
			gap = r.y - (cur.y + cur.h)
		}
		if d == paneLeft || d == paneRight {
			overlap = min(cur.y+cur.h, r.y+r.h) - max(cur.y, r.y)
			off = abs32((r.y + r.h/2) - (cur.y + cur.h/2))
		} else {
			overlap = min(cur.x+cur.w, r.x+r.w) - max(cur.x, r.x)
			off = abs32((r.x + r.w/2) - (cur.x + cur.w/2))
		}
		if gap < -eps || overlap <= eps {
			continue
		}
		if gap < bestGap-eps || abs32(gap-bestGap) <= eps && off < bestOff {
			best, bestGap, bestOff = q, gap, off
		}
	}
	return best
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// opened notes that p's shell opened, after those of the panes in
// order, and tells every follower its shell's place.
func (t *paneTree) opened(p *pane) {
	t.order = append(deletePane(t.order, p), p)
	t.renumber()
}

// renumber tells the followers of the panes in order where their shells
// are among the connection's.
func (t *paneTree) renumber() {
	n := uint64(len(t.order))
	for i, p := range t.order {
		p.pick.Store(uint64(i+1)<<32 | n)
	}
}

// pane is s's focused pane: nil while it has none, as a new tab, a
// panel row, or a session that never connected.
func (s *Session) pane() *pane { return s.panes.focus }

// term is the terminal of s's focused pane, which takes what is sent to
// the session: nil while it has none.
func (s *Session) term() *terminal.Terminal {
	if p := s.panes.focus; p != nil {
		return p.term
	}
	return nil
}

// setTerm gives s's focused pane term, making it a pane first when s
// has none.
func (s *Session) setTerm(term *terminal.Terminal) {
	if s.panes.focus == nil {
		s.panes.reset(newPane())
	}
	s.panes.focus.term = term
}

// findOpen tells whether the find bar of s's focused pane shows.
func (s *Session) findOpen() bool {
	p := s.pane()
	return p != nil && p.find.open
}

// programTitle is the title the program of s's focused pane set, for a
// local terminal: "" when it set none.
func (s *Session) programTitle() string {
	if p := s.pane(); p != nil {
		return p.title
	}
	return ""
}

// split tells whether s shows more than one pane.
func (s *Session) split() bool {
	r := s.panes.root
	return r != nil && r.pane == nil
}
