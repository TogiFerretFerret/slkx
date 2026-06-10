# Open Links Keybinding + In-App Slack Permalink Navigation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Press `o` on a selected message to open its link (picker modal for multiple links); Slack archive permalinks for the active workspace navigate in-app (channel / message / thread) instead of opening the browser.

**Architecture:** Elm-style Bubble Tea app. A new `OpenLinkMsg` is the single routing point, handled by a new `reduceLinks` reducer: Slack permalinks (parsed by a new pure `internal/slackurl` package) matching the active workspace domain dispatch `ChannelSelectedMsg` + a pending-nav completion (select-by-ts or thread open); everything else launches the OS browser. A new `internal/ui/linkpicker` modal lists links when a message has more than one.

**Tech Stack:** Go, bubbletea v2, lipgloss v2, existing reducer/mode/service patterns in `internal/ui`.

**Spec:** `docs/superpowers/specs/2026-06-09-open-links-keybinding-design.md`

**Verify after every task:** `go build ./... && go test ./...` from repo root.

---

### Task 1: Permalink parser (`internal/slackurl`)

**Files:**
- Create: `internal/slackurl/slackurl.go`
- Create: `internal/slackurl/slackurl_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/slackurl/slackurl_test.go
package slackurl

import "testing"

func TestParse_Valid(t *testing.T) {
	pl, ok := Parse("https://truelist-workspace.slack.com/archives/C054JFCBN69/p1779284733270139")
	if !ok {
		t.Fatal("expected ok")
	}
	if pl.Subdomain != "truelist-workspace" {
		t.Errorf("Subdomain = %q", pl.Subdomain)
	}
	if string(pl.ChannelID) != "C054JFCBN69" {
		t.Errorf("ChannelID = %q", pl.ChannelID)
	}
	if string(pl.MessageTS) != "1779284733.270139" {
		t.Errorf("MessageTS = %q", pl.MessageTS)
	}
	if pl.ThreadTS != "" {
		t.Errorf("ThreadTS = %q, want empty", pl.ThreadTS)
	}
}

func TestParse_ThreadTSAndCid(t *testing.T) {
	pl, ok := Parse("https://example.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999")
	if !ok {
		t.Fatal("expected ok")
	}
	if string(pl.ThreadTS) != "1700000000.000100" {
		t.Errorf("ThreadTS = %q", pl.ThreadTS)
	}
	if string(pl.ChannelID) != "C999" {
		t.Errorf("ChannelID = %q (path channel ID wins; cid ignored)", pl.ChannelID)
	}
	if string(pl.MessageTS) != "1700000050.000400" {
		t.Errorf("MessageTS = %q", pl.MessageTS)
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := []string{
		"https://github.com/foo/bar",                              // non-slack host
		"http://example.slack.com/archives/C999/p1700000050000400", // http, not https
		"https://slack.com/archives/C999/p1700000050000400",        // no subdomain
		"https://example.slack.com/archives/C999",                  // no message component
		"https://example.slack.com/archives/C999/p12345",           // p-value too short
		"https://example.slack.com/archives/C999/pabcdef",          // p-value not digits
		"https://example.slack.com/messages/C999/p1700000050000400", // not /archives/
		"mailto:foo@example.com",
		"not a url at all",
		"",
	}
	for _, c := range cases {
		if _, ok := Parse(c); ok {
			t.Errorf("Parse(%q) ok = true, want false", c)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slackurl/ -v`
Expected: FAIL (package does not exist / `Parse` undefined)

- [ ] **Step 3: Write the implementation**

```go
// Package slackurl parses Slack archive permalinks
// (https://<subdomain>.slack.com/archives/<CHANNEL>/p<digits>) into
// their components so the UI can navigate to the referenced
// conversation in-app instead of opening a browser.
//
// Pure functions only: no I/O, no dependencies beyond net/url and the
// ids types.
package slackurl

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/gammons/slk/internal/ids"
)

// Permalink is the parsed form of a Slack archive permalink.
type Permalink struct {
	// Subdomain is the workspace subdomain, e.g. "truelist-workspace"
	// for truelist-workspace.slack.com.
	Subdomain string
	ChannelID ids.ChannelID
	// MessageTS is the target message timestamp ("1779284733.270139"),
	// decoded from the path's p-value by inserting a dot before the
	// last six digits.
	MessageTS ids.MessageTS
	// ThreadTS is the thread parent ts from the thread_ts query
	// parameter; empty when the link targets a channel-level message.
	ThreadTS ids.ThreadTS
}

// archivePathRe matches "/archives/<CHANNEL>/p<digits>". The p-value
// must be long enough to split into seconds + 6 digits of microseconds
// (Slack always emits 16 digits; we accept >= 11 to be lenient about
// future second-counter widths but reject obviously-wrong values).
var archivePathRe = regexp.MustCompile(`^/archives/([A-Z0-9]+)/p(\d{11,})$`)

// Parse decodes a Slack archive permalink. ok is false for anything
// that is not an https://<subdomain>.slack.com/archives/... message
// link. The cid query parameter is accepted but ignored — the channel
// ID in the path wins.
func Parse(rawURL string) (Permalink, bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return Permalink{}, false
	}
	host := u.Host
	if !strings.HasSuffix(host, ".slack.com") {
		return Permalink{}, false
	}
	sub := strings.TrimSuffix(host, ".slack.com")
	if sub == "" {
		return Permalink{}, false
	}
	m := archivePathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return Permalink{}, false
	}
	digits := m[2]
	ts := digits[:len(digits)-6] + "." + digits[len(digits)-6:]
	return Permalink{
		Subdomain: sub,
		ChannelID: ids.ChannelID(m[1]),
		MessageTS: ids.MessageTS(ts),
		ThreadTS:  ids.ThreadTS(u.Query().Get("thread_ts")),
	}, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/slackurl/ -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Commit**

```bash
git add internal/slackurl/
git commit -m "feat: add slackurl package to parse Slack archive permalinks"
```

---

### Task 2: Link extraction helper (`messages.ExtractLinks`)

**Files:**
- Create: `internal/ui/messages/links.go`
- Create: `internal/ui/messages/links_test.go`

Reuses the existing `linkWithLabelRe` / `linkBareRe` regexes (`internal/ui/messages/render.go:36-37`) — no changes to render.go.

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/messages/links_test.go
package messages

import (
	"reflect"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Link
	}{
		{
			name: "no links",
			text: "just some text with <@U123> and <#C123|general>",
			want: nil,
		},
		{
			name: "bare link",
			text: "see <https://example.com/page>",
			want: []Link{{URL: "https://example.com/page"}},
		},
		{
			name: "labeled link",
			text: "see <https://example.com/page|the docs>",
			want: []Link{{URL: "https://example.com/page", Label: "the docs"}},
		},
		{
			name: "mixed, order of appearance",
			text: "<https://a.example/1|first> then <https://b.example/2>",
			want: []Link{
				{URL: "https://a.example/1", Label: "first"},
				{URL: "https://b.example/2"},
			},
		},
		{
			name: "duplicate URLs deduped",
			text: "<https://a.example/1> and again <https://a.example/1|same>",
			want: []Link{{URL: "https://a.example/1"}},
		},
		{
			name: "mailto",
			text: "<mailto:foo@example.com|foo@example.com>",
			want: []Link{{URL: "mailto:foo@example.com", Label: "foo@example.com"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractLinks(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ExtractLinks(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages/ -run TestExtractLinks -v`
