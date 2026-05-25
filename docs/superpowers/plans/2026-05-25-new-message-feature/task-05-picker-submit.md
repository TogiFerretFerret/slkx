# Task 5: Submit & cancel semantics

**Goal:** Enter and Esc complete the picker's behavioral surface. Enter returns a `*Result` containing the pill IDs (or, if pills are empty, the highlighted user's ID); Esc closes the picker and returns nil.

**Files:**
- Modify: `internal/ui/newmessagepicker/model.go`
- Modify: `internal/ui/newmessagepicker/model_test.go`

---

- [ ] **Step 1: Add failing tests**

Append to `internal/ui/newmessagepicker/model_test.go`:

```go
func TestHandleKey_EnterWithPillsReturnsPillIDs(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")    // select Alice (U1)
	m.HandleKey("down") // highlight Bob
	m.HandleKey(" ")    // select Bob (U2)

	res := m.HandleKey("enter")
	if res == nil {
		t.Fatal("expected non-nil result from Enter with pills")
	}
	if len(res.UserIDs) != 2 {
		t.Fatalf("expected 2 user IDs, got %d", len(res.UserIDs))
	}
	// Order within UserIDs is not guaranteed by the spec; check set membership.
	got := map[string]bool{}
	for _, id := range res.UserIDs {
		got[id] = true
	}
	if !got["U1"] || !got["U2"] {
		t.Errorf("expected {U1, U2}, got %v", res.UserIDs)
	}
}

func TestHandleKey_EnterWithoutPillsUsesHighlight(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey("down") // highlight Bob (U2)

	res := m.HandleKey("enter")
	if res == nil {
		t.Fatal("expected non-nil result from Enter with highlight")
	}
	if len(res.UserIDs) != 1 || res.UserIDs[0] != "U2" {
		t.Errorf("expected [U2], got %v", res.UserIDs)
	}
}

func TestHandleKey_EnterWithNoHighlightAndNoPillsReturnsNil(t *testing.T) {
	m := New()
	m.SetUsers(nil) // no users -> filtered is empty
	m.Open()

	res := m.HandleKey("enter")
	if res != nil {
		t.Errorf("expected nil result when nothing to submit, got %+v", res)
	}
}

func TestHandleKey_EnterWithEmptyFilteredButPillsStillSubmits(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.HandleKey(" ")        // select Alice
	m.setQuery("xyzqq")    // filter to nothing
	res := m.HandleKey("enter")
	if res == nil || len(res.UserIDs) != 1 || res.UserIDs[0] != "U1" {
		t.Errorf("expected pills to submit even with empty filter, got %+v", res)
	}
}

func TestHandleKey_EscClosesPickerAndReturnsNil(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	if !m.IsVisible() {
		t.Fatal("precondition: picker should be visible")
	}
	res := m.HandleKey("esc")
	if res != nil {
		t.Errorf("expected nil result from Esc, got %+v", res)
	}
	if m.IsVisible() {
		t.Error("expected picker to be hidden after Esc")
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

```
go test ./internal/ui/newmessagepicker/ -run TestHandleKey_Enter -v
go test ./internal/ui/newmessagepicker/ -run TestHandleKey_Esc -v
```

Expected: 5 failures.

- [ ] **Step 3: Add Enter and Esc arms to `HandleKey`**

In `internal/ui/newmessagepicker/model.go`, find the `HandleKey` switch and add two cases above the existing `"down"` case (order doesn't matter, but enter/esc first reads well):

```go
	case "enter":
		return m.submit()
	case "esc":
		m.Close()
		return nil
```

Then add the `submit` helper after `handleBackspace`:

```go
// submit returns a non-nil *Result when the picker has something to
// submit:
//   - If pills are present, the result carries the pill IDs.
//   - Otherwise, if a user is highlighted, the result carries that one ID.
//   - If neither is true (empty filter AND no pills), returns nil — Enter is a no-op.
//
// Pill order in the returned slice matches the user-list order so the
// caller sees a stable, predictable order across calls.
func (m *Model) submit() *Result {
	if len(m.selected) > 0 {
		ids := make([]string, 0, len(m.selected))
		for _, u := range m.users {
			if _, ok := m.selected[u.ID]; ok {
				ids = append(ids, u.ID)
			}
		}
		return &Result{UserIDs: ids}
	}
	if m.highlight < 0 || m.highlight >= len(m.filtered) {
		return nil
	}
	return &Result{UserIDs: []string{m.users[m.filtered[m.highlight]].ID}}
}
```

- [ ] **Step 4: Run all picker tests**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: ALL tests PASS (5 from Task 2 + 9 from Task 3 + 12 from Task 4 + 5 from this task = 31).

- [ ] **Step 5: Commit**

```
git add internal/ui/newmessagepicker/
git commit -m "feat(new-message): Enter submits, Esc cancels

Enter with pills returns the pill IDs in user-list order. Enter
without pills returns the highlighted user's ID. Enter with neither
is a no-op. Esc hides the picker and returns nil."
```
