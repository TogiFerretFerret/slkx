# Task 3: Filter & rank pure functions

**Goal:** Replace the placeholder `filter()` with the real one: prefix > substring > subsequence match tiers, recency tie-break, alpha tie-break, empty-query shows all users by recency then alpha. Filtering happens over `display_name` and `username` (the `@handle`) case-insensitively.

**Files:**
- Create: `internal/ui/newmessagepicker/filter.go`
- Modify: `internal/ui/newmessagepicker/model.go` (delete placeholder `filter`, add field for the rune-typing API)
- Modify: `internal/ui/newmessagepicker/model_test.go` (add tests)

---

- [ ] **Step 1: Add the failing filter tests**

Append to `internal/ui/newmessagepicker/model_test.go`:

```go
func TestFilter_EmptyQuerySortsByRecencyDesc(t *testing.T) {
	m := New()
	m.SetUsers(testUsers()) // Alice=500, Bob=400, Carla=300, Dan=200, Eva=100
	m.Open()

	if len(m.filtered) != 5 {
		t.Fatalf("expected 5 users, got %d", len(m.filtered))
	}
	wantOrder := []string{"U1", "U2", "U3", "U4", "U5"}
	for i, want := range wantOrder {
		got := m.users[m.filtered[i]].ID
		if got != want {
			t.Errorf("position %d: want %s, got %s", i, want, got)
		}
	}
}

func TestFilter_EmptyQueryTieBreaksAlphabetically(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "Charlie", Username: "c", Recency: 100},
		{ID: "U2", DisplayName: "Alice", Username: "a", Recency: 100},
		{ID: "U3", DisplayName: "Bob", Username: "b", Recency: 100},
	}
	m := New()
	m.SetUsers(users)
	m.Open()

	wantOrder := []string{"U2", "U3", "U1"} // Alice, Bob, Charlie
	for i, want := range wantOrder {
		got := m.users[m.filtered[i]].ID
		if got != want {
			t.Errorf("position %d: want %s, got %s", i, want, got)
		}
	}
}

func TestFilter_PrefixBeatsSubstring(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "Marcus", Username: "marcus", Recency: 100},
		{ID: "U2", DisplayName: "Alice Marketing", Username: "amark", Recency: 999},
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	m.setQuery("mar")

	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(m.filtered))
	}
	if m.users[m.filtered[0]].ID != "U1" {
		t.Errorf("prefix match should come first, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestFilter_SubstringBeatsSubsequence(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "Stephanie", Username: "steph", Recency: 100},   // contains "eph"
		{ID: "U2", DisplayName: "Edward Phillips", Username: "ep", Recency: 999}, // subseq e-p-h
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	m.setQuery("eph")

	if m.filtered[0] != 0 {
		t.Errorf("substring match should rank first, got user at index %d", m.filtered[0])
	}
}

func TestFilter_CaseInsensitive(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.setQuery("ALICE")

	if len(m.filtered) == 0 {
		t.Fatal("expected at least 1 match for ALICE")
	}
	if m.users[m.filtered[0]].ID != "U1" {
		t.Errorf("expected Alice (U1) as first match, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestFilter_MatchesUsernameHandle(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.setQuery("dan") // Dan Evans has Username="dan"

	if len(m.filtered) == 0 {
		t.Fatal("expected match for handle 'dan'")
	}
	if m.users[m.filtered[0]].ID != "U4" {
		t.Errorf("expected Dan (U4) as first match, got %s", m.users[m.filtered[0]].ID)
	}
}

func TestFilter_NoMatchesReturnsEmpty(t *testing.T) {
	m := New()
	m.SetUsers(testUsers())
	m.Open()
	m.setQuery("xyzqq")

	if len(m.filtered) != 0 {
		t.Errorf("expected 0 matches for unmatchable query, got %d", len(m.filtered))
	}
}

func TestFilter_ExcludesSelfEvenOnMatch(t *testing.T) {
	users := testUsers()
	m := New()
	m.SetCurrentUserID("U1") // Alice is self
	m.SetUsers(users)
	m.Open()
	m.setQuery("alice")

	for _, idx := range m.filtered {
		if m.users[idx].ID == "U1" {
			t.Error("self user should be excluded even when query matches")
		}
	}
}

func TestFilter_RecencyTieBreaksWithinSameTier(t *testing.T) {
	users := []User{
		{ID: "U1", DisplayName: "alice older", Username: "a1", Recency: 100},
		{ID: "U2", DisplayName: "alice newer", Username: "a2", Recency: 999},
	}
	m := New()
	m.SetUsers(users)
	m.Open()
	m.setQuery("alice") // both prefix-match

	if m.users[m.filtered[0]].ID != "U2" {
		t.Errorf("higher-recency match should come first, got %s", m.users[m.filtered[0]].ID)
	}
}
```