Expected: FAIL with "undefined: Link" / "undefined: ExtractLinks"

- [ ] **Step 3: Write the implementation**

```go
// internal/ui/messages/links.go
//
// ExtractLinks pulls http(s)/mailto links out of a message's mrkdwn
// text using the same regexes the renderer uses (render.go), so the
// "open link" keybinding sees exactly the links the user sees.
package messages

import (
	"sort"
	"strings"
)

// Link is one link found in a message's text.
type Link struct {
	URL   string
	Label string // empty for bare <url> links
}

// ExtractLinks returns the links in text in order of appearance,
// deduplicated by URL (first occurrence wins). Returns nil when text
// has no links.
func ExtractLinks(text string) []Link {
	type posLink struct {
		start int
		link  Link
	}
	var found []posLink
	for _, m := range linkWithLabelRe.FindAllStringSubmatchIndex(text, -1) {
		found = append(found, posLink{
			start: m[0],
			link:  Link{URL: text[m[2]:m[3]], Label: text[m[4]:m[5]]},
		})
	}
	for _, m := range linkBareRe.FindAllStringSubmatchIndex(text, -1) {
		url := text[m[2]:m[3]]
		// linkBareRe also matches the labeled form (its [^>]+ spans
		// the "|label" part); those were already captured above.
		if strings.Contains(url, "|") {
			continue
		}
		found = append(found, posLink{start: m[0], link: Link{URL: url}})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].start < found[j].start })
	var out []Link
	seen := make(map[string]bool, len(found))
	for _, f := range found {
		if seen[f.link.URL] {
			continue
		}
		seen[f.link.URL] = true
		out = append(out, f.link)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/messages/ -run TestExtractLinks -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/links.go internal/ui/messages/links_test.go
git commit -m "feat: add messages.ExtractLinks for pulling links out of mrkdwn text"
```

---

### Task 3: `SelectByTS` on the messages model

**Files:**
- Modify: `internal/ui/messages/model.go` (add method next to `SelectByIndex`, ~line 797)
- Create: `internal/ui/messages/selectbyts_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/messages/selectbyts_test.go
package messages

import "testing"

func TestSelectByTS_Found(t *testing.T) {
	m := New()
	m.SetMessages([]MessageItem{
		{TS: "1.000001", Text: "a"},
		{TS: "2.000002", Text: "b"},
		{TS: "3.000003", Text: "c"},
	})
	if !m.SelectByTS("2.000002") {
		t.Fatal("expected SelectByTS to return true")
	}
	if m.SelectedIndex() != 1 {
		t.Errorf("SelectedIndex = %d, want 1", m.SelectedIndex())
	}
	sel, ok := m.SelectedMessage()
	if !ok || sel.TS != "2.000002" {
		t.Errorf("SelectedMessage = %+v ok=%v", sel, ok)
	}
}

func TestSelectByTS_NotFound(t *testing.T) {
	m := New()
	m.SetMessages([]MessageItem{{TS: "1.000001", Text: "a"}})
	if m.SelectByTS("9.999999") {
		t.Error("expected false for missing ts")
	}
	if m.SelectedIndex() != 0 {
		t.Errorf("selection moved: %d", m.SelectedIndex())
	}
	if m.SelectByTS("") {
		t.Error("expected false for empty ts")
	}
}
```

Note: if `New()` is not the messages model's constructor name, check the top of `internal/ui/messages/model.go` and use the existing constructor (other tests in that package show the convention).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages/ -run TestSelectByTS -v`
Expected: FAIL with "undefined: ... SelectByTS"

- [ ] **Step 3: Write the implementation** (insert after `SelectByIndex`, model.go:797)

```go
// SelectByTS moves the selection cursor to the message with the given
// ts and forces the next View() to re-snap the viewport to it.
// Returns false (selection unchanged) when no message with that ts is
// in the loaded buffer. Used by the permalink-navigation path to land
// on the exact message a Slack archive link points at.
func (m *Model) SelectByTS(ts string) bool {
	if ts == "" {
		return false
	}
	for i := range m.messages {
		if m.messages[i].TS == ts {
			m.selected = i
			m.hasSnapped = false
			m.dirty()
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/messages/ -run TestSelectByTS -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/messages/selectbyts_test.go
git commit -m "feat: add SelectByTS to messages model for permalink jumps"
```

---

### Task 4: Workspace domain plumbing

**Files:**
- Modify: `internal/slack/client.go` (Connect, ~line 171; new accessor + helper near `deriveAPIBaseURL`)
- Create: `internal/slack/teamurl_test.go`
- Modify: `internal/ui/msgs.go` (add `Domain` to `WorkspaceReadyMsg` and `WorkspaceSwitchedMsg`)
- Modify: `internal/ui/app.go` (App struct field + NewApp init)
- Modify: `internal/ui/reducer_workspace.go` (record domain in both arms)
- Modify: `cmd/slk/main.go` (populate `Domain` in both msg constructions, lines ~1409 and ~1562)

- [ ] **Step 1: Write the failing test for the subdomain helper**

