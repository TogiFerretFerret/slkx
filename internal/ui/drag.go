// internal/ui/drag.go
//
// Mouse-drag selection FSM state.
//
// Phase 2h of the SOLID refactor of internal/ui/app.go: extracts the
// dragState struct + its primitive transitions out of App. The four
// Update arms that drive the FSM (MouseClickMsg, MouseMotionMsg,
// autoScrollTickMsg, MouseReleaseMsg) stay on App because they touch
// sub-models (messagepane, threadPanel) and dispatch tea.Cmds — but
// they now go through this controller for every state read and
// mutation.
//
// State machine:
//
//	IDLE                  panel == PanelWorkspace (zero value)
//	  │ MouseClickMsg on PanelMessages / PanelThread (Begin)
//	  ▼
//	PRESS_NOT_MOVED       panel set, moved == false
//	  │ MouseMotionMsg    (Extend → moved = true)
//	  ▼
//	DRAGGING              moved == true
//	  │ optionally: cursor at pane edge (ClaimAutoScroll → autoscroll
//	  │             tick chain starts), self-terminates on ClearAutoScroll
//	  │ MouseReleaseMsg   (Finish → IDLE; caller branches on moved
//	  ▼                    flag to either commit selection or treat as
//	IDLE                   a plain click)
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/statusbar"
)

// dragState captures an in-progress mouse drag. The originating panel
// (PanelMessages or PanelThread; PanelWorkspace == idle) clamps where
// the drag's selection extends to — leaving the pane pins the extend
// at the last known position inside it.
//
// clickedMessage records whether the press landed on a real message
// row (vs chrome or empty space). MouseReleaseMsg consults it on
// plain-click finalization: if a plain click landed on a message, that
// message's thread is opened (mirrors the Enter keypress).
//
// autoScrollActive is the once-claim guard for the edge-autoscroll
// tea.Tick chain; ClaimAutoScroll returns true exactly once until
// ClearAutoScroll resets it.
type dragState struct {
	panel            Panel
	pressX, pressY   int
	lastX, lastY     int
	moved            bool
	autoScrollActive bool
	clickedMessage   bool
}

func newDragState() *dragState { return &dragState{} }

// IsActive reports whether a drag is in progress on a real pane.
func (d *dragState) IsActive() bool {
	return d.panel == PanelMessages || d.panel == PanelThread
}

// Panel returns the originating panel. Meaningless when !IsActive.
func (d *dragState) Panel() Panel { return d.panel }

// LastPos returns the most recent cursor position recorded by Extend
// (or the initial press position if no motion has occurred yet).
func (d *dragState) LastPos() (x, y int) { return d.lastX, d.lastY }

// Begin records a fresh press on panel at (px, py). Any prior drag
// state is overwritten.
func (d *dragState) Begin(panel Panel, px, py int) {
	*d = dragState{
		panel:  panel,
		pressX: px, pressY: py,
		lastX: px, lastY: py,
	}
}

// SetClickedMessage records whether the press landed on a real message
// row. Called immediately after Begin from the MouseClickMsg arm on
// PanelMessages.
func (d *dragState) SetClickedMessage(b bool) { d.clickedMessage = b }

// Extend updates the cursor position to (px, py) and marks the drag
// as moved. If panel doesn't match the originating drag panel, the
// position is clamped to the previous lastX/lastY (pinning extension
// at the last known coordinates inside the originating pane).
// Returns the effective (lastX, lastY) after clamping.
func (d *dragState) Extend(panel Panel, px, py int) (x, y int) {
	if panel != d.panel {
		px, py = d.lastX, d.lastY
	}
	d.lastX, d.lastY = px, py
	d.moved = true
	return px, py
}

// ClaimAutoScroll flips on the autoscroll-in-flight gate. Returns
// true on first call (caller schedules an autoScrollTickMsg);
// false if a chain is already in flight (caller does nothing).
func (d *dragState) ClaimAutoScroll() bool {
	if d.autoScrollActive {
		return false
	}
	d.autoScrollActive = true
	return true
}

// ClearAutoScroll resets the autoscroll-in-flight gate. Called from
// the autoScrollTickMsg arm when the cursor leaves the pane edge or
// the drag ends.
func (d *dragState) ClearAutoScroll() { d.autoScrollActive = false }

// Finish returns the captured release context and resets the state
// to idle. Called from MouseReleaseMsg.
func (d *dragState) Finish() (moved bool, panel Panel, clickedMessage bool) {
	moved = d.moved
	panel = d.panel
	clickedMessage = d.clickedMessage
	*d = dragState{}
	return
}

// autoScrollTickInterval is the cadence for the edge-autoscroll
// tick chain while a drag is held against the top/bottom edge of a
// scrollable pane. 50ms is fast enough to feel responsive but slow
// enough not to overshoot small message lists.
const autoScrollTickInterval = 50 * time.Millisecond

