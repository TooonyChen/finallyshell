package icons

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Every agent logo parses, and draws as an icon in the color it is
// given, as a session row draws it: dark on the light theme, light on
// the dark one, white on the selected row's accent. The generic one
// (Lucide's bot) too.
func TestAgentIcons(t *testing.T) {
	entries, err := files.ReadDir("agents")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".svg"); ok {
			names = append(names, n)
			if _, err := ui.ParseSVG(must(files.ReadFile("agents/" + e.Name()))); err != nil {
				t.Errorf("%s: %v", e.Name(), err)
			}
		}
	}
	if len(names) < 12 {
		t.Fatalf("%d logos in agents/", len(names))
	}
	backs := []struct {
		name     string
		bg, text ui.Color
	}{
		{"light", ui.RGB(255, 255, 255), ui.RGB(30, 30, 30)},
		{"dark", ui.RGB(30, 30, 30), ui.RGB(235, 235, 235)},
		{"accent", ui.RGB(37, 99, 235), ui.RGB(255, 255, 255)},
	}
	draw := func(name string, svg *ui.SVG) {
		for _, b := range backs {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Box(c).Fill().Background(b.bg).Center().Children(func() {
					ui.Icon(c, svg).FontSize(28).TextColor(b.text)
				})
			}, 40, 40)
			tt.Frame()
			img := tt.Image()
			ink := 0
			for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
				for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
					p := img.RGBAAt(x, y)
					if near(p.R, b.text.R) && near(p.G, b.text.G) && near(p.B, b.text.B) {
						ink++
					}
				}
			}
			// Every logo covers a good part of its 28 by 28 square.
			if ink < 60 {
				t.Errorf("%s on %s: %d pixels in the icon's color", name, b.name, ink)
			}
		}
	}
	for _, n := range names {
		draw(n, Agent(n))
	}
	draw("bot", Get("bot"))
}

func near(a, b uint8) bool { return max(a, b)-min(a, b) < 24 }

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func TestMissingAgentIcon(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic for a logo that isn't there")
		}
	}()
	Agent("no-such-agent")
}