```go
// internal/slack/teamurl_test.go
package slack

import "testing"

func TestSubdomainFromTeamURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://truelist-workspace.slack.com/", "truelist-workspace"},
		{"https://hackclub.enterprise.slack.com/", "hackclub.enterprise"},
		{"https://slack.com/", ""},
		{"https://evil.example.com/", ""},
		{"", ""},
		{"::not-a-url::", ""},
	}
	for _, c := range cases {
		if got := subdomainFromTeamURL(c.in); got != c.want {
			t.Errorf("subdomainFromTeamURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

Note: confirm the package name at the top of `internal/slack/client.go` (it may be `slack` or `slackclient`); match it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -run TestSubdomainFromTeamURL -v`
Expected: FAIL with "undefined: subdomainFromTeamURL"

- [ ] **Step 3: Implement in client.go**

Add a field to the `Client` struct (near `apiBaseURL`):

```go
	// teamURL is the raw workspace URL from auth.test's response
	// (e.g. "https://truelist-workspace.slack.com/"). Used to derive
	// the workspace subdomain for in-app permalink routing.
	teamURL string
```

In `Connect` (client.go, after `c.apiBaseURL = deriveAPIBaseURL(resp.URL)` at line 171):

```go
	c.teamURL = resp.URL
```

Add below `deriveAPIBaseURL`:

```go
// TeamSubdomain returns the workspace's subdomain under .slack.com
// (e.g. "truelist-workspace" for truelist-workspace.slack.com), or ""
// before Connect or when the auth.test URL was not a *.slack.com host.
func (c *Client) TeamSubdomain() string {
	return subdomainFromTeamURL(c.teamURL)
}

// subdomainFromTeamURL extracts the subdomain from a workspace URL.
// Only hosts strictly under .slack.com produce a non-empty result.
func subdomainFromTeamURL(teamURL string) string {
	if teamURL == "" {
		return ""
	}
	u, err := url.Parse(teamURL)
	if err != nil || u.Host == "" {
		return ""
	}
	if !strings.HasSuffix(u.Host, ".slack.com") {
		return ""
	}
	return strings.TrimSuffix(u.Host, ".slack.com")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/slack/ -run TestSubdomainFromTeamURL -v`
Expected: PASS

- [ ] **Step 5: Plumb Domain through the workspace messages**

In `internal/ui/msgs.go` add to **both** `WorkspaceReadyMsg` (after `TeamName`, ~line 246) and `WorkspaceSwitchedMsg` (after `TeamName`, ~line 186):

```go
		// Domain is the workspace's slack.com subdomain (e.g.
		// "truelist-workspace"), from auth.test. Used to decide
		// whether an archive permalink belongs to this workspace.
		Domain string
```

In `internal/ui/app.go` App struct (near `lastChannelByTeam`, ~line 235):

```go
	// workspaceDomains maps teamID -> slack.com subdomain, recorded
	// from WorkspaceReadyMsg / WorkspaceSwitchedMsg. Read by the link
	// router to match permalink hosts against the active workspace.
	workspaceDomains map[string]string
```

In `NewApp`'s struct literal (alongside `navHistory: newNavHistoryStore(),` ~app.go:400):

```go
		workspaceDomains:     map[string]string{},
```

Add helper (anywhere sensible in app.go, e.g. near `activeTeamName`):

```go
// activeWorkspaceDomain returns the slack.com subdomain of the active
// workspace, or "" when unknown (link router then falls back to the
// browser for all slack.com permalinks).
func (a *App) activeWorkspaceDomain() string {
	return a.workspaceDomains[a.activeTeamID]
}
```

In `internal/ui/reducer_workspace.go`:
- `reduceWorkspaceReady` (after `a.bootstrap.MarkReady(m.TeamName)`, line 154):

```go
	if m.Domain != "" {
		a.workspaceDomains[m.TeamID] = m.Domain
	}
```

- `reduceWorkspaceSwitched` (after the upload guard, line 225):

```go
	if m.Domain != "" {
		a.workspaceDomains[m.TeamID] = m.Domain
	}
```

In `cmd/slk/main.go`:
- `WorkspaceReadyMsg` literal (~line 1562): add `Domain: wctx.Client.TeamSubdomain(),` after `TeamName`.
- `WorkspaceSwitchedMsg` literal (~line 1409): add `Domain: wctx.Client.TeamSubdomain(),` after `TeamName`.

- [ ] **Step 6: Build and run full tests**

Run: `go build ./... && go test ./...`
Expected: PASS (no behavior change yet; just plumbing)

- [ ] **Step 7: Commit**

```bash
git add internal/slack/ internal/ui/msgs.go internal/ui/app.go internal/ui/reducer_workspace.go cmd/slk/main.go
git commit -m "feat: plumb workspace slack.com subdomain from auth.test into the UI"
```

---

### Task 5: `OpenLinkMsg` routing reducer + pending navigation

**Files:**
- Create: `internal/ui/reducer_links.go`
- Create: `internal/ui/reducer_links_test.go`
- Modify: `internal/ui/msgs.go` (add `OpenLinkMsg`)
- Modify: `internal/ui/app.go` (App fields, NewApp init, `openURLCmd`, reducer chain at line 457)
- Modify: `internal/ui/reducer_channels.go` (completion hooks)
- Modify: `internal/ui/reducer_threads.go` (parent backfill, ~line 97)

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/reducer_links_test.go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// linkTestApp builds an App wired for link-routing tests: active
// workspace T1 with domain "myteam", channel C054JFCBN69 known to
// Lookup, and a captured browser opener.
func linkTestApp(t *testing.T) (*App, *string) {
	t.Helper()
	app := NewApp()
	app.activeTeamID = "T1"
	app.workspaceDomains["T1"] = "myteam"
	var opened string
	app.browserOpener = func(url string) tea.Cmd {
		opened = url
		return nil
	}
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		if channelID == "C054JFCBN69" {
			return "general", "channel", true
		}
		return "", "", false
	})
	return app, &opened
}

// drainCmd executes cmd (recursively draining tea.BatchMsg) and
// returns every resolved tea.Msg.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	var out []tea.Msg
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	if msg != nil {
		out = append(out, msg)
	}
	return out
}

