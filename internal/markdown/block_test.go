package markdown

import (
	"fmt"
	"strings"
	"testing"
)

// kinds renders a block tree as a compact s-expression so a table test can
// pin a whole structure in one string.
func kinds(bs []Block) string {
	var out []string
	for _, b := range bs {
		switch b.Kind {
		case KindHeading:
			out = append(out, fmt.Sprintf("h%d", b.Level))
		case KindList:
			var items []string
			for _, it := range b.Items {
				tag := "item"
				if it.Task != "" {
					tag += "[" + it.Task + "]"
				}
				items = append(items, tag+"("+kinds(it.Blocks)+")")
			}
			name := "list"
			if b.Ordered {
				name = "olist"
			}
			out = append(out, name+"("+strings.Join(items, " ")+")")
		case KindQuote:
			out = append(out, "quote("+kinds(b.Blocks)+")")
		case KindCode:
			out = append(out, fmt.Sprintf("code:%s[%d]", b.Lang, len(b.Lines)))
		case KindTable:
			out = append(out, fmt.Sprintf("table[%dx%d]", len(b.Rows), len(b.Head)))
		default:
			out = append(out, b.Kind)
		}
	}
	return strings.Join(out, " ")
}

// flat is the concatenated text of an inline run (breaks become newlines).
func flat(in []Inline) string {
	var sb strings.Builder
	for _, n := range in {
		switch n.Kind {
		case InBreak:
			sb.WriteByte('\n')
		case InText, InCode, InRef:
			sb.WriteString(n.Text)
		case InImage:
			sb.WriteString(n.Text)
		default:
			sb.WriteString(flat(n.In))
		}
	}
	return sb.String()
}

