package template

import "testing"

func TestModelToken(t *testing.T) {
	t.Parallel()
	got, err := resolveCommandFor(`tool --model <model> -p x`, nil, CmdCtx{Model: "son net"}, "linux")
	if err != nil || got != `tool --model 'son net' -p x` {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = resolveCommandFor(`tool --model=<model>`, nil, CmdCtx{Model: "o'3"}, "linux")
	if err != nil || got != `tool --model='o'\''3'` {
		t.Fatalf("quote: got %q, %v", got, err)
	}
	got, err = resolveCommandFor(`tool <model> -p x`, nil, CmdCtx{}, "linux")
	if err != nil || got != `tool  -p x` {
		t.Fatalf("empty model: got %q, %v", got, err)
	}
	// <model:FLAG> puts the flag only when there is a model.
	got, err = resolveCommandFor(`tool <model:--model> -p x`, nil, CmdCtx{Model: "opus"}, "linux")
	if err != nil || got != `tool --model 'opus' -p x` {
		t.Fatalf("flag form: got %q, %v", got, err)
	}
	got, err = resolveCommandFor(`tool <model:--model> -p x`, nil, CmdCtx{}, "linux")
	if err != nil || got != `tool  -p x` {
		t.Fatalf("flag form, no model: got %q, %v", got, err)
	}
	if err := ValidateCommandTokens(`tool <model>`, false); err != nil {
		t.Fatalf("<model> must be a known token: %v", err)
	}
	if !HasModelSlot(`a <model> b`) || HasModelSlot(`a <prompt> b`) {
		t.Fatal("HasModelSlot")
	}
	if QuoteArg("x y") == "x y" {
		t.Fatal("QuoteArg must quote")
	}
}
