# Tab Title Unread Indicator — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the tab title unread indicator described in [`docs/superpowers/specs/2026-05-21-tab-title-unread-indicator-design.md`](../specs/2026-05-21-tab-title-unread-indicator-design.md). Closes [#25](https://github.com/gammons/sl​k/issues/25).

**Architecture:** Add a `windowTitle string` field on `App` that's populated by a *pure free function* `computeWindowTitle(activeTeamID, workspaceName string, activeUnreads, otherUnreads int) string` living in `internal/ui/app_title.go`. Call it from `notifyReadStateChanged` (the existing central hook for every read-state mutation), passing inputs sourced from accessors on the sub-models that already own the data. Read the cached value into `tea.View.WindowTitle` from `View()`. Bubbletea v2's renderer emits the OSC sequence whenever the field value changes.

Counting logic lives on the sub-models that own the underlying state, not on App:

- `sidebar.Model.UnreadChannelCount() int` — uses a new `ChannelItem.IsVisiblyUnread(state)` helper that consolidates the *single* "shown as an unread dot" predicate currently duplicated inline in `View()`.
- `workspace.Model.OtherUnreadCount(activeID string) int` — uses the rail's existing `unreadReader` callback. App does *not* grow a new reader field; existing single-place setters remain symmetric.
- `workspace.Model.NameByID(id string) string` — small accessor to feed `workspace.WorkspaceInitials`.

This keeps `App` as an orchestrator: it knows where to source each input but doesn't reach into sub-model internals or duplicate predicates.

**Tech Stack:** Go, `charm.land/bubbletea/v2` (`tea.View.WindowTitle`), existing slk read-state API.

---

### Task 1: Add `NameByID` and `OtherUnreadCount` to `workspace.Model`

**Files:**
- Modify: `internal/ui/workspace/model.go`
- Modify: `internal/ui/workspace/model_test.go`

- [ ] **Step 1: Add accessors**

  Append after `SelectedID()`:

  ```go
  // NameByID returns the display name for the given team ID, or "" if
  // no workspace with that ID is present. Used by the App to compute
  // the window title's two-letter initials via WorkspaceInitials.
  func (m *Model) NameByID(id string) string {
      for _, item := range m.items {
          if item.ID == id {
              return item.Name
          }
      }
      return ""
  }

  // OtherUnreadCount returns the number of workspaces (excluding the
  // given activeID) that currently have at least one channel with
  // has_unread=true. Reads through the installed unreadReader; returns
  // 0 if no reader is set or activeID is empty. Used by the App to
  // compute the window title's "+N" overflow.
  //
  // Does NOT filter mute (matches the rail dot's existing semantics —
  // workspaces with only-muted unreads still contribute, the same way
  // they already light up the rail dot).
  func (m *Model) OtherUnreadCount(activeID string) int {
      if m.unreadReader == nil {
          return 0
      }
      count := 0
      for _, id := range m.unreadReader() {
          if id != activeID {
              count++
          }
      }
      return count
  }
  ```

- [ ] **Step 2: Add tests**

  Append to `model_test.go`:

  ```go
  func TestNameByID(t *testing.T) {
      m := New([]WorkspaceItem{
          {ID: "T1", Name: "SWAP", Initials: "SW"},
          {ID: "T2", Name: "Home", Initials: "HO"},
      }, 0)
      cases := map[string]string{"T1": "SWAP", "T2": "Home", "T-missing": ""}
      for id, want := range cases {
          if got := m.NameByID(id); got != want {
              t.Errorf("NameByID(%q) = %q, want %q", id, got, want)
          }
      }
  }

  func TestOtherUnreadCount(t *testing.T) {
      m := New([]WorkspaceItem{
          {ID: "T1"}, {ID: "T2"}, {ID: "T3"},
      }, 0)

      // No reader installed
      if got := m.OtherUnreadCount("T1"); got != 0 {
          t.Errorf("OtherUnreadCount with no reader = %d, want 0", got)
      }

      // Reader returns T1, T2, T3 (all have unreads)
      m.SetUnreadReader(func() []string { return []string{"T1", "T2", "T3"} })
      if got := m.OtherUnreadCount("T1"); got != 2 {
          t.Errorf("OtherUnreadCount(T1) = %d, want 2", got)
      }
      if got := m.OtherUnreadCount("T2"); got != 2 {
          t.Errorf("OtherUnreadCount(T2) = %d, want 2", got)
      }
      // Active ID not in the set
      if got := m.OtherUnreadCount("T-missing"); got != 3 {
          t.Errorf("OtherUnreadCount(missing) = %d, want 3", got)
      }
      // Empty active ID treated as "no exclusion" — caller's responsibility
      // to skip the call when there's no active workspace
      if got := m.OtherUnreadCount(""); got != 3 {
          t.Errorf("OtherUnreadCount(empty) = %d, want 3", got)
      }
  }
  ```

