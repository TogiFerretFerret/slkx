# Version and Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Centralize slk's build-time version metadata in a new `internal/version` package, add an author/website attribution line to `slk --version` output and the `?` help modal, and verify ldflag injection via CI.

**Architecture:** New `internal/version` package owns `Version`/`Commit`/`Date` build-time vars and four formatting helpers (`Short`, `CLILongLine`, `AttributionLine`, `ModalFooter`). The `--version` CLI branch and the TUI's help modal both read from this single source. The website URL is wrapped in OSC 8 hyperlink escape sequences so it's clickable in supporting terminals and renders as plain text elsewhere. GoReleaser ldflags target the new package path; a `make verify-version` Makefile target plus a new CI step prevent silent regressions if the path ever drifts.

**Tech Stack:** Go 1.26, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`, GoReleaser v2, GitHub Actions.

**Spec:** [`docs/superpowers/specs/2026-05-27-version-and-attribution-design.md`](../specs/2026-05-27-version-and-attribution-design.md)

---

## File Map

**Create:**
- `internal/version/version.go` — exports `Version`/`Commit`/`Date` build-time vars and `Short`/`CLILongLine`/`AttributionLine`/`ModalFooter` helpers.
- `internal/version/version_test.go` — unit tests for all helpers, including a lipgloss-width assertion validating that OSC 8 escapes are width-neutral.
- `cmd/slk/main_test.go` — tests `versionOutput()` (extracted helper) so the CLI output is unit-tested.

**Modify:**
- `cmd/slk/main.go` — remove local `version`/`commit`/`date` vars (lines 52-57), import `internal/version`, extract `versionOutput()` helper, update `--version` branch (lines 353-357) and `printHelp` (line 456).
- `internal/ui/help/model.go` — add `footer string` field to `Model`, new `SetFooter(string)` method, render `footer` above the existing controls footer in `renderBox` (line 404).
- `internal/ui/help/model_test.go` — tests for the new `SetFooter` rendering path.
- `internal/ui/app.go` — call `app.help.SetFooter(version.ModalFooter())` in `NewApp` after the existing `statusbar.SetHelpHint` block (around line 367-369).
- `.goreleaser.yaml` — retarget the three `-X` ldflags (lines 32-34) to `github.com/gammons/slk/internal/version.{Version,Commit,Date}`.
- `Makefile` — add a `verify-version` target.
- `.github/workflows/ci.yml` — add a new step in the `test` job that runs `make verify-version`.

---

## Task 1: `internal/version` package skeleton + `Short()`

**Files:**
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/version/version_test.go`:

```go
package version

import "testing"

func TestShort_Dev(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()
	Version = "dev"
	if got := Short(); got != "dev" {
		t.Errorf("Short() = %q, want %q", got, "dev")
	}
}

func TestShort_PrependsV(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()
	Version = "1.2.3"
	if got := Short(); got != "v1.2.3" {
		t.Errorf("Short() = %q, want %q", got, "v1.2.3")
	}
}

func TestShort_KeepsExistingV(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()
	Version = "v1.2.3"
	if got := Short(); got != "v1.2.3" {
		t.Errorf("Short() = %q, want %q", got, "v1.2.3")
	}
}

func TestShort_PrereleaseSuffix(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()
	Version = "1.2.3-next"
	if got := Short(); got != "v1.2.3-next" {
		t.Errorf("Short() = %q, want %q", got, "v1.2.3-next")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/...`
Expected: build error — `internal/version` package does not exist.

- [ ] **Step 3: Implement minimal package + `Short()`**

Create `internal/version/version.go`:

```go
// Package version exposes slk's build-time version metadata and formatting
// helpers used by the CLI --version flag and the TUI help modal.
//
// The Version, Commit, and Date variables are set at build time via
// -ldflags -X (see .goreleaser.yaml). Local `go build` invocations leave
// the defaults in place.
package version

import "strings"

// Build-time vars. Real values are injected by GoReleaser.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Short returns a display-friendly version string. For tagged builds it
// prepends "v" if not already present; for the local dev default it
// returns "dev" unchanged.
func Short() string {
	if Version == "dev" {
		return "dev"
	}
	if strings.HasPrefix(Version, "v") {
		return Version
	}
	return "v" + Version
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/... -v`
Expected: all four `TestShort_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat(version): add internal/version package with Short() helper"
```

---

## Task 2: `CLILongLine()` helper

**Files:**
- Modify: `internal/version/version.go`
- Modify: `internal/version/version_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/version/version_test.go`:

```go
func TestCLILongLine_Defaults(t *testing.T) {
	savedV, savedC, savedD := Version, Commit, Date
	defer func() { Version, Commit, Date = savedV, savedC, savedD }()
	Version, Commit, Date = "dev", "none", "unknown"
	want := "slk dev (commit none, built unknown)"
	if got := CLILongLine(); got != want {
		t.Errorf("CLILongLine() = %q, want %q", got, want)
	}
}

func TestCLILongLine_Tagged(t *testing.T) {
	savedV, savedC, savedD := Version, Commit, Date
	defer func() { Version, Commit, Date = savedV, savedC, savedD }()
	Version, Commit, Date = "1.2.3", "abcdef0", "2026-05-27"
	want := "slk v1.2.3 (commit abcdef0, built 2026-05-27)"
	if got := CLILongLine(); got != want {
		t.Errorf("CLILongLine() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/... -run CLILongLine -v`
Expected: compile error — `undefined: CLILongLine`.

- [ ] **Step 3: Implement `CLILongLine()`**

Append to `internal/version/version.go`:

```go
import "fmt" // add to existing import block, alongside "strings"
```

(merge with the existing `import "strings"` into a grouped block:)

```go
import (
	"fmt"
	"strings"
)
```

Then append at the bottom of the file:

```go
// CLILongLine returns the human-readable version line used as the first
// line of `slk --version` output.
func CLILongLine() string {
	return fmt.Sprintf("slk %s (commit %s, built %s)", Short(), Commit, Date)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/... -v`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat(version): add CLILongLine helper"
```

---

## Task 3: `AttributionLine()` with OSC 8 hyperlink

**Files:**
- Modify: `internal/version/version.go`
- Modify: `internal/version/version_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/version/version_test.go`:

```go
import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)
```

(merge the new `"strings"` and lipgloss imports into a grouped block alongside the existing `"testing"` import.)

Then append:

```go
func TestAttributionLine_Contents(t *testing.T) {
	line := AttributionLine()
	if !strings.Contains(line, "by Grant Ammons") {
		t.Errorf("AttributionLine() missing author: %q", line)
	}
	if !strings.Contains(line, "https://grant.dev") {
		t.Errorf("AttributionLine() missing URL: %q", line)
	}
}

func TestAttributionLine_OSC8Wrapping(t *testing.T) {
	line := AttributionLine()
	// OSC 8 opener: ESC ] 8 ;; <url> ESC \
	opener := "\x1b]8;;https://grant.dev\x1b\\"
	// OSC 8 closer: ESC ] 8 ;; ESC \
	closer := "\x1b]8;;\x1b\\"
	if !strings.Contains(line, opener) {
		t.Errorf("AttributionLine() missing OSC 8 opener; got %q", line)
	}
	if !strings.Contains(line, closer) {
		t.Errorf("AttributionLine() missing OSC 8 closer; got %q", line)
	}
}

