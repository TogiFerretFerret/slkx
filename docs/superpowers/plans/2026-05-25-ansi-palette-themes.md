# ANSI-palette themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two built-in themes (`ansi-dark`, `ansi-light`) that use ANSI 16 color codes so slk inherits the user's terminal palette.

**Architecture:** Three coordinated changes to `internal/ui/messages/render.go` plus two theme entries in `internal/ui/styles/themes.go`. (1) Teach `bgANSIFor`/`fgANSIFor` to emit native ANSI 16 / 256 escapes for `ansi.BasicColor`/`ansi.IndexedColor` colors so palette inheritance reaches slk's hand-rolled escape path. (2) Add a grammar-aware SGR bg-parameter substitution helper that correctly handles short ANSI params like `"40"` without colliding with 256-color sub-arguments. (3) Wire that helper into `RepaintBgToSelectionTint`. Then add two theme entries using `"0"`–`"15"` ANSI 16 number strings.

**Tech Stack:** Go, `charm.land/lipgloss/v2`, `github.com/charmbracelet/x/ansi`, `image/color`.

**Spec:** `docs/superpowers/specs/2026-05-25-ansi-palette-themes-design.md`

---

## File structure

- **Modify** `internal/ui/messages/render.go`
  - `bgANSIFor` / `fgANSIFor`: type-switch on `color.Color` to emit basic ANSI / 256 / truecolor variants.
  - New unexported helper `substituteBgSGR(s, fromParam, toParam string) string` — grammar-aware substitution that walks `\x1b[…m` SGR sequences and replaces bg tokens whose stringified form equals `fromParam` with `toParam`. Knows about `48;5;N` and `48;2;R;G;B` so the `N` and `R/G/B` arguments are never matched against `fromParam`.
  - `RepaintBgToSelectionTint`: switch from `strings.ReplaceAll` to the new helper.

- **Modify** `internal/ui/messages/render_test.go`
  - Add tests for the three changes above.

- **Modify** `internal/ui/styles/themes.go`
  - Add two map entries to `builtinThemes`: `"ansi-dark"` and `"ansi-light"`.

- **Modify** `internal/ui/styles/themes_test.go`
  - Add tests asserting both themes registered, have all required color fields, and that values resolve to `ansi.BasicColor`.

- **Modify** `wiki/Configuration.md`
  - Short subsection in the themes area describing the new themes.

- **Modify** `README.md`
  - Update theme count.

---

## Task 1: ANSI-aware `bgANSIFor` / `fgANSIFor`

**Files:**
- Modify: `internal/ui/messages/render.go:348-358`
- Test: `internal/ui/messages/render_test.go`

**Why:** Today these functions always emit `\x1b[48;2;R;G;Bm` truecolor. For a theme that uses `ansi.BasicColor`, this loses the "this is ANSI red" signal and the user's terminal palette is bypassed (truecolor escapes are not palette-translated). Type-switching on the color preserves palette inheritance.

- [ ] **Step 1.1: Write the failing test**

Append to `internal/ui/messages/render_test.go`:

```go
// TestBgANSIForBasicColor asserts that bgANSIFor emits native ANSI 16
// background escapes (e.g. "\x1b[41m") for ansi.BasicColor instead of
// degrading to truecolor. Native ANSI 16 escapes are required for the
// terminal palette to be honored — truecolor escapes always bypass it.
func TestBgANSIForBasicColor(t *testing.T) {
	cases := []struct {
		name string
		c    ansi.BasicColor
		want string
	}{
		{"black", 0, "\x1b[40m"},
		{"red", 1, "\x1b[41m"},
		{"white", 7, "\x1b[47m"},
		{"bright black", 8, "\x1b[100m"},
		{"bright red", 9, "\x1b[101m"},
		{"bright white", 15, "\x1b[107m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bgANSIFor(tc.c)
			if got != tc.want {
				t.Errorf("bgANSIFor(%d) = %q, want %q", tc.c, got, tc.want)
			}
		})
	}
}

// TestFgANSIForBasicColor: symmetric for foreground.
func TestFgANSIForBasicColor(t *testing.T) {
	cases := []struct {
		name string
		c    ansi.BasicColor
		want string
	}{
		{"black", 0, "\x1b[30m"},
		{"red", 1, "\x1b[31m"},
		{"white", 7, "\x1b[37m"},
		{"bright black", 8, "\x1b[90m"},
		{"bright red", 9, "\x1b[91m"},
		{"bright white", 15, "\x1b[97m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fgANSIFor(tc.c)
			if got != tc.want {
				t.Errorf("fgANSIFor(%d) = %q, want %q", tc.c, got, tc.want)
			}
		})
	}
}

// TestBgFgANSIForIndexedColor asserts ANSI 256 emission for ansi.IndexedColor.
func TestBgFgANSIForIndexedColor(t *testing.T) {
	if got := bgANSIFor(ansi.IndexedColor(42)); got != "\x1b[48;5;42m" {
		t.Errorf("bgANSIFor(IndexedColor(42)) = %q, want %q", got, "\x1b[48;5;42m")
	}
	if got := fgANSIFor(ansi.IndexedColor(200)); got != "\x1b[38;5;200m" {
		t.Errorf("fgANSIFor(IndexedColor(200)) = %q, want %q", got, "\x1b[38;5;200m")
	}
}

// TestBgFgANSIForRGBA is a regression guard: truecolor emission is unchanged
// for existing hex-based themes.
func TestBgFgANSIForRGBA(t *testing.T) {
	c := color.RGBA{R: 26, G: 26, B: 46, A: 0xff}
	if got := bgANSIFor(c); got != "\x1b[48;2;26;26;46m" {
		t.Errorf("bgANSIFor(RGBA) = %q, want %q", got, "\x1b[48;2;26;26;46m")
	}
	if got := fgANSIFor(c); got != "\x1b[38;2;26;26;46m" {
		t.Errorf("fgANSIFor(RGBA) = %q, want %q", got, "\x1b[38;2;26;26;46m")
	}
}
```