- [ ] **Step 3: Run tests**

  ```bash
  go test ./internal/ui/workspace/... -run "TestNameByID|TestOtherUnreadCount" -v
  ```

  Expected: all PASS.

---

### Task 2: Add `IsVisiblyUnread` helper + `UnreadChannelCount` to `sidebar.Model`, refactor `View()` to use the helper

**Files:**
- Modify: `internal/ui/sidebar/model.go`
- Modify: `internal/ui/sidebar/model_test.go`

This task does *two* things deliberately bundled: it (a) extracts the inline "visibly unread" predicate from `View()` into a single helper, and (b) adds the count accessor that uses that same helper. Bundling avoids creating a second call site that re-duplicates the predicate. See plan rationale at top.

- [ ] **Step 1: Add the predicate helper**

  Add as a method on `ChannelItem` (near its struct definition):

  ```go
  // IsVisiblyUnread reports whether this channel should render as having
  // unread messages — i.e., the read-state says HasUnread AND the user
  // hasn't muted the channel. This is the single source of truth for
  // the "unread dot" predicate; both the sidebar View and the count
  // accessor (UnreadChannelCount) MUST use this helper.
  func (item ChannelItem) IsVisiblyUnread(state cache.ReadState) bool {
      return state.HasUnread && !item.IsMuted
  }
  ```

- [ ] **Step 2: Refactor `View()` to use the helper**

  In the `View()` render loop in `internal/ui/sidebar/model.go`, replace:

  ```go
  hasUnread := readState[item.ID].HasUnread && !item.IsMuted
  ```

  With:

  ```go
  hasUnread := item.IsVisiblyUnread(readState[item.ID])
  ```

  Verify no other inline duplicates of this predicate exist by grepping:

  ```bash
  grep -n "HasUnread && !.*IsMuted\|HasUnread && !item.IsMuted" internal/ui/sidebar/*.go
  ```

  Expected after refactor: no matches (or only the helper definition itself).

- [ ] **Step 3: Add `UnreadChannelCount` accessor**

  Place near the existing read-state reader plumbing (search for `readStateReader` in `model.go`):

  ```go
  // UnreadChannelCount returns the number of channels in the sidebar
  // that should render as unread (HasUnread && !IsMuted). Uses the
  // installed read-state reader and the IsVisiblyUnread helper, so the
  // count matches the dot population exactly. Returns 0 if no reader
  // is installed. Used by the App to compute the window title.
  func (m *Model) UnreadChannelCount() int {
      if m.readStateReader == nil {
          return 0
      }
      state := m.readStateReader()
      count := 0
      for _, item := range m.items {
          if item.IsVisiblyUnread(state[item.ID]) {
              count++
          }
      }
      return count
  }
  ```

  Note: confirm the field name `readStateReader` against the existing `SetReadStateReader`; adjust if it differs.

