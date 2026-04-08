// Package cron provides dynamic background task scheduling with file-based persistence.
// Each job is a directory under baseDir with job.json config and logs/ subdirectory.
// Agent can create, delete, enable, disable, and list cron jobs via manage_cron tool.
package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── Model ───────────────────────────────────────────

// Job represents a cron job stored as job.json in its directory.
type Job struct {
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"` // interval like "30s", "5m", "1h"
	Prompt    string     `json:"prompt"`
	Enabled   bool       `json:"enabled"`
	Internal  bool       `json:"internal,omitempty"` // true = system heartbeat job, cannot be deleted
	RunCount  int        `json:"run_count"`
	FailCount int        `json:"fail_count"`
	LastRun   *time.Time `json:"last_run,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// LogEntry represents a single execution log file.
type LogEntry struct {
	File    string `json:"file"`
	Time    string `json:"time"`
	Size    int64  `json:"size"`
	Success bool   `json:"success"`
}

// ─── Runner Interface ────────────────────────────────

// Runner executes a prompt in an independent agent session.
type Runner interface {
	Run(ctx context.Context, message string) error
}

// RunnerFactory creates a new Runner for each cron job execution.
// The string parameter is the job name (used for log capture).
type RunnerFactory func(jobName string) Runner

// ─── Active Job (in-memory scheduler) ────────────────

type activeJob struct {
	job    Job
	cancel context.CancelFunc
}

// ─── Manager ─────────────────────────────────────────

// Manager provides dynamic cron job management with file-based persistence.
type Manager struct {
	baseDir         string // ~/.ngoagent/cron/
	runnerFactory   RunnerFactory
	nativeCallbacks map[string]func(context.Context) // job name → Go callback (no LLM)
	active          map[string]*activeJob            // name → running job
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewManager creates a cron manager with file-based storage.
func NewManager(baseDir string, factory RunnerFactory) (*Manager, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("cron mkdir: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		baseDir:         baseDir,
		runnerFactory:   factory,
		nativeCallbacks: make(map[string]func(context.Context)),
		active:          make(map[string]*activeJob),
		ctx:             ctx,
		cancel:          cancel,
	}, nil
}

// BaseDir returns the cron base directory.
func (m *Manager) BaseDir() string { return m.baseDir }

// RegisterNative registers a pure-Go callback for a system job.
// Native callbacks execute directly without spawning an LLM agent session,
// making them ideal for maintenance tasks like KI consolidation.
func (m *Manager) RegisterNative(jobName string, fn func(context.Context)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nativeCallbacks[jobName] = fn
}

// Start loads all enabled jobs from disk and schedules them.
// Also ensures internal heartbeat jobs are registered.
func (m *Manager) Start() error {
	m.ensureHeartbeatJobs()

	jobs, err := m.List()
	if err != nil {
		return fmt.Errorf("load cron jobs: %w", err)
	}

	count := 0
	for _, j := range jobs {
		if j.Enabled {
			m.schedule(j)
			count++
		}
	}

	slog.Info(fmt.Sprintf("[cron] Started with %d active jobs", count))
	return nil
}

// ensureHeartbeatJobs registers built-in system jobs if they don't already exist.
// These share the same scheduling pipeline as user cron jobs but are marked Internal=true.
func (m *Manager) ensureHeartbeatJobs() {
	heartbeats := []Job{
		{
			Name:     "_heartbeat",
			Schedule: "30m",
			Prompt:   "执行心跳巡检：检查系统状态、整理过期临时文件、报告异常。",
			Enabled:  true,
			Internal: true,
		},
		{
			Name:     "_diary_digest",
			Schedule: "24h",
			Prompt:   "整理昨天的日记：读取 memory/diary/ 下昨日文件，汇总所有条目生成精炼摘要。",
			Enabled:  true,
			Internal: true,
		},
	}
	for _, hb := range heartbeats {
		if _, err := m.getJob(hb.Name); err != nil {
			hb.CreatedAt = time.Now()
			hb.UpdatedAt = time.Now()
			os.MkdirAll(filepath.Join(m.baseDir, hb.Name, "logs"), 0755)
			if err := m.saveJob(hb); err != nil {
				slog.Info(fmt.Sprintf("[cron] failed to create heartbeat job %q: %v", hb.Name, err))
			} else {
				slog.Info(fmt.Sprintf("[cron] registered heartbeat job %q (schedule=%s)", hb.Name, hb.Schedule))
			}
		}
	}
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
	slog.Info(fmt.Sprint("[cron] Stopped"))
}

// ─── CRUD Operations ─────────────────────────────────

// Create adds a new cron job.
func (m *Manager) Create(name, schedule, prompt string) error {
	if _, err := parseInterval(schedule); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	if name == "" || strings.ContainsAny(name, "/\\. ") {
		return fmt.Errorf("invalid job name %q (no spaces, dots, or slashes)", name)
	}

	jobDir := filepath.Join(m.baseDir, name)
	if _, err := os.Stat(jobDir); err == nil {
		return fmt.Errorf("job %q already exists", name)
	}

	// Create job directory + logs subdirectory
	if err := os.MkdirAll(filepath.Join(jobDir, "logs"), 0755); err != nil {
		return fmt.Errorf("create job dir: %w", err)
	}

	now := time.Now()
	job := Job{
		Name:      name,
		Schedule:  schedule,
		Prompt:    prompt,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := m.saveJob(job); err != nil {
		os.RemoveAll(jobDir) // Cleanup on failure
		return err
	}

	m.schedule(job)
	slog.Info(fmt.Sprintf("[cron] Created job %q (schedule=%s)", name, schedule))
	return nil
}

// Delete removes a cron job and all its logs.
// Internal (heartbeat) jobs cannot be deleted.
func (m *Manager) Delete(name string) error {
	job, err := m.getJob(name)
	if err != nil {
		return err
	}
	if job.Internal {
		return fmt.Errorf("cannot delete system job %q", name)
	}
	m.unschedule(name)
	jobDir := filepath.Join(m.baseDir, name)
	if err := os.RemoveAll(jobDir); err != nil {
		return fmt.Errorf("delete job %q: %w", name, err)
	}
	slog.Info(fmt.Sprintf("[cron] Deleted job %q", name))
	return nil
}

// Enable activates a disabled job and schedules it.
func (m *Manager) Enable(name string) error {
	job, err := m.getJob(name)
	if err != nil {
		return err
	}
	job.Enabled = true
	job.UpdatedAt = time.Now()
	if err := m.saveJob(job); err != nil {
		return err
	}
	m.schedule(job)
	slog.Info(fmt.Sprintf("[cron] Enabled job %q", name))
	return nil
}

// Disable stops and deactivates a job.
func (m *Manager) Disable(name string) error {
	m.unschedule(name)
	job, err := m.getJob(name)
	if err != nil {
		return err
	}
	job.Enabled = false
	job.UpdatedAt = time.Now()
	if err := m.saveJob(job); err != nil {
		return err
	}
	slog.Info(fmt.Sprintf("[cron] Disabled job %q", name))
	return nil
}

// List returns all cron jobs sorted by creation time.
func (m *Manager) List() ([]Job, error) {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return nil, fmt.Errorf("list cron dir: %w", err)
	}

	var jobs []Job
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		job, err := m.getJob(e.Name())
		if err != nil {
			continue // Skip broken job dirs
		}
		jobs = append(jobs, job)
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs, nil
}

// RunNow triggers a job immediately (outside its schedule).
func (m *Manager) RunNow(name string) error {
	job, err := m.getJob(name)
	if err != nil {
		return err
	}
	go m.execute(job)
	return nil
}

// ListLogs returns log entries for a specific job, newest first.
func (m *Manager) ListLogs(name string) ([]LogEntry, error) {
	logsDir := filepath.Join(m.baseDir, name, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var logs []LogEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		// Parse filename: 2026-03-14T14-30-00_ok.md or 2026-03-14T14-30-00_fail.md
		fname := e.Name()
		success := !strings.Contains(fname, "_fail")
		timeStr := strings.TrimSuffix(fname, ".md")
		timeStr = strings.TrimSuffix(timeStr, "_ok")
		timeStr = strings.TrimSuffix(timeStr, "_fail")

		logs = append(logs, LogEntry{
			File:    fname,
			Time:    timeStr,
			Size:    size,
			Success: success,
		})
	}

	// Sort newest first
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].File > logs[j].File
	})
	return logs, nil
}

// ReadLog reads a specific log file.
func (m *Manager) ReadLog(name, logFile string) (string, error) {
	// Security: prevent path traversal
	if strings.Contains(logFile, "..") || strings.Contains(logFile, "/") {
		return "", fmt.Errorf("invalid log file name")
	}
	data, err := os.ReadFile(filepath.Join(m.baseDir, name, "logs", logFile))
	if err != nil {
		return "", fmt.Errorf("read log: %w", err)
	}
	return string(data), nil
}

// ─── Internal: File I/O ──────────────────────────────

func (m *Manager) jobPath(name string) string {
	return filepath.Join(m.baseDir, name, "job.json")
}

func (m *Manager) getJob(name string) (Job, error) {
	data, err := os.ReadFile(m.jobPath(name))
	if err != nil {
		return Job{}, fmt.Errorf("job %q not found: %w", name, err)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("parse job %q: %w", name, err)
	}
	return job, nil
}

func (m *Manager) saveJob(job Job) error {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	return os.WriteFile(m.jobPath(job.Name), data, 0644)
}

// ─── Internal Scheduling ─────────────────────────────

func (m *Manager) schedule(job Job) {
	m.unschedule(job.Name) // Ensure no duplicate

	interval, err := parseInterval(job.Schedule)
	if err != nil {
		slog.Info(fmt.Sprintf("[cron] Invalid schedule for %q: %v", job.Name, err))
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
				// Re-read job from disk to get latest config
				latest, err := m.getJob(aj.job.Name)
				if err != nil {
					slog.Info(fmt.Sprintf("[cron] Job %q disappeared, stopping", aj.job.Name))
					return
				}
				if !latest.Enabled {
					continue
				}
				m.execute(latest)
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
	slog.Info(fmt.Sprintf("[cron] Executing job %q", job.Name))

	tickCtx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()

	// Native callback path: run Go function directly, no LLM cost.
	m.mu.RLock()
	nativeFn, isNative := m.nativeCallbacks[job.Name]
	m.mu.RUnlock()

	var err error
	if isNative {
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("native callback panic: %v", r)
				}
			}()
			nativeFn(tickCtx)
		}()
	} else {
		runner := m.runnerFactory(job.Name)
		err = runner.Run(tickCtx, job.Prompt)
	}

	now := time.Now()
	timestamp := now.Format("2006-01-02T15-04-05")

	// Write execution log
	suffix := "_ok"
	if err != nil {
		suffix = "_fail"
		slog.Info(fmt.Sprintf("[cron] Job %q failed: %v", job.Name, err))
	}

	logFile := filepath.Join(m.baseDir, job.Name, "logs", timestamp+suffix+".md")
	logContent := fmt.Sprintf("# %s — %s\n\n", job.Name, now.Format("2006-01-02 15:04:05"))
	logContent += fmt.Sprintf("**Schedule:** %s\n**Prompt:** %s\n\n", job.Schedule, job.Prompt)
	if err != nil {
		logContent += fmt.Sprintf("**Status:** ❌ Failed\n**Error:** %v\n", err)
	} else {
		logContent += "**Status:** ✅ Success\n"
	}
	os.WriteFile(logFile, []byte(logContent), 0644)

	// Update job stats
	job.RunCount++
	if err != nil {
		job.FailCount++
	}
	job.LastRun = &now
	job.UpdatedAt = now
	m.saveJob(job)
}

// ─── Schedule Parser ─────────────────────────────────

// parseInterval converts a schedule string to a time.Duration.
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
