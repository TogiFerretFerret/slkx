# Emoji as Images — Implementation Plan (Overview)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On kitty-class terminals, render every emoji (standard Unicode + Slack workspace custom) as a PNG from Slack's CDN via the kitty graphics protocol, retiring the glyph-rendering + width-probe + `:name:`-fallback stack. Non-kitty terminals keep their current behavior unchanged.

**Architecture:** A new emoji token stream identifies `:shortcode:` matches and Unicode emoji grapheme clusters in message text and returns a sequence of `TextRun` and `EmojiToken` values. Each `EmojiToken` carries an image URL (built from codepoints or workspace customs) and a plain-text representation (for yank, copy, search). UI surfaces render `EmojiToken`s through a new `internal/emoji/place.go` helper that wraps the existing image fetcher: warm cache returns a 2-cell kitty unicode-placeholder string; cold cache returns a 2-space reservation and triggers an async fetch that re-renders on completion. Width math reports a fixed 2 cells per emoji on the image path, killing per-terminal alignment drift. The existing `internal/image` fetcher/cache/registry/kitty renderer handles transmission, dedup, and tmux passthrough with zero changes.

**Tech Stack:** Go 1.22+, `github.com/rivo/uniseg` (grapheme clustering, already a dep), `github.com/kyokomi/emoji/v2` (codemap, already a dep), `golang.org/x/sync/singleflight` (existing fetcher), the existing `internal/image` package (kitty + registry + cache).

**Spec:** `docs/superpowers/specs/2026-05-27-emoji-as-images-design.md`

---

## Phase Files

| Phase | File | Summary | Mergeable point |
|---|---|---|---|
| 1 | `01-config.md` | Add `Appearance.EmojiImages` and `Appearance.EmojiCells` config fields with defaults. No behavior change. | Pure config |
| 2 | `02-url-building.md` | New `internal/emoji/url.go` with `BuildStandardEmojiURL` and `BuildCustomEmojiURL`. Fixture-driven tests against captured live Slack URLs. | Pure library |
| 3 | `03-token-model.md` | New `internal/emoji/tokens.go` with `Token` types and `ResolveEmojiToTokens(text, customs)`. Detects both `:shortcode:` matches and Unicode pictographic clusters. | Pure library |
| 4 | `04-width-and-probe.md` | Wire `EmojiImages` + kitty detection into `internal/emoji/width.go` (force 2 cells for image-renderable clusters) and `internal/emoji/init.go` (skip probe when active). | Pure library, dark until consumed |
| 5 | `05-place-helper.md` | New `internal/emoji/place.go`: `Place(ctx, url, cells) (placement, flush, ok)` — the inline analog of `blockkit.fetchOrPlaceholder` for 2-cell emoji. New `EmojiImageReadyMsg` for re-render signaling. | Pure library |
| 6 | `06-messages-and-reactions.md` | First user-visible delivery. Wire token stream into `internal/ui/messages/render.go` (message body) and `internal/ui/messages/model.go` (reaction pill construction at line 1745+). | **Feature live on main pane** |
| 7 | `07-thread-pane.md` | Mirror Phase 6 changes in `internal/ui/thread/model.go`. | Feature live on thread pane |
| 8 | `08-picker.md` | Wire token rendering into `internal/ui/reactionpicker/model.go` for the picker grid. | Feature live in picker |
| 9 | `09-autocomplete.md` | Wire token rendering into the compose emoji autocomplete dropdown. | Feature live in autocomplete |
| 10 | `10-yank-and-search.md` | Verify yank/copy/search use `EmojiToken.plainText`, never kitty placeholder runes. Fix any regression found. | Behavior parity confirmed |
| 11 | `11-docs.md` | `wiki/Configuration.md`, `wiki/Terminal-Compatibility.md`, manual smoke checklist. | Shipped |

Each phase is **independently mergeable**. The feature is dark until Phase 6; richer through 7-9.

---

## File Structure (cumulative across all phases)

**New files:**
- `internal/emoji/url.go` — URL builders for standard + custom emoji
- `internal/emoji/url_test.go`
- `internal/emoji/tokens.go` — `ResolveEmojiToTokens` and Token types
- `internal/emoji/tokens_test.go`
- `internal/emoji/place.go` — inline placement helper (warm/cold path)
- `internal/emoji/place_test.go`
- `internal/emoji/testdata/slack_urls.json` — captured fixture of real Slack CDN URLs for representative emoji (VS16-stripping validation)

