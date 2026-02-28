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
	CategoryLesson       Category = "lesson"
	CategoryCorrection   Category = "correction"
)

type Entry struct {
	ID          int64
	Category    Category
	Content     string
	Tags        string
	Importance  float64
	AccessCount int
	CreatedAt   time.Time
	AccessedAt  time.Time
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
		);`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Migration: add access_count column if missing
	var colCount int
	err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('memories') WHERE name='access_count'`).Scan(&colCount)
	if err == nil && colCount == 0 {
		db.Exec(`ALTER TABLE memories ADD COLUMN access_count INTEGER DEFAULT 0`)
	}

	// Migration: add importance column if missing
	var impCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('memories') WHERE name='importance'`).Scan(&impCount)
	if err == nil && impCount == 0 {
		db.Exec(`ALTER TABLE memories ADD COLUMN importance REAL DEFAULT 0.5`)
	}

	rest := `

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
	_, err = db.Exec(rest)
	return err
}

// MemStore stores a memory entry with an importance score.
// Importance ranges from 0.0 to 1.0 (default 0.5).
func (s *Store) MemStore(_ context.Context, category Category, content, tags string, importance float64) (int64, error) {
	if importance <= 0 {
		importance = defaultImportance(category)
	}
	result, err := s.db.Exec(
		"INSERT INTO memories (category, content, tags, importance) VALUES (?, ?, ?, ?)",
		string(category), content, tags, importance,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// defaultImportance returns a sensible default importance for a category.
func defaultImportance(category Category) float64 {
	switch category {
	case CategoryCorrection:
		return 0.9
	case CategoryLesson:
		return 0.85
	case CategoryCore:
		return 0.8
	case CategoryDaily:
		return 0.5
	case CategoryConversation:
		return 0.3
	default:
		return 0.5
	}
}

// Recall searches memories using FTS5 keyword search with composite relevance scoring.
// Score = fts5_rank * 0.4 + decay_score * 0.3 + importance * 0.3
// where decay_score = exp(-0.05 * days_since_access) * (1 + 0.02 * access_count)
func (s *Store) Recall(_ context.Context, query string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 5
	}

	rows, err := s.db.Query(`
		SELECT m.id, m.category, m.content, m.tags, COALESCE(m.importance, 0.5),
		       COALESCE(m.access_count, 0), m.created_at, m.accessed_at
		FROM memories_fts fts
		JOIN memories m ON m.id = fts.rowid
		WHERE memories_fts MATCH ?
		ORDER BY (
			rank * 0.4
			+ (EXP(-0.05 * (julianday('now') - julianday(m.accessed_at)))
			   * (1.0 + 0.02 * COALESCE(m.access_count, 0))) * 0.3
			+ COALESCE(m.importance, 0.5) * 0.3
		)
		LIMIT ?
	`, query, limit)
	if err != nil {
		// If FTS query fails (bad syntax), fall back to LIKE search
		return s.recallLike(query, limit)
	}
	defer rows.Close()

	entries, err := scanEntriesFull(rows)
	if err != nil {
		return nil, err
	}

	// Increment access_count for returned entries
	for _, e := range entries {
		s.db.Exec("UPDATE memories SET access_count = COALESCE(access_count, 0) + 1, accessed_at = CURRENT_TIMESTAMP WHERE id = ?", e.ID)
	}

	return entries, nil
}

func (s *Store) recallLike(query string, limit int) ([]Entry, error) {
	// Build OR-ed LIKE clauses for each keyword
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		// Fall back to full query if no keywords extracted
		keywords = []string{query}
	}

	var conditions []string
	var args []any
	for _, kw := range keywords {
		pattern := "%" + kw + "%"
		conditions = append(conditions, "(content LIKE ? OR tags LIKE ?)")
		args = append(args, pattern, pattern)
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT id, category, content, tags, created_at, accessed_at
		FROM memories
		WHERE %s
		ORDER BY accessed_at DESC
		LIMIT ?
	`, strings.Join(conditions, " OR "))

	rows, err := s.db.Query(q, args...)
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

// GetLatestSessionID returns the most recent session ID from conversation history.
func (s *Store) GetLatestSessionID() (string, error) {
	var sessionID string
	err := s.db.QueryRow(`
		SELECT session_id FROM conversation_history
		ORDER BY id DESC LIMIT 1
	`).Scan(&sessionID)
	if err != nil {
		return "", err
	}
	return sessionID, nil
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

// DB returns the underlying database connection for shared use.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Consolidate removes old, low-importance memories that haven't been accessed recently.
// Keeps core, lesson, and correction memories. Removes daily/conversation/custom memories
// older than 30 days with low access counts and importance.
func (s *Store) Consolidate(_ context.Context) (int64, error) {
	result, err := s.db.Exec(`
		DELETE FROM memories
		WHERE category NOT IN ('core', 'lesson', 'correction')
		  AND julianday('now') - julianday(accessed_at) > 30
		  AND COALESCE(access_count, 0) < 2
		  AND COALESCE(importance, 0.5) < 0.5
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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

// scanEntriesFull scans rows with importance and access_count fields.
func scanEntriesFull(rows *sql.Rows) ([]Entry, error) {
	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Category, &e.Content, &e.Tags, &e.Importance,
			&e.AccessCount, &e.CreatedAt, &e.AccessedAt); err != nil {
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
// Core memories are ALWAYS included. Additional memories are keyword-matched from the query.
func (s *Store) BuildContextFromMemory(ctx context.Context, query string) string {
	seen := map[int64]bool{}

	// Always load core memories — these are identity-critical
	coreEntries, _ := s.List(ctx, CategoryCore, 10)

	// Search for query-relevant memories if we have keywords
	var relevantEntries []Entry
	if query != "" {
		keywords := extractKeywords(query)
		if len(keywords) > 0 {
			ftsQuery := strings.Join(keywords, " OR ")
			relevantEntries, _ = s.Recall(ctx, ftsQuery, 5)
		}
	}

	// If no keywords matched and no core memories, load recent memories as fallback
	if len(coreEntries) == 0 && len(relevantEntries) == 0 {
		recentEntries, _ := s.List(ctx, "", 5)
		if len(recentEntries) == 0 {
			return ""
		}
		relevantEntries = recentEntries
	}

	// Merge: core first, then relevant (deduplicated)
	var all []Entry
	for _, e := range coreEntries {
		seen[e.ID] = true
		all = append(all, e)
	}
	for _, e := range relevantEntries {
		if !seen[e.ID] {
			seen[e.ID] = true
			all = append(all, e)
		}
	}

	if len(all) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<relevant_memories>\n")
	for _, e := range all {
		b.WriteString(fmt.Sprintf("- [%s] %s\n", e.Category, e.Content))
	}
	b.WriteString("</relevant_memories>\n")
	return b.String()
}

// extractKeywords pulls meaningful words from a natural-language query for FTS5 search.
func extractKeywords(text string) []string {
	// Common stop words to filter out
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true, "was": true, "were": true,
		"be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
		"should": true, "may": true, "might": true, "can": true, "shall": true,
		"i": true, "me": true, "my": true, "we": true, "our": true, "you": true, "your": true,
		"he": true, "she": true, "it": true, "they": true, "them": true, "their": true,
		"this": true, "that": true, "these": true, "those": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "of": true, "with": true,
		"by": true, "from": true, "about": true, "into": true, "through": true,
		"and": true, "or": true, "but": true, "not": true, "no": true, "if": true,
		"what": true, "when": true, "where": true, "how": true, "who": true, "which": true, "why": true,
		"just": true, "also": true, "than": true, "then": true, "so": true, "very": true,
		"hey": true, "hi": true, "hello": true, "please": true, "thanks": true,
	}

	words := strings.Fields(strings.ToLower(text))
	var keywords []string
	seen := map[string]bool{}
	for _, w := range words {
		// Strip punctuation from edges
		w = strings.Trim(w, ".,!?;:\"'()[]{}")
		if len(w) < 2 || stop[w] || seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}
	return keywords
}
