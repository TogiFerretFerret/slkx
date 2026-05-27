# Phase 1: Config

> Index: `00-overview.md`. Next: `02-url-building.md`.

**Goal:** Add `Appearance.EmojiImages` (`"on"` | `"off"`, default `"on"`) and `Appearance.EmojiCells` (`int`, default `2`) to the config schema, with defaults, override parsing, and validation/clamping. No behavior change anywhere — these knobs are unread by other code at this point.

**Files:**
- Modify: `internal/config/config.go` (struct, `Default()`, `Load()` clamping)
- Modify: `internal/config/config_test.go` (defaults, override, clamping)

---

### Task 1.1: Failing test — default values

**Files:**
- Modify: `internal/config/config_test.go` (append to `TestConfig_Defaults` or add a new test)

- [ ] **Step 1: Write the failing test**

Append the following to `internal/config/config_test.go` at the end of `TestConfig_Defaults` (which lives around line 100-150; identify it by the `MaxImageCacheMB != 200` check):

```go
	if cfg.Appearance.EmojiImages != "on" {
		t.Errorf("expected default emoji_images 'on', got %q", cfg.Appearance.EmojiImages)
	}
	if cfg.Appearance.EmojiCells != 2 {
		t.Errorf("expected default emoji_cells 2, got %d", cfg.Appearance.EmojiCells)
	}

	// Default() directly should also yield these values.
	if d.Appearance.EmojiImages != "on" {
		t.Errorf("Default() emoji_images = %q, want 'on'", d.Appearance.EmojiImages)
	}
	if d.Appearance.EmojiCells != 2 {
		t.Errorf("Default() emoji_cells = %d, want 2", d.Appearance.EmojiCells)
	}
```

