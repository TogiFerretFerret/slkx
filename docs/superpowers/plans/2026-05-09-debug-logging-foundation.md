# Debug Logging — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `internal/debuglog` package, wire it into `cmd/slk/main.go`, migrate the in-scope existing `log.Printf` calls to category functions. After this plan, the rest of the codebase can call `debuglog.Cache(...)` / `debuglog.ImgFetch(...)` / `debuglog.ImgRender(...)` / `debuglog.WS(...)` / `debuglog.General(...)` and `debuglog.NextReqID()`.

**Architecture:** Single new package `internal/debuglog` with one initializer (`Init`), an `atomic.Bool` enabled flag, five category functions, and a monotonic `NextReqID()`. When `SLK_DEBUG=1`, `Init` truncates `slk-debug.log` in cwd, opens it for writing, configures the package logger, AND routes the global stdlib `log` package output to the same file (so untouched `log.Printf` calls land in the same file). When unset, every category function is a fast no-op via `enabled.Load()`.

**Tech Stack:** Go 1.26.1 (`t.Chdir`, `t.Setenv` available), stdlib `log` + `sync/atomic`.

**Spec:** `docs/superpowers/specs/2026-05-09-comprehensive-debug-logging-design.md`

**Follow-up plans (independent of each other, both depend on this one):**
- `docs/superpowers/plans/2026-05-09-debug-logging-cache.md`
- `docs/superpowers/plans/2026-05-09-debug-logging-images.md`

---

## File Structure

| File | Role |
|---|---|
| `internal/debuglog/debuglog.go` (NEW) | `Init`, `Enabled`, `Cache`/`ImgFetch`/`ImgRender`/`WS`/`General`, `NextReqID` |
| `internal/debuglog/debuglog_test.go` (NEW) | Unit tests for the package |
| `cmd/slk/main.go` | Replace `SLK_DEBUG` block at line 144-158 with `debuglog.Init` call; remove `debugWSEvents` var; migrate in-scope `log.Printf` calls |
| `internal/image/fetcher.go` | Migrate `log.Printf` calls at lines 345, 352, 403 to `debuglog.ImgFetch`; remove `"log"` import if unused |
| `internal/ui/imgrender/imgrender.go` | Migrate `log.Printf` at line 341 to `debuglog.ImgFetch`; remove `"log"` import if unused |
| `internal/ui/messages/blockkit/image.go` | Migrate `log.Printf` at line 160 to `debuglog.ImgFetch`; remove `"log"` import if unused |
| `internal/slack/events.go` | Remove `debugWS` var + `os.Getenv("SLK_DEBUG_WS")`; replace at line 383 with `debuglog.WS` (always called — gating happens inside the package) |

---

## Important conventions for the engineer

- **Run from the repo root** `/home/grant/local_code/slk/` (or the worktree the orchestrator created).
- **Test runner:** `go test ./<pkg>/... -run <Name> -v` for targeted, `go test ./...` for full.
- **Build check:** `go build ./...` after every code change before running tests.
- **Line numbers drift.** When a step says "around line N", confirm with `grep -n` first; the surrounding code shown in the step is authoritative.
- **TDD:** every code-producing task starts with a failing test.
- **Commit after each task** (small, focused commits, no batching).
- **Imports:** when removing the last `log.Printf` from a file, also remove the `"log"` import. Run `go build ./...` after the migration to catch any miss.

---

## Task 1: Create the `debuglog` package — basic structure and `Enabled()` semantics

**Files:**
- Create: `internal/debuglog/debuglog.go`
- Create: `internal/debuglog/debuglog_test.go`

This task lays down the package skeleton with the disabled-mode fast path. `Init` is intentionally simple in this task — file handling comes in Task 2.

- [ ] **Step 1: Write the failing test for `Enabled()` defaulting to false**

Create `internal/debuglog/debuglog_test.go`:

```go
package debuglog

import "testing"

func TestEnabled_DefaultFalse(t *testing.T) {
	if Enabled() {
		t.Fatalf("Enabled() should be false before Init")
	}
}
```

- [ ] **Step 2: Run test — expect FAIL with undefined symbol**

```bash
go test ./internal/debuglog/ -run TestEnabled_DefaultFalse -v
```

