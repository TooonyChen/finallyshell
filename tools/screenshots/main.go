// Command screenshots updates the screenshots in docs/screenshots, which
// the READMEs show. Run it from the repository's root:
//
//	go run ./tools/screenshots
//
// It runs the tests of internal/app with SHOT_DIR set, as they save the
// frames they draw there, and copies the frames listed in shots into
// docs/screenshots, cropped where a scene is a part of the window. The
// tests draw a window's content only, so each copy gets a title bar like
// macOS's (close, minimize and full screen buttons, and the window's
// title), rounded corners, a thin border and a shadow, to look like the
// window it is. The main window's scenes come from TestReadmeScreenshots
// (internal/app/readme_test.go); the other windows' from their own tests.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"

	"finallyshell/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// A shot is a frame a test saves and where it goes. Crop, in the
// window's DIPs, keeps a part of it; the frames are twice the pixels.
// Title is the window's title, from locales as the app sets it.
type shot struct {
	from, to string
	title    func() string
	crop     image.Rectangle
}

// langs are the READMEs' languages: the suffix of a frame and of its copy.
var langs = []struct{ test, out string }{{"en", "en"}, {"zh-Hans", "zh-CN"}}

// shots are the frames, by name without the language and ".png". A from
// with %s takes the language as the test spells it.
var shots = []shot{
	{from: "readme-main-%s", to: "main", title: locales.AppTitle},
	{from: "readme-quick-connect-%s", to: "quick-connect", title: locales.AppTitle},
	{from: "readme-connections-%s", to: "connections", title: locales.AppTitle, crop: image.Rect(0, 390, 640, 820)},
	{from: "readme-sysinfo-%s", to: "system-info", title: locales.AppTitle, crop: image.Rect(0, 0, 720, 480)},
	{from: "readme-transfers-%s", to: "transfers", title: locales.AppTitle, crop: image.Rect(560, 110, 1280, 590)},
	{from: "readme-quick-commands-%s", to: "quick-commands", title: locales.AppTitle, crop: image.Rect(240, 487, 1280, 820)},
	{from: "manager-%s", to: "connection-manager", title: locales.ManagerTitle},
	{from: "editor-proxy-jump-%s", to: "connection-proxy", title: locales.EditConnection},
	{from: "editor-tunnels-rows-%s", to: "connection-tunnels", title: locales.EditConnection},
	{from: "import-%s", to: "import-finalshell", title: locales.ImportTitle},
	// The editor's title is its file's host and path.
	{from: "editor-%s", to: "file-editor", title: func() string { return "web-1:/srv/app/deploy.sh" }},
	{from: "options-file-icons-%s", to: "file-icon-themes", title: locales.Options},
	{from: "options-colors-%s", to: "terminal-colors", title: locales.Options},
	{from: "options-background-%s", to: "terminal-background", title: locales.Options},
	{from: "shortcuts-%s", to: "shortcuts", title: locales.Options},
}

// Some tests spell Chinese "zh" in their frames' names.
var shortZh = map[string]bool{
	"manager-%s": true, "import-%s": true, "options-file-icons-%s": true,
	"options-colors-%s": true, "options-background-%s": true, "shortcuts-%s": true,
}

const (
	scale    = 2
	titleBar = 28 // DIPs
	radius   = 10 * scale
	border   = 1 * scale
	// The shadow: how far it spreads, how far down it falls, how dark.
	blur, drop, shade = 20 * scale, 8 * scale, 0.28
)

func main() {
	tmp, err := os.MkdirTemp("", "screenshots")
	check(err)
	defer os.RemoveAll(tmp)

	cmd := exec.Command("go", "test", "-count=1", "./internal/app")
	cmd.Env = append(os.Environ(), "SHOT_DIR="+tmp)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	check(cmd.Run())

	out := filepath.Join("docs", "screenshots")
	check(os.MkdirAll(out, 0o755))
	lang := ""
	i18n.SetLocaleFunc(func() string { return lang })
	for _, s := range shots {
		for _, l := range langs {
			lang = l.test
			name := l.test
			if name == "zh-Hans" && shortZh[s.from] {
				name = "zh"
			}
			src := filepath.Join(tmp, fmt.Sprintf(s.from, name)+".png")
			dst := filepath.Join(out, s.to+"-"+l.out+".png")
			check(copyShot(src, dst, s.title(), s.crop))
			fmt.Println(dst)
		}
	}
}

func copyShot(src, dst, title string, crop image.Rectangle) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	r := img.Bounds()
	if !crop.Empty() {
		r = image.Rect(crop.Min.X*scale, crop.Min.Y*scale, crop.Max.X*scale, crop.Max.Y*scale).Intersect(r)
	}
	out := shadow(frame(withTitleBar(img, r, title)))
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(w, out); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// withTitleBar is the part r of img under a title bar showing title.
func withTitleBar(img image.Image, r image.Rectangle, title string) *image.NRGBA {
	bar := titleBarImage(r.Dx()/scale, title)
	h := bar.Bounds().Dy()
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()+h))
	draw.Draw(out, image.Rect(0, 0, r.Dx(), h), bar, image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, h, r.Dx(), h+r.Dy()), img, r.Min, draw.Src)
	return out
}

