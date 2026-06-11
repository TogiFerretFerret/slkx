// internal/ui/windows.go
//
// App-side window management (window-management design §1, §4).
// Thin bridge between the wintree package and App state: tree ops,
// focus movement, status-bar toasts, and the focused-window channel
// contract. Phase 2: ONE live messages model — the focused window
// renders it; focusing a window on a different channel re-dispatches
// the standard ChannelSelectedMsg so the live pane follows focus.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/wintree"
)

// windowBounds returns the messages-region rectangle the window tree
// subdivides. Recomputing the layout frame here is safe: Compute is
// deterministic for unchanged inputs and View re-runs it each frame.
func (a *App) windowBounds() wintree.Rect {
	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	return wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
}

// splitWindow creates a new window (cloning the focused window's
// channel) and focuses it. Toasts "Not enough room" on refusal.
func (a *App) splitWindow(dir wintree.Dir) tea.Cmd {
	id, err := a.wins.Split(a.focusedWin, dir, a.windowBounds())
	if err != nil {
		return toastWithClear(a, "Not enough room", 2*time.Second)
	}
	a.focusedWin = id
	a.focusedPanel = PanelMessages
	return nil
}

// closeWindow closes the focused window; focus falls to its neighbor.
// Toasts "Cannot close last window" instead of ever quitting.
func (a *App) closeWindow() tea.Cmd {
	next, err := a.wins.Close(a.focusedWin)
	if err != nil {
		return toastWithClear(a, "Cannot close last window", 2*time.Second)
	}
	return a.focusWindow(next)
}

// onlyWindow closes every window except the focused one.
func (a *App) onlyWindow() {
	_ = a.wins.Only(a.focusedWin)
}

// cycleWindow focuses the next window in tree order (ctrl+w w).
func (a *App) cycleWindow() tea.Cmd {
	return a.focusWindow(a.wins.Cycle(a.focusedWin, 1))
}

// navigateWindow focuses the geometric neighbor (ctrl+w h/j/k/l).
// No neighbor is a silent no-op, like vim.
func (a *App) navigateWindow(nd wintree.NavDir) tea.Cmd {
	if id, ok := a.wins.NavigateDir(a.focusedWin, nd, a.windowBounds()); ok {
		return a.focusWindow(id)
	}
	return nil
}

// focusWindow moves window focus to id. When the target window views
// a different channel than the live model, the standard channel
// selection is dispatched so the live pane loads it (Phase 2 single-
// model semantics; Phase 3 replaces this with per-window models).
func (a *App) focusWindow(id wintree.LeafID) tea.Cmd {
	if id == a.focusedWin {
		return nil
	}
	a.focusedWin = id
	a.focusedPanel = PanelMessages
	ch, ok := a.wins.Channel(id)
	if !ok || ch.ID == "" || ch.ID == a.activeChannelID {
		return nil
	}
	return func() tea.Msg {
		return ChannelSelectedMsg{ID: ch.ID, Name: ch.Name, Type: ch.Type}
	}
}

// setFocusedWindowChannel records the applied channel selection on
// the focused window. Called from the ChannelSelectedMsg apply path.
func (a *App) setFocusedWindowChannel(id, name, chType string) {
	a.wins.SetChannel(a.focusedWin, wintree.Channel{ID: id, Name: name, Type: chType})
}
