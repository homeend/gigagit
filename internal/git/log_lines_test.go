package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestLogLinesArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "abc1234\x1ffirst subject\ndef5678\x1fsecond\n"})
	r := &Repo{Runner: f}
	got, err := r.LogLines(context.Background(), "main..HEAD", 5)
	if err != nil {
		t.Fatalf("LogLines: %v", err)
	}
	wantArgv := []string{"log", "--format=%h%x1f%s", "-n", "5", "main..HEAD"}
	if !reflect.DeepEqual(f.Calls[0].Argv, wantArgv) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, wantArgv)
	}
	want := []model.LogLine{{Hash: "abc1234", Subject: "first subject"}, {Hash: "def5678", Subject: "second"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
}

func TestCommitLineArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "abc1234\x1fthe subject\n"})
	r := &Repo{Runner: f}
	got, err := r.CommitLine(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("CommitLine: %v", err)
	}
	wantArgv := []string{"log", "-1", "--format=%h%x1f%s", "HEAD"}
	if !reflect.DeepEqual(f.Calls[0].Argv, wantArgv) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, wantArgv)
	}
	if got.Hash != "abc1234" || got.Subject != "the subject" {
		t.Fatalf("line = %+v", got)
	}
}

func TestCommitLineEmptyOutputErrors(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	r := &Repo{Runner: f}
	if _, err := r.CommitLine(context.Background(), "HEAD"); err == nil {
		t.Fatal("want error on empty output")
	}
}

func TestParseLogLinesSkipsMalformed(t *testing.T) {
	got := parseLogLines("abc\x1fok\nnot-a-log-line\n\n")
	if len(got) != 1 || got[0].Hash != "abc" {
		t.Fatalf("got %+v", got)
	}
}
