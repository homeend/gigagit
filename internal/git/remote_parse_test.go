package git

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestParseRemoteBranches(t *testing.T) {
	t.Parallel()
	// Format: %(refname:short)\x00%(objectname:short)\x00%(committerdate:unix)
	data := []byte(
		"origin/main\x00abc1234\x001700000000\n" +
			"origin/feature/x\x00def5678\x001700000100\n" +
			"upstream/main\x009990000\x001700000200\n" +
			"origin\x00abc1234\x001700000000\n" + // origin/HEAD symref short form -> dropped
			"origin/HEAD\x00abc1234\x001700000000\n" + // explicit HEAD -> dropped
			"\n", // blank -> skipped
	)
	got, err := ParseRemoteBranches(data)
	if err != nil {
		t.Fatalf("ParseRemoteBranches: %v", err)
	}
	want := []model.RemoteBranch{
		{Name: "origin/main", Remote: "origin", Branch: "main", Hash: "abc1234", UnixTime: 1700000000},
		{Name: "origin/feature/x", Remote: "origin", Branch: "feature/x", Hash: "def5678", UnixTime: 1700000100},
		{Name: "upstream/main", Remote: "upstream", Branch: "main", Hash: "9990000", UnixTime: 1700000200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
