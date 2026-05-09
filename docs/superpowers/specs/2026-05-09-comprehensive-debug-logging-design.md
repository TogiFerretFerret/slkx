# Comprehensive SLK_DEBUG logging

## Problem

Three suspected bugs are hard to diagnose with the current debug
output:

1. **Messages cache reconciliation drift.** The cache for a channel
   sometimes appears out of date relative to the server. The
   reconciliation flow (cache-first paint at `internal/ui/app.go:1340`,
   authoritative replace at `internal/ui/app.go:1372`, network
   writeback in `cmd/slk/main.go:fetchChannelMessages` line 1687)
   has a nil-vs-empty contract that distinguishes "network failed"
   from "channel empty" — but there's no way to confirm at runtime
   which branch fired or which messages survived the merge.
2. **Images sometimes render smaller than their container.**
   Suspected to be ghostty-specific. The pipeline that decides image
   size (cell-pixel detection in `internal/image/cellmetrics.go`,
   target compute in `internal/ui/imgrender/imgrender.go:100`,
   thumbnail selection in `internal/image/fetcher.go:603`) has three
   independent decision points and we can't see which one produced
   the bad number.
3. **Image fetches stall.** A loading indicator appears and never
   resolves until the user navigates away and comes back. The fetch
   pipeline (`internal/image/fetcher.go` → singleflight → bounded
   semaphore → HTTP → decode → prerender → dispatch back to bubbletea
   via `imgrender.Renderer`) has many stages where a goroutine could
   silently fail to advance. The current logging is sparse.

The existing logging is stdlib `log` gated by `SLK_DEBUG`
(`cmd/slk/main.go:144-158`), writing to `/tmp/slk-debug.log`. There
are roughly 50 ad-hoc `log.Printf` call sites, no categories, no
correlation IDs, no consistent shape. Sibling `SLK_DEBUG_WS` toggles
WS event dumps separately.

## Goals

- A single environment variable, `SLK_DEBUG=1`, enables all debug
  logging for the session. `SLK_DEBUG_WS` is folded into this and
  becomes redundant.
- Log file written to **the current working directory** (not `/tmp`)
  as `slk-debug.log`, **truncated on each session start**.
- Comprehensive logging in three focus areas — messages cache
  reconciliation, image render sizing, and image fetch lifecycle —
  sufficient to diagnose the three bugs above by reading the file.
- Categories tagged in-line (`[cache]`, `[imgfetch]`, `[imgrender]`,
  `[ws]`, `[general]`) so users can `grep` to slice the log.
- Per-fetch correlation ID (`req_id=N`) threading through the image
  fetch pipeline so one image's full timeline can be grepped.
- Zero overhead when `SLK_DEBUG` is unset (atomic-bool fast-path
  no-op in every category function).
- No regression in existing test suites or behavior.

## Non-goals

- Structured logging (`log/slog`, JSON output). Skipped for
  simplicity and grep-friendliness; logs are for humans, not tools.
- Log rotation. Truncate-on-start keeps file size bounded per
  session; rotation is unnecessary.
- Migrating every existing `log.Printf` call site. Only the call
  sites in our three focus areas are migrated to category functions.
  Unrelated `log.Printf` calls (workspace bootstrap, clipboard,
  notifications) keep going to the same file via the global stdlib
  `log` output.
- Runtime category toggling (e.g. `SLK_DEBUG=cache,imgfetch`).
  All-or-nothing for v1; categories can be added later if needed.
- Periodic in-flight dumps (the option-3 ticker proposed during
  brainstorming was declined). If a stuck-fetch repro requires a
  dump, the user can add it on demand.

## Architecture

### New package: `internal/debuglog`

Single file, single responsibility. Public surface:

