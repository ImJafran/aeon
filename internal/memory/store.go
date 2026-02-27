package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Category string

const (
	CategoryCore         Category = "core"
	CategoryDaily        Category = "daily"
	CategoryConversation Category = "conversation"
	CategoryCustom       Category = "custom"
)

type Entry struct {
	ID         int64
	Category   Category
	Content    string
	Tags       string
	CreatedAt  time.Time
	AccessedAt time.Time
}

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Performance tuning
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-8000",
		"PRAGMA mmap_size=67108864",
		"PRAGMA temp_store=MEMORY",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("setting pragma: %w", err)
		}
	}

	if err := initSchema(db); err != nil {
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	return &Store{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category TEXT NOT NULL DEFAULT 'custom',
			content TEXT NOT NULL,
			tags TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			content,
			tags,
			content='memories',
			content_rowid='id'
		);

		CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
		END;

		CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES('delete', old.id, old.content, old.tags);
		END;

		CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES('delete', old.id, old.content, old.tags);
			INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
		END;

		CREATE TABLE IF NOT EXISTS conversation_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_history_session ON conversation_history(session_id);
	`
	_, err := db.Exec(schema)
	return err
}

// MemStore stores a memory entry.
func (s *Store) MemStore(_ context.Context, category Category, content, tags string) (int64, error) {
	result, err := s.db.Exec(
		"INSERT INTO memories (category, content, tags) VALUES (?, ?, ?)",
		string(category), content, tags,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Recall searches memories using FTS5 keyword search.
func (s *Store) Recall(_ context.Context, query string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 5
	}

	rows, err := s.db.Query(`
		SELECT m.id, m.category, m.content, m.tags, m.created_at, m.accessed_at
		FROM memories_fts fts
		JOIN memories m ON m.id = fts.rowid
		WHERE memories_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		// If FTS query fails (bad syntax), fall back to LIKE search
		return s.recallLike(query, limit)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (s *Store) recallLike(query string, limit int) ([]Entry, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT id, category, content, tags, created_at, accessed_at
		FROM memories
		WHERE content LIKE ? OR tags LIKE ?
		ORDER BY accessed_at DESC
		LIMIT ?
	`, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// Get retrieves a single memory by ID.
func (s *Store) Get(_ context.Context, id int64) (*Entry, error) {
	row := s.db.QueryRow(
		"SELECT id, category, content, tags, created_at, accessed_at FROM memories WHERE id = ?", id,
	)

	var e Entry
	if err := row.Scan(&e.ID, &e.Category, &e.Content, &e.Tags, &e.CreatedAt, &e.AccessedAt); err != nil {
		return nil, err
	}

	// Update accessed_at
	s.db.Exec("UPDATE memories SET accessed_at = CURRENT_TIMESTAMP WHERE id = ?", id)

	return &e, nil
}

// List returns memories filtered by category.
func (s *Store) List(_ context.Context, category Category, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows *sql.Rows
	var err error
	if category == "" {
		rows, err = s.db.Query(
			"SELECT id, category, content, tags, created_at, accessed_at FROM memories ORDER BY created_at DESC LIMIT ?",
			limit,
		)
	} else {
		rows, err = s.db.Query(
			"SELECT id, category, content, tags, created_at, accessed_at FROM memories WHERE category = ? ORDER BY created_at DESC LIMIT ?",
			string(category), limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// Forget deletes a memory by ID.
func (s *Store) Forget(_ context.Context, id int64) error {
	_, err := s.db.Exec("DELETE FROM memories WHERE id = ?", id)
	return err
}

// Count returns the total number of memories.
func (s *Store) Count(_ context.Context) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	return count, err
}

// SaveHistory saves a conversation turn.
func (s *Store) SaveHistory(_ context.Context, sessionID, role, content string) error {
	_, err := s.db.Exec(
		"INSERT INTO conversation_history (session_id, role, content) VALUES (?, ?, ?)",
		sessionID, role, truncate(content, 2000),
	)
	return err
}

// GetHistory returns conversation history for a session.
func (s *Store) GetHistory(_ context.Context, sessionID string, limit int) ([]map[string]string, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.Query(`
		SELECT role, content FROM conversation_history
		WHERE session_id = ?
		ORDER BY id DESC LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []map[string]string
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			continue
		}
		history = append(history, map[string]string{"role": role, "content": content})
	}

	// Reverse to chronological order
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	return history, nil
}

// ClearHistory deletes all history for a session.
func (s *Store) ClearHistory(_ context.Context, sessionID string) error {
	_, err := s.db.Exec("DELETE FROM conversation_history WHERE session_id = ?", sessionID)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.Tags, &e.CreatedAt, &e.AccessedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// BuildContextFromMemory retrieves relevant memories for the current query and formats them for the system prompt.
func (s *Store) BuildContextFromMemory(ctx context.Context, query string) string {
	if query == "" {
		return ""
	}

	// Get relevant memories
	entries, err := s.Recall(ctx, query, 5)
	if err != nil || len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<relevant_memories>\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", e.Category, e.Content))
	}
	b.WriteString("</relevant_memories>\n")
	return b.String()
}
