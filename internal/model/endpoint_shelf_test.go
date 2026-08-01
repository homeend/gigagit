package model

import "testing"

func TestEndpointShelf(t *testing.T) {
	e := Endpoint{Kind: EndpointShelf, ShelfID: "commit-1a2b3c4-deadbeef"}

	if e.IsLive() {
		t.Error("a frozen shelf endpoint must not be live (it is immutable and cacheable)")
	}
	if got, want := e.CacheTag(), "shelf:commit-1a2b3c4-deadbeef"; got != want {
		t.Errorf("CacheTag = %q, want %q (prefixed: must never collide with a sha)", got, want)
	}
	ref := e.FileRef("dir/file.go")
	if ref.Source != SourceShelf || ref.Locator != "commit-1a2b3c4-deadbeef" || ref.Path != "dir/file.go" {
		t.Errorf("FileRef = %+v, want SourceShelf/entry-id/path", ref)
	}
	if got, want := e.Display(), "shelf #commit-1a (frozen)"; got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
	// A short id is not truncated.
	short := Endpoint{Kind: EndpointShelf, ShelfID: "ab"}
	if got, want := short.Display(), "shelf #ab (frozen)"; got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}
