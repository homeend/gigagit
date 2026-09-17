package git

// literalPathspec turns repo-root-relative paths into pathspec elements that
// git matches VERBATIM, by prefixing each with the `:(literal)` magic.
//
// A raw path is not a path to git, it is a pathspec, and two of its grammar
// rules silently change the answer:
//
//   - A leading ':' is short-form pathspec magic. A tracked file literally
//     named ":colon.txt" passed raw matches NOTHING, with exit 0 — so a
//     presence probe calls it absent and the comparison reports a false
//     addition or deletion instead of an error. (Legal on POSIX, illegal on
//     Windows, so rare — but wrong, and silent.)
//   - Glob metacharacters match other paths. `f[0-9].txt` passed raw to
//     `ls-files` returns f9.txt TOO, so a probe's result gains entries for
//     paths nobody asked about.
//
// `:(literal)` disables both. Verified on git 2.43.0 against `ls-files` and
// `ls-tree -r`: it finds ":colon.txt" and "sub/:weird name.txt", still matches
// an ordinary "a.txt", and narrows `f[0-9].txt` back to the one real file.
//
// It belongs inside the verbs rather than at the call sites: a caller holding
// a path out of a diff or a tree listing should not have to know git's
// pathspec grammar to ask about it.
func literalPathspec(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = ":(literal)" + p
	}
	return out
}
