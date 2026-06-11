# Vim-Style Window Management: Splits, `ctrl+w` Chords, `:` Command Mode

**Date:** 2026-06-11
**Status:** Approved
**Related:** `ModeCommand` stub (`internal/ui/mode_command.go`), panel layout
(`internal/ui/panellayout.go`), focus cycling (`internal/ui/app.go:1436`)

## Problem

slk shows exactly one channel at a time. The messages pane is a singleton:
to watch #incidents while chatting in #general you must bounce between
channels. Vim users expect `:vsp`-style splits, `ctrl+w` window navigation,
and a `:` command prompt — the mode FSM even reserves `ModeCommand` for
this, but it has never been wired up.

## Goals

1. **Split windows.** The messages area becomes a vim-style window tree:
   arbitrary nesting of horizontal (`:sp`, stacked) and vertical (`:vsp`,
   side-by-side) splits. Each window is an independent live channel view
   with its own channel, scroll position, and selection. A new split clones
   the current window's channel; the sidebar and channel finder then
   retarget the focused window.
2. **`ctrl+w` chords.** Reclaim `ctrl+w` from the workspace finder and make
   it the window-command prefix: `s`/`v` split, `h/j/k/l` directional
   focus, `w` cycle, `q`/`c` close, `o` only, `=` equalize,
   `<`/`>`/`+`/`-` resize.
3. **`:` command mode.** Activate the stubbed `ModeCommand`: a status-bar
   prompt with a command registry. v1 commands: `sp`, `vsp`, `q`, `only`
   (alias `on`), `ws` (workspace finder, replacing its old `ctrl+w`
   binding). The registry is the extension point for future commands.
4. **Chrome stays put.** The workspace rail and channel sidebar remain
   fixed chrome to the left of the tree (sidebar keeps its existing
   `ctrl+b` toggle and `[`/`]` resize). One global thread pane stays at
   the right edge and follows the focused window.

## Non-Goals (v1)

- Moving windows within the tree (`ctrl+w H/J/K/L`).
- Persisting the split layout across restarts (always start with one
  window).
- Per-window thread panes (single global thread pane for space reasons).
- Tabs (`:tabnew`) or any buffer-list concept.
- Shared per-channel data models. Two windows on the same channel each own
  a full `messages.Model` (duplicate state is accepted; a data/viewport
  split can be retrofitted if it ever matters).
- User-configurable keybindings (slk has none today; unchanged).

## Design

### 1. Window tree (`internal/ui/wintree`)

A new pure-data package, no UI dependencies. Internal nodes are splits
(direction + per-child size weights); leaves are windows (stable window ID
+ channel ID). Operations:

- `SplitH(id)` / `SplitV(id)` — insert a sibling leaf; new window gets
  focus (vim behavior). Refused if the resulting windows would violate
  minimum size.
- `Close(id)` — remove leaf; space goes to siblings; collapses
  single-child split nodes; focus moves to the nearest neighbor. Refused
  on the last window.
- `NavigateDir(id, dir)` — geometric neighbor lookup (vim-style: the
  window whose rect adjoins in that direction, nearest by overlap).
- `Cycle(id)` — next window in tree order, wrapping.
- `Only(id)` — collapse tree to one leaf.
- `Resize(id, axis, delta)` / `Equalize()` — adjust weights.
- `ComputeRects(bounds)` — resolve per-window rects from weights, honoring
  minimum window size (40 cols wide, matching the existing messages-pane
  minimum; 8 rows tall). On terminal shrink, windows
  clamp to minimums with the same degradation rules as today's panes.

Layout is never persisted; the tree starts as a single window each launch.

### 2. Per-window channel views

Each window owns its own `messages.Model` instance, constructed and seeded
from the SQLite cache through the same path used for channel switching
today. The `App` singleton field becomes a map keyed by window ID.

**Event fan-out:** every seam that currently routes a channel-scoped event
(new message, edit, delete, reaction, history loaded, typing) to the
singleton model instead dispatches to *all* windows whose channel matches.
Each window's model versions independently.

**Read state:** mark-as-read fires only for the *focused* window.
Background splits never advance the read marker.

**Channel selection:** the sidebar and fuzzy channel finder set the
channel of the focused window only.

### 3. Focus model

