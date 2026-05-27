# Phase 5: Emoji Image Placement Helper

> Index: `00-overview.md`. Previous: `04-width-and-probe.md`. Next: `06-messages-and-reactions.md`.

**Goal:** A single function `emoji.Place(ctx, url, cells)` that every UI surface calls to render an emoji image. Encapsulates the warm/cold-cache logic and the bubbletea re-render signal, so the messages pane, thread pane, reaction pills, picker, and autocomplete dropdown all share one code path.

Warm cache: returns the kitty unicode-placeholder string (exactly `cells` chars wide) and a flush callback that emits the kitty graphics protocol upload (idempotent — the existing `Registry.MarkUploaded` deduplicates across all callers).

Cold cache: returns `cells`-wide spaces, spawns an async fetch goroutine, and on completion dispatches `EmojiImageReadyMsg{URL}` so the host can invalidate its render caches and re-render. Per-URL deduplication ensures one goroutine per URL even when dozens of UI surfaces ask for the same emoji simultaneously.

**Files:**
- Create: `internal/emoji/place.go`
- Create: `internal/emoji/place_test.go`

---

### Task 5.1: Types and skeleton

**Files:**
- Create: `internal/emoji/place.go`

- [ ] **Step 1: Write the skeleton**

Create `internal/emoji/place.go`:

```go
package emoji

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	goimage "image"
	"io"
	"strings"
	"sync"

	imgpkg "github.com/gammons/slk/internal/image"
)

// PlaceFetcher is the subset of *image.Fetcher's API that Place uses.
// Defined as an interface so tests can substitute a fake without
// constructing a full Fetcher + HTTP server.
type PlaceFetcher interface {
	Fetch(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error)
	Prerendered(key string, cellTarget goimage.Point, proto imgpkg.Protocol) (imgpkg.Render, bool)
}

// PlaceContext bundles the dependencies Place needs. Built once per
// app instance and passed to every UI surface that renders emoji.
type PlaceContext struct {
	// Fetcher downloads + decodes + (via ConfigurePrerender) pre-encodes
	// the kitty payload. Required.
	Fetcher PlaceFetcher

	// SendMsg dispatches an EmojiImageReadyMsg when a cold-cache fetch
	// completes. Typically wraps bubbletea Program.Send. If nil, the
	// fetch still runs but no re-render is signaled — useful in tests.
	SendMsg func(msg any)
}

// EmojiImageReadyMsg is dispatched via PlaceContext.SendMsg when a
// previously-cold emoji image has finished fetching and is now
// renderable from the warm path. UI surfaces that buffer emoji
// placements should invalidate their render caches for any entry
// referencing this URL.
//
// Unlike BlockImageReadyMsg (per-message), EmojiImageReadyMsg is
// global — the same emoji can appear in any message, the picker, the
// autocomplete dropdown, etc. Reducers should treat it as a
// coarse-grained invalidation signal across all surfaces that render
// emoji.
type EmojiImageReadyMsg struct {
	URL string
}

// inflightEmoji guards against firing multiple fetch goroutines for
// the same URL when the cold-cache branch is reached concurrently
// from multiple UI surfaces (e.g., the same :thumbsup: appearing in
// 10 messages on the same View() pass).
var (
	inflightEmoji   = map[string]struct{}{}
	inflightEmojiMu sync.Mutex
)

// EmojiCacheKey returns the cache key used by Place for url. Stable
// hash of the URL with an "E-" prefix to isolate emoji entries from
// avatars, attachments, and block-kit images in the shared disk cache.
//
// Exported so reducers can correlate EmojiImageReadyMsg URLs to cache
// entries when wiring re-render invalidation.
func EmojiCacheKey(url string) string {
	sum := sha1.Sum([]byte(url))
	return "E-" + hex.EncodeToString(sum[:8])
}

// Place returns the kitty placement string + optional flush callback
// for url at the given cell footprint (cells wide x 1 row tall).
//
// Returns ("", nil, false) when:
//   - url is empty
//   - ctx.Fetcher is nil
//   - cells < 1
//
// Warm path (image already fetched + prerendered for this cells target):
// returns (placement, flush, true). placement is exactly cells chars
// wide; flush MUST be invoked once per frame to upload the kitty
// payload (idempotent via the registry — multiple invocations are
// safe and the first wins).
//
// Cold path: returns (strings.Repeat(" ", cells), nil, true). Spawns
// an async fetch (deduplicated per URL across all callers) and, on
// completion, dispatches EmojiImageReadyMsg{URL: url} via
// ctx.SendMsg.
func Place(ctx PlaceContext, url string, cells int) (string, func(io.Writer) error, bool) {
	if url == "" || ctx.Fetcher == nil || cells < 1 {
		return "", nil, false
	}
	// Placeholder; tasks 5.3 / 5.5 fill in warm and cold paths.
	return strings.Repeat(" ", cells), nil, true
}
```

