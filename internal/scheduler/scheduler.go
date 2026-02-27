package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job represents a scheduled task.
type Job struct {
	ID        int64
	Name      string
	Schedule  string // cron expression or "every Xm/Xh/Xd"
	SkillName string // skill to run, or empty for shell command
	Command   string // shell command if no skill
	Params    string // JSON params for skill
	Enabled   bool
	LastRun   *time.Time
	NextRun   time.Time
	FailCount int
	CreatedAt time.Time
}

const maxConsecutiveFailures = 5

// Scheduler manages cron-like scheduled jobs.
type Scheduler struct {
	db        *sql.DB
	logger    *slog.Logger
	mu        sync.Mutex
	running   map[int64]context.CancelFunc // active job cancellers
	maxConc   int                          // max concurrent jobs
	onTrigger func(job Job)                // callback when job fires
}

// New creates a scheduler backed by the given SQLite database.
func New(db *sql.DB, logger *slog.Logger) (*Scheduler, error) {
	if err := initCronSchema(db); err != nil {
		return nil, fmt.Errorf("initializing cron schema: %w", err)
	}

	return &Scheduler{
		db:      db,
		logger:  logger,
		running: make(map[int64]context.CancelFunc),
		maxConc: 3,
	}, nil
}

func initCronSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cron_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			schedule TEXT NOT NULL,
			skill_name TEXT DEFAULT '',
			command TEXT DEFAULT '',
			params TEXT DEFAULT '{}',
			enabled BOOLEAN DEFAULT 1,
			last_run DATETIME,
			next_run DATETIME NOT NULL,
			fail_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

// SetMaxConcurrent sets the max number of concurrent cron jobs.
func (s *Scheduler) SetMaxConcurrent(n int) {
	s.maxConc = n
}

// OnTrigger sets the callback invoked when a job fires.
func (s *Scheduler) OnTrigger(fn func(Job)) {
	s.onTrigger = fn
}

