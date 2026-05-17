package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// fakeHistory implements historyFetcher for backfill tests. Tracks
// call count per channel and peak concurrency. repliesResponses /
// repliesCalls support the thread-phase tests.
type fakeHistory struct {
	mu               sync.Mutex
	inFlight         int32
	maxInFlight      int32
	delay            time.Duration
	responses        map[string][]*slack.GetConversationHistoryResponse
	history          map[string][]slack.Message // alternate flat input: channelID → messages
	calls            map[string]int
	oldestSeen       map[string][]string // per-channel: oldest param of each call, in order
	repliesResponses map[string][]slack.Message // keyed by threadTS
	repliesCalls     []struct{ Channel, TS string }

	subscriptionsResponse []slackclient.ThreadSubscriptionView
	subscriptionsErr      error
	subscriptionsCalls    int

	unreads    []slackclient.UnreadInfo
	threadsAgg slackclient.ThreadsAggregate
}

// GetUnreadCounts satisfies historyFetcher. Returns the preconfigured
// unread/aggregate values. Defaults (nil/zero) yield an empty unread
// set, which is what every pre-existing backfill test expects.
func (f *fakeHistory) GetUnreadCounts() ([]slackclient.UnreadInfo, slackclient.ThreadsAggregate, error) {
	return f.unreads, f.threadsAgg, nil
}

// ListThreadSubscriptions satisfies historyFetcher. Returns the
// preconfigured slice (or error) and increments the call counter.
func (f *fakeHistory) ListThreadSubscriptions(ctx context.Context) ([]slackclient.ThreadSubscriptionView, error) {
	f.mu.Lock()
	f.subscriptionsCalls++
	f.mu.Unlock()
	if f.subscriptionsErr != nil {
		return nil, f.subscriptionsErr
	}
	return f.subscriptionsResponse, nil
}

// GetHistorySince satisfies historyFetcher. It looks up the per-channel
// response queue (f.responses) and returns its head; if responses is
// empty for the channel, falls back to f.history (a simpler flat map
// of channelID → []slack.Message) so tests can use whichever shape is
// more convenient. Capped is true when the returned slice was
// truncated by maxTotal or when the queued response had HasMore set.
func (f *fakeHistory) GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (slackclient.HistorySinceResult, error) {
	cur := atomic.AddInt32(&f.inFlight, 1)
	defer atomic.AddInt32(&f.inFlight, -1)
	for {
		hi := atomic.LoadInt32(&f.maxInFlight)
		if cur <= hi || atomic.CompareAndSwapInt32(&f.maxInFlight, hi, cur) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls != nil {
		f.calls[channelID]++
	}
	if f.oldestSeen != nil {
		f.oldestSeen[channelID] = append(f.oldestSeen[channelID], oldest)
	}
	if resps := f.responses[channelID]; len(resps) > 0 {
		resp := resps[0]
		f.responses[channelID] = resps[1:]
		msgs := resp.Messages
		capped := resp.HasMore
		if maxTotal > 0 && len(msgs) > maxTotal {
			msgs = msgs[:maxTotal]
			capped = true
		}
		return slackclient.HistorySinceResult{Messages: msgs, Capped: capped}, nil
	}
	msgs := f.history[channelID]
	capped := false
	if maxTotal > 0 && len(msgs) > maxTotal {
		msgs = msgs[:maxTotal]
		capped = true
	}
	return slackclient.HistorySinceResult{Messages: msgs, Capped: capped}, nil
}

// GetReplies satisfies historyFetcher. Records the call and returns
// the preconfigured reply slice for the given threadTS, if any.
func (f *fakeHistory) GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repliesCalls = append(f.repliesCalls, struct{ Channel, TS string }{channelID, threadTS})
	if replies, ok := f.repliesResponses[threadTS]; ok {
		return replies, nil
	}
	return nil, nil
}

// captureSender records every tea.Msg dispatched to it. Substituted
// for *tea.Program in tests via the teaSender interface.
type captureSender struct {
	mu   sync.Mutex
	sent []tea.Msg
}

func (c *captureSender) Send(msg tea.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
}

func newTestDB(t *testing.T) *cache.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := cache.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "T"}); err != nil {
		t.Fatalf("UpsertWorkspace: %v", err)
	}
	return db
}

