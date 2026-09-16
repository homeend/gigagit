package theme

import "testing"

func TestRolesCoversEveryEditableRole(t *testing.T) {
	t.Parallel()
	rs := Roles()
	if len(rs) != len(roleFields)+7+11 {
		t.Fatalf("Roles() = %d entries, want %d scalars + 7 lanes + 11 syntax", len(rs), len(roleFields))
	}
	if len(rs) != 55 {
		t.Fatalf("Roles() = %d, want 55", len(rs))
	}
	// Order: roleFields, then lanes, then syntax.
	for i, f := range roleFields {
		if rs[i].Key != f.key {
			t.Fatalf("Roles()[%d].Key = %q, want %q", i, rs[i].Key, f.key)
		}
		if rs[i].ListKey != "" {
			t.Fatalf("scalar role %q must have no ListKey", rs[i].Key)
		}
		if rs[i].Doc != f.doc {
			t.Fatalf("Roles()[%d].Doc = %q, want the roleFields doc", i, rs[i].Doc)
		}
	}
	n := len(roleFields)
	if rs[n].Key != "lanes[0]" || rs[n].ListKey != "lanes" || rs[n].Index != 0 {
		t.Fatalf("first lane row = %+v", rs[n])
	}
	if rs[n+6].Key != "lanes[6]" {
		t.Fatalf("last lane row = %+v", rs[n+6])
	}
	if rs[n+7].Key != "syntax[Plain]" || rs[n+7].ListKey != "syntax" {
		t.Fatalf("first syntax row = %+v", rs[n+7])
	}
	if rs[n+17].Key != "syntax[Attr]" {
		t.Fatalf("last syntax row = %+v", rs[n+17])
	}
	for _, r := range rs {
		if r.Doc == "" {
			t.Fatalf("role %q has no doc", r.Key)
		}
	}
}

func TestRolesIsBg(t *testing.T) {
	t.Parallel()
	want := map[string]bool{
		"bg": true, "fg": false, "diff_add_bg": true, "tooltip_bg": true,
		"field_cursor_bg": true, "field_cursor_fg": false, "lanes[0]": false,
		"syntax[Keyword]": false, "note_user": false,
	}
	got := map[string]bool{}
	for _, r := range Roles() {
		got[r.Key] = r.IsBg
	}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("IsBg[%s] = %v, want %v", k, got[k], w)
		}
	}
}

func roleByKey(t *testing.T, key string) RoleRef {
	t.Helper()
	for _, r := range Roles() {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no role %q", key)
	return RoleRef{}
}

func TestRoleGetSetRoundTrip(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"bg", "lanes[3]", "syntax[Comment]"} {
		r := roleByKey(t, key)
		if got := r.Get(Light); got == "" {
			t.Fatalf("%s: Light should define this role", key)
		}
		th := r.Set(Light, "#ABCDEF")
		if got := r.Get(th); got != "#ABCDEF" {
			t.Fatalf("%s: Get after Set = %q", key, got)
		}
		if r.Get(Light) == "#ABCDEF" {
			t.Fatalf("%s: Set must not mutate its argument", key)
		}
	}
}

func TestRoleOverrideGetSet(t *testing.T) {
	t.Parallel()
	r := roleByKey(t, "dim")
	var o Override
	if got := r.OverrideGet(o); got != "" {
		t.Fatalf("unset scalar = %q", got)
	}
	o = r.OverrideSet(o, Dark, "#112233")
	if got := r.OverrideGet(o); got != "#112233" {
		t.Fatalf("OverrideGet = %q", got)
	}
	o = r.OverrideSet(o, Dark, "")
	if got := r.OverrideGet(o); got != "" {
		t.Fatalf("clearing a scalar left %q", got)
	}
}

// A list entry written onto a nil list expands the list to the base theme's
// full palette first, so the resulting `lanes = [...]` line is complete and the
// other six lanes keep painting what they painted.
func TestRoleOverrideSetExpandsNilList(t *testing.T) {
	t.Parallel()
	r := roleByKey(t, "lanes[1]")
	o := r.OverrideSet(Override{}, Dark, "#ff5f00")
	if len(o.Lanes) != 7 {
		t.Fatalf("Lanes = %#v, want 7 entries", o.Lanes)
	}
	if o.Lanes[1] != "#ff5f00" {
		t.Fatalf("Lanes[1] = %q", o.Lanes[1])
	}
	for i, v := range o.Lanes {
		if i == 1 {
			continue
		}
		if v != Dark.Lanes[i] {
			t.Fatalf("Lanes[%d] = %q, want the base's %q", i, v, Dark.Lanes[i])
		}
	}
	if o.Syntax != nil {
		t.Fatal("touching lanes must not materialise the syntax list")
	}

	// The written override reproduces the intended theme through Overlay.
	th, bad := Overlay(Dark, o)
	if len(bad) > 0 {
		t.Fatalf("Overlay complained: %v", bad)
	}
	if th.Lanes[1] != "#ff5f00" || th.Lanes[0] != Dark.Lanes[0] {
		t.Fatalf("overlaid lanes = %#v", th.Lanes)
	}

	// Clearing the entry leaves "" in place: Overlay then keeps the base value.
	o = r.OverrideSet(o, Dark, "")
	if o.Lanes[1] != "" {
		t.Fatalf("cleared entry = %q", o.Lanes[1])
	}
	th, _ = Overlay(Dark, o)
	if th.Lanes[1] != Dark.Lanes[1] {
		t.Fatalf("cleared lane must fall back to the base, got %q", th.Lanes[1])
	}
}

// Every syntax role names its class; the ORDER is pinned against
// internal/syntax by TestThemeSyntaxRoleNamesMatchSyntaxClasses in
// internal/tui (theme is a DAG leaf and cannot import syntax itself).
func TestSyntaxRoleNames(t *testing.T) {
	t.Parallel()
	want := []string{"Plain", "Keyword", "Type", "Func", "Name", "String", "Number", "Comment", "Operator", "Punct", "Attr"}
	if len(syntaxClassNames) != len(want) {
		t.Fatalf("syntaxClassNames = %#v", syntaxClassNames)
	}
	for i, w := range want {
		if syntaxClassNames[i] != w {
			t.Fatalf("syntaxClassNames[%d] = %q, want %q", i, syntaxClassNames[i], w)
		}
		if got := SyntaxClassNames()[i]; got != w {
			t.Fatalf("SyntaxClassNames()[%d] = %q, want %q", i, got, w)
		}
	}
}