// Create adds a new scheduled job.
func (s *Scheduler) Create(name, schedule, skillName, command, params string) (int64, error) {
	nextRun, err := computeNextRun(schedule, time.Now())
	if err != nil {
		return 0, fmt.Errorf("invalid schedule: %w", err)
	}

	result, err := s.db.Exec(
		`INSERT INTO cron_jobs (name, schedule, skill_name, command, params, next_run)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		name, schedule, skillName, command, params, nextRun,
	)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// List returns all jobs, optionally filtered by enabled status.
func (s *Scheduler) List(enabledOnly bool) ([]Job, error) {
	query := "SELECT id, name, schedule, skill_name, command, params, enabled, last_run, next_run, fail_count, created_at FROM cron_jobs"
	if enabledOnly {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY next_run ASC"

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanJobs(rows)
}

// Get returns a job by ID.
func (s *Scheduler) Get(id int64) (*Job, error) {
	row := s.db.QueryRow(
		"SELECT id, name, schedule, skill_name, command, params, enabled, last_run, next_run, fail_count, created_at FROM cron_jobs WHERE id = ?",
		id,
	)
	return scanJob(row)
}

// Pause disables a job.
func (s *Scheduler) Pause(id int64) error {
	_, err := s.db.Exec("UPDATE cron_jobs SET enabled = 0 WHERE id = ?", id)
	return err
}

// Resume enables a job and resets fail count.
func (s *Scheduler) Resume(id int64) error {
	nextRun := time.Now()
	_, err := s.db.Exec("UPDATE cron_jobs SET enabled = 1, fail_count = 0, next_run = ? WHERE id = ?", nextRun, id)
	return err
}

// Delete removes a job.
func (s *Scheduler) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM cron_jobs WHERE id = ?", id)
	return err
}

// RecordSuccess updates a job after successful execution.
func (s *Scheduler) RecordSuccess(id int64) error {
	now := time.Now()
	job, err := s.Get(id)
	if err != nil {
		return err
	}

	nextRun, _ := computeNextRun(job.Schedule, now)
	_, err = s.db.Exec(
		"UPDATE cron_jobs SET last_run = ?, next_run = ?, fail_count = 0 WHERE id = ?",
		now, nextRun, id,
	)
	return err
}

// RecordFailure increments failure count and auto-pauses if threshold exceeded.
func (s *Scheduler) RecordFailure(id int64) error {
	now := time.Now()
	job, err := s.Get(id)
	if err != nil {
		return err
	}

	newFails := job.FailCount + 1
	enabled := job.Enabled
	if newFails >= maxConsecutiveFailures {
		enabled = false
		s.logger.Warn("auto-pausing cron job due to consecutive failures",
			"job_id", id, "name", job.Name, "failures", newFails)
	}

	nextRun, _ := computeNextRun(job.Schedule, now)
	_, err = s.db.Exec(
		"UPDATE cron_jobs SET last_run = ?, next_run = ?, fail_count = ?, enabled = ? WHERE id = ?",
		now, nextRun, newFails, enabled, id,
	)
	return err
}

// Count returns the total number of jobs.
func (s *Scheduler) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM cron_jobs").Scan(&count)
	return count, err
}

// Start begins the scheduler tick loop.
func (s *Scheduler) Start(ctx context.Context) {
	go s.tickLoop(ctx)
}

func (s *Scheduler) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Also check immediately on start
	s.checkAndFire(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndFire(ctx)
		}
	}
}

func (s *Scheduler) checkAndFire(ctx context.Context) {
	now := time.Now()

	rows, err := s.db.Query(
		"SELECT id, name, schedule, skill_name, command, params, enabled, last_run, next_run, fail_count, created_at FROM cron_jobs WHERE enabled = 1 AND next_run <= ?",
		now,
	)
	if err != nil {
		s.logger.Error("checking cron jobs", "error", err)
		return
	}
	defer rows.Close()

	jobs, _ := scanJobs(rows)

	for _, job := range jobs {
		s.mu.Lock()
		runCount := len(s.running)
		s.mu.Unlock()

		if runCount >= s.maxConc {
			s.logger.Warn("max concurrent cron jobs reached, skipping", "job", job.Name)
			break
		}

		// Check if already running (no stacking)
		s.mu.Lock()
		if _, running := s.running[job.ID]; running {
			s.mu.Unlock()
			s.logger.Debug("job already running, skipping", "job", job.Name)
			continue
		}
		s.mu.Unlock()

		s.fireJob(ctx, job)
	}
}

func (s *Scheduler) fireJob(ctx context.Context, job Job) {
	jobCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)

	s.mu.Lock()
	s.running[job.ID] = cancel
	s.mu.Unlock()

	s.logger.Info("firing cron job", "id", job.ID, "name", job.Name)

	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.running, job.ID)
			s.mu.Unlock()
		}()

		if s.onTrigger != nil {
			s.onTrigger(job)
		}

		// Auto-disable one-shot reminders after they fire
		if IsOneShot(job.Schedule) {
			s.logger.Info("one-shot reminder fired, disabling", "id", job.ID, "name", job.Name)
			s.Pause(job.ID)
		}

		_ = jobCtx // context passed to trigger if needed
	}()
}

// StopAll cancels all running jobs.
func (s *Scheduler) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, cancel := range s.running {
		cancel()
		delete(s.running, id)
	}
}

// RunningCount returns the number of currently running jobs.
func (s *Scheduler) RunningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}

// IsOneShot returns true if the schedule is a one-time reminder ("in ..." or "at ...").
func IsOneShot(schedule string) bool {
	s := strings.TrimSpace(strings.ToLower(schedule))
	return strings.HasPrefix(s, "in ") || strings.HasPrefix(s, "at ")
}

// computeNextRun calculates the next run time from a schedule expression.
// Supports: "every Xm", "every Xh", "every Xd", "in Xm/Xh" (one-shot), "at HH:MM" (one-shot),
// or simple interval strings like "hourly", "daily", "weekly".
func computeNextRun(schedule string, from time.Time) (time.Time, error) {
	schedule = strings.TrimSpace(strings.ToLower(schedule))

	// One-shot: "in 10m", "in 2h", "in 30s"
	if strings.HasPrefix(schedule, "in ") {
		return parseInterval(strings.TrimPrefix(schedule, "in "), from)
	}

	// One-shot: "at 16:50", "at 4:50pm", "at 4:50 pm"
	if strings.HasPrefix(schedule, "at ") {
		return parseAtTime(strings.TrimPrefix(schedule, "at "), from)
	}

	// Recurring interval: "every 5m", "every 1h", "every 2d"
	if strings.HasPrefix(schedule, "every ") {
		return parseInterval(strings.TrimPrefix(schedule, "every "), from)
	}

	// Simple cron-like: "hourly", "daily", "weekly"
	switch schedule {
	case "hourly":
		return from.Add(1 * time.Hour), nil
	case "daily":
		return from.Add(24 * time.Hour), nil
	case "weekly":
		return from.Add(7 * 24 * time.Hour), nil
	}

	return time.Time{}, fmt.Errorf("unsupported schedule format: %s (use 'in Xm', 'at HH:MM', 'every Xm/Xh/Xd', or 'hourly/daily/weekly')", schedule)
}

// parseInterval parses a duration like "10m", "2h", "1d", "30s".
func parseInterval(interval string, from time.Time) (time.Time, error) {
	interval = strings.TrimSpace(interval)

	if len(interval) < 2 {
		return time.Time{}, fmt.Errorf("invalid interval: %s", interval)
	}

	unit := interval[len(interval)-1]
	numStr := interval[:len(interval)-1]
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid interval number: %s", numStr)
	}

	switch unit {
	case 'm':
		return from.Add(time.Duration(num) * time.Minute), nil
	case 'h':
		return from.Add(time.Duration(num) * time.Hour), nil
	case 'd':
		return from.Add(time.Duration(num) * 24 * time.Hour), nil
	case 's':
		return from.Add(time.Duration(num) * time.Second), nil
	default:
		return time.Time{}, fmt.Errorf("unknown interval unit: %c (use m, h, d, or s)", unit)
	}
}

// parseAtTime parses a time-of-day like "16:50", "4:50pm", "4:50 pm".
// If the time has already passed today, schedules for tomorrow.
func parseAtTime(timeStr string, from time.Time) (time.Time, error) {
	timeStr = strings.TrimSpace(timeStr)
	timeStr = strings.ReplaceAll(timeStr, " ", "") // "4:50 pm" → "4:50pm"

	// Try 24h format first: "16:50"
	layouts := []string{"15:04", "3:04pm", "3:04PM", "3pm", "3PM", "15"}

	var parsed time.Time
	var err error
	for _, layout := range layouts {
		parsed, err = time.Parse(layout, timeStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time: %s (use HH:MM, H:MMpm, or Hpm)", timeStr)
	}

	// Build target time today
	target := time.Date(from.Year(), from.Month(), from.Day(),
		parsed.Hour(), parsed.Minute(), 0, 0, from.Location())

	// If target has already passed, schedule for tomorrow
	if target.Before(from) {
		target = target.Add(24 * time.Hour)
	}

	return target, nil
}

func scanJobs(rows *sql.Rows) ([]Job, error) {
	var jobs []Job
	for rows.Next() {
		var j Job
		var lastRun sql.NullTime
		if err := rows.Scan(&j.ID, &j.Name, &j.Schedule, &j.SkillName, &j.Command,
			&j.Params, &j.Enabled, &lastRun, &j.NextRun, &j.FailCount, &j.CreatedAt); err != nil {
			continue
		}
		if lastRun.Valid {
			j.LastRun = &lastRun.Time
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func scanJob(row *sql.Row) (*Job, error) {
	var j Job
	var lastRun sql.NullTime
	if err := row.Scan(&j.ID, &j.Name, &j.Schedule, &j.SkillName, &j.Command,
		&j.Params, &j.Enabled, &lastRun, &j.NextRun, &j.FailCount, &j.CreatedAt); err != nil {
		return nil, err
	}
	if lastRun.Valid {
		j.LastRun = &lastRun.Time
	}
	return &j, nil
}
