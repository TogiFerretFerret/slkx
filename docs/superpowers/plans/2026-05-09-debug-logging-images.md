# Debug Logging — Image Render + Fetch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive `[imgrender]` and `[imgfetch]` logging across the image-render-sizing pipeline (cell-pixel detection, target compute, thumb selection, per-protocol render entries) and the image-fetch lifecycle (enqueue → singleflight → semaphore → HTTP → decode → prerender → dispatch → recv → clear), threading a `req_id` correlator end-to-end so a single fetch's full timeline can be grepped.

**Architecture:** Two surfaces, one shared correlator.

**Render-sizing surface:** `debuglog.ImgRender` calls at `image.CellPixels` (startup), `imgrender.computeImageTarget` (per-render), `image.PickThumb` (per-render), and the entry of each protocol renderer (kitty / sixel / halfblock).

**Fetch-lifecycle surface:** A `ReqID uint64` field added to `image.FetchRequest`, `imgrender.ImageReadyMsg`, `imgrender.ImageFailedMsg`, and `blockkit.BlockImageReadyMsg`. The enqueue site (in `imgrender.Renderer.RenderBlock` and `blockkit.fetchOrPlaceholder`) generates the ID via `debuglog.NextReqID()` and threads it through. Each lifecycle stage logs `req_id=N` so `grep req_id=42 slk-debug.log` produces one fetch's complete timeline.

**Tech Stack:** Go 1.26.1, existing `internal/debuglog` package (built in foundation plan).

**Spec:** `docs/superpowers/specs/2026-05-09-comprehensive-debug-logging-design.md`

**Depends on:** `docs/superpowers/plans/2026-05-09-debug-logging-foundation.md` MUST be merged first. Independent of `2026-05-09-debug-logging-cache.md`.

---

## File Structure

| File | Role |
|---|---|
| `internal/image/cellmetrics.go` | `CellPixels` logs source (env / ioctl / fallback). |
| `internal/image/fetcher.go` | Add `ReqID` to `FetchRequest`. Log: singleflight join, semaphore acquire, disk-cache lookup, http-try, http-result, decode, prerender. `PickThumb` logs candidates+choice. |
| `internal/ui/imgrender/imgrender.go` | Add `ReqID` to `ImageReadyMsg`/`ImageFailedMsg`. Generate `ReqID` at enqueue site. Log `computeImageTarget`, RenderBlock decision, enqueue, dispatch. |
| `internal/image/kitty.go` | Log render entry. |
| `internal/image/halfblock.go` | Log render entry. |
| `internal/image/sixel.go` | Log render entry. |
| `internal/ui/messages/blockkit/image.go` | Add `ReqID` to `BlockImageReadyMsg`. Generate `ReqID` at enqueue site. Log enqueue, dispatch. |
| `internal/ui/messages/model.go` | Log `recv` + `clear-fetching` in `HandleImageReady` / `HandleImageFailed`. |
| `internal/ui/thread/model.go` | Same as messages/model.go for the thread panel handlers. |
| `internal/ui/app.go` | Log `recv` in `ImageReadyMsg`/`ImageFailedMsg` handlers (carrying req_id). |

---

## Important conventions for the engineer

- **Run from the repo root.**
- **Test runner:** `go test ./<pkg>/... -run <Name> -v` for targeted, `go test ./...` for full.
- **Build check:** `go build ./...` after every code change.
- **Line numbers drift.** Confirm with `grep -n` first.
- **TDD:** signature-changing tasks (req_id plumbing) get failing tests first. Pure-additive log lines are verified via build + smoke test.
- **Commit after each task.**
- **`req_id=0` is reserved for "not threaded" / legacy.** Tests that exercise zero-value paths don't break, but in production no fetch should log req_id=0.

---

## Task 1: Add `ReqID` field to `image.FetchRequest`

**Files:**
- Modify: `internal/image/fetcher.go`
- Create: `internal/image/fetcher_reqid_test.go`

This is the foundation of the correlator. Adding a uint64 field is additive — zero values still work fine.

- [ ] **Step 1: Write the failing test**

Create `internal/image/fetcher_reqid_test.go`:

```go
package image

import (
	"image"
	"testing"
)

func TestFetchRequest_HasReqID(t *testing.T) {
	r := FetchRequest{
		Key:    "k",
		URL:    "https://example.com/x.png",
		Target: image.Pt(100, 100),
		ReqID:  42,
	}
	if r.ReqID != 42 {
		t.Fatalf("ReqID round-trip failed: %d", r.ReqID)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
go test ./internal/image/ -run TestFetchRequest_HasReqID -v
```

Expected: FAIL with `unknown field ReqID`.

- [ ] **Step 3: Add the field**

In `internal/image/fetcher.go`, find:

```go
// FetchRequest describes one image fetch.
type FetchRequest struct {
	Key        string      // cache key (e.g. "F0123ABCD-720" or "avatar-U123")
	URL        string      // remote URL
	Target     image.Point // target downscale size in pixels (0 = no downscale)
	CellTarget image.Point // optional target in terminal cells; when nonzero,
	// the fetcher will pre-render the image into the
	// active prerender protocol for this cell footprint.
}
```

Change to:

```go
// FetchRequest describes one image fetch.
type FetchRequest struct {
	Key        string      // cache key (e.g. "F0123ABCD-720" or "avatar-U123")
	URL        string      // remote URL
	Target     image.Point // target downscale size in pixels (0 = no downscale)
	CellTarget image.Point // optional target in terminal cells; when nonzero,
	// the fetcher will pre-render the image into the
	// active prerender protocol for this cell footprint.
	// ReqID is a debuglog correlator threaded by the caller from
	// debuglog.NextReqID() at enqueue time. Zero means "not threaded"
	// (e.g. avatar fetches that don't participate in the per-image
	// timeline log). Logged as req_id=N in the [imgfetch] surface.
	ReqID uint64
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
go test ./internal/image/ -run TestFetchRequest_HasReqID -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/fetcher.go internal/image/fetcher_reqid_test.go
git commit -m "image/fetcher: add ReqID field to FetchRequest for log correlation"
```

---

## Task 2: Add `ReqID` to `imgrender.ImageReadyMsg` and `ImageFailedMsg`

**Files:**
- Modify: `internal/ui/imgrender/imgrender.go`
- Create: `internal/ui/imgrender/reqid_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/imgrender/reqid_test.go`:

```go
package imgrender

import "testing"

func TestImageReadyMsg_HasReqID(t *testing.T) {
	m := ImageReadyMsg{Key: "k", ReqID: 42}
	if m.ReqID != 42 {
		t.Fatalf("got %d", m.ReqID)
	}
}

func TestImageFailedMsg_HasReqID(t *testing.T) {
	m := ImageFailedMsg{Key: "k", ReqID: 42}
	if m.ReqID != 42 {
		t.Fatalf("got %d", m.ReqID)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
go test ./internal/ui/imgrender/ -run TestImageReadyMsg_HasReqID -v
```

