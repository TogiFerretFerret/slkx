# Task 2: `newmessagepicker` package skeleton

**Goal:** Create the package with the bare types (`User`, `Model`, `Result`) and the visibility methods (`Open`, `Close`, `IsVisible`, `SetUsers`, `SetCurrentUserID`). No filtering or rendering yet.

**Files:**
- Create: `internal/ui/newmessagepicker/model.go`
- Create: `internal/ui/newmessagepicker/model_test.go`

---

- [ ] **Step 1: Create the test file with the open/close tests (these will fail to compile until Step 2)**

Write `internal/ui/newmessagepicker/model_test.go`:

```go
package newmessagepicker

import "testing"

func testUsers() []User {
	return []User{
		{ID: "U1", DisplayName: "Alice Chen", Username: "alice", Recency: 500},
		{ID: "U2", DisplayName: "Bob Singh", Username: "bob", Recency: 400},
		{ID: "U3", DisplayName: "Carla Diaz", Username: "carla", Recency: 300},
		{ID: "U4", DisplayName: "Dan Evans", Username: "dan", Recency: 200},
		{ID: "U5", DisplayName: "Eva Frank", Username: "eva", IsExternal: true, Recency: 100},
	}
}

func TestNew_NotVisibleByDefault(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected new model to not be visible")
	}
}

func TestOpen_MakesVisible(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	if !m.IsVisible() {
		t.Error("expected Open() to make model visible")
	}
}

func TestClose_HidesModel(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.Close()
	if m.IsVisible() {
		t.Error("expected Close() to hide model")
	}
}

func TestOpen_ResetsState(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	// Simulate dirty state from a previous session.
	m.query = "old query"
	m.selected["U1"] = struct{}{}
	m.highlight = 3

	m.Close()
	m.Open()

	if m.query != "" {
		t.Errorf("expected empty query after Open, got %q", m.query)
	}
	if len(m.selected) != 0 {
		t.Errorf("expected empty selection after Open, got %d entries", len(m.selected))
	}
	if m.highlight != 0 {
		t.Errorf("expected highlight=0 after Open, got %d", m.highlight)
	}
}

func TestSetCurrentUserID_ExcludesSelfFromList(t *testing.T) {
	users := testUsers()
	m := New()
	m.SetCurrentUserID("U2") // Bob is "self"
	m.SetUsers(users)
	m.Open()

	for _, idx := range m.filtered {
		if m.users[idx].ID == "U2" {
			t.Error("self user U2 should not appear in filtered list")
		}
	}
}
```

- [ ] **Step 2: Create the model file**

Write `internal/ui/newmessagepicker/model.go`:

```go
// Package newmessagepicker is the Ctrl+N modal that lets the user
// start a DM (one recipient) or group DM / MPIM (2-8 recipients).
//
// The model is a self-contained Bubble Tea sub-model mirroring the
// channelfinder package: a fuzzy filter over a user list with a
// pill-bar multi-select layered on top. Submission returns a Result
// carrying the chosen user IDs; the caller (the App's mode handler)
// is responsible for dispatching the conversations.open call.
package newmessagepicker

// MaxRecipients is Slack's hard cap on the number of OTHER users in
// a multi-person direct message (MPIM). Slack itself caps total
// MPIM participants at 9, so up to 8 other users plus self.
const MaxRecipients = 8

// User is one row in the picker's list. DisplayName is the
// human-friendly name shown to the user; Username is the Slack handle
// (without the leading @). Recency is the unix-second timestamp of
// the most recent activity tied to this user; higher values sort
// earlier under empty-query and break ties under a query. IsExternal
// drives the [ext] tag in the rendered row.
type User struct {
	ID          string
	DisplayName string
	Username    string
	IsExternal  bool
	Recency     int64
}

// Result is returned by HandleKey when the user submits the picker.
// UserIDs is the list of recipients to pass to conversations.open;
// it always contains at least one ID when non-nil.
type Result struct {
	UserIDs []string
}

// Model is the picker's state. Constructed with New() and held on the
// App while ModeNewMessage is active.
type Model struct {
	users         []User
	filtered      []int // indices into users matching query + not-self
	query         string
	selected      map[string]struct{} // user IDs in the pill bar
	highlight     int                 // index into filtered
	visible       bool
	currentUserID string
}

// New constructs an empty picker. SetUsers and (optionally)
// SetCurrentUserID should be called before Open.
func New() Model {
	return Model{
		selected: map[string]struct{}{},
	}
}

// SetUsers replaces the user list the picker filters over.
// Does not trigger a re-filter; that happens on Open.
func (m *Model) SetUsers(users []User) {
	m.users = users
}

// SetCurrentUserID configures which user ID represents "self" so the
// picker can hide it from the list (a user cannot start a DM with
// themselves via this flow). May be called once at app start.
func (m *Model) SetCurrentUserID(userID string) {
	m.currentUserID = userID
}

// Open shows the picker and resets per-session state: query, pill
// selection, highlight, and recomputes the filtered list.
func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = map[string]struct{}{}
	m.highlight = 0
	m.filter()
}

// Close hides the picker. Does not clear users; Open will re-filter.
func (m *Model) Close() {
	m.visible = false
}

// IsVisible reports whether the picker is currently open.
func (m Model) IsVisible() bool {
	return m.visible
}

// filter is a placeholder until Task 3. For now it just includes
// every user except self. Order is the natural users-slice order.
func (m *Model) filter() {
	m.filtered = m.filtered[:0]
	for i, u := range m.users {
		if u.ID == m.currentUserID {
			continue
		}
		m.filtered = append(m.filtered, i)
	}
}
```

- [ ] **Step 3: Run the tests**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: 5 tests PASS.

- [ ] **Step 4: Commit**

```
git add internal/ui/newmessagepicker/
git commit -m "feat(new-message): add newmessagepicker package skeleton

Adds User, Result, Model types with Open/Close/IsVisible semantics
and a placeholder filter that excludes the current user. Real
filtering arrives in the next task."
```