Expected: FAIL with `undefined: Enabled` (package doesn't exist yet).

- [ ] **Step 3: Create the minimal package**

Create `internal/debuglog/debuglog.go`:

```go
// Package debuglog provides categorized debug logging for slk.
//
// When SLK_DEBUG is set in the environment, Init opens slk-debug.log
// in the current working directory (truncating any existing file) and
// configures a package-internal logger. When unset, every category
// function is a fast no-op via an atomic.Bool flag — Sprintf-style
// args still get evaluated by Go's calling convention, but no
// formatting work occurs inside the package.
//
// Categories:
//   - Cache     — messages cache + reconciliation
//   - ImgFetch  — image fetcher lifecycle
//   - ImgRender — image render sizing + protocol decisions
//   - WS        — websocket events
//   - General   — misc / catch-all
//
// All output goes to a single file. Categories are encoded as inline
// tag prefixes (e.g. "[cache] ...") so users can grep to slice the
// log.
package debuglog

import (
	"sync/atomic"
)

var enabled atomic.Bool

// Enabled reports whether logging is active. Cheap (atomic.Bool load).
func Enabled() bool {
	return enabled.Load()
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
go test ./internal/debuglog/ -run TestEnabled_DefaultFalse -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/debuglog/debuglog.go internal/debuglog/debuglog_test.go
git commit -m "debuglog: skeleton package with Enabled()"
```

---

## Task 2: Implement `Init` — truncate-on-start file behavior

**Files:**
- Modify: `internal/debuglog/debuglog.go`
- Modify: `internal/debuglog/debuglog_test.go`

`Init` is the env-gated initializer. When `SLK_DEBUG` is set, it opens `slk-debug.log` in cwd with `O_TRUNC|O_CREATE|O_WRONLY` (truncating any existing file), sets the `enabled` flag, and routes the global stdlib `log` package to the same file. When unset, it sets `log.SetOutput(io.Discard)` to preserve current "silenced when not debugging" behavior, and returns `nil, nil`.

- [ ] **Step 1: Write the failing test — `Init` truncates existing file**

Append to `internal/debuglog/debuglog_test.go`:

```go
import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit_TruncatesExisting(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("SLK_DEBUG", "1")

	// Pre-populate slk-debug.log with content.
	preexisting := filepath.Join(dir, "slk-debug.log")
	if err := os.WriteFile(preexisting, []byte("old content from a previous session"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Reset the package-level flag so this test is order-independent.
	enabled.Store(false)

	f, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if f == nil {
		t.Fatalf("Init returned nil file when SLK_DEBUG was set")
	}
	defer f.Close()

	info, err := os.Stat(preexisting)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("slk-debug.log should be truncated, got size %d", info.Size())
	}
	if !Enabled() {
		t.Fatalf("Enabled() should be true after Init with SLK_DEBUG set")
	}
}
```

The test file's `import` block should now have a single `import (...)` group with `os`, `path/filepath`, and `testing`. Merge with the existing `import "testing"` at the top — replace the single-line import with the grouped form.

- [ ] **Step 2: Write the failing test — `Init` is no-op when SLK_DEBUG unset**

Append to `internal/debuglog/debuglog_test.go`:

```go
func TestInit_NoFileWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Explicitly unset SLK_DEBUG (defensive — env may bleed in from CI).
	t.Setenv("SLK_DEBUG", "")
	enabled.Store(false)

	f, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if f != nil {
		t.Fatalf("Init should return nil file when SLK_DEBUG is unset, got %v", f.Name())
		_ = f.Close()
	}
	if Enabled() {
		t.Fatalf("Enabled() should be false after Init with SLK_DEBUG unset")
	}

	if _, err := os.Stat(filepath.Join(dir, "slk-debug.log")); !os.IsNotExist(err) {
		t.Fatalf("slk-debug.log should not exist when disabled, got err=%v", err)
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL with undefined `Init`**

```bash
go test ./internal/debuglog/ -v
```

Expected: FAIL with `undefined: Init`.

- [ ] **Step 4: Implement `Init`**

In `internal/debuglog/debuglog.go`, add the imports and the `Init` function. The full file should now look like this:

```go
// Package debuglog provides categorized debug logging for slk.
//
// When SLK_DEBUG is set in the environment, Init opens slk-debug.log
// in the current working directory (truncating any existing file) and
// configures a package-internal logger. When unset, every category
// function is a fast no-op via an atomic.Bool flag — Sprintf-style
// args still get evaluated by Go's calling convention, but no
// formatting work occurs inside the package.
//
// Categories:
//   - Cache     — messages cache + reconciliation
//   - ImgFetch  — image fetcher lifecycle
//   - ImgRender — image render sizing + protocol decisions
//   - WS        — websocket events
//   - General   — misc / catch-all
//
// All output goes to a single file. Categories are encoded as inline
// tag prefixes (e.g. "[cache] ...") so users can grep to slice the
// log.
package debuglog

import (
	"io"
	"log"
	"os"
	"sync/atomic"
)

var (
	enabled atomic.Bool
	logger  *log.Logger
)

// Init opens slk-debug.log in cwd (truncating) when SLK_DEBUG is set,
// configures the package-internal logger, and routes the global stdlib
// log package to the same file. When SLK_DEBUG is unset, Init sets the
// global stdlib log to io.Discard (so spurious log.Printf calls don't
// bleed into the user's altscreen TUI) and returns nil, nil.
//
// Returns the *os.File so the caller can close it on exit. Idempotent
// modulo the underlying file handle: calling Init twice with SLK_DEBUG
// set will truncate the file twice and return the second handle (the
// first handle is leaked — the caller is expected to call Init exactly
// once at startup).
func Init() (*os.File, error) {
	if os.Getenv("SLK_DEBUG") == "" {
		log.SetOutput(io.Discard)
		enabled.Store(false)
		return nil, nil
	}
	f, err := os.OpenFile("slk-debug.log",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		// Failed to open — keep enabled=false so calls remain no-op.
		log.SetOutput(io.Discard)
		enabled.Store(false)
		return nil, err
	}
	// Route both the package logger and the global stdlib log to the
	// same file. Log flags set ISO-ish date+time with microsecond
	// precision so timelines sort lexically.
	flags := log.Ldate | log.Ltime | log.Lmicroseconds
	logger = log.New(f, "", flags)
	log.SetOutput(f)
	log.SetFlags(flags)
	enabled.Store(true)
	return f, nil
}

// Enabled reports whether logging is active. Cheap (atomic.Bool load).
func Enabled() bool {
	return enabled.Load()
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
go test ./internal/debuglog/ -v
```

Expected: PASS for `TestEnabled_DefaultFalse`, `TestInit_TruncatesExisting`, `TestInit_NoFileWhenDisabled`.

- [ ] **Step 6: Commit**

```bash
git add internal/debuglog/debuglog.go internal/debuglog/debuglog_test.go
git commit -m "debuglog: Init opens cwd/slk-debug.log truncating, routes stdlib log"
```

---

## Task 3: Implement category functions

**Files:**
- Modify: `internal/debuglog/debuglog.go`
- Modify: `internal/debuglog/debuglog_test.go`

Each category function is a thin wrapper that no-ops when disabled and otherwise prepends the category tag to the format string before calling `logger.Printf`.

- [ ] **Step 1: Write the failing test — category prefixes**

Append to `internal/debuglog/debuglog_test.go`:

```go
import (
	"strings"
)

// (Add to existing import block; do not duplicate.)

func TestCategoryPrefixes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("SLK_DEBUG", "1")
	enabled.Store(false)

	f, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer f.Close()

	Cache("cache-line %d", 1)
	ImgFetch("imgfetch-line %d", 2)
	ImgRender("imgrender-line %d", 3)
	WS("ws-line %d", 4)
	General("general-line %d", 5)

	body, err := os.ReadFile(filepath.Join(dir, "slk-debug.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	out := string(body)
	for _, want := range []string{
		"[cache] cache-line 1",
		"[imgfetch] imgfetch-line 2",
		"[imgrender] imgrender-line 3",
		"[ws] ws-line 4",
		"[general] general-line 5",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q\nfull output:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Write the failing test — fast-path no-op when disabled**

Append:

```go
func TestEnabled_FastPathNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("SLK_DEBUG", "")
	enabled.Store(false)

	if _, err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Should not panic and should not create a file.
	Cache("nope %d", 1)
	ImgFetch("nope %d", 2)
	ImgRender("nope %d", 3)
	WS("nope %d", 4)
	General("nope %d", 5)

	if _, err := os.Stat(filepath.Join(dir, "slk-debug.log")); !os.IsNotExist(err) {
		t.Fatalf("slk-debug.log should not exist; err=%v", err)
	}
}
```

- [ ] **Step 3: Run tests — expect FAIL with undefined symbols**

```bash
go test ./internal/debuglog/ -v
```

Expected: FAIL with `undefined: Cache`, etc.

- [ ] **Step 4: Implement category functions**

Append to `internal/debuglog/debuglog.go` (after `Enabled()`):

```go
// Cache logs a message tagged [cache] for messages-cache and
// reconciliation events. No-op when !Enabled().
func Cache(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	logger.Printf("[cache] "+format, args...)
}

// ImgFetch logs a message tagged [imgfetch] for image fetcher
// lifecycle events. No-op when !Enabled().
func ImgFetch(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	logger.Printf("[imgfetch] "+format, args...)
}

// ImgRender logs a message tagged [imgrender] for image render-sizing
// and protocol decisions. No-op when !Enabled().
func ImgRender(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	logger.Printf("[imgrender] "+format, args...)
}

// WS logs a message tagged [ws] for WebSocket events. No-op when
// !Enabled().
func WS(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	logger.Printf("[ws] "+format, args...)
}

// General logs a message tagged [general] for miscellaneous events.
// No-op when !Enabled().
func General(format string, args ...any) {
	if !enabled.Load() {
		return
	}
	logger.Printf("[general] "+format, args...)
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
go test ./internal/debuglog/ -v
```

Expected: all 5 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/debuglog/debuglog.go internal/debuglog/debuglog_test.go
git commit -m "debuglog: Cache/ImgFetch/ImgRender/WS/General category functions"
```

---

## Task 4: Implement `NextReqID`

**Files:**
- Modify: `internal/debuglog/debuglog.go`
- Modify: `internal/debuglog/debuglog_test.go`

`NextReqID` returns a process-wide monotonic uint64. It works whether or not `Enabled()` is true — call sites generate an ID and stash it in a struct, the log lines just become no-ops if disabled.

- [ ] **Step 1: Write the failing test — monotonic and unique under concurrency**

Append to `internal/debuglog/debuglog_test.go`:

```go
import (
	"sync"
)
// (Add to the existing import group.)

func TestNextReqID_MonotonicUnique(t *testing.T) {
	const G = 16
	const M = 100
	var wg sync.WaitGroup
	collected := make([][]uint64, G)
	for g := 0; g < G; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids := make([]uint64, M)
			for i := 0; i < M; i++ {
				ids[i] = NextReqID()
			}
			collected[g] = ids
		}()
	}
	wg.Wait()

	seen := map[uint64]struct{}{}
	for _, ids := range collected {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate id %d", id)
			}
			seen[id] = struct{}{}
		}
	}
	if len(seen) != G*M {
		t.Fatalf("want %d unique ids, got %d", G*M, len(seen))
	}
}
```

- [ ] **Step 2: Run test — expect FAIL with undefined symbol**

```bash
go test ./internal/debuglog/ -run TestNextReqID -v
```

Expected: FAIL with `undefined: NextReqID`.

- [ ] **Step 3: Implement `NextReqID`**

Add to `internal/debuglog/debuglog.go` (in the existing var block and as a new function):

Replace:

```go
var (
	enabled atomic.Bool
	logger  *log.Logger
)
```

with:

```go
var (
	enabled atomic.Bool
	logger  *log.Logger
	reqID   atomic.Uint64
)
```

Append at the end of the file:

```go
// NextReqID returns a process-wide monotonic uint64. Used to correlate
// image-fetch lifecycle log lines across the enqueue → http →
// dispatch → recv stages. Safe to call regardless of Enabled().
func NextReqID() uint64 {
	return reqID.Add(1)
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
go test ./internal/debuglog/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/debuglog/debuglog.go internal/debuglog/debuglog_test.go
git commit -m "debuglog: NextReqID for fetch lifecycle correlation"
```

---

## Task 5: Add concurrent-write torn-line test

**Files:**
- Modify: `internal/debuglog/debuglog_test.go`

This test sanity-checks that stdlib `log.Logger`'s mutex prevents interleaved bytes when many goroutines write at once. Important because we'll be writing from the bubbletea Update goroutine, fetch goroutines, and WS event goroutine simultaneously.

- [ ] **Step 1: Write the test**

Append to `internal/debuglog/debuglog_test.go`:

```go
import (
	"bufio"
)
// (Add to the existing import group.)

func TestConcurrentWrites_NoTornLines(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("SLK_DEBUG", "1")
	enabled.Store(false)

	f, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer f.Close()

	const G = 8
	const M = 200
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < M; i++ {
				Cache("g=%d i=%d", g, i)
			}
		}()
	}
	wg.Wait()

	body, err := os.ReadFile(filepath.Join(dir, "slk-debug.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	count := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "[cache] g=") {
			t.Errorf("malformed line: %q", line)
		}
		count++
	}
	if count != G*M {
		t.Errorf("want %d lines, got %d", G*M, count)
	}
}
```

- [ ] **Step 2: Run test — expect PASS**

```bash
go test ./internal/debuglog/ -run TestConcurrentWrites -v
```

Expected: PASS (stdlib `log.Logger` already provides the mutex).

- [ ] **Step 3: Run full debuglog tests with race detector**

```bash
go test -race ./internal/debuglog/ -v
```

Expected: PASS, no race warnings.

- [ ] **Step 4: Commit**

```bash
git add internal/debuglog/debuglog_test.go
git commit -m "debuglog: concurrent-write torn-line test"
```

---

## Task 6: Wire `debuglog.Init` into `cmd/slk/main.go`

**Files:**
- Modify: `cmd/slk/main.go` (lines 144-158, the existing SLK_DEBUG block)

Replace the existing `SLK_DEBUG` block with a call to `debuglog.Init`. The existing block writes to `/tmp/slk-debug.log` in append mode; the new behavior writes to `slk-debug.log` in cwd, truncating.

- [ ] **Step 1: Verify the existing block**

Open `cmd/slk/main.go` and confirm lines around 144-158 match:

```go
func main() {
	// Debug log to file when SLK_DEBUG is set; otherwise discard so
	// log lines don't bleed into the user's terminal under altscreen
	// (some terminals show stderr writes overlaid on the rendered UI;
	// even if they don't, stderr can show up after slk exits and
	// pollute the parent shell).
	if os.Getenv("SLK_DEBUG") != "" {
		f, err := os.OpenFile("/tmp/slk-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			log.SetOutput(f)
			log.Printf("=== slk debug session started ===")
		}
	} else {
		log.SetOutput(io.Discard)
	}
```

If line numbers differ, locate the exact block by `grep -n 'slk-debug.log' cmd/slk/main.go`.

- [ ] **Step 2: Replace the block with `debuglog.Init`**

Edit `cmd/slk/main.go`. Find the block above and replace with:

```go
func main() {
	// Debug log: when SLK_DEBUG is set, debuglog.Init opens
	// slk-debug.log in cwd (truncating any prior session) and routes
	// both the package-internal logger and the global stdlib log to
	// it. When unset, stdlib log is routed to io.Discard so spurious
	// log.Printf calls don't bleed into the user's altscreen TUI.
	if debugFile, err := debuglog.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "slk: could not open debug log: %v\n", err)
	} else if debugFile != nil {
		defer debugFile.Close()
		debuglog.General("=== slk debug session started ===")
	}