You will need to ensure `image/color` is imported in `render_test.go`. Add it if missing:

```go
import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)
```

- [ ] **Step 1.2: Run the new tests to verify they fail**

Run:
```bash
go test ./internal/ui/messages/ -run 'TestBgANSIForBasicColor|TestFgANSIForBasicColor|TestBgFgANSIForIndexedColor|TestBgFgANSIForRGBA' -v
```

Expected: `TestBgFgANSIForRGBA` PASSES (existing behavior). `TestBgANSIForBasicColor`, `TestFgANSIForBasicColor`, `TestBgFgANSIForIndexedColor` all FAIL with output showing `\x1b[48;2;…;…;…m` instead of the expected `\x1b[41m`/`\x1b[48;5;42m` style.

- [ ] **Step 1.3: Implement the type-switch**

Replace `bgANSIFor` and `fgANSIFor` in `internal/ui/messages/render.go:348-358` with:

```go
// bgANSIFor returns the ANSI background-color escape for c.
// For ansi.BasicColor it emits the native 16-color SGR (\x1b[40m–\x1b[47m,
// \x1b[100m–\x1b[107m) so the user's terminal palette is honored.
// For ansi.IndexedColor it emits the 256-color form (\x1b[48;5;Nm).
// Otherwise it falls back to truecolor (\x1b[48;2;R;G;Bm).
func bgANSIFor(c color.Color) string {
	switch v := c.(type) {
	case ansi.BasicColor:
		if v < 8 {
			return fmt.Sprintf("\x1b[%dm", 40+int(v))
		}
		if v < 16 {
			return fmt.Sprintf("\x1b[%dm", 100+int(v-8))
		}
		// out-of-range BasicColor: fall through to RGBA
	case ansi.IndexedColor:
		return fmt.Sprintf("\x1b[48;5;%dm", int(v))
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// fgANSIFor returns the ANSI foreground-color escape for c.
// See bgANSIFor for the type-switch rationale.
func fgANSIFor(c color.Color) string {
	switch v := c.(type) {
	case ansi.BasicColor:
		if v < 8 {
			return fmt.Sprintf("\x1b[%dm", 30+int(v))
		}
		if v < 16 {
			return fmt.Sprintf("\x1b[%dm", 90+int(v-8))
		}
	case ansi.IndexedColor:
		return fmt.Sprintf("\x1b[38;5;%dm", int(v))
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}
```

- [ ] **Step 1.4: Run the tests to verify they pass**

Run:
```bash
go test ./internal/ui/messages/ -run 'TestBgANSIForBasicColor|TestFgANSIForBasicColor|TestBgFgANSIForIndexedColor|TestBgFgANSIForRGBA' -v
```

Expected: all 4 tests PASS.

- [ ] **Step 1.5: Run the full package test to catch any regression**

Run:
```bash
go test ./internal/ui/messages/
```

Expected: PASS. If existing tests fail, investigate — most likely a snapshot test that compared truecolor output against new ANSI output for some non-hex value (unlikely given current themes are all hex, but worth confirming).

- [ ] **Step 1.6: Commit**

```bash
git add internal/ui/messages/render.go internal/ui/messages/render_test.go
git commit -m "feat(render): emit native ANSI escapes for BasicColor/IndexedColor

bgANSIFor and fgANSIFor now type-switch on the input color. For
ansi.BasicColor they emit native 16-color SGR (\x1b[41m etc.) so the
user's terminal palette is honored. For ansi.IndexedColor they emit
the 256-color form. Existing hex/RGBA themes still emit truecolor
unchanged.

Prep for #35 ANSI-palette themes."
```

