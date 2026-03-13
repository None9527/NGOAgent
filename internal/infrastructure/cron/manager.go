// Package cron provides dynamic background task scheduling with SQLite persistence.
// Replaces the old heartbeat engine with a fully manageable cron system.
// Agent can create, delete, enable, disable, and list cron jobs via manage_cron tool.
package cron

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ─── Model ───────────────────────────────────────────

// Job represents a persistent cron job stored in SQLite.
type Job struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	Schedule  string    `gorm:"not null" json:"schedule"` // interval like "30s" or cron "*/5 * * * *"
	Prompt    string    `gorm:"type:text;not null" json:"prompt"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	RunCount  int       `json:"run_count"`
	FailCount int       `json:"fail_count"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ─── Runner Interface ────────────────────────────────

// Runner executes a prompt in an independent agent session.
type Runner interface {
	Run(ctx context.Context, message string) error
}

// RunnerFactory creates a new Runner for each cron job execution.
type RunnerFactory func() Runner

// ─── Active Job (in-memory scheduler) ────────────────

type activeJob struct {
	job    Job
	cancel context.CancelFunc
}

// ─── Manager ─────────────────────────────────────────

// Manager provides dynamic cron job management with SQLite persistence.
type Manager struct {
	db            *gorm.DB
	runnerFactory RunnerFactory
	active        map[string]*activeJob // name → running job
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewManager creates a cron manager. Call Start() to begin scheduling.
func NewManager(db *gorm.DB, factory RunnerFactory) (*Manager, error) {
	// Auto-migrate the Job table
	if err := db.AutoMigrate(&Job{}); err != nil {
		return nil, fmt.Errorf("cron migrate: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		db:            db,
		runnerFactory: factory,
		active:        make(map[string]*activeJob),
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// Start loads all enabled jobs from DB and schedules them.
func (m *Manager) Start() error {
	var jobs []Job
	if err := m.db.Where("enabled = ?", true).Find(&jobs).Error; err != nil {
		return fmt.Errorf("load cron jobs: %w", err)
	}

	for _, j := range jobs {
		m.schedule(j)
	}

	log.Printf("[cron] Started with %d active jobs", len(jobs))
	return nil
}

// Stop cancels all running job schedules.
func (m *Manager) Stop() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, aj := range m.active {
		aj.cancel()
		delete(m.active, name)
	}
	log.Println("[cron] Stopped")
}

// ─── CRUD Operations (called by manage_cron tool) ────

// Create adds a new cron job.
func (m *Manager) Create(name, schedule, prompt string) error {
	if _, err := parseInterval(schedule); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}

	job := Job{
		ID:       uuid.New().String(),
		Name:     name,
		Schedule: schedule,
		Prompt:   prompt,
		Enabled:  true,
	}

	if err := m.db.Create(&job).Error; err != nil {
		return fmt.Errorf("create job %q: %w", name, err)
	}

	m.schedule(job)
	log.Printf("[cron] Created job %q (schedule=%s)", name, schedule)
	return nil
}

// Delete removes a cron job.
func (m *Manager) Delete(name string) error {
	m.unschedule(name)
	if err := m.db.Where("name = ?", name).Delete(&Job{}).Error; err != nil {
		return fmt.Errorf("delete job %q: %w", name, err)
	}
	log.Printf("[cron] Deleted job %q", name)
	return nil
}

// Enable activates a disabled job and schedules it.
func (m *Manager) Enable(name string) error {
	var job Job
	if err := m.db.Where("name = ?", name).First(&job).Error; err != nil {
		return fmt.Errorf("job %q not found: %w", name, err)
	}
	m.db.Model(&job).Update("enabled", true)
	job.Enabled = true
	m.schedule(job)
	log.Printf("[cron] Enabled job %q", name)
	return nil
}

// Disable stops and deactivates a job.
func (m *Manager) Disable(name string) error {
	m.unschedule(name)
	if err := m.db.Model(&Job{}).Where("name = ?", name).Update("enabled", false).Error; err != nil {
		return fmt.Errorf("disable job %q: %w", name, err)
	}
	log.Printf("[cron] Disabled job %q", name)
	return nil
}

// List returns all cron jobs.
func (m *Manager) List() ([]Job, error) {
	var jobs []Job
	if err := m.db.Order("created_at ASC").Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return jobs, nil
}

// RunNow triggers a job immediately (outside its schedule).
func (m *Manager) RunNow(name string) error {
	var job Job
	if err := m.db.Where("name = ?", name).First(&job).Error; err != nil {
		return fmt.Errorf("job %q not found: %w", name, err)
	}
	go m.execute(job)
	return nil
}

// ─── Internal Scheduling ─────────────────────────────

func (m *Manager) schedule(job Job) {
	m.unschedule(job.Name) // Ensure no duplicate

	interval, err := parseInterval(job.Schedule)
	if err != nil {
		log.Printf("[cron] Invalid schedule for %q: %v", job.Name, err)
		return
	}

	ctx, cancel := context.WithCancel(m.ctx)
	aj := &activeJob{job: job, cancel: cancel}

	m.mu.Lock()
	m.active[job.Name] = aj
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.execute(aj.job)
			}
		}
	}()
}

func (m *Manager) unschedule(name string) {
	m.mu.Lock()
	if aj, ok := m.active[name]; ok {
		aj.cancel()
		delete(m.active, name)
	}
	m.mu.Unlock()
}

func (m *Manager) execute(job Job) {
	log.Printf("[cron] Executing job %q", job.Name)

	tickCtx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()

	runner := m.runnerFactory()
	now := time.Now()

	if err := runner.Run(tickCtx, job.Prompt); err != nil {
		log.Printf("[cron] Job %q failed: %v", job.Name, err)
		m.db.Model(&Job{}).Where("name = ?", job.Name).Updates(map[string]any{
			"fail_count": gorm.Expr("fail_count + 1"),
			"last_run":   now,
		})
		return
	}

	m.db.Model(&Job{}).Where("name = ?", job.Name).Updates(map[string]any{
		"run_count": gorm.Expr("run_count + 1"),
		"last_run":  now,
	})
}

// ─── Schedule Parser ─────────────────────────────────

// parseInterval converts a schedule string to a time.Duration.
// Supports Go duration format: "30s", "5m", "1h", etc.
// Future: add cron expression support via robfig/cron.
func parseInterval(schedule string) (time.Duration, error) {
	d, err := time.ParseDuration(schedule)
	if err != nil {
		return 0, fmt.Errorf("unsupported schedule format %q (use Go duration like '30s', '5m')", schedule)
	}
	if d < 10*time.Second {
		return 0, fmt.Errorf("interval too short: minimum 10s, got %v", d)
	}
	return d, nil
}
