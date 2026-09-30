package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

// toolStatusServer serves a repo whose tool-template status read (the seam)
// reports one update offer for a web-only block in its own config file.
func toolStatusServer(t *testing.T) (*Server, string, domain.ToolTemplateStatus) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	old := config.ToolCommand{Category: "conflict_complete", Name: "Claude — resolve & complete (yolo, headless)", Mode: "capture", Frontends: []string{"web"}, Command: "claude -p x"}
	if err := config.AppendToolCommands(path, []config.ToolCommand{old}); err != nil {
		t.Fatal(err)
	}
	nw := old
	nw.Frontends, nw.TemplateVersion = []string{"tui", "web"}, 2
	st := domain.ToolTemplateStatus{Path: path, Block: old, New: nw, Kind: domain.ToolUpdateAvailable, ToVersion: 2, Edited: true, ToolLabel: "Claude Code"}
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.toolStatuses = func(context.Context) []domain.ToolTemplateStatus {
		// A fresh read each call, like the real one: a taken offer is gone.
		if got, _ := config.ToolCommandsIn(path); len(got) == 1 && got[0].TemplateVersion == 2 {
			return nil
		}
		return []domain.ToolTemplateStatus{st}
	}
	return srv, path, st
}

func TestExtToolsReportsUpdateOffer(t *testing.T) {
	t.Parallel()
	srv, _, st := toolStatusServer(t)
	srv.detectTools = fakeDetections
	ts := serve(t, srv)
	var got struct {
		Offers []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
			Status   string `json:"status"`
			Reason   string `json:"reason"`
			NewText  string `json:"new_text"`
			OfferID  string `json:"offer_id"`
			Declined bool   `json:"declined"`
		} `json:"template_offers"`
	}
	if code := getJSON(t, ts, "/api/exttools", &got); code != http.StatusOK {
		t.Fatalf("GET code %d", code)
	}
	if len(got.Offers) != 1 {
		t.Fatalf("offers = %+v", got.Offers)
	}
	o := got.Offers[0]
	if o.Status != "update" || o.OfferID != promptstate.ToolUpdateID(st.OfferKey()) || !strings.Contains(o.NewText, `frontends = ["tui", "web"]`) || o.Reason == "" {
		t.Fatalf("offer = %+v", o)
	}
}

func TestExtToolsUpdateByOfferID(t *testing.T) {
	t.Parallel()
	srv, path, st := toolStatusServer(t)
	ts := serve(t, srv)
	id := promptstate.ToolUpdateID(st.OfferKey())

	if code, _ := postJSONRaw(t, ts, "/api/exttools/update", `{"offer_id":"deadbeef"}`); code != http.StatusNotFound {
		t.Fatalf("unknown offer: %d", code)
	}
	if code, body := postJSONRaw(t, ts, "/api/exttools/update", `{"offer_id":"`+id+`"}`); code != http.StatusOK {
		t.Fatalf("update: %d %v", code, body)
	}
	got, _ := config.ToolCommandsIn(path)
	if len(got) != 1 || got[0].TemplateVersion != 2 || len(got[0].Frontends) != 2 {
		t.Fatalf("not written: %+v", got)
	}
}

func TestExtToolsKeepByOfferID(t *testing.T) {
	t.Parallel()
	srv, path, st := toolStatusServer(t)
	ts := serve(t, srv)
	before, _ := os.ReadFile(path)
	id := promptstate.ToolUpdateID(st.OfferKey())
	if code, body := postJSONRaw(t, ts, "/api/exttools/keep", `{"offer_id":"`+id+`"}`); code != http.StatusOK {
		t.Fatalf("keep: %d %v", code, body)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("keep must not write the config")
	}
	store := promptstate.NewFileStore(filepath.Join(filepath.Dir(srv.reposStatePath()), "prompts.toml"))
	if !store.DeclinedToolUpdates()[id] {
		t.Fatal("keep must remember the offer")
	}
}
