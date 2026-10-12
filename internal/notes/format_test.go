package notes

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

const oldPart = `[[notes]]
id = 'aaaaaaaa'
source = 'agent'
author = 'Claude Code'
side = 'new'
range = [3, 3]
context_hash = ''
summary = 's'
tags = ['review', 'severity: risk', 'perf']
confidence = 0.6
created = 2026-10-01T10:00:00Z
updated = 2026-10-01T10:00:00Z

[notes.address]
Commit = 'abc'
Path = 'a.go'
State = 0
`

func writeRaw(t *testing.T, dir, body string) *partFile {
	t.Helper()
	p := filepath.Join(dir, "notes.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return &partFile{path: p}
}

func TestUpgradeMovesTagsAndConfidenceIntoMeta(t *testing.T) {
	t.Parallel()
	fs := writeRaw(t, t.TempDir(), oldPart)
	ns, err := fs.Load()
	if err != nil {
		t.Fatal(err)
	}
	n := ns[0]
	if !reflect.DeepEqual(n.Tags, []string{"review", "perf"}) {
		t.Fatalf("tags = %v", n.Tags)
	}
	want := []notemeta.KV{{Key: "severity", Value: "risk"}, {Key: "confidence", Value: "medium"}}
	if !reflect.DeepEqual(n.Meta, want) || n.LegacyConfidence != 0 {
		t.Fatalf("meta = %v legacy = %v", n.Meta, n.LegacyConfidence)
	}
}

func TestUpgradeIsReadOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := writeRaw(t, dir, oldPart)
	if _, err := fs.Load(); err != nil {
		t.Fatal(err)
	}
	// A mutation that changes nothing must not rewrite the converted file.
	if _, err := fs.Sweep(func(model.Note) bool { return true }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if string(data) != oldPart {
		t.Fatalf("a no-op touched the file:\n%s", data)
	}
}

func TestWriteStampsFormat3AndRoundTripsMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := newTestFile(dir)
	n := noteAt("aaaaaaaa", 1)
	n.Meta = []notemeta.KV{{Key: "kind", Value: "issue"}}
	n.Display = notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"issue": {Color: "red"}}}}
	n.Added = []notemeta.Added{{Key: "status", Value: "agreed", By: "MR", At: n.Created}}
	if err := fs.Put(n); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if !strings.HasPrefix(string(data), "format = 3\n") {
		t.Fatalf("file does not start with the format:\n%s", data)
	}
	ns, err := fs.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ns[0].Meta, n.Meta) || !reflect.DeepEqual(ns[0].Display, n.Display) ||
		len(ns[0].Added) != 1 || !ns[0].Added[0].At.Equal(n.Added[0].At) || ns[0].Added[0].Value != "agreed" {
		t.Fatalf("round trip lost meta: %+v", ns[0])
	}
}

func TestNewerFormatRefusesWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := writeRaw(t, dir, "format = 4\n"+oldPart)
	if ns, err := fs.Load(); err != nil || len(ns) != 1 {
		t.Fatalf("a newer file must still read: %v %v", ns, err)
	}
	if err := fs.Put(noteAt("bbbbbbbb", 2)); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Put = %v, want ErrNewerFormat", err)
	}
	if err := fs.Resolve(model.ThreadResolution{Root: "aaaaaaaa"}); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("Resolve = %v, want ErrNewerFormat", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.toml"))
	if string(data) != "format = 4\n"+oldPart {
		t.Fatal("a refused write touched the file")
	}
}
