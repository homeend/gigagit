package steer

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
