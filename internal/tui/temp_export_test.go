package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestTempExportPopupEnterStartsOp(t *testing.T) {
	p := &tempExportPopup{files: []model.ExportFile{{RelPath: "a.txt", Data: []byte("x")}}}
	p.dest = newTextField("/tmp/repo.tmp/commit-abc1234")
	m := footerModel()
	_, cmd := p.update(m, keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter with files + dest should start the export op")
	}
}

func TestTempExportPopupEnterNoFilesNoOp(t *testing.T) {
	p := &tempExportPopup{}
	p.dest = newTextField("/tmp/repo.tmp/commit-abc1234")
	m := footerModel()
	_, cmd := p.update(m, keyMsg("enter"))
	if cmd != nil {
		t.Fatal("enter with no files should not start an op")
	}
}

func TestTempExportPopupEnterEmptyDestNoOp(t *testing.T) {
	p := &tempExportPopup{files: []model.ExportFile{{RelPath: "a.txt", Data: []byte("x")}}}
	p.dest = newTextField("   ")
	m := footerModel()
	_, cmd := p.update(m, keyMsg("enter"))
	if cmd != nil {
		t.Fatal("enter with a blank destination should not start an op")
	}
}

func TestTempExportPopupEscPops(t *testing.T) {
	p := &tempExportPopup{files: []model.ExportFile{{RelPath: "a.txt", Data: []byte("x")}}}
	p.dest = newTextField("/tmp/x")
	m := footerModel()
	m = m.pushLayer(p)
	mm, cmd := p.update(m, keyMsg("esc"))
	if cmd != nil {
		t.Fatal("esc should not start an op")
	}
	if layerOf[*tempExportPopup](mm) != nil {
		t.Fatal("esc should pop the temp-export popup off the stack")
	}
}

func TestTempExportResolvedMsgOpensPrefilledPopup(t *testing.T) {
	m := footerModel()
	m2, _ := m.Update(tempExportResolvedMsg{
		dir:   "/tmp/repo.tmp/commit-abc1234",
		files: []model.ExportFile{{RelPath: "a.txt", Data: []byte("x")}},
	})
	p := layerOf[*tempExportPopup](m2.(Model))
	if p == nil {
		t.Fatal("resolved msg should push a tempExportPopup")
	}
	if p.dest.Value() != "/tmp/repo.tmp/commit-abc1234" {
		t.Fatalf("popup destination should be prefilled with the resolved dir, got %q", p.dest.Value())
	}
	if len(p.files) != 1 {
		t.Fatalf("popup should carry the resolved files, got %d", len(p.files))
	}
}

func TestTempExportResolvedMsgErrSetsStatusNoPopup(t *testing.T) {
	m := footerModel()
	m2, _ := m.Update(tempExportResolvedMsg{err: errors.New("boom")})
	mm := m2.(Model)
	if layerOf[*tempExportPopup](mm) != nil {
		t.Fatal("an error result must not push a popup")
	}
	if mm.statusMsg == "" {
		t.Fatal("an error result should set statusMsg")
	}
}