func TestOpenLink_NonSlackURL_OpensBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	_, cmd := app.Update(OpenLinkMsg{URL: "https://github.com/foo/bar"})
	drainCmd(cmd)
	if *opened != "https://github.com/foo/bar" {
		t.Errorf("browser opened %q", *opened)
	}
}

func TestOpenLink_ForeignWorkspace_OpensBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	url := "https://otherteam.slack.com/archives/C054JFCBN69/p1779284733270139"
	_, cmd := app.Update(OpenLinkMsg{URL: url})
	drainCmd(cmd)
	if *opened != url {
		t.Errorf("browser opened %q, want %q", *opened, url)
	}
}

func TestOpenLink_UnknownChannel_OpensBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	url := "https://myteam.slack.com/archives/CUNKNOWN1/p1779284733270139"
	_, cmd := app.Update(OpenLinkMsg{URL: url})
	drainCmd(cmd)
	if *opened != url {
		t.Errorf("browser opened %q, want %q", *opened, url)
	}
}

func TestOpenLink_OtherChannel_DispatchesChannelSelected(t *testing.T) {
	app, opened := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	msgs := drainCmd(cmd)
	var sel *ChannelSelectedMsg
	for _, m := range msgs {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			sel = &cs
		}
	}
	if sel == nil {
		t.Fatalf("no ChannelSelectedMsg in %#v", msgs)
	}
	if sel.ID != "C054JFCBN69" || sel.Name != "general" || sel.Type != "channel" {
		t.Errorf("ChannelSelectedMsg = %+v", sel)
	}
	if app.pendingLinkNav == nil || app.pendingLinkNav.messageTS != "1779284733.270139" {
		t.Errorf("pendingLinkNav = %+v", app.pendingLinkNav)
	}
	if *opened != "" {
		t.Errorf("browser should not open, got %q", *opened)
	}
}

func TestOpenLink_ActiveChannel_SelectsMessage(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1779284733.270139", Text: "target"},
		{TS: "1779284734.000000", Text: "newer"},
	})
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	drainCmd(cmd)
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1779284733.270139" {
		t.Errorf("selected = %+v ok=%v", sel, ok)
	}
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav not cleared: %+v", app.pendingLinkNav)
	}
}

func TestOpenLink_ActiveChannel_TSNotLoaded_Toasts(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1779284734.000000", Text: "only newer"},
	})
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	msgs := drainCmd(cmd)
	foundToast := false
	for _, m := range msgs {
		if _, ok := m.(ToastMsg); ok {
			foundToast = true
		}
	}
	if !foundToast {
		t.Errorf("expected ToastMsg, got %#v", msgs)
	}
}

func TestOpenLink_ThreadPermalink_OpensThread(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	var fetchedChannel, fetchedThread string
	app.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetchedChannel, fetchedThread = string(channelID), string(threadTS)
		return nil
	})
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"})
	drainCmd(cmd)
	if !app.threadVisible {
		t.Fatal("thread panel not visible")
	}
	if got := app.threadPanel.ThreadTS(); got != "1779284700.000100" {
		t.Errorf("ThreadTS = %q", got)
	}
	if fetchedChannel != "C054JFCBN69" || fetchedThread != "1779284700.000100" {
		t.Errorf("fetch = (%q, %q)", fetchedChannel, fetchedThread)
	}
}

func TestMessagesLoaded_CompletesPendingNav(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	app.pendingLinkNav = &pendingLinkNav{
		channelID: "C054JFCBN69",
		messageTS: "1779284733.270139",
	}
	_, cmd := app.Update(MessagesLoadedMsg{
		ChannelID: "C054JFCBN69",
		Messages: []messages.MessageItem{
			{TS: "1779284733.270139", Text: "target"},
			{TS: "1779284734.000000", Text: "newer"},
		},
	})
	drainCmd(cmd)
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1779284733.270139" {
		t.Errorf("selected = %+v ok=%v", sel, ok)
	}
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav not cleared: %+v", app.pendingLinkNav)
	}
}

func TestChannelSelected_DifferentChannel_DropsPendingNav(t *testing.T) {
	app, _ := linkTestApp(t)
	app.pendingLinkNav = &pendingLinkNav{channelID: "C054JFCBN69", messageTS: "1.0"}
	_, cmd := app.Update(ChannelSelectedMsg{ID: "COTHER", Name: "other", Type: "channel"})
	drainCmd(cmd)
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav should be dropped on unrelated navigation: %+v", app.pendingLinkNav)
	}
}
```

Note: check `ThreadServiceFuncs.Fetch`'s exact signature in `internal/ui/services.go` / `callbacks.go` (the `setThreadFetcherForTest` helper at `services_helpers_test.go:25` shows the func type) and adjust the fake's signature to match.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestOpenLink|TestMessagesLoaded_CompletesPendingNav|TestChannelSelected_DifferentChannel' -v`
Expected: FAIL with "undefined: OpenLinkMsg" etc.

- [ ] **Step 3: Add `OpenLinkMsg` to msgs.go** (after `ToastMsg`, ~line 317)

```go
// OpenLinkMsg requests that a URL be opened. This is the single
// routing point for all link opens (issue #62): the reduceLinks
// reducer either navigates in-app (Slack archive permalinks for the
// active workspace) or launches the OS browser. Dispatched by the
// `o` keybinding (directly for single-link messages) and by the link
// picker modal.
type OpenLinkMsg struct{ URL string }
```

- [ ] **Step 4: Add App fields, `openURLCmd`, and reducer registration in app.go**

App struct (near `workspaceDomains` from Task 4):

```go
	// pendingLinkNav tracks an in-flight permalink navigation: the
	// channel was (or is being) opened and the message-select /
	// thread-open completes when that channel's messages land. See
	// reducer_links.go.
	pendingLinkNav *pendingLinkNav

	// browserOpener launches a URL in the OS browser. Defaults to
	// openURLCmd; tests inject fakes.
	browserOpener func(url string) tea.Cmd
```

NewApp struct literal:

```go
		browserOpener:        openURLCmd,
```

Add next to `openInSystemViewerCmd` (app.go:1923):

```go
// openURLCmd asynchronously launches the OS default browser for url.
// Same launcher matrix as openInSystemViewerCmd (xdg-open / open /
// rundll32). A failed launch surfaces a toast — unlike the image
// viewer, the user otherwise gets no feedback at all.
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if url == "" {
			return nil
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			log.Printf("browser launch failed: %v", err)
			return ToastMsg{Text: "Failed to open link"}
		}
		return nil
	}
}
```

