# Task 4: Navigation + multi-select toggle + MPIM cap

**Goal:** Add `HandleKey` for navigation (up/down/ctrl+p/ctrl+n), printable-rune query input, backspace, and Space/Tab to toggle the highlighted user into the pill bar. Enforce `MaxRecipients = 8` cap.

**Files:**
- Modify: `internal/ui/newmessagepicker/model.go`
- Modify: `internal/ui/newmessagepicker/model_test.go`

---

- [ ] **Step 1: Add failing tests for navigation**

Append to `internal/ui/newmessagepicker/model_test.go`:

```go
func TestHandleKey_DownMovesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	// 5 users, highlight starts at 0.
	m.HandleKey("down")
	if m.highlight != 1 {
		t.Errorf("expected highlight=1 after down, got %d", m.highlight)
	}
}

func TestHandleKey_UpMovesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("down")
	m.HandleKey("down")
	m.HandleKey("up")
	if m.highlight != 1 {
		t.Errorf("expected highlight=1 after down,down,up, got %d", m.highlight)
	}
}

func TestHandleKey_DownClampsAtEnd(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	for i := 0; i < 20; i++ {
		m.HandleKey("down")
	}
	if m.highlight != len(m.filtered)-1 {
		t.Errorf("expected highlight clamped at %d, got %d", len(m.filtered)-1, m.highlight)
	}
}

func TestHandleKey_UpClampsAtStart(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("up")
	if m.highlight != 0 {
		t.Errorf("expected highlight=0 clamped, got %d", m.highlight)
	}
}

func TestHandleKey_CtrlNAndCtrlPAliasNavigation(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("ctrl+n")
	if m.highlight != 1 {
		t.Errorf("ctrl+n should be alias for down")
	}
	m.HandleKey("ctrl+p")
	if m.highlight != 0 {
		t.Errorf("ctrl+p should be alias for up")
	}
}

func TestHandleKey_PrintableRuneAppendsToQueryAndRefilters(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("a")
	m.HandleKey("l")
	m.HandleKey("i")
	if m.query != "ali" {
		t.Errorf("expected query=ali, got %q", m.query)
	}
	if len(m.filtered) == 0 || m.users[m.filtered[0]].ID != "U1" {
		t.Errorf("expected Alice (U1) first, got %v", m.filtered)
	}
}

func TestHandleKey_BackspaceRemovesLastRune(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("a")
	m.HandleKey("l")
	m.HandleKey("backspace")
	if m.query != "a" {
		t.Errorf("expected query=a after backspace, got %q", m.query)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```
go test ./internal/ui/newmessagepicker/ -run TestHandleKey -v
```

Expected: all `TestHandleKey_*` tests FAIL with "HandleKey undefined".

- [ ] **Step 3: Add `HandleKey` for navigation only**

In `internal/ui/newmessagepicker/model.go`, append:

```go
// HandleKey processes a single key event. The string form mirrors the
// other picker packages (channelfinder, mentionpicker): "up", "down",
// "ctrl+n", "ctrl+p", "backspace", "esc", "enter", "tab", " ", or
// any single printable ASCII character.
//
// Returns a non-nil Result when the user submits the picker; the
// caller (the mode handler) closes the picker and dispatches the
// conversations.open call.
//
// This task wires the navigation arms only. Selection (Space/Tab)
// lands in this task too; submit semantics (Enter) land in Task 5.
func (m *Model) HandleKey(keyStr string) *Result {
	switch keyStr {
	case "down", "ctrl+n":
		if m.highlight < len(m.filtered)-1 {
			m.highlight++
		}
		return nil
	case "up", "ctrl+p":
		if m.highlight > 0 {
			m.highlight--
		}
		return nil
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.highlight = 0
			m.filter()
		}
		return nil
	}

	// Single printable ASCII rune -> append to query.
	if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 && keyStr[0] != ' ' {
		m.query += keyStr
		m.highlight = 0
		m.filter()
	}
	return nil
}
```

Note we exclude the space character from the "append to query" path — Space is reserved for the selection toggle.

- [ ] **Step 4: Run navigation tests**

```
go test ./internal/ui/newmessagepicker/ -run TestHandleKey -v
```

Expected: 7 PASS.

- [ ] **Step 5: Add failing tests for selection toggle and the MPIM cap**

Append to `model_test.go`:

```go
func TestHandleKey_SpaceTogglesHighlightedUserIntoSelection(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	// highlight starts at 0 -> Alice (U1)

	m.HandleKey(" ")
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 in selection after space")
	}
	if len(m.selected) != 1 {
		t.Errorf("expected 1 selection, got %d", len(m.selected))
	}
}

func TestHandleKey_TabAliasesSpace(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("tab")
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 in selection after tab")
	}
}

func TestHandleKey_SpaceOnSelectedRemovesIt(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")
	m.HandleKey(" ")
	if _, ok := m.selected["U1"]; ok {
		t.Error("expected U1 to be removed after second space")
	}
}

