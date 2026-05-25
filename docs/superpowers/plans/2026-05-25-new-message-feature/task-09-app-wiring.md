# Task 9: App wiring — picker field, mode handler, overlay composite

**Goal:** Put a `newmessagepicker.Model` on `App`, route key events to it from a new `mode_new_message.go`, dispatch `EnterNewMessageMsg` on `Ctrl+N` from `ModeNormal`, and composite the modal in `view_overlays.go`.

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/mode_normal.go`
- Modify: `internal/ui/mode_handlers.go`
- Modify: `internal/ui/view_overlays.go`
- Create: `internal/ui/mode_new_message.go`

---

> **Note:** Submit-routing through `ChannelService.OpenConversation` lands in Task 11 via the reducer. This task gets the modal opening/closing wired so a manual smoke test from Ctrl+N already shows the picker.

- [ ] **Step 1: Add picker + in-flight tracking fields to App**

In `internal/ui/app.go`, find the sub-models block (around line 86 where `channelFinder` is declared) and add right after `channelFinder`:

```go
	newMessagePicker newmessagepicker.Model
```

Add the import (alphabetical order in the existing import block):

```go
	"github.com/gammons/slk/internal/ui/newmessagepicker"
```

Then find the in-flight/cancellation fields section. There isn't a perfect home — add near the workspace switching fields (around line 200). Add a comment block and two fields:

```go
	// newMessageInFlightID is the monotonic counter for in-flight
	// OpenConversation requests dispatched from the new-message
	// picker. 0 means no submit is in flight. The reducer drops late
	// results whose RequestID doesn't match.
	newMessageInFlightID uint64
	// newMessageCancelled is set to true when the user Escs the
	// modal while a submit is in flight. A subsequent
	// NewMessageOpenedMsg with the matching RequestID is dropped
	// rather than switching channels behind the user's back.
	newMessageCancelled bool
```

- [ ] **Step 2: Initialize the picker in `NewApp`**

In `NewApp` (around line 289), add to the struct literal next to `channelFinder`:

```go
		newMessagePicker:     newmessagepicker.New(),
```

- [ ] **Step 3: Add the Ctrl+N entry from ModeNormal**

In `internal/ui/mode_normal.go`, find the FuzzyFinder case (around line 169) and add a new case below it:

```go
	case key.Matches(msg, a.keys.NewMessage):
		return func() tea.Msg { return EnterNewMessageMsg{} }
```

(`tea` is already imported in this file as `tea "charm.land/bubbletea/v2"`.)

- [ ] **Step 4: Create the new-message mode handler**

Write `internal/ui/mode_new_message.go`:

```go
// internal/ui/mode_new_message.go
//
// New-message mode key handler. Forwards normalised keys to the
// newmessagepicker overlay. When the picker returns a Result, the
// handler dispatches a new submit via ChannelService.OpenConversation
// (with a monotonic RequestID) and tracks the in-flight state on App.
// On Esc, marks the in-flight as cancelled so a late result is
// dropped (see reducer_new_message.go for the dropping logic).
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleNewMessageMode(a *App, msg tea.KeyMsg) tea.Cmd {
	keyStr := msg.String()
	switch msg.Key().Code {
	case tea.KeyEnter:
		keyStr = "enter"
	case tea.KeyEscape:
		keyStr = "esc"
	case tea.KeyUp:
		keyStr = "up"
	case tea.KeyDown:
		keyStr = "down"
	case tea.KeyBackspace:
		keyStr = "backspace"
	case tea.KeyTab:
		keyStr = "tab"
	case tea.KeySpace:
		keyStr = " "
	}

	result := a.newMessagePicker.HandleKey(keyStr)
	if result != nil {
		// Submit. Bump the in-flight ID and clear cancellation
		// before dispatch so a fresh result is honored.
		a.newMessageInFlightID++
		a.newMessageCancelled = false
		reqID := a.newMessageInFlightID
		userIDs := result.UserIDs
		return a.channels.OpenConversation(userIDs, reqID)
	}

	// Picker closed itself (Esc). Mark any in-flight submit as
	// cancelled so its eventual result is dropped. Switch back to
	// ModeNormal.
	if !a.newMessagePicker.IsVisible() {
		if a.newMessageInFlightID != 0 {
			a.newMessageCancelled = true
		}
		a.SetMode(ModeNormal)
	}
	return nil
}
```

- [ ] **Step 5: Register the mode handler**

Open `internal/ui/mode_handlers.go` and search for the existing handler dispatch table. Add an entry for `ModeNewMessage` that calls `handleNewMessageMode`.

```
grep -n "handleChannelFinderMode" internal/ui/mode_handlers.go
```

The file has a dispatch map or switch. Locate the line that wires `ModeChannelFinder -> handleChannelFinderMode` and add a sibling entry directly below it:

```go
	case ModeNewMessage:
		return handleNewMessageMode(a, msg)
```

(Match the existing file's syntax — if it's a map literal, add `ModeNewMessage: handleNewMessageMode,`.)

- [ ] **Step 6: Composite the picker overlay**

In `internal/ui/view_overlays.go`, edit `applyOverlays` to add a check after `channelFinder` (around line 41):

```go
	if a.newMessagePicker.IsVisible() {
		screen = a.newMessagePicker.ViewOverlay(a.width, a.height, screen)
	}
```

Then add the same `IsVisible()` check to `overlayActive()` (around line 75):

```go
	return a.channelFinder.IsVisible() ||
		a.newMessagePicker.IsVisible() ||
		a.reactionPicker.IsVisible() ||
```

- [ ] **Step 7: Skip — no residual-switch wiring needed**

`internal/ui/app.go`'s `Update` residual switch (at line 440) only handles `tea.WindowSizeMsg` and `tea.KeyMsg` — all app-level messages route through reducers. The `EnterNewMessageMsg` arm lives in the new reducer in Task 11. This task does NOT touch `Update`.

That means `Ctrl+N` will be a no-op until Task 11 lands — that's fine; behavioral tests live in Task 11.

- [ ] **Step 8: Verify the wiring compiles**

The behavioral test for "Ctrl+N enters ModeNewMessage" lives in Task 11 (where the reducer wires the actual transition). Task 9's verification is compilation only:

```
go build ./...
```

Expected: succeeds. The picker field, mode handler, overlay composite, and message constants are all in place; the reducer behavior lands next.

- [ ] **Step 9: Run all existing tests to confirm no regressions**

```
go test ./...
```

Expected: all existing tests PASS. No new tests in this task.

- [ ] **Step 10: Manual smoke render (optional but recommended)**

```
go build -o /tmp/slk-smoke ./cmd/slk
```

Expected: build succeeds. (Don't run the binary — it requires real Slack credentials. The visual smoke test lives in Task 14.)

- [ ] **Step 11: Commit**

```
git add internal/ui/app.go internal/ui/mode_normal.go internal/ui/mode_handlers.go internal/ui/view_overlays.go internal/ui/mode_new_message.go
git commit -m "feat(new-message): wire picker into App, mode, and overlay stack

Adds App.newMessagePicker + in-flight tracking fields, the Ctrl+N
dispatch from ModeNormal, the ModeNewMessage key handler, and the
overlay composite. Submit-side wiring (calling OpenConversation,
handling its result) is deferred to the reducer in Task 11."
```
