package web

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// The list endpoints stop at a page (maxBookmarkRows); a link's ?bookmark=<id>
// hint names ONE entry, which may sit past it. ?id= answers that entry from
// the store, so the page never has to call a live entry "gone".
func TestBookmarkByIDReachesPastTheListCap(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	svc := domain.Open(dir)
	ctx := context.Background()
	all := map[string]bool{}
	for i := 0; i < maxBookmarkRows+3; i++ {
		b, err := svc.BookmarkAdd(ctx, model.Bookmark{State: model.StateUnstaged, Path: fmt.Sprintf("f%03d.txt", i)})
		if err != nil {
			t.Fatal(err)
		}
		all[b.ID] = true
	}
	ts := serve(t, New(svc))
	var list bmList
	if code := getJSON(t, ts, "/api/bookmarks", &list); code != http.StatusOK || len(list.Entries) != maxBookmarkRows {
		t.Fatalf("fixture: list code=%d entries=%d, want the cap %d", code, len(list.Entries), maxBookmarkRows)
	}
	for _, e := range list.Entries {
		delete(all, e.ID)
	}
	if len(all) != 3 {
		t.Fatalf("fixture: %d entries past the cap, want 3", len(all))
	}
	for id := range all {
		var one bmList
		if code := getJSON(t, ts, "/api/bookmarks?id="+id, &one); code != http.StatusOK {
			t.Fatalf("by id: code %d", code)
		}
		if len(one.Entries) != 1 || one.Entries[0].ID != id {
			t.Fatalf("by id: want exactly %s, got %+v", id, one.Entries)
		}
	}
	var e map[string]any
	if code := getAny(t, ts, "/api/bookmarks?id=no-such-entry", &e); code != http.StatusNotFound {
		t.Fatalf("an unknown id: code %d, want 404", code)
	}
}

// The shelf list reads ONE bucket; ShelfFind scans them all. By id, an entry in
// another bucket is found without the page walking the buckets itself.
func TestShelfByIDFindsAnEntryInAnyBucket(t *testing.T) {
	isolateState(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(dir)
	ts := serve(t, New(svc))
	if code := postJSON(t, ts, "/api/shelf", `{"path":"wip.txt","state":"untracked","bucket":"side"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add: code %d", code)
	}
	es, err := svc.ShelfList(context.Background(), "side", 0, 0)
	if err != nil || len(es) != 1 {
		t.Fatalf("fixture: %v %d", err, len(es))
	}
	id := es[0].ID
	var list shList
	if code := getJSON(t, ts, "/api/shelf", &list); code != http.StatusOK || len(list.Entries) != 0 {
		t.Fatalf("fixture: the default bucket must NOT list it (code %d, %d entries)", code, len(list.Entries))
	}
	var one shList
	if code := getJSON(t, ts, "/api/shelf?id="+id, &one); code != http.StatusOK || len(one.Entries) != 1 || one.Entries[0].ID != id {
		t.Fatalf("by id: code %d, %+v", code, one.Entries)
	}
	var e map[string]any
	if code := getAny(t, ts, "/api/shelf?id=no-such-entry", &e); code != http.StatusNotFound {
		t.Fatalf("an unknown id: code %d, want 404", code)
	}
	// id names ONE entry wherever it lives; a bucket beside it is a contradiction.
	if code := getAny(t, ts, "/api/shelf?id="+id+"&bucket=side", &e); code != http.StatusBadRequest {
		t.Fatalf("id + bucket: code %d, want 400", code)
	}
}