---

## Task 2: Grammar-aware SGR bg-parameter substitution helper

**Files:**
- Modify: `internal/ui/messages/render.go` (add new helper near `bgSGRParams`)
- Test: `internal/ui/messages/render_test.go`

**Why:** `RepaintBgToSelectionTint` currently uses `strings.ReplaceAll(s, fromParams, toParams)`. With truecolor source params like `"48;2;26;26;46"`, collisions with rendered text are extremely unlikely. With ANSI 16 source params like `"40"`, collisions are common — any literal `"40"` in a timestamp, username, or message body would be corrupted. A naive boundary check is also unsafe because `38;5;40` (256-color FG index 40) contains a `40` that must NOT be matched as a bg basic-color.

The fix is a grammar-aware walker that tokenizes SGR parameter lists, treating `38;5;N`, `38;2;R;G;B`, `48;5;N`, `48;2;R;G;B` as multi-param tokens, then substitutes any bg token whose stringified form equals `from`.

- [ ] **Step 2.1: Write the failing test**

Append to `internal/ui/messages/render_test.go`:

```go
// TestSubstituteBgSGR exercises the grammar-aware bg-parameter
// substitution. The helper must:
//   (1) substitute the param when it stands alone (\x1b[40m)
//   (2) substitute the param within a bundled SGR (\x1b[1;31;40m)
//   (3) NOT corrupt literal digits in non-SGR content
//   (4) NOT match the param value inside a 256-color sub-argument
//       (\x1b[38;5;40m is an FG index 40, not a bg basic 40)
//   (5) leave the string unchanged when from == to
func TestSubstituteBgSGR(t *testing.T) {
	const to = "48;2;100;100;200"

	cases := []struct {
		name string
		in   string
		from string
		want string
	}{
		{
			name: "standalone ANSI bg",
			in:   "\x1b[40mhello\x1b[m",
			from: "40",
			want: "\x1b[" + to + "mhello\x1b[m",
		},
		{
			name: "bundled SGR with ANSI bg",
			in:   "\x1b[1;31;40mbold red on black\x1b[m",
			from: "40",
			want: "\x1b[1;31;" + to + "mbold red on black\x1b[m",
		},
		{
			name: "literal 40 in content is not touched",
			in:   "page 40 of 100\x1b[40mtinted\x1b[m",
			from: "40",
			want: "page 40 of 100\x1b[" + to + "mtinted\x1b[m",
		},
		{
			name: "256-color FG index 40 is not touched",
			in:   "\x1b[38;5;40mfg only\x1b[m",
			from: "40",
			want: "\x1b[38;5;40mfg only\x1b[m",
		},
		{
			name: "256-color FG index 40 alongside bg 40 — only bg substituted",
			in:   "\x1b[38;5;40;40mfg256 bg basic\x1b[m",
			from: "40",
			want: "\x1b[38;5;40;" + to + "mfg256 bg basic\x1b[m",
		},
		{
			name: "truecolor bg substring substitution",
			in:   "\x1b[48;2;26;26;46mhello\x1b[m",
			from: "48;2;26;26;46",
			want: "\x1b[" + to + "mhello\x1b[m",
		},
		{
			name: "truecolor bg within bundled SGR",
			in:   "\x1b[1;38;2;255;255;255;48;2;26;26;46mtext\x1b[m",
			from: "48;2;26;26;46",
			want: "\x1b[1;38;2;255;255;255;" + to + "mtext\x1b[m",
		},
		{
			name: "no match leaves string unchanged",
			in:   "\x1b[31mred fg only\x1b[m",
			from: "40",
			want: "\x1b[31mred fg only\x1b[m",
		},
		{
			name: "from == to is a no-op",
			in:   "\x1b[40mhello\x1b[m",
			from: "40",
			// to passed = "40" same as from
			want: "\x1b[40mhello\x1b[m",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useTo := to
			if tc.name == "from == to is a no-op" {
				useTo = "40"
			}
			got := substituteBgSGR(tc.in, tc.from, useTo)
			if got != tc.want {
				t.Errorf("substituteBgSGR(%q, %q, %q) = %q, want %q",
					tc.in, tc.from, useTo, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2.2: Run the new test to verify it fails**

Run:
```bash
go test ./internal/ui/messages/ -run 'TestSubstituteBgSGR' -v
```

Expected: FAIL with `undefined: substituteBgSGR`.

- [ ] **Step 2.3: Implement `substituteBgSGR`**

Add this helper to `internal/ui/messages/render.go` immediately below `bgSGRParams` (around line 346):

```go
// substituteBgSGR walks s, finds every SGR sequence (\x1b[...m), tokenizes
// its parameter list with awareness of extended-color groups (38;5;N,
// 38;2;R;G;B, 48;5;N, 48;2;R;G;B), and substitutes any bg-token whose
// stringified form equals from with to. Non-SGR text and SGR sequences
// that don't contain a matching bg token are passed through unchanged.
//
// Grammar awareness matters because a bg token can be a single param
// ("40"–"47", "100"–"107") that may otherwise collide with arbitrary
// digit substrings — both inside non-SGR content and as arguments to
// 256-color FG codes like "38;5;40". Splitting on ";" without grammar
// knowledge would corrupt those.
//
// If from == to the input is returned unchanged.
func substituteBgSGR(s, from, to string) string {
	if from == "" || from == to {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "\x1b[")
		if start < 0 {
			b.WriteString(s[i:])
			break
		}
		// Copy plain text before the escape verbatim.
		b.WriteString(s[i : i+start])
		seqStart := i + start
		// Find the terminating 'm'. SGR parameters are digits and ';';
		// anything else (e.g. another '\x1b') means this isn't a well-formed
		// SGR sequence and we bail back to plain copy.
		j := seqStart + 2
		for j < len(s) && s[j] != 'm' {
			c := s[j]
			if c != ';' && (c < '0' || c > '9') {
				break
			}
			j++
		}
		if j >= len(s) || s[j] != 'm' {
			// Not an SGR; copy "\x1b[" and continue scanning past it.
			b.WriteString("\x1b[")
			i = seqStart + 2
			continue
		}
		// s[seqStart+2:j] is the param list, s[j] == 'm'.
		params := splitSGRParams(s[seqStart+2 : j])
		out := make([]string, 0, len(params))
		for _, p := range params {
			if p.isBg && p.text == from {
				out = append(out, to)
			} else {
				out = append(out, p.text)
			}
		}
		b.WriteString("\x1b[")
		b.WriteString(strings.Join(out, ";"))
		b.WriteString("m")
		i = j + 1
	}
	return b.String()
}