Expected: FAIL.

- [ ] **Step 3: Add the field to both message types**

In `internal/ui/imgrender/imgrender.go`, find:

```go
// ImageReadyMsg is dispatched by the prefetcher when an image
// attachment has finished downloading and decoding. The host panel
// uses Channel + TS to identify the affected message; Key clears the
// in-flight bit on the renderer that was tracking the fetch.
type ImageReadyMsg struct {
	Channel string
	TS      string
	Key     string
}

// ImageFailedMsg is dispatched when all auth attempts for an image
// have failed. Carries the cache key only; hosts use it to mark the
// key as permanently failed so RenderBlock won't re-spawn a fetch
// goroutine until the channel is switched.
type ImageFailedMsg struct {
	Key string
}
```

Change to:

```go
// ImageReadyMsg is dispatched by the prefetcher when an image
// attachment has finished downloading and decoding. The host panel
// uses Channel + TS to identify the affected message; Key clears the
// in-flight bit on the renderer that was tracking the fetch. ReqID
// is the debuglog correlator threaded from the enqueue site.
type ImageReadyMsg struct {
	Channel string
	TS      string
	Key     string
	ReqID   uint64
}

// ImageFailedMsg is dispatched when all auth attempts for an image
// have failed. Carries the cache key only; hosts use it to mark the
// key as permanently failed so RenderBlock won't re-spawn a fetch
// goroutine until the channel is switched. ReqID is the debuglog
// correlator threaded from the enqueue site.
type ImageFailedMsg struct {
	Key   string
	ReqID uint64
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
go test ./internal/ui/imgrender/ -run TestImageReadyMsg_HasReqID -v
go test ./internal/ui/imgrender/ -run TestImageFailedMsg_HasReqID -v
```

Expected: PASS.

- [ ] **Step 5: Build the rest of the project**

```bash
go build ./...
```

Expected: clean build (the new field is additive — zero values from existing call sites still work).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/imgrender/imgrender.go internal/ui/imgrender/reqid_test.go
git commit -m "imgrender: add ReqID to ImageReady/ImageFailed msgs"
```

---

## Task 3: Add `ReqID` to `blockkit.BlockImageReadyMsg`

**Files:**
- Modify: `internal/ui/messages/blockkit/image.go`
- Create: `internal/ui/messages/blockkit/reqid_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/messages/blockkit/reqid_test.go`:

```go
package blockkit

import "testing"