// titleBarImage draws a macOS title bar width DIPs wide: the close,
// minimize and full screen buttons on the left, title in the middle.
func titleBarImage(width int, title string) image.Image {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.Hex("#ebebeb")).Children(func() {
			ui.Row(c).Absolute().Top(0).Left(12).Height(titleBar).Gap(8).AlignItems(ui.Center).Children(func() {
				for _, k := range [][2]string{{"#ff5f57", "#e0443e"}, {"#febc2e", "#dea123"}, {"#28c840", "#1aab29"}} {
					ui.Box(c).Size(12, 12).Radius(6).Background(ui.Hex(k[0])).Border(0.5, ui.Hex(k[1]))
				}
			})
			ui.Row(c).Fill().PaddingX(80).Center().Children(func() {
				ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(ui.Hex("#4d4d4d")).SingleLine()
			})
			ui.Box(c).Absolute().Left(0).Top(titleBar - 1).Width(float32(width)).Height(1).Background(ui.Hex("#d6d6d6"))
		})
	}, width, titleBar)
	tt.SetScale(scale)
	tt.Frame()
	return tt.Image()
}

// frame gives img rounded corners, transparent outside them, and a
// border just inside its edge.
func frame(out *image.NRGBA) *image.NRGBA {
	edge := color.NRGBA{0xc8, 0xcc, 0xd0, 0xff}
	r := out.Bounds()
	w, h := float64(r.Dx()), float64(r.Dy())
	for y := range r.Dy() {
		for x := range r.Dx() {
			d := roundedDist(float64(x)+0.5, float64(y)+0.5, w, h, radius)
			if d < -border-1 {
				continue // well inside: the picture as it is
			}
			c := out.NRGBAAt(x, y)
			// The border covers the band within border of the edge.
			if b := clamp(d + border + 0.5); b > 0 {
				c = mix(c, edge, b)
			}
			// Outside the rounded edge is transparent.
			c.A = uint8(float64(c.A) * clamp(0.5-d))
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

// shadow puts win on a transparent margin, over a soft shadow of its
// shape that falls a little down.
func shadow(win *image.NRGBA) *image.NRGBA {
	pad := blur + drop
	b := win.Bounds()
	w, h := b.Dx()+2*pad, b.Dy()+2*pad
	alpha := make([]float64, w*h)
	for y := range b.Dy() {
		for x := range b.Dx() {
			alpha[(y+pad+drop)*w+x+pad] = float64(win.NRGBAAt(x, y).A) / 255
		}
	}
	// Three box blurs are close to a Gaussian one.
	for range 3 {
		boxBlur(alpha, w, h, blur/3)
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, a := range alpha {
		out.Pix[i*4+3] = uint8(a*shade*255 + 0.5)
	}
	draw.Draw(out, b.Add(image.Pt(pad, pad)), win, image.Point{}, draw.Over)
	return out
}

// boxBlur averages each value of the w×h grid a over the square of
// radius r around it, in two passes.
func boxBlur(a []float64, w, h, r int) {
	tmp := make([]float64, len(a))
	pass := func(src, dst []float64, n, m int, at func(i, j int) int) {
		for j := range m {
			sum := 0.0
			for i := -r; i <= r; i++ {
				if i >= 0 && i < n {
					sum += src[at(i, j)]
				}
			}
			for i := range n {
				dst[at(i, j)] = sum / float64(2*r+1)
				if k := i - r; k >= 0 {
					sum -= src[at(k, j)]
				}
				if k := i + r + 1; k < n {
					sum += src[at(k, j)]
				}
			}
		}
	}
	pass(a, tmp, w, h, func(i, j int) int { return j*w + i })
	pass(tmp, a, h, w, func(i, j int) int { return i*w + j })
}

// roundedDist is how far (x, y) is outside the w×h rectangle with corners
// of radius rad: negative inside.
func roundedDist(x, y, w, h, rad float64) float64 {
	cx := math.Max(rad-x, x-(w-rad))
	cy := math.Max(rad-y, y-(h-rad))
	if cx > 0 && cy > 0 {
		return math.Hypot(cx, cy) - rad
	}
	return math.Max(math.Max(-x, x-w), math.Max(-y, y-h))
}

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(p, q uint8) uint8 { return uint8(float64(p)*(1-t) + float64(q)*t + 0.5) }
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), m(a.A, b.A)}
}

func clamp(v float64) float64 { return math.Min(1, math.Max(0, v)) }

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "screenshots:", err)
		os.Exit(1)
	}
}
