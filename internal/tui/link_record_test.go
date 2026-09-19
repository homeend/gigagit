package tui

import (
	"context"
	"errors"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingModel is a real Model whose link history lives in a temp dir and
// whose clipboard is a slice.
func recordingModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	m := newTestModel(t)
	m.svc.UseLinkHistDir(t.TempDir())
	var wrote []string
	m.clipWrite = func(_ io.Writer, s string) (string, error) {
		wrote = append(wrote, s)
		return "fake", nil
	}
	return m, &wrote
}

func TestCopyingALinkRecordsIt(t *testing.T) {
	t.Parallel()
	m, wrote := recordingModel(t)
	m.copyToClipboardCmd("ok", "gg://r@ref:main")()
	h := m.svc.LinkHistory(context.Background())
	if len(*wrote) != 1 || len(h) != 1 || h[0].Link != "gg://r@ref:main" || h[0].Desc != "branch: main" {
		t.Fatalf("wrote=%v history=%+v", *wrote, h)
	}
}

// Two layers refuse a non-link — isLinkText here, ParseLink inside
// domain.RecordCopiedLink — so this test goes red only with BOTH removed.
// That is deliberate: the domain guard is the contract, this one saves a parse.
func TestCopyingPlainTextRecordsNothing(t *testing.T) {
	t.Parallel()
	m, wrote := recordingModel(t)
	m.copyToClipboardCmd("ok", "internal/tui/link.go")()
	if h := m.svc.LinkHistory(context.Background()); len(h) != 0 || len(*wrote) != 1 {
		t.Fatalf("wrote=%v history=%+v", *wrote, h)
	}
}

func TestALinkIsRecordedEvenWhenTheClipboardFails(t *testing.T) {
	t.Parallel()
	m, _ := recordingModel(t)
	m.clipWrite = func(io.Writer, string) (string, error) { return "", errors.New("no clipboard") }
	msg := m.copyToClipboardCmd("ok", "gg://r@ref:main")()
	if c, ok := msg.(clipboardCopiedMsg); !ok || c.err == nil {
		t.Fatalf("msg = %#v, want the clipboard error", msg)
	}
	if h := m.svc.LinkHistory(context.Background()); len(h) != 1 {
		t.Fatalf("history = %+v, want the link recorded anyway", h)
	}
}

// A Model built without New must not reach for the real clipboard.
func TestAModelLiteralNeverTouchesTheRealClipboard(t *testing.T) {
	t.Parallel()
	msg := Model{}.copyToClipboardCmd("ok", "text")()
	if c, ok := msg.(clipboardCopiedMsg); !ok || !errors.Is(c.err, errNoClipboardWriter) {
		t.Fatalf("msg = %#v", msg)
	}
}

// Recording at the clipboard writer is only a guarantee while there is ONE
// writer. Counted over the TOKEN stream, so a doc comment may name
// clipboard.Copy freely: the one legitimate reference is New handing it to
// Model.clipWrite.
func TestTheTUIHasOneClipboardWriter(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var where []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var sc scanner.Scanner
		fset := token.NewFileSet()
		sc.Init(fset.AddFile(f, fset.Base(), len(src)), src, nil, 0) // mode 0: comments skipped
		var prev2, prev1 string
		for {
			_, tok, lit := sc.Scan()
			if tok == token.EOF {
				break
			}
			cur := lit
			if cur == "" {
				cur = tok.String()
			}
			if prev2 == "clipboard" && prev1 == "." && cur == "Copy" {
				where = append(where, f)
			}
			prev2, prev1 = prev1, cur
		}
	}
	if len(where) != 1 || where[0] != "model.go" {
		t.Fatalf("clipboard.Copy is referenced in %v; it must be reachable only through Model.clipWrite (set once, in New)", where)
	}
}