func TestBlockImageReadyMsg_HasReqID(t *testing.T) {
	m := BlockImageReadyMsg{URL: "u", ReqID: 42}
	if m.ReqID != 42 {
		t.Fatalf("got %d", m.ReqID)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
go test ./internal/ui/messages/blockkit/ -run TestBlockImageReadyMsg_HasReqID -v
```

Expected: FAIL.

- [ ] **Step 3: Add the field**

In `internal/ui/messages/blockkit/image.go`, find:

```go
// BlockImageReadyMsg is dispatched by the prefetcher when a block
// image has finished downloading. The host's Update handler wires
// this to a render-cache invalidation for the matching (Channel, TS)
// message so the next render picks up the cached image.
type BlockImageReadyMsg struct {
	Channel string
	TS      string
	URL     string
}
```

Change to:

```go
// BlockImageReadyMsg is dispatched by the prefetcher when a block
// image has finished downloading. The host's Update handler wires
// this to a render-cache invalidation for the matching (Channel, TS)
// message so the next render picks up the cached image. ReqID is
// the debuglog correlator threaded from the enqueue site.
type BlockImageReadyMsg struct {
	Channel string
	TS      string
	URL     string
	ReqID   uint64
}
```

- [ ] **Step 4: Run test — expect PASS, then build everything**

```bash
go test ./internal/ui/messages/blockkit/ -v
go build ./...
```

Expected: PASS, clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/blockkit/image.go internal/ui/messages/blockkit/reqid_test.go
git commit -m "blockkit: add ReqID to BlockImageReadyMsg"
```

---

## Task 4: Log `CellPixels` (cell-pixel source)

**Files:**
- Modify: `internal/image/cellmetrics.go`

This is a startup-only log line, but critical for the "image renders too small" diagnosis: it tells you whether the cell-pixel measurement came from env override / ioctl / fallback.

- [ ] **Step 1: Modify `CellPixels`**

Find:

```go
func CellPixels(fd int) (pxW, pxH int) {
	if w, ok := atoi(getenv("COLORTERM_CELL_WIDTH")); ok {
		if h, ok := atoi(getenv("COLORTERM_CELL_HEIGHT")); ok {
			return w, h
		}
	}
	if fd >= 0 {
		if w, h, ok := winsizePixels(fd); ok {
			return w, h
		}
	}
	return 8, 16
}
```

Change to:

```go
func CellPixels(fd int) (pxW, pxH int) {
	if w, ok := atoi(getenv("COLORTERM_CELL_WIDTH")); ok {
		if h, ok := atoi(getenv("COLORTERM_CELL_HEIGHT")); ok {
			debuglog.ImgRender("CellPixels: cell_w=%d cell_h=%d source=env_override", w, h)
			return w, h
		}
	}
	if fd >= 0 {
		if w, h, ok := winsizePixels(fd); ok {
			debuglog.ImgRender("CellPixels: cell_w=%d cell_h=%d source=ioctl fd=%d", w, h, fd)
			return w, h
		}
	}
	debuglog.ImgRender("CellPixels: cell_w=8 cell_h=16 source=fallback (no env, no ioctl)")
	return 8, 16
}
```

- [ ] **Step 2: Add the import**

Change:

```go
package image

import "strconv"
```

to:

```go
package image

import (
	"strconv"

	"github.com/gammons/slk/internal/debuglog"
)
```

- [ ] **Step 3: Build and test**

```bash
go build ./... && go test ./internal/image/... -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/image/cellmetrics.go
git commit -m "image/cellmetrics: log CellPixels source (env/ioctl/fallback)"
```

---

## Task 5: Log `imgrender.computeImageTarget`

**Files:**
- Modify: `internal/ui/imgrender/imgrender.go`

The per-render target compute. Logs natural dims, available cols, caps, and the computed target. This is where you spot a `MaxRows` clamp that's truncating an image.

- [ ] **Step 1: Modify `computeImageTarget`**

Find:

```go
func computeImageTarget(thumbs []ThumbSpec, ctx ImageContext, availWidth int) image.Point {
	if len(thumbs) == 0 || ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		return image.Point{}
	}
	largest := thumbs[len(thumbs)-1]
	if largest.W <= 0 || largest.H <= 0 {
		return image.Point{}
	}
	aspect := float64(largest.W) / float64(largest.H)
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)

	rows := ctx.MaxRows
	if rows <= 0 {
		rows = 20
	}
	maxCols := availWidth
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	cols := int(float64(rows) * aspect / cellRatio)
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
	}
	if rows < 1 {
		rows = 1
	}
	return image.Pt(cols, rows)
}
```

Change to:

```go
func computeImageTarget(thumbs []ThumbSpec, ctx ImageContext, availWidth int) image.Point {
	if len(thumbs) == 0 || ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		debuglog.ImgRender("computeImageTarget: thumbs=%d cell_px=(%d,%d) → zero target",
			len(thumbs), ctx.CellPixels.X, ctx.CellPixels.Y)
		return image.Point{}
	}
	largest := thumbs[len(thumbs)-1]
	if largest.W <= 0 || largest.H <= 0 {
		debuglog.ImgRender("computeImageTarget: largest_thumb_dims=(%d,%d) → zero target",
			largest.W, largest.H)
		return image.Point{}
	}
	aspect := float64(largest.W) / float64(largest.H)
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)

	rows := ctx.MaxRows
	if rows <= 0 {
		rows = 20
	}
	maxCols := availWidth
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	cols := int(float64(rows) * aspect / cellRatio)
	if cols < 1 {
		cols = 1
	}
	clamped := false
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
		clamped = true
	}
	if rows < 1 {
		rows = 1
	}
	debuglog.ImgRender("computeImageTarget: natural=(%d,%d) avail_cols=%d MaxCols=%d MaxRows=%d cell_px=(%d,%d) target=(%d,%d) clamped_to_cols=%v",
		largest.W, largest.H, availWidth, ctx.MaxCols, ctx.MaxRows,
		ctx.CellPixels.X, ctx.CellPixels.Y, cols, rows, clamped)
	return image.Pt(cols, rows)
}
```

- [ ] **Step 2: Build and test**

```bash
go build ./... && go test ./internal/ui/imgrender/... -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/imgrender/imgrender.go
git commit -m "imgrender: log computeImageTarget natural→target with clamps"
```

---

## Task 6: Log `image.PickThumb`

**Files:**
- Modify: `internal/image/fetcher.go`

`PickThumb` picks the smallest thumbnail ≥ target on both axes. Logging the candidates and the choice exposes "we picked too small a thumb".

- [ ] **Step 1: Modify `PickThumb`**

Find:

```go
func PickThumb(thumbs []ThumbSpec, target image.Point) (url, suffix string) {
	if len(thumbs) == 0 {
		return "", ""
	}
	// Sort ascending by max(W, H).
	sorted := make([]ThumbSpec, len(thumbs))
	copy(sorted, thumbs)
	sort.Slice(sorted, func(i, j int) bool {
		return max(sorted[i].W, sorted[i].H) < max(sorted[j].W, sorted[j].H)
	})
	for _, t := range sorted {
		if t.W >= target.X && t.H >= target.Y {
			return t.URL, fmt.Sprintf("%d", max(t.W, t.H))
		}
	}
	last := sorted[len(sorted)-1]
```

Change to:

```go
func PickThumb(thumbs []ThumbSpec, target image.Point) (url, suffix string) {
	if len(thumbs) == 0 {
		debuglog.ImgRender("PickThumb: no thumbs available target=(%d,%d)", target.X, target.Y)
		return "", ""
	}
	// Sort ascending by max(W, H).
	sorted := make([]ThumbSpec, len(thumbs))
	copy(sorted, thumbs)
	sort.Slice(sorted, func(i, j int) bool {
		return max(sorted[i].W, sorted[i].H) < max(sorted[j].W, sorted[j].H)
	})
	if debuglog.Enabled() {
		var b strings.Builder
		for i, t := range sorted {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "(%dx%d)", t.W, t.H)
		}
		debuglog.ImgRender("PickThumb: target=(%d,%d) candidates=[%s]",
			target.X, target.Y, b.String())
	}
	for _, t := range sorted {
		if t.W >= target.X && t.H >= target.Y {
			debuglog.ImgRender("PickThumb: chose=(%d,%d) suffix=%d url=%s",
				t.W, t.H, max(t.W, t.H), t.URL)
			return t.URL, fmt.Sprintf("%d", max(t.W, t.H))
		}
	}
	last := sorted[len(sorted)-1]
```

Now find the existing return at the end of `PickThumb` (just below the loop):

```go
	last := sorted[len(sorted)-1]
	return last.URL, fmt.Sprintf("%d", max(last.W, last.H))
}
```

Change to:

```go
	last := sorted[len(sorted)-1]
	debuglog.ImgRender("PickThumb: chose=(%d,%d) suffix=%d url=%s (fallback: largest, no candidate satisfied target)",
		last.W, last.H, max(last.W, last.H), last.URL)
	return last.URL, fmt.Sprintf("%d", max(last.W, last.H))
}
```

- [ ] **Step 2: Build and test**

```bash
go build ./... && go test ./internal/image/... -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/image/fetcher.go
git commit -m "image/fetcher: log PickThumb candidates+choice"
```

---

## Task 7: Log per-protocol render entries (kitty, halfblock, sixel)

**Files:**
- Modify: `internal/image/kitty.go`
- Modify: `internal/image/halfblock.go`
- Modify: `internal/image/sixel.go`

Each protocol's `Render` is the bottom of the render-sizing pipeline. Logging entry+encoded-byte-len lets you confirm the size made it to the encoder and confirm what was emitted (useful for diagnosing terminal-side bugs like ghostty rendering small).

- [ ] **Step 1: Add log to kitty `RenderKey`**

In `internal/image/kitty.go`, find:

```go
func (k *KittyRenderer) RenderKey(key string, target image.Point) Render {
	k.mu.Lock()
	src, ok := k.sources[key]
	k.mu.Unlock()
	if !ok || target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}

	id, fresh := k.registry.Lookup(key, target)

	lines := buildPlaceholderLines(id, target)
```

Change to:

```go
func (k *KittyRenderer) RenderKey(key string, target image.Point) Render {
	k.mu.Lock()
	src, ok := k.sources[key]
	k.mu.Unlock()
	if !ok || target.X <= 0 || target.Y <= 0 {
		debuglog.ImgRender("kitty.RenderKey: key=%s target=(%d,%d) abort reason=%s",
			key, target.X, target.Y,
			func() string {
				if !ok {
					return "no_source"
				}
				return "zero_target"
			}())
		return Render{Cells: target}
	}

	id, fresh := k.registry.Lookup(key, target)

	lines := buildPlaceholderLines(id, target)
	debuglog.ImgRender("kitty.RenderKey: key=%s target=(%d,%d) image_id=%d fresh=%v lines=%d",
		key, target.X, target.Y, id, fresh, len(lines))
```

Then find the `OnFlush` closure that emits the upload:

```go
			r.OnFlush = func(w io.Writer) error {
				if !fired.CompareAndSwap(false, true) {
					return nil
				}
				return emitKittyUpload(w, imgID, payload, cellsCols, cellsRows)
			}
```

Change to:

```go
			r.OnFlush = func(w io.Writer) error {
				if !fired.CompareAndSwap(false, true) {
					return nil
				}
				debuglog.ImgRender("kitty.OnFlush: image_id=%d cells=(%d,%d) payload_len=%d",
					imgID, cellsCols, cellsRows, len(payload))
				return emitKittyUpload(w, imgID, payload, cellsCols, cellsRows)
			}
```

Add the `debuglog` import to `internal/image/kitty.go`. Check the existing imports first; add `"github.com/gammons/slk/internal/debuglog"` to the project-imports group.

- [ ] **Step 2: Add log to `HalfBlockRenderer.Render`**

In `internal/image/halfblock.go`, find:

```go
// Render satisfies the Renderer interface.
func (HalfBlockRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}
	pxW, pxH := target.X, target.Y*2
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	lines := make([]string, target.Y)
```

Change to:

```go
// Render satisfies the Renderer interface.
func (HalfBlockRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		debuglog.ImgRender("halfblock.Render: target=(%d,%d) abort=zero_target", target.X, target.Y)
		return Render{Cells: target}
	}
	debuglog.ImgRender("halfblock.Render: target=(%d,%d) source_bounds=%v",
		target.X, target.Y, img.Bounds())
	pxW, pxH := target.X, target.Y*2
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	lines := make([]string, target.Y)
```

Add the `debuglog` import to `internal/image/halfblock.go`.

- [ ] **Step 3: Add log to `SixelRenderer.Render`**

In `internal/image/sixel.go`, find:

```go
func (s *SixelRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}

	pxW := target.X * 8
	pxH := target.Y * 16
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	var sx bytes.Buffer
	enc := gosixel.NewEncoder(&sx)
	if err := enc.Encode(resized); err != nil {
		return HalfBlockRenderer{}.Render(img, target)
	}
	sixelBytes := sx.Bytes()
```

Change to:

```go
func (s *SixelRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		debuglog.ImgRender("sixel.Render: target=(%d,%d) abort=zero_target", target.X, target.Y)
		return Render{Cells: target}
	}

	pxW := target.X * 8
	pxH := target.Y * 16
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	var sx bytes.Buffer
	enc := gosixel.NewEncoder(&sx)
	if err := enc.Encode(resized); err != nil {
		debuglog.ImgRender("sixel.Render: target=(%d,%d) encode_err=%v fallback=halfblock",
			target.X, target.Y, err)
		return HalfBlockRenderer{}.Render(img, target)
	}
	sixelBytes := sx.Bytes()
	debuglog.ImgRender("sixel.Render: target=(%d,%d) sixel_bytes=%d", target.X, target.Y, len(sixelBytes))
```

Add the `debuglog` import to `internal/image/sixel.go`.

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./internal/image/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/kitty.go internal/image/halfblock.go internal/image/sixel.go
git commit -m "image: log per-protocol render entries (kitty/halfblock/sixel)"
```

---

## Task 8: Log fetch enqueue + dispatch in `imgrender.Renderer.RenderBlock`

**Files:**
- Modify: `internal/ui/imgrender/imgrender.go`

This is the enqueue site. Generate a `req_id` here, log enqueue, thread it into the goroutine, and log dispatch. The goroutine receives the enqueue's req_id and uses it in both the `FetchRequest.ReqID` field AND the dispatched `ImageReadyMsg`/`ImageFailedMsg`.

- [ ] **Step 1: Modify the goroutine block in `RenderBlock`**

Find:

```go
		r.fetching[key] = struct{}{}
		ctx := r.ctx // capture for the goroutine
		go func() {
			_, err := ctx.Fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
				Key:        key,
				URL:        url,
				Target:     pixelTarget,
				CellTarget: target,
			})
			if ctx.SendMsg == nil {
				return
			}
			if err != nil {
				debuglog.ImgFetch("image fetch failed: key=%s url=%s err=%v", key, url, err)
				ctx.SendMsg(ImageFailedMsg{Key: key})
				return
			}
			ctx.SendMsg(ImageReadyMsg{Channel: channel, TS: ts, Key: key})
		}()
		return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
	}
