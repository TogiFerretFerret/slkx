// internal/ui/view_early.go
//
// Pre-measurement View fallback (Phase 6b).
//
// Before the terminal reports its size via WindowSizeMsg, App.View
// has no real width/height to lay out the three-panel UI. Without
// a fallback, the user sees a blank altscreen while workspaces
// connect.
//
// renderEarlyFallback returns:
//   - the loading overlay rendered at a generous default canvas
//     (80x24) when the bootstrap is still loading -- centered
//     content lands roughly where the eye expects;
//   - a minimal "Initializing..." string when there's nothing to
//     show yet.
//
// The second return value reports whether the caller should
// short-circuit (true when width or height is still 0).
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func (a *App) renderEarlyFallback() (tea.View, bool) {
	if a.width != 0 && a.height != 0 {
		return tea.View{}, false
	}
	var screen string
	if a.bootstrap.IsLoading() {
		// Use a generous default canvas so the centered overlay
		// lands roughly where the user's eye expects it. The real
		// WindowSizeMsg arrives within a frame and the overlay
		// re-renders correctly.
		screen = a.bootstrap.Render(80, 24, a.spinnerGlyph())
	} else {
		screen = "Initializing..."
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	return v, true
}
