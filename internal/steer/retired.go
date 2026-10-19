package steer

// RetiredVerb says whether cmd is one of the note_*/overview_* verbs the
// session-docs spec retired (2026-10-18 §6.3): an agent writes the file
// instead (gg session note|overview, or by hand). The refusal names the
// fix; every consumer answers it the same way, as it answers an unknown
// verb. steer-cases.json carries them as `retired` cases.
func RetiredVerb(cmd string) (refusal string, ok bool) {
	switch cmd {
	case "note_add", "note_list", "note_show", "note_rm":
		return cmd + " is retired: write the file (gg session note add)", true
	case "overview_add", "overview_set", "overview_list", "overview_show", "overview_rm":
		return cmd + " is retired: write the file (gg session overview add)", true
	}
	return "", false
}