// TestAttributionLine_WidthMatchesPlain asserts that lipgloss.Width
// ignores OSC 8 escape sequences when measuring display width. If this
// test fails, lipgloss is miscounting OSC 8; see the spec's "OSC 8 width
// miscounting" mitigation.
func TestAttributionLine_WidthMatchesPlain(t *testing.T) {
	got := lipgloss.Width(AttributionLine())
	plain := "by Grant Ammons \u2014 https://grant.dev"
	want := lipgloss.Width(plain)
	if got != want {
		t.Errorf("lipgloss.Width(AttributionLine()) = %d, want %d (matching plain text width)", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/... -run Attribution -v`
Expected: compile error — `undefined: AttributionLine`.

- [ ] **Step 3: Implement `AttributionLine()`**

Append to `internal/version/version.go`:

```go
// osc8Hyperlink wraps the given URL as a clickable OSC 8 hyperlink with
// the URL as the visible label. Terminals without OSC 8 support display
// the visible label as plain text.
func osc8Hyperlink(url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
}

// AttributionLine returns the author/website credit line used both in
// `slk --version` output and embedded into the help modal footer. The
// URL is OSC 8 wrapped.
func AttributionLine() string {
	return "by Grant Ammons \u2014 " + osc8Hyperlink("https://grant.dev")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/... -v`
Expected: all tests PASS, including `TestAttributionLine_WidthMatchesPlain`.

If `TestAttributionLine_WidthMatchesPlain` fails: lipgloss does not strip OSC 8 escapes. Stop and surface this. The mitigation is to add a parallel `AttributionLinePlain()` helper (without OSC 8) and have the modal use that instead — but only resort to this if lipgloss truly miscounts.

- [ ] **Step 5: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat(version): add OSC-8-hyperlinked AttributionLine"
```

---

## Task 4: `ModalFooter()` helper

**Files:**
- Modify: `internal/version/version.go`
- Modify: `internal/version/version_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/version/version_test.go`:

```go
func TestModalFooter_Composition(t *testing.T) {
	saved := Version
	defer func() { Version = saved }()
	Version = "1.2.3"
	want := "slk v1.2.3 \u2014 " + AttributionLine()
	if got := ModalFooter(); got != want {
		t.Errorf("ModalFooter() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/... -run ModalFooter -v`
Expected: compile error — `undefined: ModalFooter`.

- [ ] **Step 3: Implement `ModalFooter()`**

Append to `internal/version/version.go`:

```go
// ModalFooter returns the single-line attribution string used as the
// extra footer line in the TUI's help modal.
func ModalFooter() string {
	return "slk " + Short() + " \u2014 " + AttributionLine()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/... -v`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat(version): add ModalFooter helper"
```

---

## Task 5: Migrate `cmd/slk/main.go` to `internal/version` + extract testable `versionOutput()`

**Files:**
- Modify: `cmd/slk/main.go`
- Create: `cmd/slk/main_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/slk/main_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/version"
)

func TestVersionOutput_ContainsAllParts(t *testing.T) {
	out := versionOutput()
	if !strings.Contains(out, version.CLILongLine()) {
		t.Errorf("versionOutput missing CLILongLine; got:\n%s", out)
	}
	if !strings.Contains(out, "by Grant Ammons") {
		t.Errorf("versionOutput missing attribution; got:\n%s", out)
	}
	if !strings.Contains(out, "Unofficial Slack client") {
		t.Errorf("versionOutput missing disclaimer; got:\n%s", out)
	}
	if !strings.Contains(out, "Slack's TOS") {
		t.Errorf("versionOutput missing TOS warning; got:\n%s", out)
	}
	// Should end with a single trailing newline.
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("versionOutput should end with exactly one newline; got:\n%q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/... -run TestVersionOutput -v`
Expected: compile error — `undefined: versionOutput`.

- [ ] **Step 3: Refactor `cmd/slk/main.go`**

3a. Remove the build-time var block at lines 52-57:

```go
// Build-time version info, injected via -ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)
```

3b. Add the import (alphabetically placed in the existing import block, after `"github.com/gammons/slk/internal/ui/themeswitcher"`):

```go
"github.com/gammons/slk/internal/version"
```

3c. Replace the existing `--version` case block (lines 353-357):

```go
case "--version", "-v", "version":
    fmt.Printf("slk %s (commit %s, built %s)\n", version, commit, date)
    fmt.Println("Unofficial Slack client. Not affiliated with Slack Technologies, LLC.")
    fmt.Println("Uses Slack's internal browser protocol; may violate Slack's TOS. Use at your own risk.")
    return
```

with:

```go
case "--version", "-v", "version":
    fmt.Print(versionOutput())
    return
```

3d. Update `printHelp` (line 456) — change the `version` reference to `version.Short()`:

Locate the trailing `, version)` in the `fmt.Printf(...)` block at the bottom of `printHelp()` (line 456) and replace `version` with `version.Short()`.

3e. Add the `versionOutput` helper function. Insert it immediately after the `printHelp()` function (after the closing `}` near line 457):

```go
// versionOutput returns the full multi-line text printed by
// `slk --version` / `slk -v` / `slk version`. Extracted into a helper so
// it can be unit-tested in cmd/slk/main_test.go.
func versionOutput() string {
	return version.CLILongLine() + "\n" +
		version.AttributionLine() + "\n" +
		"Unofficial Slack client. Not affiliated with Slack Technologies, LLC.\n" +
		"Uses Slack's internal browser protocol; may violate Slack's TOS. Use at your own risk.\n"
}
```

- [ ] **Step 4: Build to verify it compiles**

Run: `go build ./...`
Expected: no errors. (Catches any missed `version`/`commit`/`date` references.)

- [ ] **Step 5: Run the new test**

Run: `go test ./cmd/slk/... -run TestVersionOutput -v`
Expected: PASS.

- [ ] **Step 6: Smoke-check the binary output**

Run:
```bash
go build -o /tmp/slk-smoke ./cmd/slk
/tmp/slk-smoke --version
rm /tmp/slk-smoke
```

Expected output (the URL portion will contain escape sequences when viewed via `cat -v` but renders cleanly in a terminal):
```
slk dev (commit none, built unknown)
by Grant Ammons — https://grant.dev
Unofficial Slack client. Not affiliated with Slack Technologies, LLC.
Uses Slack's internal browser protocol; may violate Slack's TOS. Use at your own risk.
```

- [ ] **Step 7: Run the full test suite**

Run: `go test ./... -race`
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/slk/main.go cmd/slk/main_test.go
git commit -m "refactor(cli): migrate version vars to internal/version + add attribution line"
```

---

## Task 6: Help modal `SetFooter` + rendering

**Files:**
- Modify: `internal/ui/help/model.go`
- Modify: `internal/ui/help/model_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/help/model_test.go`:

```go
func TestSetFooterRenders(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.SetFooter("by Someone Special — https://example.com")
	m.Open()
	out := m.ViewOverlay(80, 24, "")
	if !strings.Contains(out, "by Someone Special") {
		t.Errorf("ViewOverlay missing footer text; got:\n%s", out)
	}
	if !strings.Contains(out, "https://example.com") {
		t.Errorf("ViewOverlay missing footer URL; got:\n%s", out)
	}
	// Existing controls footer must still render.
	if !strings.Contains(out, "esc/q close") {
		t.Errorf("ViewOverlay lost controls footer; got:\n%s", out)
	}
}

func TestSetFooterEmptyByDefault(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	out := m.ViewOverlay(80, 24, "")
	// No footer set: should NOT contain a literal attribution marker.
	if strings.Contains(out, "by Grant Ammons") {
		t.Errorf("ViewOverlay unexpectedly contains author line; got:\n%s", out)
	}
	// Controls footer still present.
	if !strings.Contains(out, "esc/q close") {
		t.Errorf("ViewOverlay missing controls footer; got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/help/... -run TestSetFooter -v`
Expected: compile error — `(*Model).SetFooter undefined`.

- [ ] **Step 3: Add `footer` field + `SetFooter` method**

In `internal/ui/help/model.go`, modify the `Model` struct (lines 28-35) by adding a `footer` field:

```go
// Model is the help overlay state.
type Model struct {
	entries   []Entry
	filtered  []int // indices into entries that match the current query
	query     string
	selected  int // index into filtered
	visible   bool
	searching bool // true while typing in the / search input
	footer    string // optional extra line rendered above the controls footer
}
```

Then add the setter immediately after `SetEntries` (after line 46):

```go
// SetFooter sets an optional extra footer line rendered above the
// controls footer. Pass "" to clear.
func (m *Model) SetFooter(s string) {
	m.footer = s
}
```

- [ ] **Step 4: Render the footer in `renderBox`**

In `internal/ui/help/model.go`, locate the footer rendering block (lines 404-409):

```go
footer := lipgloss.NewStyle().
    Background(bg).
    Foreground(styles.TextMuted).
    Render("/ search   esc/q close")

content := title + "\n" + inputLine + "\n\n" + strings.Join(rows, "\n") + "\n\n" + footer
```

Replace with:

```go
controlsFooter := lipgloss.NewStyle().
    Background(bg).
    Foreground(styles.TextMuted).
    Render("/ search   esc/q close")

footer := controlsFooter
if m.footer != "" {
    extraFooter := lipgloss.NewStyle().
        Background(bg).
        Foreground(styles.TextMuted).
        MaxWidth(innerWidth).
        Render(m.footer)
    footer = extraFooter + "\n" + controlsFooter
}

content := title + "\n" + inputLine + "\n\n" + strings.Join(rows, "\n") + "\n\n" + footer
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/help/... -v`
Expected: all tests PASS, including the two new ones and all pre-existing tests.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/help/model.go internal/ui/help/model_test.go
git commit -m "feat(help): add SetFooter for extra attribution line above controls"
```

---

## Task 7: Wire `app.go` to call `SetFooter(version.ModalFooter())`

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add the import**

In `internal/ui/app.go`, find the existing import block (around line 30 where `"github.com/gammons/slk/internal/ui/help"` is imported). Add immediately after it (alphabetical order within `github.com/gammons/slk/internal/`):

```go
"github.com/gammons/slk/internal/version"
```

(Place it after the last `internal/ui/...` import; if uncertain, run `goimports` on the file after editing.)

- [ ] **Step 2: Call `SetFooter` in `NewApp`**

Locate the statusbar hint block at lines 365-369:

```go
// Seed the statusbar hint with the configured help key label so it
// stays accurate if the binding is ever changed.
if helpKey := app.keys.Help.Help().Key; helpKey != "" {
    app.statusbar.SetHelpHint(helpKey + " for keybindings")
}
return app
```

Insert the new `SetFooter` call immediately before the `return app` line:

```go
// Seed the statusbar hint with the configured help key label so it
// stays accurate if the binding is ever changed.
if helpKey := app.keys.Help.Help().Key; helpKey != "" {
    app.statusbar.SetHelpHint(helpKey + " for keybindings")
}
// Seed the help modal's extra footer with the version + attribution line.
app.help.SetFooter(version.ModalFooter())
return app
```

- [ ] **Step 3: Build to verify it compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 4: Run the relevant tests**

Run: `go test ./internal/ui/... -race`
Expected: all tests PASS.

- [ ] **Step 5: Manual smoke check (optional but recommended)**

Run: `go run ./cmd/slk` and press `?`. Verify the modal shows `slk dev — by Grant Ammons — https://grant.dev` (or a soft-wrapped variant at narrow widths) just above the `/ search   esc/q close` line. Press `esc` then `Ctrl+C` to quit.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(ui): wire help modal footer to version.ModalFooter()"
```

---

## Task 8: Update `.goreleaser.yaml` ldflags + add `make verify-version`

**Files:**
- Modify: `.goreleaser.yaml`
- Modify: `Makefile`

- [ ] **Step 1: Update goreleaser ldflags**

In `.goreleaser.yaml`, replace lines 30-34:

```yaml
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}
```

with:

```yaml
    ldflags:
      - -s -w
      - -X github.com/gammons/slk/internal/version.Version={{.Version}}
      - -X github.com/gammons/slk/internal/version.Commit={{.Commit}}
      - -X github.com/gammons/slk/internal/version.Date={{.Date}}
```

- [ ] **Step 2: Add `verify-version` target to the Makefile**

In `Makefile`, update the `.PHONY` line to include the new target:

```makefile
.PHONY: build test lint run clean verify-version
```

Then append at the end of the file:

```makefile
verify-version:
	@go build -ldflags "-X github.com/gammons/slk/internal/version.Version=test1.2.3" -o /tmp/slk-vtest ./cmd/slk
	@if /tmp/slk-vtest --version | grep -q "slk vtest1.2.3"; then \
		echo "ldflag injection OK"; \
	else \
		echo "ldflag injection broken: --version did not contain 'slk vtest1.2.3'"; \
		/tmp/slk-vtest --version; \
		rm -f /tmp/slk-vtest; \
		exit 1; \
	fi
	@rm -f /tmp/slk-vtest
```

- [ ] **Step 3: Run `make verify-version` locally**

Run: `make verify-version`
Expected output:
```
ldflag injection OK
```
Exit code 0.

If it fails, the most likely cause is a typo in the goreleaser path or the Makefile path — they must match exactly.

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yaml Makefile
git commit -m "build(version): retarget ldflags to internal/version + add verify-version"
```

---

## Task 9: Add `make verify-version` step to CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add a new step after the existing `Test` step**

In `.github/workflows/ci.yml`, locate lines 40-41:

```yaml
      - name: Test
        run: go test ./... -race
```

Insert immediately after them:

```yaml
      - name: Verify version ldflag injection
        run: make verify-version
```

The final `test` job section should read:

```yaml
  test:
    name: test
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install X11 headers (cgo dep of golang.design/x/clipboard)
        run: |
          sudo apt-get update
          sudo apt-get install -y --no-install-recommends libx11-dev

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: stable
          cache: true

      - name: Build
        run: go build ./...

      - name: Test
        run: go test ./... -race

      - name: Verify version ldflag injection
        run: make verify-version
```

- [ ] **Step 2: Lint the yaml locally if you have yamllint installed (optional)**

Run: `yamllint .github/workflows/ci.yml` (skip if not installed).

- [ ] **Step 3: Run the full test suite + verify-version locally to confirm everything still works end-to-end**

Run: `go test ./... -race && make verify-version`
Expected: all tests PASS and `ldflag injection OK`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: verify version ldflag injection on every push/PR"
```

---

## Final verification

- [ ] **Run the full test suite once more:**

```bash
go test ./... -race
```
Expected: all tests PASS.

- [ ] **Run lint:**

```bash
make lint
```
Expected: no findings (or only pre-existing ones — confirm any reported issues are not in files touched by this plan).

- [ ] **Run `make verify-version`:**

```bash
make verify-version
```
Expected: `ldflag injection OK`.

- [ ] **Smoke-run the binary:**

```bash
go run ./cmd/slk --version
```

Expected:
```
slk dev (commit none, built unknown)
by Grant Ammons — https://grant.dev
Unofficial Slack client. Not affiliated with Slack Technologies, LLC.
Uses Slack's internal browser protocol; may violate Slack's TOS. Use at your own risk.
```

- [ ] **Smoke-test the TUI help modal:**

```bash
go run ./cmd/slk
```

Press `?` and confirm the modal shows the attribution footer line. Press `esc` then `Ctrl+C` to quit.

- [ ] **Review the commit log:**

```bash
git log --oneline -10
```

Expected: 9 new commits corresponding to Tasks 1-9.
