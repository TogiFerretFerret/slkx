// Package text provides string utilities for search and matching.
package text

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Fold returns s with diacritics removed and lowercased, suitable for
// case- and accent-insensitive substring matching.
//
//	Fold("Mélanie")  == "melanie"
//	Fold("François") == "francois"
//	Fold("Café")     == "cafe"
//
// Characters with no decomposition (CJK, emoji, plain ASCII) pass through
// unchanged apart from lowercasing. Fold uses NFD (canonical) decomposition,
// not NFKD — ligatures and the German Eszett are NOT expanded, so
// Fold("ß") == "ß" and Fold("ﬃ") == "ﬃ". This is deliberate; NFKD changes
// far more than diacritics.
//
// If the transform pipeline ever returns an error (it should not for
// in-memory strings with this chain), Fold falls back to strings.ToLower(s)
// so matching degrades gracefully instead of panicking.
func Fold(s string) string {
	// Build the chain per call: transform.Chain returns a *chain with
	// internal buffers and cursor state that transform.String mutates,
	// so it is NOT safe to share across goroutines. Per-call allocation
	// is fine at this volume (a TUI re-filters O(items) per keystroke).
	chain := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	out, _, err := transform.String(chain, s)
	if err != nil {
		return strings.ToLower(s)
	}
	return strings.ToLower(out)
}
