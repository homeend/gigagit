package exttool

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeLook simulates exec.LookPath over a fixed set of installed binaries.
func fakeLook(installed map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := installed[name]; ok {
			return p, nil
		}
		return "", errors.New("not found")
	}
}

// fakeStat simulates os.Stat over a fixed set of existing paths.
func fakeStat(existing map[string]bool) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		if existing[path] {
			return nil, nil // callers only check the error
		}
		return nil, os.ErrNotExist
	}
}

func TestDetectFindsBinsOnPath(t *testing.T) {
	dets := Detect(fakeLook(map[string]string{"claude": "/usr/bin/claude", "meld": "/usr/bin/meld"}), fakeStat(nil))
	got := map[string]string{}
	for _, d := range dets {
		got[d.Tool.ID] = d.Bin
	}
	if got["claude"] != "claude" {
		t.Errorf("claude Bin = %q, want bare name %q (PATH hit)", got["claude"], "claude")
	}
	if got["meld"] != "meld" {
		t.Errorf("meld Bin = %q, want %q", got["meld"], "meld")
	}
	if _, ok := got["junie"]; ok {
		t.Errorf("junie detected but not installed")
	}
}

func TestDetectExtraProbeYieldsAbsolutePath(t *testing.T) {
	// Meld's Windows install dir is off PATH; an ExtraProbes hit must return
	// the absolute path so the generated command can run it.
	var meld Tool
	for _, tl := range Builtins() {
		if tl.ID == "meld" {
			meld = tl
		}
	}
	if len(meld.ExtraProbes) == 0 {
		t.Fatal("meld has no ExtraProbes; expected the Windows install path")
	}
	probe := meld.ExtraProbes[0]
	dets := Detect(fakeLook(nil), fakeStat(map[string]bool{probe: true}))
	if len(dets) != 1 || dets[0].Tool.ID != "meld" || dets[0].Bin != probe {
		t.Fatalf("dets = %+v, want one meld detection with Bin=%q", dets, probe)
	}
}

func TestBuiltinsCatalogInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range Builtins() {
		if tl.ID == "" || tl.Label == "" || len(tl.Bins) == 0 {
			t.Errorf("tool %+v: ID/Label/Bins must be set", tl)
		}
		for _, ct := range tl.Commands {
			switch ct.Category {
			case CatConflict, CatCommitMessage, CatReview:
			default:
				t.Errorf("%s/%s: bad category %q", tl.ID, ct.Name, ct.Category)
			}
			switch ct.Mode {
			case ModeTerminal, ModeCapture:
			default:
				t.Errorf("%s/%s: bad mode %q", tl.ID, ct.Name, ct.Mode)
			}
			switch ct.WhenOp {
			case "", "merge", "rebase", "cherry-pick", "revert":
			default:
				t.Errorf("%s/%s: bad when_op %q", tl.ID, ct.Name, ct.WhenOp)
			}
			if ct.PerFile && ct.Category != CatConflict {
				t.Errorf("%s/%s: per_file outside conflict", tl.ID, ct.Name)
			}
			if !strings.Contains(ct.Command, "<bin>") {
				t.Errorf("%s/%s: command must start from <bin>", tl.ID, ct.Name)
			}
			key := string(ct.Category) + "\x00" + ct.Name
			if seen[key] {
				t.Errorf("duplicate (category,name): %s/%s", ct.Category, ct.Name)
			}
			seen[key] = true
		}
	}
}

func TestStage1CatalogIsConflictOnly(t *testing.T) {
	for _, tl := range Builtins() {
		for _, ct := range tl.Commands {
			if ct.Category != CatConflict {
				t.Errorf("stage 1 ships conflict templates only; found %s/%s", ct.Category, ct.Name)
			}
		}
	}
}

func TestGenerateCommand(t *testing.T) {
	ct := CommandTemplate{Command: "<bin> --auto-merge <local>"}
	if got := GenerateCommand(ct, "meld"); got != "meld --auto-merge <local>" {
		t.Errorf("bare bin: got %q", got)
	}
	if got := GenerateCommand(ct, `C:\Program Files\Meld\Meld.exe`); got != `"C:\Program Files\Meld\Meld.exe" --auto-merge <local>` {
		t.Errorf("spaced bin must be double-quoted: got %q", got)
	}
}
