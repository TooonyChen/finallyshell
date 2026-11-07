package main

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(trayIcon()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("size %v", b)
	}
	inked := 0
	for y := range 32 {
		for x := range 32 {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				inked++
			}
		}
	}
	if inked < 20 || inked > 32*32/2 {
		t.Errorf("%d pixels drawn", inked)
	}
}
