# New Message Feature — Design Spec

**Date:** 2026-05-25
**Status:** Approved (pending spec review)

## Summary

Add a "new message" picker that lets the user start a DM (1 user) or group DM / MPIM (2–8 users) without first finding an existing conversation. Triggered by `Ctrl+N` from `ModeNormal`. Opens a centered modal with fuzzy user search, multi-select via Space, and submits via Enter. On submit, calls Slack's `conversations.open`, then switches to the resulting channel and focuses the compose box.

## Goals

- One keystroke from anywhere to start messaging anyone (or any group).
- Multi-select up to 8 recipients in a single pass.
- Match Slack desktop's behavior for edge cases (self excluded, existing conversation reused, MPIM cap = 8).
- Respect the in-progress SOLID refactor: new state in a dedicated sub-model, mode handler is thin.

## Non-Goals

- Self-DM ("Notes to self"). Excluded.
- Bots, deactivated users in the picker. Excluded.
- Renaming or converting an MPIM to a private channel. Out of scope.
- User-list refresh while the modal is open. Snapshot at open time.
- Golden-file rendering tests. The repo doesn't have that infra and we won't add it for this feature.

## User Flow

1. User in `ModeNormal` presses `Ctrl+N`.
2. Modal opens, centered, dimmed backdrop. Pill bar empty, query input focused. Result list shows top users by recency, then alpha.
3. User types — list filters by prefix > substring > subsequence rank, then recency, then alpha.
4. User presses `Space` (or `Tab`) on a highlighted user to add to pills. Repeat for additional users up to 8.
5. User presses `Enter`:
   - With pills empty → opens DM with currently-highlighted user.
   - With pills populated → opens DM (1 pill) or MPIM (2–8 pills).
6. Modal shows `Opening conversation...`; input becomes read-only.
7. On success, modal closes; app switches to the opened channel and enters `ModeInsert` with cursor in compose box.
8. On Esc during in-flight: modal closes immediately; if the API call completes later, the result is dropped.
9. On Esc before submit: modal closes, no-op.

## Architecture

```
+--------------------------------------+
| User presses Ctrl+N                  |
+-------------------+------------------+
                    |
                    v
+--------------------------------------+
| reducer: enter ModeNewMessage,       |
|   build user list snapshot,          |
|   construct newmessagepicker.Model   |
+-------------------+------------------+
                    |
                    v
+--------------------------------------+      +-----------------------+
| mode_new_message.go                  |      | newmessagepicker.Model|
|   routes key events to Model         |----->|   - query input       |
|   on Submit -> OpenConversationCmd   |      |   - filtered list     |
+-------------------+------------------+      |   - selected userIDs  |
                    |                         |   - HandleKey, View   |
                    v                         +-----------------------+
+--------------------------------------+
| ChannelService.OpenConversation(IDs) |
+-------------------+------------------+
                    |
                    v
+--------------------------------------+
| Client.OpenConversation              |
|   -> slack-go OpenConversationContext|
+-------------------+------------------+
                    |
                    v
+--------------------------------------+
| ConversationOpenedMsg{ChannelID,     |
|                       AlreadyOpen}   |
+-------------------+------------------+
                    |
                    v
+--------------------------------------+
| reducer: if not cancelled,           |
|   ensure cache record,               |
|   emit ChannelSelectedMsg +          |
|   EnterInsertModeMsg                 |
+--------------------------------------+
```

### Surfaces (new + changed)

1. **`internal/ui/newmessagepicker/` (new package)**
   - `model.go` — `Model` struct (query, filtered list, `selected map[string]struct{}`, viewport, cap=8).
   - `filter.go` — pure functions for filtering and ranking. Table-driven testable.
   - `model_test.go` — tests for filter, navigation, selection, submit semantics.
   - `View` returns the modal contents; `ViewOverlay(width, height)` returns the dimmed-overlay-placed version (mirrors `channelfinder.ViewOverlay`).
   - `Resize(width, height)` adjusts viewport.

2. **`internal/ui/mode_new_message.go` (new)** — handler routing key events to the picker; on submit, dispatches `ChannelService.OpenConversation`; on cancel, returns to `ModeNormal`. Mirrors `mode_channel_finder.go`.

3. **`internal/ui/mode.go` + `keys.go` (edit)**
   - Add `ModeNewMessage` constant and its `String()` case.
   - Bind `Ctrl+N` in `ModeNormal` to dispatch `EnterNewMessageMsg`.

4. **`internal/slack/client.go` (edit)**
   - Add `OpenConversationContext(ctx, *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)` to the `SlackAPI` interface.
   - Add `Client.OpenConversation(ctx, userIDs []string) (channelID string, alreadyOpen bool, err error)`.

5. **`internal/ui/services.go` + `cmd/slk/main.go` (edit)**
   - Add `OpenConversation(userIDs []string) tea.Cmd` to `ChannelService`.
   - Implementation in `main.go` where `ChannelService` is constructed, mirroring existing service methods.

