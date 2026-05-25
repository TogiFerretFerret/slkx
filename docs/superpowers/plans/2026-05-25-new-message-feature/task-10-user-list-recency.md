# Task 10: User list snapshot on modal open

**Goal:** Build the `[]newmessagepicker.User` slice from the App's existing state (`a.userNames`, `a.externalUsers`, `a.currentUserID`). Push it into the picker at modal-open time so the picker always sees a fresh snapshot.

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/newmessagepicker/model.go` (add `Users()` accessor for tests)

---

> **Scoping cut — recency deferred to a follow-up:** The spec calls for recency-DESC ordering with a 30-day "recent DMs first" signal. Sourcing that data cleanly requires plumbing `WorkspaceContext.LastVisitedByChannel` (which lives in main.go) into the App alongside `SetUserNames`. That's a horizontal slice change touching `cmd/slk/main.go`, `msgs.go` (`WorkspaceReadyMsg`/`WorkspaceSwitchedMsg`), and `reducer_workspace.go`. To keep this plan shippable, v1 sorts purely by display name (alphabetical) and leaves the `User.Recency` field as 0. A follow-up plan adds the recency wiring once this lands. The picker's filter code already handles `Recency == 0` correctly — it falls through to the alpha tie-break, which IS the v1 behavior.

- [ ] **Step 1: Add a failing test for the snapshot**

Append to `internal/ui/app_test.go`:

```go
func TestSeedNewMessagePicker_PopulatesUsersAndExcludesSelf(t *testing.T) {
	app := NewApp()
	app.currentUserID = "USELF"
	app.SetUserNames(map[string]string{
		"USELF": "Me",
		"U1":    "Alice",
		"U2":    "Bob",
	})
	app.SetExternalUsers(map[string]bool{"U2": true})

	app.seedNewMessagePicker()

	users := app.newMessagePicker.Users()
	// Picker holds all non-self users. The picker excludes self via
	// SetCurrentUserID at filter time, not at the SetUsers slice level.
	// So the slice should contain Alice + Bob + Me, but after Open()
	// the filtered list excludes Me.
	ids := map[string]bool{}
	for _, u := range users {
		ids[u.ID] = true
	}
	if !ids["U1"] {
		t.Error("expected Alice (U1) in picker users")
	}
	if !ids["U2"] {
		t.Error("expected Bob (U2) in picker users")
	}
	if ids["USELF"] {
		t.Error("expected USELF excluded from seeded slice")
	}

	// External flag should propagate.
	for _, u := range users {
		if u.ID == "U2" && !u.IsExternal {
			t.Error("expected Bob (U2) to be marked external")
		}
	}
}
```

> The seed helper excludes self at slice-build time (skipping the iteration over `a.currentUserID`), so the slice does NOT contain Me. The `SetCurrentUserID` call is belt-and-suspenders for the picker's filter to also reject self if it somehow slips in.

This test refers to `app.newMessagePicker.Users()` which doesn't exist yet — add it.

- [ ] **Step 2: Add `Users()` accessor on the picker**

In `internal/ui/newmessagepicker/model.go`, add (after `SetUsers`):

```go
// Users returns the user list most recently set via SetUsers. Used
// by tests; not part of the picker's input API.
func (m *Model) Users() []User {
	return m.users
}
```

- [ ] **Step 3: Run the test to confirm it fails**

```
go test ./internal/ui/ -run TestSeedNewMessagePicker_PopulatesUsersAndExcludesSelf -v
```

Expected: FAIL — `app.seedNewMessagePicker` doesn't exist yet.

- [ ] **Step 4: Add `seedNewMessagePicker` helper on App**

In `internal/ui/app.go`, add this method near the existing `SetUserNames` (around line 1612).

```go
// seedNewMessagePicker snapshots the current workspace's user list
// into the new-message picker and configures the self-exclusion.
// Called when ModeNewMessage is entered so the modal always sees a
// fresh view of the workspace.
//
// User-list shape:
//   - DisplayName from a.userNames (the workspace display name map).
//   - Username falls back to DisplayName — there is no separate
//     userID->handle map on App today. The picker uses Username
//     only for filter matching, so this fallback degrades
//     gracefully: queries against the display name still hit.
//   - IsExternal from a.externalUsers.
//   - Recency is left at 0 (see scoping note at the top of this task).
//   - Self (a.currentUserID) is excluded via SetCurrentUserID.
func (a *App) seedNewMessagePicker() {
	users := make([]newmessagepicker.User, 0, len(a.userNames))
	for id, name := range a.userNames {
		if id == a.currentUserID {
			continue
		}
		users = append(users, newmessagepicker.User{
			ID:          id,
			DisplayName: name,
			Username:    name, // see scoping note; replaceable when a handle map lands
			IsExternal:  a.externalUsers[id],
		})
	}

	a.newMessagePicker.SetCurrentUserID(a.currentUserID)
	a.newMessagePicker.SetUsers(users)
}
```

- [ ] **Step 5: Skip — wiring happens in Task 11**

The reducer in Task 11 calls `a.seedNewMessagePicker()` from its `EnterNewMessageMsg` arm. Nothing to wire here.

- [ ] **Step 6: Run the test**

```
go test ./internal/ui/ -run TestSeedNewMessagePicker_PopulatesUsersAndExcludesSelf -v
```

Expected: PASS.

- [ ] **Step 7: Run the full suite**

```
go test ./...
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```
git add internal/ui/app.go internal/ui/newmessagepicker/model.go internal/ui/app_test.go
git commit -m "feat(new-message): snapshot workspace users on modal open

Builds a []newmessagepicker.User slice from a.userNames and
a.externalUsers. Self is excluded via SetCurrentUserID. Recency
is left at 0 (alphabetical order); plumbing it through from
WorkspaceContext.LastVisitedByChannel is a follow-up."
```
