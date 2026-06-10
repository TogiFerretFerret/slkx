package messages

import "testing"

func TestSelectByTS_Found(t *testing.T) {
	m := New(nil, "general")
	m.SetMessages([]MessageItem{
		{TS: "1.000001", Text: "a"},
		{TS: "2.000002", Text: "b"},
		{TS: "3.000003", Text: "c"},
	})
	if !m.SelectByTS("2.000002") {
		t.Fatal("expected SelectByTS to return true")
	}
	if m.SelectedIndex() != 1 {
		t.Errorf("SelectedIndex = %d, want 1", m.SelectedIndex())
	}
	sel, ok := m.SelectedMessage()
	if !ok || sel.TS != "2.000002" {
		t.Errorf("SelectedMessage = %+v ok=%v", sel, ok)
	}
}

func TestSelectByTS_NotFound(t *testing.T) {
	m := New(nil, "general")
	m.SetMessages([]MessageItem{{TS: "1.000001", Text: "a"}})
	if m.SelectByTS("9.999999") {
		t.Error("expected false for missing ts")
	}
	if m.SelectedIndex() != 0 {
		t.Errorf("selection moved: %d", m.SelectedIndex())
	}
	if m.SelectByTS("") {
		t.Error("expected false for empty ts")
	}
}
