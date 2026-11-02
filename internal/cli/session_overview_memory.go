package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const memdocNeedsLive = "no gg session for this worktree: a memory document needs one"

// askMemdoc asks the gg this process talks to about a memory document: $GG_INBOX
// while that gg is live, else this worktree's inbox.
func askMemdoc(dir sessDir, c steer.Command) (steer.Reply, steer.AskResult) {
	return steer.AskMemdoc(preferredInbox(dir.inbox), c, steerReplyWaitForTest)
}

// liveMemoryOverviews are the worktree's memory documents, newest first; none
// with no live gg.
func liveMemoryOverviews(dir sessDir, worktree string) []steer.Overview {
	rep, res := askMemdoc(dir, steer.Command{Cmd: "memdoc_list", Worktree: worktree})
	if res != steer.AskAnswered || !rep.OK {
		return nil
	}
	out := make([]steer.Overview, 0, len(rep.Overviews))
	for i := len(rep.Overviews) - 1; i >= 0; i-- {
		o := rep.Overviews[i]
		o.Backing = agentdocs.BackingMemory
		out = append(out, o)
	}
	return out
}

// memdocRow is a listing's end of line: a file row stays as it always was.
func memdocRow(o steer.Overview) string {
	if o.Backing == agentdocs.BackingMemory {
		return "\tmemory"
	}
	return ""
}

// memoryOverviewAdd is `gg session overview add --memory`: the document goes to
// the live gg, never to a file. The id is minted here, so the link is known even
// when gg does not answer, and a retry with it replaces instead of duplicating.
func memoryOverviewAdd(dir sessDir, svc *domain.Service, title, text string, plain, background, asJSON bool, stdout, stderr io.Writer) int {
	wt := callerWorktree(svc)
	if wt == "" {
		fmt.Fprintln(stderr, "session overview add: not in a git worktree")
		return 1
	}
	id := agentdocs.MintID(clock.Now(), nil)
	link, err := domain.SessionOverviewLink(wt, id)
	if err != nil {
		fmt.Fprintln(stderr, "session overview add:", err)
		return 1
	}
	rep, res := askMemdoc(dir, steer.Command{Cmd: "memdoc_add", FileID: id, Title: title, Text: text, Worktree: wt, Plain: &plain})
	switch res {
	case steer.AskNoLive:
		fmt.Fprintln(stderr, memdocNeedsLive)
		return 1
	case steer.AskNoAnswer:
		fmt.Fprintf(stdout, "%s\nlink: %s\n", id, link)
		fmt.Fprintln(stderr, "gg did not answer; it may still add it")
		return 1
	}
	if !rep.OK {
		fmt.Fprintln(stderr, rep.Error)
		return 1
	}
	w := steer.Overview{ID: id, Title: title, Plain: plain, Backing: agentdocs.BackingMemory, Link: link}
	if len(rep.Overviews) == 1 {
		w = rep.Overviews[0]
		w.Link, w.Backing = link, agentdocs.BackingMemory
	}
	if asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stdout, "%s\nlink: %s\n", id, link)
		if !plain {
			backticked, anchors := domain.OverviewTextProblems(text)
			for _, b := range backticked {
				fmt.Fprintln(stdout, "in backticks: "+b)
			}
			if anchors == 0 {
				fmt.Fprintln(stdout, noAnchorWarning)
			}
		}
	}
	if !background {
		focusAfterWrite(dir, id, wt, stdout, stderr)
	}
	return 0
}

// memorySet is set for an id no file holds. done=false: no live gg holds it
// either, and the caller answers as it always has.
func memorySet(dir sessDir, worktree, id, title, text string, plainSeen, plain, background, asJSON bool, stdout, stderr io.Writer) (int, bool) {
	c := steer.Command{Cmd: "memdoc_set", FileID: id, Title: title, Text: text, Worktree: worktree}
	if plainSeen {
		c.Plain = &plain
	}
	rep, res := askMemdoc(dir, c)
	switch {
	case res == steer.AskNoLive:
		return 0, false
	case res == steer.AskNoAnswer:
		fmt.Fprintln(stderr, "gg did not answer; it may still change it")
		return 1, true
	case !rep.OK:
		fmt.Fprintln(stderr, rep.Error)
		return 1, true
	}
	if asJSON && len(rep.Overviews) == 1 {
		w := rep.Overviews[0]
		w.Link, _ = domain.SessionOverviewLink(worktree, id)
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintln(stdout, id)
	}
	if !background {
		focusAfterWrite(dir, id, worktree, stdout, stderr)
	}
	return 0, true
}

// memoryOverviewOf reads a link's memory document from the live gg behind that
// checkout's inbox, as the store's shape.
func memoryOverviewOf(checkout, commonDir, id string) (agentdocs.Overview, bool) {
	inbox := config.SessionSteerDir(commonDir, checkout)
	rep, res := steer.AskMemdoc(inbox, steer.Command{Cmd: "memdoc_show", FileID: id, Worktree: checkout}, steerReplyWaitForTest)
	if res != steer.AskAnswered || !rep.OK || len(rep.Overviews) != 1 {
		return agentdocs.Overview{}, false
	}
	w := rep.Overviews[0]
	o := agentdocs.Overview{ID: w.ID, Title: w.Title, Text: w.Text, Author: w.Author, Plain: w.Plain, Memory: true}
	if t, err := time.Parse(time.RFC3339, w.Created); err == nil {
		o.Created = t
	}
	if !o.Plain {
		_, o.Anchors = agentdocs.ParseOverview(o.Text)
	}
	return o, true
}

func backingOf(o agentdocs.Overview) string {
	if o.Memory {
		return agentdocs.BackingMemory
	}
	return agentdocs.BackingFile
}

// memoryLinkElsewhere refuses to launch or steer for a memory document whose
// gg has moved to another worktree: a new TUI would not hold it, and a verb
// reaching that gg would not land it.
const memoryLinkElsewhere = "that document is in a gg showing another worktree: read it with gg overview show <link>"
