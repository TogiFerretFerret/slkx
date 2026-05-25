# Task 11: Reducer — message dispatch and in-flight tracking

**Goal:** Add a dedicated reducer that handles `EnterNewMessageMsg`, `NewMessageOpenedMsg`, and `NewMessageFailedMsg`. The reducer enforces the `RequestID`-based cancellation discipline.

**Files:**
- Create: `internal/ui/reducer_new_message.go`
- Create: `internal/ui/reducer_new_message_test.go`
- Modify: `internal/ui/app.go` (register the reducer in `dispatchReducers`)

---

- [ ] **Step 1: Add a failing reducer test**

Write `internal/ui/reducer_new_message_test.go`:

```go
package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newApp_WithOpenConvCapture(t *testing.T) (*App, *capturedOpenConv) {
	t.Helper()
	app := NewApp()
	app.currentUserID = "USELF"
	app.SetUserNames(map[string]string{"USELF": "Me", "U1": "Alice", "U2": "Bob"})

	cap := &capturedOpenConv{}
	app.SetChannelService(NewChannelService(ChannelServiceFuncs{
		OpenConversation: func(userIDs []string, requestID uint64) tea.Cmd {
			cap.calls = append(cap.calls, openConvCall{UserIDs: userIDs, RequestID: requestID})
			return nil // tests synthesize the result message directly
		},
	}))
	return app, cap
}

type openConvCall struct {
	UserIDs   []string
	RequestID uint64
}

type capturedOpenConv struct {
	calls []openConvCall
}

func TestReducer_EnterNewMessageMsg_OpensPickerAndEntersMode(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)

	_, _ = app.Update(EnterNewMessageMsg{})

	if app.mode != ModeNewMessage {
		t.Errorf("expected ModeNewMessage, got %v", app.mode)
	}
	if !app.newMessagePicker.IsVisible() {
		t.Error("expected picker visible")
	}
}

func TestReducer_NewMessageOpenedMsg_SwitchesChannelAndEntersInsertMode(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 7 // simulate an in-flight submit

	cmd, _ := app.Update(NewMessageOpenedMsg{
		ChannelID:   "D123",
		AlreadyOpen: true,
		UserIDs:     []string{"U1"},
		RequestID:   7,
	})

	if app.mode != ModeInsert {
		t.Errorf("expected ModeInsert after opened msg, got %v", app.mode)
	}
	if app.newMessagePicker.IsVisible() {
		t.Error("expected picker closed")
	}
	// The reducer should emit a ChannelSelectedMsg.
	if cmd == nil {
		t.Fatal("expected a tea.Cmd that fires ChannelSelectedMsg")
	}
	msg := cmd()
	sel, ok := msg.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("expected ChannelSelectedMsg, got %T", msg)
	}
	if sel.ID != "D123" {
		t.Errorf("expected ChannelID=D123, got %s", sel.ID)
	}
}

func TestReducer_NewMessageOpenedMsg_DroppedWhenCancelled(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 7
	app.newMessageCancelled = true
	priorMode := app.mode

	cmd, _ := app.Update(NewMessageOpenedMsg{
		ChannelID: "D123",
		RequestID: 7,
	})

	if app.mode != priorMode {
		t.Errorf("mode should not change for cancelled result, got %v", app.mode)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd for cancelled result, got %T", cmd())
	}
}

func TestReducer_NewMessageOpenedMsg_DroppedWhenRequestIDMismatches(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 9 // current in-flight is 9
	priorMode := app.mode

	// Late arrival for an older request 7.
	cmd, _ := app.Update(NewMessageOpenedMsg{
		ChannelID: "D123",
		RequestID: 7,
	})

	if app.mode != priorMode {
		t.Errorf("mode should not change for stale RequestID, got %v", app.mode)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd for stale result")
	}
}

func TestReducer_NewMessageFailedMsg_KeepsModalOpenAndEmitsToast(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 7

	cmd, _ := app.Update(NewMessageFailedMsg{
		RequestID: 7,
		Err:       errors.New("rate_limited"),
	})

	if app.mode != ModeNewMessage {
		t.Errorf("expected to stay in ModeNewMessage on failure, got %v", app.mode)
	}
	if !app.newMessagePicker.IsVisible() {
		t.Error("expected picker still visible on failure")
	}
	if cmd == nil {
		t.Fatal("expected a tea.Cmd emitting ToastMsg")
	}
	msg := cmd()
	toast, ok := msg.(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %T", msg)
	}
	if toast.Text == "" {
		t.Error("expected non-empty toast text")
	}
}
```

If `SetChannelService` isn't in scope, search for it:

```
grep -n "func (a \*App) SetChannelService" internal/ui/app.go
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/ui/ -run TestReducer_ -v
```

