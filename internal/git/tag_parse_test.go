package git

import "testing"

func TestParseTags(t *testing.T) {
	t.Parallel()
	// fields: name \x00 objecttype \x00 objectname:short \x00 *objectname:short
	//         \x00 contents:subject \x00 creatordate:unix
	data := []byte(
		"v2.0.0\x00tag\x00aaaaaaa\x00bbbbbbb\x00release two\x001700000000\n" +
			"v1.0.0\x00commit\x00ccccccc\x00\x00init commit\x001600000000\n")
	tags, err := ParseTags(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("len = %d, want 2", len(tags))
	}
	// Annotated: target is the PEELED object (*objectname), subject is the tag message.
	if got := tags[0]; !got.Annotated || got.Name != "v2.0.0" || got.Target != "bbbbbbb" || got.Subject != "release two" {
		t.Fatalf("annotated tag wrong: %+v", got)
	}
	// Lightweight: objecttype=commit, target is objectname (no peel), subject is the commit's.
	if got := tags[1]; got.Annotated || got.Name != "v1.0.0" || got.Target != "ccccccc" || got.Subject != "init commit" {
		t.Fatalf("lightweight tag wrong: %+v", got)
	}
}
