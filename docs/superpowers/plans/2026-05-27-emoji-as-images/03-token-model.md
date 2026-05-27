# Phase 3: Token Model

> Index: `00-overview.md`. Previous: `02-url-building.md`. Next: `04-width-and-probe.md`.

**Goal:** `ResolveEmojiToTokens(text, customs) []Token` — a pure function that converts a Slack message body (or any plain text) into a stream of `TextRun` and `EmojiToken` values. Detects both `:shortcode:` matches and Unicode emoji grapheme clusters in a single linear pass.

The resulting token stream is what UI surfaces walk to build rendered output. Each emoji token carries both its image URL (for the kitty placement) and a plain-text representation (for yank, copy, search, and cold-cache fallback).

**Files:**
- Create: `internal/emoji/tokens.go`
- Create: `internal/emoji/tokens_test.go`

---

### Task 3.1: Token type and `Kind` constants

**Files:**
- Create: `internal/emoji/tokens.go`

- [ ] **Step 1: Write the skeleton with types**

Create `internal/emoji/tokens.go`:

```go
package emoji

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
	// Implementation lives below; this stub keeps the file
	// compilable while tests are written incrementally.
	return []Token{{Kind: TokenText, Text: text}}
}
```

- [ ] **Step 2: Build to confirm the skeleton compiles**

Run: `go build ./internal/emoji/`
Expected: builds clean.

- [ ] **Step 3: Commit**

```bash
git add internal/emoji/tokens.go
git commit -m "feat(emoji): Token types and ResolveEmojiToTokens skeleton"
```

---

### Task 3.2: Failing test — trivial inputs

**Files:**
- Create: `internal/emoji/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/tokens_test.go`:

```go
package emoji

import (
	"reflect"
	"testing"
)

// text builds a TokenText for table-driven test brevity.
func text(s string) Token { return Token{Kind: TokenText, Text: s} }

// emoji builds a TokenEmoji for table-driven test brevity.
func emoji(plain, url string) Token { return Token{Kind: TokenEmoji, Text: plain, URL: url} }

func TestResolveEmojiToTokens_Trivial(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{"empty", "", nil},
		{"ascii only", "hello world", []Token{text("hello world")}},
		{"only spaces", "   ", []Token{text("   ")}},
		{"newlines preserved", "line one\nline two", []Token{text("line one\nline two")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveEmojiToTokens(c.in, nil)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ResolveEmojiToTokens(%q) = %#v\n want %#v", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_Trivial -v`
Expected: FAIL on the "empty" subtest — the stub returns `[]Token{{TokenText, ""}}` instead of nil.

---

### Task 3.3: Implement the linear scanner (empty + ASCII passthrough)

**Files:**
- Modify: `internal/emoji/tokens.go`

- [ ] **Step 1: Replace the stub body**

Replace the body of `ResolveEmojiToTokens` in `internal/emoji/tokens.go` with:

```go
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
```

Add the imports at the top of the file (merge with the existing `package emoji` declaration):

```go
package emoji

import (
	"strings"
	"unicode/utf8"
)
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_Trivial -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/emoji/tokens.go internal/emoji/tokens_test.go
git commit -m "feat(emoji): linear scanner skeleton (ASCII passthrough)"
```

---

### Task 3.4: Failing test — shortcode replacement

**Files:**
- Modify: `internal/emoji/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/tokens_test.go`:

```go
func TestResolveEmojiToTokens_Shortcodes(t *testing.T) {
	thumbURL := CDNBaseURL + "1f44d.png"     // :thumbsup:
	heartURL := CDNBaseURL + "2764.png"      // :heart: (VS16 stripped)
	rocketURL := CDNBaseURL + "1f680.png"    // :rocket:
	customParrot := "https://emoji.slack-edge.com/T01/party_parrot/abc.gif"
	customs := map[string]string{
		"party_parrot": customParrot,
		"alias_for_rocket": "alias:rocket",
	}

	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{
			"shortcode at start",
			":thumbsup: nice",
			[]Token{emoji(":thumbsup:", thumbURL), text(" nice")},
		},
		{
			"shortcode at end",
			"nice :thumbsup:",
			[]Token{text("nice "), emoji(":thumbsup:", thumbURL)},
		},
		{
			"shortcode in middle",
			"a :heart: b",
			[]Token{text("a "), emoji(":heart:", heartURL), text(" b")},
		},
		{
			"two shortcodes with text between",
			":heart: and :rocket:",
			[]Token{emoji(":heart:", heartURL), text(" and "), emoji(":rocket:", rocketURL)},
		},
		{
			"adjacent shortcodes (no separator)",
			":heart::rocket:",
			[]Token{emoji(":heart:", heartURL), emoji(":rocket:", rocketURL)},
		},
		{
			"unknown shortcode passes through as text",
			":not_an_emoji_xyz: hello",
			[]Token{text(":not_an_emoji_xyz: hello")},
		},
		{
			"broken shortcode (missing closing colon)",
			":heart still text",
			[]Token{text(":heart still text")},
		},
		{
			"workspace custom",
			"hello :party_parrot:",
			[]Token{text("hello "), emoji(":party_parrot:", customParrot)},
		},
		{
			"alias resolves to builtin",
			"go :alias_for_rocket: go",
			[]Token{text("go "), emoji(":alias_for_rocket:", rocketURL), text(" go")},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveEmojiToTokens(c.in, customs)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ResolveEmojiToTokens(%q):\n got  %#v\n want %#v", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_Shortcodes -v`