func TestParseBlocks(t *testing.T) {
	t.Parallel()
	fence := "```"
	cases := []struct{ name, src, want string }{
		{"heading+para", "# T\n\ntext", "h1 p"},
		{"seven hashes", "####### x", "p"},
		{"hash no space", "#x", "p"},
		{"closing hashes", "## T ##", "h2"},
		{"nested list", "- a\n- b\n  - c\n\npara", "list(item(p) item(p list(item(p)))) p"},
		{"delimiter change", "1. a\n2) b", "olist(item(p)) olist(item(p))"},
		{"bullet change", "- a\n* b", "list(item(p)) list(item(p))"},
		{"tasks", "- [ ] a\n- [x] b\n- [X] c", "list(item[open](p) item[done](p) item[done](p))"},
		{"loose list", "- a\n\n- b", "list(item(p) item(p))"},
		{"list then para", "- a\n\npara", "list(item(p)) p"},
		{"item continuation", "- a\n  more", "list(item(p))"},
		{"quote nested lazy", "> q\n> > deep\nlazy", "quote(p quote(p))"},
		{"fence", fence + "go\nx := 1\n" + fence, "code:go[1]"},
		{"tilde fence", "~~~\na\nb\n~~~", "code:[2]"},
		{"long fence", "````\n" + fence + "\nx\n````", "code:[2]"},
		{"unclosed fence", fence + "\na\nb", "code:[2]"},
		{"fence in item", "- a\n  " + fence + "\n  x\n  " + fence + "\n- b", "list(item(p code:[1]) item(p))"},
		{"suggestion", fence + "suggestion\nx\n" + fence, "code:suggestion[1]"},
		{"fence ends para", "text\n" + fence + "\nx\n" + fence, "p code:[1]"},
		{"table", "a | b\n:-|-:\n1 | 2", "table[1x2]"},
		{"bordered table", "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |", "table[2x2]"},
		{"no delimiter row", "a | b\nc | d", "p"},
		{"table then para", "a|b\n-|-\n1|2\n\nafter", "table[1x2] p"},
		{"rules", "---\n\n***\n\n_ _ _", "hr hr hr"},
		{"two dashes", "--", "p"},
		{"indented is a paragraph", "    indented", "p"},
		{"html is text", "<script>alert(1)</script>", "p"},
		{"empty", "", ""},
		{"blank only", "  \n\t\n", ""},
	}
	for _, c := range cases {
		if got := kinds(Parse(c.src).Blocks); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseBlockDetails(t *testing.T) {
	t.Parallel()
	if b := Parse("3. a\n4. b").Blocks[0]; !b.Ordered || b.Start != 3 || len(b.Items) != 2 {
		t.Errorf("ordered start: %+v", b)
	}
	b := Parse("a | b | c\n:-|:-:|-:\n1 | 2\n1 | 2 | 3 | 4\nx \\| y | `p\\|q` | z").Blocks[0]
	if got := strings.Join(b.Align, ","); got != "left,center,right" {
		t.Errorf("align = %q", got)
	}
	if len(b.Rows) != 3 || len(b.Rows[0]) != 3 || len(b.Rows[1]) != 3 {
		t.Fatalf("rows not normalised to the head width: %+v", b.Rows)
	}
	if got := flat(b.Rows[0][2]); got != "" {
		t.Errorf("a short row pads with empty cells, got %q", got)
	}
	if got := flat(b.Rows[2][0]); got != "x | y" {
		t.Errorf("escaped pipe: %q", got)
	}
	if got := flat(b.Rows[2][1]); got != "p|q" {
		t.Errorf("pipe in a code span: %q", got)
	}
	if got := flat(Parse("<script>alert(1)</script>").Blocks[0].Inline); got != "<script>alert(1)</script>" {
		t.Errorf("html must stay literal text, got %q", got)
	}
	if got := flat(Parse("a\r\nb").Blocks[0].Inline); got != "a\nb" {
		t.Errorf("crlf: %q", got)
	}
	code := Parse("  ```\n  x\n    y\n  ```").Blocks[0]
	if code.Lines[0].Text != "x" || code.Lines[1].Text != "  y" {
		t.Errorf("fence indent is stripped from its lines: %+v", code.Lines)
	}
	q := Parse("> q\n> > deep\nlazy").Blocks[0]
	if got := flat(q.Blocks[1].Blocks[0].Inline); got != "deep\nlazy" {
		t.Errorf("lazy continuation: %q", got)
	}
}

func TestParseBounds(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("> ", 12) + "bottom"
	d := Parse(deep)
	depth := 0
	for bs := d.Blocks; len(bs) > 0 && bs[0].Kind == KindQuote; bs = bs[0].Blocks {
		depth++
	}
	if depth != MaxDepth {
		t.Errorf("quote depth = %d, want the cap %d", depth, MaxDepth)
	}
	if !strings.Contains(allText(d), "bottom") {
		t.Error("text below the depth cap must survive as literal text")
	}

	big := strings.Repeat("- item with **bold**\n", MaxInput/20+100)
	bd := Parse(big)
	if len(bd.Blocks) == 0 || bd.Blocks[0].Kind != KindPara {
		t.Errorf("oversized input degrades to paragraphs, got %q…", kinds(bd.Blocks[:1]))
	}

	wide := strings.Repeat("a|", MaxTableCols+6) + "\n" + strings.Repeat("-|", MaxTableCols+6)
	if got := kinds(Parse(wide).Blocks); got != "p" {
		t.Errorf("over-wide table: %q", got)
	}
}

// allText is every text-bearing leaf of a doc, concatenated.
func allText(d Doc) string {
	var sb strings.Builder
	var walk func([]Block)
	walk = func(bs []Block) {
		for _, b := range bs {
			sb.WriteString(flat(b.Inline))
			for _, c := range b.Head {
				sb.WriteString(flat(c))
			}
			for _, r := range b.Rows {
				for _, c := range r {
					sb.WriteString(flat(c))
				}
			}
			for _, l := range b.Lines {
				sb.WriteString(l.Text)
			}
			for _, it := range b.Items {
				walk(it.Blocks)
			}
			walk(b.Blocks)
		}
	}
	walk(d.Blocks)
	return sb.String()
}