- [ ] **Step 4: Add tests**

  Append to `model_test.go`. Pattern after existing sidebar tests to construct a `Model` with seeded items.

  ```go
  func TestIsVisiblyUnread(t *testing.T) {
      cases := []struct {
          name  string
          item  ChannelItem
          state cache.ReadState
          want  bool
      }{
          {"unread, unmuted", ChannelItem{IsMuted: false}, cache.ReadState{HasUnread: true}, true},
          {"unread, muted", ChannelItem{IsMuted: true}, cache.ReadState{HasUnread: true}, false},
          {"read, unmuted", ChannelItem{IsMuted: false}, cache.ReadState{HasUnread: false}, false},
          {"read, muted", ChannelItem{IsMuted: true}, cache.ReadState{HasUnread: false}, false},
      }
      for _, tt := range cases {
          t.Run(tt.name, func(t *testing.T) {
              if got := tt.item.IsVisiblyUnread(tt.state); got != tt.want {
                  t.Errorf("IsVisiblyUnread = %v, want %v", got, tt.want)
              }
          })
      }
  }

  func TestUnreadChannelCount_NoReader(t *testing.T) {
      m := /* construct empty Model per existing test patterns */
      if got := m.UnreadChannelCount(); got != 0 {
          t.Errorf("UnreadChannelCount with no reader = %d, want 0", got)
      }
  }

  func TestUnreadChannelCount_FiltersMute(t *testing.T) {
      m := /* construct Model */
      m.SetItems([]ChannelItem{
          {ID: "C1", IsMuted: false},
          {ID: "C2", IsMuted: false},
          {ID: "C3", IsMuted: true},
          {ID: "C4", IsMuted: false},
      })
      m.SetReadStateReader(func() map[string]cache.ReadState {
          return map[string]cache.ReadState{
              "C1": {HasUnread: true},  // counted
              "C2": {HasUnread: false}, // skipped (read)
              "C3": {HasUnread: true},  // skipped (muted)
              "C4": {HasUnread: true},  // counted
          }
      })
      if got := m.UnreadChannelCount(); got != 2 {
          t.Errorf("UnreadChannelCount = %d, want 2 (C1, C4)", got)
      }
  }
  ```

  Match constructor/setter names to the existing sidebar test patterns.

- [ ] **Step 5: Run tests**

  ```bash
  go test ./internal/ui/sidebar/... -run "TestIsVisiblyUnread|TestUnreadChannelCount" -v
  go test ./internal/ui/sidebar/... -race
  ```

  Expected: new tests PASS, existing tests still PASS (View refactor must not regress dot rendering).

---

### Task 3: Add pure `formatTitle` + `computeWindowTitle` + tests (TDD)

**Files:**
- Create: `internal/ui/app_title.go`
- Create: `internal/ui/app_title_test.go`

Both functions in this file are pure (no App reference, no I/O). Tests live alongside and run without standing up an `App` instance.

- [ ] **Step 1: Write failing tests first**

  Create `internal/ui/app_title_test.go`:

  ```go
  package ui

  import "testing"

  func TestFormatTitle(t *testing.T) {
      tests := []struct {
          name     string
          initials string
          active   int
          other    int
          want     string
      }{
          {"no unreads", "SW", 0, 0, "slk SW"},
          {"active only", "SW", 3, 0, "slk SW (3)"},
          {"other only", "SW", 0, 1, "slk SW +1"},
          {"both", "SW", 3, 1, "slk SW (3) +1"},
          {"max values", "SW", 99, 99, "slk SW (99) +99"},
          {"empty initials fallback", "?", 0, 0, "slk ?"},
          {"empty initials with unreads", "?", 5, 2, "slk ? (5) +2"},
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              got := formatTitle(tt.initials, tt.active, tt.other)
              if got != tt.want {
                  t.Errorf("formatTitle(%q, %d, %d) = %q, want %q",
                      tt.initials, tt.active, tt.other, got, tt.want)
              }
          })
      }
  }

  func TestComputeWindowTitle(t *testing.T) {
      tests := []struct {
          name          string
          activeTeamID  string
          workspaceName string
          activeUnreads int
          otherUnreads  int
          want          string
      }{
          {"pre-bootstrap (no active workspace)", "", "", 0, 0, "slk"},
          {"pre-bootstrap with stray inputs", "", "Ignored", 99, 99, "slk"},
          {"active workspace, no unreads", "T1", "SWAP", 0, 0, "slk SW"},
          {"active workspace, with unreads", "T1", "SWAP", 3, 0, "slk SW (3)"},
          {"active + other workspaces have unreads", "T1", "SWAP", 3, 2, "slk SW (3) +2"},
          {"only other workspaces have unreads", "T1", "SWAP", 0, 1, "slk SW +1"},
          {"empty workspace name yields fallback initials", "T1", "", 0, 0, "slk ?"},
          {"single-word name uses first 2 chars", "T1", "Home", 1, 0, "slk HO (1)"},
          {"multi-word name uses initials of first 2", "T1", "StratusGrid Eng", 5, 1, "slk SE (5) +1"},
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              got := computeWindowTitle(tt.activeTeamID, tt.workspaceName, tt.activeUnreads, tt.otherUnreads)
              if got != tt.want {
                  t.Errorf("computeWindowTitle(%q,%q,%d,%d) = %q, want %q",
                      tt.activeTeamID, tt.workspaceName, tt.activeUnreads, tt.otherUnreads, got, tt.want)
              }
          })
      }
  }
  ```

