package persistence

import (
	"time"

	"gorm.io/gorm"
)

// EvoTrace records the structured execution trace of a single Run.
type EvoTrace struct {
	ID        uint      `gorm:"primarykey"`
	SessionID string    `gorm:"index"`
	RunIndex  int       // Which run in this session (1, 2, 3...)
	Steps     string    `gorm:"type:text"` // JSON: []TraceStep
	Summary   string    `gorm:"type:text"` // LLM-compressed summary (lazy-generated)
	TokensIn  int       // Total input tokens
	TokensOut int       // Total output tokens
	Duration  int       // Duration in milliseconds
	CreatedAt time.Time
}

// EvoEvaluation records the evaluation sub-agent's judgment.
type EvoEvaluation struct {
	ID        uint      `gorm:"primarykey"`
	SessionID string    `gorm:"index"`
	TraceID   uint      `gorm:"index"` // FK → EvoTrace
	Score     float64   // Quality score 0.0-1.0
	Passed    bool      // Whether evaluation passed
	ErrorType string    // intent_mismatch | param_wrong | tool_wrong | capability_gap | quality_low | ""
	Issues    string    `gorm:"type:text"` // JSON: []Issue
	Feedback  string    `gorm:"type:text"` // Raw user feedback text
	Model     string    // Model used for evaluation
	CreatedAt time.Time
}

// EvoRepair records a single repair attempt.
type EvoRepair struct {
	ID          uint      `gorm:"primarykey"`
	SessionID   string    `gorm:"index"`
	EvalID      uint      `gorm:"index"` // FK → EvoEvaluation
	Strategy    string    // param_fix | tool_swap | re_route | iterate | escalate
	RepairTrace string    `gorm:"type:text"` // JSON: repair execution TraceStep[]
	Success     bool
	NewScore    float64   // Post-repair score
	TokensUsed  int
	Duration    int       // Duration in milliseconds
	CreatedAt   time.Time
}

// EvoStore provides CRUD operations for evolution data.
type EvoStore struct {
	db *gorm.DB
}

// NewEvoStore creates an evolution data store.
func NewEvoStore(db *gorm.DB) *EvoStore {
	db.AutoMigrate(&EvoTrace{}, &EvoEvaluation{}, &EvoRepair{})
	return &EvoStore{db: db}
}

// SaveTrace persists an execution trace.
func (s *EvoStore) SaveTrace(trace *EvoTrace) error {
	return s.db.Create(trace).Error
}

// SaveEvaluation persists an evaluation result.
func (s *EvoStore) SaveEvaluation(eval *EvoEvaluation) error {
	return s.db.Create(eval).Error
}

// SaveRepair persists a repair record.
func (s *EvoStore) SaveRepair(repair *EvoRepair) error {
	return s.db.Create(repair).Error
}

// GetTrace retrieves the latest trace for a session.
func (s *EvoStore) GetTrace(sessionID string) (*EvoTrace, error) {
	var trace EvoTrace
	err := s.db.Where("session_id = ?", sessionID).Order("created_at DESC").First(&trace).Error
	if err != nil {
		return nil, err
	}
	return &trace, nil
}

// GetTraceByID retrieves a specific trace.
func (s *EvoStore) GetTraceByID(id uint) (*EvoTrace, error) {
	var trace EvoTrace
	if err := s.db.First(&trace, id).Error; err != nil {
		return nil, err
	}
	return &trace, nil
}

// CountRepairs returns the number of repair attempts for a session.
func (s *EvoStore) CountRepairs(sessionID string) int {
	var count int64
	s.db.Model(&EvoRepair{}).Where("session_id = ?", sessionID).Count(&count)
	return int(count)
}

// CleanOld removes evo data older than the specified number of days.
func (s *EvoStore) CleanOld(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("created_at < ?", cutoff).Delete(&EvoRepair{}).Error; err != nil {
			return err
		}
		if err := tx.Where("created_at < ?", cutoff).Delete(&EvoEvaluation{}).Error; err != nil {
			return err
		}
		return tx.Where("created_at < ?", cutoff).Delete(&EvoTrace{}).Error
	})
}

// ═══════════════════════════════════════════
// Aggregated Statistics
// ═══════════════════════════════════════════

// EvoStats holds aggregated evolution metrics.
type EvoStats struct {
	TotalEvals        int            `json:"total_evals"`
	PassedEvals       int            `json:"passed_evals"`
	SuccessRate       float64        `json:"success_rate"`
	AvgScore          float64        `json:"avg_score"`
	RepairAttempts    int            `json:"repair_attempts"`
	RepairSuccesses   int            `json:"repair_successes"`
	RepairSuccessRate float64        `json:"repair_success_rate"`
	TopErrorTypes     []ErrorCount   `json:"top_error_types"`
	TopStrategies     []StrategyCount `json:"top_strategies"`
}

// ErrorCount pairs an error type with its frequency.
type ErrorCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

// StrategyCount pairs a strategy with its frequency and success count.
type StrategyCount struct {
	Strategy string `json:"strategy"`
	Total    int    `json:"total"`
	Success  int    `json:"success"`
}

// GetStats returns aggregated evolution metrics for the last N days.
func (s *EvoStore) GetStats(days int) (*EvoStats, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	stats := &EvoStats{}

	// Evaluations
	var evals []EvoEvaluation
	if err := s.db.Where("created_at >= ?", cutoff).Find(&evals).Error; err != nil {
		return nil, err
	}

	stats.TotalEvals = len(evals)
	var scoreSum float64
	for _, e := range evals {
		scoreSum += e.Score
		if e.Passed {
			stats.PassedEvals++
		}
	}
	if stats.TotalEvals > 0 {
		stats.SuccessRate = float64(stats.PassedEvals) / float64(stats.TotalEvals)
		stats.AvgScore = scoreSum / float64(stats.TotalEvals)
	}

	// Error type distribution
	errorCounts := make(map[string]int)
	for _, e := range evals {
		if e.ErrorType != "" {
			errorCounts[e.ErrorType]++
		}
	}
	for t, c := range errorCounts {
		stats.TopErrorTypes = append(stats.TopErrorTypes, ErrorCount{Type: t, Count: c})
	}

	// Repairs
	var repairs []EvoRepair
	if err := s.db.Where("created_at >= ?", cutoff).Find(&repairs).Error; err != nil {
		return nil, err
	}

	stats.RepairAttempts = len(repairs)
	strategyCounts := make(map[string]*StrategyCount)
	for _, r := range repairs {
		if r.Success {
			stats.RepairSuccesses++
		}
		sc, ok := strategyCounts[r.Strategy]
		if !ok {
			sc = &StrategyCount{Strategy: r.Strategy}
			strategyCounts[r.Strategy] = sc
		}
		sc.Total++
		if r.Success {
			sc.Success++
		}
	}
	if stats.RepairAttempts > 0 {
		stats.RepairSuccessRate = float64(stats.RepairSuccesses) / float64(stats.RepairAttempts)
	}
	for _, sc := range strategyCounts {
		stats.TopStrategies = append(stats.TopStrategies, *sc)
	}

	return stats, nil
}
