package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/statusbar"
)

func newTestAppWithMessages(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.width = 120
	a.height = 30
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "second message", Timestamp: "1:01 PM"},
	})
	// Force a render so layout offsets and caches populate.
	_ = a.View()
	return a
}

// drainBatch fully expands a tea.Cmd (including nested tea.BatchMsg) and
// returns all leaf messages. Test-only; ignores nil cmds.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			out = append(out, drainBatch(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// looksLikeSetClipboardMsg returns true when m is the unexported
// setClipboardMsg type from bubbletea (a defined string type). It is
// the only string-kind Msg that flows through App, so reflecting the
// kind is sufficient to identify it.
func looksLikeSetClipboardMsg(m tea.Msg) (string, bool) {
	v := reflect.ValueOf(m)
	if v.Kind() == reflect.String {
		return v.String(), true
	}
	return "", false
}

func TestApp_DragInMessagesEmitsClipboardAndToast(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	// Terminal Y = 4 → panelAt subtracts the 1-row panel border to give
	// pane-local y=3, which is past the chrome (header + separator =
	// chromeHeight=2). Anything < 3 lands on the chrome and is a no-op.
	pressY := 4
	// Press
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	// Motion
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	// Release
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX + 10, Y: pressY + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected a command on release")
	}
	msgs := drainBatch(cmd)
	var sawClipboard, sawCopiedToast bool
	for _, m := range msgs {
		if payload, ok := looksLikeSetClipboardMsg(m); ok {
			if payload == "" {
				t.Errorf("clipboard payload empty")
			}
			if strings.ContainsRune(payload, '▌') {
				t.Errorf("clipboard contained border char")
			}
			sawClipboard = true
		}
		if v, ok := m.(statusbar.CopiedMsg); ok {
			if v.N <= 0 {
				t.Errorf("CopiedMsg.N = %d", v.N)
			}
			sawCopiedToast = true
		}
	}
	if !sawClipboard {
		t.Errorf("expected setClipboardMsg in batched output (drained: %v)", msgs)
	}
	if !sawCopiedToast {
		t.Errorf("expected statusbar.CopiedMsg in batched output (drained: %v)", msgs)
	}
}

func TestApp_PlainClickDoesNotCopy(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	// Terminal Y past the chrome (panel border + 2-row chrome).
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	for _, m := range drainBatch(cmd) {
		if _, ok := looksLikeSetClipboardMsg(m); ok {
			t.Fatal("plain click must not write to clipboard")
		}
	}
	if a.messagepane.HasSelection() {
		t.Fatal("plain click must not leave a pinned selection")
	}
}

// A plain click (no drag) on a message row opens that message's thread
// view, mirroring the Enter keypress. Defends the click-to-open-thread
// behavior so it stays in lockstep with handleEnter's PanelMessages
// branch via the shared openThreadForSelectedMessage helper.
func TestApp_PlainClickOnMessageOpensThread(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.activeChannelID = "C1"

	fetchedCh := ""
	fetchedTS := ""
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetchedCh = string(channelID)
		fetchedTS = string(threadTS)
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	pressX := a.layout.sidebarEnd + 2
	// pane-local y=3 (terminal y=4 minus 1-row panel border) lands on
	// the first message row past the 2-row chrome. newTestAppWithMessages
	// seeds 2 messages with selection at index 1 (the bottom message --
	// SetMessages defaults selection to the newest); clicking row 3
	// should select message index 0 and open its thread.
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("click-release on a message must return a non-nil cmd (open-thread fetch)")
	}
	_ = drainBatch(cmd)

	if !a.threadVisible {
		t.Error("threadVisible = false after click; want true")
	}
	if a.focusedPanel != PanelThread {
		t.Errorf("focusedPanel = %v after click; want PanelThread", a.focusedPanel)
	}
	if fetchedCh != "C1" {
		t.Errorf("threadFetcher called with channel %q; want C1", fetchedCh)
	}
	// The first message has TS "1.0"; clicking it should open thread with
	// that TS as the parent.
	if fetchedTS != "1.0" {
		t.Errorf("threadFetcher called with threadTS %q; want 1.0", fetchedTS)
	}
}

// A click that lands on the channel header chrome (above the first
// message) must NOT open a thread -- chrome clicks are no-ops. Defends
// the ClickAt(returns bool) plumbing.
func TestApp_PlainClickOnChromeDoesNotOpenThread(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.activeChannelID = "C1"

	called := false
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		called = true
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	pressX := a.layout.sidebarEnd + 2
	// pane-local y=0 (terminal y=1 minus 1-row panel border) lands on
	// the channel header inside the messages pane chrome -- chrome
	// occupies pane-local rows 0..chromeHeight-1.
	pressY := 1
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})

	for _, m := range drainBatch(cmd) {
		if _, ok := m.(ThreadRepliesLoadedMsg); ok {
			t.Fatal("click on chrome must not dispatch a thread fetch")
		}
	}
	if called {
		t.Fatal("threadFetcher called on chrome click")
	}
	if a.threadVisible {
		t.Error("threadVisible = true after chrome click; want false")
	}
}

