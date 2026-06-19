package shelf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir())
}

func addr(path string) model.FileAddress {
	return model.FileAddress{State: model.StateUnstaged, Path: path}
}

func TestPutGetRoundTrip(t *testing.T) {
	s := newStore(t)
	data := []byte("hello shelf\n")
	e, err := s.Put("", addr("a/b.go"), data)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if e.Bucket != "default" {
		t.Errorf("bucket = %q, want default", e.Bucket)
	}
	if e.Origin.Path != "a/b.go" || e.Size != int64(len(data)) {
		t.Errorf("entry meta wrong: %+v", e)
	}
	got, err := s.Get(e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get = %q, want %q", got, data)
	}
}

func TestPutDedupsIdenticalBytes(t *testing.T) {
	s := newStore(t)
	data := []byte("same")
	e1, _ := s.Put("", addr("x.go"), data)
	e2, _ := s.Put("", addr("y.go"), data)
	if e1.SHA != e2.SHA {
		t.Fatalf("identical bytes got different SHAs: %s vs %s", e1.SHA, e2.SHA)
	}
	if e1.ID == e2.ID {
		t.Fatalf("different paths should get different IDs")
	}
}

func TestPutRefusesOversize(t *testing.T) {
	s := newStore(t)
	big := make([]byte, MaxShelfBytes+1)
	if _, err := s.Put("", addr("big.bin"), big); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestListPagingAndExhaustion(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"a", "b", "c"} {
		if _, err := s.Put("", addr(p), []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List("", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page))
	}
	page2, _ := s.List("", 2, 2)
	if len(page2) != 1 {
		t.Fatalf("page2 len = %d, want 1 (exhausted)", len(page2))
	}
}

func TestRemoveReclaimsUnreferencedBlobKeepsShared(t *testing.T) {
	s := newStore(t)
	shared := []byte("shared")
	e1, _ := s.Put("", addr("p1"), shared)
	e2, _ := s.Put("", addr("p2"), shared) // same blob
	if err := s.Remove(e1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(e2.ID); err != nil {
		t.Fatalf("shared blob removed too early: %v", err)
	}
	if err := s.Remove(e2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(e2.ID); err == nil {
		t.Fatalf("blob should be gone after last reference removed")
	}
}

func TestBucketsListNamed(t *testing.T) {
	s := newStore(t)
	s.Put("feature", addr("f.go"), []byte("f"))
	bs, err := s.Buckets()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range bs {
		names = append(names, b.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "feature") {
		t.Fatalf("buckets %v missing 'feature'", names)
	}
}
