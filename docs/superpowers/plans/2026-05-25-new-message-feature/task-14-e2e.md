# Task 14: End-to-end test + manual verification + final commit

**Goal:** Confirm the feature works end to end. Add one final integration test that simulates the full Ctrl+N → pill → Enter → channel-switch flow, then perform the manual smoke test against a real Slack workspace.

**Files:**
- Modify: `internal/ui/reducer_new_message_test.go`

---

- [ ] **Step 1: Add an end-to-end integration test**

Append to `internal/ui/reducer_new_message_test.go`:

```go
func TestEndToEnd_CtrlN_SelectUser_Enter_OpensDM(t *testing.T) {
	app, cap := newApp_WithOpenConvCapture(t)

	// 1. User presses Ctrl+N.
	_, _ = app.Update(EnterNewMessageMsg{})

	if app.mode != ModeNewMessage {
		t.Fatalf("step 1: expected ModeNewMessage, got %v", app.mode)
	}

	// 2. User types "ali" to filter down to Alice.
	app.newMessagePicker.HandleKey("a")
	app.newMessagePicker.HandleKey("l")
	app.newMessagePicker.HandleKey("i")

	// 3. User presses Enter on the highlighted (Alice) row.
	result := app.newMessagePicker.HandleKey("enter")
	if result == nil {
		t.Fatal("step 3: expected non-nil Result from Enter")
	}
	if len(result.UserIDs) != 1 || result.UserIDs[0] != "U1" {
		t.Errorf("step 3: expected [U1], got %v", result.UserIDs)
	}

	// 4. The mode handler would call OpenConversation here. Simulate.
	app.newMessageInFlightID++
	reqID := app.newMessageInFlightID
	_ = app.channels.OpenConversation(result.UserIDs, reqID)

	if len(cap.calls) != 1 {
		t.Fatalf("step 4: expected exactly 1 OpenConversation call, got %d", len(cap.calls))
	}
	if cap.calls[0].UserIDs[0] != "U1" {
		t.Errorf("step 4: expected userID U1, got %s", cap.calls[0].UserIDs[0])
	}

	// 5. Slack returns success; reducer transitions to ModeInsert.
	cmd, _ := app.Update(NewMessageOpenedMsg{
		ChannelID:   "D123",
		AlreadyOpen: true,
		UserIDs:     []string{"U1"},
		RequestID:   reqID,
	})

	if app.mode != ModeInsert {
		t.Errorf("step 5: expected ModeInsert, got %v", app.mode)
	}
	if app.newMessagePicker.IsVisible() {
		t.Error("step 5: expected picker hidden")
	}

	// 6. The cmd should emit ChannelSelectedMsg{ID: D123, Type: dm}.
	msg := cmd()
	sel, ok := msg.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("step 6: expected ChannelSelectedMsg, got %T", msg)
	}
	if sel.ID != "D123" || sel.Type != "dm" {
		t.Errorf("step 6: want {D123, dm}, got {%s, %s}", sel.ID, sel.Type)
	}
}
```

- [ ] **Step 2: Run the integration test**

```
go test ./internal/ui/ -run TestEndToEnd_CtrlN_SelectUser_Enter_OpensDM -v
```

Expected: PASS.

- [ ] **Step 3: Run the full test suite**

```
go test ./...
```

Expected: ALL tests PASS across the entire module.

- [ ] **Step 4: Run linters**

```
go vet ./...
```

Expected: no output.

If golangci-lint is available locally (the CI runs it):

```
golangci-lint run
```

Expected: no diagnostics. If diagnostics fire only in the new files, fix them inline before the final commit.

- [ ] **Step 5: Build a release-mode binary**

```
go build -o /tmp/slk-new-message ./cmd/slk
```

Expected: succeeds; binary is around 19-22 MB.

- [ ] **Step 6: Manual smoke test**

Run the binary against a real Slack workspace. Verify each:

1. From the channels view (ModeNormal), press `Ctrl+N`. The modal appears centered with title "New message", an empty pill bar, and a list of users sorted by recency.
2. Type a few characters of a coworker's name. The list filters; the top match is highlighted.
3. Press Enter. The modal closes, the app switches to the DM, and the compose box has focus.
4. Press `Ctrl+N` again. Type one name, press Space. A pill appears. Backspace. The pill is removed.
5. Press `Ctrl+N`. Build a 3-person group by toggling 3 users with Space. The counter shows `3 / 8`. Press Enter. The MPIM opens and the compose box has focus.
6. Press `Ctrl+N`. Select 8 users. Try to select a 9th. The 9th selection is ignored; counter shows `8 / 8 MPIM limit reached`.
7. Press `Ctrl+N`. Press Esc immediately. Modal closes, mode returns to Normal.
8. Press `Ctrl+N`. Type a query, hit Enter to trigger a real submit. Before the response arrives (use a flaky network or a slow workspace), press Esc. Modal closes; when the response eventually lands, the app does NOT switch channels.

If any check fails, file an issue and return to the relevant earlier task to fix.

- [ ] **Step 7: Final commit (if any small fixes from manual testing)**

If steps 1–8 all passed cleanly with no code changes, skip this step. Otherwise commit any fixes:

```
git add -p
git commit -m "fix(new-message): <specific issue found in manual testing>"
```

- [ ] **Step 8: Verify the git log**

```
git log --oneline | head -16
```

Expected: 14 commits with `feat(new-message):` prefixes (one per task) plus the two spec commits from before the plan was executed (`30eeafb` and `e1ec0b2`).

The feature is shippable.