```go
package debuglog

// Init opens slk-debug.log in cwd (truncating) when SLK_DEBUG is set,
// configures the package-internal logger, and routes the global stdlib
// log package to the same file. When SLK_DEBUG is unset, Init is a
// no-op and Enabled() returns false.
//
// Returns the *os.File so the caller can close it on exit. Returns
// nil, nil when disabled.
func Init() (*os.File, error)

// Enabled reports whether logging is active. Cheap (atomic.Bool load).
func Enabled() bool

// Category functions. Each is a no-op when !Enabled(). Format matches
// log.Printf. Output line shape (one line per call):
//   2026-05-09T14:30:22.123456 [cache] message body here
func Cache(format string, args ...any)
func ImgFetch(format string, args ...any)
func ImgRender(format string, args ...any)
func WS(format string, args ...any)
func General(format string, args ...any)

// NextReqID returns a process-wide monotonic uint64. Used to correlate
// image fetch lifecycle log lines.
func NextReqID() uint64
```

### Implementation details

- One package-level `*log.Logger` writing to the truncated file.
- `enabled atomic.Bool`, set inside `Init` when `os.Getenv("SLK_DEBUG") != ""`
  and the file open succeeds.
- Each category function:

  ```go
  func Cache(format string, args ...any) {
      if !enabled.Load() {
          return
      }
      logger.Printf("[cache] "+format, args...)
  }
  ```

- Timestamp format via `log.Lmicroseconds | log.Ldate | log.Ltime`
  (or a custom `Logger.SetFlags(0)` + manual ISO-8601 prefix in the
  message — pick whichever produces cleaner lines; sortable
  microsecond precision is the requirement).
- `NextReqID()` reads/increments a package-level `atomic.Uint64`. It
  works regardless of `Enabled()` so signatures don't need to branch
  — call sites generate an ID and stash it in a struct, the log
  lines just become no-ops if disabled.
- `Init` also calls `log.SetOutput(file)` so untouched stdlib
  `log.Printf` calls in the rest of the codebase still land in the
  same file. When disabled, it sets `log.SetOutput(io.Discard)` to
  preserve current "silenced when not debugging" behavior.

### Wiring at startup

Replace `cmd/slk/main.go:144-158`:

```go
debugFile, err := debuglog.Init()
if err != nil {
    fmt.Fprintf(os.Stderr, "slk: could not open debug log: %v\n", err)
}
if debugFile != nil {
    defer debugFile.Close()
    debuglog.General("=== slk debug session started ===")
}
```

The existing `SLK_DEBUG_WS` block at `cmd/slk/main.go:2459-2462` is
removed. WS event tracing migrates to `debuglog.WS` calls under the
unified `SLK_DEBUG=1` switch.

## Logging surface — focus area 1: messages cache reconciliation

| Site | File:line | What we log |
|---|---|---|
| `loadCachedMessages` | `cmd/slk/main.go:1511` | entry channel; result count, oldest_ts, newest_ts, or error |
| `loadCachedThreadReplies` | `cmd/slk/main.go:1651` | entry channel/thread_ts; result count, oldest_ts, newest_ts |
| `fetchChannelMessages` | `cmd/slk/main.go:1687` | entry channel/page_size/cursor; HTTP duration; result count, oldest_ts, newest_ts; nil-vs-`[]` decision branch |
| `fetchThreadReplies` | `cmd/slk/main.go:1760` | same shape, for thread |
| Per-message upserts inside fetch paths | `cmd/slk/main.go` (new helper / inline) | channel, ts, edited?, deleted?, has_files?, num_reactions |
| `cache.UpsertMessage` | `internal/cache/messages.go:24` | channel, ts, action=insert|update (one line per upsert — fires for WS deltas too) |
| `cache.DeleteMessage` | `internal/cache/messages.go:88` | channel, ts |
| `messages.Model.SetMessages` | `internal/ui/messages/model.go` | count_before, count_new, oldest_ts_before, oldest_ts_new, newest_ts_before, newest_ts_new |
| `messages.Model.PrependMessages` | `internal/ui/messages/model.go` | count_before, count_added |
| `ChannelSelectedMsg` handler | `internal/ui/app.go:1340` | channel, cache_hit_count, fired_network_fetch=true |
| `MessagesLoadedMsg` handler | `internal/ui/app.go:1372` | channel, kind=nil_keep_cache|empty_replace|full_replace, count |
| `OlderMessagesLoadedMsg` handler | `internal/ui/app.go:1386` | channel, count_added |
| WS event handlers | `internal/slack/events.go` | event_type, channel, ts, edited_ts (when applicable). Replaces `SLK_DEBUG_WS` and extends to known events too |
| WS message dispatch | `internal/ui/app.go` (NewMessageMsg / MessageEditedMsg / MessageDeletedMsg) | what UI did: appended? replaced? cache invalidate? |

