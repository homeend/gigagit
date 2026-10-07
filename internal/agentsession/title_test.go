package agentsession

import (
	"context"
	"runtime"
	"testing"
)

func TestTitle(t *testing.T) {
	t.Parallel()
	cases := []struct{ label, name, want string }{
		{"Claude (yolo)", "viewer", "Claude (yolo) [viewer]"},
		{"Claude", "", "Claude"},
		{"Junie", "worker 2", "Junie [worker 2]"},
	}
	for _, c := range cases {
		if got := Title(c.label, c.name); got != c.want {
			t.Errorf("Title(%q, %q) = %q, want %q", c.label, c.name, got, c.want)
		}
		if got := (Info{Label: c.label, Name: c.name}).Title(); got != c.want {
			t.Errorf("Info.Title() = %q, want %q", got, c.want)
		}
	}
}

func TestStartCarriesName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	m := NewManager()
	t.Cleanup(func() { m.KillAll(context.Background()) })
	s, err := m.Start(StartSpec{Label: "Sh", Name: "viewer", Dir: t.TempDir(), Argv: []string{"sh", "-c", "sleep 30"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if in := s.Info(); in.Name != "viewer" || in.Title() != "Sh [viewer]" {
		t.Fatalf("info %+v", in)
	}
}