- [ ] **Step 2: Build to confirm**

Run: `go build ./internal/emoji/`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/emoji/place.go
git commit -m "feat(emoji): PlaceContext and Place skeleton with EmojiImageReadyMsg"
```

---

### Task 5.2: Failing test — invalid inputs

**Files:**
- Create: `internal/emoji/place_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/place_test.go`:

```go
package emoji

import (
	"context"
	"errors"
	goimage "image"
	"sync"
	"testing"
	"time"

	imgpkg "github.com/gammons/slk/internal/image"
)

// fakeFetcher implements PlaceFetcher for unit tests. Behavior is
// controlled by the prerender map (warm hits) and a fetchFn closure
// (cold-path fetch behavior).
type fakeFetcher struct {
	mu         sync.Mutex
	prerender  map[string]imgpkg.Render // keyed by "<key>|<cx>x<cy>"
	fetchFn    func(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error)
	fetchCalls []imgpkg.FetchRequest
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{prerender: map[string]imgpkg.Render{}}
}

func (f *fakeFetcher) prerenderKey(key string, t goimage.Point) string {
	return key + "|" + itoa(t.X) + "x" + itoa(t.Y)
}

func itoa(n int) string {
	// avoid strconv import here for brevity in tests
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (f *fakeFetcher) setPrerendered(key string, target goimage.Point, r imgpkg.Render) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prerender[f.prerenderKey(key, target)] = r
}

func (f *fakeFetcher) Prerendered(key string, t goimage.Point, proto imgpkg.Protocol) (imgpkg.Render, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.prerender[f.prerenderKey(key, t)]
	return r, ok
}

func (f *fakeFetcher) Fetch(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	f.mu.Lock()
	f.fetchCalls = append(f.fetchCalls, req)
	fn := f.fetchFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return imgpkg.FetchResult{}, errors.New("fakeFetcher: no fetchFn set")
}

