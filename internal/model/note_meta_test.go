package model

import (
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestMetaValuePrefersAdded(t *testing.T) {
	t.Parallel()
	n := Note{Meta: []notemeta.KV{{Key: "severity", Value: "high"}},
		Added: []notemeta.Added{{Key: "severity", Value: "low"}}}
	if got := MetaValue(n, "severity"); got != "low" {
		t.Fatalf("MetaValue = %q", got)
	}
	if got := MetaValue(n, "kind"); got != "" {
		t.Fatalf("MetaValue(kind) = %q", got)
	}
}
