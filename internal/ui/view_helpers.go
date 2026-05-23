// internal/ui/view_helpers.go
//
// Phase 6 of the SOLID refactor of internal/ui/app.go: per-region
// View renderer extraction.
//
// This file holds shared primitives used by the per-region
// renderers in view_*.go (and still by App.View itself until all
// regions are extracted).
//
// Both helpers were originally inline closures at the top of
// App.View. Hoisting them out is a prerequisite for the region
// split -- per-region renderers in view_*.go need to call them
// without capturing the View-scoped closure environment.
//
// Both are pure (no App state, no goroutines, no allocations
// beyond what lipgloss does internally). The Go compiler inlines
// the no-capture closures into their call site; the free-function
// form is bytecode-equivalent.
package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

// exactSizeBg forces s to exactly (w, h) cells with bg as the
// background color. Uses an explicit width parameter instead of
// lipgloss.Width(s) to avoid ANSI miscounting in complex rendered
// content (e.g. nested borders, mixed-foreground spans).
func exactSizeBg(s string, w, h int, bg color.Color) string {
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Background(bg).Render(s)
}

// exactSize is exactSizeBg with the default theme background.
// The vast majority of pane renders want this; only the workspace
// rail and sidebar (which use distinct panel colors) reach for
// exactSizeBg directly.
func exactSize(s string, w, h int) string {
	return exactSizeBg(s, w, h, styles.Background)
}
