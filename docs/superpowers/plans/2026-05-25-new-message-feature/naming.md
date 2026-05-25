# Canonical names used across all tasks

Refer to this file when a task uses a name that's defined in a different task. **Do not improvise alternative names** — later tasks reference these exactly.

## Slack client layer (Task 1)

- `slackclient.Client.OpenConversation(ctx context.Context, userIDs []string) (channelID string, alreadyOpen bool, err error)`
- `slackclient.SlackAPI.OpenConversationContext(ctx, *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)` — already in slack-go; we add it to our local interface
- Mock field: `openConversationContextFn func(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)`

## Picker package (Tasks 2–6)

- Package: `newmessagepicker`
- Import path: `github.com/gammons/slk/internal/ui/newmessagepicker`
- `Model` struct
- `User` struct: `{ID, DisplayName, Username string; IsExternal bool; Recency int64}`
- `Result` struct: `{UserIDs []string}` — non-nil with at least one ID when the user submits
- Public methods on `*Model`: `SetUsers([]User)`, `SetCurrentUserID(string)`, `Open()`, `Close()`, `IsVisible() bool`, `HandleKey(string) *Result`, `View(termWidth int) string`, `ViewOverlay(termWidth, termHeight int, background string) string`
- Constant: `MaxRecipients = 8`

## UI layer (Tasks 7, 9)

- Mode constant: `ModeNewMessage` (added to `internal/ui/mode.go`)
- Mode display string: `"NEW MSG"`
- KeyMap field: `NewMessage` with default binding `ctrl+n`
- App field: `newMessagePicker newmessagepicker.Model`
- App field: `newMessageInFlightID uint64` — monotonic counter; 0 means no in-flight request
- App field: `newMessageCancelled bool` — set true when user Escs during in-flight

## Message types (Task 7)

Added to `internal/ui/msgs.go`:

- `EnterNewMessageMsg struct{}` — dispatched when user presses Ctrl+N in `ModeNormal`
- `NewMessageOpenedMsg struct{ ChannelID string; AlreadyOpen bool; UserIDs []string; RequestID uint64 }` — service result, success
- `NewMessageFailedMsg struct{ RequestID uint64; Err error }` — service result, error

> `ConversationOpenedMsg` already exists in `msgs.go:198` for unrelated WS events. Do not reuse its name.

## Service layer (Task 8)

Added to `internal/ui/services.go`:

- `ChannelService.OpenConversation(userIDs []string, requestID uint64) tea.Cmd`
- `ChannelServiceFuncs.OpenConversation func(userIDs []string, requestID uint64) tea.Cmd`

## Reducer (Tasks 11, 12)

- File: `internal/ui/reducer_new_message.go`
- Reducer var: `reduceNewMessage reducerFunc`
- Registered in `app.go`'s `dispatchReducers(...)` call after `reduceWorkspace`

## Test fixtures

- `internal/ui/newmessagepicker/model_test.go` — uses a `testUsers()` helper that returns 5–6 representative users
- `internal/slack/client_test.go` — extends existing `mockSlackAPI` with `openConversationContextFn`
- `internal/ui/reducer_new_message_test.go` — uses `services_helpers_test.go` patterns

## Commit message style

```
feat(new-message): <one-line summary>

<optional body>
```

Use `feat(new-message)` for all task commits so the feature is easy to identify in `git log`.