```

- [ ] **Step 3: Add the `debuglog` import**

Add `"github.com/gammons/slk/internal/debuglog"` to the import block in `cmd/slk/main.go`. Look for the existing block of imports near the top of the file and add it in alphabetical order among the `github.com/gammons/slk/...` entries.

- [ ] **Step 4: Build to catch issues**

```bash
go build ./...
```

Expected: clean build. If `io` is no longer used, the compiler will tell you to remove the `"io"` import. The existing code does still use `io.Discard` elsewhere — keep it if so.

Run `grep -n '"io"' cmd/slk/main.go` and `grep -n 'io\.' cmd/slk/main.go` to confirm.

- [ ] **Step 5: Manually verify behavior — debug enabled**

```bash
go build -o /tmp/slk-test ./cmd/slk
cd /tmp
SLK_DEBUG=1 timeout 0.5 /tmp/slk-test --version
ls -la /tmp/slk-debug.log
cat /tmp/slk-debug.log
```

Expected: `/tmp/slk-debug.log` exists (slk's `--version` exits before any other logging, but `Init` should have created the file). The file may be empty or contain only the "session started" line — both are fine.

- [ ] **Step 6: Manually verify behavior — debug disabled**

```bash
rm -f /tmp/slk-debug.log
cd /tmp
unset SLK_DEBUG
timeout 0.5 /tmp/slk-test --version
ls /tmp/slk-debug.log 2>&1 || echo "OK: no file created"
```

Expected: `OK: no file created`.

- [ ] **Step 7: Commit**

```bash
git add cmd/slk/main.go
git commit -m "main: wire debuglog.Init replacing inline SLK_DEBUG block"
```

---

## Task 7: Migrate in-scope `log.Printf` calls in `cmd/slk/main.go`

**Files:**
- Modify: `cmd/slk/main.go`

Migrate the cache-related and image-protocol-related `log.Printf` calls in our three focus areas to `debuglog.Cache` or `debuglog.ImgRender`. Out-of-scope `log.Printf` calls (workspace bootstrap, clipboard, etc.) stay on stdlib `log` and still flow to the same file via `log.SetOutput`.

In-scope migrations (find each by `grep -n` first):

| Old (`log.Printf` form) | New |
|---|---|
| `log.Printf("loadCachedMessages: GetMessages %s: %v", channelID, err)` | `debuglog.Cache("loadCachedMessages: GetMessages %s: %v", channelID, err)` |
| `log.Printf("%s: GetReactions %s/%s: %v", logPrefix, channelID, m.TS, err)` | `debuglog.Cache("%s: GetReactions %s/%s: %v", logPrefix, channelID, m.TS, err)` |
| `log.Printf("%s: raw_json unmarshal for %s/%s: %v", logPrefix, channelID, m.TS, err)` | `debuglog.Cache("%s: raw_json unmarshal for %s/%s: %v", logPrefix, channelID, m.TS, err)` |
| `log.Printf("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)` | `debuglog.Cache("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)` |
| `log.Printf("fetchChannelMessages: GetHistory %s: %v", channelID, err)` | `debuglog.Cache("fetchChannelMessages: GetHistory %s: %v", channelID, err)` |
| `log.Printf("fetchThreadReplies: GetReplies %s/%s: %v", channelID, threadTS, err)` | `debuglog.Cache("fetchThreadReplies: GetReplies %s/%s: %v", channelID, threadTS, err)` |
| `log.Printf("image protocol: %s", proto)` | `debuglog.ImgRender("image protocol: %s", proto)` |
| `log.Printf("cell pixels: %dx%d", pxW, pxH)` | `debuglog.ImgRender("cell pixels: %dx%d", pxW, pxH)` |

The protocol-detection-area `log.Printf` lines for `kitty probe skipped`, `term restore after kitty probe`, and `kitty probe failed, downgrading to halfblock` are also in-scope. They become `debuglog.ImgRender(...)`.

Out-of-scope (LEAVE AS-IS): `avatar migration`, `migrated %d avatars`, and any other `log.Printf` calls not in the above list.

- [ ] **Step 1: Find each in-scope call site**

```bash
grep -n 'log.Printf' cmd/slk/main.go
```

Identify each in-scope line by matching the format string against the table above.

- [ ] **Step 2: Migrate `loadCachedMessages` GetMessages error**

In `cmd/slk/main.go`, find:

```go
	rows, err := db.GetMessages(channelID, 50, "")
	if err != nil {
		log.Printf("loadCachedMessages: GetMessages %s: %v", channelID, err)
		return nil
	}
