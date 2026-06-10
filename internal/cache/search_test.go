package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// ftsRowCount returns the number of rows in messages_fts joined back to
// messages (external-content tables can't be counted directly without
// a query term, so probe via a rowid join).
func ftsRowCount(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	err := db.conn.QueryRow(
		`SELECT count(*) FROM messages m JOIN messages_fts f ON f.rowid = m.rowid WHERE messages_fts MATCH '"hello"*'`).Scan(&n)
	if err != nil {
		t.Fatalf("counting fts rows: %v", err)
	}
	return n
}

// TestFTS5Available is the canary: if modernc.org/sqlite ever ships
// without FTS5, this fails loudly instead of silently degrading every
// install to LIKE search.
func TestFTS5Available(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.ftsDisabled {
		t.Fatal("ftsDisabled is set: FTS5 unavailable in the sqlite driver")
	}
	var n int
	if err := db.conn.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='messages_fts'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("messages_fts table missing (n=%d)", n)
	}
}

func TestFTSTriggers_InsertUpdateDelete(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	msg := Message{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "hello world"}
	if err := db.UpsertMessage(msg); err != nil {
		t.Fatal(err)
	}
	if got := ftsRowCount(t, db); got != 1 {
		t.Fatalf("after insert: fts matches = %d, want 1", got)
	}

	// Edit via upsert (the real edit path): text changes, old token gone.
	msg.Text = "goodbye world"
	if err := db.UpsertMessage(msg); err != nil {
		t.Fatal(err)
	}
	if got := ftsRowCount(t, db); got != 0 {
		t.Fatalf("after edit: stale 'hello' match still present (%d)", got)
	}

	// Hard DELETE keeps the index consistent (soft delete is filtered at
	// query time, but the AFTER DELETE trigger must exist for integrity).
	if _, err := db.conn.Exec(`DELETE FROM messages WHERE ts = ? AND channel_id = ?`, msg.TS, "C1"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.conn.QueryRow(
		`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH '"goodbye"*'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("after delete: stale fts row remains (%d)", n)
	}
}

// TestFTSBackfillOnPreExistingDB simulates upgrading an existing
// install: a DB created without the FTS table gets its rows indexed on
// next open. Pattern follows TestSubtypeMigrationOnPreExistingDB
// (db_test.go).
func TestFTSBackfillOnPreExistingDB(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.db")

	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE messages (
		ts TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		workspace_id TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		text TEXT NOT NULL DEFAULT '',
		thread_ts TEXT NOT NULL DEFAULT '',
		reply_count INTEGER NOT NULL DEFAULT 0,
		edited_at TEXT NOT NULL DEFAULT '',
		is_deleted INTEGER NOT NULL DEFAULT 0,
		raw_json TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0,
		subtype TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (ts, channel_id)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(
		`INSERT INTO messages (ts, channel_id, workspace_id, text) VALUES ('1700000001.000000', 'C1', 'T1', 'hello world')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := ftsRowCount(t, db); got != 1 {
		t.Fatalf("backfill: fts matches = %d, want 1", got)
	}
}