func TestBackfillChannels_FetchesPerChannelSinceSyncedAt(t *testing.T) {
	db := newTestDB(t)

	// Two channels with cached messages and a watermark set. The
	// backfiller now derives `oldest` from latest_synced_ts (via
	// GetChannelWatermark), not from synced_at — so set both, but the
	// per-channel ts watermark is what should reach the API call.
	db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "a", Type: "channel"})
	db.UpsertChannel(cache.Channel{ID: "C2", WorkspaceID: "T1", Name: "b", Type: "channel"})
	db.UpsertMessage(cache.Message{TS: "10.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "old"})
	db.UpsertMessage(cache.Message{TS: "20.000000", ChannelID: "C2", WorkspaceID: "T1", UserID: "U1", Text: "old"})
	db.SetChannelSyncedAt("C1", 100)
	db.SetChannelSyncedAt("C2", 200)
	if err := db.SetChannelLatestSyncedTS("C1", "100.000000"); err != nil {
		t.Fatalf("SetChannelLatestSyncedTS C1: %v", err)
	}
	if err := db.SetChannelLatestSyncedTS("C2", "200.000000"); err != nil {
		t.Fatalf("SetChannelLatestSyncedTS C2: %v", err)
	}

	fh := &fakeHistory{
		responses: map[string][]*slack.GetConversationHistoryResponse{
			"C1": {{Messages: []slack.Message{{Msg: slack.Msg{Timestamp: "150.000000", User: "U2", Text: "new in c1"}}}}},
			"C2": {{Messages: []slack.Message{{Msg: slack.Msg{Timestamp: "250.000000", User: "U2", Text: "new in c2"}}}}},
		},
		calls:      map[string]int{},
		oldestSeen: map[string][]string{},
	}

	bf := newBackfiller(fh, db, "T1", "USELF", nil, 4, 500, nil)
	if err := bf.runChannelPhase(context.Background()); err != nil {
		t.Fatalf("runChannelPhase: %v", err)
	}

	if fh.calls["C1"] != 1 || fh.calls["C2"] != 1 {
		t.Errorf("expected 1 call each for C1 and C2, got %+v", fh.calls)
	}
	if got := fh.oldestSeen["C1"]; len(got) != 1 || got[0] != "100.000000" {
		t.Errorf("C1 oldest = %+v, want [100.000000]", got)
	}
	if got := fh.oldestSeen["C2"]; len(got) != 1 || got[0] != "200.000000" {
		t.Errorf("C2 oldest = %+v, want [200.000000]", got)
	}
	// New messages were upserted.
	if _, err := db.GetMessage("C1", "150.000000"); err != nil {
		t.Errorf("missing upserted message C1/150: %v", err)
	}
	if _, err := db.GetMessage("C2", "250.000000"); err != nil {
		t.Errorf("missing upserted message C2/250: %v", err)
	}
}

func TestBackfillChannels_BoundedConcurrency(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 8; i++ {
		id := "C" + string(rune('1'+i))
		db.UpsertChannel(cache.Channel{ID: id, WorkspaceID: "T1", Name: id, Type: "channel"})
		db.UpsertMessage(cache.Message{TS: "1.000000", ChannelID: id, WorkspaceID: "T1", UserID: "U", Text: "x"})
	}

	responses := map[string][]*slack.GetConversationHistoryResponse{}
	for i := 0; i < 8; i++ {
		id := "C" + string(rune('1'+i))
		responses[id] = []*slack.GetConversationHistoryResponse{{}}
	}
	fh := &fakeHistory{
		delay:      50 * time.Millisecond,
		responses:  responses,
		calls:      map[string]int{},
		oldestSeen: map[string][]string{},
	}

	bf := newBackfiller(fh, db, "T1", "USELF", nil, 4, 500, nil)
	if err := bf.runChannelPhase(context.Background()); err != nil {
		t.Fatalf("runChannelPhase: %v", err)
	}

	if got := atomic.LoadInt32(&fh.maxInFlight); got > 4 {
		t.Errorf("max in-flight = %d, want <= 4", got)
	}
	if len(fh.calls) != 8 {
		t.Errorf("expected 8 channels called, got %d", len(fh.calls))
	}
}

// TestBackfillThreads_FetchesRepliesForInvolvedThreads verifies that
// after the channel phase populates discoveredThreads, the thread
// phase filters to threads where the cache shows the user is involved
// (parent or any reply authored by selfUserID) and fetches replies
// only for those.
func TestBackfillThreads_FetchesRepliesForInvolvedThreads(t *testing.T) {
	db := newTestDB(t)
	db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "a", Type: "channel"})
	// Existing cached parent in thread 100: self authored → involved.
	db.UpsertMessage(cache.Message{TS: "100.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "USELF", Text: "self parent", ThreadTS: "100.000000"})
	// Existing cached parent in thread 200: not involved.
	db.UpsertMessage(cache.Message{TS: "200.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "other parent", ThreadTS: "200.000000"})
	db.SetChannelSyncedAt("C1", 50)

	fh := &fakeHistory{
		responses: map[string][]*slack.GetConversationHistoryResponse{
			"C1": {{Messages: []slack.Message{
				// New reply on involved thread 100.
				{Msg: slack.Msg{Timestamp: "150.000000", User: "U2", Text: "reply to self", ThreadTimestamp: "100.000000"}},
				// New reply on non-involved thread 200.
				{Msg: slack.Msg{Timestamp: "250.000000", User: "U3", Text: "reply on other", ThreadTimestamp: "200.000000"}},
			}}},
		},
		calls:      map[string]int{},
		oldestSeen: map[string][]string{},
		repliesResponses: map[string][]slack.Message{
			"100.000000": {
				{Msg: slack.Msg{Timestamp: "100.000000", User: "USELF", Text: "self parent", ThreadTimestamp: "100.000000"}},
				{Msg: slack.Msg{Timestamp: "150.000000", User: "U2", Text: "reply to self", ThreadTimestamp: "100.000000"}},
			},
		},
	}

	bf := newBackfiller(fh, db, "T1", "USELF", nil, 4, 500, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(fh.repliesCalls) != 1 {
		t.Fatalf("expected 1 replies call (for involved thread 100), got %d: %+v", len(fh.repliesCalls), fh.repliesCalls)
	}
	if fh.repliesCalls[0].Channel != "C1" || fh.repliesCalls[0].TS != "100.000000" {
		t.Errorf("replies call = %+v, want C1/100.000000", fh.repliesCalls[0])
	}
}

// TestBackfill_FiresThreadsListDirtyMsg verifies that run() dispatches
// exactly one ThreadsListDirtyMsg into the program, carrying the
// workspace ID so the UI knows which team's threads view to re-query.
func TestBackfill_FiresThreadsListDirtyMsg(t *testing.T) {
	db := newTestDB(t)
	db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "a", Type: "channel"})
	db.UpsertMessage(cache.Message{TS: "1.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U", Text: "x"})
	db.SetChannelSyncedAt("C1", 100)

	fh := &fakeHistory{
		responses: map[string][]*slack.GetConversationHistoryResponse{
			"C1": {{}},
		},
		calls:      map[string]int{},
		oldestSeen: map[string][]string{},
	}

	captured := &captureSender{}
	bf := newBackfiller(fh, db, "T1", "USELF", captured, 4, 500, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	captured.mu.Lock()
	defer captured.mu.Unlock()
	if len(captured.sent) != 1 {
		t.Fatalf("expected 1 sent msg, got %d", len(captured.sent))
	}
	dirty, ok := captured.sent[0].(ui.ThreadsListDirtyMsg)
	if !ok {
		t.Fatalf("expected ThreadsListDirtyMsg, got %T", captured.sent[0])
	}
	if dirty.TeamID != "T1" {
		t.Errorf("TeamID = %s, want T1", dirty.TeamID)
	}
}

func TestBackfill_DedupeWindow(t *testing.T) {
	gate := &dedupeGate{window: 30 * time.Second}

	first := gate.tryStart(time.Unix(1000, 0))
	if !first {
		t.Fatal("first call should be allowed")
	}
	second := gate.tryStart(time.Unix(1010, 0))
	if second {
		t.Error("second call within 30s should be blocked")
	}
	third := gate.tryStart(time.Unix(1031, 0))
	if !third {
		t.Error("call after window should be allowed")
	}
}

// subView constructs a ThreadSubscriptionView from primitives so the
// subscription-phase tests stay readable.
func subView(channel, threadTS, lastRead, text, user string, active bool) slackclient.ThreadSubscriptionView {
	return slackclient.ThreadSubscriptionView{
		Subscription: slackclient.ThreadSubscription{
			ChannelID: channel, ThreadTS: threadTS, LastRead: lastRead, Active: active,
		},
		RootMessage: slack.Message{
			Msg: slack.Msg{
				Timestamp:       threadTS,
				ThreadTimestamp: threadTS,
				User:            user,
				Text:            text,
				Channel:         channel,
			},
		},
	}
}

// TestBackfillSubscriptions_PopulatesTable verifies that the phase
// fetches the workspace's subscription list and writes each active
// item into the thread_subscriptions table.
func TestBackfillSubscriptions_PopulatesTable(t *testing.T) {
	db := newTestDB(t)
	fake := &fakeHistory{
		responses: map[string][]*slack.GetConversationHistoryResponse{}, // no channels
		subscriptionsResponse: []slackclient.ThreadSubscriptionView{
			subView("C1", "1700000100.000000", "1700000150.000000", "p1", "U2", true),
			subView("C2", "1700000200.000000", "1700000250.000000", "p2", "U3", true),
		},
	}
	bf := newBackfiller(fake, db, "T1", "U1", nil, 4, 500, nil)
	if err := bf.runSubscriptionPhase(context.Background()); err != nil {
		t.Fatalf("runSubscriptionPhase: %v", err)
	}
	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 subscriptions in DB, got %d", len(got))
	}
}

// TestBackfillSubscriptions_UpsertsRootMessageIntoMessagesCache
// verifies that every root_msg from the view response is upserted
// into the messages cache so the threads view can render parents
// even without a separate conversations.replies fetch.
func TestBackfillSubscriptions_UpsertsRootMessageIntoMessagesCache(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general"}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	fake := &fakeHistory{
		subscriptionsResponse: []slackclient.ThreadSubscriptionView{
			subView("C1", "1700000100.000000", "1700000150.000000", "parent X", "U2", true),
		},
	}
	bf := newBackfiller(fake, db, "T1", "U1", nil, 4, 500, nil)
	if err := bf.runSubscriptionPhase(context.Background()); err != nil {
		t.Fatalf("runSubscriptionPhase: %v", err)
	}

	// The root_msg should have been upserted into the messages cache.
	msgs, err := db.GetThreadReplies("C1", "1700000100.000000")
	if err != nil {
		t.Fatalf("GetThreadReplies: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 cached message (the parent), got %d", len(msgs))
	}
	if msgs[0].Text != "parent X" || msgs[0].UserID != "U2" {
		t.Fatalf("root_msg fields not preserved: %+v", msgs[0])
	}

	// No GetReplies calls should have been made — root_msg already
	// gave us the parent.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.repliesCalls) != 0 {
		t.Fatalf("expected 0 GetReplies calls, got %d: %v", len(fake.repliesCalls), fake.repliesCalls)
	}
}

