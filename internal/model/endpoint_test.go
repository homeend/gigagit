package model

import "testing"

func TestEndpointDisplay(t *testing.T) {
	cases := []struct {
		e    Endpoint
		want string
	}{
		{Endpoint{Kind: EndpointWorkTree}, "Working Tree"},
		{Endpoint{Kind: EndpointIndex}, "Staged"},
		{Endpoint{Kind: EndpointCommit, Hash: "0123456789abcdef"}, "0123456"},
		{Endpoint{Kind: EndpointCommit, Hash: "abc"}, "abc"},
	}
	for _, c := range cases {
		if got := c.e.Display(); got != c.want {
			t.Errorf("Display(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestEndpointFileRef(t *testing.T) {
	if got := (Endpoint{Kind: EndpointWorkTree}).FileRef("a.go"); got != (FileRef{Source: SourceUnstaged, Path: "a.go"}) {
		t.Errorf("worktree FileRef = %+v", got)
	}
	if got := (Endpoint{Kind: EndpointIndex}).FileRef("a.go"); got != (FileRef{Source: SourceStaged, Path: "a.go"}) {
		t.Errorf("index FileRef = %+v", got)
	}
	if got := (Endpoint{Kind: EndpointCommit, Hash: "deadbeef"}).FileRef("a.go"); got != (FileRef{Source: SourceCommit, Locator: "deadbeef", Path: "a.go"}) {
		t.Errorf("commit FileRef = %+v", got)
	}
}

func TestEndpointIsLiveAndCacheTag(t *testing.T) {
	if !(Endpoint{Kind: EndpointWorkTree}).IsLive() || !(Endpoint{Kind: EndpointIndex}).IsLive() {
		t.Error("worktree/index must be live")
	}
	if (Endpoint{Kind: EndpointCommit, Hash: "x"}).IsLive() {
		t.Error("commit must not be live")
	}
	if got := (Endpoint{Kind: EndpointCommit, Hash: "x"}).CacheTag(); got != "x" {
		t.Errorf("commit CacheTag = %q", got)
	}
	if got := (Endpoint{Kind: EndpointWorkTree}).CacheTag(); got != "worktree" {
		t.Errorf("worktree CacheTag = %q", got)
	}
}
