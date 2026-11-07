package app

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"finalshelly/internal/config"

	"github.com/egoist/mygo/ui"
)

// heldRead is a reader of background images that waits, for the paths it
// holds, until the test lets it go.
type heldRead struct {
	started chan string          // each path as its read starts
	release map[string]chan bool // closed to let the read of a path go on
}

func newHeldRead(paths ...string) *heldRead {
	h := &heldRead{started: make(chan string, 16), release: map[string]chan bool{}}
	for _, p := range paths {
		h.release[p] = make(chan bool)
	}
	return h
}

func (h *heldRead) read(path string) (*ui.Bitmap, error) {
	h.started <- path
	if ch := h.release[path]; ch != nil {
		<-ch
	}
	return readImage(path)
}

// startedRead waits for a read of path to start.
func (h *heldRead) startedRead(t *testing.T, path string) {
	t.Helper()
	select {
	case got := <-h.started:
		if got != path {
			t.Fatalf("read %s, want %s", got, path)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no read of %s", path)
	}
}

// savedBackground keeps an image of w×h pixels as a's background copy,
// as choosing it would, and returns its name and path.
func savedBackground(t *testing.T, w, h int) (string, string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "img.png")
	writeImage(t, src, w, h)
	data, _ := os.ReadFile(src)
	name, err := config.SaveBackground(data, src)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := config.BackgroundPath(name)
	return name, path
}

// Startup reads no image, and the window draws while the image is read:
// the plain background until it is.
func TestBackgroundReadsOffUIThread(t *testing.T) {
	_, p := testApp(t)
	name, path := savedBackground(t, 320, 200)
	config.SaveSettings(config.Settings{TermBackground: name})
	a := New(p)
	if a.look.requested || a.look.image != nil {
		t.Fatal("startup read the image")
	}
	held := newHeldRead(path)
	a.look.read = held.read
	s := demoSession(a, "web-1", "45.56.78.90")
	lookTerminal(t, a, s)
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	tt.Frame() // returns while the read is held
	held.startedRead(t, path)
	tt.Frame()
	plain := a.termColors().Background
	x, y := int(a.sideWidth)+500, int(820*a.terminalPct/100)-40
	if px := tt.Image().RGBAAt(x, y); px.R != plain.R || px.G != plain.G || px.B != plain.B || a.look.image != nil {
		t.Errorf("while reading: %v at %d,%d, image %v", px, x, y, a.look.image)
	}
	// Frames while it reads start no second read.
	tt.Frame()
	select {
	case got := <-held.started:
		t.Fatalf("a second read of %s", got)
	default:
	}

	close(held.release[path])
	p.until(t, "the image", func() bool { return a.look.image != nil })
	tt.Frame()
	if px := tt.Image().RGBAAt(x, y); px.R == plain.R && px.G == plain.G && px.B == plain.B {
		t.Errorf("read, the plain background %v still shows", px)
	}
}

// A read that ends after the image changed is dropped.
func TestStaleBackgroundReadDropped(t *testing.T) {
	a, p := testApp(t)
	first, firstPath := savedBackground(t, 40, 30)
	second, secondPath := savedBackground(t, 64, 64)
	held := newHeldRead(firstPath)
	a.look.read = held.read

	a.changeTerminalSettings(func() { a.settings.TermBackground = first })
	a.backgroundImage()
	held.startedRead(t, firstPath)
	a.changeTerminalSettings(func() { a.settings.TermBackground = second })
	a.backgroundImage()
	held.startedRead(t, secondPath)
	p.until(t, "the second image", func() bool { return a.look.image != nil })
	if w, h := a.look.image.Size(); w != 64 || h != 64 {
		t.Fatalf("image %dx%d", w, h)
	}

	close(held.release[firstPath])
	select {
	case fn := <-p.posts: // what the first read posts
		fn()
	case <-time.After(10 * time.Second):
		t.Fatal("the first read posted nothing")
	}
	if w, h := a.look.image.Size(); w != 64 || h != 64 || a.look.imageErr != nil {
		t.Errorf("after the stale read: image %dx%d, error %v", w, h, a.look.imageErr)
	}

	// Nor does a read of an image since removed show it.
	held = newHeldRead(secondPath)
	a.look.read = held.read
	a.changeTerminalSettings(func() { a.settings.TermBackground = "" })
	a.changeTerminalSettings(func() { a.settings.TermBackground = second })
	a.backgroundImage()
	held.startedRead(t, secondPath)
	a.removeBackground()
	close(held.release[secondPath])
	select {
	case fn := <-p.posts:
		fn()
	case <-time.After(10 * time.Second):
		t.Fatal("the read posted nothing")
	}
	if a.look.image != nil || a.backgroundImage() != nil {
		t.Error("a removed image showed")
	}
}

