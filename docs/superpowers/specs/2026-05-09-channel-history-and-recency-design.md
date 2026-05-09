# Channel navigation history and recency-ordered finder

## Problem

Two related quality-of-life gaps in slk's channel navigation:

1. **No back/forward.** The user has no way to "go back" to a channel
   they were just viewing. The only existing memory is
   `lastChannelByTeam` (`internal/ui/app.go:711`) — one slot per
   workspace, used solely to restore "where you were" when switching
   workspaces. Within a workspace, opening a channel is a one-way trip;
   re-finding the previous one means Ctrl+T → type → Enter.
2. **Channel finder doesn't know what you use.** `Ctrl+T` (the
   `channelfinder` overlay, `internal/ui/channelfinder/model.go`)
   sorts results by match quality + a fixed `typeRank` (DMs first,
   channels middle, group DMs last), then original index. With no
   query typed, the order is whatever `SetItems` happened to receive
   — typically joined-channels first in API order, then browseable
   channels appended. A channel you open ten times a day appears
   alphabetically next to one you've never visited.

The first problem motivates a Vim-style jump list bound to
`Ctrl+H` / `Ctrl+K`. The second motivates persisting per-channel
visit timestamps and using them as a sort key.

## Goals

- **Back/forward** within a workspace via `Ctrl+H` (back) and `Ctrl+K`
  (forward) in normal mode. Behaves like a browser tab: each channel
  open pushes onto a stack; back/forward walk the stack; opening a
  new channel from a back-position truncates the forward path.
- **Channel finder ordered by recency.**
  - Empty query: most-recently-visited channels first; never-visited
    fall through to the existing `typeRank` + name order.
  - With query: existing match-quality tiers still win, but recency
    breaks ties within a tier.
- **Recency persists across slk restarts** (SQLite-backed). The
  back/forward stack does not — it is session-only.
- **Per-workspace isolation.** Each workspace has its own back/forward
  stack and its own visit timestamps. `Ctrl+H` never causes a
  workspace switch.
- **Stale channels survive gracefully.** A back/forward target that
  is no longer in the workspace (left, archived, kicked) is silently
  skipped and dropped from the stack; navigation continues with the
  next valid entry.

## Non-goals

- Persisting the back/forward stack across restarts. Session-only,
  per workspace.
- Workspace-crossing back/forward. The stack is per-workspace; a
  global stack that triggers workspace switches is rejected.
- Frecency formula (count + decay, like `frecent_emoji`). Pure
  recency only — `last_visited` is the single sort key.
- Recording thread side-panel opens, the workspace-wide threads view,
  or workspace switches as nav events. Channels only.
- Visual indicator / status flash on stack-boundary press (e.g. when
  back is pressed at the start of the stack). Silent — matches Vim's
  jump-list behavior.
- A "history menu" UI (browser-style long-press to see the full
  stack).

## Design

### 1. Shared integration point: `ChannelSelectedMsg`

Every channel-open path in slk already funnels through
`case ChannelSelectedMsg` in `internal/ui/app.go:1314`:

- Sidebar Enter (`app.go:3287`)
- Sidebar mouse click (`app.go:1027`)
- Channel finder selection (`app.go:2628`)
- Workspace switch auto-restore (`app.go:1942, 2040, 2086`)

Both features hook into this single chokepoint. To distinguish
"user-initiated open" from "back/forward-synthesized open", we add
one field:

```go
type ChannelSelectedMsg struct {
    ID, Name, Type string
    FromHistory    bool // NEW: true when synthesized by ctrl+h/ctrl+k
}
```

All existing emit sites omit the field (zero value `false`),
preserving today's behavior. Only the new `navigateBack` /
`navigateForward` set it `true`.

In the handler, after the existing channel-open work runs, we add
two responsibilities:

1. **Push to nav stack** if `!msg.FromHistory`.
2. **Record a channel visit** in SQLite — always, including
   `FromHistory` navigations. Pressing `Ctrl+H` to a channel makes
   that channel "most recent"; this matches browser-style recency.

