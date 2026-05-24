# Tab Title Unread Indicator Design

## Motivation

Closes [#25](https://github.com/gammons/slk/issues/25).

slk has no notification surface beyond the in-app sidebar dot. A user with slk in a background tab (terminal tab, tmux window, multiplexer pane) cannot tell whether new messages have arrived without switching to it. Desktop notifications are gated by Slack's notification preferences and (intentionally) easy to silence, which leaves a real gap: "is there anything for me?" requires an active context switch.

The terminal window title is the natural place to expose this signal. It is already visible in every terminal multiplexer (tmux/screen), every modern terminal emulator's tab bar (kitty, alacritty, wezterm, iTerm2, Windows Terminal, gnome-terminal), and most window managers' task lists. Setting it costs nothing — a single OSC 2 escape per state change — and it degrades gracefully on terminals that don't render the title.

## Format

Workspace identity uses the same two-letter initials the workspace rail renders (`WorkspaceInitials` in `internal/ui/workspace/model.go`). Followed by an optional count of channels-with-unreads in the active workspace, followed by an optional cross-workspace overflow marker.

| State | Title |
|---|---|
| Pre-bootstrap (no workspace selected) | `slk` |
| No unreads anywhere | `slk SW` |
| Unreads only in active workspace | `slk SW (3)` |
| Unreads only in inactive workspaces | `slk SW +1` |
| Unreads in active and inactive workspaces | `slk SW (3) +1` |

The count `(N)` is channels-with-unreads in the active workspace, consistent with the deliberate semantic established in [`2026-05-17-read-state-sync-design.md`](./2026-05-17-read-state-sync-design.md): "boolean, not integer." The trailing `+N` is the count of *other* workspaces with at least one unread channel, derived from `db.WorkspacesWithUnreads()` minus the active workspace.

Worst-case width: with the maximum reasonable values for both numbers (`(99) +99`), the title is `slk SW (99) +99` = 15 characters. Fits the tightest realistic tab budget (tmux window list at ~15 chars; kitty/wezterm with many tabs at ~20). Initials are fixed-width, so the title never overflows on long workspace names.

### Format alternatives considered

- **Full slug or workspace name** (`slk swap (3) +1`). Better disambiguation in the common 1–3-workspace case but breaks the width budget when slugs exceed ~10 characters (a `stratusgrid-eng`-style slug pushes the title past 25 chars). The rail already uses initials for the same reason; the title inherits that convention rather than re-litigating it.
- **`slk *` (single marker)** — the issue's first suggestion. Loses the count, loses the workspace identity, loses the multi-workspace signal. Rejected as strictly less informative at the same character cost.
- **No workspace identifier** (`slk (3) +1`). Most compact, but a user with multiple workspaces tabs back and forth between them and the title doesn't tell them which one is active. The whole point of the rail showing initials is that "which workspace am I in" is a real question.
- **`(3) slk SW`** — number first. Slightly easier to scan in a wide tab bar but breaks the "app name comes first" convention every other terminal app follows. Rejected for consistency.
- **Channel-name display** (`slk #general (3)`) — communicates *where* an unread is, but only useful when there is exactly one. Drops information in the common case. Rejected.

## Architecture

Bubbletea v2's `tea.View` struct exposes a `WindowTitle string` field. The renderer (`charm.land/bubbletea/v2@v2.0.6/cursed_renderer.go:128-129,189-191,372-374`) emits `ansi.SetWindowTitle()` whenever the value changes between renders. This is the entire transport — no manual escape emission, no separate `tea.Cmd`, no goroutine.

The unread state already drives a single in-app event: `ReadStateChangedMsg` (declared in `internal/ui/app.go`) is sent by every read-state mutator (in `cmd/slk/main.go` and `cmd/slk/reconnect_backfill.go`) and handled by `App.Update`, which calls `notifyReadStateChanged`. That function currently invalidates the sidebar cache and refreshes the workspace rail; this design adds one more line to it: update a cached `windowTitle` field on `App`.

```go
// internal/ui/app.go
type App struct {
    // ...
    windowTitle string // computed in notifyReadStateChanged, read in View
}

func (a *App) notifyReadStateChanged() {
    a.sidebar.Invalidate()
    a.workspaceRail.RefreshUnreads()
    a.windowTitle = a.computeWindowTitle() // NEW
}

func (a *App) View() tea.View {
    // ... existing layout ...
    v := tea.NewView(screen)
    v.AltScreen = true
    v.WindowTitle = a.windowTitle // NEW (or "" pre-bootstrap)
    return v
}
```

The title is *not* recomputed on every `View()` call. `notifyReadStateChanged` is the only source of truth and is already invoked by all six read-state write paths. Workspace switches also call it (the workspace switch already calls `notifyReadStateChanged` via the existing rail refresh path).

## Computation

```go
// internal/ui/app.go
func (a *App) computeWindowTitle() string {
    if a.activeWorkspace == nil {
        return "slk"
    }
    activeID := a.activeWorkspace.TeamID
    initials := workspace.WorkspaceInitials(a.activeWorkspace.Name)

    activeCount := 0
    if states, err := a.db.GetWorkspaceReadState(activeID); err == nil {
        for _, s := range states {
            if s.HasUnread {
                activeCount++
            }
        }
    }

    otherCount := 0
    if ids, err := a.db.WorkspacesWithUnreads(); err == nil {
        for _, id := range ids {
            if id != activeID {
                otherCount++
            }
        }
    }

    return formatTitle(initials, activeCount, otherCount)
}

// formatTitle is pure; covered by table-driven tests.
func formatTitle(initials string, active, other int) string {
    out := "slk " + initials
    if active > 0 {
        out += fmt.Sprintf(" (%d)", active)
    }
    if other > 0 {
        out += fmt.Sprintf(" +%d", other)
    }
    return out
}
```

Both DB calls are already used elsewhere in the render path (in `cmd/slk/main.go`); no new query surface. Errors are logged and the title degrades to the initials-only form rather than failing. The `WorkspaceInitials` helper already handles edge cases (empty name → `"?"`, single-letter names, multi-word names).

## Muted channels

Channels marked muted in Slack already have their `HasUnread` flag stored at the DB level (the flag is set by the same write paths regardless of mute state), but the sidebar suppresses the dot for muted channels at render time using its `ChannelItem.IsMuted` field (`internal/ui/sidebar/model.go`).

For the **active-workspace count** (the `(N)` portion), the title filters mute the same way: a new accessor on `sidebar.Model` reads the installed read-state callback, iterates the sidebar's items, and counts entries where `state[id].HasUnread && !item.IsMuted`. The sidebar already has both inputs in hand at render time; exposing the count is a one-method addition. This guarantees `(N)` matches the sidebar dot population exactly — no "title says 3 but I only see 2 dots."

For the **`+N` overflow**, the title uses the existing `db.WorkspacesWithUnreads()` reader unchanged. That reader does **not** filter mute today — the workspaceRail dot it drives (`internal/ui/workspace/model.go`) also doesn't — so a workspace whose only unreads are muted does show both a rail dot and contribute to the title's `+N`. We accept this small inconsistency rather than re-engineer mute-aware aggregation at the DB level. If users complain that `+N` is over-counted, the fix lives in the rail too (a single coherent change), not in title-only special-casing.

## tmux and multiplexer passthrough

`ansi.SetWindowTitle` emits a plain OSC 2 sequence (`ESC ] 2 ; <title> BEL` or with `ST` terminator). Inside tmux, this reaches the outer terminal *only* when the user has `set -g set-titles on` configured. Without that, tmux swallows the OSC.

We will:

- Emit the plain OSC and rely on the user's tmux config, the same approach the kitty image escape passthrough fix (`acda354 image: detect kitty under tmux ...`, `30c998a fix: wrap Kitty image escapes for tmux passthrough`) took before adding the DCS wrap. This is the path of least surprise.
- Document the tmux `set-titles on` requirement in the README's tmux section alongside the existing kitty notes.
- Defer DCS-wrapped passthrough (`ESC P tmux ; ESC <OSC> ESC \`) until a user reports it actually missing. The image-escape case was a real demand; the title is a far weaker signal and we should not preemptively grow the renderer's complexity.

## Configuration

A configuration knob is **out of scope for v1**. Justifications:

- The title only updates on real state changes (handful per minute at most).
- The format is benign: it never exceeds ~25 characters, contains no sensitive content, and degrades to plain `slk` at zero-unreads-anywhere.
- Terminals and multiplexers that don't support OSC 2 silently drop it.
- Users who want it off can almost always disable it at the terminal level (kitty: `dynamic_background_opacity yes` doesn't help here, but most muxers have a per-window override).

If a credible "off" request lands, the config schema is straightforward: a `[ui] tab_title = "off" | "count" | "marker"` key, defaulting to `"count"`. Adding the switch later is a single conditional in `computeWindowTitle`.

## Edge cases

- **Pre-bootstrap.** Before any workspace is connected, `a.activeWorkspace` is nil. Title is `"slk"`. The post-bootstrap rebroadcast of `ReadStateChangedMsg` (already fired by `connectWorkspace` after `BatchUpdateChannelReadState`) updates the title to the real value.
- **Workspace switch.** The switch handler already calls `notifyReadStateChanged` (via the existing rail refresh). Title updates with the same tick.
- **Workspace removed.** `slk --remove-workspace` is a separate `os.Exit` flow; in-process workspace removal does not exist. Out of scope.
- **All workspaces disconnected.** `a.activeWorkspace` remains set to the last-active workspace; title shows its cached state. Acceptable: the user knows what they were looking at.
- **No channels yet (fresh workspace).** `GetWorkspaceReadState` returns an empty map; `activeCount` is 0; title is `slk SW` (no count). Correct.
- **Hundreds of unread channels.** `(347)` rendered literally. No truncation. Terminal handles it.

## Test plan

### New tests

**`internal/ui/app_title_test.go`** — table-driven on `formatTitle`:

| initials | active | other | expected |
|---|---|---|---|
| `SW` | 0 | 0 | `slk SW` |
| `SW` | 3 | 0 | `slk SW (3)` |
| `SW` | 0 | 1 | `slk SW +1` |
| `SW` | 3 | 1 | `slk SW (3) +1` |
| `SW` | 99 | 99 | `slk SW (99) +99` |
| `?` | 0 | 0 | `slk ?` (fallback from `WorkspaceInitials` for empty name) |

**`internal/ui/app_test.go`** — integration:

- `notifyReadStateChanged` populates `a.windowTitle` with the expected string for a workspace whose cache has N unread channels.
- Muted channels are excluded from the count.
- `View()` returns a `tea.View` whose `WindowTitle` field equals `a.windowTitle`.
- Pre-bootstrap `View()` returns `WindowTitle == "slk"`.

### Tests expected to break

None. This change is additive: it adds a field, populates it in an existing hook, and reads it in an existing function. No existing assertions about `View()` content cover `WindowTitle`.

## Out of scope (follow-ups)

1. Configuration knob (`[ui] tab_title = ...`). Add when a user asks.
2. DCS-wrapped passthrough for tmux users without `set-titles on`. Add when a user reports the missing signal.
3. Channel-level granularity in the title (e.g., `slk #general 3`). Distinct UX call; not in #25.
4. Workspace-icon glyphs (Slack workspace icons in the title). Outside the OSC contract; not in #25.

## Open questions

None. All design decisions are settled.
