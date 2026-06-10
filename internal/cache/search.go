// Package cache: full-text search over the messages table.
//
// messages_fts is an external-content FTS5 table over messages.text,
// kept in sync by AFTER INSERT/UPDATE/DELETE triggers (the standard
// FTS5 external-content pattern). Soft deletes (is_deleted=1) do not
// touch text, so query time filters them via a join back to messages.
package cache

import "fmt"

// migrateSearch creates the FTS5 table, sync triggers, and backfills
// existing rows. Idempotent: a no-op when messages_fts already exists.
func (db *DB) migrateSearch() error {
	var n int
	if err := db.conn.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='messages_fts'`).Scan(&n); err != nil {
		return fmt.Errorf("probing messages_fts: %w", err)
	}
	if n == 1 {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("beginning fts migration: %w", err)
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE VIRTUAL TABLE messages_fts USING fts5(
			text,
			content='messages',
			content_rowid='rowid',
			tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
		END`,
		`CREATE TRIGGER messages_fts_au AFTER UPDATE OF text ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
			INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
		END`,
		`CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
		END`,
		`INSERT INTO messages_fts(rowid, text) SELECT rowid, text FROM messages`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("fts migration: %w", err)
		}
	}
	return tx.Commit()
}