### 2. Keybindings

In `internal/ui/keys.go`, add two `key.Binding` fields to `KeyMap`:

```go
NavBack    key.Binding
NavForward key.Binding
```

In `DefaultKeyMap()`:

```go
NavBack:    key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "navigate back")),
NavForward: key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "navigate forward")),
```

Dispatched only from `handleNormalMode` (`app.go:2230`), so they
don't conflict with the channel finder, command line, search, or
insert mode.

**Terminal byte note:** `Ctrl+H` shares its byte (`0x08`) with
`Backspace` on most terminals, so `Backspace` in normal mode will
also trigger back. `Backspace` is currently unbound in normal mode,
so this is a benign side effect (arguably a feature — Backspace as
"back" matches some users' instincts).

### 3. Feature 1: per-workspace back/forward stack (in-memory)

#### State on `App`

Added near `lastChannelByTeam` (`internal/ui/app.go:711`):

```go
type navStack struct {
    entries []string // channel IDs, oldest at index 0
    cursor  int      // index of current entry; -1 when empty
}

navHistory map[string]*navStack // teamID -> stack (lazy-init)
```

`lastChannelByTeam` stays — it drives workspace-switch auto-restore
and is conceptually different (one slot per team, not a stack). The
two coexist; minimum blast radius.

The "user-initiated vs back/forward-initiated" distinction is carried
on the message itself via `ChannelSelectedMsg.FromHistory`, not via
mutable App state. This survives any future async re-ordering of
message dispatch and avoids a hidden flag-flipping protocol between
the two normal-mode handlers and the central `ChannelSelectedMsg`
case.

#### Push semantics

In `case ChannelSelectedMsg`, after the existing handler body, when
`!msg.FromHistory`:

```go
stack := a.getOrCreateStack(a.activeTeamID)

// Dedupe consecutive: selecting the same channel you're on is a no-op.
if stack.cursor >= 0 && stack.entries[stack.cursor] == msg.ID {
    return ...
}

// New navigation kills the forward path (browser-style).
if stack.cursor < len(stack.entries)-1 {
    stack.entries = stack.entries[:stack.cursor+1]
}

stack.entries = append(stack.entries, msg.ID)
stack.cursor = len(stack.entries) - 1

// Cap at 50; evict oldest.
const navStackMax = 50
if len(stack.entries) > navStackMax {
    drop := len(stack.entries) - navStackMax
    stack.entries = stack.entries[drop:]
    stack.cursor -= drop
}
```

#### Back/forward navigation

Two new methods on `App`:

```go
func (a *App) navigateBack() (tea.Model, tea.Cmd)
func (a *App) navigateForward() (tea.Model, tea.Cmd)
```

Both walk the stack in their direction, dropping any channel ID that
no longer exists in the current workspace (and adjusting `cursor`
accordingly), until either:

- A valid target is found → emit `ChannelSelectedMsg{..., FromHistory: true}` as a `tea.Cmd`. The handler will see the flag and skip the push (but will still call the visit recorder).
- No valid target → silent no-op. The user gets no flash, matching Vim.

**Channel-existence check.** New callback on `App`:

```go
type ChannelLookupFunc func(channelID string) (name, channelType string, ok bool)
SetChannelLookupFunc(fn ChannelLookupFunc)
```

In `cmd/slk/main.go:wireCallbacks(wctx)`, bound to a closure that
consults `wctx.Channels` (sidebar items) and falls back to
`wctx.FinderItems` for channels in the finder but not the sidebar
(rare — DMs without an active conversation, etc.). Returns the
display name and channel type, which `navigateBack/Forward` need to
populate the synthesized message.

#### Workspace switch interaction

Untouched. `lastChannelByTeam` continues to drive auto-restore. The
new workspace's `navHistory[teamID]` is a simple map lookup: present
if the user has been there before this session, absent (lazy-create
on first push) otherwise. The auto-restore goes through the normal
`ChannelSelectedMsg` path, which dedupes against the cursor — so
re-visiting workspace B after going A→B→A→B does not duplicate
B's most-recent entry.

### 4. Feature 2: channel finder ordered by recency (SQLite-backed)

#### Schema (`internal/cache/db.go`)

Additive table, added to the initial-schema block:

```sql
CREATE TABLE IF NOT EXISTS channel_visits (
    workspace_id TEXT    NOT NULL,
    channel_id   TEXT    NOT NULL,
    last_visited INTEGER NOT NULL, -- unix seconds
    PRIMARY KEY (workspace_id, channel_id)
);
CREATE INDEX IF NOT EXISTS idx_channel_visits_recent
    ON channel_visits(workspace_id, last_visited DESC);
```

`CREATE TABLE IF NOT EXISTS` handles both fresh installs and existing
DBs without a migration. Composite primary key is keyed on
`(workspace_id, channel_id)` so that per-workspace queries can use
the index and so the same Slack channel ID across workspaces (rare
but possible for DM IDs) doesn't collide.

#### Cache layer: `internal/cache/channelvisits.go`

Mirrors `internal/cache/frecent.go`:

```go
func (s *Store) RecordChannelVisit(ctx context.Context, workspaceID, channelID string) error
func (s *Store) GetChannelVisits(ctx context.Context, workspaceID string) (map[string]int64, error)
```

`RecordChannelVisit` is `INSERT ... ON CONFLICT(workspace_id, channel_id) DO UPDATE SET last_visited = excluded.last_visited`.
`GetChannelVisits` returns `channelID -> last_visited` for the
workspace, used to seed the in-memory map at workspace connect.

Companion test `internal/cache/channelvisits_test.go` mirrors
`frecent_test.go`: round-trip, last-write-wins, multi-workspace
isolation.

#### Wiring (`cmd/slk/main.go`)

`WorkspaceContext` (`cmd/slk/main.go:89`) gains:

```go
LastVisitedByChannel map[string]int64 // channelID -> unix seconds
```

Populated in `connectWorkspace` (`cmd/slk/main.go:1053`) from
`cache.GetChannelVisits(wctx.TeamID)` once, then mutated in-place
on every visit.

New `App` callback:

```go
type ChannelVisitRecorder func(channelID string)
SetChannelVisitRecorder(fn ChannelVisitRecorder)
```

In `wireCallbacks(wctx)`, bound to a closure that:

1. Updates `wctx.LastVisitedByChannel[channelID]` to `time.Now().Unix()`.
2. Calls `cache.RecordChannelVisit(wctx.TeamID, channelID)` as a
   tea.Cmd (fire-and-forget; SQLite write off the UI thread).
3. Calls `app.channelFinder.UpdateLastVisited(channelID, ts)` so
   live finder ordering reflects the visit immediately, without a
   full `SetItems` rebuild.

The recorder is invoked from `case ChannelSelectedMsg` regardless
of `FromHistory` — going back updates recency too.

#### Channel finder changes (`internal/ui/channelfinder/model.go`)

`Item` (`channelfinder/model.go:28`) gains:

```go
LastVisited int64 // unix seconds; 0 = never visited
```

`SetItems` and `SetBrowseable` accept the existing slice shape; the
caller in `cmd/slk/main.go` populates `LastVisited` from
`wctx.LastVisitedByChannel` before calling.

New method:

```go
func (m *Model) UpdateLastVisited(channelID string, ts int64)
```

Walks `m.items`, updates the matching entry's `LastVisited`, and (if
the finder is currently visible with no query) re-runs `filter()` so
the user sees the new order on next render.