Expected: every subcase FAILS — the scanner currently treats all input as text.

---

### Task 3.5: Implement the shortcode pass

**Files:**
- Modify: `internal/emoji/tokens.go`

- [ ] **Step 1: Add the shortcode-scanning helper**

Append to `internal/emoji/tokens.go`:

```go
// tryShortcodeAt attempts to read a ":name:" shortcode starting at
// text[i]. Returns (endByte, name, true) on success — endByte is the
// byte index immediately past the closing colon. Returns (0, "", false)
// if text[i] is not ':', if no closing colon is found before the next
// non-shortcode rune, or if the name is empty.
//
// The name character class matches shortcodeRe in render.go:
// [A-Za-z0-9_+-]. Mismatched chars terminate the scan with ok=false.
func tryShortcodeAt(text string, i int) (int, string, bool) {
	if i >= len(text) || text[i] != ':' {
		return 0, "", false
	}
	j := i + 1
	for j < len(text) {
		c := text[j]
		if c == ':' {
			break
		}
		if !isShortcodeChar(c) {
			return 0, "", false
		}
		j++
	}
	if j >= len(text) || text[j] != ':' {
		return 0, "", false
	}
	name := text[i+1 : j]
	if name == "" {
		return 0, "", false
	}
	return j + 1, name, true
}

func isShortcodeChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '+' || c == '-'
}
```

- [ ] **Step 2: Wire the shortcode pass into `ResolveEmojiToTokens`**

Replace the placeholder comment `// (a) Shortcode pass — implemented in Task 3.5.` inside `ResolveEmojiToTokens` with:

```go
		// (a) Shortcode pass.
		if text[i] == ':' {
			if end, name, ok := tryShortcodeAt(text, i); ok {
				if url, urlOK := URLForShortcode(name, customs); urlOK {
					flushText()
					tokens = append(tokens, Token{
						Kind: TokenEmoji,
						Text: ":" + name + ":",
						URL:  url,
					})
					i = end
					continue
				}
				// Known to be a syntactically valid shortcode but not
				// resolvable. Fall through to default rune-consume so
				// the literal ":name:" appears verbatim in the next
				// text run.
			}
		}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_Shortcodes -v`
Expected: every subcase PASS.