`App.focusedPanel` keeps the existing `Panel` enum (rail / sidebar /
messages region / thread). A new `focusedWindowID` identifies the active
window while focus is in the messages region. Tab / `h` / `l` panel
cycling extends to: rail → sidebar → each window in tree order → thread.
`FocusNext`/`FocusPrev` (`internal/ui/app.go:1436`) generalize accordingly.

### 4. Keybindings

`ctrl+w` in normal mode enters a one-shot pending state (Esc cancels;
shown in the status bar like vim's partial-command area):

| Chord | Action |
|---|---|
| `ctrl+w s` | horizontal split (stacked), clone current channel |
| `ctrl+w v` | vertical split (side-by-side), clone current channel |
| `ctrl+w h/j/k/l` | focus window in direction |
| `ctrl+w w` | cycle to next window |
| `ctrl+w q`, `ctrl+w c` | close focused window |
| `ctrl+w o` | close all other windows |
| `ctrl+w =` | equalize sizes |
| `ctrl+w <` / `>` | shrink / grow width |
| `ctrl+w +` / `-` | grow / shrink height |

The workspace finder loses its `ctrl+w` binding (workspaces remain
reachable via `1`–`9` and the new `:ws` command). All chords and commands
register in the central `KeyMap` (`internal/ui/keys.go`) so the help
overlay picks them up automatically.

### 5. Command mode (`internal/ui/mode_command.go`)

`:` in normal mode enters `ModeCommand`. The status bar renders the prompt
(`:vsp▌`) and the mode indicator shows COMMAND. Enter parses and executes;
Esc cancels; unknown commands produce a status-bar error
(`Unknown command: foo`). Commands dispatch through a small registry
(name → handler), the designated extension point for future `:` commands.

v1 registry: `sp`, `vsp`, `q`, `only`/`on`, `ws`. No arguments in v1
(`:sp #channel` is a natural follow-up, not included).

### 6. Rendering and mouse

- `view_messages.go` walks `ComputeRects` and renders each window through
  the per-panel render cache (`internal/ui/panelcache.go`), with cache
  keys extended from the fixed `PanelMessages` slot to per-window IDs
  (window ID + model version + rect + theme version + focused flag).
- Focused window: existing focus border. Unfocused windows: dimmed border
  with channel name in the title, matching current unfocused-pane styling.
- `panelLayout` (`internal/ui/panellayout.go`) consults the tree's rects
  inside the messages region: `PanelAt` resolves to a specific window for
  click-to-focus and scroll routing; dragging an internal tree border
  resizes the adjoining windows (reusing the existing border-drag
  machinery in `internal/ui/drag.go`).

### 7. Thread pane

Unchanged position: a single global pane at the right edge, outside the
tree. It opens from the focused window's selected message. Changing the
focused window while a thread is open closes the thread (simplest correct
behavior; reopening is one keypress).

## Errors

- Split refused for space → status bar: `Not enough room`.
- Close/`:q` on last window → status bar: `Cannot close last window`
  (never quits the app).
- Unknown `:` command → status bar: `Unknown command: <name>`.
- Pending `ctrl+w` followed by an unmapped key → cancel silently (vim
  behavior).

## Testing

- **`wintree` unit tests:** split/close/navigate/cycle/only/resize/
  equalize; rect computation including weights, minimums, refusal cases,
  and collapse-on-close invariants. Pure functions, no UI.
- **Command mode:** parser + registry dispatch, unknown-command error,
  prompt entry/exit through the mode FSM.
- **Fan-out:** channel-scoped events reach all matching windows and only
  matching windows; read-marker only advances for the focused window.
- **Rendering:** view-level tests for multi-window composition following
  existing `view_*_test.go` patterns; cache-key correctness (no stale
  frames after a background window updates).
- **Manual smoke:** mouse click-to-focus, border drag-resize, terminal
  resize reflow.

## Implementation Phasing

1. **Command mode + key reclaim** — wire `ModeCommand`, registry, `:ws`,
   unbind workspace finder from `ctrl+w`. Shippable alone.
2. **Window tree** — `wintree` package with full test coverage; rendering
   of multiple windows with the focused window live (others static
   placeholders during this phase only).
3. **Per-window channel views** — model-per-window, event fan-out,
   channel retargeting, read-state rule.
4. **Integration polish** — thread-follow behavior, mouse routing and
   drag-resize, focus cycling, help overlay entries, resize chords.