`filter()` (`channelfinder/model.go:173`) sort key is rewritten:

- **Empty query:** `(LastVisited DESC, typeRank ASC, name ASC)`
  - Channels with a non-zero `LastVisited` come first, ordered by
    most-recent.
  - Never-visited (`LastVisited == 0`) fall to the bottom and use
    the existing `typeRank` (DMs first) + name secondary sort.
- **With query:** `(matchTier ASC, LastVisited DESC, typeRank ASC, name ASC)`
  - Match tier (prefix → substring → subsequence) still wins,
    preserving "best matches first" for searches.
  - Within a tier, recency is the next tiebreaker.
  - `typeRank` and name are final tiebreakers.

The existing `typeRank` function is unchanged — it just moves down
in the sort key priority.

### 5. Tests

**New test file `internal/ui/app_nav_test.go`:**

- `TestNavStack_PushAndBack` — three opens, two backs, assert cursor and synthesized `ChannelSelectedMsg{FromHistory: true}`.
- `TestNavStack_DedupeConsecutive` — opening the same channel twice in a row pushes once.
- `TestNavStack_ForwardTruncation` — open A, B, C; back to B; open D; assert C is dropped, forward from B goes to D only.
- `TestNavStack_Cap` — 51 distinct opens; assert length stays at 50, oldest evicted, cursor adjusted.
- `TestNavStack_StaleChannelSkipped` — A, B (stale), C; back from C skips B and lands on A; B removed from entries.
- `TestNavStack_PerWorkspaceIsolation` — workspace switch doesn't bleed history; each team has its own cursor.
- `TestNavStack_BoundaryNoOp` — back at cursor 0 and forward at top of stack are silent no-ops.