Register the reducer in the dispatch chain (app.go:457-471), adding after `reduceChannels,`:

```go
		reduceLinks,
```

- [ ] **Step 5: Create reducer_links.go**

```go
// internal/ui/reducer_links.go
//
// Link-open routing (issue #62 + in-app permalink navigation).
//
// OpenLinkMsg is the single place every link open flows through:
//   - Slack archive permalinks whose subdomain matches the active
//     workspace AND whose channel resolves via ChannelService.Lookup
//     navigate in-app: dispatch ChannelSelectedMsg, then complete via
//     pendingLinkNav once the channel's messages are loaded (select
//     the target ts, or open the thread panel for thread_ts links).
//   - Everything else opens in the OS browser (a.browserOpener).
//
// Completion hooks live in reducer_channels.go (ChannelSelectedMsg
// and MessagesLoadedMsg arms call completePendingLinkNav).
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/messages"
)

// pendingLinkNav is the not-yet-completed tail of an in-app permalink
// navigation. Set by routeLink, consumed by completePendingLinkNav.
type pendingLinkNav struct {
	channelID string
	messageTS string
	threadTS  string // non-empty: open the thread panel instead of selecting
}

var reduceLinks reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(OpenLinkMsg)
	if !ok {
		return nil, false
	}
	return a.routeLink(m.URL), true
}

// routeLink decides between in-app navigation and the browser.
func (a *App) routeLink(rawURL string) tea.Cmd {
	pl, ok := slackurl.Parse(rawURL)
	if !ok {
		return a.browserOpener(rawURL)
	}
	domain := a.activeWorkspaceDomain()
	if domain == "" || pl.Subdomain != domain {
		return a.browserOpener(rawURL)
	}
	name, chType, found := a.channels.Lookup(pl.ChannelID)
	if !found {
		return a.browserOpener(rawURL)
	}
	a.pendingLinkNav = &pendingLinkNav{
		channelID: string(pl.ChannelID),
		messageTS: string(pl.MessageTS),
		threadTS:  string(pl.ThreadTS),
	}
	if string(pl.ChannelID) == a.activeChannelID {
		// Already viewing the channel; the loaded buffer is as good
		// as it gets, so complete authoritatively right now.
		return a.completePendingLinkNav(a.activeChannelID, true)
	}
	id, n, t := string(pl.ChannelID), name, chType
	return func() tea.Msg {
		return ChannelSelectedMsg{ID: id, Name: n, Type: t}
	}
}

// completePendingLinkNav finishes (or drops) the pending permalink
// navigation for channelID. authoritative=true means "no more message
// data is coming for this channel" — if the target ts still isn't in
// the buffer, give up with a toast instead of waiting.
//
// Called from: routeLink (already-active channel, authoritative),
// reduceChannels' ChannelSelectedMsg arm (cache render, best-effort),
// and reduceChannels' MessagesLoadedMsg arm (authoritative).
func (a *App) completePendingLinkNav(channelID string, authoritative bool) tea.Cmd {
	p := a.pendingLinkNav
	if p == nil {
		return nil
	}
	if p.channelID != channelID {
		// The user navigated somewhere unrelated before the link
		// target finished loading; the pending nav is stale.
		a.pendingLinkNav = nil
		return nil
	}
	if p.threadTS != "" {
		a.pendingLinkNav = nil
		return a.openThreadForPermalink(p.channelID, p.threadTS)
	}
	if a.messagepane.SelectByTS(p.messageTS) {
		a.pendingLinkNav = nil
		return nil
	}
	if authoritative {
		a.pendingLinkNav = nil
		return func() tea.Msg {
			return ToastMsg{Text: "Message is older than loaded history"}
		}
	}
	return nil
}

// openThreadForPermalink opens the thread panel for a permalink that
// carried thread_ts. Unlike openThreadForSelectedMessage it does not
// require the parent message to be in the pane buffer (mirrors
// openSelectedThreadCmd, which builds the parent from a summary):
// the parent row is taken from the loaded buffer or the thread cache
// when available, else a minimal stub that the ThreadRepliesLoadedMsg
// handler backfills from cache once the fetch lands.
func (a *App) openThreadForPermalink(channelID, threadTS string) tea.Cmd {
	chID := ids.ChannelID(channelID)
	tTS := ids.ThreadTS(threadTS)

	parent := messages.MessageItem{TS: threadTS, ThreadTS: threadTS}
	if channelID == a.activeChannelID {
		for _, m := range a.messagepane.Messages() {
			if m.TS == threadTS {
				parent = m
				break
			}
		}
	}
	if parent.Text == "" {
		if cached := a.threads.CacheRead(chID, tTS); len(cached) > 0 {
			parent = cached[0]
		}
	}

	a.threadVisible = true
	a.statusbar.SetInThread(true)
	a.focusedPanel = PanelThread
	a.threadPanel.SetThread(parent, nil, channelID, threadTS)
	a.threadCompose.SetChannel("thread")
	a.applyThreadUnreadBoundary(channelID)

	threads := a.threads
	var batch []tea.Cmd
	if cached := threads.CacheRead(chID, tTS); len(cached) > 1 {
		replies := cached[1:] // strip parent; reducer expects replies-only
		batch = append(batch, func() tea.Msg {
			return ThreadRepliesLoadedMsg{ThreadTS: threadTS, Replies: replies}
		})
	}
	batch = append(batch, func() tea.Msg { return threads.Fetch(chID, tTS) })
	return tea.Batch(batch...)
}
```

- [ ] **Step 6: Hook completion into reducer_channels.go**

Change the `ChannelSelectedMsg` arm (line 63-64) from:

```go
	case ChannelSelectedMsg:
		return reduceChannelSelected(a, m), true
```

to:

```go
	case ChannelSelectedMsg:
		cmd := reduceChannelSelected(a, m)
		// Best-effort permalink completion against whatever cache
		// tier just rendered; MessagesLoadedMsg (if a fetch was
		// fired) retries authoritatively. Also drops a stale pending
		// nav when the user navigated to an unrelated channel.
		if nav := a.completePendingLinkNav(m.ID, false); nav != nil {
			cmd = tea.Batch(cmd, nav)
		}
		return cmd, true
```