// sgrParam is one logical SGR parameter. For extended-color groups
// (38;5;N, 38;2;R;G;B, 48;5;N, 48;2;R;G;B) text contains the joined
// form (e.g. "48;5;40" or "48;2;26;26;46") and isBg is true for any
// bg variant. For single parameters text is just the number (e.g.
// "40", "1", "31") and isBg is true only when the number is in the
// basic bg range (40–47, 100–107).
type sgrParam struct {
	text string
	isBg bool
}

// splitSGRParams tokenizes an SGR parameter list with awareness of
// extended-color sub-sequences so a "40" appearing as an argument to
// "38;5" is not mistaken for a standalone bg-black token.
func splitSGRParams(params string) []sgrParam {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]sgrParam, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		switch p {
		case "38", "48":
			isBg := p == "48"
			// Look ahead for "5;N" or "2;R;G;B".
			if i+1 < len(parts) {
				switch parts[i+1] {
				case "5":
					if i+2 < len(parts) {
						out = append(out, sgrParam{
							text: strings.Join(parts[i:i+3], ";"),
							isBg: isBg,
						})
						i += 2
						continue
					}
				case "2":
					if i+4 < len(parts) {
						out = append(out, sgrParam{
							text: strings.Join(parts[i:i+5], ";"),
							isBg: isBg,
						})
						i += 4
						continue
					}
				}
			}
			// Malformed — treat as a bare param so we don't drop data.
			out = append(out, sgrParam{text: p, isBg: false})
		default:
			out = append(out, sgrParam{
				text: p,
				isBg: isBasicBgParam(p),
			})
		}
	}
	return out
}

// isBasicBgParam reports whether s is a single SGR parameter representing
// a basic 16-color background: "40"–"47" or "100"–"107".
func isBasicBgParam(s string) bool {
	switch s {
	case "40", "41", "42", "43", "44", "45", "46", "47",
		"100", "101", "102", "103", "104", "105", "106", "107":
		return true
	}
	return false
}
```

- [ ] **Step 2.4: Run the test to verify it passes**

Run:
```bash
go test ./internal/ui/messages/ -run 'TestSubstituteBgSGR' -v
```

Expected: all 9 sub-tests PASS.

- [ ] **Step 2.5: Run full package tests**

Run:
```bash
go test ./internal/ui/messages/
```

Expected: PASS.

- [ ] **Step 2.6: Commit**

```bash
git add internal/ui/messages/render.go internal/ui/messages/render_test.go
git commit -m "feat(render): grammar-aware SGR bg-param substitution helper

substituteBgSGR tokenizes SGR parameter lists with awareness of
extended-color sub-sequences (38;5;N, 38;2;R;G;B, 48;5;N, 48;2;R;G;B)
and substitutes only complete bg tokens. This makes safe substitution
of short ANSI bg params like \"40\" possible without corrupting
collateral digits in content or 256-color sub-arguments.