```

Change to:

```go
		r.fetching[key] = struct{}{}
		reqID := debuglog.NextReqID()
		debuglog.ImgFetch("enqueue: key=%s url=%s panel=msgs channel=%s ts=%s req_id=%d fetching_set_size=%d",
			key, url, channel, ts, reqID, len(r.fetching))
		ctx := r.ctx // capture for the goroutine
		go func() {
			fetchStart := time.Now()
			_, err := ctx.Fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
				Key:        key,
				URL:        url,
				Target:     pixelTarget,
				CellTarget: target,
				ReqID:      reqID,
			})
			if ctx.SendMsg == nil {
				debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=skipped (SendMsg=nil)",
					key, reqID, time.Since(fetchStart).Milliseconds())
				return
			}
			if err != nil {
				debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=failed err=%v",
					key, reqID, time.Since(fetchStart).Milliseconds(), err)
				ctx.SendMsg(ImageFailedMsg{Key: key, ReqID: reqID})
				return
			}
			debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=ready",
				key, reqID, time.Since(fetchStart).Milliseconds())
			ctx.SendMsg(ImageReadyMsg{Channel: channel, TS: ts, Key: key, ReqID: reqID})
		}()
		return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
	}
```

- [ ] **Step 2: Add `time` import**

In `internal/ui/imgrender/imgrender.go`, ensure `"time"` is imported. Add it to the stdlib group if missing:

```bash
grep -n '"time"' internal/ui/imgrender/imgrender.go
```

If absent, add it.

- [ ] **Step 3: Build and test**

```bash
go build ./... && go test ./internal/ui/imgrender/... -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/imgrender/imgrender.go
git commit -m "imgrender: log fetch enqueue+dispatch with req_id correlator"
```

---

## Task 9: Log a `RenderBlock` decision summary

**Files:**
- Modify: `internal/ui/imgrender/imgrender.go`

A one-line summary at the END of `RenderBlock` saying which path it took (cached_fast_path / prerendered / spawned_fetch / placeholder / failed_marker). Useful for "why is this image showing the placeholder?".

- [ ] **Step 1: Modify the various return points**

`RenderBlock` has multiple return points. Add a log line just before each. Read the function carefully — line numbers will move as you edit. The patterns to find:

Pattern 1 — non-image / no fetcher / no FileID legacy returns:

```go
	if att.Kind != "image" || r.ctx.Protocol == imgpkg.ProtoOff || r.ctx.Fetcher == nil {
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
	if att.FileID == "" {
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
```

Change to:

```go
	if att.Kind != "image" || r.ctx.Protocol == imgpkg.ProtoOff || r.ctx.Fetcher == nil {
		debuglog.ImgRender("RenderBlock: file_id=%s decision=legacy_text reason=non_image_or_off", att.FileID)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
	if att.FileID == "" {
		debuglog.ImgRender("RenderBlock: decision=legacy_text reason=no_file_id name=%q", att.Name)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
```

Pattern 2 — zero target / no thumb URL:

```go
	target := computeImageTarget(att.Thumbs, r.ctx, availWidth)
	if target.X <= 0 || target.Y <= 0 {
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
```

Change to:

```go
	target := computeImageTarget(att.Thumbs, r.ctx, availWidth)
	if target.X <= 0 || target.Y <= 0 {
		debuglog.ImgRender("RenderBlock: file_id=%s decision=legacy_text reason=zero_target", att.FileID)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
```

```go
	url, suffix := imgpkg.PickThumb(imgThumbs, pixelTarget)
	if url == "" {
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
```

Change to:

```go
	url, suffix := imgpkg.PickThumb(imgThumbs, pixelTarget)
	if url == "" {
		debuglog.ImgRender("RenderBlock: file_id=%s decision=legacy_text reason=no_thumb_url", att.FileID)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
```

Pattern 3 — failed and in-flight placeholders:

```go
	img, cached := r.ctx.Fetcher.Cached(key, pixelTarget)
	if !cached {
		if _, failed := r.failed[key]; failed {
			return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
		}
		if _, inFlight := r.fetching[key]; inFlight {
			return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
		}
		r.fetching[key] = struct{}{}
```

Change to:

```go
	img, cached := r.ctx.Fetcher.Cached(key, pixelTarget)
	if !cached {
		if _, failed := r.failed[key]; failed {
			debuglog.ImgRender("RenderBlock: key=%s decision=placeholder_failed", key)
			return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
		}
		if _, inFlight := r.fetching[key]; inFlight {
			debuglog.ImgRender("RenderBlock: key=%s decision=placeholder_in_flight", key)
			return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
		}
		r.fetching[key] = struct{}{}
```

Pattern 4 — cached fast and slow paths. Find the cached returns near the end of the function and add a log line before each return. Run `grep -n 'return BlockResult' internal/ui/imgrender/imgrender.go` to enumerate them.

For the "Fast path: prerendered output baked off the UI thread" return:

```go
	// Fast path: prerendered output baked off the UI thread.
	if pr, ok := r.ctx.Fetcher.Prerendered(key, target, r.ctx.Protocol); ok {
```

Add inside the `if pr, ok := ...; ok {` block, before the `return`:

Find:

```go
		return BlockResult{Lines: pr.Lines, Flushes: fl, SixelRows: sxlMap, Height: target.Y, Hit: hit}
	}
```

Change to:

```go
		debuglog.ImgRender("RenderBlock: key=%s decision=prerendered proto=%v target=(%d,%d)",
			key, r.ctx.Protocol, target.X, target.Y)
		return BlockResult{Lines: pr.Lines, Flushes: fl, SixelRows: sxlMap, Height: target.Y, Hit: hit}
	}
```

For the slow-path kitty return:

```go
	// Slow path: prerender wasn't populated. Encode on this goroutine.
	if r.ctx.Protocol == imgpkg.ProtoKitty && r.ctx.KittyRender != nil {
		ckey := "F-" + att.FileID
		r.ctx.KittyRender.SetSource(ckey, img)
		out := r.ctx.KittyRender.RenderKey(ckey, target)
		var fl []func(io.Writer) error
		if out.OnFlush != nil {
			fl = []func(io.Writer) error{out.OnFlush}
		}
		return BlockResult{Lines: out.Lines, Flushes: fl, Height: target.Y, Hit: hit}
	}
```

Change the return inside this block to:

```go
		debuglog.ImgRender("RenderBlock: key=%s decision=cached_kitty_slow target=(%d,%d)",
			key, target.X, target.Y)
		return BlockResult{Lines: out.Lines, Flushes: fl, Height: target.Y, Hit: hit}
	}
```

For the final cached non-kitty return at the end:

```go
	out := imgpkg.RenderImage(r.ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	var sxlMap map[int]SixelEntry
	if r.ctx.Protocol == imgpkg.ProtoSixel {
		// ...
	} else if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	return BlockResult{Lines: out.Lines, Flushes: fl, SixelRows: sxlMap, Height: target.Y, Hit: hit}
}
```

Change to:

```go
	out := imgpkg.RenderImage(r.ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	var sxlMap map[int]SixelEntry
	if r.ctx.Protocol == imgpkg.ProtoSixel {
		// ...
	} else if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	debuglog.ImgRender("RenderBlock: key=%s decision=cached_slow proto=%v target=(%d,%d)",
		key, r.ctx.Protocol, target.X, target.Y)
	return BlockResult{Lines: out.Lines, Flushes: fl, SixelRows: sxlMap, Height: target.Y, Hit: hit}
}
```

(Don't reformat the inner sixel block — just add the log line before the final `return`.)

- [ ] **Step 2: Build and test**

```bash
go build ./... && go test ./internal/ui/imgrender/... -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/imgrender/imgrender.go
git commit -m "imgrender: log RenderBlock decision summary at every return"
```

---

## Task 10: Log fetch lifecycle in `image.Fetcher.Fetch` / `fetchInner`

**Files:**
- Modify: `internal/image/fetcher.go`

The middle of the lifecycle: singleflight join, semaphore acquire, disk-cache lookup, decode, prerender. Each line tagged with `req_id` (passed in via `FetchRequest.ReqID`).

- [ ] **Step 1: Modify `Fetch`**

Find:

```go
func (f *Fetcher) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	v, err, _ := f.sf.Do(req.Key, func() (any, error) {
		// Cache-only path doesn't need an HTTP slot; check first.
		if _, hit := f.cache.Get(req.Key); !hit {
			select {
			case f.sem <- struct{}{}:
				defer func() { <-f.sem }()
			case <-ctx.Done():
				return FetchResult{}, ctx.Err()
			}
		}
		return f.fetchInner(ctx, req)
	})
	if err != nil {
		return FetchResult{}, err
	}
	return v.(FetchResult), nil
}
```

Change to:

```go
func (f *Fetcher) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	leader := false
	v, err, shared := f.sf.Do(req.Key, func() (any, error) {
		leader = true
		debuglog.ImgFetch("sf-leader: key=%s req_id=%d", req.Key, req.ReqID)
		// Cache-only path doesn't need an HTTP slot; check first.
		if _, hit := f.cache.Get(req.Key); !hit {
			semStart := time.Now()
			select {
			case f.sem <- struct{}{}:
				debuglog.ImgFetch("sem-acquire: key=%s req_id=%d wait_ms=%d",
					req.Key, req.ReqID, time.Since(semStart).Milliseconds())
				defer func() { <-f.sem }()
			case <-ctx.Done():
				debuglog.ImgFetch("sem-cancel: key=%s req_id=%d wait_ms=%d err=%v",
					req.Key, req.ReqID, time.Since(semStart).Milliseconds(), ctx.Err())
				return FetchResult{}, ctx.Err()
			}
		} else {
			debuglog.ImgFetch("sem-skip: key=%s req_id=%d (disk cache hit, no HTTP needed)",
				req.Key, req.ReqID)
		}
		return f.fetchInner(ctx, req)
	})
	if !leader {
		debuglog.ImgFetch("sf-join: key=%s req_id=%d shared=%v leader=false",
			req.Key, req.ReqID, shared)
	}
	if err != nil {
		return FetchResult{}, err
	}
	return v.(FetchResult), nil
}
```

- [ ] **Step 2: Modify `fetchInner` for disk-cache + decode + prerender logs**

Find:

```go
func (f *Fetcher) fetchInner(ctx context.Context, req FetchRequest) (FetchResult, error) {
	path, hit := f.cache.Get(req.Key)
	if !hit {
		body, ct, err := f.download(ctx, req.URL)
		if err != nil {
			return FetchResult{}, err
		}
		ext := extFromMime(ct, req.URL)
		path, err = f.cache.Put(req.Key, ext, body)
		if err != nil {
			return FetchResult{}, err
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return FetchResult{}, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		// Cache poisoning recovery: a previously persisted file isn't
		// decodable as an image (e.g., an HTML auth-failure response from
		// before this auth path was wired up). Evict so the next Fetch
		// re-downloads with the now-correct credentials.
		file.Close()
		f.cache.Delete(req.Key)
		return FetchResult{}, fmt.Errorf("decode %s: %w (cache evicted)", path, err)
	}

	if req.Target.X > 0 && req.Target.Y > 0 {
		img = downscale(img, req.Target)
	}

	// Populate the render-time memo so the UI thread's Cached() call
	// becomes a pure map lookup instead of os.Open + image.Decode +
	// downscale. Critical for keeping the bubbletea Update goroutine
	// responsive when many images arrive in a burst (channel switch
	// or scroll-up into unseen history).
	f.decoded.Store(decodedMemoKey(req.Key, req.Target), img)

	// Eagerly run protocol encoding off the UI thread so the next
	// View() doesn't have to. Skipped when not configured or when
	// CellTarget is zero (e.g., avatars and full-screen preview).
	f.maybePrerender(req.Key, img, req.CellTarget)

	mime := mimeFromExt(filepath.Ext(path))
	return FetchResult{Img: img, Source: path, Mime: mime}, nil
}
```

Change to:

```go
func (f *Fetcher) fetchInner(ctx context.Context, req FetchRequest) (FetchResult, error) {
	path, hit := f.cache.Get(req.Key)
	debuglog.ImgFetch("disk-cache: key=%s req_id=%d result=%s",
		req.Key, req.ReqID,
		func() string {
			if hit {
				return "hit"
			}
			return "miss"
		}())
	if !hit {
		dlStart := time.Now()
		body, ct, err := f.download(ctx, req.URL)
		if err != nil {
			debuglog.ImgFetch("download-err: key=%s req_id=%d dur_ms=%d err=%v",
				req.Key, req.ReqID, time.Since(dlStart).Milliseconds(), err)
			return FetchResult{}, err
		}
		debuglog.ImgFetch("download-ok: key=%s req_id=%d dur_ms=%d bytes=%d ct=%s",
			req.Key, req.ReqID, time.Since(dlStart).Milliseconds(), len(body), ct)
		ext := extFromMime(ct, req.URL)
		path, err = f.cache.Put(req.Key, ext, body)
		if err != nil {
			debuglog.ImgFetch("cache-put-err: key=%s req_id=%d err=%v", req.Key, req.ReqID, err)
			return FetchResult{}, err
		}
	}

	file, err := os.Open(path)
	if err != nil {
		debuglog.ImgFetch("open-err: key=%s req_id=%d path=%s err=%v",
			req.Key, req.ReqID, path, err)
		return FetchResult{}, err
	}
	defer file.Close()

	decStart := time.Now()
	img, _, err := image.Decode(file)
	if err != nil {
		// Cache poisoning recovery: a previously persisted file isn't
		// decodable as an image (e.g., an HTML auth-failure response from
		// before this auth path was wired up). Evict so the next Fetch
		// re-downloads with the now-correct credentials.
		file.Close()
		f.cache.Delete(req.Key)
		debuglog.ImgFetch("decode-err: key=%s req_id=%d path=%s err=%v (cache evicted)",
			req.Key, req.ReqID, path, err)
		return FetchResult{}, fmt.Errorf("decode %s: %w (cache evicted)", path, err)
	}
	bounds := img.Bounds()
	debuglog.ImgFetch("decode: key=%s req_id=%d dur_ms=%d dims=(%d,%d)",
		req.Key, req.ReqID, time.Since(decStart).Milliseconds(),
		bounds.Dx(), bounds.Dy())

	if req.Target.X > 0 && req.Target.Y > 0 {
		img = downscale(img, req.Target)
	}

	// Populate the render-time memo so the UI thread's Cached() call
	// becomes a pure map lookup instead of os.Open + image.Decode +
	// downscale. Critical for keeping the bubbletea Update goroutine
	// responsive when many images arrive in a burst (channel switch
	// or scroll-up into unseen history).
	f.decoded.Store(decodedMemoKey(req.Key, req.Target), img)

	// Eagerly run protocol encoding off the UI thread so the next
	// View() doesn't have to. Skipped when not configured or when
	// CellTarget is zero (e.g., avatars and full-screen preview).
	prStart := time.Now()
	f.maybePrerender(req.Key, img, req.CellTarget)
	debuglog.ImgFetch("prerender: key=%s req_id=%d cell_target=(%d,%d) dur_ms=%d",
		req.Key, req.ReqID, req.CellTarget.X, req.CellTarget.Y,
		time.Since(prStart).Milliseconds())

	mime := mimeFromExt(filepath.Ext(path))
	return FetchResult{Img: img, Source: path, Mime: mime}, nil
}
```

- [ ] **Step 3: Add log to `tryDownload` (HTTP-attempt log)**

Find:

```go
func (f *Fetcher) tryDownload(ctx context.Context, url string, auth TeamAuth) ([]byte, string, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", 0, err
	}
	httpReq.Header.Set("User-Agent", "slk/inline-image-fetcher")
	if auth.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+auth.Token)
	}
	if auth.DCookie != "" {
		// Inline cookie header: a shared cookie jar can hold only one
		// 'd' value at a time but workspaces may have different ones.
		httpReq.Header.Set("Cookie", "d="+auth.DCookie)
	}
	resp, err := f.http.Do(httpReq)
	if err != nil {
		return nil, "", 0, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		// Drain body for connection reuse, but we don't return it.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, ct, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ct, resp.StatusCode, err
	}
	return body, ct, resp.StatusCode, nil
}
```

Change to:

```go
func (f *Fetcher) tryDownload(ctx context.Context, url string, auth TeamAuth) ([]byte, string, int, error) {
	httpStart := time.Now()
	debuglog.ImgFetch("http-try: url=%s auth_team=%q has_token=%v",
		url, auth.TeamID, auth.Token != "")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", 0, err
	}
	httpReq.Header.Set("User-Agent", "slk/inline-image-fetcher")
	if auth.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+auth.Token)
	}
	if auth.DCookie != "" {
		// Inline cookie header: a shared cookie jar can hold only one
		// 'd' value at a time but workspaces may have different ones.
		httpReq.Header.Set("Cookie", "d="+auth.DCookie)
	}
	resp, err := f.http.Do(httpReq)
	if err != nil {
		debuglog.ImgFetch("http-result: url=%s dur_ms=%d transport_err=%v",
			url, time.Since(httpStart).Milliseconds(), err)
		return nil, "", 0, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		// Drain body for connection reuse, but we don't return it.
		_, _ = io.Copy(io.Discard, resp.Body)
		debuglog.ImgFetch("http-result: url=%s status=%d ct=%q dur_ms=%d body_drained",
			url, resp.StatusCode, ct, time.Since(httpStart).Milliseconds())
		return nil, ct, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		debuglog.ImgFetch("http-result: url=%s status=%d ct=%q dur_ms=%d body_read_err=%v",
			url, resp.StatusCode, ct, time.Since(httpStart).Milliseconds(), err)
		return nil, ct, resp.StatusCode, err
	}
	debuglog.ImgFetch("http-result: url=%s status=%d ct=%q dur_ms=%d bytes=%d",
		url, resp.StatusCode, ct, time.Since(httpStart).Milliseconds(), len(body))
	return body, ct, resp.StatusCode, nil
}
```

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./internal/image/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/fetcher.go
git commit -m "image/fetcher: log fetch lifecycle (sf/sem/disk/decode/http/prerender)"
```

---

## Task 11: Log recv + clear-fetching in app + messages/thread models

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/messages/model.go`
- Modify: `internal/ui/thread/model.go`

The closing parens of the lifecycle: when bubbletea's Update receives the dispatched message, the panel's handler clears the fetching bit. Logging recv + clear lets you spot stuck states where the message was dispatched but the panel never cleared its fetching set (or vice versa).

- [ ] **Step 1: Log recv in `internal/ui/app.go`**

Find (around line 1393):

```go
	case imgrender.ImageReadyMsg:
		// Image attachment finished downloading; invalidate the
		// messages pane's render cache for the affected channel so the
		// next View() picks up the cached bytes inline. Only the
		// specific key's in-flight bit is cleared so sibling images
		// that are still mid-fetch don't trigger fresh respawns. The
		// model itself filters by active channel name (no-op when the
		// user has switched away).
		a.messagepane.HandleImageReady(msg.Channel, msg.TS, msg.Key)
```

Change to:

```go
	case imgrender.ImageReadyMsg:
		debuglog.ImgFetch("recv: kind=ready channel=%s ts=%s key=%s req_id=%d",
			msg.Channel, msg.TS, msg.Key, msg.ReqID)
		// Image attachment finished downloading; invalidate the
		// messages pane's render cache for the affected channel so the
		// next View() picks up the cached bytes inline. Only the
		// specific key's in-flight bit is cleared so sibling images
		// that are still mid-fetch don't trigger fresh respawns. The
		// model itself filters by active channel name (no-op when the
		// user has switched away).
		a.messagepane.HandleImageReady(msg.Channel, msg.TS, msg.Key)
```

Find:

```go
	case imgrender.ImageFailedMsg:
		// Image attachment fetch hit a permanent failure (all auths
		// exhausted, or some other terminal error). Clear the in-flight
		// bit so a future cache invalidation doesn't keep retrying;
		// don't trigger a re-render — the placeholder is already on
		// screen and we have no new bytes to show.
		a.messagepane.HandleImageFailed(msg.Key)
```

Change to:

```go
	case imgrender.ImageFailedMsg:
		debuglog.ImgFetch("recv: kind=failed key=%s req_id=%d", msg.Key, msg.ReqID)
		// Image attachment fetch hit a permanent failure (all auths
		// exhausted, or some other terminal error). Clear the in-flight
		// bit so a future cache invalidation doesn't keep retrying;
		// don't trigger a re-render — the placeholder is already on
		// screen and we have no new bytes to show.
		a.messagepane.HandleImageFailed(msg.Key)
```

If `internal/ui/app.go` doesn't yet import `debuglog` (the cache plan is independent and may not be merged), add the import to the project-imports group.

- [ ] **Step 2: Log clear-fetching in `messages.Model.HandleImageReady`**

Find (around line 338):

```go
func (m *Model) HandleImageReady(channel, ts, key string) {
	if channel != m.channelName {
		return
	}
	if key == "" {
		// Legacy path: no per-key bookkeeping available, so we fall
		// back to the wholesale invalidation that the new fast path
		// is meant to avoid. Used by tests that drive transitions
		// synchronously without a real fetch key.
		m.cache = nil
		if m.imgRenderer != nil {
			m.imgRenderer.ResetFailed()
		}
		m.dirty()
		return
	}
	if m.imgRenderer != nil {
		m.imgRenderer.ClearFetching(key)
	}
```

Change to:

```go
func (m *Model) HandleImageReady(channel, ts, key string) {
	if channel != m.channelName {
		debuglog.ImgFetch("messages.HandleImageReady: channel=%q active_channel=%q key=%s SKIP (not active)",
			channel, m.channelName, key)
		return
	}
	if key == "" {
		debuglog.ImgFetch("messages.HandleImageReady: channel=%q ts=%s key=<empty> legacy_path",
			channel, ts)
		// Legacy path: no per-key bookkeeping available, so we fall
		// back to the wholesale invalidation that the new fast path
		// is meant to avoid. Used by tests that drive transitions
		// synchronously without a real fetch key.
		m.cache = nil
		if m.imgRenderer != nil {
			m.imgRenderer.ResetFailed()
		}
		m.dirty()
		return
	}
	if m.imgRenderer != nil {
		cleared := m.imgRenderer.ClearFetching(key)
		debuglog.ImgFetch("messages.HandleImageReady: channel=%q key=%s cleared=%v",
			channel, key, cleared)
	}
```

- [ ] **Step 3: Log mark-failed in `messages.Model.HandleImageFailed`**

Find:

```go
func (m *Model) HandleImageFailed(key string) {
	if key == "" {
		return
	}
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	m.imgRenderer.MarkFailed(key)
}
```

Change to:

```go
func (m *Model) HandleImageFailed(key string) {
	if key == "" {
		return
	}
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	tracked := m.imgRenderer.MarkFailed(key)
	debuglog.ImgFetch("messages.HandleImageFailed: channel=%q key=%s was_in_flight=%v",
		m.channelName, key, tracked)
}
```

The `debuglog` import is already added in the cache plan. If you're running this plan independently of the cache plan, add it now.

- [ ] **Step 4: Mirror the same logs in `internal/ui/thread/model.go`**

Open `internal/ui/thread/model.go`. Find its `HandleImageReady` and `HandleImageFailed` (search with `grep -n 'HandleImageReady\|HandleImageFailed' internal/ui/thread/model.go`). Apply the same pattern: log entry with key + cleared/tracked status. Use `[imgfetch] thread.HandleImageReady: ...` and `[imgfetch] thread.HandleImageFailed: ...` so they're distinguishable from the messages-pane lines.

- [ ] **Step 5: Build and test**

```bash
go build ./... && go test ./internal/ui/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "ui: log image-fetch recv + clear-fetching across panels"
```

---

## Task 12: Log enqueue + dispatch in `blockkit` parallel path

**Files:**
- Modify: `internal/ui/messages/blockkit/image.go`

The block-kit path has its own goroutine and its own in-flight set; mirror the same enqueue+dispatch logging for consistency.

- [ ] **Step 1: Modify `fetchOrPlaceholder`'s goroutine block**

Find:

```go
		if !busy {
			channel := ctx.Channel
			ts := ctx.MessageTS
			send := ctx.SendMsg
			fetcher := ctx.Fetcher
			go func() {
				_, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
					Key: key, URL: url, Target: pixelTarget,
				})
				inflightURLMu.Lock()
				delete(inflightURL, key)
				inflightURLMu.Unlock()
				if err != nil {
					debuglog.ImgFetch("blockkit image fetch failed: key=%s url=%s err=%v", key, url, err)
					return
				}
				if send != nil {
					send(BlockImageReadyMsg{Channel: channel, TS: ts, URL: url})
				}
			}()
		}
```

Change to:

```go
		if !busy {
			channel := ctx.Channel
			ts := ctx.MessageTS
			send := ctx.SendMsg
			fetcher := ctx.Fetcher
			reqID := debuglog.NextReqID()
			debuglog.ImgFetch("enqueue: key=%s url=%s panel=blockkit channel=%s ts=%s req_id=%d",
				key, url, channel, ts, reqID)
			go func() {
				fetchStart := time.Now()
				_, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
					Key: key, URL: url, Target: pixelTarget, ReqID: reqID,
				})
				inflightURLMu.Lock()
				delete(inflightURL, key)
				inflightURLMu.Unlock()
				if err != nil {
					debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=failed (blockkit) err=%v",
						key, reqID, time.Since(fetchStart).Milliseconds(), err)
					debuglog.ImgFetch("blockkit image fetch failed: key=%s url=%s err=%v", key, url, err)
					return
				}
				if send != nil {
					debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=ready (blockkit)",
						key, reqID, time.Since(fetchStart).Milliseconds())
					send(BlockImageReadyMsg{Channel: channel, TS: ts, URL: url, ReqID: reqID})
				} else {
					debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=skipped (blockkit, SendMsg=nil)",
						key, reqID, time.Since(fetchStart).Milliseconds())
				}
			}()
		}
