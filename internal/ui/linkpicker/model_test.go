package linkpicker

import "testing"

func items3() []Item {
	return []Item{
		{URL: "https://a.example/1", Label: "first"},
		{URL: "https://b.example/2"},
		{URL: "https://myteam.slack.com/archives/C1/p1700000000000001", InApp: true},
	}
}

func TestOpenAndNavigate(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Fatal("visible before Open")
	}
	m.Open(items3())
	if !m.IsVisible() {
		t.Fatal("not visible after Open")
	}
	if _, chosen := m.HandleKey("j"); chosen {
		t.Error("j must not choose")
	}
	m.HandleKey("j")
	item, chosen := m.HandleKey("enter")
	if !chosen {
		t.Fatal("enter should choose")
	}
	if item.URL != items3()[2].URL {
		t.Errorf("chose %q", item.URL)
	}
	if m.IsVisible() {
		t.Error("should close after choose")
	}
}

func TestNavigationBounds(t *testing.T) {
	m := New()
	m.Open(items3())
	m.HandleKey("k") // at top; no-op
	item, chosen := m.HandleKey("enter")
	if !chosen || item.URL != items3()[0].URL {
		t.Errorf("chose %+v chosen=%v", item, chosen)
	}
	m.Open(items3())
	for i := 0; i < 10; i++ {
		m.HandleKey("j") // clamps at bottom
	}
	item, _ = m.HandleKey("enter")
	if item.URL != items3()[2].URL {
		t.Errorf("chose %q", item.URL)
	}
}

func TestEscCloses(t *testing.T) {
	m := New()
	m.Open(items3())
	if _, chosen := m.HandleKey("esc"); chosen {
		t.Error("esc must not choose")
	}
	if m.IsVisible() {
		t.Error("esc should close")
	}
}

func TestEnterOnEmptyIsNoop(t *testing.T) {
	m := New()
	m.Open(nil)
	if _, chosen := m.HandleKey("enter"); chosen {
		t.Error("enter on empty list must not choose")
	}
}