Prep for #35 ANSI-palette themes."
```

---

## Task 3: Wire `substituteBgSGR` into `RepaintBgToSelectionTint`

**Files:**
- Modify: `internal/ui/messages/render.go:327-334`
- Test: `internal/ui/messages/render_test.go`

**Why:** With the helper in place, the selection-tint repainter can safely substitute even short ANSI params.

- [ ] **Step 3.1: Write an end-to-end failing test**

Append to `internal/ui/messages/render_test.go`:

```go
// TestRepaintBgToSelectionTintWithANSITheme exercises the integration
// of substituteBgSGR via RepaintBgToSelectionTint when the theme uses
// an ANSI 16 background. We apply ansi-dark so styles.Background is
// ansi.BasicColor(0) → BgANSI() == "\x1b[40m". The repaint must
// substitute the bundled "40" param without corrupting either literal
// content or 256-color sub-arguments.
func TestRepaintBgToSelectionTintWithANSITheme(t *testing.T) {
	// Apply ansi-dark; restore dark afterward.
	styles.Apply("ansi-dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	if BgANSI() != "\x1b[40m" {
		t.Fatalf("precondition: expected BgANSI() == \"\\x1b[40m\", got %q", BgANSI())
	}

	// Build a synthetic rendered string mixing: plain text containing
	// the digit "40", a bundled SGR with bg 40, and a 256-color FG that
	// includes "40" as its index (must NOT be substituted).
	in := "see line 40\x1b[1;31;40mbold red on black\x1b[m and \x1b[38;5;40mfg256\x1b[m"
	out := RepaintBgToSelectionTint(in, true)

	// "line 40" plain digits must survive.
	if !strings.Contains(out, "see line 40") {
		t.Errorf("plain digit run was corrupted: %q", out)
	}
	// The bundled bg "40" must be replaced.
	if strings.Contains(out, "\x1b[1;31;40m") {
		t.Errorf("expected bundled bg 40 to be substituted, got %q", out)
	}
	// The 256-color FG with index 40 must be intact.
	if !strings.Contains(out, "\x1b[38;5;40m") {
		t.Errorf("expected 256-color FG index 40 to remain intact, got %q", out)
	}
}

// TestRepaintBgToSelectionTintBackwardCompat asserts no behavior change
// for truecolor themes — substituting a long, unique RGB param.
func TestRepaintBgToSelectionTintBackwardCompat(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	bg := BgANSI()
	// We don't know the exact params; just verify a string carrying
	// that exact bg escape gets substituted and a string with no bg
	// escape passes through unchanged.
	withBg := "prefix" + bg + "tinted\x1b[m suffix"
	got := RepaintBgToSelectionTint(withBg, true)
	if got == withBg {
		t.Errorf("expected substitution to occur for dark theme bg")
	}
	noBg := "no escape here"
	if got := RepaintBgToSelectionTint(noBg, true); got != noBg {
		t.Errorf("expected pass-through for string with no bg escape, got %q", got)
	}
}
```

Make sure these imports are present at the top of `render_test.go`:

```go
import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)
```

- [ ] **Step 3.2: Run the tests to verify they fail**

Run:
```bash
go test ./internal/ui/messages/ -run 'TestRepaintBgToSelectionTintWithANSITheme|TestRepaintBgToSelectionTintBackwardCompat' -v
```

Expected outcomes:
- `TestRepaintBgToSelectionTintWithANSITheme` FAILS — either `BgANSI()` precondition fails because `ansi-dark` theme doesn't exist yet (we'll add it in Task 4) **OR** the substitution corrupts the 256-color sub-argument. *Note:* this test depends on `ansi-dark` being registered. If you're running Task 3 before Task 4, this test will fail at the precondition step. That's expected — re-run after Task 4 lands and it will exercise the integration.
- `TestRepaintBgToSelectionTintBackwardCompat` likely PASSES already since `strings.ReplaceAll` works fine for long truecolor params.

If `TestRepaintBgToSelectionTintWithANSITheme` is blocked by the missing theme, **skip Step 3.4's verification of this specific test for now and revisit after Task 4**. Steps 3.3 (the implementation) can still be done now.

- [ ] **Step 3.3: Replace `RepaintBgToSelectionTint`**

In `internal/ui/messages/render.go:327-334`, replace the body:

```go
func RepaintBgToSelectionTint(s string, focused bool) string {
	from := bgSGRParams(BgANSI())
	to := bgSGRParams(SelectionTintBgANSI(focused))
	if from == "" || from == to {
		return s
	}
	return substituteBgSGR(s, from, to)
}
```

The signature, exported name, and high-level contract are unchanged. Only the substitution mechanism inside the function changes.

- [ ] **Step 3.4: Run backward-compat test to verify behavior preserved**

Run:
```bash
go test ./internal/ui/messages/ -run 'TestRepaintBgToSelectionTintBackwardCompat' -v
```

Expected: PASS.

- [ ] **Step 3.5: Run full package tests**

Run:
```bash
go test ./internal/ui/messages/
```

Expected: PASS. `TestRepaintBgToSelectionTintWithANSITheme` will still fail until Task 4 registers `ansi-dark`. Note this as a known gap to revisit after Task 4.

- [ ] **Step 3.6: Commit**

```bash
git add internal/ui/messages/render.go internal/ui/messages/render_test.go
git commit -m "refactor(render): use grammar-aware substitution in RepaintBgToSelectionTint

