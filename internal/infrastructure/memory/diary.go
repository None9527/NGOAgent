package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DiaryEntry represents a single event recorded in the daily diary.
type DiaryEntry struct {
	Time      time.Time
	SessionID string
	Scope     string // Namespace (project/domain)
	Task      string // Short task summary
	ToolCount int
	Steps     int
	Result    string // "✅ success" or "❌ failed: ..."
}

// DailyDigest holds all entries for a single day.
type DailyDigest struct {
	Date    string       // YYYY-MM-DD
	Entries []DiaryEntry // Ordered by time
	Raw     string       // Full markdown content
}

// DiaryStore provides time-axis memory via daily markdown files.
// Each day has a file: ~/.ngoagent/memory/diary/YYYY-MM-DD.md
type DiaryStore struct {
	mu  sync.Mutex
	dir string // e.g., ~/.ngoagent/memory/diary/
}

// NewDiaryStore creates a diary store at the given directory.
func NewDiaryStore(dir string) *DiaryStore {
	os.MkdirAll(dir, 0755)
	return &DiaryStore{dir: dir}
}

// Append adds an entry to today's diary file.
func (d *DiaryStore) Append(entry DiaryEntry) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	date := entry.Time.Format("2006-01-02")
	path := filepath.Join(d.dir, date+".md")

	// Check if file exists; if not, write header
	header := ""
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header = fmt.Sprintf("# 日记 %s\n\n", date)
	}

	// Format entry as markdown
	timeStr := entry.Time.Format("15:04")
	var sb strings.Builder
	if header != "" {
		sb.WriteString(header)
	}
	scopeTag := ""
	if entry.Scope != "" {
		scopeTag = fmt.Sprintf(" [%s]", entry.Scope)
	}
	sb.WriteString(fmt.Sprintf("### %s | %s%s\n", timeStr, entry.SessionID[:8], scopeTag))
	sb.WriteString(fmt.Sprintf("- **任务**: %s\n", entry.Task))
	if entry.Steps > 0 {
		sb.WriteString(fmt.Sprintf("- **步骤**: %d steps, %d tools\n", entry.Steps, entry.ToolCount))
	}
	if entry.Result != "" {
		sb.WriteString(fmt.Sprintf("- **结果**: %s\n", entry.Result))
	}
	sb.WriteString("\n")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open diary %s: %w", path, err)
	}
	defer f.Close()

	_, err = f.WriteString(sb.String())
	return err
}

// ReadDate reads the diary for a specific date.
func (d *DiaryStore) ReadDate(date string) (string, error) {
	path := filepath.Join(d.dir, date+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// ReadRange reads diaries within a date range (inclusive).
func (d *DiaryStore) ReadRange(from, to time.Time) ([]DailyDigest, error) {
	var digests []DailyDigest
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		dateStr := date.Format("2006-01-02")
		content, err := d.ReadDate(dateStr)
		if err != nil {
			continue
		}
		if content == "" {
			continue
		}
		digests = append(digests, DailyDigest{
			Date: dateStr,
			Raw:  content,
		})
	}
	return digests, nil
}

// ReadRecent reads the most recent N days of diaries, formatted for prompt injection.
func (d *DiaryStore) ReadRecent(days int) string {
	if days <= 0 {
		days = 7
	}
	to := time.Now()
	from := to.AddDate(0, 0, -days+1)

	digests, err := d.ReadRange(from, to)
	if err != nil || len(digests) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, digest := range digests {
		sb.WriteString(digest.Raw)
		sb.WriteString("\n---\n\n")
	}
	return sb.String()
}

// ListDates returns all diary dates available, newest first.
func (d *DiaryStore) ListDates() ([]string, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dates []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		date := strings.TrimSuffix(e.Name(), ".md")
		dates = append(dates, date)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

// Consolidate rewrites a day's diary with an optional LLM-generated summary.
// If summarizer is nil, no consolidation happens.
func (d *DiaryStore) Consolidate(date string, summarizer func(raw string) (string, error)) error {
	if summarizer == nil {
		return nil
	}

	raw, err := d.ReadDate(date)
	if err != nil || raw == "" {
		return err
	}

	summary, err := summarizer(raw)
	if err != nil {
		return fmt.Errorf("summarize diary %s: %w", date, err)
	}

	// Prepend summary to existing content
	d.mu.Lock()
	defer d.mu.Unlock()

	consolidated := fmt.Sprintf("# 日记 %s\n\n## 摘要\n\n%s\n\n---\n\n## 详细记录\n\n%s", date, summary, raw)
	path := filepath.Join(d.dir, date+".md")
	return os.WriteFile(path, []byte(consolidated), 0644)
}
