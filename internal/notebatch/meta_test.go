package notebatch

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestAgentContextMetaAndDisplay(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"version":1,
	  "display":{"kind":{"show":["badge"]}},
	  "files":[{"path":"a.go","annotations":[
	    {"newRange":[1,1],"summary":"s","confidence":"high","meta":{"kind":"issue","n":3},
	     "display":{"kind":{"show":["line"]}}},
	    {"newRange":[2,2],"summary":"t","meta":{"kind":"good"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	first, second := b.Items[0], b.Items[1]
	if !reflect.DeepEqual(first.Meta, []notemeta.KV{{Key: "confidence", Value: "high"}, {Key: "kind", Value: "issue"}, {Key: "n", Value: "3"}}) {
		t.Fatalf("meta = %v", first.Meta)
	}
	if first.Display["kind"].Show[0] != "line" {
		t.Fatalf("an annotation's display must replace the batch's: %+v", first.Display)
	}
	if second.Display["kind"].Show[0] != "badge" {
		t.Fatalf("the batch display must reach every note: %+v", second.Display)
	}
}

func TestCommentBatchMetaAndDisplay(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":3,"summary":"s",
	  "meta":{"status":"disagreed"},"display":{"status":{"show":["badge"],"colour":"x"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if b.Items[0].Meta[0] != (notemeta.KV{Key: "status", Value: "disagreed"}) {
		t.Fatalf("meta = %v", b.Items[0].Meta)
	}
	if len(b.Problems) != 1 {
		t.Fatalf("the unknown field must be a problem, got %v", b.Problems)
	}
}

func TestBatchMalformedDisplayFailsTheBatch(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":3,"summary":"s","display":{"status":"badge"}}]}`)); err == nil {
		t.Fatal("a malformed display must fail the batch")
	}
}

func TestBatchOverLimitMetaIsDroppedWithAProblem(t *testing.T) {
	t.Parallel()
	b, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":3,"summary":"s","meta":{"intent":"a\nb","kind":"issue"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items[0].Meta) != 1 || len(b.Problems) != 1 {
		t.Fatalf("meta %v problems %v", b.Items[0].Meta, b.Problems)
	}
}

func TestReviewDocDisplayAtThreeLevels(t *testing.T) {
	t.Parallel()
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x",
	  "display":{"kind":{"show":["badge"]}},
	  "files":[{"path":"a.go","display":{"kind":{"show":["count"]}},
	    "annotations":[{"newRange":[1,1],"summary":"s","meta":{"kind":"issue"},"display":{"intent":{"show":["line"]}}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Display["kind"].Show[0] != "badge" || doc.Files[0].Display["kind"].Show[0] != "count" ||
		doc.Files[0].Notes[0].Display["intent"].Show[0] != "line" {
		t.Fatalf("displays = %+v / %+v / %+v", doc.Display, doc.Files[0].Display, doc.Files[0].Notes[0].Display)
	}
}

func TestReviewDocMalformedDisplayIsAProblemNotALostDoc(t *testing.T) {
	t.Parallel()
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x","display":[1],
	  "files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"s","display":{"kind":{"show":"badge"}}}]}]}`))
	if err != nil {
		t.Fatalf("a malformed display must not cost the document its remarks: %v", err)
	}
	if doc.Display != nil || doc.Files[0].Notes[0].Display != nil || len(doc.Problems) != 2 {
		t.Fatalf("display %v / %v problems %v", doc.Display, doc.Files[0].Notes[0].Display, doc.Problems)
	}
}

func TestCanonicalKeepsDisplayAtEveryLevel(t *testing.T) {
	t.Parallel()
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x","display":{"kind":{"show":["badge"]}},
	  "files":[{"path":"a.go","display":{"kind":{"show":["count"]}},
	    "annotations":[{"newRange":[1,1],"summary":"s","display":{"intent":{"show":["line"]}}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseReview(doc.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Display, doc.Display) || !reflect.DeepEqual(again.Files[0].Display, doc.Files[0].Display) ||
		!reflect.DeepEqual(again.Files[0].Notes[0].Display, doc.Files[0].Notes[0].Display) {
		t.Fatalf("canonical lost a display:\n%s", doc.Canonical())
	}
}
