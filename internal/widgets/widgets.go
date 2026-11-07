// Package widgets holds small controls the app's views share. They keep
// no state of their own and take their colors from the window's theme.
package widgets

import (
	"math"
	"strconv"

	"finalshelly/internal/icons"

	"github.com/egoist/mygo/ui"
)

// IconButton is a small square button showing an icon, named by label,
// which is also its tooltip. Ask Clicked on what it returns.
func IconButton(c *ui.Context, icon, label string) ui.Element {
	return IconButtonBadge(c, icon, label, 0)
}

// IconButtonBadge is an IconButton with count in a small badge at its
// top right corner, none for 0.
func IconButtonBadge(c *ui.Context, icon, label string, count int) ui.Element {
	b := ui.Box(c.Key(icon+"|"+label)).Size(24, 24).Center().Radius(5).
		Role(ui.RoleButton).Label(label).Tooltip(label).Shrink(0)
	if b.Hovered() {
		b.Background(c.Theme().Text.Alpha(0.07))
	}
	b.Children(func() {
		ui.Icon(c, icons.Get(icon)).FontSize(15).TextColor(c.Theme().TextMuted)
		if count > 0 {
			text := strconv.Itoa(count)
			if count > 99 {
				text = "99+"
			}
			ui.Text(c, text).FontSize(9).Bold().TextColor(c.Theme().AccentText).Background(c.Theme().Accent).
				MinWidth(13).Height(13).Radius(6.5).PaddingX(3).TextAlign(ui.Center).
				Absolute().Top(-3).Right(-4).ZIndex(1).AccessHidden(true)
		}
	})
	return b
}

// Muted is text in the theme's secondary color.
func Muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

// UsageBar is a labeled bar filled to frac, from 0 to 1, with text over
// it, as FinalShell's CPU and memory bars.
func UsageBar(c *ui.Context, label string, frac float64, text string, fill ui.Color) {
	frac = math.Max(0, math.Min(1, frac))
	ui.Row(c).Gap(6).Children(func() {
		ui.Text(c, label).Width(34).Shrink(0).SingleLine()
		ui.Box(c).Grow(1).Height(18).Background(ui.Hex("#e5e7eb")).Border(1, ui.Hex("#d1d5db")).Clip().Children(func() {
			ui.Box(c).Absolute().Top(0).Left(0).Bottom(0).WidthPercent(float32(frac * 100)).Background(fill)
			ui.Row(c).Fill().PaddingX(6).AlignItems(ui.Center).ZIndex(1).Children(func() {
				ui.Text(c, text).FontSize(12).SingleLine()
			})
		})
	})
}

// Sparkline draws series as bars side by side, the newest on the right,
// in a box room for n samples wide, labeled with the largest value.
func Sparkline(c *ui.Context, n int, series [][]float64, colors []ui.Color, format func(float64) string) {
	peak := 0.0
	for _, s := range series {
		for _, v := range s {
			peak = math.Max(peak, v)
		}
	}
	ui.Box(c).Height(46).Background(ui.Hex("#ffffff")).Border(1, ui.Hex("#e5e7eb")).Draw(func(p *ui.Painter, r ui.Rect) {
		if peak <= 0 {
			return
		}
		step := (r.W - 2) / float32(n)
		for si, s := range series {
			for i, v := range s {
				x := r.X + 1 + float32(n-len(s)+i)*step
				h := float32(v/peak) * (r.H - 4)
				w := max(step*0.6, 1)
				if len(series) > 1 {
					w = max(step*0.4, 1)
					x += float32(si) * step * 0.45
				}
				p.Fill(ui.Rect{X: x, Y: r.Y + r.H - 2 - h, W: w, H: h}, colors[si], 0)
			}
		}
	}).Children(func() {
		ui.Text(c, format(peak)).FontSize(10).TextColor(c.Theme().TextMuted).Absolute().Top(1).Left(1).
			Padding(0, 3).Radius(3).Background(ui.RGBA(255, 255, 255, 0.85)).ZIndex(1)
	})
}

// Field is a form row: a label, then what fn builds.
func Field(c *ui.Context, label string, fn func()) {
	ui.Row(c).Gap(10).Children(func() {
		ui.Text(c, label).Width(84).Shrink(0)
		ui.Row(c).Grow(1).Gap(8).Children(fn)
	})
}

// StatusDot is a small colored circle.
func StatusDot(c *ui.Context, color ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(color).Shrink(0)
}

// MiddleEllipsis shortens s by its middle, with an ellipsis, to fit in
// width DIPs at the font size size, as a long path keeps its start and
// its last folder.
func MiddleEllipsis(c *ui.Context, s string, width, size float32) string {
	fits := func(t string) bool {
		w, _ := c.MeasureText(0, ui.Span{Text: t, Size: size})
		return w <= width
	}
	if fits(s) {
		return s
	}
	r := []rune(s)
	cut := func(keep int) string {
		head := (keep + 1) / 2
		return string(r[:head]) + "…" + string(r[len(r)-(keep-head):])
	}
	lo, hi := 0, len(r)-1 // the runes kept
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(cut(mid)) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return cut(lo)
}

// MiddleText is s on one line, in a box that grows to the room its row
// leaves, shortened by its middle (MiddleEllipsis) to the width the box
// had in the last frame. Style the box with what it returns; padding
// goes on the text, as pad DIPs on each side.
func MiddleText(c *ui.Context, s string, size, pad float32) ui.Element {
	box := ui.Box(c).Grow(1).MinWidth(0).Clip()
	text := s
	if w := box.Bounds().W - 2*pad - 1; w > 0 {
		text = MiddleEllipsis(c, s, w, size)
	}
	box.Children(func() {
		ui.Text(c, text).FontSize(size).SingleLine().PaddingX(pad)
	})
	return box
}
