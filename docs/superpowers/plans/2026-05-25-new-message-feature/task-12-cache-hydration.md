# Task 12: Cache hydration on `AlreadyOpen=false`

**Goal:** When `conversations.open` returns a freshly-created channel (`AlreadyOpen=false`), insert a minimal record into the sidebar / channel cache before emitting `ChannelSelectedMsg`. Otherwise the channel-switch path can blow up on a missing record.

**Files:**
- Modify: `internal/ui/reducer_new_message.go`
- Modify: `internal/ui/reducer_new_message_test.go`

---

> **What "minimal record" means:** insert a `sidebar.ChannelItem` with `ID`, `Type` (`dm` if 1 user, `group_dm` otherwise), `Section` = the empty string (so the sidebar's default DM-section logic in `sectionForLegacy` buckets it), and `Name` derived from the workspace's `UserNames` map (joined display names for MPIM; the single peer's display name for DM). Members live in the membership manager, not on `ChannelItem`, so we don't add them here — the existing `membership.Manager` will hydrate on first need.

- [ ] **Step 1: Use the existing sidebar API**

The sidebar model already has the methods we need:

- `UpsertItem(item ChannelItem)` at `internal/ui/sidebar/model.go:625` — appends a new channel or updates an existing one by ID.
- `AllItems() []ChannelItem` at `internal/ui/sidebar/model.go:650` — returns the full channel slice (used in tests below).

No sidebar changes are needed in this task.

- [ ] **Step 2: Add failing tests for the hydration arm**

Append to `internal/ui/reducer_new_message_test.go`:

```go
func TestReducer_NewMessageOpenedMsg_AlreadyOpenSkipsCacheInsert(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 1
	priorCount := len(app.sidebar.AllItems())

	_, _ = app.Update(NewMessageOpenedMsg{
		ChannelID:   "D456",
		AlreadyOpen: true,
		UserIDs:     []string{"U1"},
		RequestID:   1,
	})

	if got := len(app.sidebar.AllItems()); got != priorCount {
		t.Errorf("expected no sidebar mutation for AlreadyOpen=true, count went from %d to %d", priorCount, got)
	}
}

func TestReducer_NewMessageOpenedMsg_NewChannelInsertedAsDM(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 1

	_, _ = app.Update(NewMessageOpenedMsg{
		ChannelID:   "D789",
		AlreadyOpen: false,
		UserIDs:     []string{"U1"},
		RequestID:   1,
	})

	found := false
	for _, ch := range app.sidebar.AllItems() {
		if ch.ID == "D789" {
			found = true
			if ch.Type != "dm" {
				t.Errorf("expected Type=dm, got %s", ch.Type)
			}
			if ch.DMUserID != "U1" {
				t.Errorf("expected DMUserID=U1, got %s", ch.DMUserID)
			}
		}
	}
	if !found {
		t.Error("expected D789 inserted into sidebar")
	}
}

func TestReducer_NewMessageOpenedMsg_NewChannelInsertedAsGroupDM(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 1

	_, _ = app.Update(NewMessageOpenedMsg{
		ChannelID:   "G999",
		AlreadyOpen: false,
		UserIDs:     []string{"U1", "U2"},
		RequestID:   1,
	})

	found := false
	for _, ch := range app.sidebar.AllItems() {
		if ch.ID == "G999" {
			found = true
			if ch.Type != "group_dm" {
				t.Errorf("expected Type=group_dm, got %s", ch.Type)
			}
		}
	}
	if !found {
		t.Error("expected G999 inserted into sidebar")
	}
}
```

- [ ] **Step 3: Run to confirm tests fail**

```
go test ./internal/ui/ -run TestReducer_NewMessageOpenedMsg_ -v
```

Expected: the two `_NewChannelInserted*` tests FAIL.

- [ ] **Step 4: Add hydration to the reducer**

In `internal/ui/reducer_new_message.go`, replace the `NewMessageOpenedMsg` case body with:

```go
	case NewMessageOpenedMsg:
		if !newMessageResultIsCurrent(a, m.RequestID) {
			debuglog.Printf("new-message: dropping stale/cancelled NewMessageOpenedMsg req=%d inflight=%d cancelled=%v", m.RequestID, a.newMessageInFlightID, a.newMessageCancelled)
			return nil, true
		}
		a.newMessagePicker.Close()
		a.newMessageInFlightID = 0
		a.newMessageCancelled = false
		a.SetMode(ModeInsert)

		channelType := "dm"
		if len(m.UserIDs) > 1 {
			channelType = "group_dm"
		}
		if !m.AlreadyOpen {
			a.hydrateNewConversation(m.ChannelID, channelType, m.UserIDs)
		}

		channelID := m.ChannelID
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: channelID, Type: channelType}
		}, true
```

Then add the helper at the bottom of the file:

```go
// hydrateNewConversation inserts a minimal sidebar.ChannelItem for a
// freshly-opened DM or MPIM so the channel-switch path has a record
// to render against. The full channel metadata is filled in by the
// existing RTM event handlers (mpim_open / im_created) and the
// membership.Manager.
func (a *App) hydrateNewConversation(channelID, channelType string, userIDs []string) {
	item := sidebar.ChannelItem{
		ID:   channelID,
		Type: channelType,
		Name: a.deriveConversationName(channelType, userIDs),
	}
	if channelType == "dm" && len(userIDs) == 1 {
		item.DMUserID = userIDs[0]
	}
	a.sidebar.UpsertItem(item)
}

// deriveConversationName builds a display name for a freshly-opened
// DM/MPIM. For DMs we use the peer's display name. For MPIMs we
// join up to 3 names with ", " and append "+N" for the rest.
func (a *App) deriveConversationName(channelType string, userIDs []string) string {
	const previewLimit = 3
	names := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if name, ok := a.userNames[id]; ok && name != "" {
			names = append(names, name)
		} else {
			names = append(names, id)
		}
	}
	if channelType == "dm" {
		return names[0]
	}
	if len(names) <= previewLimit {
		return strings.Join(names, ", ")
	}
	preview := strings.Join(names[:previewLimit], ", ")
	return fmt.Sprintf("%s +%d", preview, len(names)-previewLimit)
}
```

Add imports to the file: `"fmt"`, `"strings"`, and `"github.com/gammons/slk/internal/ui/sidebar"`. Update the import block at the top.

- [ ] **Step 5: Run all reducer tests**

```
go test ./internal/ui/ -run TestReducer_ -v
```

Expected: all reducer tests PASS, including the new 3.

- [ ] **Step 6: Run full suite**

```
go test ./...
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```
git add internal/ui/reducer_new_message.go internal/ui/reducer_new_message_test.go internal/ui/sidebar/model.go
git commit -m "feat(new-message): hydrate cache record for freshly-opened DMs/MPIMs

When conversations.open returns AlreadyOpen=false, insert a minimal
sidebar.ChannelItem (ID, Type, DMUserID for 1:1, derived Name) so
the channel-switch path has a record to render against. Existing
RTM event handlers and membership.Manager fill in the rest."
```

(Drop `sidebar/model.go` from `git add` if it didn't need changes.)