```

Change the `log.Printf` line to:

```go
		debuglog.Cache("loadCachedMessages: GetMessages %s: %v", channelID, err)
```

- [ ] **Step 3: Migrate `enrichCachedRow` GetReactions error**

Find:

```go
	} else {
		log.Printf("%s: GetReactions %s/%s: %v", logPrefix, channelID, m.TS, err)
	}
```

Change to:

```go
	} else {
		debuglog.Cache("%s: GetReactions %s/%s: %v", logPrefix, channelID, m.TS, err)
	}
```

- [ ] **Step 4: Migrate `enrichCachedRow` raw_json unmarshal error**

Find:

```go
		if err := json.Unmarshal([]byte(m.RawJSON), &raw); err != nil {
			log.Printf("%s: raw_json unmarshal for %s/%s: %v",
				logPrefix, channelID, m.TS, err)
		} else {
```

Change the `log.Printf` block to:

```go
		if err := json.Unmarshal([]byte(m.RawJSON), &raw); err != nil {
			debuglog.Cache("%s: raw_json unmarshal for %s/%s: %v",
				logPrefix, channelID, m.TS, err)
		} else {
```

- [ ] **Step 5: Migrate `loadCachedThreadReplies` GetThreadReplies error**

Find:

```go
	rows, err := db.GetThreadReplies(channelID, threadTS)
	if err != nil {
		log.Printf("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}
```

Change the `log.Printf` line to:

```go
		debuglog.Cache("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)
```

- [ ] **Step 6: Migrate `fetchChannelMessages` GetHistory error**

Find:

```go
	history, err := client.GetHistory(ctx, channelID, 50, "")
	if err != nil {
		log.Printf("fetchChannelMessages: GetHistory %s: %v", channelID, err)
		return nil
	}
```

Change to:

```go
		debuglog.Cache("fetchChannelMessages: GetHistory %s: %v", channelID, err)
```

- [ ] **Step 7: Migrate `fetchThreadReplies` GetReplies error**

Find:

```go
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		log.Printf("fetchThreadReplies: GetReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}
```

Change to:

```go
		debuglog.Cache("fetchThreadReplies: GetReplies %s/%s: %v", channelID, threadTS, err)
```

- [ ] **Step 8: Migrate image-protocol log lines**

Find these four lines in the protocol-detection area (around line 410-434):

```go
		if err != nil {
			log.Printf("kitty probe skipped: cannot enter raw mode: %v", err)
		} else {
			ok := imgpkg.ProbeKittyGraphics(os.Stdout, os.Stdin, 200*time.Millisecond)
			if rerr := term.Restore(int(os.Stdin.Fd()), state); rerr != nil {
				log.Printf("term restore after kitty probe: %v", rerr)
			}
			if !ok {
				log.Println("kitty probe failed, downgrading to halfblock")
				proto = imgpkg.ProtoHalfBlock
			}
		}
	}
	log.Printf("image protocol: %s", proto)
```

…and the `cell pixels` line a few lines below:

```go
	pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
	log.Printf("cell pixels: %dx%d", pxW, pxH)
```

Replace each `log.Printf`/`log.Println` with `debuglog.ImgRender`. The block becomes:

```go
		if err != nil {
			debuglog.ImgRender("kitty probe skipped: cannot enter raw mode: %v", err)
		} else {
			ok := imgpkg.ProbeKittyGraphics(os.Stdout, os.Stdin, 200*time.Millisecond)
			if rerr := term.Restore(int(os.Stdin.Fd()), state); rerr != nil {
				debuglog.ImgRender("term restore after kitty probe: %v", rerr)
			}
			if !ok {
				debuglog.ImgRender("kitty probe failed, downgrading to halfblock")
				proto = imgpkg.ProtoHalfBlock
			}
		}
	}
	debuglog.ImgRender("image protocol: %s", proto)
```

…and:

```go
	pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
	debuglog.ImgRender("cell pixels: %dx%d", pxW, pxH)
```

- [ ] **Step 9: Build to catch any errors**

```bash
go build ./...
```

Expected: clean build. The `"log"` import in `cmd/slk/main.go` is still used by other call sites — do NOT remove it.

- [ ] **Step 10: Run the full test suite**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add cmd/slk/main.go
git commit -m "main: migrate cache+image-protocol log.Printf calls to debuglog"
```

---

## Task 8: Migrate `log.Printf` calls in `internal/image/fetcher.go`

**Files:**
- Modify: `internal/image/fetcher.go`

Three `log.Printf` calls in `fetcher.go`:
1. Line ~345: `"file auth: learned team %q is reachable via team %q's auth"`
2. Line ~352: `"file auth: attempt with team %q failed for %s (status=%d ct=%q); trying next"`
3. Line ~403: `"file auth: HTTP 429 for %s (attempt %d/%d); backing off %s"`

All three move to `debuglog.ImgFetch`.

- [ ] **Step 1: Find the call sites**

```bash
grep -n 'log.Printf' internal/image/fetcher.go
```

Confirm exactly three matches.

- [ ] **Step 2: Migrate the three calls**

In `internal/image/fetcher.go`, find:

```go
				if _, known := f.authsByTeam[teamID]; !known && auth.TeamID != "" {
					f.learnedAuths.Store(teamID, auth)
					log.Printf("file auth: learned team %q is reachable via team %q's auth", teamID, auth.TeamID)
				}
```

Change the `log.Printf` line to:

```go
					debuglog.ImgFetch("file auth: learned team %q is reachable via team %q's auth", teamID, auth.TeamID)
```

Find:

```go
		// Auth failure (HTML response or 401/403) — try next auth.
		lastErr = fmt.Errorf("fetch %s: HTTP %d ct=%q (auth failure?)", url, status, ct)
		log.Printf("file auth: attempt with team %q failed for %s (status=%d ct=%q); trying next",
			auth.TeamID, url, status, ct)
```

Change the `log.Printf` block to:

```go
		debuglog.ImgFetch("file auth: attempt with team %q failed for %s (status=%d ct=%q); trying next",
			auth.TeamID, url, status, ct)
```

Find:

```go
		log.Printf("file auth: HTTP 429 for %s (attempt %d/%d); backing off %s",
			url, attempt+1, rateLimitMaxRetries+1, backoff)
```

Change to:

```go
		debuglog.ImgFetch("file auth: HTTP 429 for %s (attempt %d/%d); backing off %s",
			url, attempt+1, rateLimitMaxRetries+1, backoff)
```

- [ ] **Step 3: Update the import block**

`fetcher.go` currently imports `"log"`. After this migration, `log` is no longer referenced. Remove `"log"` from the import block and add `"github.com/gammons/slk/internal/debuglog"` in its place (sorted alphabetically among the project imports — i.e. before `"golang.org/x/image/draw"`).

The import block should change from:

```go
import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"
)
```

to:

```go
import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gammons/slk/internal/debuglog"
	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"
)
```

- [ ] **Step 4: Build**

```bash
go build ./...
```

Expected: clean build. If the build fails with `log: declared and not used`, double-check that all three `log.Printf` calls are migrated.

- [ ] **Step 5: Run image-package tests**

```bash
go test ./internal/image/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/image/fetcher.go
git commit -m "image/fetcher: migrate log.Printf to debuglog.ImgFetch"
```

---

## Task 9: Migrate `log.Printf` in `internal/ui/imgrender/imgrender.go`

**Files:**
- Modify: `internal/ui/imgrender/imgrender.go`

One `log.Printf` call to migrate (around line 341):

- [ ] **Step 1: Find the call site**

```bash
grep -n 'log.Printf' internal/ui/imgrender/imgrender.go
```

Confirm exactly one match.

- [ ] **Step 2: Migrate the call**

Find:

```go
			if err != nil {
				log.Printf("image fetch failed: key=%s url=%s err=%v", key, url, err)
				ctx.SendMsg(ImageFailedMsg{Key: key})
				return
			}
```

Change to:

```go
			if err != nil {
				debuglog.ImgFetch("image fetch failed: key=%s url=%s err=%v", key, url, err)
				ctx.SendMsg(ImageFailedMsg{Key: key})
				return
			}
```

- [ ] **Step 3: Update imports**

The current import block has `"log"`. Remove it and add `"github.com/gammons/slk/internal/debuglog"`.

Change:

```go
import (
	"bytes"
	"context"
	"image"
	"io"
	"log"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)
```

to:

```go
import (
	"bytes"
	"context"
	"image"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)
```

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./internal/ui/imgrender/... -v
```

Expected: clean build, tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/imgrender/imgrender.go
git commit -m "imgrender: migrate log.Printf to debuglog.ImgFetch"
```

---

## Task 10: Migrate `log.Printf` in `internal/ui/messages/blockkit/image.go`

**Files:**
- Modify: `internal/ui/messages/blockkit/image.go`

- [ ] **Step 1: Find the call site**

```bash
grep -n 'log.Printf' internal/ui/messages/blockkit/image.go
```

Confirm exactly one match.

- [ ] **Step 2: Migrate the call**

Find:

```go
				if err != nil {
					log.Printf("blockkit image fetch failed: key=%s url=%s err=%v", key, url, err)
					return
				}
```

Change to:

```go
				if err != nil {
					debuglog.ImgFetch("blockkit image fetch failed: key=%s url=%s err=%v", key, url, err)
					return
				}
```

- [ ] **Step 3: Update imports**

Remove `"log"`, add `"github.com/gammons/slk/internal/debuglog"`. Change:

```go
import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"image"
	"io"
	"log"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)
```

to:

```go
import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"image"
	"io"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)
```

- [ ] **Step 4: Build and test**

```bash
go build ./... && go test ./internal/ui/messages/... -v
```

Expected: clean build, tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/blockkit/image.go
git commit -m "blockkit/image: migrate log.Printf to debuglog.ImgFetch"
```

---

## Task 11: Remove `SLK_DEBUG_WS` and migrate `internal/slack/events.go`

**Files:**
- Modify: `internal/slack/events.go`
- Modify: `cmd/slk/main.go`

The `SLK_DEBUG_WS` env var is now redundant. The unknown-WS-event dump moves to `debuglog.WS`, which is already gated by the unified `SLK_DEBUG=1` switch.

There are TWO places where `SLK_DEBUG_WS` is read:
1. `internal/slack/events.go` — the `debugWS` package var at line 18 and its use at line 378-384.
2. `cmd/slk/main.go` — the `debugWSEvents` package var (around line 2462) and its use (around line 2439).

Both removals are part of this task.

- [ ] **Step 1: Migrate `internal/slack/events.go`**

Open `internal/slack/events.go`. Remove the `debugWS` var (around line 18):

Delete:

```go
// debugWS is true when SLK_DEBUG_WS is set. When enabled, dispatchWebSocketEvent
// logs every unknown WebSocket event type (with a truncated payload) to the
// standard logger — useful for reverse-engineering undocumented Slack events
// such as sidebar-section updates. Pair with SLK_DEBUG=1 to route logs to
// /tmp/slk-debug.log.
var debugWS = os.Getenv("SLK_DEBUG_WS") != ""
```

Change the unknown-event dump (around line 374-385) from:

```go
	default:
		// Ignore other event types. When SLK_DEBUG_WS is set, dump them
		// to the debug log so we can reverse-engineer undocumented
		// events (e.g. sidebar-section updates).
		if debugWS {
			payload := data
			if len(payload) > 4096 {
				payload = payload[:4096]
			}
			log.Printf("[ws] unknown event type=%q raw=%s", evt.Type, string(payload))
		}
	}
```

to:

```go
	default:
		// Ignore other event types. When debug logging is on, dump them
		// to the [ws] category so we can reverse-engineer undocumented
		// events (e.g. sidebar-section updates).
		if debuglog.Enabled() {
			payload := data
			if len(payload) > 4096 {
				payload = payload[:4096]
			}
			debuglog.WS("unknown event type=%q raw=%s", evt.Type, string(payload))
		}
	}
```

(The `debuglog.Enabled()` check avoids the up-to-4KB slice copy on the hot path when debug is off — even though `debuglog.WS` itself would no-op, the slicing happens before the call.)

- [ ] **Step 2: Update imports in `internal/slack/events.go`**

Remove `"log"` and `"os"` (if `os` is unused after removing the `debugWS` var). Add `"github.com/gammons/slk/internal/debuglog"`.

Run `grep -n '"os"\|os\.' internal/slack/events.go` to check whether `os` is still referenced (e.g. by other code). If `os` only appeared in the removed `debugWS` line, drop it. Otherwise keep it.

The expected new import block (assuming `os` is no longer referenced):

```go
import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/slack-go/slack"
)
```

- [ ] **Step 3: Migrate `cmd/slk/main.go` — remove `debugWSEvents` and migrate its use**

In `cmd/slk/main.go`, find the `debugWSEvents` var (around line 2462):

```go
// debugWSEvents flips on extra per-event logging when SLK_DEBUG_WS is
// set. Same env var the slack package uses for unknown-event dumps;
// reuse so users can flip both on with one variable.
var debugWSEvents = os.Getenv("SLK_DEBUG_WS") != ""
```

Delete this entire block (comment + var).

Find the use site (around line 2439). It looks like:

```go
	if debugWSEvents {
		log.Printf("pref_change received: name=%q value-len=%d", name, len(value))
	}
```

Change to:

```go
	debuglog.WS("pref_change received: name=%q value-len=%d", name, len(value))
```

(The `if debugWSEvents` gate is now redundant — `debuglog.WS` already gates internally.)

The very next `log.Printf` line is also pref-change-related and should also be migrated:

```go
	log.Printf("pref_change %s for %s: changed=%v muted=%v", name, h.wsCtx.TeamName, changed, h.wsCtx.MuteStore.MutedChannels())
```

Change to:

```go
	debuglog.WS("pref_change %s for %s: changed=%v muted=%v", name, h.wsCtx.TeamName, changed, h.wsCtx.MuteStore.MutedChannels())
```

- [ ] **Step 4: Build to catch any references to removed symbols**

```bash
go build ./...
```

Expected: clean build. If anything else references `debugWSEvents` or `debugWS`, the build will fail and tell you where.

- [ ] **Step 5: Run full test suite**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go internal/slack/events.go
git commit -m "ws: fold SLK_DEBUG_WS into unified SLK_DEBUG via debuglog.WS"
```

---

## Task 12: Verify the foundation end-to-end

**Files:** None (verification only)

A short manual smoke test to confirm the foundation is solid before moving on to the cache and images plans.

- [ ] **Step 1: Build a release-mode binary**

```bash
go build -o /tmp/slk-foundation ./cmd/slk
```

Expected: clean build.

- [ ] **Step 2: Run with `SLK_DEBUG` set in a fresh dir**

```bash
mkdir -p /tmp/slk-debug-smoketest
cd /tmp/slk-debug-smoketest
echo "preexisting" > slk-debug.log
SLK_DEBUG=1 timeout 0.5 /tmp/slk-foundation --version
ls -la slk-debug.log
cat slk-debug.log
```

Expected:
- `slk-debug.log` exists in `/tmp/slk-debug-smoketest`.
- Its previous contents (`preexisting`) are gone (truncate-on-start verified).
- It contains exactly one line ending with `[general] === slk debug session started ===` (note: `--version` exits before any other logging).

- [ ] **Step 3: Run without `SLK_DEBUG`**

```bash
rm -f /tmp/slk-debug-smoketest/slk-debug.log
cd /tmp/slk-debug-smoketest
unset SLK_DEBUG
timeout 0.5 /tmp/slk-foundation --version
ls /tmp/slk-debug-smoketest/slk-debug.log 2>&1 | grep -q "No such file" && echo "OK: no file" || echo "BUG: file exists"
```

Expected: `OK: no file`.

- [ ] **Step 4: Run race-detected tests for the package**

```bash
go test -race ./internal/debuglog/ -v
```

Expected: PASS, no race warnings.

- [ ] **Step 5: Final full test run**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit any housekeeping (none expected — this is verification)**

If the verification surfaced a regression, fix it as a new task. Otherwise:

```bash
git status   # should be clean
```

Foundation complete. Proceed to:
- `docs/superpowers/plans/2026-05-09-debug-logging-cache.md` (cache reconciliation logging)
- `docs/superpowers/plans/2026-05-09-debug-logging-images.md` (image render + fetch logging)

These two plans are independent of each other and can be implemented in either order, or in parallel worktrees.
