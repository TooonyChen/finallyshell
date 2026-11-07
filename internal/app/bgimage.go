package app

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// Reading the background image: the file whole, then its pixels, at most
// maxBackgroundSide across or down, so that a large photo does not stay
// in memory at full size. Both run off the UI thread.

const (
	// maxBackgroundSize is the largest image file the background takes.
	maxBackgroundSize = 64 << 20
	// maxBackgroundPixels is the most pixels an image the background
	// takes may have before it is scaled down, in millions: decoding one
	// takes four bytes each.
	maxBackgroundPixels = 100
	// maxBackgroundSide is the longest side of the background as the app
	// keeps it, in pixels: a 4K display's width.
	maxBackgroundSide = 3840
)

// readImageFile reads the image file at path, of at most
// maxBackgroundSize bytes.
func readImageFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBackgroundSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBackgroundSize {
		return nil, errImageTooLarge
	}
	return data, nil
}

// decodeBackground decodes data as ui.DecodeBitmap does, upright as a
// JPEG's EXIF orientation says, scaled down to at most maxSide pixels
// across and down.
func decodeBackground(data []byte, maxSide int) (*ui.Bitmap, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errImageFormat, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errImageFormat
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxBackgroundPixels*1_000_000 {
		return nil, errImageTooLarge
	}
	if max(cfg.Width, cfg.Height) <= maxSide {
		img, err := ui.DecodeBitmap(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errImageFormat, err)
		}
		return img, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errImageFormat, err)
	}
	w, h := cfg.Width, cfg.Height
	if w >= h {
		w, h = maxSide, max(1, int(int64(h)*int64(maxSide)/int64(w)))
	} else {
		w, h = max(1, int(int64(w)*int64(maxSide)/int64(h))), maxSide
	}
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(small, small.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return ui.NewBitmap(orient(small, exifOrientation(data))), nil
}

// exifOrientation returns the orientation of a JPEG's EXIF data, 1 to 8,
// or 0 without one, as MyGo's DecodeBitmap reads it.
func exifOrientation(jpg []byte) int {
	if len(jpg) < 4 || jpg[0] != 0xff || jpg[1] != 0xd8 {
		return 0
	}
	for i := 2; i+4 <= len(jpg); {
		if jpg[i] != 0xff {
			return 0
		}
		marker, size := jpg[i+1], int(binary.BigEndian.Uint16(jpg[i+2:]))
		if marker == 0xda || size < 2 || i+2+size > len(jpg) { // the image data starts
			return 0
		}
		seg := jpg[i+4 : i+2+size]
		if marker == 0xe1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 0
}

// tiffOrientation reads the Orientation tag of the first IFD of TIFF data.
func tiffOrientation(t []byte) int {
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[ifd:]))
	for e := ifd + 2; e+12 <= len(t) && n > 0; e, n = e+12, n-1 {
		if bo.Uint16(t[e:]) == 0x0112 && bo.Uint16(t[e+2:]) == 3 { // Orientation, a SHORT
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 0
		}
	}
	return 0
}

// orient turns and flips img as EXIF orientation o says it is to show.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si, di := img.PixOffset(sx, sy), out.PixOffset(x, y)
			copy(out.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return out
}
