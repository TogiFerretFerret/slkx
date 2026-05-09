# Debug Logging — Cache Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive `[cache]` and `[ws]` logging across the messages-cache reconciliation pipeline so per-channel timelines (cache load → network fetch → SetMessages replace → WS deltas → UpsertMessage) can be reconstructed from `slk-debug.log`.

**Architecture:** Add `debuglog.Cache(...)` calls at every node of the reconciliation graph, plus `debuglog.WS(...)` calls in the slack-events dispatcher for known event types. Two new helper functions in `cmd/slk/main.go` (`summarizeMessages`, `summarizeCachedRows`) collapse a slice of messages into `(count, oldest_ts, newest_ts)` for compact log output.

**Tech Stack:** Go 1.26.1, existing `internal/debuglog` package (built in foundation plan).

**Spec:** `docs/superpowers/specs/2026-05-09-comprehensive-debug-logging-design.md`

**Depends on:** `docs/superpowers/plans/2026-05-09-debug-logging-foundation.md` MUST be merged first.

---

## File Structure

| File | Role |
|---|---|
| `cmd/slk/main.go` | Entry/exit log lines for `loadCachedMessages`, `loadCachedThreadReplies`, `fetchChannelMessages`, `fetchThreadReplies`. Add `summarizeMessages`/`summarizeCachedRows` helpers. Per-message upsert log inside fetcher loops. |
| `internal/cache/messages.go` | `UpsertMessage` and `DeleteMessage` log calls. |
| `internal/ui/messages/model.go` | `SetMessages` and `PrependMessages` log calls (count + oldest/newest TS deltas). |
| `internal/ui/app.go` | `ChannelSelectedMsg`, `MessagesLoadedMsg`, `OlderMessagesLoadedMsg` handler logs. WS-message dispatch logs (NewMessageMsg / MessageEditedMsg / MessageDeletedMsg). |
| `internal/slack/events.go` | Per-event-type `[ws]` log calls for known events (message, message_changed, message_deleted, reactions, channel_marked, etc.). |

---

## Important conventions for the engineer

