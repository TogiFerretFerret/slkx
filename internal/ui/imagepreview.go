// internal/ui/imagepreview.go
//
// Full-screen image preview overlay state.
//
// Phase 2i of the SOLID refactor of internal/ui/app.go: extracts the
// four overlay-state fields (previewOverlay, previewChannel,
// previewTS, previewAttIdx) plus the spinner tick scheduler into a
// self-contained controller.
//
// The cmd-building helpers (openImagePreviewCmd, cycleImagePreviewCmd,
// previewFetchCmd, previewMetaForOpen) STAY on App because they couple
// to findMessageInActiveChannel which itself couples to messagepane /
// threadPanel — pulling them in here would require either passing
// several sub-model references at construction or moving the message-
// lookup helper too. Cleaner boundary: this file owns the overlay
// state; App's cmd helpers orchestrate the async fetch + dispatch.
//
// imageFetcher and imgProtocol also stay on App: they're wired once at
// startup via SetImageFetcher / SetImageProtocol and never mutate; not
// preview-specific state.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
)

// previewSpinnerTickInterval is the redraw cadence for the loading
// spinner. 100ms feels alive without being a CPU hog.
const previewSpinnerTickInterval = 100 * time.Millisecond

// previewSpinnerTickCmd schedules the next previewSpinnerTickMsg. The
// Update arm reschedules until the overlay either closes or the image
// has loaded; the chain self-terminates.
func previewSpinnerTickCmd() tea.Cmd {
	return tea.Tick(previewSpinnerTickInterval, func(time.Time) tea.Msg {
		return previewSpinnerTickMsg{}
	})
}

// imagePreviewController owns the full-screen image preview overlay
// state. A nil overlay (the zero value) means no preview is currently
// open; the channel / ts / attIdx triple identifies the message whose
// attachment is being shown (used by cycle keys to locate siblings).
type imagePreviewController struct {
	overlay *imgpkg.Preview
	channel string
	ts      string
	attIdx  int
}

func newImagePreviewController() *imagePreviewController {
	return &imagePreviewController{}
}

// Active reports whether a preview overlay is currently visible.
func (p *imagePreviewController) Active() bool {
	return p.overlay != nil && !p.overlay.IsClosed()
}

// Overlay returns the current preview overlay, or nil when no preview
// is open. Callers that only need to query Active() should prefer it.
func (p *imagePreviewController) Overlay() *imgpkg.Preview { return p.overlay }

// Channel / TS / AttIdx return the (channel, message ts, attachment
// idx) of the message whose attachment is currently displayed.
// Meaningless when !Active.
func (p *imagePreviewController) Channel() string { return p.channel }
func (p *imagePreviewController) TS() string      { return p.ts }
func (p *imagePreviewController) AttIdx() int     { return p.attIdx }

// Open installs overlay as the currently-shown preview and records the
// (channel, ts, attIdx) source so cycle keys can locate siblings.
// Any previous overlay is overwritten (caller is responsible for
// closing it first if needed).
func (p *imagePreviewController) Open(overlay *imgpkg.Preview, channel, ts string, attIdx int) {
	p.overlay = overlay
	p.channel = channel
	p.ts = ts
	p.attIdx = attIdx
}

// Close dismisses the current preview. Safe to call when no preview
// is open (no-op). Note: leaves the channel/ts/attIdx fields populated
// — they're meaningless once overlay is nil and Active() returns false.
func (p *imagePreviewController) Close() {
	if p.overlay == nil {
		return
	}
	p.overlay.Close()
	p.overlay = nil
}

// SetAttIdx updates the remembered attachment index. Called from the
// previewLoadedMsg arm after a cycle swap so the next cycle key starts
// from the new position.
func (p *imagePreviewController) SetAttIdx(idx int) { p.attIdx = idx }
