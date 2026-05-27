package emoji

import (
	"strings"
	"unicode/utf8"
)

// TokenKind discriminates between text and emoji tokens in the
// output of ResolveEmojiToTokens.
type TokenKind int

const (
	// TokenText is a literal run of non-emoji text.
	TokenText TokenKind = iota
	// TokenEmoji is one emoji — either a resolved :shortcode: match
	// or a Unicode grapheme cluster carrying emoji presentation.
	TokenEmoji
)

// Token is one chunk emitted by ResolveEmojiToTokens.
//
// For TokenText: Text holds the literal substring, URL is empty.
// For TokenEmoji: Text holds the plain-text representation used for
// yank, clipboard copy, in-buffer search, and cold-cache fallback
// (":name:" form for shortcodes/customs, the Unicode glyph for
// emoji that appeared as raw codepoints in the source). URL holds
// the Slack CDN URL for the image.
type Token struct {
	Kind TokenKind
	Text string
	URL  string
}

// ResolveEmojiToTokens scans text and emits a token stream. Every
// emoji that can be resolved to a Slack CDN URL becomes a
// TokenEmoji; everything else is folded into TokenText runs.
//
// Two detection paths run in a single linear pass:
//
//  1. ":shortcode:" matches (e.g., ":thumbsup:", ":party_parrot:").
//     Resolved via URLForShortcode, which consults the workspace
//     customs map first (with alias chains) and falls through to
//     the kyokomi builtin codemap.
//
//  2. Unicode emoji grapheme clusters embedded in the source text
//     (e.g., a literal "👍" in a Slack message body). Detected by
//     matching the cluster against the set of all kyokomi-known
//     emoji clusters; URL is built directly from the cluster's
//     codepoints.
//
// Unresolvable shortcodes (unknown name, alias cycle, etc.) pass
// through verbatim as TokenText so the user still sees the
// readable ":name:" form. Same for Unicode codepoints that aren't
// in the known-emoji set — they remain in the text run unchanged.
//
// Adjacent emoji produce adjacent TokenEmoji values with no
// intervening TokenText.
//
// customs may be nil; nil is treated as an empty workspace
// (kyokomi-only resolution).
func ResolveEmojiToTokens(text string, customs map[string]string) []Token {
	if text == "" {
		return nil
	}
	var tokens []Token
	var textBuf strings.Builder
	flushText := func() {
		if textBuf.Len() > 0 {
			tokens = append(tokens, Token{Kind: TokenText, Text: textBuf.String()})
			textBuf.Reset()
		}
	}

	// Linear byte-position walk. At each position we try (a) shortcode
	// match, (b) emoji-cluster match, then fall through to a single-rune
	// advance into the running text buffer.
	i := 0
	for i < len(text) {
		// (a) Shortcode pass — implemented in Task 3.5.
		// (b) Emoji-cluster pass — implemented in Task 3.7.

		// Default: consume one rune into the text buffer.
		r, sz := utf8.DecodeRuneInString(text[i:])
		textBuf.WriteRune(r)
		i += sz
	}
	flushText()
	return tokens
}
