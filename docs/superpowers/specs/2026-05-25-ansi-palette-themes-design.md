# ANSI-palette themes design

**Issue:** [#35 — Add a theme that uses ANSI colors only](https://github.com/gammons/slk/issues/35)

**Date:** 2026-05-25

## Goal

Add two built-in themes — `ansi-dark` and `ansi-light` — that inherit
the user's terminal color palette instead of using hard-coded RGB
values. When the user re-themes their terminal (light/dark mode,
solarized, accessibility palette, etc.), slk's UI colors should follow.

## Background

slk's theme system stores every color as a `string` in `ThemeColors`
(`internal/ui/styles/themes.go:21`). At render time the string is
passed to `lipgloss.Color(...)`, which already accepts three forms:

- `"#RRGGBB"` → 24-bit truecolor (`color.RGBA`)
- `"0"`–`"15"` → ANSI 16 (`ansi.BasicColor`)
- `"16"`–`"255"` → ANSI 256 (`ansi.IndexedColor`)

All 34 existing built-in themes use hex. No code today uses
`ansi.BasicColor` directly, and no test exercises the non-hex code
path.

A theme that uses ANSI 16 colors is the only mechanism that yields
palette inheritance — truecolor escapes always bypass the user's
terminal palette and ANSI 256 indices ≥ 16 are fixed by the terminal,
not user-configurable.

## Why two themes (not one auto-adapting)

An alternative design considered a single `terminal` theme that uses
the terminal's default foreground/background (via `lipgloss.NoColor`
or empty strings) so it auto-adapts to light or dark terminals. That
approach was rejected for the initial implementation because slk's
rendering pipeline assumes every color resolves to concrete RGB
values:

- `internal/ui/styles/tint.go:51-67` linearly mixes RGB channels to
  derive selection-tint and compose-insert backgrounds. With "no
  background," there's no RGB to mix.
- `internal/ui/messages/render.go` always emits `\x1b[48;2;R;G;Bm`
  style escapes; `.RGBA()` on `NoColor{}` returns `(0,0,0,0)` which
  would render as literal black, not "terminal default."
- `RepaintBgToSelectionTint` assumes the bg has a parameter substring
  to substitute against.

Each of these would need a NoColor branch and a fallback strategy.
Two pinned themes ship the user-visible feature with materially less
code change and risk. An auto-adapting variant remains a possible
follow-up.

## Required code changes

### 1. New theme entries (`internal/ui/styles/themes.go`)

Two entries in `builtinThemes`. All 10 required `ThemeColors` fields
populated with ANSI 16 number strings (`"0"`–`"15"`). Optional fields
(`SidebarBackground`, selection backgrounds, etc.) left unset so
derived defaults compute from base colors via existing fallback
behavior.

Initial palette (subject to taste during implementation):

| Field         | `ansi-dark` | `ansi-light` |
|---------------|-------------|--------------|
| `primary`     | `"4"` blue  | `"4"` blue   |
| `accent`      | `"6"` cyan  | `"6"` cyan   |
| `warning`     | `"3"` yellow| `"3"` yellow |
| `error`       | `"1"` red   | `"1"` red    |
| `background`  | `"0"` black | `"15"` white |
| `surface`     | `"8"` br.bk | `"7"` white  |
| `surface_dark`| `"0"` black | `"7"` white  |
| `text`        | `"15"` white| `"0"` black  |
| `text_muted`  | `"8"` br.bk | `"8"` br.bk  |
| `border`      | `"8"` br.bk | `"8"` br.bk  |

The final palette values may be tuned during implementation based on
visual review against real terminal palettes.

### 2. ANSI-aware escape emission (`internal/ui/messages/render.go`)

`bgANSIFor` and `fgANSIFor` currently always emit truecolor. They
need to type-switch on the input `color.Color`:

- `ansi.BasicColor` 0–7  → `\x1b[4{n}m`  (bg) / `\x1b[3{n}m`  (fg)
- `ansi.BasicColor` 8–15 → `\x1b[10{n-8}m` (bg) / `\x1b[9{n-8}m` (fg)
- `ansi.IndexedColor`    → `\x1b[48;5;Nm` (bg) / `\x1b[38;5;Nm` (fg)
- Otherwise              → existing `\x1b[48;2;R;G;Bm` / `\x1b[38;2;R;G;Bm`

This makes palette inheritance work through slk's hand-rolled escape
path (used by `BgANSI`, `FgANSI`, `SidebarBgANSI`, etc.), not just
through bubbletea's screen-level color-profile downgrade.

### 3. Boundary-aware selection-tint substitution

`RepaintBgToSelectionTint` (`render.go:327`) does
`strings.ReplaceAll(s, fromParams, toParams)` where `fromParams` is
the bg's SGR parameter substring (e.g. `"48;2;26;26;46"`). With
ANSI 16 colors the substring becomes a 1–3 character number like
`"40"`, which would collide with arbitrary text in rendered output
(timestamps, message body content, usernames).

