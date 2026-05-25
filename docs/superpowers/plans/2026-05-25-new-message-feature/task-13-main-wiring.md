# Task 13: `cmd/slk/main.go` — production `OpenConversation` closure

**Goal:** Wire the real `ChannelService.OpenConversation` closure that calls `wctx.Client.OpenConversation` in a goroutine and emits `NewMessageOpenedMsg` or `NewMessageFailedMsg` back into the bubbletea program.

**Files:**
- Modify: `cmd/slk/main.go`

---

- [ ] **Step 1: Locate the existing `ChannelServiceFuncs` literal**

```
grep -n "NewChannelService(ui.ChannelServiceFuncs" cmd/slk/main.go
```

You'll land on the literal (around line 858). Other closures (Fetch, Lookup, MembershipFetch, etc.) sit inside this literal — model the new closure after them.

- [ ] **Step 2: Add the `OpenConversation` closure to the literal**

Inside the `ui.ChannelServiceFuncs{...}` block, add this field. Place it near `MembershipFetch` (the existing API-call-style closure) for readability:

```go
			OpenConversation: func(userIDs []string, requestID uint64) tea.Cmd {
				wctx := router.Active()
				if wctx == nil {
					return func() tea.Msg {
						return ui.NewMessageFailedMsg{
							RequestID: requestID,
							Err:       fmt.Errorf("no active workspace"),
						}
					}
				}
				client := wctx.Client
				return func() tea.Msg {
					channelID, alreadyOpen, err := client.OpenConversation(ctx, userIDs)
					if err != nil {
						return ui.NewMessageFailedMsg{
							RequestID: requestID,
							Err:       err,
						}
					}
					return ui.NewMessageOpenedMsg{
						ChannelID:   channelID,
						AlreadyOpen: alreadyOpen,
						UserIDs:     userIDs,
						RequestID:   requestID,
					}
				}
			},
```

Variables `ctx`, `router`, and `fmt` are all in scope at this point in main.go — confirm with:

```
grep -n "^	ctx\|router :=\|\"fmt\"" cmd/slk/main.go | head -10
```

If `fmt` isn't already imported in main.go (it likely is — most main.go files import it), add it to the import block.

- [ ] **Step 3: Build**

```
go build ./cmd/slk
```

Expected: succeeds. The binary now wires the real closure.

- [ ] **Step 4: Run the full test suite to confirm no regression**

```
go test ./...
```

Expected: all PASS. (No test touches main.go directly; this build is the verification.)

- [ ] **Step 5: Commit**

```
git add cmd/slk/main.go
git commit -m "feat(new-message): wire production OpenConversation closure

ChannelServiceFuncs.OpenConversation now dispatches a goroutine that
calls wctx.Client.OpenConversation and emits NewMessageOpenedMsg or
NewMessageFailedMsg back into the bubbletea program with the
submit's RequestID for cancellation routing."
```
