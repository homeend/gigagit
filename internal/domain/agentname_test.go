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

func TestNameReachesAgentListAndRegistry(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	s, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{Name: "viewer"}, "")
	if err != nil {
		t.Fatal(err)
	}
	full := FullSessionID(s.Info().ID)
	var found bool
	for _, e := range AgentList("") {
		if e.ID == full {
			found = true
			if e.Name != "viewer" || e.Tool != "Sleeper" || e.Label != "Sleeper" {
				t.Fatalf("entry %+v — tool/label stay the command, name is separate", e)
			}
		}
	}
	if !found {
		t.Fatal("session not listed")
	}
	reg := snapshotRegistry("", "", "")
	if len(reg.Sessions) != 1 || reg.Sessions[0].Name != "viewer" || reg.Sessions[0].Label != "Sleeper" {
		t.Fatalf("registry %+v", reg.Sessions)
	}
}