Change the end of the `MessagesLoadedMsg` arm (lines 95-98) from:

```go
		if m.Messages != nil {
			a.messagepane.SetMessages(m.Messages)
		}
		return nil, true
```

to:

```go
		if m.Messages != nil {
			a.messagepane.SetMessages(m.Messages)
		}
		// Authoritative permalink completion: this is the freshest
		// data we'll get for this channel.
		return a.completePendingLinkNav(m.ChannelID, true), true
```

- [ ] **Step 7: Backfill thread parent in reducer_threads.go**

In the `ThreadRepliesLoadedMsg` arm, change line 97-98 from:

```go
		channelID := a.threadPanel.ChannelID()
		a.threadPanel.SetThread(a.threadPanel.ParentMsg(), m.Replies, channelID, m.ThreadTS)
```

to:

```go
		channelID := a.threadPanel.ChannelID()
		parentMsg := a.threadPanel.ParentMsg()
		// Permalink-opened threads start with a stub parent (TS only).
		// The fetch that produced this msg also wrote the full thread
		// to cache — backfill the parent row from there.
		if parentMsg.Text == "" {
			if cached := a.threads.CacheRead(ids.ChannelID(channelID), ids.ThreadTS(m.ThreadTS)); len(cached) > 0 && cached[0].Text != "" {
				parentMsg = cached[0]
			}
		}
		a.threadPanel.SetThread(parentMsg, m.Replies, channelID, m.ThreadTS)
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestOpenLink|TestMessagesLoaded_CompletesPendingNav|TestChannelSelected_DifferentChannel' -v`
Expected: PASS

Then full suite: `go build ./... && go test ./...`
Expected: PASS (existing reducer_channels/threads tests still green)

- [ ] **Step 9: Commit**

```bash
git add internal/ui/msgs.go internal/ui/app.go internal/ui/reducer_links.go internal/ui/reducer_links_test.go internal/ui/reducer_channels.go internal/ui/reducer_threads.go
git commit -m "feat: route OpenLinkMsg — in-app navigation for active-workspace permalinks"
```

---

### Task 6: Link picker modal (`internal/ui/linkpicker`)

**Files:**
- Create: `internal/ui/linkpicker/model.go`
- Create: `internal/ui/linkpicker/model_test.go`
- Create: `internal/ui/linkpicker/view.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/linkpicker/model_test.go
package linkpicker

import "testing"

func items3() []Item {
	return []Item{
		{URL: "https://a.example/1", Label: "first"},
		{URL: "https://b.example/2"},
		{URL: "https://myteam.slack.com/archives/C1/p1700000000000001", InApp: true},
	}
}

func TestOpenAndNavigate(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Fatal("visible before Open")
	}
	m.Open(items3())
	if !m.IsVisible() {
		t.Fatal("not visible after Open")
	}
	if _, chosen := m.HandleKey("j"); chosen {
		t.Error("j must not choose")
	}
	m.HandleKey("j")
	item, chosen := m.HandleKey("enter")
	if !chosen {
		t.Fatal("enter should choose")
	}
	if item.URL != items3()[2].URL {
		t.Errorf("chose %q", item.URL)
	}
	if m.IsVisible() {
		t.Error("should close after choose")
	}
}

func TestNavigationBounds(t *testing.T) {
	m := New()
	m.Open(items3())
	m.HandleKey("k") // at top; no-op
	item, chosen := m.HandleKey("enter")
	if !chosen || item.URL != items3()[0].URL {
		t.Errorf("chose %+v chosen=%v", item, chosen)
	}
	m.Open(items3())
	for i := 0; i < 10; i++ {
		m.HandleKey("j") // clamps at bottom
	}
	item, _ = m.HandleKey("enter")
	if item.URL != items3()[2].URL {
		t.Errorf("chose %q", item.URL)
	}
}

func TestEscCloses(t *testing.T) {
	m := New()
	m.Open(items3())
	if _, chosen := m.HandleKey("esc"); chosen {
		t.Error("esc must not choose")
	}
	if m.IsVisible() {
		t.Error("esc should close")
	}
}

func TestEnterOnEmptyIsNoop(t *testing.T) {
	m := New()
	m.Open(nil)
	if _, chosen := m.HandleKey("enter"); chosen {
		t.Error("enter on empty list must not choose")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/linkpicker/ -v`
Expected: FAIL (package does not exist)

- [ ] **Step 3: Write model.go**

```go
// Package linkpicker provides the modal overlay that lets the user
// pick which link in a message to open (issue #62). Opened by the
// `o` keybinding when the selected message has more than one link;
// the chosen link is dispatched as ui.OpenLinkMsg by the mode handler.
package linkpicker

// Item is one selectable link row.
type Item struct {
	URL   string
	Label string // empty for bare links
	// InApp marks links that the router will navigate inside slk
	// (active-workspace archive permalinks); rendered with a badge.
	InApp bool
}

// Model is the link picker overlay state.
type Model struct {
	items    []Item
	selected int
	visible  bool
}

// New creates a hidden link picker.
func New() *Model { return &Model{} }

// Open shows the picker over items, with the first row selected.
func (m *Model) Open(items []Item) {
	m.items = items
	m.selected = 0
	m.visible = true
}

// Close hides the picker and drops its items.
func (m *Model) Close() {
	m.visible = false
	m.items = nil
	m.selected = 0
}

// IsVisible reports whether the picker is showing.
func (m *Model) IsVisible() bool { return m.visible }

// Items returns the current rows (for rendering and tests).
func (m *Model) Items() []Item { return m.items }

// Selected returns the highlighted row index.
func (m *Model) Selected() int { return m.selected }

// HandleKey processes one key. Returns (item, true) when the user
// chose a link with enter (the picker closes itself); (Item{}, false)
// otherwise. esc/q close without choosing.
func (m *Model) HandleKey(key string) (Item, bool) {
	switch key {
	case "esc", "q":
		m.Close()
	case "j", "down":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
	case "enter":
		if len(m.items) == 0 {
			return Item{}, false
		}
		item := m.items[m.selected]
		m.Close()
		return item, true
	}
	return Item{}, false
}
```

