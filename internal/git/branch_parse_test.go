package git

import "testing"

func TestParseBranchesCommitterDate(t *testing.T) {
	data := "*\x00main\x00origin/main\x00abc1234\x00[ahead 1]\x001717777777\n" +
		" \x00old\x00\x00def5678\x00\x00\n"
	bs, err := ParseBranches([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 {
		t.Fatalf("parsed %d branches, want 2", len(bs))
	}
	if bs[0].UnixTime != 1717777777 {
		t.Errorf("UnixTime = %d, want 1717777777", bs[0].UnixTime)
	}
	if bs[1].UnixTime != 0 {
		t.Errorf("empty date field should parse as 0, got %d", bs[1].UnixTime)
	}
}

func TestParseBranches(t *testing.T) {
	// Format: %(HEAD)\x00%(refname:short)\x00%(upstream:short)\x00%(objectname:short)\x00%(upstream:track)
	lines := "*\x00main\x00origin/main\x00abc1234\x00[ahead 2, behind 1]\n" +
		" \x00feature\x00\x00def5678\x00\n"
	got, err := ParseBranches([]byte(lines))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("branches = %d, want 2", len(got))
	}
	if !got[0].IsHead || got[0].Name != "main" || got[0].Upstream != "origin/main" {
		t.Errorf("branch0 = %+v", got[0])
	}
	if got[0].Ahead != 2 || got[0].Behind != 1 {
		t.Errorf("branch0 ahead/behind = %d/%d, want 2/1", got[0].Ahead, got[0].Behind)
	}
	if got[1].IsHead || got[1].Name != "feature" || got[1].Upstream != "" {
		t.Errorf("branch1 = %+v", got[1])
	}
}