func TestHandleKey_SpaceCapsAtMaxRecipients(t *testing.T) {
	users := make([]User, 10)
	for i := range users {
		users[i] = User{
			ID:          fmt.Sprintf("U%d", i),
			DisplayName: fmt.Sprintf("User %d", i),
			Username:    fmt.Sprintf("u%d", i),
		}
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	// Select 8 users by hitting space then down 8 times.
	for i := 0; i < 8; i++ {
		m.HandleKey(" ")
		m.HandleKey("down")
	}
	if len(m.selected) != 8 {
		t.Fatalf("expected 8 selections, got %d", len(m.selected))
	}
	// 9th selection attempt must be a no-op.
	m.HandleKey(" ")
	if len(m.selected) != 8 {
		t.Errorf("expected selection to be capped at 8, got %d", len(m.selected))
	}
}

func TestHandleKey_BackspaceAtEmptyQueryRemovesLastPill(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")          // select U1
	m.HandleKey("down")       // highlight U2
	m.HandleKey(" ")          // select U2
	// Two pills. Query is empty. Backspace should remove the LAST pill (U2).
	m.HandleKey("backspace")
	if _, ok := m.selected["U2"]; ok {
		t.Error("expected U2 removed by backspace at empty query")
	}
	if _, ok := m.selected["U1"]; !ok {
		t.Error("expected U1 still selected")
	}
}
```

You'll need `"fmt"` in the test imports if not already present.

- [ ] **Step 6: Run tests to confirm they fail**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: 5 new tests FAIL; navigation tests still PASS.

- [ ] **Step 7: Add the selection-toggle arm and a helper**

In `internal/ui/newmessagepicker/model.go`, find the `HandleKey` switch and add cases for `" "` and `"tab"`. Replace the existing function with this version:

```go
func (m *Model) HandleKey(keyStr string) *Result {
	switch keyStr {
	case "down", "ctrl+n":
		if m.highlight < len(m.filtered)-1 {
			m.highlight++
		}
		return nil
	case "up", "ctrl+p":
		if m.highlight > 0 {
			m.highlight--
		}
		return nil
	case " ", "tab":
		m.toggleHighlightedSelection()
		return nil
	case "backspace":
		m.handleBackspace()
		return nil
	}

	// Single printable ASCII rune -> append to query.
	if len(keyStr) == 1 && keyStr[0] >= 33 && keyStr[0] <= 126 {
		m.query += keyStr
		m.highlight = 0
		m.filter()
	}
	return nil
}

// toggleHighlightedSelection adds or removes the currently-highlighted
// user from the pill bar. No-op when nothing is highlighted, when the
// pill bar is at MaxRecipients capacity, or when the highlighted user
// is already selected (in which case it removes them).
func (m *Model) toggleHighlightedSelection() {
	if m.highlight < 0 || m.highlight >= len(m.filtered) {
		return
	}
	userID := m.users[m.filtered[m.highlight]].ID
	if _, already := m.selected[userID]; already {
		delete(m.selected, userID)
		return
	}
	if len(m.selected) >= MaxRecipients {
		return
	}
	m.selected[userID] = struct{}{}
}

// handleBackspace deletes the last rune of the query. If the query is
// already empty AND the pill bar is non-empty, it removes the LAST
// pill instead — letting the user erase a wrong recipient without
// reaching for the mouse.
func (m *Model) handleBackspace() {
	if len(m.query) > 0 {
		m.query = m.query[:len(m.query)-1]
		m.highlight = 0
		m.filter()
		return
	}
	if len(m.selected) == 0 {
		return
	}
	// No pill order is recorded; we use the last user ID inserted
	// into the selected set. Since Go map iteration is randomized, we
	// instead pop the one that appears last in m.users order — this
	// gives the user a stable, predictable "remove most recently
	// added" behavior tied to the user list order rather than a
	// hidden insertion order. (Alternative would be a slice of IDs;
	// YAGNI — a single backspace mid-flow is rare.)
	for i := len(m.users) - 1; i >= 0; i-- {
		id := m.users[i].ID
		if _, ok := m.selected[id]; ok {
			delete(m.selected, id)
			return
		}
	}
}
```

> **SOLID note:** `toggleHighlightedSelection` and `handleBackspace` are extracted as methods with single responsibilities. `HandleKey` becomes a dispatcher — fewer lines, one level of indentation per arm.

> **Order-of-pills correction:** The test `TestHandleKey_BackspaceAtEmptyQueryRemovesLastPill` selects U1 then U2 and expects U2 to be removed. In `testUsers()`, U1 appears at index 0 and U2 at index 1; iterating `m.users` backwards finds U2 first. The test will pass.

- [ ] **Step 8: Run all picker tests**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: ALL tests PASS.

- [ ] **Step 9: Commit**

```
git add internal/ui/newmessagepicker/
git commit -m "feat(new-message): navigation, multi-select toggle, MPIM cap

HandleKey now supports up/down/ctrl+p/ctrl+n navigation, Space/Tab
to toggle a user into the pill bar (capped at MaxRecipients=8),
printable runes to filter, and backspace to either delete the last
query rune or remove the last pill when the query is empty."
```
