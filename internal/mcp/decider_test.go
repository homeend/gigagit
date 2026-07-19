package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
)

func TestStaticDeciderUnknownIDFailsLoud(t *testing.T) {
	d := staticDecider{policy: map[string]string{"overwrite": "cancel"}}
	_, err := d.Decide(context.Background(), engine.DecisionRequest{
		ID: "mystery", Options: []string{"a", "b"},
	})
	if err == nil || !strings.Contains(err.Error(), `"mystery"`) {
		t.Fatalf("unknown decision id must fail loud, got err=%v", err)
	}
	res, err := d.Decide(context.Background(), engine.DecisionRequest{
		ID: "overwrite", Options: []string{"overwrite", "cancel"},
	})
	if err != nil || res.Option != "cancel" {
		t.Fatalf("policy answer: res=%+v err=%v", res, err)
	}
}
