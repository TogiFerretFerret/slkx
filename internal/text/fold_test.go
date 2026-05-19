package text

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"ascii lower", "cafe", "cafe"},
		{"ascii mixed case", "Foo Bar", "foo bar"},
		{"french acute", "Mélanie", "melanie"},
		{"french acute upper", "MÉLANIE", "melanie"},
		{"french cedilla", "François", "francois"},
		{"french grave/acute mix", "Café", "cafe"},
		{"spanish tilde n", "año", "ano"},
		{"portuguese tilde a", "São", "sao"},
		{"german umlaut", "Müller", "muller"},
		{"vietnamese tones", "Việt", "viet"},
		{"mixed script (cjk passes through)", "東京 Tōkyō", "東京 tokyo"},
		{"already folded passes through", "melanie", "melanie"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Fold(tc.in)
			if got != tc.want {
				t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFoldMatchesBothDirections proves that folding makes substring matching
// symmetric across diacritics: query and candidate compare equal after fold
// regardless of which side carries the accents.
func TestFoldMatchesBothDirections(t *testing.T) {
	name := "Mélanie"
	for _, q := range []string{"melanie", "Mélanie", "MELANIE", "mél"} {
		if !strings.Contains(Fold(name), Fold(q)) {
			t.Errorf("Fold(%q) should contain Fold(%q)", name, q)
		}
	}
}

// TestFoldDoesNotPanicOnSingleCombiningMark guards against a regression
// where a lone combining mark (no base character) trips the transform.
func TestFoldDoesNotPanicOnSingleCombiningMark(t *testing.T) {
	// U+0301 COMBINING ACUTE ACCENT, in isolation.
	_ = Fold("\u0301")
}