**Modified files:**
- `internal/config/config.go` — add `Appearance.EmojiImages string` (`"on"` default), `Appearance.EmojiCells int` (`2` default)
- `internal/config/config_test.go` — defaults + override tests
- `internal/emoji/width.go` — image-path branch in `Width()`
- `internal/emoji/width_test.go` — image-path coverage
- `internal/emoji/init.go` — skip probe on kitty + `EmojiImages=on`
- `internal/emoji/init_test.go` — skip-path coverage
- `internal/emoji/render.go` — leave `ResolveShortcodesInText` untouched (still used on fallback path); documented
- `internal/ui/messages/render.go:570` — switch from `ResolveShortcodesInText` to token-stream rendering on the image path
- `internal/ui/messages/model.go:1745-1797` — reaction pill construction
- `internal/ui/messages/model_test.go` — pill-with-image tests
- `internal/ui/thread/model.go:1657-1690, 1803` — mirror of messages-pane changes
- `internal/ui/reactionpicker/model.go` — grid row rendering
- `internal/ui/compose/...` — autocomplete dropdown row rendering (exact file determined in Phase 9 recon)
- `internal/ui/app.go` — pass kitty-detected + EmojiImages config to a new `emoji.PlaceContext` available to all UI surfaces
- `internal/ui/msgs.go` — new `EmojiImageReadyMsg` type
- `internal/ui/reducer_*.go` — handle `EmojiImageReadyMsg` (invalidate emoji caches in messages + thread + picker + autocomplete)
- `cmd/slk/main.go` — construct `emoji.PlaceContext` at startup
- `wiki/Configuration.md` — document the two new keys
- `wiki/Terminal-Compatibility.md` — escape-hatch section for CDN-blocked networks

---

## Sequencing Rationale

Bottom-up. Phases 1-5 land **pure library code** with no UI consumers — the feature is dark on `main` after each. This lets each phase land with full test coverage and zero risk to the running app.

Phase 6 is the first user-visible delivery: it wires the library through the main messages pane and reaction pills. After Phase 6, `emoji_images = "on"` on a kitty terminal produces image-rendered emoji in the most prominent UI surface. Phases 7-9 extend to thread pane, picker, and autocomplete in that order of user-visibility weight.

Phase 10 is a verification phase: the design requires that yank/copy/search use `plainText`, never the kitty placeholder runes. Phase 10 confirms this end-to-end and fixes any regression discovered. Phase 11 ships docs.

If only Phases 1-5 ship, no user-visible change. If Phase 6 ships, main pane and reactions are image-rendered. If 7-9 ship, every in-scope surface is covered. If 10-11 ship, the feature is documented and verified.

---

## Test Conventions

- Go test files live next to the code under test (existing repo convention).
- Tests use the standard library `testing` only — no testify.
- Image-fetcher integration tests use `httptest.NewServer` (matching existing `fetcher_test.go` pattern).
- Token model tests are pure-data table-driven tests.
- `internal/emoji/testdata/slack_urls.json` is committed; it's a small JSON file mapping `name → url` captured from a live Slack workspace's network panel.
- Each phase finishes with `go test ./...` passing.

---

## Commit Convention

Each task ends with a commit. Commits use Conventional Commits prefixes already in repo history: `feat:`, `refactor:`, `test:`, `docs:`, `chore:`.

Suggested scopes for this work: `emoji`, `image`, `messages`, `thread`, `picker`, `compose`, `config`, `docs`.

---

## Open Items Carried From Spec

These are noted in the spec as "resolved during implementation":

1. **VS16-stripping rules** — validated against `internal/emoji/testdata/slack_urls.json`. Phase 2 captures the fixture; the URL builder is implemented to match it byte-for-byte.
2. **Density perf** — measured during Phase 6 manual smoke on a heavy-reaction channel. If transmission throughput is a bottleneck, the existing fetcher's `prerender` machinery already off-loads encoding; only transmit scheduling would need adjustment.
3. **Picker prefetch** — left lazy in Phase 8 per the spec's cold-cache UX choice. If open-to-render latency is bad in practice, a follow-up adds eager prefetch on picker-open.

---

Continue to `01-config.md`.
