package statusbar

import (
	"strings"
	"testing"
)

func TestSetCommandLine_RendersPromptInView(t *testing.T) {
	m := New()
	m.SetCommandLine(":vsp")
	out := m.View(80)
	if !strings.Contains(out, ":vsp") {
		t.Fatalf("expected view to contain %q, got:\n%s", ":vsp", out)
	}
}

func TestSetCommandLine_HidesChannelSegmentWhileActive(t *testing.T) {
	m := New()
	m.SetChannel("general")
	m.SetCommandLine(":ws")
	if out := m.View(80); strings.Contains(out, "general") {
		t.Fatalf("channel segment should be hidden while command line active:\n%s", out)
	}
	m.SetCommandLine("")
	if out := m.View(80); !strings.Contains(out, "general") {
		t.Fatalf("channel segment should return after clearing command line:\n%s", out)
	}
}

func TestSetCommandLine_BumpsVersion(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.SetCommandLine(":q")
	if m.Version() == v0 {
		t.Fatal("SetCommandLine should bump version")
	}
	v1 := m.Version()
	m.SetCommandLine(":q") // no change → no bump
	if m.Version() != v1 {
		t.Fatal("identical SetCommandLine should not bump version")
	}
}