All of these go to `debuglog.Cache(...)` except the WS event handlers
which go to `debuglog.WS(...)`.

**Diagnostic example:**

```
$ grep '\[cache\] .* channel=C123ABC' slk-debug.log
```

…produces a strict timeline for one channel. A cache that's "out of
date" will manifest as either (a) a `[ws] message` line with no
following `cache.UpsertMessage` entry, (b) a `[cache] SetMessages
kind=full_replace` whose `count_new` doesn't include a TS that
appeared in a recent `[ws]` event, or (c) a `nil_keep_cache` branch
firing when it shouldn't.

## Logging surface — focus area 2: image render sizing

**Startup (one-shot):**

| Site | File:line | What we log |
|---|---|---|
| Protocol detection | `cmd/slk/main.go:406` (after `imgpkg.Detect`) | detected protocol, env vars consulted (`TERM`, `TERM_PROGRAM`, `KITTY_WINDOW_ID`, `TMUX`), config override |
| Kitty active probe | `cmd/slk/main.go:415` | probe attempted? probe result? downgrade to halfblock? |
| Existing protocol/cell logs | `cmd/slk/main.go:425, 434` | upgrade from `log.Printf` to `debuglog.ImgRender` |
| `CellPixels` | `internal/image/cellmetrics.go:12` | cell_w, cell_h, source=env_override|ioctl|fallback |

**Per-render:**

| Site | File:line | What we log |
|---|---|---|
| `computeImageTarget` | `internal/ui/imgrender/imgrender.go:100` | image_natural=(W,H), avail_cols, ctx.MaxCols, ctx.MaxRows, cell_px=(cw,ch), computed_target=(cols,rows) |
| `PickThumb` | `internal/image/fetcher.go:603` | target_px=(W,H), candidates=[(W,H,url),…], chosen=(W,H,url) |
| Kitty render entry | `internal/image/kitty.go` | render_key, target=(cols,rows), placement_diacritics_count, encoded_byte_len |
| Halfblock render entry | `internal/image/halfblock.go` | target=(cols,rows), encoded_byte_len |
| Sixel render entry | `internal/image/sixel.go` | target=(cols,rows), encoded_byte_len |
| `RenderBlock` per-block summary | `internal/ui/imgrender/imgrender.go:285` | key, decision=cached_fast_path|prerendered|spawned_fetch|placeholder |

All go to `debuglog.ImgRender(...)`.

**Diagnostic example:**

If an image renders too small, grep for the URL/key and read backwards:

1. `[imgrender] PickThumb target_px=(640,360) chose=(320,180)` →
   bad thumb pick.
2. `[imgrender] computeImageTarget natural=(800,600) avail_cols=80 MaxCols=80 MaxRows=20 target=(20,8)` →
   `MaxRows` clamp truncated unexpectedly.
3. `[imgrender] CellPixels cell_w=4 cell_h=8 source=fallback` →
   ioctl returned nonsense, fallback wrong for ghostty.

Encoded_byte_len lines also let you confirm whether ghostty received
bytes for a given size — if those are correct and the image still
renders small, it's a terminal-side bug, not ours.

## Logging surface — focus area 3: image fetch lifecycle

Each fetch is assigned a `req_id` (from `debuglog.NextReqID()`) at
the enqueue site and threaded through the pipeline so a single
fetch's full lifecycle can be grepped.

