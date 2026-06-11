// internal/ui/view_window_region.go
//
// Multi-window messages-region renderer (window-management design
// §6, Phase 2). With a single window the existing renderMessagesRegion
// path runs untouched — identical output, identical caching. With
// splits, the wintree layout is walked recursively: the FOCUSED
// window renders the real (cached) messages panel sized to its rect;
// every other window renders a cheap static placeholder (dimmed
// border + channel name). Phase 3 replaces placeholders with live
// per-window models.
//
// NAMING: this file would naturally be view_windows.go, but a
// `_windows` filename suffix is a GOOS build constraint — Go would
// silently exclude the file from every non-Windows build (it lands in
// IgnoredGoFiles, no error). Hence view_window_region.go.
package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/wintree"
)

// renderWindowsRegion is the messages-region entry point called from
// App.View. Preview mode and the single-window tree delegate to the
// existing path unchanged.
func (a *App) renderWindowsRegion(frame panelLayoutFrame, themeVer int64, previewActive bool) string {
	if previewActive || a.wins.Len() == 1 {
		return a.renderMessagesRegion(frame, themeVer, previewActive)
	}
	bounds := wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
	return a.renderWindowNode(a.wins.Layout(bounds), frame, themeVer)
}

// renderWindowNode renders one layout-tree node to a string of
// exactly Rect.W x Rect.H cells. Callers guarantee Rect.W >= 1 and
// Rect.H >= 1 (zero-extent children are skipped below). exactSize
// enforces a minimum width and exact height at every leaf; widths
// can only exceed a rect on the sub-minimum focused leaf, which
// hard-truncates back via MaxWidth. Sub-minimum windows thus degrade
// to garbled-but-correctly-sized cells instead of corrupting the
// region geometry.
func (a *App) renderWindowNode(n wintree.LayoutNode, frame panelLayoutFrame, themeVer int64) string {
	if n.Leaf {
		if n.ID == a.focusedWin {
			sub := frame
			sub.MsgWidth = n.Rect.W - 2
			// Floor: a hard shrink can leave rects narrower than the
			// panel chrome (W-2 < 2). Below 2, renderMessagesTop's
			// borderedTopPane computes strings.Repeat(top, MsgWidth-2)
			// with a negative count and panics (mirrors the
			// msgContentHeight < 3 clamp downstream). The over-wide
			// result is wrapped back to Rect.W by exactSize.
			if sub.MsgWidth < 2 {
				sub.MsgWidth = 2
			}
			sub.MsgBorder = 2
			sub.ContentHeight = n.Rect.H
			out := exactSize(a.renderMessagesRegion(sub, themeVer, false), n.Rect.W, n.Rect.H)
			if sub.MsgWidth+sub.MsgBorder > n.Rect.W {
				// The floored frame renders wider than the rect
				// (exactSize pads to a minimum width but never
				// shrinks). Hard-truncate each row back, ANSI-aware,
				// so the column tiling stays exact.
				out = lipgloss.NewStyle().MaxWidth(n.Rect.W).Render(out)
			}
			return out
		}
		return a.renderPlaceholderWindow(n)
	}
	parts := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		// Zero-extent rects (more windows than rows/cols after a hard
		// shrink) contribute zero cells and are skipped: exactSize
		// treats a 0 dimension as "unset" and would render at natural
		// size, breaking the region dimension contract. childRects
		// extents sum exactly to the parent extent along the split
		// axis, so the remaining children still tile the parent.
		if c.Rect.W < 1 || c.Rect.H < 1 {
			continue
		}
		parts = append(parts, a.renderWindowNode(c, frame, themeVer))
	}
	if n.Dir == wintree.SplitSideBySide {
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderPlaceholderWindow renders an unfocused window: dimmed border,
// channel name centered. Static and cheap — no cache needed.
func (a *App) renderPlaceholderWindow(n wintree.LayoutNode) string {
	ch, _ := a.wins.Channel(n.ID)
	name := ch.Name
	if name == "" {
		name = "(no channel)"
	} else {
		name = "# " + name
	}
	innerW, innerH := n.Rect.W-2, n.Rect.H-2
	if innerW < 1 || innerH < 1 {
		// Too small for border + label: a blank block of exactly
		// Rect.W x Rect.H keeps the tiling intact (caller guarantees
		// both >= 1; negative inner dims would otherwise flow into
		// lipgloss.Place / Style.Width).
		return exactSize("", n.Rect.W, n.Rect.H)
	}
	label := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(name)
	inner := lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, label)
	return exactSize(
		styles.UnfocusedBorder.Width(innerW).Render(inner),
		n.Rect.W, n.Rect.H,
	)
}
