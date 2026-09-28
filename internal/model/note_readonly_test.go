package model

import "testing"

func TestIsReviewNoteID(t *testing.T) {
	if !IsReviewNoteID("review:n1:0") || IsReviewNoteID("n1") || IsReviewNoteID("forge:C1") {
		t.Fatal("IsReviewNoteID")
	}
	for id, want := range map[string]bool{"review:n1:0": true, "forge:C1": true, "n1": false, "": false} {
		if IsReadOnlyNoteID(id) != want {
			t.Errorf("IsReadOnlyNoteID(%q) != %v", id, want)
		}
	}
}
