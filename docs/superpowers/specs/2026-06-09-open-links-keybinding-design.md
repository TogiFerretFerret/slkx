# Open Links Keybinding + In-App Slack Permalink Navigation

**Date:** 2026-06-09
**Status:** Approved
**Related:** Issue #62 (keybinding to open links in a message)

## Problem

Links in messages render as OSC 8 terminal hyperlinks only. Following a link
requires the mouse and terminal-specific modifier-click behavior. There is no
keyboard-driven way to open a link, and no way to follow a Slack permalink
(e.g. `https://truelist-workspace.slack.com/archives/C054JFCBN69/p1779284733270139`)
to that conversation *inside* slk — it always opens the browser.

## Goals

1. A keybinding (`o`) opens the link in the selected message (issue #62).
2. Messages with multiple links show a picker modal.
3. All link opens flow through a single routing point. Slack archive
   permalinks for the active workspace navigate in-app (channel, message
   selection, thread); everything else opens in the default browser.

## Non-Goals (v1)

- Links from attachments, Block Kit elements, or image URLs (message text only).
- Cross-workspace navigation (foreign-workspace permalinks open in browser).
- Fetching history windows around an old message ts ("full jump-to-message").
- Mouse click hit-testing for links in message text.

## Design

### 1. Link extraction

Factor the existing regexes (`linkWithLabelRe`, `linkBareRe` in
`internal/ui/messages/render.go:36-37`) into a shared helper:

```go
// internal/ui/messages
type Link struct{ URL, Label string }
func ExtractLinks(text string) []Link
```

Returns links in order of appearance, deduplicated by URL. Only `https?://`
and `mailto:` URLs (what the regexes already match).

### 2. Keybinding and dispatch

- New `OpenLink` binding in `KeyMap` (`internal/ui/keys.go`): key `o`,
  help text "open link". `o` is currently unbound in normal mode
  (`O`/`v` are image preview).
- Handled in normal mode for both the messages pane and the thread panel,
  acting on the selected message:
  - **0 links:** status-bar toast "No links in message".
  - **1 link:** dispatch `OpenLinkMsg{URL}` directly (no modal).
  - **2+ links:** open the link picker modal.

### 3. Link picker modal

New package `internal/ui/linkpicker`, modeled on existing modal components
(e.g. `reactionpicker`). Behavior:

- Lists each link: label (if different from URL) plus URL, truncated to fit.
- Links that will resolve in-app (per the permalink parser + active workspace
  domain) get an "in slk" annotation.
- Keys: `j`/`k` (and arrows) move, `enter` dispatches `OpenLinkMsg{URL}` and
  closes, `esc` closes.

### 4. Permalink parser

New pure package `internal/slackurl`:

```go
type Permalink struct {
    Subdomain string         // "truelist-workspace"
    ChannelID ids.ChannelID  // "C054JFCBN69"
    MessageTS ids.MessageTS  // "1779284733.270139"
    ThreadTS  ids.ThreadTS   // optional, from thread_ts= query param
}
func Parse(rawURL string) (Permalink, bool)
```

Recognizes `https://<subdomain>.slack.com/archives/<CHANNEL>/p<digits>` with
optional `thread_ts=` and `cid=` query params (`cid` is accepted but
ignored; the path channel ID wins). The `p`-value maps to a Slack
ts by inserting a dot before the last 6 digits
(`p1779284733270139` → `1779284733.270139`). Anything else returns `ok=false`.

### 5. OpenLinkMsg routing (the single place)

New `OpenLinkMsg{URL string}` in `internal/ui/msgs.go`, handled in a new
`internal/ui/reducer_links.go`:

1. `slackurl.Parse(URL)`. If not a permalink, or `Subdomain` does not match
   the active workspace's domain, or `ChannelService.Lookup(ChannelID)` fails
   (unknown/private channel) → open in browser via `openURLCmd`.
2. Otherwise:
   - If the channel is not active: dispatch
     `ChannelSelectedMsg{ID, Name, Type}` and record a pending link
     navigation `{ChannelID, MessageTS, ThreadTS}` in app state, completed
     when that channel's messages finish loading. If the channel is already
     active, complete immediately.
   - Completion:
     - `ThreadTS` set → open the thread panel for `ThreadTS` and fetch
       replies, modeled on `openSelectedThreadCmd`
       (`internal/ui/app.go:1442-1495`), which does not require the parent
       message to be in the pane buffer.
     - else → `SelectByTS(MessageTS)` (new method on the messages model);
       if the ts is not in loaded history, stay at the newest page and toast
       "message is older than loaded history".
   - The pending navigation is cleared on completion, and discarded if the
     user navigates elsewhere before the load completes.

`openURLCmd` is a new cross-platform browser-open command
(`xdg-open`/`open`/`rundll32`), generalizing the existing
`openInSystemViewerCmd` pattern (`internal/ui/app.go:1923-1942`).

### 6. Workspace domain

`cache.Workspace.Domain` exists but production passes `""`
(`cmd/slk/main.go:1493`). Populate it from the `auth.test` response URL
(available at `internal/slack/client.go:165-171`) so the active workspace's
subdomain is known to the UI for host matching. Matching is on subdomain
only: `<domain>.slack.com`.

### 7. Error handling

Every failure path degrades to browser-open or a toast — an `o` press is
never silently dead:

| Failure | Behavior |
|---|---|
| No links in message | Toast |
| Not a Slack permalink | Browser |
| Foreign workspace subdomain | Browser |
| Channel lookup fails | Browser |
| Workspace domain unknown (empty) | Browser |
| Target ts not in loaded history | Channel opens at newest + toast |
| Browser open fails | Toast with error |

The modal closes before any navigation or browser open.

## Testing

- `internal/slackurl`: table-driven tests — valid permalink, `thread_ts`,
  `cid`, malformed `p`-value, non-archive slack.com URLs, non-slack URLs,
  `http` vs `https`.
- `ExtractLinks`: labeled, bare, mixed, duplicate URLs, no links.
- Reducer tests in the style of `internal/ui/app_test.go` (its fixture at
  line 504 already uses this exact URL shape):
  - permalink for active workspace → `ChannelSelectedMsg` + pending nav set
  - already-active channel → immediate `SelectByTS`
  - `thread_ts` permalink → thread panel opened with correct thread ts
  - foreign domain / non-permalink → browser command produced
  - 0 / 1 / multiple links on `o` → toast / direct dispatch / modal
- `linkpicker`: model key-handling tests (navigation, enter, esc).
