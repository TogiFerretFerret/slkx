package emoji

import (
	"fmt"
	"strings"
	"sync"

	emojilib "github.com/kyokomi/emoji/v2"
)

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

// CDNBaseURL is the prefix Slack uses for its standard-emoji asset
// images. The "16.0" version segment changes when Slack ships a new
// emoji generation; if a future asset reorganization breaks our URLs,
// updating this constant is the single edit needed.
//
// "google-small" is Slack's Google-style emoji set at the smallest
// pre-rendered size (~16x16px). Workspace admins on Slack web can
// pick between Apple / Google / Twitter / Slack-classic; v1 of this
// renderer hardcodes the google set, matching the default for most
// workspaces. Per-workspace style detection is a follow-up.
const CDNBaseURL = "https://a.slack-edge.com/production-standard-emoji-assets/16.0/google-small/"

// vs16 is U+FE0F, the variation selector that requests emoji
// presentation for a base codepoint. Slack's CDN PRESERVES VS16 in
// URL paths (e.g., :heart: at U+2764 U+FE0F serves as
// "2764-fe0f.png", not "2764.png"). The earlier design doc
// hypothesized VS16 was stripped; empirical fetching against the CDN
// (a 403 for "2764.png" vs a 200 for "2764-fe0f.png") disproved
// that. ZWJ (U+200D) sequences and regional-indicator flag pairs
// are also preserved verbatim.
//
// The constant is retained because tokens.go uses it for scanning
// raw emoji-presentation sequences in message text.
const vs16 = '\uFE0F'

// BuildStandardEmojiURL returns the Slack CDN URL for a standard
// (kyokomi-known or Unicode-property-detected) emoji's codepoint
// sequence. Codepoints are lowercase-hex, dash-joined; ALL
// codepoints are preserved (including U+FE0F / VS16), matching
// Slack's CDN naming.
//
// Returns "" if codepoints is empty.
func BuildStandardEmojiURL(codepoints []rune) string {
	if len(codepoints) == 0 {
		return ""
	}
	parts := make([]string, 0, len(codepoints))
	for _, r := range codepoints {
		parts = append(parts, fmt.Sprintf("%x", r))
	}
	return CDNBaseURL + strings.Join(parts, "-") + ".png"
}

// CodepointsForShortcode resolves a Slack-style shortcode name (no
// colons) to its Unicode codepoint sequence using the kyokomi
// codemap. Returns (codepoints, true) on hit, (nil, false) on miss.
//
// Shortcodes that aren't in the kyokomi codemap (Slack-specific
// names, workspace customs) are not resolved here — call
// BuildCustomEmojiURL with the workspace customs map for those.
//
// VS16 and ZWJ are returned verbatim and preserved through URL
// construction (see BuildStandardEmojiURL).
func CodepointsForShortcode(name string) ([]rune, bool) {
	ensureKyokomiCodeMap()
	key := ":" + name + ":"
	u, ok := kyokomiCodeMap[key]
	if !ok {
		return nil, false
	}
	return []rune(strings.TrimRight(u, " ")), true
}

// customEmojiAliasPrefix marks a Slack custom-emoji alias entry
// (e.g., "alias:thumbsup"). Mirrors aliasPrefix in entries.go; kept
// separate so the URL-building code is self-contained at a glance.
//
// (Note: maxAliasHops is already declared in entries.go with the
// same value/semantic and is reused here rather than re-declared.)
const customEmojiAliasPrefix = "alias:"

// BuildCustomEmojiURL resolves a shortcode name to a URL using the
// workspace's customs map first, then (if the customs map has an
// "alias:<target>" entry) the kyokomi codemap for the target.
//
// Returns (url, true) on hit. Returns ("", false) when:
//   - name is not in customs and is not a kyokomi builtin reachable
//     through an alias entry
//   - the alias chain cycles or exceeds maxAliasHops
//
// Direct kyokomi-known names (no customs entry) are intentionally
// NOT resolved here — call CodepointsForShortcode +
// BuildStandardEmojiURL for those. This separation keeps each
// function single-purpose and lets callers fall back to glyph
// rendering at the right granularity when one path fails.
func BuildCustomEmojiURL(name string, customs map[string]string) (string, bool) {
	visited := make(map[string]struct{}, maxAliasHops)
	current := name
	for hop := 0; hop < maxAliasHops; hop++ {
		if _, seen := visited[current]; seen {
			return "", false // cycle
		}
		visited[current] = struct{}{}

		entry, ok := customs[current]
		if !ok {
			return "", false
		}
		if !strings.HasPrefix(entry, customEmojiAliasPrefix) {
			// Direct custom URL.
			return entry, true
		}
		target := strings.TrimPrefix(entry, customEmojiAliasPrefix)

		// If the alias target is a kyokomi builtin, resolve it now.
		// If it's another custom name, loop and follow.
		if cps, ok := CodepointsForShortcode(target); ok {
			return BuildStandardEmojiURL(cps), true
		}
		current = target
	}
	return "", false // exceeded max hops
}

// URLForShortcode resolves a Slack-style shortcode name (no colons)
// to a Slack CDN URL using the workspace customs map first
// (including alias resolution) and then the kyokomi codemap.
// Returns (url, true) on hit, ("", false) when both lookups miss
// or when an alias chain cycles.
//
// Workspace customs win over kyokomi for the same name — this
// matches the precedence already used in
// internal/emoji/entries.go:resolveCustomDisplay and the picker
// preview behavior.
func URLForShortcode(name string, customs map[string]string) (string, bool) {
	if u, ok := BuildCustomEmojiURL(name, customs); ok {
		return u, true
	}
	if cps, ok := CodepointsForShortcode(name); ok {
		return BuildStandardEmojiURL(cps), true
	}
	return "", false
}
