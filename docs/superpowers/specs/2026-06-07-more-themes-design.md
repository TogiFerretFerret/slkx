# More Themes + Channels-Panel Contrast — Design

## Problem

`slk` ships 36 built-in themes, but two gaps motivate this work:

1. **Not enough variety.** Users want more recognizable editor/vim palettes
   and a few Slack-branded looks.
2. **Weak channels-panel separation.** Almost every existing dark theme sets
   `SidebarBackground` only a shade or two darker than `Background`, so the
   channels panel (sidebar) and the message pane read as one flat surface.
   The contrast capability exists (see `slack default`: white pane + dark
   `#434243` sidebar) but is barely used. This is a wasted opportunity.

## Goals

- Add a curated batch of ~24 new themes, each with deliberate channels-panel
  contrast.
- Retune existing dark themes so the channels panel reads as a distinct
  surface, **without** altering each theme's core identity (background, text,
  accent colors stay the same — only sidebar/rail backgrounds deepen).
- Encode the contrast requirement as a regression-guarding test across all
  hex-based themes.

## Non-Goals

- No refactor of the theme storage mechanism (stays in the `builtinThemes`
  Go map). No `go:embed` TOML migration.
- No algorithmic sidebar-darkening knob — sidebar colors are hand-tuned.
- No changes to `Background`/`Text`/accent colors of existing themes.
- ANSI themes and `hot dog stand` are left untouched (palette-bound /
  intentional).

## Approach

Stay with the existing pattern: themes are entries in the `builtinThemes`
map in `internal/ui/styles/themes.go`. Add new entries; edit existing
entries in place for the sidebar retune. Compile-time-checked, matches every
prior theme change, trivially testable.

### The contrast rule

Every theme (new and retuned), except the allowlisted exceptions, must give
the channels panel a **perceptibly distinct surface** from the message pane:

- **Mid / light themes:** `SidebarBackground` is a clearly darker (and often
  more saturated) shade than `Background` — a deliberate step, not a 1–2%
  nudge. `RailBackground` darker still.
- **Near-black themes** (e.g. Vesper, GitHub Dark): a uniformly darker
  sidebar is impossible, so the sidebar may instead be a slightly *raised*
  (lighter) surface. Direction is per-theme aesthetics; what matters is a
  perceptible step.
- **Light themes:** keep the existing convention — a dark sidebar against the
  light pane (already enforced by `TestLightThemesHaveDarkSidebars`).

Encoded as a **CIELAB lightness (L\*) difference test**: for every hex-based
theme, `|Lstar(Background) − Lstar(SidebarBackground)|` must meet or exceed a
threshold. We use CIELAB L\* (derived from sRGB relative luminance via the
standard linearization + the L\* transfer function) because it is
*perceptually uniform* — a single threshold behaves consistently across light
and dark themes, unlike raw relative luminance whose absolute deltas collapse
toward zero on dark palettes. The difference is **absolute** so near-black
themes may raise rather than darken the sidebar.

Starting threshold: **`ΔL* ≥ 6.0`** (calibrated so `slack default`'s split,
`ΔL* ≈ 72`, passes easily while the current `dark` theme's `ΔL* ≈ 3.2` nudge
fails). The contrast test reports each theme's measured `ΔL*` on failure so
the retune is a deterministic adjust-and-rerun loop. The exact threshold may
be nudged within ~5–7 during implementation if a hand-tuned palette that
looks clearly distinct lands just under the line.

**Allowlist (excluded from the test):** `ansi dark`, `ansi light` (ANSI
palette numbers, not hex — `SidebarBackground` falls back to `Background`),
and `hot dog stand` (deliberately garish; its red sidebar vs yellow pane is
already high-contrast but we don't want it to constrain the threshold).

### New themes

All distinct from the current 36; each built to the contrast rule. Any that
end up too close to an existing theme during build get dropped.

- **Dark editor palettes:** Zenburn, Gruvbox Material Dark, Nightfox,
  Carbonfox, Melange Dark, Vesper, Flexoki Dark, Modus Vivendi, Night Owl,
  Poimandres, Ayu Dark, Kanagawa Dragon
- **Light editor palettes:** Rosé Pine Dawn, Everforest Light, Dawnfox,
  Flexoki Light, Modus Operandi, Kanagawa Lotus, PaperColor Light
- **Slack-branded sidebar looks** (each paired with a tuned message pane):
  Aubergine (classic Slack), Hoth, Monument, Choco Mint, Ochin

Light themes set `SidebarBackground`, `SidebarText`, `SidebarTextMuted`,
`RailBackground` explicitly (dark sidebar against light pane). Dark themes
set `SidebarBackground` + `RailBackground` (text falls back to `TextPrimary`/
`TextMuted` unless legibility needs an override).

### Retune existing themes

For each existing **dark** hex theme, deepen `SidebarBackground` and
`RailBackground` enough to pass the contrast test, keeping the same hue
family so the theme still looks like itself. Existing light themes already
comply. Untouched: `hot dog stand`, `ansi dark`, `ansi light`.

## Components Affected

- `internal/ui/styles/themes.go` — new map entries; edited sidebar/rail
  values on existing dark themes.
- `internal/ui/styles/themes_test.go` — new registration + required-color
  tests for the new themes; the CIELAB ΔL* contrast test (with allowlist);
  a small `lstar(hex)` helper (test-local).
- `README.md` — theme count (3 occurrences: lines ~4, ~17, ~30).
- `wiki/Features.md` — theme count (line ~95).
- `wiki/Configuration.md` — optional short note that themes ship with a
  distinct channels panel; no exhaustive name list exists to maintain.

## Testing

- `TestMoreThemesRegistered` — every new theme appears in `ThemeNames()`.
- `TestMoreThemesHaveRequiredColors` — every new theme has all 10 required
  color fields populated.
- `TestMoreLightThemesHaveDarkSidebars` — new light themes set
  `SidebarBackground` + `RailBackground`.
- `TestChannelsPanelContrast` — for every hex theme not in the allowlist,
  `|Lstar(Background) − Lstar(SidebarBackground)| ≥ 6.0`. Reports each
  theme's measured ΔL* on failure. This is the guard that forces the retune
  and prevents regressions.
- Full `go test ./...` and `golangci-lint run` green before completion.

## Verification of "looks good"

The contrast test guarantees mechanical separation, but final palette
quality is subjective. The reviewer (user) previews live by running `slk`
and cycling themes with `Ctrl+y` in the worktree. Implementation builds the
binary and confirms it launches; the user signs off on the visual result.

## Risks

- **Retuning changes appearance for current users of existing themes.**
  Accepted by the user (the "it's a waste" call). Mitigated by changing only
  sidebar/rail backgrounds, never the message-pane background/text/accents.
- **Threshold too strict/loose.** Calibrated empirically against the known-good
  `slack default` split during implementation.
