package wintree

import (
	"errors"
	"testing"
)

var testBounds = Rect{X: 0, Y: 0, W: 180, H: 48}

func TestComputeRects_TilesExactly(t *testing.T) {
	// Build: vsplit (side-by-side), then split the right window
	// stacked. 3 windows; rects must tile bounds with no gaps or
	// overlaps and odd extents must be fully distributed.
	tr, a := New(Channel{ID: "C1"})
	bounds := Rect{X: 0, Y: 0, W: 121, H: 41} // odd on purpose
	b, err := tr.Split(a, SplitSideBySide, bounds)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tr.Split(b, SplitStacked, bounds)
	if err != nil {
		t.Fatal(err)
	}
	rects := tr.ComputeRects(bounds)
	if len(rects) != 3 {
		t.Fatalf("got %d rects, want 3", len(rects))
	}
	area := 0
	for id, r := range rects {
		if r.W < MinWidth || r.H < MinHeight {
			t.Fatalf("window %v rect %+v below minimums", id, r)
		}
		area += r.W * r.H
	}
	if area != bounds.W*bounds.H {
		t.Fatalf("rect areas sum to %d, want %d (gap or overlap)", area, bounds.W*bounds.H)
	}
	// a is the left column: full height, x=0.
	if rects[a].X != 0 || rects[a].H != bounds.H {
		t.Fatalf("left window rect = %+v", rects[a])
	}
	// b above c in the right column, same x/width.
	if rects[b].X != rects[c].X || rects[b].W != rects[c].W {
		t.Fatalf("right column rects misaligned: b=%+v c=%+v", rects[b], rects[c])
	}
	if rects[b].Y+rects[b].H != rects[c].Y {
		t.Fatalf("b and c not vertically adjacent: b=%+v c=%+v", rects[b], rects[c])
	}
}

func TestComputeRects_OffsetBoundsTile(t *testing.T) {
	// Non-zero-origin bounds: child offsets must accumulate from the
	// bounds origin, and tiling must still be exact.
	tr, a := New(Channel{})
	bounds := Rect{X: 5, Y: 3, W: 120, H: 40}
	b, err := tr.Split(a, SplitSideBySide, bounds)
	if err != nil {
		t.Fatal(err)
	}
	rects := tr.ComputeRects(bounds)
	if rects[a].X != 5 || rects[a].Y != 3 {
		t.Fatalf("first window must start at bounds origin, got %+v", rects[a])
	}
	if rects[a].X+rects[a].W != rects[b].X {
		t.Fatalf("windows not horizontally adjacent: a=%+v b=%+v", rects[a], rects[b])
	}
	if rects[b].X+rects[b].W != bounds.X+bounds.W {
		t.Fatalf("right window must end at bounds edge: %+v", rects[b])
	}
}

func TestLayout_TreeShapeMatchesRects(t *testing.T) {
	tr, a := New(Channel{})
	bounds := Rect{X: 0, Y: 0, W: 120, H: 40}
	b, _ := tr.Split(a, SplitSideBySide, bounds)
	layout := tr.Layout(bounds)
	if layout.Leaf {
		t.Fatal("root layout node should be a split")
	}
	if layout.Dir != SplitSideBySide || len(layout.Children) != 2 {
		t.Fatalf("layout = %+v", layout)
	}
	if !layout.Children[0].Leaf || layout.Children[0].ID != a {
		t.Fatalf("first child = %+v, want leaf %v", layout.Children[0], a)
	}
	if layout.Children[1].ID != b {
		t.Fatalf("second child = %+v, want leaf %v", layout.Children[1], b)
	}
	rects := tr.ComputeRects(bounds)
	if layout.Children[0].Rect != rects[a] || layout.Children[1].Rect != rects[b] {
		t.Fatal("Layout rects disagree with ComputeRects")
	}
}

func TestSplit_ClonesChannelAndOrdersNewWindowAfter(t *testing.T) {
	tr, a := New(Channel{ID: "C1", Name: "general", Type: "channel"})
	b, err := tr.Split(a, SplitSideBySide, testBounds)
	if err != nil {
		t.Fatal(err)
	}
	if ch, _ := tr.Channel(b); ch != (Channel{ID: "C1", Name: "general", Type: "channel"}) {
		t.Fatalf("new window channel = %+v, want clone of source", ch)
	}
	if got := tr.Leaves(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("Leaves() = %v, want [%v %v] (new window after/right of source)", got, a, b)
	}
	rects := tr.ComputeRects(testBounds)
	if rects[b].X <= rects[a].X {
		t.Fatalf("vsp must place new window to the right: a=%+v b=%+v", rects[a], rects[b])
	}
}