func TestPlace_InvalidInputs(t *testing.T) {
	ff := newFakeFetcher()
	ctx := PlaceContext{Fetcher: ff}

	cases := []struct {
		name string
		url  string
		cell int
		fctx PlaceContext
	}{
		{"empty url", "", 2, ctx},
		{"zero cells", "https://x", 0, ctx},
		{"negative cells", "https://x", -1, ctx},
		{"nil fetcher", "https://x", 2, PlaceContext{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, flush, ok := Place(c.fctx, c.url, c.cell)
			if ok {
				t.Errorf("Place(%q, %d) = (%q, %v, true), want ok=false", c.url, c.cell, s, flush)
			}
			if s != "" {
				t.Errorf("Place(%q, %d) placement = %q, want \"\"", c.url, c.cell, s)
			}
		})
	}

	// The fetcher should not have been called for any of these inputs.
	if len(ff.fetchCalls) != 0 {
		t.Errorf("fetcher was called %d times for invalid inputs, want 0", len(ff.fetchCalls))
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestPlace_InvalidInputs -v`
Expected: PASS — the skeleton from Task 5.1 already returns ok=false for the documented invalid inputs.

- [ ] **Step 3: Commit**

```bash
git add internal/emoji/place_test.go
git commit -m "test(emoji): cover Place invalid-input rejection"
```

---

### Task 5.3: Failing test — warm path

**Files:**
- Modify: `internal/emoji/place_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/place_test.go`:

```go
func TestPlace_WarmPath_ReturnsKittyLine(t *testing.T) {
	ff := newFakeFetcher()
	url := "https://a.slack-edge.com/...1f44d.png"
	key := EmojiCacheKey(url)
	target := goimage.Pt(2, 1)

	// Seed a prerender hit: 2-cell-wide kitty placement string.
	wantLine := "\U0010EEEE\U0010EEEE"      // two kitty placeholder runes (the real renderer emits this with diacritics + SGR fg; for the unit test, any deterministic placement string is fine)
	flushCalled := 0
	ff.setPrerendered(key, target, imgpkg.Render{
		Cells:    target,
		Lines:    []string{wantLine},
		Fallback: []string{wantLine},
		OnFlush: func(_ writerInterface) error {
			flushCalled++
			return nil
		},
	})

	ctx := PlaceContext{Fetcher: ff}
	got, flush, ok := Place(ctx, url, 2)
	if !ok {
		t.Fatalf("Place: ok=false, want true (warm path)")
	}
	if got != wantLine {
		t.Errorf("Place placement = %q, want %q", got, wantLine)
	}
	if flush == nil {
		t.Fatalf("Place: flush is nil, want a callback for the warm path")
	}
	// flush is io.Writer-shaped; call with a discarding writer to
	// verify it doesn't panic and increments the counter.
	if err := flush(discardWriter{}); err != nil {
		t.Errorf("flush returned err = %v, want nil", err)
	}
	if flushCalled != 1 {
		t.Errorf("flush invocation count = %d, want 1", flushCalled)
	}

	// No fetch goroutine should have been spawned on the warm path.
	if len(ff.fetchCalls) != 0 {
		t.Errorf("fetcher.Fetch called %d times on warm path, want 0", len(ff.fetchCalls))
	}
}

// writerInterface is an alias to keep imports tidy.
type writerInterface = interface {
	Write(p []byte) (int, error)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestPlace_WarmPath -v`
Expected: FAIL — the skeleton's `Place` returns spaces, not the prerendered line.

---

### Task 5.4: Implement the warm path

**Files:**
- Modify: `internal/emoji/place.go`

- [ ] **Step 1: Replace the `Place` body**

Replace the body of `Place` in `internal/emoji/place.go` with:

```go
func Place(ctx PlaceContext, url string, cells int) (string, func(io.Writer) error, bool) {
	if url == "" || ctx.Fetcher == nil || cells < 1 {
		return "", nil, false
	}

	target := goimage.Pt(cells, 1)
	key := EmojiCacheKey(url)

	// Warm path: a prerendered kitty placement string is ready in the
	// fetcher's prerender memo. The fetcher's worker populated this
	// off the UI thread when the fetch completed.
	if r, ok := ctx.Fetcher.Prerendered(key, target, imgpkg.ProtoKitty); ok {
		if len(r.Lines) > 0 {
			return r.Lines[0], r.OnFlush, true
		}
	}

	// Cold path — implemented in Task 5.6.
	// For now, return a placeholder reservation without spawning a
	// fetch so this task's tests pass in isolation.
	return strings.Repeat(" ", cells), nil, true
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestPlace_WarmPath -v`
Expected: PASS.

- [ ] **Step 3: Run all Place tests**

Run: `go test ./internal/emoji/ -run TestPlace -v`
Expected: PASS — both `TestPlace_InvalidInputs` and `TestPlace_WarmPath`.

- [ ] **Step 4: Commit**

```bash
git add internal/emoji/place.go internal/emoji/place_test.go
git commit -m "feat(emoji): Place warm path returns prerendered kitty line"
```

---

### Task 5.5: Failing test — cold path spawns fetch and dispatches msg

**Files:**
- Modify: `internal/emoji/place_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/place_test.go`:

```go
func TestPlace_ColdPath_SpawnsFetch(t *testing.T) {
	ff := newFakeFetcher()
	url := "https://a.slack-edge.com/...1f44d.png"

	// Configure fetch to succeed instantly. Prerender map is empty so
	// Prerendered returns false — cold path triggered.
	ff.fetchFn = func(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
		return imgpkg.FetchResult{}, nil
	}

	done := make(chan EmojiImageReadyMsg, 1)
	pctx := PlaceContext{
		Fetcher: ff,
		SendMsg: func(m any) {
			if r, ok := m.(EmojiImageReadyMsg); ok {
				done <- r
			}
		},
	}

	got, flush, ok := Place(pctx, url, 2)
	if !ok {
		t.Fatalf("Place: ok=false, want true (cold path)")
	}
	if got != "  " {
		t.Errorf("cold-path placement = %q, want %q (two spaces)", got, "  ")
	}
	if flush != nil {
		t.Errorf("cold-path flush should be nil; got %T", flush)
	}

	// Wait for SendMsg to fire from the fetch goroutine.
	select {
	case msg := <-done:
		if msg.URL != url {
			t.Errorf("EmojiImageReadyMsg.URL = %q, want %q", msg.URL, url)
		}
	case <-time.After(time.Second):
		t.Fatal("SendMsg(EmojiImageReadyMsg) never fired within 1s")
	}

	// Exactly one fetch should have been issued.
	if len(ff.fetchCalls) != 1 {
		t.Errorf("fetcher.Fetch called %d times, want 1", len(ff.fetchCalls))
	}
	if got := ff.fetchCalls[0]; got.URL != url || got.Key != EmojiCacheKey(url) || got.CellTarget != goimage.Pt(2, 1) {
		t.Errorf("FetchRequest = {Key:%q URL:%q CellTarget:%v}, want consistent with url and 2x1",
			got.Key, got.URL, got.CellTarget)
	}
}

func TestPlace_ColdPath_DedupsConcurrentCalls(t *testing.T) {
	ff := newFakeFetcher()
	url := "https://a.slack-edge.com/...1f44d.png"

	gate := make(chan struct{})
	ff.fetchFn = func(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
		<-gate // hold the fetch until the test releases it
		return imgpkg.FetchResult{}, nil
	}

	pctx := PlaceContext{Fetcher: ff, SendMsg: func(any) {}}

	// Fire 20 concurrent Place calls for the same URL.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Place(pctx, url, 2)
		}()
	}

	// Give goroutines a moment to enqueue their fetches.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	// All Place calls should have observed the in-flight dedup and
	// only one Fetch should have actually been issued.
	if len(ff.fetchCalls) != 1 {
		t.Errorf("concurrent Place calls produced %d Fetch invocations, want 1 (dedup failed)", len(ff.fetchCalls))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/emoji/ -run TestPlace_ColdPath -v`
Expected: FAIL — the placeholder body doesn't spawn fetches at all.

---

### Task 5.6: Implement the cold path

**Files:**
- Modify: `internal/emoji/place.go`

- [ ] **Step 1: Replace the cold-path stub with a real implementation**

In `internal/emoji/place.go`, replace the cold-path stub in `Place` with:

```go
	// Cold path: kick off an async fetch (deduplicated per-URL across
	// all concurrent Place calls), return a cells-wide space
	// reservation. Width math reports the same value (cells) for both
	// the warm and cold output, so the eventual swap doesn't shift
	// layout.
	spawnEmojiFetch(ctx, key, url, target)
	return strings.Repeat(" ", cells), nil, true
}

// spawnEmojiFetch starts (at most) one fetch goroutine per URL. On
// success, dispatches EmojiImageReadyMsg via ctx.SendMsg so the host
// can re-render. The fetch's CellTarget triggers the fetcher's
// prerender machinery (see Fetcher.maybePrerender) so subsequent
// Place calls hit the warm path without further work on the UI
// thread.
func spawnEmojiFetch(ctx PlaceContext, key, url string, target goimage.Point) {
	inflightEmojiMu.Lock()
	if _, busy := inflightEmoji[key]; busy {
		inflightEmojiMu.Unlock()
		return
	}
	inflightEmoji[key] = struct{}{}
	inflightEmojiMu.Unlock()

	send := ctx.SendMsg
	fetcher := ctx.Fetcher

	go func() {
		defer func() {
			inflightEmojiMu.Lock()
			delete(inflightEmoji, key)
			inflightEmojiMu.Unlock()
		}()

		_, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
			Key:        key,
			URL:        url,
			CellTarget: target, // triggers prerender into the kitty pipeline
			// Target left zero: Slack emoji PNGs are already ~64px;
			// the kitty prerender downscales to the cell-pixel target
			// internally during RenderKey.
		})
		if err != nil {
			// Fetcher logs the error in its own [imgfetch] surface;
			// no need to duplicate here. The Place caller will keep
			// rendering reservations on every frame until a successful
			// fetch lands a prerender entry.
			return
		}
		if send != nil {
			send(EmojiImageReadyMsg{URL: url})
		}
	}()
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/emoji/ -run TestPlace_ColdPath -v`
Expected: PASS for both subtests.

- [ ] **Step 3: Run the full Place test suite**

Run: `go test ./internal/emoji/ -run TestPlace -v`
Expected: PASS — `TestPlace_InvalidInputs`, `TestPlace_WarmPath`, `TestPlace_ColdPath_SpawnsFetch`, `TestPlace_ColdPath_DedupsConcurrentCalls`.

- [ ] **Step 4: Commit**

```bash
git add internal/emoji/place.go internal/emoji/place_test.go
git commit -m "feat(emoji): Place cold path spawns deduped fetch and signals re-render"
```

---

### Task 5.7: Failing test — cold path falls back to repeat-fetch attempts on transient error

**Files:**
- Modify: `internal/emoji/place_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/place_test.go`:

```go
func TestPlace_ColdPath_FetchError_LeavesNoInflight(t *testing.T) {
	ff := newFakeFetcher()
	url := "https://a.slack-edge.com/...1f44d.png"
	key := EmojiCacheKey(url)

	// First fetch fails. The inflight registration must clear so a
	// subsequent Place call re-tries (otherwise a transient error
	// would permanently freeze the placeholder).
	first := make(chan struct{})
	ff.fetchFn = func(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
		close(first)
		return imgpkg.FetchResult{}, errors.New("network down")
	}

	pctx := PlaceContext{Fetcher: ff, SendMsg: func(any) {}}

	Place(pctx, url, 2)
	<-first

	// Give the deferred inflight cleanup time to run.
	deadline := time.Now().Add(time.Second)
	for {
		inflightEmojiMu.Lock()
		_, busy := inflightEmoji[key]
		inflightEmojiMu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("inflight entry for %q never cleared after fetch error", key)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A second Place call should issue a new fetch.
	Place(pctx, url, 2)
	deadline = time.Now().Add(time.Second)
	for {
		ff.mu.Lock()
		n := len(ff.fetchCalls)
		ff.mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second Place did not issue a retry fetch (n=%d)", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestPlace_ColdPath_FetchError -v`
Expected: PASS — the `defer` in `spawnEmojiFetch` already clears the inflight entry on error.

- [ ] **Step 3: Commit**

```bash
git add internal/emoji/place_test.go
git commit -m "test(emoji): cover Place cold-path retry behavior after fetch error"
```

---

### Task 5.8: Final phase check

- [ ] **Step 1: Build the full project**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: no new failures.

- [ ] **Step 3: Verify package-level integration**

Run: `go test ./internal/emoji/ -v -count=1`
Expected: all PASS, including all earlier-phase tests (`TestResolveShortcodesInText`, `TestBuild*URL`, `TestResolveEmojiToTokens_*`, `TestImageMode_*`, `TestWidth*`, `TestWillProbe*`, `TestPlace_*`).

`-count=1` defeats Go's test result caching so process-globals (`imageMode`, `inflightEmoji`) start clean every run.

Phase 5 complete. The library layer is finished. Every UI surface in Phases 6-9 calls `emoji.Place(...)` with a `PlaceContext` it builds from the app's `Fetcher` and `Program.Send`, and gets back either a kitty placement line or a 2-cell reservation. The fetcher's prerender machinery (already in place) populates the warm path; `EmojiImageReadyMsg` signals re-renders.

The feature is still dark on `main` — no code outside `internal/emoji` calls these helpers yet. Continue to `06-messages-and-reactions.md`.