The substitution must match the source param only when it is a
complete SGR token — i.e. preceded by `\x1b[` or `;` and followed by
`;` or `m`. A compiled regex per-source-param is the most direct fix
and preserves the "match across bundled SGR sequences" property the
current code is built around (lipgloss bundles bold + fg + bg into a
single `\x1b[1;38;2;…;48;2;…m` escape).

This change applies regardless of whether the theme is ANSI or
truecolor — it makes the substitution always safe.

## Tests

### `internal/ui/styles/themes_test.go` (extend)

- `ansi-dark` and `ansi-light` registered in `builtinThemes` and
  surfaced by `ThemeNames()`.
- Required color fields populated and parse via `lipgloss.Color(...)`
  to `ansi.BasicColor` (type assertion, not RGB comparison).

### `internal/ui/messages/render_test.go` (new file)

- `bgANSIFor(ansi.BasicColor(1))` → `"\x1b[41m"`
- `bgANSIFor(ansi.BasicColor(9))` → `"\x1b[101m"`
- `bgANSIFor(ansi.IndexedColor(42))` → `"\x1b[48;5;42m"`
- `bgANSIFor(color.RGBA{R:26,G:26,B:46,A:255})` → `"\x1b[48;2;26;26;46m"`
  (regression guard for existing themes)
- Symmetric `fgANSIFor` cases.
- `RepaintBgToSelectionTint` correctness:
  - ANSI source bg (`"40"`): substitutes only complete SGR tokens.
    `"hello 40 world\x1b[40mtinted\x1b[m"` → only the second `40` is
    replaced.
  - Bundled SGR: `"\x1b[1;31;40mboldred\x1b[m"` → the `40` token is
    correctly substituted within the bundle.
  - Truecolor source bg: existing behavior unchanged.

## Edge cases & known behaviors

- **Selection tint stays truecolor on ANSI themes.** `mixColors`
  always returns RGB; the tinted region of a selected row uses a
  truecolor approximation rather than a palette color. Acceptable:
  the tint is a small accent and the rest of the row inherits palette
  via the substituted ANSI codes.
- **Compose-insert background** is similarly derived via `mixColors`,
  so it's truecolor RGB on an ANSI theme. Same trade-off.
- **Color caches** (`cachedBgColor` etc., `render.go:262-280`) key on
  `==`; `ansi.BasicColor` is a `uint8` alias and compares fine. No
  cache invalidation work required.
- **Bubbletea's color-profile downgrade** still applies — on a
  16-color terminal, even the truecolor selection-tint escapes are
  quantized to ANSI 16, so the visual gap between "palette-inherited
  base" and "truecolor tint" disappears on the most palette-sensitive
  terminals.

## Documentation

- `wiki/Configuration.md` — short subsection in the themes area
  explaining `ansi-dark` and `ansi-light` inherit the user's terminal
  palette, with the selection-tint caveat noted.
- `README.md:17` — bump the theme count from "35+" to reflect the
  actual count (currently 34 built-ins; will be 36 after this change).
- No changes to `wiki/Terminal-Compatibility.md` — the new themes
  work on both truecolor and 16-color terminals.

## Out of scope

- Auto-adapting single `terminal` theme using default fg/bg
  (`lipgloss.NoColor`). Possible follow-up.
- ANSI-aware `mixColors` so selection tints also honor the user's
  palette.
- Documenting non-hex color strings in the user custom-theme TOML
  schema. Works incidentally today; explicit support deferred.

## Success criteria

1. `ansi-dark` and `ansi-light` appear in the `Ctrl+y` theme
   switcher.
2. `go build ./...` succeeds.
3. `go test ./...` passes, including new tests in `themes_test.go`
   and `render_test.go`.
4. **Manual verification:** while slk is open on `ansi-dark`,
   changing the terminal's palette (e.g. switching iTerm/Alacritty
   colorscheme) changes slk's primary/accent/warning/error/border
   colors on the next render.
5. **Regression:** switching to any existing hex-based theme renders
   identically to current behavior; existing render tests pass
   unchanged.

## Risk assessment

- **Highest risk:** the boundary-aware regex in
  `RepaintBgToSelectionTint`. A wrong regex could break selection
  rendering for all themes, not just ANSI ones. Mitigated by
  explicit unit tests covering truecolor (existing), ANSI bare,
  bundled SGR, and the "literal number in content" collision case.
- **Lower risk:** ugly palette choices. Mitigated by visual review
  during implementation.

## Files touched

- `internal/ui/styles/themes.go`
- `internal/ui/styles/themes_test.go`
- `internal/ui/messages/render.go`
- `internal/ui/messages/render_test.go` (new)
- `wiki/Configuration.md`
- `README.md` (theme count)
