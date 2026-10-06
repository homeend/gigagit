package git

import (
	"errors"
	"testing"
)

func TestIsLocalChangesRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		msg  string
		want bool
	}{
		{"git pull: exit 1: error: Your local changes to the following files would be overwritten by merge:\n\tf.txt", true},
		{"git pull: exit 1: error: The following untracked working tree files would be overwritten by merge:\n\tn.txt", true},
		{"git pull: exit 128: error: cannot pull with rebase: You have unstaged changes.", true},
		{"git pull: exit 128: fatal: Not possible to fast-forward, aborting.", false},
		{"git pull: exit 128: fatal: unable to access 'https://x/': Could not resolve host", false},
	} {
		if got := IsLocalChangesRefusal(errors.New(tc.msg)); got != tc.want {
			t.Errorf("IsLocalChangesRefusal(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
	if IsLocalChangesRefusal(nil) {
		t.Error("nil error must not be a refusal")
	}
}
