package web

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type imgSide struct {
	Kind   string `json:"kind"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
}

type imgDiffBody struct {
	Binary bool               `json:"binary"`
	Images map[string]imgSide `json:"images"`
}

// imageRepo: c2 adds shot.png (8×4) and blob.bin, c3 changes shot.png
// (16×8) and blob.bin, c4 renames shot.png to moved.png; the working tree
// then rewrites moved.png (20×10, unstaged).
func imageRepo(t *testing.T) (dir string, v1, v2, v3 []byte, c3, c4 string) {
	t.Helper()
	dir = newRepoDir(t, 1)
	v1 = pngFile(t, dir, "shot.png", 8, 4)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("\x00\x01\x02"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "add")
	v2 = pngFile(t, dir, "shot.png", 16, 8)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("\x00\x09"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "change")
	c3 = strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	gitRun(t, dir, "mv", "shot.png", "moved.png")
	gitRun(t, dir, "commit", "-m", "move")
	c4 = strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	v3 = pngFile(t, dir, "moved.png", 20, 10)
	return
}

func TestDiffJSONNamesTheImageSides(t *testing.T) {
	t.Parallel()
	dir, v1, v2, _, c3, _ := imageRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var b imgDiffBody
	if code := getJSON(t, ts, "/api/diff?sha="+c3+"&path=shot.png&status=M", &b); code != http.StatusOK || !b.Binary {
		t.Fatalf("code=%d body=%+v", code, b)
	}
	if b.Images["old"] != (imgSide{"png", 8, 4, len(v1)}) || b.Images["new"] != (imgSide{"png", 16, 8, len(v2)}) {
		t.Fatalf("images = %+v", b.Images)
	}
	b = imgDiffBody{}
	getJSON(t, ts, "/api/diff?sha="+c3+"&path=blob.bin&status=M", &b)
	if !b.Binary || b.Images != nil {
		t.Fatalf("a non-image binary names no images: %+v", b)
	}
}

func TestDiffImgServesEachSidesBytes(t *testing.T) {
	t.Parallel()
	dir, v1, v2, v3, c3, c4 := imageRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	base := "/api/diff?sha=" + c3 + "&path=shot.png&status=M"
	for side, want := range map[string][]byte{"old": v1, "new": v2} {
		code, hdr, body := getRaw(t, ts, base+"&img="+side)
		if code != http.StatusOK || hdr.Get("Content-Type") != "image/png" || !bytes.Equal(body, want) {
			t.Fatalf("%s: code=%d type=%q len=%d", side, code, hdr.Get("Content-Type"), len(body))
		}
		if hdr.Get("X-Content-Type-Options") != "nosniff" || hdr.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s headers = %v", side, hdr)
		}
	}
	// A rename: the old side is read at the OLD path.
	if code, _, body := getRaw(t, ts, "/api/diff?sha="+c4+"&path=moved.png&old=shot.png&status=R&img=old"); code != http.StatusOK || !bytes.Equal(body, v2) {
		t.Fatalf("rename old side: code=%d len=%d", code, len(body))
	}
	// The working-tree form.
	if code, _, body := getRaw(t, ts, "/api/diff?wt=unstaged&path=moved.png&img=new"); code != http.StatusOK || !bytes.Equal(body, v3) {
		t.Fatalf("wt new side: code=%d len=%d", code, len(body))
	}
	// The rev-pair form.
	c2 := strings.TrimSpace(gitRun(t, dir, "rev-parse", c3+"^"))
	if code, _, body := getRaw(t, ts, "/api/diff?left="+c2+"&right="+c3+"&path=shot.png&status=M&img=old"); code != http.StatusOK || !bytes.Equal(body, v1) {
		t.Fatalf("rev form: code=%d len=%d", code, len(body))
	}
	// The entry (link) compare form.
	if code, _, body := getRaw(t, ts, "/api/entry-diff?left=commit:"+c2+"&right=commit:"+c3+"&path=shot.png&status=M&img=new"); code != http.StatusOK || !bytes.Equal(body, v2) {
		t.Fatalf("entry form: code=%d len=%d", code, len(body))
	}
	if code, _, _ := getRaw(t, ts, "/api/diff?sha="+c3+"&path=blob.bin&status=M&img=new"); code != http.StatusNotFound {
		t.Fatalf("a non-image side: code=%d, want 404", code)
	}
	if code, _, _ := getRaw(t, ts, base+"&img=both"); code != http.StatusBadRequest {
		t.Fatalf("bad img: code=%d, want 400", code)
	}
}