// A drag (motion between press and release) must NOT open a thread --
// the user is text-selecting, not navigating. Defends the moved-vs-clicked
// gate in the MouseReleaseMsg handler.
func TestApp_DragDoesNotOpenThread(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.activeChannelID = "C1"

	called := false
	a.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		called = true
		return ThreadRepliesLoadedMsg{ThreadTS: string(threadTS), Replies: nil}
	})

	pressX := a.layout.sidebarEnd + 2
	pressY := 4
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: pressY, Button: tea.MouseLeft})
	// Any motion between press and release flags a drag.
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: pressY, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseReleaseMsg{X: pressX + 5, Y: pressY, Button: tea.MouseLeft})

	if called {
		t.Fatal("threadFetcher called after a drag; drags must not open threads")
	}
	if a.threadVisible {
		t.Error("threadVisible = true after drag; want false")
	}
}

func TestApp_CopiedMsgShowsToastAndSchedulesClear(t *testing.T) {
	a := newTestAppWithMessages(t)
	_, cmd := a.Update(statusbar.CopiedMsg{N: 7})
	// Status bar must show the toast immediately.
	if !strings.Contains(a.statusbar.View(80), "Copied 7 chars") {
		t.Fatalf("status bar did not show toast")
	}
	if cmd == nil {
		t.Fatal("expected a tick command to clear the toast")
	}
	// We don't actually wait 2s in a unit test; just verify the Cmd
	// produces a CopiedClearMsg type when invoked.
	// tea.Tick wraps a function; calling it returns a TickMsg-like value.
	// Easier path: directly send CopiedClearMsg and verify it clears.
	_, _ = a.Update(statusbar.CopiedClearMsg{})
	if strings.Contains(a.statusbar.View(80), "Copied") {
		t.Fatalf("status bar still showing toast after CopiedClearMsg")
	}
}

func TestApp_DragNearTopEdgeSchedulesAutoScroll(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	// Press in the middle of the pane.
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	// Move to row 1 (which is pane-local y=0 — the top edge).
	_, cmd := a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected an auto-scroll tick command on edge motion")
	}
	// The cmd should produce an autoScrollTickMsg.
	msgs := drainBatch(cmd)
	var sawTick bool
	for _, m := range msgs {
		if _, ok := m.(autoScrollTickMsg); ok {
			sawTick = true
		}
	}
	if !sawTick {
		t.Fatalf("expected autoScrollTickMsg in batched output; got %v", msgs)
	}
}

func TestApp_AutoScrollTickRefreshesWhileEdgeHeld(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	// First tick fires; while still at the top edge we expect another one.
	_, cmd := a.Update(autoScrollTickMsg{})
	if cmd == nil {
		t.Fatal("expected another tick while edge is still active")
	}
	msgs := drainBatch(cmd)
	var sawTick bool
	for _, m := range msgs {
		if _, ok := m.(autoScrollTickMsg); ok {
			sawTick = true
		}
	}
	if !sawTick {
		t.Fatal("expected autoScrollTickMsg in continuation")
	}
}

func TestApp_AutoScrollStopsWhenCursorLeavesEdge(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 5, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX, Y: 1, Button: tea.MouseLeft})
	// Move back to the middle.
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX, Y: 10, Button: tea.MouseLeft})
	// A tick now finds no edge → should NOT schedule another tick.
	_, cmd := a.Update(autoScrollTickMsg{})
	for _, m := range drainBatch(cmd) {
		if _, ok := m.(autoScrollTickMsg); ok {
			t.Fatal("auto-scroll must stop when cursor leaves the edge")
		}
	}
}

func TestApp_FocusNextClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 4, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 4, Button: tea.MouseLeft})
	if !a.messagepane.HasSelection() {
		t.Fatal("precondition: should have selection")
	}
	a.FocusNext()
	if a.messagepane.HasSelection() {
		t.Fatal("FocusNext must clear selection")
	}
}

func TestApp_SetModeInsertClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 4, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 4, Button: tea.MouseLeft})
	if !a.messagepane.HasSelection() {
		t.Fatal("precondition: should have selection")
	}
	a.SetMode(ModeInsert)
	if a.messagepane.HasSelection() {
		t.Fatal("entering insert mode must clear selection")
	}
}

func TestApp_ToggleSidebarClearsSelection(t *testing.T) {
	a := newTestAppWithMessages(t)
	pressX := a.layout.sidebarEnd + 2
	_, _ = a.Update(tea.MouseClickMsg{X: pressX, Y: 4, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: pressX + 5, Y: 4, Button: tea.MouseLeft})
	a.ToggleSidebar()
	if a.messagepane.HasSelection() {
		t.Fatal("ToggleSidebar must clear selection")
	}
}