```

- [ ] **Step 2: Add `time` import**

Check the existing imports in `internal/ui/messages/blockkit/image.go` and add `"time"` if missing.

- [ ] **Step 3: Build and test**

```bash
go build ./... && go test ./internal/ui/messages/... -v
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages/blockkit/image.go
git commit -m "blockkit: log enqueue+dispatch with req_id correlator"
```

---

## Task 13: Manual end-to-end verification

**Files:** None (verification only)

Confirm the image logging surface produces a coherent timeline.

- [ ] **Step 1: Build and run with `SLK_DEBUG=1`**

```bash
go build -o /tmp/slk-img-verify ./cmd/slk
mkdir -p /tmp/slk-img-verify-dir
cd /tmp/slk-img-verify-dir
SLK_DEBUG=1 /tmp/slk-img-verify
```

Inside slk, navigate to a channel that has at least one image attachment. Wait for the image to load (or fail). Note the file/key. Quit (`Ctrl+C` or `q`).

- [ ] **Step 2: Inspect startup render-sizing logs**

```bash
grep '\[imgrender\] CellPixels\|\[imgrender\] image protocol\|\[imgrender\] kitty' /tmp/slk-img-verify-dir/slk-debug.log
```

Expected: at least one line each for `CellPixels:`, image protocol detection, and (on a kitty/ghostty terminal) the kitty probe outcome.

- [ ] **Step 3: Inspect a single fetch's full timeline**

Pick a `req_id` from the log and grep for it:

```bash
grep 'req_id=1' /tmp/slk-img-verify-dir/slk-debug.log
```

Expected (in order, more or less):

1. `[imgfetch] enqueue: key=… url=… req_id=1`
2. `[imgfetch] sf-leader: key=… req_id=1`
3. `[imgfetch] sem-acquire: key=… req_id=1 wait_ms=0` (or sem-skip if disk-cache hit)
4. `[imgfetch] disk-cache: key=… req_id=1 result=miss`
5. `[imgfetch] http-try: url=…`
6. `[imgfetch] http-result: url=… status=200 dur_ms=…`
7. `[imgfetch] download-ok: key=… req_id=1 …`
8. `[imgfetch] decode: key=… req_id=1 …`
9. `[imgfetch] prerender: key=… req_id=1 …`
10. `[imgfetch] dispatch: key=… req_id=1 kind=ready`
11. `[imgfetch] recv: kind=ready … req_id=1`
12. `[imgfetch] messages.HandleImageReady: … cleared=true`

If the timeline ends prematurely, the log tells you exactly where:
- ends at `enqueue` only → `sf-join` shows it joined an existing leader; check that leader's req_id
- ends at `http-try` with no `http-result` → HTTP hang
- ends at `dispatch` with no `recv` → SendMsg was nil
- ends at `recv` with no `cleared=` → bug in panel handler

- [ ] **Step 4: Inspect render-sizing for the same image**

```bash
grep '\[imgrender\] PickThumb\|\[imgrender\] computeImageTarget\|\[imgrender\] RenderBlock' /tmp/slk-img-verify-dir/slk-debug.log | head -20
```

Expected: a `computeImageTarget` line showing natural→target dimensions, a `PickThumb` line showing candidates+choice, and a `RenderBlock` decision summary. If the image rendered too small, this is where you read backwards from.

- [ ] **Step 5: Inspect an avatar fetch (req_id=0 path)**

Avatar fetches don't currently thread a `req_id` (they use `image.FetchRequest` with the zero value). They'll still log most lifecycle stages but `req_id=0`. This is intentional — avatars churn too fast to correlate per-fetch. Confirm:

```bash
grep 'req_id=0' /tmp/slk-img-verify-dir/slk-debug.log | head -5
```

Expected: a few lines, all from avatar fetches (you can tell by the `key=avatar-…` prefix).

- [ ] **Step 6: No commit needed**

If everything checks out, this plan is complete.

---

## Self-review checklist (run before declaring done)

- [ ] All call sites in spec sections "Logging surface — focus area 2" and "focus area 3" are covered.
- [ ] `req_id` threads from `enqueue` to `recv` to `clear-fetching` for at least one image fetch, end-to-end.
- [ ] No log lines hardcode `[imgrender]` or `[imgfetch]` inside the format string (the prefix should come ONLY from `debuglog.ImgRender` / `debuglog.ImgFetch`).
- [ ] No `log.Printf` references newly introduced — all new logs use `debuglog`.
- [ ] `go test ./...` passes.
- [ ] `go build ./...` clean.
- [ ] Manual smoke test produced a coherent per-fetch timeline.