func TestSplit_StackedPlacesNewWindowBelow(t *testing.T) {
	tr, a := New(Channel{ID: "C1"})
	b, err := tr.Split(a, SplitStacked, testBounds)
	if err != nil {
		t.Fatal(err)
	}
	rects := tr.ComputeRects(testBounds)
	if rects[b].Y <= rects[a].Y {
		t.Fatalf("sp must place new window below: a=%+v b=%+v", rects[a], rects[b])
	}
}

func TestSplit_SameDirInsertsSiblingNotNested(t *testing.T) {
	tr, a := New(Channel{})
	b, _ := tr.Split(a, SplitSideBySide, testBounds)
	c, err := tr.Split(b, SplitSideBySide, Rect{X: 0, Y: 0, W: 300, H: 48})
	if err != nil {
		t.Fatal(err)
	}
	layout := tr.Layout(Rect{X: 0, Y: 0, W: 300, H: 48})
	if len(layout.Children) != 3 {
		t.Fatalf("same-dir split should produce 3 siblings, got layout %+v", layout)
	}
	if got := tr.Leaves(); got[0] != a || got[1] != b || got[2] != c {
		t.Fatalf("Leaves() = %v, want [a b c] order", got)
	}
}

func TestSplit_RefusesWhenNoRoom(t *testing.T) {
	tr, a := New(Channel{})
	// Bounds too narrow for two side-by-side windows.
	narrow := Rect{X: 0, Y: 0, W: 2*MinWidth - 1, H: 48}
	if _, err := tr.Split(a, SplitSideBySide, narrow); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("err = %v, want ErrNoRoom", err)
	}
	// Stacked still fits in the same bounds.
	if _, err := tr.Split(a, SplitStacked, narrow); err != nil {
		t.Fatalf("stacked split should fit: %v", err)
	}
	// Unknown window.
	if _, err := tr.Split(LeafID(999), SplitStacked, testBounds); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSplit_SameDirRefusalCountsAllSiblings(t *testing.T) {
	tr, a := New(Channel{})
	bounds := Rect{X: 0, Y: 0, W: 3*MinWidth - 1, H: 48} // room for 2, not 3
	b, err := tr.Split(a, SplitSideBySide, bounds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Split(b, SplitSideBySide, bounds); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("third column must be refused: err = %v", err)
	}
}

func TestClose_CollapsesAndReturnsNeighbor(t *testing.T) {
	tr, a := New(Channel{ID: "C1"})
	b, _ := tr.Split(a, SplitSideBySide, testBounds)
	c, _ := tr.Split(b, SplitStacked, testBounds)
	// Close c: focus falls to its sibling b; b's column re-expands.
	next, err := tr.Close(c)
	if err != nil {
		t.Fatal(err)
	}
	if next != b {
		t.Fatalf("focus candidate = %v, want %v", next, b)
	}
	rects := tr.ComputeRects(testBounds)
	if len(rects) != 2 {
		t.Fatalf("got %d windows, want 2", len(rects))
	}
	if rects[b].H != testBounds.H {
		t.Fatalf("b should re-expand to full height, got %+v", rects[b])
	}
}

func TestClose_LastWindowRefused(t *testing.T) {
	tr, a := New(Channel{})
	if _, err := tr.Close(a); !errors.Is(err, ErrLastWindow) {
		t.Fatalf("err = %v, want ErrLastWindow", err)
	}
}

func TestOnly_CollapsesToSingleWindow(t *testing.T) {
	tr, a := New(Channel{ID: "C1"})
	b, _ := tr.Split(a, SplitSideBySide, testBounds)
	_, _ = tr.Split(b, SplitStacked, testBounds)
	if err := tr.Only(b); err != nil {
		t.Fatal(err)
	}
	if got := tr.Leaves(); len(got) != 1 || got[0] != b {
		t.Fatalf("Leaves() = %v, want [%v]", got, b)
	}
	if ch, _ := tr.Channel(b); ch.ID != "C1" {
		t.Fatalf("surviving window lost its channel: %+v", ch)
	}
}

func TestCycle_WrapsInTreeOrder(t *testing.T) {
	tr, a := New(Channel{})
	b, _ := tr.Split(a, SplitSideBySide, testBounds)
	c, _ := tr.Split(b, SplitStacked, testBounds)
	if got := tr.Cycle(a, 1); got != b {
		t.Fatalf("Cycle(a) = %v, want %v", got, b)
	}
	if got := tr.Cycle(c, 1); got != a {
		t.Fatalf("Cycle(c) should wrap to %v, got %v", a, got)
	}
	if got := tr.Cycle(a, -1); got != c {
		t.Fatalf("Cycle(a, -1) should wrap to %v, got %v", c, got)
	}
}