- [ ] **Step 2: Add `setQuery` test helper to the model**

In `internal/ui/newmessagepicker/model.go`, add this method after `filter()`:

```go
// setQuery is a test helper: replaces the query and refilters. The
// real keystroke API in Task 4 will go through HandleKey.
func (m *Model) setQuery(q string) {
	m.query = q
	m.highlight = 0
	m.filter()
}
```

- [ ] **Step 3: Run tests to confirm they fail**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: the new tests FAIL — the placeholder filter doesn't honor `query`, recency, or tiering. Older tests still pass.

- [ ] **Step 4: Create `filter.go` with the real ranking**

Write `internal/ui/newmessagepicker/filter.go`:

```go
package newmessagepicker

import (
	"sort"
	"strings"
)

// filter rebuilds m.filtered from m.users honoring the current query,
// the current-user exclusion, and the ranking rules:
//
//  1. Match tier (only when query non-empty):
//     0 = prefix on display name OR username
//     1 = substring on display name OR username
//     2 = subsequence on display name OR username
//     Non-matchers are dropped.
//  2. Recency DESC.
//  3. DisplayName ASC (case-insensitive).
//
// Empty query: include all users (minus self) sorted by Recency DESC
// then DisplayName ASC.
func (m *Model) filter() {
	m.filtered = m.filtered[:0]
	q := strings.ToLower(m.query)

	if q == "" {
		for i, u := range m.users {
			if u.ID == m.currentUserID {
				continue
			}
			m.filtered = append(m.filtered, i)
		}
		sort.SliceStable(m.filtered, func(i, j int) bool {
			return m.lessNoQuery(m.filtered[i], m.filtered[j])
		})
		return
	}

	type match struct {
		idx  int
		tier int
	}
	var matches []match
	for i, u := range m.users {
		if u.ID == m.currentUserID {
			continue
		}
		tier, ok := matchTier(u, q)
		if !ok {
			continue
		}
		matches = append(matches, match{idx: i, tier: tier})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		ua, ub := m.users[a.idx], m.users[b.idx]
		if ua.Recency != ub.Recency {
			return ua.Recency > ub.Recency
		}
		return strings.ToLower(ua.DisplayName) < strings.ToLower(ub.DisplayName)
	})

	for _, mm := range matches {
		m.filtered = append(m.filtered, mm.idx)
	}
}

// matchTier returns (tier, true) if u matches q on either its
// DisplayName or its Username. tier 0 = prefix, 1 = substring,
// 2 = subsequence. q is expected to already be lower-cased.
func matchTier(u User, q string) (int, bool) {
	name := strings.ToLower(u.DisplayName)
	handle := strings.ToLower(u.Username)
	if strings.HasPrefix(name, q) || strings.HasPrefix(handle, q) {
		return 0, true
	}
	if strings.Contains(name, q) || strings.Contains(handle, q) {
		return 1, true
	}
	if isSubsequence(name, q) || isSubsequence(handle, q) {
		return 2, true
	}
	return 0, false
}

// isSubsequence reports whether every rune of q appears in s in
// order. Both inputs are expected to already be lower-cased.
func isSubsequence(s, q string) bool {
	qi := 0
	qrunes := []rune(q)
	if len(qrunes) == 0 {
		return true
	}
	for _, r := range s {
		if qi >= len(qrunes) {
			break
		}
		if r == qrunes[qi] {
			qi++
		}
	}
	return qi == len(qrunes)
}

// lessNoQuery is the comparator used when the query is empty:
// Recency DESC, then DisplayName ASC.
func (m *Model) lessNoQuery(ai, bi int) bool {
	a, b := m.users[ai], m.users[bi]
	if a.Recency != b.Recency {
		return a.Recency > b.Recency
	}
	return strings.ToLower(a.DisplayName) < strings.ToLower(b.DisplayName)
}
```

- [ ] **Step 5: Remove the placeholder filter from `model.go`**

In `internal/ui/newmessagepicker/model.go`, delete the entire placeholder `filter()` function (the one with the comment "filter is a placeholder until Task 3"). The real implementation lives in `filter.go` now.

- [ ] **Step 6: Run tests to confirm they pass**

```
go test ./internal/ui/newmessagepicker/ -v
```

Expected: ALL tests PASS (the 5 from Task 2 plus the 9 added in this task).

- [ ] **Step 7: Commit**

```
git add internal/ui/newmessagepicker/
git commit -m "feat(new-message): filter and rank users in the picker

Prefix > substring > subsequence tiers, with recency DESC and
display name ASC tie-breaks. Matches against both display name
and the @handle, case-insensitively. Self is excluded from the
filtered list even on a query match."
```
