package help

import (
	"strings"
	"testing"
)

func TestFooterRendersWhenSet(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.SetFooter("slk dev - footer line")
	m.Open()

	out := m.ViewOverlay(100, 40, "")
	if !strings.Contains(out, "slk dev - footer line") {
		t.Errorf("expected footer text in rendered modal, got:\n%s", out)
	}
}

func TestFooterAbsentWhenEmpty(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()

	out := m.ViewOverlay(100, 40, "")
	if strings.Contains(out, "Made with") {
		t.Errorf("did not expect a footer line when none set, got:\n%s", out)
	}
}