- [ ] **Step 2: Verify tests fail to compile**

  ```bash
  go test ./internal/ui/... -run "TestFormatTitle|TestComputeWindowTitle" 2>&1 | head -5
  ```

  Expected: compile error — `undefined: formatTitle`, `undefined: computeWindowTitle`.

- [ ] **Step 3: Implement**

  Create `internal/ui/app_title.go`:

  ```go
  package ui

  import (
      "fmt"

      "github.com/gammons/slk/internal/ui/workspace"
  )

  // computeWindowTitle builds the slk terminal-window-title string from
  // pre-computed inputs. Pure; covered by table-driven tests in
  // app_title_test.go.
  //
  // The caller (App.notifyReadStateChanged) is responsible for sourcing
  // each input from the appropriate collaborator:
  //   - activeTeamID:   App.activeTeamID
  //   - workspaceName:  App.workspaceRail.NameByID(activeTeamID)
  //   - activeUnreads:  App.sidebar.UnreadChannelCount() (mute-filtered)
  //   - otherUnreads:   App.workspaceRail.OtherUnreadCount(activeTeamID)
  //
  // See docs/superpowers/specs/2026-05-21-tab-title-unread-indicator-design.md.
  func computeWindowTitle(activeTeamID, workspaceName string, activeUnreads, otherUnreads int) string {
      if activeTeamID == "" {
          return "slk"
      }
      initials := workspace.WorkspaceInitials(workspaceName)
      return formatTitle(initials, activeUnreads, otherUnreads)
  }

  // formatTitle assembles the final title string. Pure helper for
  // computeWindowTitle; separated so the assembly format is testable
  // independent of input sourcing.
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

- [ ] **Step 4: Run tests**

  ```bash
  go test ./internal/ui/... -run "TestFormatTitle|TestComputeWindowTitle" -v
  ```

  Expected: all cases PASS.

---

### Task 4: Add `windowTitle` field to `App` and initialize it

**Files:**
- Modify: `internal/ui/app.go`

The only state added to `App` is one cached string. No new reader callbacks, no new setters.

- [ ] **Step 1: Add `windowTitle` field**

  In the `App` struct block, alongside the other "current context" fields (next to `activeTeamID`):

  ```go
      // windowTitle is the cached terminal-title string. Computed by
      // notifyReadStateChanged on every read-state change and on
      // workspace switch (which also routes through that hook); read
      // by View() into tea.View.WindowTitle. See
      // docs/superpowers/specs/2026-05-21-tab-title-unread-indicator-design.md.
      windowTitle string
  ```

- [ ] **Step 2: Initialize in the constructor**

  Find the `App` literal in the constructor (search for `workspaceRail:        workspace.New(nil, 0),`). Add a sibling field:

  ```go
      windowTitle: "slk",
  ```

  This is the pre-bootstrap default. The first `notifyReadStateChanged` (from bootstrap's `BatchUpdateChannelReadState`) replaces it with the real value.

- [ ] **Step 3: Build to confirm**

  ```bash
  go build ./...
  ```

  Expected: clean build. (`SetWorkspaceUnreadReader` is unchanged from upstream — single-place install symmetry preserved.)

---

### Task 5: Hook orchestration into `notifyReadStateChanged` and surface in `View()`

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Extend `notifyReadStateChanged` to compute and cache the title**

  Replace the existing body of `notifyReadStateChanged`:

  ```go
  func (a *App) notifyReadStateChanged() {
      a.sidebar.Invalidate()
      a.workspaceRail.RefreshUnreads()
      a.windowTitle = computeWindowTitle(
          a.activeTeamID,
          a.workspaceRail.NameByID(a.activeTeamID),
          a.sidebar.UnreadChannelCount(),
          a.workspaceRail.OtherUnreadCount(a.activeTeamID),
      )
  }
  ```

  Note: `computeWindowTitle` is a free function in the same package, so no receiver and no import path within `ui`.

- [ ] **Step 2: Set `WindowTitle` in `View()`**

  Locate `func (a *App) View() tea.View`. Two return sites need the field set:

  - The early-return for pre-layout (before the terminal size is known): set `v.WindowTitle = a.windowTitle` before `return v`.
  - The main return at end of the function: same. Find the final `tea.View` value being returned (search for `return v` near end of function) and apply the field assignment.

  Example for the early-return branch. The pre-layout fallback is extracted into `renderEarlyFallback()` (see `internal/ui/view_early.go`), so the title is set on the returned view at the call site:

  ```go
      if v, handled := a.renderEarlyFallback(); handled {
          v.WindowTitle = a.windowTitle
          return v
      }
  ```

- [ ] **Step 3: Build**

  ```bash
  go build ./...
  ```

  Expected: clean build.

---

### Task 6: Integration test on `App`

**Files:**
- Modify: `internal/ui/app_test.go` (extend)

The unit-level coverage in Tasks 1–3 already proves the pieces. The integration test here only needs to confirm the orchestration: `notifyReadStateChanged` produces the expected `windowTitle`, and `View()` surfaces it on `tea.View.WindowTitle`.

- [ ] **Step 1: Add an integration test**

  Pattern after existing `app_test.go` tests that construct a real `App` with seeded sub-models and read-state. Cases to cover:

  - `notifyReadStateChanged` populates `a.windowTitle` to the expected string when the active workspace has N unreads and 0/M other workspaces have unreads.
  - Pre-bootstrap (`activeTeamID == ""`) yields `slk`.
  - Muted channels are excluded from the active count (cross-check: same item set passed to sidebar produces both the expected count and the expected dot population).
  - `App.View()` returns a `tea.View` whose `WindowTitle == a.windowTitle` — covered in both the pre-layout fallback and the main branch.

  Most of the per-case format verification is already in `TestComputeWindowTitle`; the integration test's job is to verify the *wiring*, not re-test the formatting.

- [ ] **Step 2: Run all UI tests**

  ```bash
  go test ./internal/ui/... -race
  ```

  Expected: all tests pass, including new ones and unchanged existing ones (especially the sidebar View tests that exercise the refactored predicate).

---

### Task 7: README tmux note

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Find the existing tmux section**

  Search the README for `tmux` — there's already a section documenting Kitty image escapes under tmux (added by commit `acda354` / `30c998a`).

- [ ] **Step 2: Add window-title bullet**

  Add a bullet to the tmux section:

  > **Window title:** slk emits OSC 2 to set the terminal window title on every read-state change. tmux passes these through to your terminal only when `set -g set-titles on` is configured. Add this to your `~/.tmux.conf` if you want the unread indicator to appear in your terminal's tab bar.

---

### Task 8: Final verification

**Files:** (no edits)

- [ ] **Step 1: Full test suite**

  ```bash
  go test ./... -race
  ```

  Expected: all green. Pay special attention to the sidebar tests — the `View()` refactor (Task 2 Step 2) shares its predicate with the new count function; both must continue rendering dots and counting identically.

- [ ] **Step 2: Vet**

  ```bash
  go vet ./...
  ```

  Expected: clean (modulo the pre-existing emoji probe warning).

- [ ] **Step 3: Lint**

  ```bash
  golangci-lint run ./...
  ```

  Expected: clean.

- [ ] **Step 4: Manual smoke test**

  - Launch `slk` against the test workspace.
  - Verify title shows `slk <initials>` (no count) on a workspace with no unreads.
  - Have a teammate (or use the official Slack client) send a message in a non-active channel; verify title updates to `slk <initials> (1)` within ~1s.
  - Mark the channel read in the official client; verify title returns to `slk <initials>`.
  - Mute a channel, then have a message arrive there; verify the title's `(N)` does NOT increment (and the sidebar dot does NOT appear) — both gated by the same `IsVisiblyUnread` predicate.
  - Open `slk --add-workspace` for a second workspace; switch to it; have unread activity on the first; verify title shows `+1`.
  - Under tmux with `set-titles on`: verify the same in the terminal's tab bar.

- [ ] **Step 5: Confirm no regressions in the in-app sidebar dot**

  All read-state changes that update the title also update the sidebar dot. Spot-check that the sidebar renders correctly across each scenario above. (The `View()` refactor in Task 2 should be invisible — the predicate is exactly the same, just sourced via a helper.)

---

## Out of scope for this plan

Per the spec's "Out of scope" section: config knob, DCS-wrapped passthrough for tmux without `set-titles on`, channel-level granularity, workspace-icon glyphs. These are follow-ups, not part of this branch.
