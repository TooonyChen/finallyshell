package widgets

import "github.com/egoist/mygo/ui"

// NavPage is a page of NavPages: an entry in its tree, and what it shows.
type NavPage struct {
	// ID names the page, unique among the pages.
	ID string
	// Parent is the ID of the page whose entry this one's sits under, ""
	// at the top. A parent comes before its children in the list.
	Parent string
	// Title is the entry's text and the page's heading, from locales:
	// asked each time it is drawn, so it follows the language.
	Title func() string
	// View builds the page, below its heading; nil shows nothing.
	View func(c *ui.Context)
}

// NavState is the state of NavPages, which the caller keeps: the page
// shown, and the tree's.
type NavState struct {
	// Page is the ID of the page shown: "", or an ID no page has, is the
	// first page. Setting it shows that page.
	Page string

	tree   ui.OutlineState[string]
	row    int
	opened bool // the entries with children opened, the first time
}

// NavWidth is the width of the tree of NavPages, in DIPs.
const NavWidth = 168

// NavPages is a window of pages, as a preferences window's: a tree of
// their titles on the left, the page chosen on the right, under its
// title. Every entry with children starts open.
func NavPages(c *ui.Context, s *NavState, pages []NavPage) ui.Element {
	byID := make(map[string]*NavPage, len(pages))
	children := map[string][]string{}
	var roots []string
	for i := range pages {
		p := &pages[i]
		byID[p.ID] = p
		if p.Parent == "" {
			roots = append(roots, p.ID)
		} else {
			children[p.Parent] = append(children[p.Parent], p.ID)
		}
	}
	if !s.opened {
		s.opened = true
		for id := range children {
			s.tree.Open.Add(id)
		}
	}
	if byID[s.Page] == nil && len(pages) > 0 {
		s.Page = pages[0].ID
	}
	s.row = navRow(s.Page, roots, children, &s.tree.Open)
	s.tree.List.Selected = &s.row

	t := c.Theme()
	return ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Outline(c, &s.tree, roots, func(id string) []string {
			return children[id]
		}, func(id string) {
			ui.Text(c, byID[id].Title()).SingleLine()
		}).Width(NavWidth).Shrink(0).Padding(6, 4).
			BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
		if s.row >= 0 && s.row < s.tree.Rows() {
			s.Page = s.tree.Item(s.row)
		}
		page := byID[s.Page]
		ui.Column(c.Key(s.Page)).Grow(1).MinWidth(0).Children(func() {
			if page == nil {
				return
			}
			ui.Text(c, page.Title()).FontSize(15).Bold().Padding(14, 18, 8, 18)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Padding(4, 18, 14, 18).Gap(10).Children(func() {
					if page.View != nil {
						page.View(c)
					}
				})
			})
		})
	})
}

// navRow is the row of the tree that shows the page of id, as the tree
// opens the entries in open; -1 when it shows none, as under a closed
// entry.
func navRow(id string, roots []string, children map[string][]string, open *ui.Selection[string]) int {
	row := 0
	var walk func(ids []string) int
	walk = func(ids []string) int {
		for _, x := range ids {
			if x == id {
				return row
			}
			row++
			if open.Has(x) {
				if r := walk(children[x]); r >= 0 {
					return r
				}
			}
		}
		return -1
	}
	return walk(roots)
}