`req_id` plumbing: a new field on `image.FetchRequest`, set by the
caller in `imgrender.Renderer.RenderBlock`. The blockkit parallel
path (`internal/ui/messages/blockkit/image.go`) also generates a
`req_id` at its enqueue site. Dispatched messages
(`ImageReadyMsg`, `ImageFailedMsg`, `BlockImageReadyMsg`) gain a
`req_id` field so the UI-side handlers can log it.

| Stage | Site | What we log |
|---|---|---|
| Enqueue | `internal/ui/imgrender/imgrender.go:330` (before `go ctx.Fetcher.Fetch`) | `[imgfetch] enqueue key=… url=… panel=msgs|thread req_id=N fetching_set_size=K` |
| Singleflight | `internal/image/fetcher.go:Fetch` line 208 | `[imgfetch] sf-join key=… leader=true|false req_id=N` |
| Sem acquire | `internal/image/fetcher.go:fetchInner` line 227 | `[imgfetch] sem-acquire key=… wait_ms=X queue_depth=K req_id=N` |
| Disk-cache | inside `fetchInner` | `[imgfetch] disk-cache key=… result=hit|miss req_id=N` |
| HTTP try | `internal/image/fetcher.go:tryDownload` line 422 | `[imgfetch] http-try url=… auth_team=T attempt=A req_id=N` |
| HTTP result | same | `[imgfetch] http-result status=200 bytes=12345 dur_ms=87 req_id=N` |
| 429 backoff | `internal/image/fetcher.go:tryDownloadWithBackoff` line 393 | `[imgfetch] backoff attempt=A sleep_ms=500 req_id=N` |
| Auth fallback | `internal/image/fetcher.go:authsForURL` line 372 + existing logs at 345/352 | upgrade existing `log.Printf` to `debuglog.ImgFetch`; log auth_source=own_team|learned|fallback|public |
| Decode | `fetchInner` after download | `[imgfetch] decode key=… dur_ms=X dims=(W,H) req_id=N` |
| Prerender | `internal/image/fetcher.go:maybePrerender` line 285 | `[imgfetch] prerender key=… proto=kitty dur_ms=X req_id=N` |
| Dispatch | `imgrender.go:330-346` goroutine end | `[imgfetch] dispatch key=… kind=ready|failed req_id=N total_ms=X` |
| Receive | `internal/ui/app.go:1393-1420` (`ImageReadyMsg`/`ImageFailedMsg`) | `[imgrender] recv kind=ready key=… panel=msgs req_id=N` |
| Clear in-flight | `internal/ui/messages/model.go:HandleImageReady` line 338 + thread variant `internal/ui/thread/model.go:317` | `[imgrender] clear-fetching key=… fetching_set_size_after=K req_id=N` |
| Mark failed | `HandleImageFailed` line 381 | `[imgrender] mark-failed key=… req_id=N` |

Block-kit parallel path at `internal/ui/messages/blockkit/image.go`
gets the same shape: enqueue / http-try / http-result / dispatch /
recv. Existing `log.Printf` at `image.go:160` is upgraded.

**Diagnostic example:**

```
$ grep 'req_id=42' slk-debug.log
```

…produces a single fetch's full timeline. A stuck loading indicator
manifests as a timeline that ends prematurely:

