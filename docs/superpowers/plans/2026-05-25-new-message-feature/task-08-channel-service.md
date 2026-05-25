# Task 8: `ChannelService.OpenConversation` interface + adapter

**Goal:** Extend `ChannelService` with an `OpenConversation` method so the reducer can dispatch the open-conversation call without knowing about the Slack client directly. Follows the established adapter pattern (`channelAdapter` + `ChannelServiceFuncs`).

**Files:**
- Modify: `internal/ui/services.go`

---

> **SOLID note — ISP:** The `ChannelService` interface is already large (the spec calls this out at services.go:339-344). Adding a method here is the right move because conversation-opening is squarely a channel-domain concern (the result IS a channel), and pulling out a one-method interface for it would just create a second collaborator with no other callers. Rule of Three not yet reached.

- [ ] **Step 1: Add `OpenConversation` to the `ChannelService` interface**

In `internal/ui/services.go`, find the `ChannelService` interface (starts at line 346) and add this method after `MembershipFetch` (the last method, ending around line 393):

```go
	// OpenConversation dispatches conversations.open for userIDs (1
	// recipient = IM, 2-8 recipients = MPIM). Returns a tea.Cmd whose
	// resolved tea.Msg is NewMessageOpenedMsg on success or
	// NewMessageFailedMsg on error; both carry requestID so the
	// reducer can drop late results from cancelled submits.
	OpenConversation(userIDs []string, requestID uint64) tea.Cmd
```

- [ ] **Step 2: Add the closure field to `ChannelServiceFuncs`**

In the same file, find `ChannelServiceFuncs` (around line 399) and add a field after `MembershipFetch`:

```go
	OpenConversation func(userIDs []string, requestID uint64) tea.Cmd
```

- [ ] **Step 3: Add the adapter method**

After the existing `MembershipFetch` adapter method (the last one in the file, around line 481-486), append:

```go
func (c channelAdapter) OpenConversation(userIDs []string, requestID uint64) tea.Cmd {
	if c.fns.OpenConversation == nil {
		return nil
	}
	return c.fns.OpenConversation(userIDs, requestID)
}
```

- [ ] **Step 4: Verify compilation**

```
go build ./...
```

Expected: no output. The build succeeds because `noopChannelService` (the default zero-fns adapter) returns nil from the new method via the nil-check.

- [ ] **Step 5: Confirm existing channel-service tests still pass**

```
go test ./internal/ui/ -run TestChannel -v
```

Expected: existing tests PASS. (No new test is needed at this layer — `channelAdapter` is a pass-through; behavior is tested through the reducer in Task 11.)

- [ ] **Step 6: Commit**

```
git add internal/ui/services.go
git commit -m "feat(new-message): add ChannelService.OpenConversation

Adapter follows the existing ChannelServiceFuncs/channelAdapter
pattern: nil-safe (returns nil tea.Cmd when no closure is wired)
and accepts a monotonic requestID for cancellation tracking."
```
