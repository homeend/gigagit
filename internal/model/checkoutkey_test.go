package model

import "testing"

func withCaseFold(t *testing.T, on bool) {
	t.Helper()
	was := CaseInsensitivePaths()
	SetCaseInsensitivePaths(on)
	t.Cleanup(func() { SetCaseInsensitivePaths(was) })
}

func TestKeyOfFoldsCaseOnCaseInsensitivePaths(t *testing.T) {
	withCaseFold(t, true)
	if KeyOf("/Work/Repo") != KeyOf("/work/repo") {
		t.Fatalf("%q vs %q: two spellings of one directory must share a key", KeyOf("/Work/Repo"), KeyOf("/work/repo"))
	}
	if KeyOf("/work/repo/") != KeyOf("/work/./repo") {
		t.Fatal("cleaning is part of the key")
	}
	if !SamePath("/Work/Repo", "/work/repo/") {
		t.Fatal("SamePath must agree with KeyOf")
	}
}

func TestKeyOfKeepsCaseOnCaseSensitivePaths(t *testing.T) {
	withCaseFold(t, false)
	if KeyOf("/Work/Repo") == KeyOf("/work/repo") {
		t.Fatal("a case-sensitive filesystem tells the two apart")
	}
	if KeyOf("") != "" {
		t.Fatal("an empty path is an unset identity")
	}
}