- [ ] **Step 4: Re-run the trivial tests to confirm no regression**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/tokens.go internal/emoji/tokens_test.go
git commit -m "feat(emoji): shortcode-to-image-token pass in ResolveEmojiToTokens"
```

---

### Task 3.6: Failing test — Unicode emoji clusters

**Files:**
- Modify: `internal/emoji/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/tokens_test.go`:

```go
func TestResolveEmojiToTokens_UnicodeClusters(t *testing.T) {
	thumbURL := CDNBaseURL + "1f44d.png"                  // 👍
	astronautURL := CDNBaseURL + "1f468-200d-1f680.png"   // 👨‍🚀 (ZWJ kept)
	heartURL := CDNBaseURL + "2764.png"                   // ❤️ (VS16 stripped)
	flagUSURL := CDNBaseURL + "1f1fa-1f1f8.png"           // 🇺🇸 (regional indicators)
	rainbowURL := CDNBaseURL + "1f3f3-200d-1f308.png"     // 🏳️‍🌈

	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{
			"single emoji",
			"\U0001F44D",
			[]Token{emoji("\U0001F44D", thumbURL)},
		},
		{
			"emoji with text",
			"nice \U0001F44D!",
			[]Token{text("nice "), emoji("\U0001F44D", thumbURL), text("!")},
		},
		{
			"ZWJ sequence stays one token",
			"\U0001F468\u200D\U0001F680",
			[]Token{emoji("\U0001F468\u200D\U0001F680", astronautURL)},
		},
		{
			"VS16 sequence",
			"\u2764\uFE0F",
			[]Token{emoji("\u2764\uFE0F", heartURL)},
		},
		{
			"regional indicator pair",
			"\U0001F1FA\U0001F1F8",
			[]Token{emoji("\U0001F1FA\U0001F1F8", flagUSURL)},
		},
		{
			"rainbow flag (ZWJ + VS16)",
			"\U0001F3F3\uFE0F\u200D\U0001F308",
			[]Token{emoji("\U0001F3F3\uFE0F\u200D\U0001F308", rainbowURL)},
		},
		{
			"two emoji adjacent",
			"\U0001F44D\u2764\uFE0F",
			[]Token{emoji("\U0001F44D", thumbURL), emoji("\u2764\uFE0F", heartURL)},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveEmojiToTokens(c.in, nil)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ResolveEmojiToTokens(%q):\n got  %#v\n want %#v", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_UnicodeClusters -v`
Expected: every subcase FAILS — the scanner doesn't detect emoji clusters yet.

---

### Task 3.7: Implement the Unicode emoji-cluster pass

**Files:**
- Modify: `internal/emoji/tokens.go`

- [ ] **Step 1: Add the cluster-lookup machinery**

Append to `internal/emoji/tokens.go`:

```go
// emojiClusterSet is the set of all grapheme clusters known to
// resolve to standard emoji. Populated from the kyokomi codemap
// values on first lookup. Both the canonical form and a
// VS16-stripped form are inserted so source text using either
// presentation triggers a match.
var (
	emojiClusterSetOnce sync.Once
	emojiClusterSet     map[string]struct{}
)

func initEmojiClusterSet() {
	set := make(map[string]struct{}, 4096)
	for _, u := range emojilibCodeMap() {
		// kyokomi appends a trailing space for Sprint-style use; strip it.
		canonical := strings.TrimRight(u, " ")
		if canonical == "" {
			continue
		}
		set[canonical] = struct{}{}

		// Also insert the VS16-stripped form for source text that
		// uses bare codepoints without the variation selector.
		stripped := stripVS16(canonical)
		if stripped != canonical && stripped != "" {
			set[stripped] = struct{}{}
		}
	}
	emojiClusterSet = set
}

func isKnownEmojiCluster(cluster string) bool {
	emojiClusterSetOnce.Do(initEmojiClusterSet)
	_, ok := emojiClusterSet[cluster]
	return ok
}

func stripVS16(s string) string {
	if !strings.ContainsRune(s, vs16) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == vs16 {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// emojilibCodeMap is a thin indirection so the kyokomi import can be
// shared with url.go without re-importing the package here.
func emojilibCodeMap() map[string]string {
	return kyokomiCodeMap
}
```

- [ ] **Step 2: Add the shared kyokomi codemap reference in `url.go`**

In `internal/emoji/url.go`, add a package-level variable below the existing imports:

```go
// kyokomiCodeMap is a one-time-snapshot of emojilib.CodeMap() for
// use by url.go and tokens.go without each call paying the cost of
// emojilib's map-building. Initialized at first use of any URL
// helper (CodepointsForShortcode triggers it).
var (
	kyokomiCodeMapOnce sync.Once
	kyokomiCodeMap     map[string]string
)

func ensureKyokomiCodeMap() {
	kyokomiCodeMapOnce.Do(func() {
		kyokomiCodeMap = emojilib.CodeMap()
	})
}
```

And add the `sync` import to `url.go`:

```go
import (
	"fmt"
	"strings"
	"sync"

	emojilib "github.com/kyokomi/emoji/v2"
)
```

Modify `CodepointsForShortcode` in `url.go` to use the cached map:

```go
func CodepointsForShortcode(name string) ([]rune, bool) {
	ensureKyokomiCodeMap()
	key := ":" + name + ":"
	u, ok := kyokomiCodeMap[key]
	if !ok {
		return nil, false
	}
	return []rune(strings.TrimRight(u, " ")), true
}
```

(The trailing-space trim matches what kyokomi does in `Sprint` and prevents the trailing space from leaking into the codepoint slice.)

- [ ] **Step 3: Re-run existing URL tests to confirm no regression**

Run: `go test ./internal/emoji/ -run TestCodepointsForShortcode -v`
Expected: PASS.

- [ ] **Step 4: Add the `sync` import to `tokens.go`**

Merge the import block at the top of `internal/emoji/tokens.go`:

```go
package emoji

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)
```

- [ ] **Step 5: Wire the cluster pass into `ResolveEmojiToTokens`**

Replace the placeholder comment `// (b) Emoji-cluster pass — implemented in Task 3.7.` inside `ResolveEmojiToTokens` with:

```go
		// (b) Emoji-cluster pass.
		if r, _ := utf8.DecodeRuneInString(text[i:]); r >= 0x80 {
			// Only attempt grapheme segmentation when the byte at i is
			// the start of a non-ASCII rune. Pure-ASCII fast path
			// avoids the uniseg.NewGraphemes call cost per byte.
			cluster, clusterLen, found := nextGraphemeCluster(text[i:])
			if found && isKnownEmojiCluster(cluster) {
				url := BuildStandardEmojiURL([]rune(cluster))
				if url != "" {
					flushText()
					tokens = append(tokens, Token{
						Kind: TokenEmoji,
						Text: cluster,
						URL:  url,
					})
					i += clusterLen
					continue
				}
			}
		}
```

Add the helper at the bottom of `tokens.go`:

```go
// nextGraphemeCluster returns the first grapheme cluster of s, its
// byte length, and true if a cluster was extracted. Returns
// ("", 0, false) on empty input.
func nextGraphemeCluster(s string) (string, int, bool) {
	if s == "" {
		return "", 0, false
	}
	gr := uniseg.NewGraphemes(s)
	if !gr.Next() {
		return "", 0, false
	}
	cluster := gr.Str()
	return cluster, len(cluster), true
}
```

- [ ] **Step 6: Run the cluster tests to verify they pass**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_UnicodeClusters -v`
Expected: every subcase PASS.

- [ ] **Step 7: Run all tokens tests to confirm no regression**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens -v`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/emoji/tokens.go internal/emoji/url.go internal/emoji/tokens_test.go
git commit -m "feat(emoji): detect Unicode emoji grapheme clusters in tokens pass"
```

---

### Task 3.8: Failing test — mixed and edge cases

**Files:**
- Modify: `internal/emoji/tokens_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/tokens_test.go`:

```go
func TestResolveEmojiToTokens_Mixed(t *testing.T) {
	thumbURL := CDNBaseURL + "1f44d.png"
	heartURL := CDNBaseURL + "2764.png"
	rocketURL := CDNBaseURL + "1f680.png"

	cases := []struct {
		name string
		in   string
		want []Token
	}{
		{
			"shortcode then unicode emoji",
			":thumbsup: yes \u2764\uFE0F",
			[]Token{
				emoji(":thumbsup:", thumbURL),
				text(" yes "),
				emoji("\u2764\uFE0F", heartURL),
			},
		},
		{
			"unicode emoji then shortcode",
			"\U0001F44D :heart:",
			[]Token{
				emoji("\U0001F44D", thumbURL),
				text(" "),
				emoji(":heart:", heartURL),
			},
		},
		{
			"colon inside non-emoji text (URL-like)",
			"see https://example.com path",
			[]Token{text("see https://example.com path")},
		},
		{
			"lone colon",
			"foo : bar",
			[]Token{text("foo : bar")},
		},
		{
			"empty colon pair",
			"foo :: bar",
			[]Token{text("foo :: bar")},
		},
		{
			"non-emoji unicode passes through",
			"caf\u00e9",
			[]Token{text("caf\u00e9")},
		},
		{
			"three emoji + text + shortcode at boundaries",
			"\U0001F44D\u2764\uFE0F\U0001F680 mid :thumbsup:",
			[]Token{
				emoji("\U0001F44D", thumbURL),
				emoji("\u2764\uFE0F", heartURL),
				emoji("\U0001F680", rocketURL),
				text(" mid "),
				emoji(":thumbsup:", thumbURL),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveEmojiToTokens(c.in, nil)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ResolveEmojiToTokens(%q):\n got  %#v\n want %#v", c.in, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestResolveEmojiToTokens_Mixed -v`
Expected: PASS — the implementation from Tasks 3.5 + 3.7 already handles these cases. If any subcase fails, the scanner has a bug; the fix lives in the existing implementation.

- [ ] **Step 3: Commit**

```bash
git add internal/emoji/tokens_test.go
git commit -m "test(emoji): cover mixed shortcode + unicode emoji + edge cases"
```

---

### Task 3.9: Final phase check

- [ ] **Step 1: Build the full project**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: no new failures.

- [ ] **Step 3: Verify the emoji package's tests all pass together**

Run: `go test ./internal/emoji/ -v`
Expected: all PASS — `TestResolveShortcodesInText` (existing), `TestShouldRenderUnicode` (existing), `TestBuildStandardEmojiURL`, `TestBuildCustomEmojiURL`, `TestCodepointsForShortcode_*`, `TestURLForShortcode`, `TestResolveEmojiToTokens_*`.

Phase 3 complete. The token model is built with full coverage of shortcode, Unicode cluster, and mixed inputs. No UI surface consumes it yet — the feature remains dark. Continue to `04-width-and-probe.md`.
