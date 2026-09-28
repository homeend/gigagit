package web

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type fcBinBody struct {
	Lines  []struct{} `json:"lines"`
	Binary bool       `json:"binary"`
	Image  string     `json:"image"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Size   int        `json:"size"`
}

func pngFile(t *testing.T, dir, name string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFileContentNamesBinariesAndImages(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("GIF\x00\x01\x02\xff\xfe"), 0o644); err != nil {
		t.Fatal(err)
	}
	pngFile(t, dir, "shot.png", 40, 20)
	ts := serve(t, New(domain.Open(dir)))
	var b fcBinBody
	if code := getJSON(t, ts, "/api/file-content?path=blob.bin", &b); code != http.StatusOK || !b.Binary || b.Image != "" || b.Size != 8 || len(b.Lines) != 0 {
		t.Fatalf("blob.bin: code=%d body=%+v, want binary with its size and no lines", code, b)
	}
	b = fcBinBody{}
	if code := getJSON(t, ts, "/api/file-content?path=shot.png", &b); code != http.StatusOK || !b.Binary || b.Image != "png" || b.Width != 40 || b.Height != 20 || b.Size == 0 {
		t.Fatalf("shot.png: code=%d body=%+v, want an image with its format and pixel size", code, b)
	}
}

func TestFileRawServesImagesOnly(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	want := pngFile(t, dir, "shot.png", 8, 8)
	write(t, dir, "a.go", "package a\n")
	ts := serve(t, New(domain.Open(dir)))
	code, hdr, body := getRaw(t, ts, "/api/file-raw?path=shot.png")
	if code != http.StatusOK || hdr.Get("Content-Type") != "image/png" || !bytes.Equal(body, want) {
		t.Fatalf("shot.png: code=%d type=%q bytes=%d, want the PNG as image/png", code, hdr.Get("Content-Type"), len(body))
	}
	if hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Cache-Control") == "" {
		t.Fatalf("headers = %v: want nosniff and a cache rule", hdr)
	}
	if code, _, _ := getRaw(t, ts, "/api/file-raw?path=a.go"); code != http.StatusUnsupportedMediaType {
		t.Fatalf("a.go: code=%d, want 415 — only images are served raw", code)
	}
	if code, _, _ := getRaw(t, ts, "/api/file-raw?path=nope.png"); code != http.StatusNotFound {
		t.Fatalf("missing: code=%d, want 404", code)
	}
	if code, _, _ := getRaw(t, ts, "/api/file-raw?path=../shot.png"); code != http.StatusBadRequest {
		t.Fatalf("escaping path: code=%d, want 400", code)
	}
}