// TestBackfillSubscriptions_ReconcilesUnsubscribes verifies that a
// local subscription absent from the server's fresh list is
// tombstoned (no longer active).
func TestBackfillSubscriptions_ReconcilesUnsubscribes(t *testing.T) {
	db := newTestDB(t)
	// Seed a local subscription that's no longer in the server's fresh list.
	if err := db.UpsertThreadSubscription("T1", "C1", "1700000100.000000", "1700000150.000000", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}
	fake := &fakeHistory{
		subscriptionsResponse: []slackclient.ThreadSubscriptionView{
			subView("C2", "1700000300.000000", "1700000350.000000", "p2", "U3", true),
		},
	}
	bf := newBackfiller(fake, db, "T1", "U1", nil, 4, 500, nil)
	if err := bf.runSubscriptionPhase(context.Background()); err != nil {
		t.Fatalf("runSubscriptionPhase: %v", err)
	}
	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 || got[0].ChannelID != "C2" {
		t.Fatalf("expected only C2 active after reconcile, got %+v", got)
	}
}

// TestBackfillSubscriptions_ErrorTriggersAvailabilityCallback verifies
// that an API error fires availableCb(false) and surfaces the error
// to the caller.
func TestBackfillSubscriptions_ErrorTriggersAvailabilityCallback(t *testing.T) {
	db := newTestDB(t)
	fake := &fakeHistory{
		subscriptionsErr: errors.New("network kaboom"),
	}
	var calls []bool
	cb := func(available bool) { calls = append(calls, available) }
	bf := newBackfiller(fake, db, "T1", "U1", nil, 4, 500, cb)

	if err := bf.runSubscriptionPhase(context.Background()); err == nil {
		t.Fatalf("expected error, got nil")
	}
	if len(calls) != 1 || calls[0] != false {
		t.Fatalf("expected one callback with available=false, got %v", calls)
	}
}

