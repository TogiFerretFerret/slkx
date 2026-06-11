package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/wintree"
)

// renderRegion renders the messages region exactly as App.View does,
// without depending on the tea.View wrapper API.
func renderRegion(a *App) string {
	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	return a.renderWindowsRegion(frame, 0, false)
}

func TestRegion_SingleWindowUnchanged(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	multi := a.renderWindowsRegion(frame, 0, false)
	direct := a.renderMessagesRegion(frame, 0, false)
	if multi != direct {
		t.Fatal("single-window region must be byte-identical to the direct messages render")
	}
}

func TestRegion_SplitRendersPlaceholderWithChannelName(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.splitWindow(wintree.SplitSideBySide)
	// Focused (new) window is live; the original window is a
	// placeholder showing its channel name.
	out := ansi.Strip(renderRegion(a))
	if !strings.Contains(out, "# general") {
		t.Fatalf("placeholder should show channel name '# general':\n%s", out)
	}
}

func TestRegion_SplitOutputDimensionsStable(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	before := renderRegion(a)
	_ = a.splitWindow(wintree.SplitSideBySide)
	after := renderRegion(a)
	if lipgloss.Height(before) != lipgloss.Height(after) {
		t.Fatalf("row count changed after split: %d -> %d", lipgloss.Height(before), lipgloss.Height(after))
	}
	if lipgloss.Width(before) != lipgloss.Width(after) {
		t.Fatalf("width changed after split: %d -> %d", lipgloss.Width(before), lipgloss.Width(after))
	}
}

func TestRegion_CloseRestoresSingleWindowPath(t *testing.T) {
	a := newWideTestApp(t)
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	_ = a.splitWindow(wintree.SplitStacked)
	_ = a.closeWindow()
	if a.wins.Len() != 1 {
		t.Fatalf("Len = %d, want 1", a.wins.Len())
	}
	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	multi := a.renderWindowsRegion(frame, 0, false)
	direct := a.renderMessagesRegion(frame, 0, false)
	if multi != direct {
		t.Fatal("after closing back to one window the region must take the direct single-window path")
	}
}
