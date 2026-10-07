package domain

import (
	"context"
	"strings"
	"testing"
)

func TestCleanAgentName(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"  viewer  ", "viewer"},
		{"", ""},
		{" \t \n ", ""},
		{"line one\nline two", "line oneline two"},
		{"tab\there", "tabhere"},
		{"bell\x07\x1b[31mred", "bell[31mred"},
		{strings.Repeat("é", 45), strings.Repeat("é", 40)},
		// Order: trim, drop controls, cut — the cut is not re-trimmed.
		{"  " + strings.Repeat("a", 39) + "  b", strings.Repeat("a", 39) + " "},
	}
	for _, c := range cases {
		if got := CleanAgentName(c.in); got != c.want {
			t.Errorf("CleanAgentName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStartAgentSessionCarriesTheName(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	s, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{Name: "  viewer\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if in := s.Info(); in.Name != "viewer" || in.Title() != "Sleeper [viewer]" {
		t.Fatalf("info %+v", in)
	}
	s2, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if in := s2.Info(); in.Name != "" || in.Title() != "Sleeper" {
		t.Fatalf("unnamed info %+v", in)
	}
}

func TestSessionTitleReexport(t *testing.T) {
	t.Parallel()
	if got := SessionTitle("Junie", "worker"); got != "Junie [worker]" {
		t.Fatal(got)
	}
}
