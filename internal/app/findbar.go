package app

import (
	"github.com/egoist/mygo/ui"
)

// findState is a pane's find bar: whether it shows, what it looks for,
// and the match chosen, which the terminal's search keeps.
type findState struct {
	open  bool
	query string
	match int // the bar's, 0 the newest match
	// opened counts the times the bar was asked to open, so that the key
	// focuses its field again while it shows.
	opened int
}

// openFind shows the find bar of the selected session's focused pane,
// over its terminal, its field focused with the last text selected.
func (a *App) openFind() {
	if s := a.current(); s != nil && s.term() != nil {
		p := s.pane()
		p.find.open = true
		p.find.opened++
	}
}

// findBar shows p's find bar over its terminal at the top right, as
// Ghostty's: MyGo's find bar, driving the terminal's search, which
// highlights the matches and counts them. Enter goes to the next match,
// up into the scrollback, Shift+Enter back down, as do F3 and Shift+F3
// (Cmd+G and Shift+Cmd+G on macOS); Escape and Done close it and give the
// keyboard back to the terminal, view. It comes after the terminal's view
// in the frame, which drives the search, so that the count it shows is
// the frame's.
func (a *App) findBar(c *ui.Context, p *pane, view ui.Element) {
	f := &p.find
	if !f.open {
		return
	}
	term := p.term
	st := term.SearchStatus()
	f.match = max(st.Selected-1, 0)
	before := f.match
	var bar ui.Element
	ui.Box(c.Key(f.opened)).Absolute().Top(0).Right(16).Width(460).MaxWidthPercent(92).ZIndex(2).
		Radius(0, 0, 6, 6).Clip().Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
		bar = ui.FindBar(c, &f.open, &f.query, st.Total, &f.match)
	})
	moved := bar.Changed()
	switch {
	case !f.open:
		term.EndSearch()
		view.Focus()
	case f.query != st.Text:
		term.Search(f.query) // "" ends it
	case moved && st.Total > 1:
		// The bar steps round the matches by one: forward is the
		// terminal's next, toward older output.
		if (f.match-before+st.Total)%st.Total == 1 {
			term.SearchNext()
		} else {
			term.SearchPrevious()
		}
	}
}
