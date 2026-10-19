package steer

import (
	"path/filepath"
	"testing"
)

func TestDocsDirIsTheDocsSubdirectoryOfTheInbox(t *testing.T) {
	t.Parallel()
	if got := DocsDir(filepath.Join("x", "inbox")); got != filepath.Join("x", "inbox", "docs") {
		t.Fatalf("DocsDir = %q", got)
	}
	if DocsInbox != "docs" {
		t.Fatalf("DocsInbox = %q", DocsInbox)
	}
}

// No inbox, no docs directory: a caller that has not resolved its inbox yet
// must not attach to a relative "docs".
func TestDocsDirOfNoInboxIsEmpty(t *testing.T) {
	t.Parallel()
	if got := DocsDir(""); got != "" {
		t.Fatalf("DocsDir(\"\") = %q", got)
	}
}