// A large image is kept scaled down; the copy is the file as it was.
func TestBackgroundDownscaled(t *testing.T) {
	_, _ = testApp(t)
	src := filepath.Join(t.TempDir(), "wide.png")
	data := writeImage(t, src, 5000, 200)
	img, err := readImage(src)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Size(); w != maxBackgroundSide || h != 153 {
		t.Errorf("read %dx%d", w, h)
	}
	name, img, err := importBackground(src)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Size(); w != maxBackgroundSide || h != 153 {
		t.Errorf("imported %dx%d", w, h)
	}
	path, _ := config.BackgroundPath(name)
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Error("the copy is not the file")
	}

	tall := filepath.Join(t.TempDir(), "tall.png")
	writeImage(t, tall, 100, 4000)
	if img, err := readImage(tall); err != nil {
		t.Fatal(err)
	} else if w, h := img.Size(); w != 96 || h != maxBackgroundSide {
		t.Errorf("tall read %dx%d", w, h)
	}
	small := filepath.Join(t.TempDir(), "small.png")
	writeImage(t, small, 300, 200)
	if img, err := readImage(small); err != nil {
		t.Fatal(err)
	} else if w, h := img.Size(); w != 300 || h != 200 {
		t.Errorf("small read %dx%d", w, h)
	}
}

// A photo turned by its EXIF orientation comes out upright, scaled down
// or not, as DecodeBitmap shows it.
func TestBackgroundOrientation(t *testing.T) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 40)), nil); err != nil {
		t.Fatal(err)
	}
	// An APP1 segment of EXIF after SOI: one IFD entry, Orientation 6
	// (turned 90° clockwise to show).
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x06\x00\x00\x00\x00\x00\x00")
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	data := append(append(append([]byte{}, buf.Bytes()[:2]...), append(app1, seg...)...), buf.Bytes()[2:]...)
	if o := exifOrientation(data); o != 6 {
		t.Fatalf("orientation %d", o)
	}
	full, err := decodeBackground(data, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := full.Size(); w != 40 || h != 64 {
		t.Errorf("full size %dx%d", w, h)
	}
	scaled, err := decodeBackground(data, 32)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := scaled.Size(); w != 20 || h != 32 {
		t.Errorf("scaled %dx%d", w, h)
	}

	// Each orientation puts the top left pixel where it shows.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Pix[0] = 255 // the top left pixel is red
	for o, want := range map[int]image.Point{1: {0, 0}, 2: {2, 0}, 3: {2, 1}, 4: {0, 1}, 5: {0, 0}, 6: {1, 0}, 7: {1, 2}, 8: {0, 2}} {
		out := orient(src, o)
		if o >= 5 && out.Bounds().Dx() != 2 {
			t.Errorf("orientation %d: %v", o, out.Bounds())
		}
		if out.Pix[out.PixOffset(want.X, want.Y)] != 255 {
			t.Errorf("orientation %d: red is not at %v", o, want)
		}
	}
}

// The find bar shows over the background image, and full screen keeps
// the image under the terminal.
func TestBackgroundUnderFindBarAndFullScreen(t *testing.T) {
	a, p := testApp(t)
	name, _ := savedBackground(t, 320, 200)
	a.changeTerminalSettings(func() { a.settings.TermBackground = name })
	s := demoSession(a, "web-1", "45.56.78.90")
	lookTerminal(t, a, s)
	a.active = 0
	tt := ui.NewTester(a.View, 1280, 820)
	p.tt = tt
	waitBackground(t, a, p)
	a.openFind()
	tt.Frame()
	tt.Type("deploy")
	tt.Frame()
	if !s.find.open || s.term.SearchStatus().Total == 0 {
		t.Errorf("find bar open %v, matches %+v", s.find.open, s.term.SearchStatus())
	}
	saveShot(t, tt, "terminal-background-find-en.png")

	a.setFullScreen(true)
	tt.Frame()
	plain := a.termColors().Background
	if px := tt.Image().RGBAAt(600, 760); px.R == plain.R && px.G == plain.G && px.B == plain.B {
		t.Errorf("full screen: the plain background %v shows", px)
	}
	saveShot(t, tt, "terminal-background-fullscreen-en.png")
}

// An image of more pixels than the app decodes is refused before it is.
func TestBackgroundTooManyPixels(t *testing.T) {
	// A PNG header saying 20000×20000, with no pixels after it.
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 20000)
	binary.BigEndian.PutUint32(ihdr[4:], 20000)
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	var png bytes.Buffer
	png.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&png, binary.BigEndian, uint32(len(ihdr)))
	png.Write(chunk)
	binary.Write(&png, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	if _, err := decodeBackground(png.Bytes(), maxBackgroundSide); err != errImageTooLarge {
		t.Errorf("decoded: %v", err)
	}
	if msg := describeImageError(errImageTooLarge); msg == "" {
		t.Error("no message")
	}
}
