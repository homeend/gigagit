package domain

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func pngBytes(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func fixed(b []byte) ByteSource {
	return func(context.Context) ([]byte, error) { return b, nil }
}

func TestDiffDecodesTheImagesOfABinaryPair(t *testing.T) {
	t.Parallel()
	d := plainDiffer{}
	out, err := d.Diff(context.Background(), Request{
		Old: fixed(pngBytes(t, 8, 4, color.RGBA{255, 0, 0, 255})),
		New: fixed(pngBytes(t, 16, 8, color.RGBA{0, 0, 255, 255})),
	})
	if err != nil || !out.Binary {
		t.Fatalf("Diff = %+v, %v; want a binary verdict", out, err)
	}
	if out.OldImg == nil || out.NewImg == nil || out.NewImg.Bounds().Dx() != 16 {
		t.Fatalf("both sides should be decoded: old=%v new=%v", out.OldImg, out.NewImg)
	}
	if out.OldKind != "png" || out.NewKind != "png" {
		t.Fatalf("kinds = %q %q", out.OldKind, out.NewKind)
	}
	if out.Size() < 8*4*4+16*8*4 {
		t.Fatalf("Size() = %d must weigh the decoded pixels", out.Size())
	}
	// An added image: the old side is absent.
	out, _ = d.Diff(context.Background(), Request{Old: nil, New: fixed(pngBytes(t, 4, 4, color.RGBA{A: 255}))})
	if !out.Binary || out.OldImg != nil || out.NewImg == nil {
		t.Fatalf("added image: %+v", out)
	}
	// A binary that is no image decodes nothing.
	out, _ = d.Diff(context.Background(), Request{Old: fixed([]byte("\x00\x01\x02")), New: fixed([]byte("\x00\x03"))})
	if !out.Binary || out.OldImg != nil || out.NewImg != nil {
		t.Fatalf("plain binary: %+v", out)
	}
}

func TestDiffReportsTheOriginalPixelSize(t *testing.T) {
	t.Parallel()
	out, _ := plainDiffer{}.Diff(context.Background(), Request{
		New: fixed(pngBytes(t, 2000, 100, color.RGBA{A: 255})),
	})
	if out.NewImg == nil || out.NewImg.Bounds().Dx() != 512 {
		t.Fatalf("the working copy is shrunk to the cap: %v", out.NewImg)
	}
	if out.NewDim != (image.Point{2000, 100}) {
		t.Fatalf("NewDim = %v, want the original 2000×100", out.NewDim)
	}
}
