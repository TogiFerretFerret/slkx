package cache

import "fmt"

type User struct {
	ID          string
	WorkspaceID string
	Name        string
	DisplayName string
	AvatarURL   string
	Presence    string
	// IsBot is true for Slack apps and classic bots (the union of
	// slack.User.IsBot and IsAppUser). Used to bucket their DMs into
	// the "Apps" sidebar section so they don't clutter the human DM
	// list.
	IsBot bool
	// IsExternal is true for users whose home team_id differs from
	// the workspace's TeamID (Slack Connect / shared-channel guests).
	// Set by the user-resolution path; persisted so we don't re-resolve.
	IsExternal bool
	UpdatedAt  int64
}

func (db *DB) UpsertUser(u User) error {
	_, err := db.conn.Exec(`
		INSERT INTO users (id, workspace_id, name, display_name, avatar_url, presence, is_bot, is_external, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			display_name=excluded.display_name,
			avatar_url=excluded.avatar_url,
			presence=excluded.presence,
			is_bot=excluded.is_bot,
			is_external=excluded.is_external,
			updated_at=excluded.updated_at
	`, u.ID, u.WorkspaceID, u.Name, u.DisplayName, u.AvatarURL, u.Presence, u.IsBot, u.IsExternal, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting user: %w", err)
	}
	return nil
}

func (db *DB) GetUser(id string) (User, error) {
	var u User
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, is_bot, is_external, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.IsBot, &u.IsExternal, &u.UpdatedAt)
	if err != nil {
		return u, fmt.Errorf("getting user: %w", err)
	}
	return u, nil
}

func (db *DB) ListUsers(workspaceID string) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, is_bot, is_external, updated_at
		FROM users WHERE workspace_id = ? ORDER BY display_name, name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.IsBot, &u.IsExternal, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) UpdatePresence(userID, presence string) error {
	_, err := db.conn.Exec(`UPDATE users SET presence = ? WHERE id = ?`, presence, userID)
	if err != nil {
		return fmt.Errorf("updating presence: %w", err)
	}
	return nil
}

// FilterCachedUsers takes a list of user IDs and returns a set containing the ones
// that are already present in the users cache table.
func (db *DB) FilterCachedUsers(ids []string) (map[string]struct{}, error) {
	cached := make(map[string]struct{})
	if len(ids) == 0 {
		return cached, nil
	}

	// Chunk the query to avoid SQLite parameter limit (999 by default)
	const chunkSize = 999
	for i := 0; i < len(ids); i += chunkSize {
		end := i + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]

		// Build query with placeholders
		query := "SELECT id FROM users WHERE id IN ("
		args := make([]any, len(chunk))
		for j, id := range chunk {
			if j > 0 {
				query += ","
			}
			query += "?"
			args[j] = id
		}
		query += ")"

		rows, err := db.conn.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("filtering cached users: %w", err)
		}

		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				cached[id] = struct{}{}
			}
		}
		rows.Close() // Close early since we are in a loop
	}

	return cached, nil
}