Expected: 5 failures (or build errors, depending on how Task 9 left the residual switch).

- [ ] **Step 3: Create the reducer file**

Write `internal/ui/reducer_new_message.go`:

```go
// internal/ui/reducer_new_message.go
//
// Reducer for the new-message picker lifecycle:
//
//   EnterNewMessageMsg     - user pressed Ctrl+N: seed the picker
//                            with current workspace users, open it,
//                            enter ModeNewMessage.
//   NewMessageOpenedMsg    - conversations.open succeeded: validate
//                            RequestID against the in-flight counter
//                            and the cancelled flag, then close the
//                            modal, switch to the opened channel,
//                            and enter ModeInsert (so the cursor
//                            lands in compose ready to type).
//   NewMessageFailedMsg    - conversations.open failed: log and
//                            keep the modal open so the user can
//                            retry or cancel.
//
// Cache hydration for AlreadyOpen=false is implemented in Task 12;
// this task emits ChannelSelectedMsg directly without inserting a
// channel record. Task 12 adds that insert.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
)

var reduceNewMessage reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case EnterNewMessageMsg:
		a.seedNewMessagePicker()
		a.newMessagePicker.Open()
		a.SetMode(ModeNewMessage)
		return nil, true

	case NewMessageOpenedMsg:
		if !newMessageResultIsCurrent(a, m.RequestID) {
			debuglog.Printf("new-message: dropping stale/cancelled NewMessageOpenedMsg req=%d inflight=%d cancelled=%v", m.RequestID, a.newMessageInFlightID, a.newMessageCancelled)
			return nil, true
		}
		a.newMessagePicker.Close()
		a.newMessageInFlightID = 0
		a.newMessageCancelled = false
		a.SetMode(ModeInsert)

		// Task 12 inserts a minimal cache record here when
		// m.AlreadyOpen == false. For now, emit ChannelSelectedMsg
		// directly; existing flows (cache miss, RTM hydration) fill
		// in the rest.
		channelID := m.ChannelID
		channelType := "dm"
		if len(m.UserIDs) > 1 {
			channelType = "group_dm"
		}
		return func() tea.Msg {
			return ChannelSelectedMsg{ID: channelID, Type: channelType}
		}, true

	case NewMessageFailedMsg:
		if !newMessageResultIsCurrent(a, m.RequestID) {
			return nil, true
		}
		debuglog.Printf("new-message: OpenConversation failed: %v", m.Err)
		// Stay in ModeNewMessage; modal stays visible; clear the
		// in-flight so a follow-up submit gets a fresh ID. Surface
		// the error via a toast (the existing app-wide notification
		// channel) so the user knows the submit didn't go through.
		a.newMessageInFlightID = 0
		errText := m.Err.Error()
		return func() tea.Msg { return ToastMsg{Text: "Open DM failed: " + errText} }, true
	}
	return nil, false
}

// newMessageResultIsCurrent reports whether a NewMessage* message
// with requestID is the response to the current in-flight submit
// AND wasn't cancelled by an Esc. Late or cancelled results are
// dropped silently.
func newMessageResultIsCurrent(a *App, requestID uint64) bool {
	if requestID == 0 {
		return false
	}
	if requestID != a.newMessageInFlightID {
		return false
	}
	if a.newMessageCancelled {
		return false
	}
	return true
}
```

- [ ] **Step 4: Register the reducer in `App.Update`**

In `internal/ui/app.go`, find the `dispatchReducers` call (around line 385) and add `reduceNewMessage` to the chain. Insert after `reduceWorkspace`:

```go
	if cmd, handled := dispatchReducers(a, msg,
		a.presence,
		a.preview,
		a.drag,
		a.typing,
		a.bootstrap,
		reduceReactions,
		reduceThreads,
		reduceSend,
		reduceChannels,
		reduceWorkspace,
		reduceNewMessage,
		reduceIO,
		reduceMouse,
	); handled {
```

- [ ] **Step 5: Run all reducer tests**

```
go test ./internal/ui/ -run TestReducer_ -v
```

Expected: 5 PASS.

- [ ] **Step 6: Run the full suite**

```
go test ./...
```

Expected: all tests PASS across the module.

- [ ] **Step 7: Commit**

```
git add internal/ui/reducer_new_message.go internal/ui/reducer_new_message_test.go internal/ui/app.go
git commit -m "feat(new-message): reducer with in-flight cancellation tracking

Reducer claims EnterNewMessageMsg, NewMessageOpenedMsg, and
NewMessageFailedMsg. RequestID matching and the cancelled flag
together drop late results from cancelled or superseded submits.
Success path emits ChannelSelectedMsg + transitions to ModeInsert."
```
