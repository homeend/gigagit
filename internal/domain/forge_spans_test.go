package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/observ"
)

// The forge CLI's calls land in the session's span ring, so a slow or failing
// gh shows up in the operation log beside the git calls. Serial: sets env.
func TestForgeCallsReachTheSessionRing(t *testing.T) {
	dir := gittest.BasicRepo(t, "hi\n")
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	t.Setenv(forgetest.EnvFixtures, "")
	b, err := os.ReadFile(filepath.Join("..", "forge", "testdata", "pr-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), map[string]string{"pr-list.json": string(b)})
	ring := observ.NewRing(50)
	svc := OpenTUIWithRing(dir, ring)
	if st := svc.ForgeStatus(context.Background()); !st.Available() {
		t.Fatalf("forge unavailable: %v", st.Err)
	}
	for _, sp := range ring.Snapshot() {
		if sp.Name == "gh pr list (detect)" {
			return
		}
	}
	t.Fatalf("no gh span in the session ring: %+v", ring.Snapshot())
}
