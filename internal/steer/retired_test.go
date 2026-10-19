package steer

import "testing"

func TestRetiredVerbs(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"note_add", "note_list", "note_show", "note_rm"} {
		if got, ok := RetiredVerb(v); !ok || got != v+" is retired: write the file (gg session note add)" {
			t.Errorf("%s: %q %v", v, got, ok)
		}
	}
	for _, v := range []string{"overview_add", "overview_set", "overview_list", "overview_show", "overview_rm"} {
		if got, ok := RetiredVerb(v); !ok || got != v+" is retired: write the file (gg session overview add)" {
			t.Errorf("%s: %q %v", v, got, ok)
		}
	}
	for _, v := range []string{"navigate", "files", "file_focus", "note", "overview", ""} {
		if _, ok := RetiredVerb(v); ok {
			t.Errorf("%q retired", v)
		}
	}
}