Switch from strings.ReplaceAll on the bg-param substring to
substituteBgSGR, which understands SGR token grammar. Behaviorally
identical for truecolor themes; required for safe substitution when
the theme uses short ANSI 16 bg params.

Prep for #35 ANSI-palette themes."
```

---

## Task 4: Add `ansi-dark` theme

**Files:**
- Modify: `internal/ui/styles/themes.go`
- Test: `internal/ui/styles/themes_test.go`

- [ ] **Step 4.1: Write the failing test**

Append to `internal/ui/styles/themes_test.go`:

```go
// TestANSIDarkThemeRegistered asserts the ansi-dark theme is present
// in the theme switcher and that every color field is populated with
// a value that resolves to ansi.BasicColor — confirming the theme
// will inherit the user's terminal palette rather than emit truecolor.
func TestANSIDarkThemeRegistered(t *testing.T) {
	names := ThemeNames()
	found := false
	for _, n := range names {
		if n == "ANSI Dark" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected \"ANSI Dark\" in ThemeNames, got %v", names)
	}

	c := lookupTheme("ansi-dark")
	required := map[string]string{
		"Primary":     c.Primary,
		"Accent":      c.Accent,
		"Warning":     c.Warning,
		"Error":       c.Error,
		"Background":  c.Background,
		"Surface":     c.Surface,
		"SurfaceDark": c.SurfaceDark,
		"Text":        c.Text,
		"TextMuted":   c.TextMuted,
		"Border":      c.Border,
	}
	for name, val := range required {
		if val == "" {
			t.Errorf("ansi-dark.%s is empty", name)
			continue
		}
		col := lipgloss.Color(val)
		if _, ok := col.(ansi.BasicColor); !ok {
			t.Errorf("ansi-dark.%s = %q resolves to %T, want ansi.BasicColor",
				name, val, col)
		}
	}
}
```

Add imports to `themes_test.go` if not already present:

```go
import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
)
```

- [ ] **Step 4.2: Run the test to verify it fails**

Run:
```bash
go test ./internal/ui/styles/ -run TestANSIDarkThemeRegistered -v
```

Expected: FAIL — theme not in `ThemeNames()`.

- [ ] **Step 4.3: Add the theme entry**

In `internal/ui/styles/themes.go`, add this entry to the `builtinThemes` map. The existing themes are grouped by family rather than strictly alphabetical, so just pick a sensible location — at the end of the map literal is fine. The key is `"ansi-dark"` (lowercase, hyphenated). The struct literal uses ANSI 16 number strings:

```go
"ansi-dark": {
	Name: "ANSI Dark",
	Colors: ThemeColors{
		// All values are ANSI 16 color numbers ("0"–"15"). They are
		// passed through lipgloss.Color() which returns ansi.BasicColor,
		// so rendering uses native 16-color SGR escapes and inherits the
		// user's terminal palette.
		Primary:     "4",  // blue
		Accent:      "6",  // cyan
		Warning:     "3",  // yellow
		Error:       "1",  // red
		Background:  "0",  // black
		Surface:     "8",  // bright black (slightly lifted panel bg)
		SurfaceDark: "0",  // black (matches background)
		Text:        "15", // bright white
		TextMuted:   "8",  // bright black
		Border:      "8",  // bright black
	},
},
```

- [ ] **Step 4.4: Run the test to verify it passes**

Run:
```bash
go test ./internal/ui/styles/ -run TestANSIDarkThemeRegistered -v
```

Expected: PASS.

- [ ] **Step 4.5: Re-run the Task 3 integration test**

Run:
```bash
go test ./internal/ui/messages/ -run TestRepaintBgToSelectionTintWithANSITheme -v
```

Expected: PASS. The integration test from Task 3 now has its prerequisite theme and exercises the full ANSI substitution path.

- [ ] **Step 4.6: Run all tests in the affected packages**

Run:
```bash
go test ./internal/ui/styles/ ./internal/ui/messages/
```

Expected: PASS.

- [ ] **Step 4.7: Commit**

```bash
git add internal/ui/styles/themes.go internal/ui/styles/themes_test.go
git commit -m "feat(themes): add ansi-dark theme

