package web

import (
	"net/http"
	"strings"
	"testing"
)

// gg session overview add writes the file and posts file_focus at once:
// the server must find the document before its store's watch has fired.
func TestWebFileFocusOfAJustWrittenOverviewReloadsFirst(t *testing.T) {
	t.Parallel()
	s, _, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T000000-ffff.md", "+++\ntitle = \"Fresh\"\nauthor = \"agent\"\ncreated = 2026-01-01T00:00:00Z\n+++\nSee [a](f.txt:1).\n")
	code, r := steerAsk(t, s, `{"id":"c1","cmd":"file_focus","file_id":"20260101T000000-ffff","wait":true}`)
	if code != http.StatusOK || !r.OK || !strings.HasPrefix(r.Detail, "focused overview 20260101T000000-ffff \"Fresh\"") {
		t.Fatalf("code %d reply = %+v", code, r)
	}
}