**New test file `internal/cache/channelvisits_test.go`** (modeled on
`frecent_test.go`):

- Round-trip insert + read.
- Last-write-wins on conflict.
- Multi-workspace isolation.

**Additions to `internal/ui/channelfinder/model_test.go`:**

- Empty query orders visited channels by recency desc.
- Empty query: never-visited fall through to existing typeRank + name.
- With query: match tier still wins regardless of recency.
- With query: same-tier matches ordered by recency desc.
- `UpdateLastVisited` reflects in next `filter()` call.

## Files touched

**New:**

- `internal/cache/channelvisits.go`
- `internal/cache/channelvisits_test.go`
- `internal/ui/app_nav_test.go`

**Modified:**

- `internal/cache/db.go` — `channel_visits` table + index in initial schema block
- `internal/ui/keys.go` — `NavBack`, `NavForward` bindings
- `internal/ui/app.go` — `navStack` type, `navHistory` field, extended `ChannelSelectedMsg`, push/dedupe/cap logic in handler, `navigateBack`/`navigateForward`, two new key cases in `handleNormalMode`, `SetChannelLookupFunc`, `SetChannelVisitRecorder` callbacks, recorder invocation in `case ChannelSelectedMsg`
- `internal/ui/channelfinder/model.go` — `Item.LastVisited`, `UpdateLastVisited` method, rewritten `filter()` sort key
- `internal/ui/channelfinder/model_test.go` — new test cases for recency ordering
- `cmd/slk/main.go` — `WorkspaceContext.LastVisitedByChannel` field, `connectWorkspace` populates it from `cache.GetChannelVisits`, `wireCallbacks` binds visit recorder + channel lookup, finder `SetItems`/`SetBrowseable` calls pass through last-visited timestamps

## Implementation order

To keep changes reviewable and reduce conflict surface:

1. **Foundation:** add `FromHistory` field to `ChannelSelectedMsg`, add `NavBack`/`NavForward` keymap entries (no behavior yet).
2. **Feature 1 (back/forward):** add `navStack` + `navHistory`, wire push in handler, implement `navigateBack`/`navigateForward`, add `ChannelLookupFunc` callback and main.go binding, add `app_nav_test.go`.
3. **Feature 2 cache layer:** add `channel_visits` schema, `channelvisits.go`, tests.
4. **Feature 2 wiring:** `WorkspaceContext.LastVisitedByChannel`, populate on connect, `ChannelVisitRecorder` callback + main.go binding, recorder call in `case ChannelSelectedMsg`.
5. **Feature 2 finder:** `Item.LastVisited`, `UpdateLastVisited`, rewritten `filter()`, finder tests.

Each step compiles and is independently shippable; steps 1-2 deliver
back/forward, steps 3-5 deliver recency ordering.
