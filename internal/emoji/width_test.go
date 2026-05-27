package emoji

import (
	"testing"
)

func TestWidthASCIIBypass(t *testing.T) {
	resetWidthMap()
	// Even with empty cache, ASCII should work via lipgloss fallback
	if got := Width("hello"); got != 5 {
		t.Errorf("Width(\"hello\") = %d, want 5", got)
	}
	if got := Width(""); got != 0 {
		t.Errorf("Width(\"\") = %d, want 0", got)
	}
	if got := Width("a"); got != 1 {
		t.Errorf("Width(\"a\") = %d, want 1", got)
	}
}

func TestWidthCacheHit(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"❤️": 1,
		"👍":  2,
	})

	if got := Width("❤️"); got != 1 {
		t.Errorf("Width(❤️) = %d, want 1 (cache hit)", got)
	}
	if got := Width("👍"); got != 2 {
		t.Errorf("Width(👍) = %d, want 2 (cache hit)", got)
	}
}

func TestWidthCacheMissFallback(t *testing.T) {
	resetWidthMap()
	// Empty cache; emoji not present → fall back to lipgloss
	got := Width("👍")
	if got < 1 || got > 2 {
		t.Errorf("Width(👍) fallback = %d, want 1 or 2", got)
	}
}

func TestWidthMixedContent(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"❤️": 1,
	})

	// "abc❤️def" → 3 + 1 + 3 = 7
	if got := Width("abc❤️def"); got != 7 {
		t.Errorf("Width(\"abc❤️def\") = %d, want 7", got)
	}
}

func TestWidthStripsANSI(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"👍": 2,
	})

	// Lipgloss-style ANSI-wrapped pill: red foreground + reset.
	// Visible content is "👍5" → emoji (2) + "5" (1) = 3.
	styled := "\x1b[31m👍5\x1b[0m"
	if got := Width(styled); got != 3 {
		t.Errorf("Width(%q) = %d, want 3 (ANSI must be stripped)", styled, got)
	}

	// True pill string with background+foreground+padding.
	// Visible content " 👍 5 " = space + emoji(2) + space + "5" + space = 6.
	pill := "\x1b[38;2;100;100;100m\x1b[48;2;26;46;26m 👍 5 \x1b[0m"
	if got := Width(pill); got != 6 {
		t.Errorf("Width(pill) = %d, want 6 (ANSI must be stripped)", got)
	}

	// Empty cache + ANSI: should fall through to lipgloss.Width on stripped content.
	resetWidthMap()
	if got := Width("\x1b[31mhello\x1b[0m"); got != 5 {
		t.Errorf("Width(ansi 'hello') = %d, want 5", got)
	}
}

func TestIsCalibrated(t *testing.T) {
	resetWidthMap()
	if IsCalibrated() {
		t.Error("IsCalibrated should be false with empty map")
	}

	setWidthMap(map[string]int{"👍": 2})
	if !IsCalibrated() {
		t.Error("IsCalibrated should be true after setWidthMap")
	}
}

func TestWidth_ImageMode_KnownEmojiClusters(t *testing.T) {
	resetImageMode()
	resetWidthMap()
	t.Cleanup(func() {
		resetImageMode()
		resetWidthMap()
	})

	SetImageMode(true, 2)

	cases := []struct {
		name string
		in   string
		want int
	}{
		// Each single emoji reports the image-mode footprint (2 cells).
		{"single thumb", "\U0001F44D", 2},
		{"VS16 sequence", "\u2764\uFE0F", 2},
		{"ZWJ sequence", "\U0001F468\u200D\U0001F680", 2},
		{"regional indicator pair", "\U0001F1FA\U0001F1F8", 2},

		// Mixed text + emoji: emoji = 2, plus the ASCII run.
		{"text + emoji", "hi \U0001F44D", 5}, // "hi " (3) + emoji (2)
		{"emoji + text", "\U0001F44D hi", 5}, // emoji (2) + " hi" (3)

		// Two adjacent emoji.
		{"emoji + emoji", "\U0001F44D\u2764\uFE0F", 4},

		// Pure ASCII: probe-map / lipgloss path unchanged.
		{"ascii only", "hello", 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Width(c.in)
			if got != c.want {
				t.Errorf("Width(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestWidth_ImageMode_OneCellOverride(t *testing.T) {
	resetImageMode()
	resetWidthMap()
	t.Cleanup(func() {
		resetImageMode()
		resetWidthMap()
	})

	SetImageMode(true, 1)
	if got := Width("\U0001F44D"); got != 1 {
		t.Errorf("Width(thumb) with cells=1 = %d, want 1", got)
	}
	if got := Width("hi \U0001F44D"); got != 4 {
		t.Errorf("Width('hi ' + thumb) with cells=1 = %d, want 4 ('hi ' + 1)", got)
	}
}

func TestWidth_ImageMode_InactiveFallsThrough(t *testing.T) {
	resetImageMode()
	resetWidthMap()
	t.Cleanup(func() {
		resetImageMode()
		resetWidthMap()
	})

	// Mode is inactive — should NOT force 2-cell width for emoji
	// clusters; behavior comes from probe map (empty here) or lipgloss
	// fallback. Confirm by checking ASCII unaffected and emoji uses the
	// non-image-mode path (which may be != 2, depending on lipgloss).
	if got := Width("hello"); got != 5 {
		t.Errorf("Width(ascii) with image-mode off = %d, want 5", got)
	}
	// The exact width for an emoji here is whatever lipgloss reports —
	// we only assert that ImageModeActive is false so the new branch
	// is not taken.
	if ImageModeActive() {
		t.Fatalf("ImageModeActive() = true after resetImageMode()")
	}
}
