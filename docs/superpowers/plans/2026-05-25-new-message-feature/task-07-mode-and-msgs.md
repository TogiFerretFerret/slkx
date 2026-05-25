# Task 7: Mode constant + message types + Ctrl+N keybind

**Goal:** Wire the type-level surfaces that the App needs: the new `ModeNewMessage` constant, the `NewMessage` keybind in the default keymap, and the three message types in `msgs.go`. No App state or reducer yet — those land in Tasks 9 and 11.

**Files:**
- Modify: `internal/ui/mode.go`
- Modify: `internal/ui/keys.go`
- Modify: `internal/ui/msgs.go`

---

- [ ] **Step 1: Add `ModeNewMessage` to the Mode enum**

In `internal/ui/mode.go`, add the new constant to the iota block (after `ModeHelp`):

```go
const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeSearch
	ModeChannelFinder
	ModeReactionPicker
	ModeWorkspaceFinder
	ModeThemeSwitcher
	ModePresenceMenu
	ModePresenceCustomSnooze
	ModeConfirm
	ModeHelp
	ModeNewMessage
)
```

Then add the `String()` case (before the `default`):

```go
	case ModeNewMessage:
		return "NEW MSG"
```

- [ ] **Step 2: Add the `NewMessage` keybind to the KeyMap**

In `internal/ui/keys.go`, add a `NewMessage` field to the `KeyMap` struct (anywhere in the struct, but next to `WorkspaceFinder` reads well):

```go
	NewMessage          key.Binding
```

In `DefaultKeyMap()`, add the corresponding initializer (place it next to `WorkspaceFinder`):

```go
		NewMessage:          key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new message")),
```

- [ ] **Step 3: Add the three message types to `msgs.go`**

In `internal/ui/msgs.go`, scroll to the bottom of the file (after `editEmptyToastMsg`) and append:

```go
// EnterNewMessageMsg is dispatched when the user presses Ctrl+N in
// ModeNormal. The reducer (reduceNewMessage) handles it by snapshotting
// the workspace user list, opening the newmessagepicker, and switching
// to ModeNewMessage.
type EnterNewMessageMsg struct{}

// NewMessageOpenedMsg carries the result of a successful
// ChannelService.OpenConversation call. RequestID identifies which
// submit this is the response to so the reducer can drop late
// arrivals from cancelled submits. AlreadyOpen=true means Slack
// returned an existing DM/MPIM; the reducer skips the
// minimal-channel-record insert in that case (Task 12).
//
// Distinct from the existing ConversationOpenedMsg in this file,
// which is dispatched by the WS event handler for Slack's
// mpim_open / im_created events (channel side-effect of someone
// being added to a conversation). The new-message flow has its own
// type because it carries the in-flight RequestID and we need
// per-message routing in reduceNewMessage.
type NewMessageOpenedMsg struct {
	ChannelID   string
	AlreadyOpen bool
	UserIDs     []string // copied through so the reducer can hydrate the cache record
	RequestID   uint64
}

// NewMessageFailedMsg carries an error from a failed
// ChannelService.OpenConversation call. The reducer surfaces Err in
// the modal's footer banner; the modal stays open with the user's
// selection intact so they can retry.
type NewMessageFailedMsg struct {
	RequestID uint64
	Err       error
}
```

- [ ] **Step 4: Verify compilation**

```
go build ./...
```

Expected: no output (build succeeds). The new constants and types are used in later tasks.

- [ ] **Step 5: Run the full test suite to confirm nothing regressed**

```
go test ./...
```

Expected: all existing tests still PASS. (No new tests in this task — these are pure type additions.)

- [ ] **Step 6: Commit**

```
git add internal/ui/mode.go internal/ui/keys.go internal/ui/msgs.go
git commit -m "feat(new-message): add ModeNewMessage, Ctrl+N keybind, msg types

ModeNewMessage joins the Mode enum with display string \"NEW MSG\".
Ctrl+N is bound to the new NewMessage KeyMap field. Three messages
sit in msgs.go: EnterNewMessageMsg (Ctrl+N dispatch),
NewMessageOpenedMsg (success), NewMessageFailedMsg (error)."
```