- Ends at `enqueue` only → blocked in singleflight or semaphore
  (look for the leader's `req_id` and check if *its* timeline ended).
- Ends at `http-try` with no `http-result` → HTTP hang or panic.
- Ends at `dispatch` with no matching `recv` → `ctx.SendMsg` was nil
  or program was paused.
- Ends at `recv` with no `clear-fetching` → bug in the panel
  handler (e.g. key mismatch between fetcher and `fetching` set).

## Migration of existing `log.Printf` call sites

In-scope (rewritten to use category functions):

- `cmd/slk/main.go` lines 1523, 1603, 1615, 1663, 1691, 1764 →
  `debuglog.Cache`
- `cmd/slk/main.go` lines 425, 434 → `debuglog.ImgRender`
- `internal/image/fetcher.go` lines 345, 352, 403 →
  `debuglog.ImgFetch`
- `internal/ui/imgrender/imgrender.go:341` → `debuglog.ImgFetch`
  (or `debuglog.ImgRender` depending on which side of the
  fetch-vs-render boundary it falls on; pick during implementation)
- `internal/ui/messages/blockkit/image.go:160` → `debuglog.ImgFetch`
- `internal/slack/events.go:383` (existing unknown-WS-event dump)
  → `debuglog.WS`

Out of scope (stay on stdlib `log.Printf`, still land in the same
file):

- All other ~30 `log.Printf` call sites in `cmd/slk/main.go`
  (workspace bootstrap, clipboard, system viewer, etc.)
- `internal/slack/client.go:970`
- `internal/ui/app.go:1478, 2091, 3080, 4029`
- `internal/service/sectionstore.go:66`
- `internal/ui/clipboard_wayland.go:66`

## Testing

Unit tests in `internal/debuglog/debuglog_test.go`:

- `TestInit_TruncatesExisting` — pre-create `slk-debug.log` with
  content, set `SLK_DEBUG=1` via `t.Setenv`, `t.Chdir(t.TempDir())`,
  call `Init`, verify file size is 0.
- `TestInit_NoFileWhenDisabled` — unset SLK_DEBUG, call `Init`,
  verify no file in cwd and `Enabled()` returns false.
- `TestEnabled_FastPathNoOp` — SLK_DEBUG unset, call every category
  function, verify nothing is written and no panic.
- `TestCategoryPrefixes` — enable, call each of `Cache`, `ImgFetch`,
  `ImgRender`, `WS`, `General` once, read file, assert each line
  contains its tag.
- `TestTimestampFormat` — regex-match a line against the documented
  shape (ISO-8601 + tag + body).
- `TestConcurrentWrites` — N goroutines × M lines, assert final
  line count = N×M and no torn writes (relies on stdlib
  `log.Logger`'s mutex).
- `TestNextReqID_Monotonic` — call concurrently from N goroutines,
  collect all returned IDs, assert they are unique and span the
  expected range.

Tests use `t.TempDir()` + `t.Chdir()` (Go 1.24) to isolate the cwd.
`t.Setenv` for `SLK_DEBUG`.

Manual integration check after implementation: run
`SLK_DEBUG=1 ./slk` in a clean directory, switch between two
channels, hover an image, quit. Verify `slk-debug.log` contains
coherent timelines. Qualitative — no automated test.

## Risks and mitigations

- **Volume.** Per-message cache logging in a busy multi-workspace
  session may produce hundreds of lines per minute. Mitigation:
  truncate-on-start keeps each session bounded; user can `cp` the
  file before quitting if they want to preserve.
- **`req_id` plumbing changes signatures.** Adding a `ReqID uint64`
  to `image.FetchRequest`, `imgrender.ImageReadyMsg`,
  `imgrender.ImageFailedMsg`, and `blockkit.BlockImageReadyMsg`
  touches several files. Mitigation: it's an additive field, all
  existing zero-value paths still work; tests catch any forgotten
  threading.
- **Unrelated `log.Printf` calls now go to cwd instead of `/tmp`.**
  The behavior change is visible (file location moved) but
  intentional. Mitigation: documented in the commit message and in
  any release notes.
- **`SLK_DEBUG_WS` removal.** Anyone scripting against the old
  variable will be surprised. Mitigation: it's an undocumented
  developer flag; it gets a one-line note in the commit message.

## Out-of-scope ideas (future work)

- Per-category enable flags
  (`SLK_DEBUG=cache,imgfetch`).
- Periodic in-flight set dumps (5-second tick listing stuck fetches).
- Per-line length cap (truncate any single log line beyond e.g.
  4KB). Encoded byte-length values themselves are in-scope and
  small; the cap would only be needed if we later log raw payloads.
- Slog migration if structured queryability becomes valuable later.