// TestBackfillSubscriptions_SuccessTriggersAvailabilityCallback
// verifies that a successful pass fires availableCb(true) exactly
// once.
func TestBackfillSubscriptions_SuccessTriggersAvailabilityCallback(t *testing.T) {
	db := newTestDB(t)
	fake := &fakeHistory{}
	var calls []bool
	cb := func(available bool) { calls = append(calls, available) }
	bf := newBackfiller(fake, db, "T1", "U1", nil, 4, 500, cb)
	if err := bf.runSubscriptionPhase(context.Background()); err != nil {
		t.Fatalf("runSubscriptionPhase: %v", err)
	}
	if len(calls) != 1 || calls[0] != true {
		t.Fatalf("expected one callback with available=true, got %v", calls)
	}
}

// TestBackfill_CapHit_DoesNotAdvanceWatermark verifies the
// silent-message-drop fix: when GetHistorySince returns Capped=true,
// the backfiller must NOT advance latest_synced_ts. If it did, the
// un-fetched (oldest, capped-batch-max-ts) window would be silently
// skipped on the next reconnect.
func TestBackfill_CapHit_DoesNotAdvanceWatermark(t *testing.T) {
	db := newTestDB(t)

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "busy", Type: "channel", IsMember: true}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000000"); err != nil {
		t.Fatalf("set watermark: %v", err)
	}
	// Pre-existing message so the channel appears in ChannelsWithMessages.
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000000.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "anchor", CreatedAt: 1700000000,
	}); err != nil {
		t.Fatalf("upsert anchor: %v", err)
	}

	// Fake returns 10 messages but cap is 5 → Capped == true.
	fh := &fakeHistory{
		history: map[string][]slack.Message{
			"C1": makeBackfillMessages("1700001000", 10),
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 1 /*conc*/, 5 /*cap*/, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := db.GetChannelLatestSyncedTS("C1")
	if got != "1700000000.000000" {
		t.Errorf("watermark advanced despite cap: got %q want %q", got, "1700000000.000000")
	}
}

// TestBackfill_FullFetch_AdvancesWatermarkToMaxTS verifies the
// happy-path: when GetHistorySince returns all available messages
// (Capped=false), the backfiller advances latest_synced_ts to the
// highest ts in the batch so subsequent reconnects start fetching
// from that point forward.
func TestBackfill_FullFetch_AdvancesWatermarkToMaxTS(t *testing.T) {
	db := newTestDB(t)

	if err := db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "quiet", Type: "channel", IsMember: true}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	if err := db.SetChannelLatestSyncedTS("C1", "1700000000.000000"); err != nil {
		t.Fatalf("set watermark: %v", err)
	}
	if err := db.UpsertMessage(cache.Message{
		TS: "1700000000.000000", ChannelID: "C1", WorkspaceID: "T1",
		UserID: "U1", Text: "anchor", CreatedAt: 1700000000,
	}); err != nil {
		t.Fatalf("upsert anchor: %v", err)
	}

	// 3 messages, cap is 10 → Capped == false. Highest ts is .000002.
	fh := &fakeHistory{
		history: map[string][]slack.Message{
			"C1": makeBackfillMessages("1700001000", 3),
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 1, 10, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := db.GetChannelLatestSyncedTS("C1")
	want := "1700001000.000002"
	if got != want {
		t.Errorf("watermark not advanced to MAX(ts): got %q want %q", got, want)
	}
}

// TestBackfill_NewDM_NoCachedMessages_IsCaughtUp verifies the
// brand-new-DM case: a DM the user has never opened in slk (no
// channel row, no cached messages) but which Slack reports as having
// unreads via client.counts must be included in the reconnect
// backfill. Pre-Task-4, ChannelsWithMessages excluded it entirely.
func TestBackfill_NewDM_NoCachedMessages_IsCaughtUp(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// D1 is a DM that has never been opened in slk: no UpsertChannel,
	// no cached messages. Slack tells us via client.counts that it
	// has unreads.
	fh := &fakeHistory{
		history: map[string][]slack.Message{
			"D1": {{Msg: slack.Msg{
				Type:      "message",
				Timestamp: "1700009999.000001",
				User:      "U_FRIEND",
				Text:      "hey, you up?",
			}}},
		},
		unreads: []slackclient.UnreadInfo{
			{ChannelID: "D1", HasUnread: true, Count: 1, LastRead: "0"},
		},
	}

	bf := newBackfiller(fh, db, "T1", "U_ME", nil, 1, 10, nil)
	if err := bf.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	msgs, err := db.GetMessages("D1", 10, "")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 cached message for D1, got %d", len(msgs))
	}
	if msgs[0].TS != "1700009999.000001" {
		t.Errorf("wrong ts: got %q want %q", msgs[0].TS, "1700009999.000001")
	}
}

// makeBackfillMessages returns n messages with monotonically
// increasing ts starting from base. Each ts is base + ".000NNN" so
// they sort correctly as Slack ts strings under lexicographic compare.
func makeBackfillMessages(base string, n int) []slack.Message {
	out := make([]slack.Message, n)
	for i := 0; i < n; i++ {
		out[i] = slack.Message{Msg: slack.Msg{
			Type:      "message",
			Timestamp: fmt.Sprintf("%s.%06d", base, i),
			User:      "U1",
			Text:      fmt.Sprintf("msg %d", i),
		}}
	}
	return out
}
