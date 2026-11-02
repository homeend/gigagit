package steer

import "time"

// MemdocsPresence names the presence a gg keeps in the inbox of a worktree
// holding a memory document it is not otherwise answering for. Only memdoc_*
// routing reads it: every other verb still finds no session there.
const MemdocsPresence = "memdocs.json"

// IsMemdocVerb says whether cmd is a memory-document verb. These touch no
// screen, so a consumer answers them before any other check.
func IsMemdocVerb(cmd string) bool {
	switch cmd {
	case "memdoc_add", "memdoc_set", "memdoc_list", "memdoc_show", "memdoc_rm":
		return true
	}
	return false
}

// AskResult says how an AskMemdoc went.
type AskResult int

const (
	AskNoLive   AskResult = iota // no gg answers memory documents for this inbox
	AskNoAnswer                  // one is there and did not answer in time
	AskAnswered
)

// AskMemdoc delivers a memdoc_* command to the gg behind inbox and waits. A
// TUI takes it as a file whichever interface is active (the page shares its
// store), so does a gg holding the inbox for memory documents alone; a
// standalone page takes it over HTTP. An IDE never does: it holds no documents.
func AskMemdoc(inbox string, c Command, wait time.Duration) (Reply, AskResult) {
	if inbox == "" {
		return Reply{}, AskNoLive
	}
	_, tui := Live(inbox, TUIPresence)
	_, kept := Live(inbox, MemdocsPresence)
	if tui || kept {
		c.Wait = true
		id, err := Post(inbox, c)
		if err != nil {
			return Reply{}, AskNoAnswer
		}
		rep, got := AwaitReply(inbox, id, wait)
		if !got {
			return Reply{}, AskNoAnswer
		}
		return rep, AskAnswered
	}
	if p, ok := Live(inbox, WebPresence); ok && !p.Inactive {
		if c.ID == "" {
			c.ID = NewID()
		}
		rep, err := PostHTTPReply(p.URL, c)
		if err != nil {
			return Reply{}, AskNoAnswer
		}
		return rep, AskAnswered
	}
	return Reply{}, AskNoLive
}
