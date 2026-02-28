package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Consolidator merges old memories into concise summaries.
type Consolidator struct {
	store     *Store
	summarize func(ctx context.Context, memories []Entry) (string, error)
	maxAge    time.Duration
}

// SummarizeFunc takes a batch of memories and returns a consolidated summary.
type SummarizeFunc func(ctx context.Context, memories []Entry) (string, error)

// NewConsolidator creates a memory consolidator.
// summarize is called to produce a summary of a group of memories.
// If nil, a simple concatenation is used as fallback.
func NewConsolidator(store *Store, summarize SummarizeFunc) *Consolidator {
	if summarize == nil {
		summarize = defaultSummarize
	}
	return &Consolidator{
		store:     store,
		summarize: summarize,
		maxAge:    7 * 24 * time.Hour, // 7 days
	}
}

// Consolidate finds old daily/conversation memories, groups by tags, summarizes, and replaces.
func (c *Consolidator) Consolidate(ctx context.Context) (int, error) {
	cutoff := time.Now().Add(-c.maxAge)

	// Find old daily and conversation memories
	old, err := c.store.ListOlderThan(ctx, cutoff, 100)
	if err != nil {
		return 0, fmt.Errorf("listing old memories: %w", err)
	}

	if len(old) == 0 {
		return 0, nil
	}

	// Group by primary tag
	groups := make(map[string][]Entry)
	for _, e := range old {
		key := primaryTag(e.Tags)
		groups[key] = append(groups[key], e)
	}

	consolidated := 0
	for tag, entries := range groups {
		if len(entries) < 2 {
			continue // don't consolidate singles
		}

		summary, err := c.summarize(ctx, entries)
		if err != nil {
			continue // skip this group on error
		}

		// Store consolidated memory as core
		_, err = c.store.MemStore(ctx, CategoryCore, summary, tag)
		if err != nil {
			continue
		}

		// Delete originals
		for _, e := range entries {
			c.store.Forget(ctx, e.ID)
		}
		consolidated += len(entries)
	}

	return consolidated, nil
}

// ListOlderThan returns daily/conversation memories older than the given time.
func (s *Store) ListOlderThan(ctx context.Context, olderThan time.Time, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, category, content, tags, created_at, accessed_at
		FROM memories
		WHERE category IN ('daily', 'conversation')
		AND created_at < ?
		ORDER BY created_at ASC
		LIMIT ?
	`, olderThan.Format(time.DateTime), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

func primaryTag(tags string) string {
	parts := strings.Split(tags, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return "general"
}

func defaultSummarize(_ context.Context, memories []Entry) (string, error) {
	var parts []string
	for _, m := range memories {
		parts = append(parts, m.Content)
	}
	return fmt.Sprintf("[Consolidated from %d memories] %s", len(memories), strings.Join(parts, "; ")), nil
}