Uses ANSI 16 color numbers (\"0\"–\"15\") for every theme color so
rendering inherits the user's terminal palette via native 16-color
SGR escapes. Resolves the dark half of #35."
```

---

## Task 5: Add `ansi-light` theme

**Files:**
- Modify: `internal/ui/styles/themes.go`
- Test: `internal/ui/styles/themes_test.go`

- [ ] **Step 5.1: Write the failing test**

Append to `internal/ui/styles/themes_test.go`:

```go
// TestANSILightThemeRegistered: mirror of TestANSIDarkThemeRegistered
// for the light variant.
func TestANSILightThemeRegistered(t *testing.T) {
	names := ThemeNames()
	found := false
	for _, n := range names {
		if n == "ANSI Light" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected \"ANSI Light\" in ThemeNames, got %v", names)
	}

	c := lookupTheme("ansi-light")
	required := map[string]string{
		"Primary":     c.Primary,
		"Accent":      c.Accent,
		"Warning":     c.Warning,
		"Error":       c.Error,
		"Background":  c.Background,
		"Surface":     c.Surface,
		"SurfaceDark": c.SurfaceDark,
		"Text":        c.Text,
		"TextMuted":   c.TextMuted,
		"Border":      c.Border,
	}
	for name, val := range required {
		if val == "" {
			t.Errorf("ansi-light.%s is empty", name)
			continue
		}
		col := lipgloss.Color(val)
		if _, ok := col.(ansi.BasicColor); !ok {
			t.Errorf("ansi-light.%s = %q resolves to %T, want ansi.BasicColor",
				name, val, col)
		}
	}
}
```

- [ ] **Step 5.2: Run the test to verify it fails**

Run:
```bash
go test ./internal/ui/styles/ -run TestANSILightThemeRegistered -v
```

Expected: FAIL — theme not registered.

- [ ] **Step 5.3: Add the theme entry**

In `internal/ui/styles/themes.go`, add immediately after the `ansi-dark` entry:

```go
"ansi-light": {
	Name: "ANSI Light",
	Colors: ThemeColors{
		// Same ANSI-16-only constraint as ansi-dark; values chosen for
		// readability on light terminal backgrounds.
		Primary:     "4",  // blue
		Accent:      "6",  // cyan
		Warning:     "3",  // yellow
		Error:       "1",  // red
		Background:  "15", // bright white
		Surface:     "7",  // white (slightly muted panel bg)
		SurfaceDark: "7",  // white
		Text:        "0",  // black
		TextMuted:   "8",  // bright black (grey)
		Border:      "8",  // bright black
	},
},
```

- [ ] **Step 5.4: Run the test to verify it passes**

Run:
```bash
go test ./internal/ui/styles/ -run TestANSILightThemeRegistered -v
```

Expected: PASS.

- [ ] **Step 5.5: Run the full project test suite**

Run:
```bash
go test ./...
```

Expected: PASS. Watch for any unrelated tests that might brittle-match against the theme count or theme list.

- [ ] **Step 5.6: Build the binary to confirm no compile errors**

Run:
```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 5.7: Commit**

```bash
git add internal/ui/styles/themes.go internal/ui/styles/themes_test.go
git commit -m "feat(themes): add ansi-light theme

Light-terminal counterpart to ansi-dark. Uses ANSI 16 color numbers
so the user's terminal palette is honored. Resolves #35."
```

---

## Task 6: Documentation — `wiki/Configuration.md`

**Files:**
- Modify: `wiki/Configuration.md`

- [ ] **Step 6.1: Read the current themes section**

Run:
```bash
grep -n "theme" wiki/Configuration.md | head -20
```

Identify the section that lists built-in themes or describes theme selection. The new content should slot in there.

- [ ] **Step 6.2: Add the ANSI-themes subsection**