- **Run from the repo root** `/home/grant/local_code/slk/` (or the worktree the orchestrator created).
- **Test runner:** `go test ./<pkg>/... -run <Name> -v` for targeted, `go test ./...` for full.
- **Build check:** `go build ./...` after every code change.
- **Line numbers drift.** When a step says "around line N", confirm with `grep -n` first.
- **TDD:** every behavior-changing task starts with a failing test. Pure-additive log lines (which don't change observable behavior) are verified via build + manual smoke test rather than unit test, since asserting on log content is brittle and adds little value.
- **Commit after each task.**
- **Log line shape:** keep them short, key=value style, no embedded newlines. Quote string values that may contain spaces.

---

## Task 1: Add `summarizeMessages` and `summarizeCachedRows` helpers

**Files:**
- Modify: `cmd/slk/main.go`
- Create: `cmd/slk/summarize_test.go`

These helpers collapse a slice of messages into a compact `(count, oldest_ts, newest_ts)` triple for log output. Two variants because the two slice types live in different packages: `cache.Message` (raw cache rows) and `messages.MessageItem` (UI-side enriched).

- [ ] **Step 1: Write failing tests**

Create `cmd/slk/summarize_test.go`:

```go
package main

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestSummarizeMessages_Empty(t *testing.T) {
	got := summarizeMessages(nil)
	if got != "count=0" {
		t.Fatalf("nil: got %q", got)
	}
	got = summarizeMessages([]messages.MessageItem{})
	if got != "count=0" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestSummarizeMessages_OldestNewest(t *testing.T) {
	items := []messages.MessageItem{
		{TS: "1700000000.000100"},
		{TS: "1700000001.000200"},
		{TS: "1700000002.000300"},
	}
	got := summarizeMessages(items)
	want := "count=3 oldest=1700000000.000100 newest=1700000002.000300"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSummarizeCachedRows_OldestNewest(t *testing.T) {
	rows := []cache.Message{
		{TS: "1700000000.000100"},
		{TS: "1700000001.000200"},
	}
	got := summarizeCachedRows(rows)
	want := "count=2 oldest=1700000000.000100 newest=1700000001.000200"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
go test ./cmd/slk/ -run TestSummarize -v
```

Expected: FAIL with `undefined: summarizeMessages`.

- [ ] **Step 3: Implement helpers**

Append to `cmd/slk/main.go` (just before `loadCachedMessages` is fine — keep them near the cache-related functions):

```go
// summarizeMessages collapses a slice of messages.MessageItem into a
// compact "count=N oldest=<ts> newest=<ts>" string for [cache] log
// lines. Empty/nil slices return "count=0" with no ts fields. Assumes
// the slice is sorted ascending by TS (the convention everywhere in
// slk's cache and fetch paths).
func summarizeMessages(items []messages.MessageItem) string {
	if len(items) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(items), items[0].TS, items[len(items)-1].TS)
}

// summarizeCachedRows is summarizeMessages's twin for raw cache.Message
// rows (used by loadCachedMessages / loadCachedThreadReplies).
func summarizeCachedRows(rows []cache.Message) string {
	if len(rows) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(rows), rows[0].TS, rows[len(rows)-1].TS)
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
go test ./cmd/slk/ -run TestSummarize -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go cmd/slk/summarize_test.go
git commit -m "main: summarizeMessages/summarizeCachedRows helpers for cache logs"
```

---

## Task 2: Log entry/exit of `loadCachedMessages` and `loadCachedThreadReplies`

**Files:**
- Modify: `cmd/slk/main.go`

Add bracketed entry+exit log lines around each cache-read function so a `[cache]` grep produces a complete read timeline.

- [ ] **Step 1: Add logs to `loadCachedMessages`**

Find:

```go
func loadCachedMessages(
	db *cache.DB,
	selfUserID string,
	channelID string,
	userNames map[string]string,
	tsFormat string,
) []messages.MessageItem {
	if db == nil {
		return nil
	}
	rows, err := db.GetMessages(channelID, 50, "")
	if err != nil {
		debuglog.Cache("loadCachedMessages: GetMessages %s: %v", channelID, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedMessages"))
	}
	return out
}
```

Change to:

```go
func loadCachedMessages(
	db *cache.DB,
	selfUserID string,
	channelID string,
	userNames map[string]string,
	tsFormat string,
) []messages.MessageItem {
	if db == nil {
		debuglog.Cache("loadCachedMessages: channel=%s db=nil", channelID)
		return nil
	}
	debuglog.Cache("loadCachedMessages: channel=%s entry", channelID)
	rows, err := db.GetMessages(channelID, 50, "")
	if err != nil {
		debuglog.Cache("loadCachedMessages: GetMessages %s: %v", channelID, err)
		return nil
	}
	if len(rows) == 0 {
		debuglog.Cache("loadCachedMessages: channel=%s result count=0 (no cached rows)", channelID)
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedMessages"))
	}
	debuglog.Cache("loadCachedMessages: channel=%s result %s", channelID, summarizeMessages(out))
	return out
}
```

- [ ] **Step 2: Add logs to `loadCachedThreadReplies`**

Find:

```go
func loadCachedThreadReplies(
	db *cache.DB,
	selfUserID string,
	channelID, threadTS string,
	userNames map[string]string,
	tsFormat string,
) []messages.MessageItem {
	if db == nil {
		return nil
	}
	rows, err := db.GetThreadReplies(channelID, threadTS)
	if err != nil {
		debuglog.Cache("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedThreadReplies"))
	}
	return out
}
```

Change to:

```go
func loadCachedThreadReplies(
	db *cache.DB,
	selfUserID string,
	channelID, threadTS string,
	userNames map[string]string,
	tsFormat string,
) []messages.MessageItem {
	if db == nil {
		debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s db=nil", channelID, threadTS)
		return nil
	}
	debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s entry", channelID, threadTS)
	rows, err := db.GetThreadReplies(channelID, threadTS)
	if err != nil {
		debuglog.Cache("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}
	if len(rows) == 0 {
		debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s result count=0", channelID, threadTS)
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedThreadReplies"))
	}
	debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s result %s",
		channelID, threadTS, summarizeMessages(out))
	return out
}
```

- [ ] **Step 3: Build and test**

```bash
go build ./... && go test ./cmd/slk/... ./internal/...
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "main: log loadCachedMessages/loadCachedThreadReplies entry+exit"
```

---

## Task 3: Log entry/exit of `fetchChannelMessages` and `fetchThreadReplies`

**Files:**
- Modify: `cmd/slk/main.go`

Add timing-aware entry/exit logs and per-message upsert detail. The nil-vs-empty contract is logged so reconciliation can be reasoned about.

- [ ] **Step 1: Update `fetchChannelMessages`**

Find:

```go
func fetchChannelMessages(client *slackclient.Client, channelID string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	history, err := client.GetHistory(ctx, channelID, 50, "")
	if err != nil {
		debuglog.Cache("fetchChannelMessages: GetHistory %s: %v", channelID, err)
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
```

Change to:

```go
func fetchChannelMessages(client *slackclient.Client, channelID string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchChannelMessages: channel=%s entry", channelID)
	start := time.Now()
	history, err := client.GetHistory(ctx, channelID, 50, "")
	if err != nil {
		debuglog.Cache("fetchChannelMessages: GetHistory %s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("fetchChannelMessages: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
```

Find the end of `fetchChannelMessages`:

```go
	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
}
```

Change to:

```go
	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	debuglog.Cache("fetchChannelMessages: channel=%s result %s dur_ms=%d (authoritative replace)",
		channelID, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	return msgItems
}
```

- [ ] **Step 2: Update `fetchThreadReplies`**

Find:

```go
func fetchThreadReplies(client *slackclient.Client, channelID, threadTS string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		debuglog.Cache("fetchThreadReplies: GetReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		db.UpsertMessage(cache.Message{
```

Change to:

```go
func fetchThreadReplies(client *slackclient.Client, channelID, threadTS string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchThreadReplies: channel=%s thread_ts=%s entry", channelID, threadTS)
	start := time.Now()
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		debuglog.Cache("fetchThreadReplies: GetReplies %s/%s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, threadTS, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("fetchThreadReplies: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		db.UpsertMessage(cache.Message{
```

Find the end of `fetchThreadReplies` (look for the last `return msgItems` in that function):

```go
	return msgItems
}
```

Change the last line of the function body. The exit-log line should immediately precede the final `return msgItems`. Run `grep -n 'fetchThreadReplies\|return msgItems' cmd/slk/main.go` to find the exact location, then change:

```go
	return msgItems
}
```

at the end of `fetchThreadReplies` to:

```go
	debuglog.Cache("fetchThreadReplies: channel=%s thread_ts=%s result %s dur_ms=%d (authoritative replace)",
		channelID, threadTS, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	return msgItems
}
```

(Be careful: there are two `return msgItems` lines in this file — one in `fetchChannelMessages`, one in `fetchThreadReplies`. The final one is in `fetchThreadReplies` since `fetchThreadReplies` is defined after `fetchChannelMessages` in `main.go`.)

- [ ] **Step 3: Verify `time` is already imported**

`time` should already be in the import block (it's used in many other places). Run `grep -n '"time"' cmd/slk/main.go`. If for some reason it isn't, add it.

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/slk/main.go
git commit -m "main: log fetchChannelMessages/fetchThreadReplies entry+exit+upserts"
```

---

## Task 4: Log `cache.UpsertMessage` and `cache.DeleteMessage`

**Files:**
- Modify: `internal/cache/messages.go`

The lower-level upsert log fires from ALL code paths (fetch loops, WS handlers, optimistic local sends). This is the canonical "did this message hit the cache?" line.

- [ ] **Step 1: Add log to `UpsertMessage`**

Find:

```go
func (db *DB) UpsertMessage(m Message) error {
	_, err := db.conn.Exec(`
		INSERT INTO messages (ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts, channel_id) DO UPDATE SET
			user_id=excluded.user_id,
			text=excluded.text,
			thread_ts=excluded.thread_ts,
			reply_count=excluded.reply_count,
			edited_at=excluded.edited_at,
			is_deleted=excluded.is_deleted,
			raw_json=excluded.raw_json,
			subtype=excluded.subtype
	`, m.TS, m.ChannelID, m.WorkspaceID, m.UserID, m.Text, m.ThreadTS,
		m.ReplyCount, m.EditedAt, boolToInt(m.IsDeleted), m.RawJSON, m.CreatedAt, m.Subtype)
	if err != nil {
		return fmt.Errorf("upserting message: %w", err)
	}
	return nil
}
```

Change to:

```go
func (db *DB) UpsertMessage(m Message) error {
	_, err := db.conn.Exec(`
		INSERT INTO messages (ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at, subtype)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts, channel_id) DO UPDATE SET
			user_id=excluded.user_id,
			text=excluded.text,
			thread_ts=excluded.thread_ts,
			reply_count=excluded.reply_count,
			edited_at=excluded.edited_at,
			is_deleted=excluded.is_deleted,
			raw_json=excluded.raw_json,
			subtype=excluded.subtype
	`, m.TS, m.ChannelID, m.WorkspaceID, m.UserID, m.Text, m.ThreadTS,
		m.ReplyCount, m.EditedAt, boolToInt(m.IsDeleted), m.RawJSON, m.CreatedAt, m.Subtype)
	if err != nil {
		debuglog.Cache("UpsertMessage: channel=%s ts=%s ERR=%v", m.ChannelID, m.TS, err)
		return fmt.Errorf("upserting message: %w", err)
	}
	debuglog.Cache("UpsertMessage: channel=%s ts=%s thread_ts=%s subtype=%q deleted=%v edited=%q",
		m.ChannelID, m.TS, m.ThreadTS, m.Subtype, m.IsDeleted, m.EditedAt)
	return nil
}
```

- [ ] **Step 2: Add log to `DeleteMessage`**

Find:

```go
func (db *DB) DeleteMessage(channelID, ts string) error {
	_, err := db.conn.Exec(`UPDATE messages SET is_deleted = 1 WHERE channel_id = ? AND ts = ?`, channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}
```

Change to:

```go
func (db *DB) DeleteMessage(channelID, ts string) error {
	_, err := db.conn.Exec(`UPDATE messages SET is_deleted = 1 WHERE channel_id = ? AND ts = ?`, channelID, ts)
	if err != nil {
		debuglog.Cache("DeleteMessage: channel=%s ts=%s ERR=%v", channelID, ts, err)
		return fmt.Errorf("deleting message: %w", err)
	}
	debuglog.Cache("DeleteMessage: channel=%s ts=%s", channelID, ts)
	return nil
}
```

- [ ] **Step 3: Update imports in `internal/cache/messages.go`**

The current import is `import "fmt"`. Change to:

```go
import (
	"fmt"

	"github.com/gammons/slk/internal/debuglog"
)
```

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./internal/cache/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cache/messages.go
git commit -m "cache: log UpsertMessage and DeleteMessage"
```

---

## Task 5: Log `messages.Model.SetMessages` and `PrependMessages`

**Files:**
- Modify: `internal/ui/messages/model.go`

The UI-side authoritative replace and the older-history prepend. Logging the before/after summary makes "the cache replaced N with M" timelines obvious.

- [ ] **Step 1: Find existing imports in model.go**

```bash
grep -n '^import\|"github.com/gammons' internal/ui/messages/model.go | head -20
```

The import block already exists. Note its location.

- [ ] **Step 2: Add `debuglog` import**

In `internal/ui/messages/model.go`, find the existing import block. Add `"github.com/gammons/slk/internal/debuglog"` to the project-imports group (where other `github.com/gammons/slk/...` paths are). If the layout uses a single group, add it alphabetically.

If you're not sure where it goes, run `grep -n '"github.com/gammons/slk' internal/ui/messages/model.go` to see the existing pattern.

- [ ] **Step 3: Add log to `SetMessages`**

Find:

```go
func (m *Model) SetMessages(msgs []MessageItem) {
	m.messages = msgs
	m.ClearSelection()
	m.cache = nil // invalidate cache
	// Force the next View() to re-snap yOffset to the new selection -- without
	// this, switching to a channel that happens to have the same selected
	// index as the previous channel would leave yOffset at its old value.
	m.hasSnapped = false
	m.dirty()

	if len(msgs) == 0 {
		m.selected = 0
		return
	}
	// Start at the bottom -- newest messages visible
	m.selected = len(msgs) - 1
}
```

Change to:

```go
func (m *Model) SetMessages(msgs []MessageItem) {
	if debuglog.Enabled() {
		oldSummary := summarizeMessageItems(m.messages)
		newSummary := summarizeMessageItems(msgs)
		debuglog.Cache("messages.Model.SetMessages: channel=%q before=[%s] after=[%s]",
			m.channelName, oldSummary, newSummary)
	}
	m.messages = msgs
	m.ClearSelection()
	m.cache = nil // invalidate cache
	// Force the next View() to re-snap yOffset to the new selection -- without
	// this, switching to a channel that happens to have the same selected
	// index as the previous channel would leave yOffset at its old value.
	m.hasSnapped = false
	m.dirty()

	if len(msgs) == 0 {
		m.selected = 0
		return
	}
	// Start at the bottom -- newest messages visible
	m.selected = len(msgs) - 1
}
```

- [ ] **Step 4: Add log to `PrependMessages`**

Find:

```go
func (m *Model) PrependMessages(msgs []MessageItem) {
	if len(msgs) == 0 {
		return
	}
	count := len(msgs)
	m.messages = append(msgs, m.messages...)
	m.selected += count
	m.cache = nil // invalidate cache
	m.dirty()
}
```

Change to:

```go
func (m *Model) PrependMessages(msgs []MessageItem) {
	if len(msgs) == 0 {
		return
	}
	count := len(msgs)
	if debuglog.Enabled() {
		debuglog.Cache("messages.Model.PrependMessages: channel=%q count_before=%d count_added=%d added=[%s]",
			m.channelName, len(m.messages), count, summarizeMessageItems(msgs))
	}
	m.messages = append(msgs, m.messages...)
	m.selected += count
	m.cache = nil // invalidate cache
	m.dirty()
}
```

- [ ] **Step 5: Add the local helper `summarizeMessageItems`**

The model package can't import `cmd/slk` (cyclic), so add a local copy of the summarize helper. Append to `internal/ui/messages/model.go` (file-end is fine; it's a private helper):

```go
// summarizeMessageItems collapses a slice into a compact
// "count=N oldest=<ts> newest=<ts>" string for [cache] log lines.
// Empty/nil slices return "count=0". Mirrors summarizeMessages in
// cmd/slk/main.go but lives here to avoid a circular import.
func summarizeMessageItems(items []MessageItem) string {
	if len(items) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(items), items[0].TS, items[len(items)-1].TS)
}
```

If `fmt` isn't imported in `model.go`, add it. Run `grep -n '"fmt"' internal/ui/messages/model.go` to check.

- [ ] **Step 6: Build and test**

```bash
go build ./... && go test ./internal/ui/messages/... -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/messages/model.go
git commit -m "messages: log SetMessages and PrependMessages with TS deltas"
```

---

## Task 6: Log `ChannelSelectedMsg`, `MessagesLoadedMsg`, `OlderMessagesLoadedMsg` handlers

**Files:**
- Modify: `internal/ui/app.go`

These three handlers are the reconciliation chokepoints. The `MessagesLoadedMsg` log line distinguishes the three contract branches (nil → keep cache, [] → empty replace, non-empty → full replace).

- [ ] **Step 1: Add `debuglog` import**

In `internal/ui/app.go`, find the existing import block. Add `"github.com/gammons/slk/internal/debuglog"` in the project-imports group.

```bash
grep -n '"github.com/gammons/slk' internal/ui/app.go | head -5
```

- [ ] **Step 2: Add log to `ChannelSelectedMsg` handler**

Find (around line 1340-1370):

```go
	case ChannelSelectedMsg:
```

Locate the start of the case block. There's a lot of body; find the section that does cache-first render:

```go
		var cached []messages.MessageItem
		if a.channelCacheReader != nil {
			cached = a.channelCacheReader(msg.ID)
		}
		if len(cached) > 0 {
			a.messagepane.SetLoading(false)
			a.messagepane.SetMessages(cached)
		} else {
			a.messagepane.SetLoading(true)
			a.messagepane.SetMessages(nil) // clear while loading
			cmds = append(cmds, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			}))
		}
```

Change to:

```go
		var cached []messages.MessageItem
		if a.channelCacheReader != nil {
			cached = a.channelCacheReader(msg.ID)
		}
		debuglog.Cache("ChannelSelectedMsg: channel=%s name=%q cache_hit_count=%d",
			msg.ID, msg.Name, len(cached))
		if len(cached) > 0 {
			a.messagepane.SetLoading(false)
			a.messagepane.SetMessages(cached)
		} else {
			a.messagepane.SetLoading(true)
			a.messagepane.SetMessages(nil) // clear while loading
			cmds = append(cmds, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			}))
		}
```

Find the network-fetch trigger a few lines below:

```go
		// Always fetch fresh from the network in the background; the
		// cached render is best-effort.
		if a.channelFetcher != nil {
			fetcher := a.channelFetcher
			chID, chName := msg.ID, msg.Name
			cmds = append(cmds, func() tea.Msg {
				return fetcher(chID, chName)
			})
		}
```

Change the `if a.channelFetcher != nil` block to:

```go
		// Always fetch fresh from the network in the background; the
		// cached render is best-effort.
		if a.channelFetcher != nil {
			fetcher := a.channelFetcher
			chID, chName := msg.ID, msg.Name
			debuglog.Cache("ChannelSelectedMsg: channel=%s firing background network fetch", msg.ID)
			cmds = append(cmds, func() tea.Msg {
				return fetcher(chID, chName)
			})
		} else {
			debuglog.Cache("ChannelSelectedMsg: channel=%s no channelFetcher wired", msg.ID)
		}
```

- [ ] **Step 3: Add log to `MessagesLoadedMsg` handler**

Find:

```go
	case MessagesLoadedMsg:
		if msg.ChannelID == a.activeChannelID {
			a.messagepane.SetLoading(false)
			a.messagepane.SetLastReadTS(msg.LastReadTS)
			// nil Messages from the fetcher signals network FAILURE, not an
			// empty channel (empty channels return []messages.MessageItem{}).
			// On failure, preserve whatever the cache already rendered so a
			// transient blip doesn't blank a working view. The Slack-side
			// fetcher logs the error before returning nil.
			if msg.Messages != nil {
				a.messagepane.SetMessages(msg.Messages)
			}
		}
```

Change to:

```go
	case MessagesLoadedMsg:
		// Distinguish the three cases of the fetcher's nil-vs-[] contract:
		//   nil      → network failure, keep cached render
		//   []       → channel is genuinely empty, replace with empty
		//   non-empty → authoritative replace
		var kind string
		switch {
		case msg.Messages == nil:
			kind = "nil_keep_cache"
		case len(msg.Messages) == 0:
			kind = "empty_replace"
		default:
			kind = "full_replace"
		}
		debuglog.Cache("MessagesLoadedMsg: channel=%s active=%s kind=%s count=%d",
			msg.ChannelID, a.activeChannelID, kind, len(msg.Messages))
		if msg.ChannelID == a.activeChannelID {
			a.messagepane.SetLoading(false)
			a.messagepane.SetLastReadTS(msg.LastReadTS)
			if msg.Messages != nil {
				a.messagepane.SetMessages(msg.Messages)
			}
		}
```

- [ ] **Step 4: Add log to `OlderMessagesLoadedMsg` handler**

Find:

```go
	case OlderMessagesLoadedMsg:
		if msg.ChannelID == a.activeChannelID {
			a.fetchingOlder = false
			a.messagepane.SetLoading(false)
			a.messagepane.PrependMessages(msg.Messages)
		}
```

Change to:

```go
	case OlderMessagesLoadedMsg:
		debuglog.Cache("OlderMessagesLoadedMsg: channel=%s active=%s count=%d",
			msg.ChannelID, a.activeChannelID, len(msg.Messages))
		if msg.ChannelID == a.activeChannelID {
			a.fetchingOlder = false
			a.messagepane.SetLoading(false)
			a.messagepane.PrependMessages(msg.Messages)
		}
```

- [ ] **Step 5: Build and test**

```bash
go build ./... && go test ./internal/ui/...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "app: log ChannelSelected/MessagesLoaded/OlderMessagesLoaded handlers"
```

---

## Task 7: Log known-event types in `internal/slack/events.go`

**Files:**
- Modify: `internal/slack/events.go`

The unknown-event dump already moved to `debuglog.WS` in the foundation plan. This task adds `[ws]` log lines for the known events that drive cache reconciliation.

- [ ] **Step 1: Add log lines in `dispatchWebSocketEvent`**

Open `internal/slack/events.go` and locate `func dispatchWebSocketEvent`. Add log lines at the START of each relevant case. Modify these cases:

For `case "message":` block:

```go
	case "message":
		var msg wsMessageEvent
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		switch msg.SubType {
		case "", "bot_message", "thread_broadcast", "file_share":
			// thread_broadcast is a thread reply that the author also
			// posted to the main channel; render it like a regular
			// message but with the subtype preserved so the UI can
			// label it. file_share is a regular message that has one
			// or more files attached (Slack's V2 upload flow uses
			// this subtype).
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, msg.SubType, false, msg.Files, msg.Blocks, msg.Attachments)
		case "message_changed":
			if msg.Message != nil {
				handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, "", true, msg.Message.Files, msg.Message.Blocks, msg.Message.Attachments)
			}
		case "message_deleted":
			handler.OnMessageDeleted(msg.Channel, msg.DeletedTS)
		}
```

Change to:

```go
	case "message":
		var msg wsMessageEvent
		if err := json.Unmarshal(data, &msg); err != nil {
			return
		}
		switch msg.SubType {
		case "", "bot_message", "thread_broadcast", "file_share":
			debuglog.WS("message: channel=%s user=%s ts=%s subtype=%q thread_ts=%s files=%d",
				msg.Channel, msg.User, msg.TS, msg.SubType, msg.ThreadTS, len(msg.Files))
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, msg.SubType, false, msg.Files, msg.Blocks, msg.Attachments)
		case "message_changed":
			if msg.Message != nil {
				debuglog.WS("message_changed: channel=%s user=%s ts=%s thread_ts=%s edited=true",
					msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.ThreadTS)
				handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, "", true, msg.Message.Files, msg.Message.Blocks, msg.Message.Attachments)
			}
		case "message_deleted":
			debuglog.WS("message_deleted: channel=%s deleted_ts=%s", msg.Channel, msg.DeletedTS)
			handler.OnMessageDeleted(msg.Channel, msg.DeletedTS)
		}
```

For `case "reaction_added":`:

```go
	case "reaction_added":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnReactionAdded(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
```

Change to:

```go
	case "reaction_added":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("reaction_added: channel=%s ts=%s user=%s emoji=%q",
			evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
		handler.OnReactionAdded(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
```

For `case "reaction_removed":`:

```go
	case "reaction_removed":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnReactionRemoved(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
```

Change to:

```go
	case "reaction_removed":
		var evt wsReactionEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("reaction_removed: channel=%s ts=%s user=%s emoji=%q",
			evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
		handler.OnReactionRemoved(evt.Item.Channel, evt.Item.TS, evt.User, evt.Reaction)
```

For the channel-marked group:

```go
	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var evt wsChannelMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		handler.OnChannelMarked(evt.Channel, evt.TS, evt.UnreadCountDisplay)
```

Change to:

```go
	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var evt wsChannelMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		debuglog.WS("%s: channel=%s ts=%s unread_count=%d",
			evt.Type, evt.Channel, evt.TS, evt.UnreadCountDisplay)
		handler.OnChannelMarked(evt.Channel, evt.TS, evt.UnreadCountDisplay)
```

For `case "thread_marked":`:

```go
	case "thread_marked":
		var evt wsThreadMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		// active=true means subscribed-for-unread, i.e. the thread is
		// now unread. Invert for the read flag we hand to the handler.
		read := !evt.Subscription.Active
		handler.OnThreadMarked(evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)
```

Change to:

```go
	case "thread_marked":
		var evt wsThreadMarkedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		// active=true means subscribed-for-unread, i.e. the thread is
		// now unread. Invert for the read flag we hand to the handler.
		read := !evt.Subscription.Active
		debuglog.WS("thread_marked: channel=%s thread_ts=%s last_read=%s read=%v",
			evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)
		handler.OnThreadMarked(evt.Subscription.Channel, evt.Subscription.ThreadTS, evt.Subscription.LastRead, read)
```

For `case "hello":`:

```go
	case "hello":
		handler.OnConnect()
```

Change to:

```go
	case "hello":
		debuglog.WS("hello: connected")
		handler.OnConnect()
```

(Leave the other cases — `presence_change`, `manual_presence_change`, `dnd_*`, `mpim_open`, etc. — alone for now. They can be added later if reconciliation evidence points there.)

- [ ] **Step 2: Build and test**

```bash
go build ./... && go test ./internal/slack/... -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/slack/events.go
git commit -m "slack/events: log known WS events as [ws] lines"
```

---

## Task 8: Log WS-message UI dispatch in `internal/ui/app.go`

**Files:**
- Modify: `internal/ui/app.go`

When a WS event arrives, the rtm event handler in `cmd/slk/main.go` translates it to a bubbletea message via `program.Send`. New messages and edits both flow through `NewMessageMsg{ChannelID, Message}` (where `Message.TS` and `Message.ThreadTS` carry the message-level TS values). Deletes flow through `WSMessageDeletedMsg{ChannelID, TS}`.

Note: the similarly-named `MessageEditedMsg` and `MessageDeletedMsg` types are NOT the WS-driven handlers — they carry the result of slk's own outgoing `chat.update` / `chat.delete` API calls. Logging them would be useful for tracking optimistic local sends, but it's out of scope for this plan (which is about reconciliation drift driven by WS events). Leave them alone.

- [ ] **Step 1: Locate the WS-message handlers**

```bash
grep -n 'case NewMessageMsg\|case WSMessageDeletedMsg' internal/ui/app.go
```

Expected: two case blocks, around lines 1486 and 1815.

- [ ] **Step 2: Add a log line at the top of `NewMessageMsg`**

Find:

```go
	case NewMessageMsg:
```

Locate the start of the case body (the line immediately after the `case` line). Insert a `debuglog.Cache(...)` call as the first line of the case body:

```go
	case NewMessageMsg:
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s thread_ts=%s active=%s",
			msg.ChannelID, msg.Message.TS, msg.Message.ThreadTS, a.activeChannelID)
		// ... existing body ...
```

(Don't modify the rest of the case body. Just prepend the log line.)

- [ ] **Step 3: Add a log line at the top of `WSMessageDeletedMsg`**

Find:

```go
	case WSMessageDeletedMsg:
```

Insert a log line as the first line of the case body:

```go
	case WSMessageDeletedMsg:
		debuglog.Cache("WSMessageDeletedMsg: channel=%s ts=%s active=%s",
			msg.ChannelID, msg.TS, a.activeChannelID)
		// ... existing body ...
```

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./internal/ui/...
```

Expected: PASS. Tests in `internal/ui/app_test.go` (e.g. `TestWSMessageDeletedMsg_*`) exercise these paths — they should still pass since the log calls are pure additive observation.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go
git commit -m "app: log WS-message UI dispatch (NewMessageMsg + WSMessageDeletedMsg)"
```

---

## Task 9: Manual end-to-end verification

**Files:** None (verification only)

Confirm the cache logging surface produces a coherent timeline.

- [ ] **Step 1: Build and run with `SLK_DEBUG=1`**

```bash
go build -o /tmp/slk-cache-verify ./cmd/slk
mkdir -p /tmp/slk-cache-verify-dir
cd /tmp/slk-cache-verify-dir
SLK_DEBUG=1 /tmp/slk-cache-verify
```

Inside slk, switch to a channel you know has recent messages, wait a moment for messages to load, then quit (`Ctrl+C` or `q` in normal mode).

- [ ] **Step 2: Inspect the log**

```bash
grep '\[cache\]' /tmp/slk-cache-verify-dir/slk-debug.log | head -40
grep '\[ws\]' /tmp/slk-cache-verify-dir/slk-debug.log | head -20
```

Expected: a coherent timeline. For one channel, you should see (in order):

1. `ChannelSelectedMsg: channel=Cxxx` with cache_hit_count
2. `loadCachedMessages: channel=Cxxx entry` (only if cache was hit)
3. `loadCachedMessages: channel=Cxxx result count=N oldest=... newest=...`
4. `messages.Model.SetMessages: channel=...` (cache paint)
5. `ChannelSelectedMsg: channel=Cxxx firing background network fetch`
6. `fetchChannelMessages: channel=Cxxx entry`
7. Multiple `UpsertMessage: channel=Cxxx ts=...` lines
8. `fetchChannelMessages: channel=Cxxx result count=N oldest=... newest=... dur_ms=X`
9. `MessagesLoadedMsg: channel=Cxxx kind=full_replace count=N`
10. `messages.Model.SetMessages: channel=... before=[...] after=[...]`

If you see a `[ws] message:` event come in while the channel is open, you should see a follow-up `UpsertMessage:` line.

- [ ] **Step 3: Document any gaps**

If the timeline is missing a step, the call site was missed. Either go back and add it (as a new task), or note the gap as out-of-scope.

- [ ] **Step 4: No commit needed**

If everything checks out, this plan is complete.

---

## Self-review checklist (run before declaring done)

- [ ] All call sites in spec section "Logging surface — focus area 1" are covered.
- [ ] No log lines accidentally hardcode `[cache]` or `[ws]` inside the format string (the prefix should come ONLY from `debuglog.Cache` / `debuglog.WS`).
- [ ] No `log.Printf` references newly introduced — all new logs use `debuglog`.
- [ ] `go test ./...` passes.
- [ ] `go build ./...` clean.
- [ ] Manual smoke test produced a coherent per-channel timeline.