// autoScrollTickCmd schedules the next autoScrollTickMsg. The chain
// self-terminates when the drag ends or the cursor leaves the edge
// (see Handle's autoScrollTickMsg arm).
func autoScrollTickCmd() tea.Cmd {
	return tea.Tick(autoScrollTickInterval, func(time.Time) tea.Msg {
		return autoScrollTickMsg{}
	})
}

// Handle is the drag-FSM reducer for App.Update (Phase 4c). Owns
// the three Update arms that read/mutate drag state:
//
//   - tea.MouseMotionMsg  -- extend the selection + maybe start
//     the autoscroll chain when the cursor hits an edge.
//   - autoScrollTickMsg   -- one tick of the chain: scroll the
//     originating pane, re-extend the selection, reschedule.
//   - tea.MouseReleaseMsg -- finalize: plain click (open thread or
//     clear selection) vs drag (copy selection to clipboard).
//
// tea.MouseClickMsg and tea.MouseWheelMsg deliberately do NOT route
// through here. MouseClick is a multi-panel router (workspace rail,
// sidebar, channels, reactions, image preview, drag-begin) whose
// drag-begin is only one of many outcomes; MouseWheel is pure
// viewport scrolling unrelated to drag. Both belong in a future
// reducer_mouse.go (Phase 4m) once their non-drag responsibilities
// have a home.
//
// Returns (nil, false) for any other message type.
func (d *dragState) Handle(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.MouseMotionMsg:
		if a.bootstrap.IsLoading() {
			return nil, true
		}
		if m.Button != tea.MouseLeft {
			return nil, true
		}
		if !d.IsActive() {
			return nil, true
		}
		panel, px, py, _ := a.panelAt(m.X, m.Y)
		// Clamp to the originating pane: if the cursor leaves the
		// pane, pin extension at the last known coordinates inside it.
		px, py = d.Extend(panel, px, py)
		switch d.Panel() {
		case PanelMessages:
			a.messagepane.ExtendSelectionAt(py, px)
		case PanelThread:
			a.threadPanel.ExtendSelectionAt(py, px)
		}
		// If the cursor is at the top/bottom edge of the originating
		// pane, schedule an auto-scroll tick. ClaimAutoScroll returns
		// true once until ClearAutoScroll resets it, guarding against
		// parallel tick chains accumulating.
		var hint int
		switch d.Panel() {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(py)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(py)
		}
		if hint != 0 && d.ClaimAutoScroll() {
			return autoScrollTickCmd(), true
		}
		return nil, true

	case autoScrollTickMsg:
		_ = m
		// If the drag ended (release clears the drag state),
		// self-terminate.
		if !d.IsActive() {
			d.ClearAutoScroll()
			return nil, true
		}
		lastX, lastY := d.LastPos()
		var hint int
		switch d.Panel() {
		case PanelMessages:
			hint = a.messagepane.ScrollHintForDrag(lastY)
		case PanelThread:
			hint = a.threadPanel.ScrollHintForDrag(lastY)
		}
		if hint == 0 {
			// Cursor left the edge -- stop ticking. Re-entering the
			// edge in a future motion event will re-arm the loop.
			d.ClearAutoScroll()
			return nil, true
		}
		switch d.Panel() {
		case PanelMessages:
			if hint < 0 {
				a.messagepane.ScrollUp(1)
			} else {
				a.messagepane.ScrollDown(1)
			}
			a.messagepane.ExtendSelectionAt(lastY, lastX)
		case PanelThread:
			if hint < 0 {
				a.threadPanel.ScrollUp(1)
			} else {
				a.threadPanel.ScrollDown(1)
			}
			a.threadPanel.ExtendSelectionAt(lastY, lastX)
		}
		// Schedule the next tick. autoScrollActive remains true.
		return autoScrollTickCmd(), true

	case tea.MouseReleaseMsg:
		_ = m
		if !d.IsActive() {
			return nil, true
		}
		moved, panel, clickedMessage := d.Finish()
		if !moved {
			// Plain click -- drop any previous pinned selection.
			switch panel {
			case PanelMessages:
				a.messagepane.ClearSelection()
				// Treat a click on a real message row as Enter:
				// open that message's thread. Clicks that missed
				// (chrome, empty space) leave the panel as-is.
				if clickedMessage {
					if cmd := a.openThreadForSelectedMessage(); cmd != nil {
						return cmd, true
					}
				}
			case PanelThread:
				a.threadPanel.ClearSelection()
			}
			return nil, true
		}
		var (
			text string
			ok   bool
		)
		switch panel {
		case PanelMessages:
			text, ok = a.messagepane.EndSelection()
		case PanelThread:
			text, ok = a.threadPanel.EndSelection()
		}
		if !(ok && text != "") {
			return nil, true
		}
		n := len([]rune(text))
		return tea.Batch(
			tea.SetClipboard(text),
			func() tea.Msg { return statusbar.CopiedMsg{N: n} },
		), true
	}
	return nil, false
}