Add the following block after the existing list of built-in themes (find the right anchor in the file; if there's a built-in themes list, append; if not, add after the line that introduces the `[appearance] theme = "..."` config key):

```markdown
### Terminal-palette themes (`ansi-dark`, `ansi-light`)

Two built-in themes use ANSI 16 color codes exclusively rather than
fixed RGB values. They inherit the user's terminal color palette, so
changing your terminal colorscheme (light/dark, solarized,
accessibility palettes, etc.) immediately changes slk's UI colors to
match.

```toml
[appearance]
theme = "ansi-dark"   # or "ansi-light"
```

Pick the variant whose background matches your terminal's background.

**Trade-off:** selection-row highlights and compose-input tints are
still computed as RGB approximations, so the tint regions of those
elements use truecolor rather than your palette. The rest of the UI
honors the palette.
```

- [ ] **Step 6.3: Sanity-check the diff**

Run:
```bash
git diff wiki/Configuration.md
```

Confirm the addition is in a sensible place, headers nest correctly under the existing structure, and the fenced code block doesn't accidentally break a parent fenced block.

- [ ] **Step 6.4: Commit**

```bash
git add wiki/Configuration.md
git commit -m "docs(wiki): document ansi-dark and ansi-light themes (#35)"
```

---

## Task 7: Documentation — `README.md` theme count + final verification

**Files:**
- Modify: `README.md:17` and `README.md:30`

The README currently says "35+ built-in themes" at line 17 and "12 themes" at line 30 (the 12 figure is stale — the codebase currently ships 34 builtins; this PR brings it to 36). Update both to a single accurate count.

- [ ] **Step 7.1: Verify current theme count programmatically**

Run:
```bash
go test ./internal/ui/styles/ -run 'TestANSIDarkThemeRegistered|TestANSILightThemeRegistered' -v
```

Then count themes:
```bash
grep -c '^\s"[a-z]' internal/ui/styles/themes.go
```

Confirm the count is 36. If the count differs from expectations, recount manually before editing the README — do not blindly trust a stale number.

- [ ] **Step 7.2: Update `README.md:17`**

Find the line: `- **Pretty.** 35+ built-in themes, lipgloss-styled panels, ...`

Replace `35+` with `36` (or whatever the verified count is). Same line, no other changes.

- [ ] **Step 7.3: Update `README.md:30`**

Find the line: `- 12 themes + drop-in custom themes, live theme switcher`

Replace `12 themes` with `36 themes` (or the verified count).

- [ ] **Step 7.4: Final full build + test**

Run:
```bash
go build ./... && go test ./...
```

Expected: clean build, all tests pass.

- [ ] **Step 7.5: Manual verification (palette inheritance)**

This step requires a terminal and a human:

1. Build and run slk: `go run ./cmd/slk`.
2. Open the theme switcher: `Ctrl+y`.
3. Select `ANSI Dark`.
4. Note the colors of: sidebar borders (border), unread counts (primary or accent), error messages (error), warnings (warning).
5. **Without quitting slk**, change your terminal emulator's color scheme to a visibly different palette (e.g., switch iTerm/Alacritty/kitty from default to Solarized or vice versa).
6. Trigger a re-render in slk (resize the window, switch channels, etc.).
7. The colors noted in step 4 should now reflect the new palette — e.g., "red" is now solarized-red, not standard #FF0000 red.

If colors did NOT change, the palette-inheritance path is broken — investigate `bgANSIFor`/`fgANSIFor` and confirm the new themes' colors resolve to `ansi.BasicColor` at runtime (a debug print in `styles.Apply` will confirm).

Also visually inspect: selection highlight (`j`/`k` to move cursor) — the tint should look reasonable on the ANSI background. Note that the tint itself is a truecolor RGB; it doesn't need to inherit palette.

- [ ] **Step 7.6: Commit**

```bash
git add README.md
git commit -m "docs(readme): bump theme count for ansi-dark/ansi-light (#35)"
```

- [ ] **Step 7.7: Inspect commit history before opening PR**

Run:
```bash
git log --oneline -10
```

Confirm the commit sequence is clean and each commit message accurately reflects its diff. If anything looks off, rebase interactively before opening the PR.

---

## Plan self-review summary

- **Spec coverage:** All four spec sections (Required code changes 1-3, Tests, Documentation) map to Tasks 1-7. The optional `mixColors` ANSI-awareness and single-`terminal` theme were explicitly listed as Out of scope in the spec and are correctly absent from the plan.
- **Test coverage:**
  - Task 1: `TestBgANSIForBasicColor`, `TestFgANSIForBasicColor`, `TestBgFgANSIForIndexedColor`, `TestBgFgANSIForRGBA`.
  - Task 2: `TestSubstituteBgSGR` with 9 sub-cases including all the collision scenarios from the spec.
  - Task 3: `TestRepaintBgToSelectionTintWithANSITheme` (integration), `TestRepaintBgToSelectionTintBackwardCompat` (regression).
  - Tasks 4-5: `TestANSIDarkThemeRegistered`, `TestANSILightThemeRegistered`.
- **Type/name consistency:** `substituteBgSGR` referenced in Task 3 implementation matches Task 2 definition. `sgrParam`, `splitSGRParams`, `isBasicBgParam` are defined once in Task 2. Theme keys `"ansi-dark"` / `"ansi-light"` and display names `"ANSI Dark"` / `"ANSI Light"` used consistently across Tasks 4, 5, 6, 7.
- **Cross-task dependency:** `TestRepaintBgToSelectionTintWithANSITheme` (Task 3) depends on the `ansi-dark` theme registered in Task 4. This is called out explicitly in Step 3.2 and re-verified in Step 4.5.
