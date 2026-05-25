# New Message Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `Ctrl+N` modal picker that lets the user start a DM (1 recipient) or group DM / MPIM (2–8 recipients) via Slack's `conversations.open` API, then switches to the opened channel and focuses the compose box.

**Architecture:** New `internal/ui/newmessagepicker` package mirrors the existing `channelfinder` pattern (centered modal overlay, fuzzy filter, scrollbar). Selection state holds a multi-select pill bar; submit dispatches a new `ChannelService.OpenConversation` method that wraps slack-go's `OpenConversationContext`. The reducer tracks an in-flight `RequestID` so an Esc during the network round-trip abandons the result cleanly.

**Tech Stack:** Go 1.26, charm.land/bubbletea v2, charm.land/lipgloss v2, github.com/slack-go/slack v0.23.0, standard `go test`.

**Spec:** `docs/superpowers/specs/2026-05-25-new-message-feature-design.md`

---

## Task Files

Each task file is self-contained — exact paths, full code, expected test output, commit message. Tasks build bottom-up; later tasks depend on the surfaces created by earlier ones.

| # | Task | File |
|---|---|---|
| 1 | Slack client: `OpenConversation` wrapper + mock | [tasks/task-01-slack-client.md](2026-05-25-new-message-feature/task-01-slack-client.md) |
| 2 | `newmessagepicker` package skeleton (`Model`, `User`, `Open/Close/IsVisible`) | [tasks/task-02-picker-skeleton.md](2026-05-25-new-message-feature/task-02-picker-skeleton.md) |
| 3 | Filter & rank pure functions | [tasks/task-03-picker-filter.md](2026-05-25-new-message-feature/task-03-picker-filter.md) |
| 4 | Navigation + multi-select toggle (Space/Tab) + MPIM cap | [tasks/task-04-picker-selection.md](2026-05-25-new-message-feature/task-04-picker-selection.md) |
| 5 | Submit & cancel semantics (`Result`, Enter/Esc/Backspace-at-col-0) | [tasks/task-05-picker-submit.md](2026-05-25-new-message-feature/task-05-picker-submit.md) |
| 6 | Picker view rendering (`View`, `ViewOverlay`) | [tasks/task-06-picker-view.md](2026-05-25-new-message-feature/task-06-picker-view.md) |
| 7 | Mode constant + message types + Ctrl+N keybind | [tasks/task-07-mode-and-msgs.md](2026-05-25-new-message-feature/task-07-mode-and-msgs.md) |
| 8 | `ChannelService.OpenConversation` interface + adapter | [tasks/task-08-channel-service.md](2026-05-25-new-message-feature/task-08-channel-service.md) |
| 9 | App wiring: picker field, mode handler, overlay composite | [tasks/task-09-app-wiring.md](2026-05-25-new-message-feature/task-09-app-wiring.md) |
| 10 | User list + recency snapshot on modal open | [tasks/task-10-user-list-recency.md](2026-05-25-new-message-feature/task-10-user-list-recency.md) |
| 11 | Reducer: submit dispatch + in-flight tracking + cancel | [tasks/task-11-reducer.md](2026-05-25-new-message-feature/task-11-reducer.md) |
| 12 | Cache hydration on `AlreadyOpen=false` + post-open `ChannelSelectedMsg` + `ModeInsert` | [tasks/task-12-cache-hydration.md](2026-05-25-new-message-feature/task-12-cache-hydration.md) |
| 13 | `cmd/slk/main.go`: production `OpenConversation` closure | [tasks/task-13-main-wiring.md](2026-05-25-new-message-feature/task-13-main-wiring.md) |
| 14 | End-to-end test + manual verification + final commit | [tasks/task-14-e2e.md](2026-05-25-new-message-feature/task-14-e2e.md) |

## Conventions used in every task file

- **Files** block lists exact paths the task touches.
- Every step shows exact code or exact command + expected output.
- Tests are written FIRST, run to confirm they fail, then the implementation makes them pass.
- Each task ends with a commit step.
- Type names, function signatures, and field names are consistent across tasks. The canonical names are in [naming.md](2026-05-25-new-message-feature/naming.md).

## Execution

Once a task is complete (tests pass + commit landed), proceed to the next task. Do not skip tasks — later tasks assume the earlier surfaces exist with the exact signatures defined.

After Task 14, the feature is shippable. No follow-up tasks are required; any improvements (MPIM-derived recency, user-list refresh while modal open, etc.) are out of scope for this plan per the spec's Non-Goals section.
