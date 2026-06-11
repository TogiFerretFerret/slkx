// internal/ui/messages/prepend_test.go
//
// PrependMessages boundary guard: an older-history backfill that
// raced a channel re-select (the re-select replaced the buffer while
// the backfill was in flight) can deliver items the buffer already
// contains. Items not strictly older than the buffer head are
// overlap/duplicates and must be dropped.
package messages

import "testing"

func newPrependTestModel() *Model {
	m := New([]MessageItem{
		{TS: "1717171000.000100", UserName: "alice", UserID: "U1", Text: "head", Timestamp: "1:00 PM"},
		{TS: "1717171000.000200", UserName: "bob", UserID: "U2", Text: "tail", Timestamp: "1:01 PM"},
	}, "general")
	return &m
}

func TestPrependMessages_DropsItemsNotOlderThanHead(t *testing.T) {
	m := newPrependTestModel()
	m.PrependMessages([]MessageItem{
		{TS: "1717170999.000050", UserName: "zoe", UserID: "U9", Text: "older", Timestamp: "12:59 PM"},   // strictly older: kept
		{TS: "1717171000.000100", UserName: "zoe", UserID: "U9", Text: "dup-head", Timestamp: "1:00 PM"}, // == head: dropped
		{TS: "1717171000.000150", UserName: "zoe", UserID: "U9", Text: "overlap", Timestamp: "1:00 PM"},  // inside buffer: dropped
	})
	msgs := m.Messages()
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages (1 kept + 2 baseline), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].TS != "1717170999.000050" {
		t.Fatalf("kept item must be at the head, got TS %q", msgs[0].TS)
	}
	if msgs[1].TS != "1717171000.000100" || msgs[2].TS != "1717171000.000200" {
		t.Fatalf("baseline must be intact, got %q %q", msgs[1].TS, msgs[2].TS)
	}
}

func TestPrependMessages_AllOverlapIsNoOp(t *testing.T) {
	m := newPrependTestModel()
	m.PrependMessages([]MessageItem{
		{TS: "1717171000.000100", UserName: "zoe", UserID: "U9", Text: "dup-head", Timestamp: "1:00 PM"},
	})
	if got := len(m.Messages()); got != 2 {
		t.Fatalf("fully-overlapping prepend must be a no-op, got %d messages", got)
	}
}

func TestPrependMessages_EmptyBufferPrependsAll(t *testing.T) {
	m := New(nil, "general")
	m.PrependMessages([]MessageItem{
		{TS: "1717171000.000100", UserName: "alice", UserID: "U1", Text: "first", Timestamp: "1:00 PM"},
	})
	if got := len(m.Messages()); got != 1 {
		t.Fatalf("prepend into empty buffer must keep all items, got %d", got)
	}
}
