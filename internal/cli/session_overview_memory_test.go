package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// liveMemdocs stands in for the live gg: a real store behind the inbox.
func liveMemdocs(t *testing.T, inbox string) *agentdocs.Store {
	t.Helper()
	s := agentdocs.New()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			for _, c := range steer.Drain(inbox) {
				rep := steer.Reply{ID: c.ID, OK: true}
				if steer.IsMemdocVerb(c.Cmd) {
					rep = agentdocs.AnswerMemdoc(s, domain.CheckoutKey(c.Worktree), c)
				}
				if c.Wait {
					_ = steer.PostReply(inbox, rep)
				}
			}
		}
	}()
	return s
}

func dirNames(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return strings.Join(out, ",")
}

func TestAddMemoryNoLiveGG(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runOverview(t, svc, inbox, "", "add", "--title", "T", "--memory", "--text", "hello")
	if code != 1 || out != "" || !strings.Contains(errb, "no gg session for this worktree: a memory document needs one") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errb)
	}
	if got := dirNames(t, dir); got != "" {
		t.Fatalf("written: %s", got)
	}
}

func TestAddMemoryIDEOnlyRefused(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	if err := steer.Touch(inbox, steer.IDEPresence, steer.Presence{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runOverview(t, svc, inbox, "", "add", "--title", "T", "--memory", "--text", "x"); code != 1 || !strings.Contains(errb, "a memory document needs one") {
		t.Fatalf("code=%d err=%q", code, errb)
	}
}

// Serial: steerReplyWaitForTest is a package var.
func TestAddMemoryNoReply(t *testing.T) {
	svc, inbox, _ := docsCLI(t)
	livePresence(t, inbox) // nobody drains
	old := steerReplyWaitForTest
	steerReplyWaitForTest = 50 * 1e6
	defer func() { steerReplyWaitForTest = old }()
	code, out, errb := runOverview(t, svc, inbox, "", "add", "--title", "T", "--memory", "--background", "--text", "x")
	if code != 1 || !strings.Contains(errb, "gg did not answer; it may still add it") {
		t.Fatalf("code=%d err=%q", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !agentdocs.ValidID(lines[0]) || !strings.HasPrefix(lines[1], "link: gg://") || !strings.Contains(lines[1], "?overview="+lines[0]) {
		t.Fatalf("out = %q", out)
	}
}

func TestAddTextExclusive(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	if code, _, errb := runOverview(t, svc, inbox, "", "add", "--title", "T", "--text", "x", "--file", "f"); code != 2 || !strings.Contains(errb, "--text or --file") {
		t.Fatalf("code=%d err=%q", code, errb)
	}
	if code, _, errb := runOverview(t, svc, inbox, "", "set", "20261102T031500-a1b2", "--memory", "--text", "x"); code != 2 || !strings.Contains(errb, "--memory") {
		t.Fatalf("code=%d err=%q", code, errb)
	}
}

func TestAddPrintsLinkForMemoryOnly(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runOverview(t, svc, inbox, "", "add", "--title", "T", "--background", "--text", "[a](a.go:1)")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("a file add's text output changed: %q", out) // $(gg session overview add …) is the id
	}
	if !strings.Contains(dirNames(t, dir), lines[0]+".md") {
		t.Fatal("--text did not write the file")
	}
	store := liveMemdocs(t, inbox)
	livePresence(t, inbox)
	code, out, errb = runOverview(t, svc, inbox, "", "add", "--title", "T", "--memory", "--plain", "--background", "--text", "see [a](a.go:1)")
	if code != 0 || errb != "" {
		t.Fatalf("code=%d err=%q", code, errb)
	}
	lines = strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "link: gg://") {
		t.Fatalf("plain add printed: %q", out)
	}
	root := domain.CheckoutKey(callerWorktree(svc))
	o, ok := store.OverviewIn(root, lines[0])
	if !ok || !o.Memory || !o.Plain || o.Text != "see [a](a.go:1)" {
		t.Fatalf("doc = %+v, %v", o, ok)
	}
	if strings.Contains(dirNames(t, dir), lines[0]) {
		t.Fatal("memory document on disk")
	}
}

func TestAddJSONHasLinkAndBacking(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out, _ := runOverview(t, svc, inbox, "", "add", "--title", "T", "--background", "--plain", "--json", "--text", "x")
	var w struct {
		Link, Backing string
		Plain         bool
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if !strings.HasPrefix(w.Link, "gg://") || w.Backing != "file" || !w.Plain {
		t.Fatalf("w = %+v", w)
	}
}

func TestPlainNoAnchorWarning(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	if _, out, _ := runOverview(t, svc, inbox, "", "add", "--title", "T", "--plain", "--background", "--text", "x"); strings.Contains(out, "no anchor") {
		t.Fatalf("out = %q", out)
	}
	if _, out, _ := runOverview(t, svc, inbox, "", "add", "--title", "T", "--background", "--text", "x"); !strings.Contains(out, "no anchor") {
		t.Fatalf("a file markdown document lost its warning: %q", out)
	}
}

func TestListMemoryRowsTagged(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, added, _ := runOverview(t, svc, inbox, "", "add", "--title", "F", "--background", "--text", "x")
	fileID := strings.Split(added, "\n")[0]
	_, fileOnly, _ := runOverview(t, svc, inbox, "", "list")
	if strings.Contains(fileOnly, "memory") {
		t.Fatalf("no live gg: %q", fileOnly)
	}
	liveMemdocs(t, inbox)
	livePresence(t, inbox)
	_, added, _ = runOverview(t, svc, inbox, "", "add", "--title", "M", "--memory", "--plain", "--background", "--text", "y")
	memID := strings.Split(added, "\n")[0]
	_, out, errb := runOverview(t, svc, inbox, "", "list")
	if !strings.Contains(out, fileOnly) {
		t.Fatalf("file row changed: %q (was %q; err %q)", out, fileOnly, errb)
	}
	if !strings.Contains(out, memID+"\t0 anchors\tM\tmemory\n") {
		t.Fatalf("out = %q", out)
	}
	_ = fileID
	_, out, _ = runOverview(t, svc, inbox, "", "show", memID)
	if !strings.HasSuffix(out, "\n\ny\n") {
		t.Fatalf("show = %q", out)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "list", "--json")
	if !strings.Contains(out, `"backing": "memory"`) || !strings.Contains(out, `"backing": "file"`) {
		t.Fatalf("json = %q", out)
	}
	if code, out, _ := runOverview(t, svc, inbox, "", "export", memID); code != 1 || out != "" {
		t.Fatalf("export code=%d out=%q", code, out)
	}
	if code, _, errb := runOverview(t, svc, inbox, "", "export", memID); !strings.Contains(errb, "overview "+memID+" has no export: read it with gg overview show") || code != 1 {
		t.Fatalf("export err = %q", errb)
	}
	if code, out, _ := runOverview(t, svc, inbox, "", "set", memID, "--text", "z"); code != 0 || !strings.HasPrefix(out, memID) {
		t.Fatalf("set code=%d out=%q", code, out)
	}
	if code, out, _ := runOverview(t, svc, inbox, "", "rm", memID); code != 0 || !strings.Contains(out, "removed "+memID) {
		t.Fatalf("rm code=%d out=%q", code, out)
	}
	if code, _, errb := runOverview(t, svc, inbox, "", "show", memID); code != 1 || !strings.Contains(errb, "no overview") {
		t.Fatalf("show after rm code=%d err=%q", code, errb)
	}
}

func TestMemdocsPresenceDoesNotRouteNavigate(t *testing.T) {
	t.Parallel()
	inbox := t.TempDir()
	if err := steer.Touch(inbox, steer.MemdocsPresence, steer.Presence{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	r := routeFor(inbox)
	if r.tuiOK || r.webOK || r.ideOK {
		t.Fatalf("the memdocs presence routed a verb: %+v", r)
	}
	if rep, res := steer.AskMemdoc(inbox, steer.Command{Cmd: "memdoc_list", Worktree: "x"}, 1e6); res != steer.AskNoAnswer {
		t.Fatalf("ask = %+v %v", rep, res)
	}
}

// docsCLISteerDir is docsCLI with the inbox a link's reader computes for the repo.
func docsCLISteerDir(t *testing.T) (svc *domain.Service, inbox, dir string) {
	t.Helper()
	svc, _, _ = docsCLI(t)
	cd, err := svc.GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	inbox = config.SessionSteerDir(cd, callerWorktree(svc))
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	return svc, inbox, steer.DocsDir(inbox)
}

// docsLink adds a memory document and returns its link and id.
func docsLink(t *testing.T, svc *domain.Service, inbox string) (id, link string) {
	t.Helper()
	code, out, errb := runOverview(t, svc, inbox, "", "add", "--title", "M", "--memory", "--plain", "--background", "--text", "<b>body</b>")
	if code != 0 {
		t.Fatalf("add: %d %q", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return lines[0], strings.TrimPrefix(lines[1], "link: ")
}

func TestShowLinkFallsBackToLive(t *testing.T) {
	svc, inbox, _ := docsCLISteerDir(t)
	liveMemdocs(t, inbox)
	if err := steer.Touch(inbox, steer.MemdocsPresence, steer.Presence{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	keepPresenceAlive(t, inbox, steer.MemdocsPresence)
	_, link := docsLink(t, svc, inbox)
	var o, e bytes.Buffer
	other := domain.Open(newCLIRepo(t))
	if code := overviewShow(other, []string{link}, &o, &e); code != 0 {
		t.Fatalf("show: %d %q", code, e.String())
	}
	if !strings.Contains(o.String(), "<b>body</b>") {
		t.Fatalf("show = %q", o.String())
	}
	if r := routeFor(inbox); r.tuiOK || r.webOK {
		t.Fatal("a kept memdocs inbox looks like a live session")
	}
}

func TestShowLinkGoneAfterQuit(t *testing.T) {
	svc, inbox, _ := docsCLISteerDir(t)
	liveMemdocs(t, inbox)
	if err := steer.Touch(inbox, steer.MemdocsPresence, steer.Presence{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	_, link := docsLink(t, svc, inbox)
	steer.Remove(inbox, steer.MemdocsPresence)
	var o, e bytes.Buffer
	code := overviewShow(svc, []string{link}, &o, &e)
	if code != 1 || !strings.Contains(e.String(), "no longer exists") {
		t.Fatalf("show: %d %q", code, e.String())
	}
}

func TestOpenMemoryLinkElsewhereRefused(t *testing.T) {
	svc, inbox, _ := docsCLISteerDir(t)
	liveMemdocs(t, inbox)
	if err := steer.Touch(inbox, steer.MemdocsPresence, steer.Presence{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	keepPresenceAlive(t, inbox, steer.MemdocsPresence)
	_, link := docsLink(t, svc, inbox)
	launched := false
	old := LaunchTUI
	LaunchTUI = func(string, model.Link) int { launched = true; return 0 }
	defer func() { LaunchTUI = old }()
	var o, e bytes.Buffer
	code := cmdOpen(svc, []string{link}, &o, &e)
	if code != 1 || launched || !strings.Contains(e.String(), "read it with gg overview show") {
		t.Fatalf("open: %d launched=%v %q", code, launched, e.String())
	}
}
