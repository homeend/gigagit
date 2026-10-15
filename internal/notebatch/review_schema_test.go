package notebatch

import (
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

// Every canonical review document gg writes validates against the strict
// review schema — part 2 §9's drift guard.
func TestCanonicalReviewMatchesStrictSchema(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "review-document.schema.json")
	lenient := loadSchema(t, "review-document.lenient.schema.json")
	docs := []string{
		`{"version":1,"summary":"## Fine\nNothing to say."}`,
		`{"version":1,"summary":"text","overview":"[gate](internal/x/gate.go:3-4)","meta":{"verdict":"approve","n":3},
		  "display":{"severity":{"show":["badge","count"],"label":"Severity","order":1,"style":"filled","as":"level",
		    "values":{"high":{"color":"red","glyph":"!","level":"high"},"low":{"color":"yellow","level":"low"}},"other":{"color":"grey"}}},
		  "files":[{"path":"internal/x/gate.go","summary":"the gate","meta":{"touched":"yes"},
		    "annotations":[{"newRange":[3,4],"summary":"The refusal.","rationale":"why","meta":{"severity":"high"},"display":{"severity":{"show":["frame"]}}},
		                   {"oldRange":[2,2],"summary":"was open"}]}]}`,
		`{"version":1,"summary":"legacy fields fold into meta","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"s","tags":["kind: question"],"confidence":"high"}]}]}`,
	}
	for _, in := range docs {
		d, err := ParseReview([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		canon := d.Canonical()
		if err := validateJSON(strict, canon); err != nil {
			t.Errorf("strict review schema rejects gg's canonical form:\n%s\n%v", canon, err)
		}
		if err := validateJSON(lenient, canon); err != nil {
			t.Errorf("lenient review schema rejects gg's canonical form:\n%s\n%v", canon, err)
		}
	}
}

// Review focus 1 + the lenient/strict split: unknown keys pass only the
// lenient schema; the shapes gg refuses pass neither.
func TestReviewSchemaStrictRejectsLenientAccepts(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "review-document.schema.json")
	lenient := loadSchema(t, "review-document.lenient.schema.json")
	extra := []byte(`{"version":1,"summary":"s","future":1,"files":[{"path":"a","extra":true,"annotations":[{"newRange":[1,1],"summary":"s","confidence":0.5}]}]}`)
	if err := validateJSON(strict, extra); err == nil {
		t.Fatal("strict schema accepted unknown keys")
	}
	if err := validateJSON(lenient, extra); err != nil {
		t.Fatalf("lenient schema rejected unknown keys: %v", err)
	}
	for _, bad := range []string{
		`{"version":1,"summary":""}`,
		`{"version":2,"summary":"s"}`,
		`{"version":1,"summary":"s","files":[{"path":"a","annotations":[{"newRange":[1,1],"oldRange":[1,1],"summary":"s"}]}]}`,
		`{"version":1,"summary":"s","files":[{"path":"a","annotations":[{"summary":"s"}]}]}`,
		// (first <= last is NOT expressible in JSON Schema: a reader relies on
		// gg's parser for that rule — the schemas' descriptions say so.)
		`{"version":1,"summary":"s","files":[{"path":"","annotations":[]}]}`,
		`{"version":1,"summary":"s","meta":"not an object"}`,
	} {
		if err := validateJSON(strict, []byte(bad)); err == nil {
			t.Errorf("strict schema accepted %s", bad)
		}
		if err := validateJSON(lenient, []byte(bad)); err == nil {
			t.Errorf("lenient schema accepted %s", bad)
		}
	}
}

// Review focus 2: the display vocabularies are closed in the schemas, while
// gg's parser keeps the document and drops the bad entry — a reader must
// expect a stored document its schema rejects on display alone.
func TestDisplayVocabularyIsClosed(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "review-document.schema.json")
	bad := `{"version":1,"summary":"s","display":{"k":{"show":["badge"],"values":{"v":{"color":"crimson","glyph":"ab"}}}}}`
	if err := validateJSON(strict, []byte(bad)); err == nil {
		t.Fatal("schema accepted colour crimson / glyph ab")
	}
	d, err := ParseReview([]byte(bad))
	if err != nil {
		t.Fatalf("gg refused the document outright: %v", err)
	}
	// ParseReview checks only the declaration's SHAPE (unknown fields); the
	// vocabularies are checked when the note is displayed (notemeta.Resolve).
	if r := notemeta.Resolve(nil, d.Display, nil); len(r.Problems) == 0 {
		t.Fatal("gg displayed the bad colour/glyph without naming a problem")
	}
}
