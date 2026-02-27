package scheduler

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sched, err := New(db, logger)
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	return sched
}

func TestCreateAndList(t *testing.T) {
	sched := setupTestScheduler(t)

	id, err := sched.Create("backup", "every 1h", "backup_skill", "", `{"path": "/data"}`)
	if err != nil {
		t.Fatalf("create error: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive ID, got %d", id)
	}

	jobs, err := sched.List(false)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "backup" {
		t.Errorf("expected name 'backup', got '%s'", jobs[0].Name)
	}
}

func TestPauseResume(t *testing.T) {
	sched := setupTestScheduler(t)

	id, _ := sched.Create("check", "every 5m", "", "curl -s https://example.com", "{}")

	// Pause
	if err := sched.Pause(id); err != nil {
		t.Fatalf("pause error: %v", err)
	}

	job, _ := sched.Get(id)
	if job.Enabled {
		t.Error("expected job to be disabled after pause")
	}

	// Resume
	if err := sched.Resume(id); err != nil {
		t.Fatalf("resume error: %v", err)
	}

	job, _ = sched.Get(id)
	if !job.Enabled {
		t.Error("expected job to be enabled after resume")
	}
}

func TestDelete(t *testing.T) {
	sched := setupTestScheduler(t)

	id, _ := sched.Create("temp", "every 1h", "", "echo hello", "{}")
	if err := sched.Delete(id); err != nil {
		t.Fatalf("delete error: %v", err)
	}

	count, _ := sched.Count()
	if count != 0 {
		t.Errorf("expected 0 jobs after delete, got %d", count)
	}
}

func TestRecordSuccessAndFailure(t *testing.T) {
	sched := setupTestScheduler(t)

	id, _ := sched.Create("test_job", "every 1h", "", "echo hi", "{}")

	// Record success
	if err := sched.RecordSuccess(id); err != nil {
		t.Fatalf("record success error: %v", err)
	}

	job, _ := sched.Get(id)
	if job.LastRun == nil {
		t.Error("expected last_run to be set after success")
	}
	if job.FailCount != 0 {
		t.Errorf("expected 0 failures after success, got %d", job.FailCount)
	}

	// Record failures up to auto-pause threshold
	for i := 0; i < maxConsecutiveFailures; i++ {
		sched.RecordFailure(id)
	}

	job, _ = sched.Get(id)
	if job.Enabled {
		t.Error("expected job to be auto-paused after max failures")
	}
	if job.FailCount != maxConsecutiveFailures {
		t.Errorf("expected %d failures, got %d", maxConsecutiveFailures, job.FailCount)
	}
}

func TestComputeNextRun(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		schedule string
		expected time.Time
		wantErr  bool
	}{
		{"every 5m", now.Add(5 * time.Minute), false},
		{"every 2h", now.Add(2 * time.Hour), false},
		{"every 1d", now.Add(24 * time.Hour), false},
		{"every 30s", now.Add(30 * time.Second), false},
		{"hourly", now.Add(1 * time.Hour), false},
		{"daily", now.Add(24 * time.Hour), false},
		{"weekly", now.Add(7 * 24 * time.Hour), false},
		{"invalid", time.Time{}, true},
		{"every abc", time.Time{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.schedule, func(t *testing.T) {
			next, err := computeNextRun(tt.schedule, now)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !next.Equal(tt.expected) {
				t.Errorf("got %v, want %v", next, tt.expected)
			}
		})
	}
}

func TestListEnabledOnly(t *testing.T) {
	sched := setupTestScheduler(t)

	id1, _ := sched.Create("enabled_job", "every 1h", "", "echo 1", "{}")
	id2, _ := sched.Create("disabled_job", "every 1h", "", "echo 2", "{}")
	sched.Pause(id2)

	_ = id1

	all, _ := sched.List(false)
	if len(all) != 2 {
		t.Errorf("expected 2 total jobs, got %d", len(all))
	}

	enabled, _ := sched.List(true)
	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled job, got %d", len(enabled))
	}
}

func TestCount(t *testing.T) {
	sched := setupTestScheduler(t)

	count, _ := sched.Count()
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	sched.Create("job1", "every 1h", "", "echo 1", "{}")
	sched.Create("job2", "every 2h", "", "echo 2", "{}")

	count, _ = sched.Count()
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}