6. **`internal/ui/msgs.go` (edit)**
   - `EnterNewMessageMsg struct{}` (dispatched by Ctrl+N).
   - `ConversationOpenedMsg{ChannelID string; AlreadyOpen bool; RequestID uint64}` (returned by the service cmd).
   - Errors reuse the existing error message type used by `SendMessage`.

7. **Reducer (`reducer_new_message.go` or extend an existing reducer)**
   - Handles `EnterNewMessageMsg`, `ConversationOpenedMsg`, error path, and the cancel/in-flight tracking.
   - On `ConversationOpenedMsg` with a matching un-cancelled `RequestID`: insert minimal channel record into cache (ID, Type, Members), emit `ChannelSelectedMsg`, emit `EnterInsertModeMsg`, close modal.

## UI Layout

```
+--------------------------------------------------------+
|  New message                                           |
+--------------------------------------------------------+
|  To: [Alice Chen] [Bob Singh] |                        |
+--------------------------------------------------------+
|  > Carla Diaz          @carla        recent            |
|    Dan Evans           @dan          recent            |
|    Eva Frank [ext]     @eva                            |
|    Frank Gomez         @frankg                         |
|    ...                                                 |
+--------------------------------------------------------+
|  space toggle  enter open  esc cancel    2 / 8         |
+--------------------------------------------------------+
```

- **Title row:** `New message`.
- **Pill bar + input:** Selected users render as inline lipgloss-styled pills (same token family as reaction pills in `internal/ui/messages/render.go`). Cursor sits after the pills; typed query renders to the right. Backspace at column 0 with pills present removes the last pill.
- **Result list:** Scrollable, highlight marker `>`. Each row: display name, `@handle`, right-aligned `recent` hint if the user appears in a DM/MPIM with a message in the last 30 days. External users tagged `[ext]` inline.
- **Footer:** Key hints on the left, `N / 8` selection counter on the right. Counter dims at `8 / 8` with `MPIM limit reached`.

### Key bindings (modal)

| Key | Action |
|---|---|
| any printable | append to query |
| `Backspace` | delete char; at col 0 with pills present, remove last pill |
| `Up` / `Down` / `Ctrl+P` / `Ctrl+N` | move highlight |
| `Space` | toggle highlighted user into selection (no-op at cap) |
| `Tab` | alias for Space |
| `Enter` | submit: empty pills → use highlighted user; otherwise use pills |
| `Esc` / `Ctrl+G` | cancel modal |

Note: `Ctrl+N` is the open keybind in `ModeNormal`; inside the modal it is repurposed for "next" (consistent with `channelfinder` repurposing `Ctrl+T`).

## Filtering & Ranking

Pure functions in `newmessagepicker/filter.go`:

1. **Source set:** cached workspace users where `Deleted == false && IsBot == false && ID != currentUserID`. The current user ID is read from `WorkspaceContext` at modal-open time and held on the `Model`. Externals included with `[ext]` tag.
2. **Match (when query non-empty):** case-insensitive on `display_name` and `@handle`. Tiers: prefix > substring > subsequence. Non-matchers dropped.
3. **Recency map:** built once at modal open by scanning cached `dm` and `group_dm` channels for last-message timestamps. A user's recency = max timestamp across DM/MPIM channels they are a member of. No API calls.
4. **Sort:** (a) match tier, (b) recency desc, (c) display name asc.
5. **Empty query:** Show top ~50 users by recency, then alpha. So `Ctrl+N` then Enter immediately opens the most recent DM target.

## Slack API Integration

`conversations.open` is dual-purpose:
- 1 user → IM (same as legacy `im.open`).
- 2–8 users → MPIM (same as legacy `mpim.open`).
- Idempotent: returns existing channel with `already_open=true` when present.

slack-go signature: `OpenConversationContext(ctx, &OpenConversationParameters{Users: []string, ReturnIM: true})` → `(*Channel, noOp bool, alreadyOpen bool, error)`.

### `Client.OpenConversation`

```go
// OpenConversation opens or returns an existing IM (1 user) or MPIM (2-8 users).
func (c *Client) OpenConversation(ctx context.Context, userIDs []string) (channelID string, alreadyOpen bool, err error) {
    if len(userIDs) == 0 {
        return "", false, fmt.Errorf("OpenConversation: at least one user ID required")
    }
    if len(userIDs) > 8 {
        return "", false, fmt.Errorf("OpenConversation: at most 8 user IDs allowed (got %d)", len(userIDs))
    }
    ch, _, alreadyOpen, err := c.api.OpenConversationContext(ctx, &slack.OpenConversationParameters{
        Users:    userIDs,
        ReturnIM: true,
    })
    if err != nil {
        return "", false, fmt.Errorf("OpenConversation: %w", err)
    }
    return ch.ID, alreadyOpen, nil
}
```

Belt-and-suspenders: also strip out the current user's ID before calling (the picker filters it out at source, but defending the API call adds zero cost).

### Cache hydration

When `alreadyOpen == false`, the new channel is not yet in the SQLite channel cache. Reducer inserts a minimal record before emitting `ChannelSelectedMsg`:

- `ID` = channel ID returned by `conversations.open`.
- `Type` = `dm` if `len(userIDs) == 1`, else `group_dm`.
- `Members` = the submitted user IDs plus the current user's ID.

Subsequent RTM events and the next user-list refresh fill in remaining fields. This avoids a slow round-trip refresh on the hot path.

## State Machine

```
ModeNormal --(Ctrl+N)--> ModeNewMessage
ModeNewMessage --(Esc / Ctrl+G)--> ModeNormal
ModeNewMessage --(Enter w/ valid selection)--> ModeNewMessage[in-flight]
ModeNewMessage[in-flight] --(Esc)--> ModeNormal (cancelled; late result dropped)
ModeNewMessage[in-flight] --(ConversationOpenedMsg, not cancelled)--> ModeInsert (in opened channel)
ModeNewMessage[in-flight] --(error)--> ModeNewMessage (modal stays open, error banner shown)
```

### Cancellation

Each submit gets a monotonic `RequestID uint64`. The reducer tracks the in-flight ID and a `cancelled bool`. On Esc during in-flight, modal closes and `cancelled=true`. When the service result arrives:

- If its `RequestID` matches AND not cancelled → proceed.
- Otherwise → drop silently.

This avoids the surprising "channel switches after the user backed out" behavior.

## Error Handling

| Failure | Surface | Recovery |
|---|---|---|
| Slack API error (network, auth, rate-limit) | Error banner in modal footer; modal stays open; selections preserved | User retries via Enter or Esc to cancel |
| User picks 8 then tries to add a 9th | Space is no-op; footer dims `8 / 8` and shows `MPIM limit reached` | Remove a pill (backspace) first |
| User cache empty at modal open | `Loading users...` placeholder; refreshes when cache populates | Type to filter once loaded |
| Query has no matches | List shows `No users match "<query>"`; Enter is no-op | Refine query |
| Self ID somehow selected | Filtered at source AND in `Client.OpenConversation`; API call would also reject | Defensive — should not occur |

## Edge Cases

- **Existing DM/MPIM:** `conversations.open` returns existing channel with `already_open=true`. We switch to it. No duplicate.
- **Channel not in cache:** Minimal record inserted by reducer before switch.
- **User list updates mid-modal:** Snapshot at open time; new users not visible until next open.
- **Theme:** Reuses existing lipgloss style tokens; no new theme entries needed.
- **Resize:** `Model.Resize(width, height)` recomputes viewport; centered overlay re-places via `lipgloss.Place`.

## Testing Strategy

Standard `go test`. No new frameworks.

### Layer 1 — Picker model (`internal/ui/newmessagepicker/model_test.go`)

Pure-function tests with no Slack API:

- Filter & rank: prefix > substring > subsequence tiers; recency tie-breaking; alpha tie-breaking; externals tagged; current user excluded; deactivated/bots excluded.
- Empty query → top-N by recency then alpha.
- No matches → empty list and placeholder.
- Navigation: Up/Down/Ctrl+P/Ctrl+N move highlight; clamp at boundaries (match `channelfinder`).
- Selection: Space toggles; Space on selected user removes; Tab aliases Space.
- MPIM cap: Space no-op at 8; counter state correct.
- Backspace at column 0 removes last pill when query is empty.
- Submit: empty pills + highlighted → `{userIDs: [highlighted.ID]}`; pills populated → `{userIDs: pills}`; no pills + no highlight → no-op.
- Esc returns cancel signal.
- Resize correctly recomputes viewport.

### Layer 2 — Slack client (`internal/slack/client_test.go`)

Add `openConversationContextFn` to `mockSlackAPI`:

- 1 user → calls API with that user; returns channel ID.
- 3 users → MPIM-shaped result.
- 0 users → returns error; no API call.
- 9 users → returns error; no API call.
- API error propagates with wrapping.
- `alreadyOpen=true` plumbed through.

### Layer 3 — Mode + reducer

- `Ctrl+N` in `ModeNormal` enters `ModeNewMessage` and constructs picker.
- Submit dispatches `ChannelService.OpenConversation` with correct user IDs.
- `ConversationOpenedMsg` (not cancelled) → emits `ChannelSelectedMsg` + `EnterInsertModeMsg`; modal closes.
- Esc during in-flight sets cancelled; late `ConversationOpenedMsg` is dropped (no mode change, no channel switch).
- Error during in-flight keeps modal open with error banner; selections intact.
- Channel-not-in-cache path: minimal record inserted before `ChannelSelectedMsg`.

### Manual verification (not automated)

- Pill rendering, overlay centering, theme application.
- End-to-end RTM event arrival for a newly opened MPIM.

## Open Questions

None at design time. Captured decisions:

- Keybind: `Ctrl+N`.
- Selection model: explicit toggle (Space/Tab), Enter submits.
- User list: active humans + externals, recent DMs first.
- Post-open: switch + focus compose (`ModeInsert`).
- Edge cases: standard Slack behavior (self excluded, MPIM cap = 8, existing reused).
- Esc during in-flight: abandon result.