- [ ] **Step 4: Write view.go** (rendering modeled on `internal/ui/help/model.go:269-489`, simplified)

```go
package linkpicker

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// ViewOverlay renders the picker centered on a dimmed copy of
// background. Returns background unchanged when not visible.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}
	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}
	return overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
}

func (m *Model) renderBox(termWidth int) string {
	overlayWidth := termWidth * 6 / 10
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayWidth > 80 {
		overlayWidth = 80
	}
	if overlayWidth > termWidth-2 {
		overlayWidth = termWidth - 2
	}
	innerWidth := overlayWidth - 4 // border + padding

	bg := styles.Background
	title := lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(styles.Primary).
		Render("Open link")

	badgeStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent)

	var rows []string
	for i, it := range m.items {
		// Row text: "label  url" for labeled links, bare URL otherwise.
		text := it.URL
		if it.Label != "" && it.Label != it.URL {
			text = it.Label + "  " + it.URL
		}
		badge := ""
		if it.InApp {
			badge = " [slk]"
		}
		budget := innerWidth - 1 - lipgloss.Width(badge) // 1 = indicator column
		if budget < 1 {
			budget = 1
		}
		if lipgloss.Width(text) > budget {
			text = truncate.StringWithTail(text, uint(budget), "\u2026")
		}
		var row string
		if i == m.selected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("\u258c")
			body := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.Primary).
				Bold(true).
				Width(budget).
				Render(text)
			row = indicator + body + badgeStyle.Render(badge)
		} else {
			body := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.TextPrimary).
				Width(budget).
				Render(text)
			row = " " + body + badgeStyle.Render(badge)
		}
		rows = append(rows, row)
	}

	footer := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.TextMuted).
		Render("j/k move   enter open   esc close")

	content := title + "\n\n" + strings.Join(rows, "\n") + "\n\n" + footer
	// Re-paint modal bg+fg after every ANSI reset so trailing cells
	// don't leak the dimmed app behind the overlay.
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/linkpicker/ -v && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/linkpicker/
git commit -m "feat: add linkpicker modal for choosing a link to open"
```

---

### Task 7: `o` keybinding, ModeLinkPicker, and wiring

**Files:**
- Modify: `internal/ui/keys.go` (KeyMap field + DefaultKeyMap entry)
- Modify: `internal/ui/mode.go` (ModeLinkPicker const, IsModalOverlay, String)
- Create: `internal/ui/mode_linkpicker.go`
- Modify: `internal/ui/mode_handlers.go` (map entry)
- Modify: `internal/ui/mode_normal.go` (key case)
- Modify: `internal/ui/app.go` (linkPicker field + init + `openLinksOfSelected` + `linkOpensInApp`)
- Modify: `internal/ui/view_overlays.go` (overlay + overlayActive)
- Create: `internal/ui/open_links_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/ui/open_links_test.go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

func pressO(app *App) tea.Cmd {
	return app.handleNormalMode(tea.KeyPressMsg{Code: 'o', Text: "o"})
}

func TestOpenLinkKey_NoLinks_Toasts(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "no links here"}})
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Errorf("expected ToastMsg, got %#v", cmd())
	}
}

func TestOpenLinkKey_SingleLink_DispatchesOpenLinkMsg(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "see <https://example.com/docs|docs>"},
	})
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok {
		t.Fatalf("expected OpenLinkMsg, got %#v", cmd())
	}
	if msg.URL != "https://example.com/docs" {
		t.Errorf("URL = %q", msg.URL)
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal (no modal for single link)", app.mode)
	}
}

func TestOpenLinkKey_MultipleLinks_OpensPicker(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "<https://a.example/1|one> and <https://b.example/2>"},
	})
	cmd := pressO(app)
	if cmd != nil {
		t.Errorf("expected nil cmd (modal opens), got %#v", cmd())
	}
	if app.mode != ModeLinkPicker {
		t.Fatalf("mode = %v, want ModeLinkPicker", app.mode)
	}
	if !app.linkPicker.IsVisible() {
		t.Fatal("picker not visible")
	}
	items := app.linkPicker.Items()
	if len(items) != 2 || items[0].URL != "https://a.example/1" || items[1].URL != "https://b.example/2" {
		t.Errorf("items = %#v", items)
	}
}

func TestLinkPickerMode_EnterDispatchesOpenLinkMsg(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "<https://a.example/1> <https://b.example/2>"},
	})
	pressO(app)
	cmd := app.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	_ = cmd
	cmd = app.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok {
		t.Fatalf("expected OpenLinkMsg, got %#v", cmd())
	}
	if msg.URL != "https://b.example/2" {
		t.Errorf("URL = %q", msg.URL)
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v after choose", app.mode)
	}
}

func TestLinkPickerMode_EscCloses(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "<https://a.example/1> <https://b.example/2>"},
	})
	pressO(app)
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Errorf("expected nil cmd, got %#v", cmd())
	}
	if app.mode != ModeNormal || app.linkPicker.IsVisible() {
		t.Errorf("mode=%v visible=%v after esc", app.mode, app.linkPicker.IsVisible())
	}
}

func TestOpenLinkKey_FromThreadPanel(t *testing.T) {
	app := NewApp()
	parent := messages.MessageItem{TS: "1.0", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.0", Text: "parent"},
		{TS: "2.0", Text: "see <https://example.com/from-thread>"},
	}
	app.threadPanel.SetThread(parent, replies, "C1", "1.0")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	for i := 0; i < len(replies); i++ {
		if sel := app.threadPanel.SelectedReply(); sel != nil && sel.TS == "2.0" {
			break
		}
		app.threadPanel.MoveDown()
	}
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok || msg.URL != "https://example.com/from-thread" {
		t.Errorf("got %#v", cmd())
	}
}
```

Note: for key construction (`tea.KeyPressMsg{Code: tea.KeyEnter}`, escape, etc.), copy the exact convention used by existing tests in `internal/ui/app_test.go` (e.g. search for `KeyEnter` / `KeyEscape` there) — adjust if the codebase spells them differently.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestOpenLinkKey|TestLinkPickerMode' -v`
Expected: FAIL (undefined: ModeLinkPicker, linkPicker field, etc.)

- [ ] **Step 3: keys.go** — add to KeyMap struct:

```go
	OpenLink            key.Binding
```

and to DefaultKeyMap (after `OpenPreview`):

```go
		OpenLink:            key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open link in message")),
```

(The help modal picks this up automatically via `help.FromKeyMap`.)

- [ ] **Step 4: mode.go** — add `ModeLinkPicker` to the const block (after `ModeReactionsView`), to `IsModalOverlay`'s case list, and to `String`:

```go
	case ModeLinkPicker:
		return "LINKS"
```

- [ ] **Step 5: mode_handlers.go** — add map entry:

```go
	ModeLinkPicker:           handleLinkPickerMode,
```

- [ ] **Step 6: Create mode_linkpicker.go**

```go
// internal/ui/mode_linkpicker.go
//
// Key handler for ModeLinkPicker: the link-choice modal opened by
// the `o` keybinding when the selected message has multiple links.
// Enter dispatches the chosen URL as OpenLinkMsg (routed by
// reduceLinks); esc/q closes.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleLinkPickerMode(a *App, msg tea.KeyMsg) tea.Cmd {
	item, chosen := a.linkPicker.HandleKey(msg.String())
	if chosen {
		a.SetMode(ModeNormal)
		url := item.URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	}
	if !a.linkPicker.IsVisible() {
		// esc/q closed the picker.
		a.SetMode(ModeNormal)
	}
	return nil
}
```

- [ ] **Step 7: app.go wiring**

Imports: add `"github.com/gammons/slk/internal/slackurl"` and `"github.com/gammons/slk/internal/ui/linkpicker"`.

App struct (near `reactionPicker`, ~line 200):

```go
	// linkPicker is the open-link choice modal (issue #62).
	linkPicker *linkpicker.Model
```

NewApp struct literal (find where `reactionPicker` is constructed — same spot):

```go
		linkPicker:           linkpicker.New(),
```

Add methods (near `copyPermalinkOfSelected`, app.go:842):

```go
// openLinksOfSelected implements the `o` keybinding: collect the
// links in the selected message (messages pane or thread panel).
// 0 links -> toast; 1 link -> dispatch OpenLinkMsg directly; 2+ ->
// open the link picker modal. All opens converge on OpenLinkMsg,
// the single routing point in reducer_links.go.
func (a *App) openLinksOfSelected() tea.Cmd {
	var text string
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		text = msg.Text
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		text = reply.Text
	default:
		return nil
	}
	links := messages.ExtractLinks(text)
	switch len(links) {
	case 0:
		return func() tea.Msg { return ToastMsg{Text: "No links in message"} }
	case 1:
		url := links[0].URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	default:
		items := make([]linkpicker.Item, len(links))
		for i, l := range links {
			items[i] = linkpicker.Item{URL: l.URL, Label: l.Label, InApp: a.linkOpensInApp(l.URL)}
		}
		a.linkPicker.Open(items)
		a.SetMode(ModeLinkPicker)
		return nil
	}
}

// linkOpensInApp reports whether routeLink would navigate this URL
// inside slk (used for the picker's "[slk]" badge). Mirrors the
// guards at the top of routeLink.
func (a *App) linkOpensInApp(rawURL string) bool {
	pl, ok := slackurl.Parse(rawURL)
	if !ok {
		return false
	}
	domain := a.activeWorkspaceDomain()
	if domain == "" || pl.Subdomain != domain {
		return false
	}
	_, _, found := a.channels.Lookup(pl.ChannelID)
	return found
}
```

- [ ] **Step 8: mode_normal.go** — add case (after `OpenPreview`, line 218):

```go
	case key.Matches(msg, a.keys.OpenLink):
		return a.openLinksOfSelected()
```

- [ ] **Step 9: view_overlays.go** — in `applyOverlays` (after the help block, line 63):

```go
	if a.linkPicker.IsVisible() {
		screen = a.linkPicker.ViewOverlay(a.width, a.height, screen)
	}
```

and in `overlayActive` (line 88 area):

```go
		a.linkPicker.IsVisible() ||
```

- [ ] **Step 10: Run tests to verify they pass**

Run: `go test ./internal/ui/... -run 'TestOpenLinkKey|TestLinkPickerMode' -v`
Expected: PASS

Then: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/ui/keys.go internal/ui/mode.go internal/ui/mode_linkpicker.go internal/ui/mode_handlers.go internal/ui/mode_normal.go internal/ui/app.go internal/ui/view_overlays.go internal/ui/open_links_test.go
git commit -m "feat: add o keybinding + link picker modal to open message links (#62)"
```

---

### Task 8: Final verification

- [ ] **Step 1: Full build, vet, test**

Run: `gofmt -l . && go vet ./... && go build ./... && go test ./...`
Expected: gofmt prints nothing; vet clean; all tests PASS

- [ ] **Step 2: Manual smoke test**

Run `go run ./cmd/slk` against a real workspace and verify:
1. `o` on a message with one external link opens the browser
2. `o` on a message with multiple links shows the picker; enter opens, esc closes
3. `o` on a message containing an active-workspace permalink (use `Y` to copy one, paste it into a message) navigates to the channel and selects the message
4. A `thread_ts` permalink opens the thread panel
5. `o` on a message with no links shows the toast

- [ ] **Step 3: Commit any fixups**

```bash
git status --short
# commit fixups if any, message: "fix: <what>"
```

---

## Self-Review Notes

- **Spec coverage:** extraction (Task 2), keybinding+dispatch (Task 7), modal (Tasks 6-7), parser (Task 1), routing + pending nav + thread open (Task 5), SelectByTS (Task 3), workspace domain (Task 4), error paths (toasts/browser fallbacks in Tasks 5 & 7).
- **Known v1 limitation (documented in spec):** for a channel rendered from a provably-fresh cache (tier 1, no network fetch) with the target ts absent from the cached page, the pending nav is completed best-effort only — no toast fires; the channel opens at the newest page. The pending nav is dropped on the next unrelated navigation.
- **Type consistency check:** `pendingLinkNav` fields are plain strings (internal App state per ids.go scope rules); ids types appear only at service boundaries (`Lookup`, `CacheRead`, `Fetch`); `slackurl.Permalink` uses ids types. `completePendingLinkNav(channelID string, authoritative bool) tea.Cmd` is called from three sites with matching signatures.