(`d` is the existing `d := Default()` local in `TestConfig_Defaults`; the second pair of asserts goes inside the block that already calls `Default()`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestConfig_Defaults -v`
Expected: FAIL with "EmojiImages undefined" (compile error — the field doesn't exist yet).

---

### Task 1.2: Implement the fields and defaults

**Files:**
- Modify: `internal/config/config.go` (struct + `Default()`)

- [ ] **Step 1: Add the fields to the `Appearance` struct**

In `internal/config/config.go`, locate the `Appearance` struct (around line 42-59). Append two fields after `MouseWheelLines`:

```go
type Appearance struct {
	Theme           string `toml:"theme"`
	TimestampFormat string `toml:"timestamp_format"`
	ShowAvatars     bool   `toml:"show_avatars"`
	// ImageProtocol controls how inline images are rendered.
	// One of: "auto", "kitty", "sixel", "halfblock", "off".
	ImageProtocol string `toml:"image_protocol"`
	// MaxImageRows caps the height of inline images in terminal rows.
	MaxImageRows int `toml:"max_image_rows"`
	// MaxImageCols caps the width of inline images in terminal columns.
	// If 0 or unset, defaults to 60. The image is also bounded by the
	// available message-pane width when narrower.
	MaxImageCols int `toml:"max_image_cols"`
	// MouseWheelLines controls how many lines the viewport scrolls per
	// mouse-wheel notch. Higher = faster scroll. Defaults to 3 (typical
	// terminal behavior). Clamped to >= 1 at load time.
	MouseWheelLines int `toml:"mouse_wheel_lines"`
	// EmojiImages controls whether emoji are rendered as PNG images
	// (from Slack's CDN) via the kitty graphics protocol. One of:
	// "on" (default) or "off". On non-kitty terminals this is silently
	// treated as "off"; see internal/emoji/place.go.
	EmojiImages string `toml:"emoji_images"`
	// EmojiCells is the terminal-cell footprint reserved for each
	// emoji image (cells wide x 1 row tall). 2 (default) matches the
	// East-Asian-Wide convention; 1 is an escape hatch if 2 looks too
	// large in a given font. Clamped to {1, 2} at load time.
	EmojiCells int `toml:"emoji_cells"`
}
```

- [ ] **Step 2: Set defaults in `Default()`**

Locate `Default()` (around line 127). Inside the `Appearance: Appearance{...}` literal, add the two new fields after `MouseWheelLines: 3`:

```go
	return Config{
		Appearance: Appearance{
			Theme:           "nord",
			TimestampFormat: "3:04 PM",
			ImageProtocol:   "auto",
			MaxImageRows:    20,
			MaxImageCols:    60,
			MouseWheelLines: 3,
			EmojiImages:     "on",
			EmojiCells:      2,
		},
		// ... rest unchanged
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestConfig_Defaults -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add emoji_images and emoji_cells appearance fields"
```

---

### Task 1.3: Failing test — TOML override

**Files:**
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Add a new test function after `TestConfig_ImageOverrides`:

```go
func TestConfig_EmojiOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[appearance]
emoji_images = "off"
emoji_cells = 1
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.EmojiImages != "off" {
		t.Errorf("expected emoji_images 'off', got %q", cfg.Appearance.EmojiImages)
	}
	if cfg.Appearance.EmojiCells != 1 {
		t.Errorf("expected emoji_cells 1, got %d", cfg.Appearance.EmojiCells)
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestConfig_EmojiOverrides -v`
Expected: PASS (the TOML library reads the fields into the already-defined struct fields with no additional code).

- [ ] **Step 3: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test(config): cover emoji_images and emoji_cells overrides"
```

---

### Task 1.4: Failing test — clamping invalid values

**Files:**
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Add a new test function after `TestConfig_EmojiOverrides`:

```go
func TestConfig_EmojiClamp(t *testing.T) {
	// emoji_cells outside {1, 2} clamps to 2.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\nemoji_cells = 5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.EmojiCells != 2 {
		t.Errorf("emoji_cells=5 should clamp to 2, got %d", cfg.Appearance.EmojiCells)
	}

	// emoji_cells = 0 (unset after partial [appearance]) clamps to 2.
	path2 := filepath.Join(dir, "config2.toml")
	if err := os.WriteFile(path2, []byte("[appearance]\ntheme = \"nord\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(path2)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Appearance.EmojiCells != 2 {
		t.Errorf("unset emoji_cells should default to 2, got %d", cfg2.Appearance.EmojiCells)
	}
	if cfg2.Appearance.EmojiImages != "on" {
		t.Errorf("unset emoji_images should default to 'on', got %q", cfg2.Appearance.EmojiImages)
	}

	// emoji_images with an unrecognized value clamps to "on".
	path3 := filepath.Join(dir, "config3.toml")
	if err := os.WriteFile(path3, []byte("[appearance]\nemoji_images = \"weird\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg3, err := Load(path3)
	if err != nil {
		t.Fatal(err)
	}
	if cfg3.Appearance.EmojiImages != "on" {
		t.Errorf("unrecognized emoji_images should clamp to 'on', got %q", cfg3.Appearance.EmojiImages)
	}

	// emoji_cells = 1 explicit value passes through (valid).
	path4 := filepath.Join(dir, "config4.toml")
	if err := os.WriteFile(path4, []byte("[appearance]\nemoji_cells = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg4, err := Load(path4)
	if err != nil {
		t.Fatal(err)
	}
	if cfg4.Appearance.EmojiCells != 1 {
		t.Errorf("emoji_cells=1 should pass through, got %d", cfg4.Appearance.EmojiCells)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestConfig_EmojiClamp -v`
Expected: FAIL — the first sub-case (`emoji_cells = 5`) returns 5 since no clamping is applied yet.

---

### Task 1.5: Implement clamping in Load()

**Files:**
- Modify: `internal/config/config.go` (`Load()` function)

- [ ] **Step 1: Add clamping after the existing MouseWheelLines clamp**

In `internal/config/config.go`, locate the `Load()` function (around line 160). After the existing `MouseWheelLines` clamp (around line 184-186), add:

```go
	// Clamp MouseWheelLines: 0 (unset, after a user supplied a partial
	// [appearance] block without this key) and negative values both fall
	// back to the default. >= 1 to guarantee scroll progress per notch.
	if cfg.Appearance.MouseWheelLines < 1 {
		cfg.Appearance.MouseWheelLines = 3
	}

	// Clamp EmojiCells to the documented set {1, 2}. 0 (unset after a
	// partial [appearance] block) and any other value fall back to 2.
	if cfg.Appearance.EmojiCells != 1 && cfg.Appearance.EmojiCells != 2 {
		cfg.Appearance.EmojiCells = 2
	}

	// Clamp EmojiImages to the documented set {"on", "off"}. Empty
	// (unset) and any unrecognized value fall back to "on".
	if cfg.Appearance.EmojiImages != "on" && cfg.Appearance.EmojiImages != "off" {
		cfg.Appearance.EmojiImages = "on"
	}

	return cfg, nil
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestConfig_EmojiClamp -v`
Expected: PASS.

- [ ] **Step 3: Run the full config test suite**

Run: `go test ./internal/config/ -v`
Expected: all PASS. In particular `TestConfig_Defaults`, `TestConfig_EmojiOverrides`, `TestConfig_EmojiClamp`, and the existing `TestConfig_MouseWheelLines`, `TestConfig_ImageOverrides` all green.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): clamp emoji_cells to {1,2} and emoji_images to {on,off}"
```

---

### Task 1.6: Final phase check

- [ ] **Step 1: Build the full project**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: no new failures introduced by Phase 1.

Phase 1 complete. The config knobs exist, parse correctly, and validate. No other code reads them yet — the feature is dark. Continue to `02-url-building.md`.
