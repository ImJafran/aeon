package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestStoreAndRecall(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Store a memory
	id, err := store.MemStore(ctx, CategoryCore, "My server IP is 192.168.1.100", "server,ip", 0)
	if err != nil {
		t.Fatalf("store error: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	// Recall it
	entries, err := store.Recall(ctx, "server IP", 5)
	if err != nil {
		t.Fatalf("recall error: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one result")
	}
	if entries[0].Content != "My server IP is 192.168.1.100" {
		t.Errorf("unexpected content: %s", entries[0].Content)
	}
}

func TestStoreAndGet(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	id, _ := store.MemStore(ctx, CategoryDaily, "Had a meeting about project X", "meeting", 0)

	entry, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if entry.Category != CategoryDaily {
		t.Errorf("expected 'daily' category, got '%s'", entry.Category)
	}
}

func TestCount(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	count, _ := store.Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 entries, got %d", count)
	}

	store.MemStore(ctx, CategoryCore, "entry 1", "", 0)
	store.MemStore(ctx, CategoryCore, "entry 2", "", 0)

	count, _ = store.Count(ctx)
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}
}

func TestForget(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	id, _ := store.MemStore(ctx, CategoryCustom, "forget me", "", 0)

	err := store.Forget(ctx, id)
	if err != nil {
		t.Fatalf("forget error: %v", err)
	}

	count, _ := store.Count(ctx)
	if count != 0 {
		t.Errorf("expected 0 entries after forget, got %d", count)
	}
}

func TestConversationHistory(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	store.SaveHistory(ctx, "session1", "user", "hello")
	store.SaveHistory(ctx, "session1", "assistant", "hi there")
	store.SaveHistory(ctx, "session2", "user", "different session")

	history, err := store.GetHistory(ctx, "session1", 10)
	if err != nil {
		t.Fatalf("get history error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0]["role"] != "user" || history[0]["content"] != "hello" {
		t.Errorf("unexpected first entry: %v", history[0])
	}
}

func TestClearHistory(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	store.SaveHistory(ctx, "session1", "user", "hello")
	store.ClearHistory(ctx, "session1")

	history, _ := store.GetHistory(ctx, "session1", 10)
	if len(history) != 0 {
		t.Errorf("expected empty history after clear, got %d entries", len(history))
	}
}

func TestList(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	store.MemStore(ctx, CategoryCore, "core entry", "", 0)
	store.MemStore(ctx, CategoryDaily, "daily entry", "", 0)

	// List all
	all, _ := store.List(ctx, "", 10)
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}

	// List by category
	core, _ := store.List(ctx, CategoryCore, 10)
	if len(core) != 1 {
		t.Errorf("expected 1 core entry, got %d", len(core))
	}
}

func TestBuildContextFromMemory(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	store.MemStore(ctx, CategoryCore, "The database password is in /etc/secrets", "db,password", 0)

	result := store.BuildContextFromMemory(ctx, "database password")
	if result == "" {
		t.Error("expected non-empty context")
	}
}

func TestBuildContextFromMemory_StopWordsOnly(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Store core memories (like a user's identity)
	store.MemStore(ctx, CategoryCore, "The user's name is Alice", "name,user", 0)
	store.MemStore(ctx, CategoryCore, "Alice lives in Berlin, Germany", "location", 0)

	// "who am I" — all words are stop words, extractKeywords returns empty
	// But core memories should still be injected
	result := store.BuildContextFromMemory(ctx, "who am I")
	if result == "" {
		t.Error("expected core memories even when query has only stop words")
	}
	if !contains(result, "Alice") {
		t.Errorf("expected memory about Alice, got: %s", result)
	}
}

func TestBuildContextFromMemory_EmptyDB(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// No memories stored — should return empty
	result := store.BuildContextFromMemory(ctx, "who am I")
	if result != "" {
		t.Errorf("expected empty context for empty DB, got: %s", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
