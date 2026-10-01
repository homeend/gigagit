package domain

import "testing"

func TestCheckoutKeyAgreesWithSameCheckout(t *testing.T) {
	t.Parallel()
	for _, p := range [][2]string{{"/a/b", "/a/b/"}, {"/a/b", "/a/./b"}, {"/a/b", "/a/c"}, {"/A/b", "/a/b"}} {
		if got, want := CheckoutKey(p[0]) == CheckoutKey(p[1]), SameCheckout(p[0], p[1]); got != want {
			t.Errorf("%q vs %q: keys equal=%v, SameCheckout=%v", p[0], p[1], got, want)
		}
	}
}
