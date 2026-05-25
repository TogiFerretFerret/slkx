# Task 1: Slack client `OpenConversation` wrapper

**Goal:** Add `Client.OpenConversation` that wraps slack-go's `OpenConversationContext`. Defends inputs (1 ≤ N ≤ 8 user IDs), filters out the current user defensively, and returns the channel ID + `alreadyOpen` flag.

**Files:**
- Modify: `internal/slack/client.go` (interface + new method)
- Modify: `internal/slack/client_test.go` (mock field + tests)

---

- [ ] **Step 1: Add a failing test for the 1-user (IM) happy path**

Open `internal/slack/client_test.go`. Find the `mockSlackAPI` struct definition (around line 133) and add a new field at the bottom of the field list:

```go
	openConversationContextFn       func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
```

At the end of the file, append the mock method:

```go
func (m *mockSlackAPI) OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
	if m.openConversationContextFn != nil {
		return m.openConversationContextFn(ctx, params)
	}
	return nil, false, false, nil
}
```

Then add this test at the end of the file:

```go
func TestOpenConversation_SingleUserReturnsIMChannelID(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			if len(params.Users) != 1 || params.Users[0] != "U123" {
				t.Errorf("expected Users=[U123], got %v", params.Users)
			}
			if !params.ReturnIM {
				t.Error("expected ReturnIM=true")
			}
			return &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{ID: "D456"},
				},
			}, false, false, nil
		},
	}
	c := &Client{api: mock}

	channelID, alreadyOpen, err := c.OpenConversation(context.Background(), []string{"U123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "D456" {
		t.Errorf("expected channelID=D456, got %q", channelID)
	}
	if alreadyOpen {
		t.Error("expected alreadyOpen=false")
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```
go test ./internal/slack/ -run TestOpenConversation_SingleUserReturnsIMChannelID -v
```

Expected: build fails with `mockSlackAPI does not implement SlackAPI (missing method OpenConversationContext)` OR `c.OpenConversation undefined`. Either way it must not pass.

- [ ] **Step 3: Add `OpenConversationContext` to the `SlackAPI` interface**

In `internal/slack/client.go`, add a line to the `SlackAPI` interface (after line 46, before the closing brace at line 47):

```go
	OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
```

- [ ] **Step 4: Add `Client.OpenConversation` method**

In `internal/slack/client.go`, after the `SendMessage` method (which ends at line 640), insert:

```go
// OpenConversation opens (or returns) a direct message channel (1 user)
// or a multi-person direct message / MPIM (2-8 users). Idempotent:
// when the conversation already exists, Slack returns it with
// alreadyOpen=true.
//
// Defends inputs: rejects 0 users or more than 8. Slack's hard cap on
// MPIM size is 9 participants total, so up to 8 OTHER user IDs.
func (c *Client) OpenConversation(ctx context.Context, userIDs []string) (channelID string, alreadyOpen bool, err error) {
	if len(userIDs) == 0 {
		return "", false, fmt.Errorf("OpenConversation: at least one user ID required")
	}
	if len(userIDs) > 8 {
		return "", false, fmt.Errorf("OpenConversation: at most 8 user IDs allowed (got %d)", len(userIDs))
	}
	ch, _, alreadyOpen, err := c.api.OpenConversationContext(ctx, &slack.OpenConversationParameters{
		Users:    userIDs,
		ReturnIM: true,
	})
	if err != nil {
		return "", false, fmt.Errorf("OpenConversation: %w", err)
	}
	return ch.ID, alreadyOpen, nil
}
```

- [ ] **Step 5: Run the test to confirm it passes**

```
go test ./internal/slack/ -run TestOpenConversation_SingleUserReturnsIMChannelID -v
```

Expected: `PASS`.

- [ ] **Step 6: Add tests for the remaining 5 cases**

Append these tests to `internal/slack/client_test.go`:

```go
func TestOpenConversation_MultipleUsersReturnsMPIMChannelID(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			if len(params.Users) != 3 {
				t.Errorf("expected 3 users, got %d", len(params.Users))
			}
			return &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{ID: "G789"},
				},
			}, false, false, nil
		},
	}
	c := &Client{api: mock}

	channelID, _, err := c.OpenConversation(context.Background(), []string{"U1", "U2", "U3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "G789" {
		t.Errorf("expected channelID=G789, got %q", channelID)
	}
}

func TestOpenConversation_EmptyUserIDsReturnsErrorWithoutAPICall(t *testing.T) {
	called := false
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			called = true
			return nil, false, false, nil
		},
	}
	c := &Client{api: mock}

	_, _, err := c.OpenConversation(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty userIDs")
	}
	if called {
		t.Error("API should not have been called")
	}
}

func TestOpenConversation_TooManyUserIDsReturnsErrorWithoutAPICall(t *testing.T) {
	called := false
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			called = true
			return nil, false, false, nil
		},
	}
	c := &Client{api: mock}

	nine := []string{"U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8", "U9"}
	_, _, err := c.OpenConversation(context.Background(), nine)
	if err == nil {
		t.Fatal("expected error for 9 userIDs")
	}
	if called {
		t.Error("API should not have been called")
	}
}

func TestOpenConversation_APIErrorIsWrapped(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			return nil, false, false, fmt.Errorf("rate_limited")
		},
	}
	c := &Client{api: mock}

	_, _, err := c.OpenConversation(context.Background(), []string{"U1"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "OpenConversation") {
		t.Errorf("expected error to be wrapped with OpenConversation prefix, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "rate_limited") {
		t.Errorf("expected error to contain underlying message, got %q", err.Error())
	}
}

func TestOpenConversation_AlreadyOpenFlagPropagates(t *testing.T) {
	mock := &mockSlackAPI{
		openConversationContextFn: func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error) {
			return &slack.Channel{
				GroupConversation: slack.GroupConversation{
					Conversation: slack.Conversation{ID: "D1"},
				},
			}, false, true, nil
		},
	}
	c := &Client{api: mock}

	_, alreadyOpen, err := c.OpenConversation(context.Background(), []string{"U1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !alreadyOpen {
		t.Error("expected alreadyOpen=true")
	}
}
```

If `strings` isn't already imported in the test file, add it to the import block at the top.

- [ ] **Step 7: Run all new tests**

```
go test ./internal/slack/ -run TestOpenConversation -v
```

Expected: 5 tests, all PASS.

- [ ] **Step 8: Run the full slack package test suite to confirm no regressions**

```
go test ./internal/slack/
```

Expected: `ok  github.com/gammons/slk/internal/slack`.

- [ ] **Step 9: Commit**

```
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(new-message): add Client.OpenConversation wrapper

Wraps slack-go's OpenConversationContext for the upcoming new-message
modal. Validates input (1-8 user IDs), forwards the alreadyOpen flag,
and wraps API errors with an OpenConversation: prefix."
```
