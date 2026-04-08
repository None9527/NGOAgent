// Package persistence — vecstore provides SQL-native vector storage via sqlite-vec.
// All vector data lives in the same SQLite database as relational data,
// enabling atomic scope-filtered ANN queries in a single SQL statement.
package persistence

import (
	"database/sql"
	"fmt"
	"log/slog"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/gorm"
)

// VecStore provides vector CRUD operations backed by sqlite-vec's vec0 virtual tables.
// Each logical collection (e.g., "memories", "knowledge") maps to one virtual table.
type VecStore struct {
	db        *gorm.DB
	tableName string
	dims      int
}

// NewVecStore creates a vector store backed by a vec0 virtual table.
// Creates the table if it doesn't exist. Dimensions must match the embedder output.
func NewVecStore(db *gorm.DB, tableName string, dims int) (*VecStore, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}

	// Create vec0 virtual table with auxiliary columns for scope/session filtering.
	// The "+" prefix marks auxiliary (metadata) columns that don't affect the vector index
	// but can be used in WHERE clauses for filtered search.
	createSQL := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(
			embedding float[%d],
			+scope     TEXT,
			+source_id TEXT
		)
	`, tableName, dims)

	if _, err := sqlDB.Exec(createSQL); err != nil {
		return nil, fmt.Errorf("create vec0 table %s: %w", tableName, err)
	}

	slog.Info(fmt.Sprintf("[vecstore] initialized table=%s dims=%d", tableName, dims))
	return &VecStore{db: db, tableName: tableName, dims: dims}, nil
}

// VecSearchResult holds a single vector search result with distance.
type VecSearchResult struct {
	RowID    int64
	Distance float64 // L2 distance (lower = closer)
}

// Insert adds a vector with metadata. Returns the assigned rowid.
func (v *VecStore) Insert(embedding []float32, scope, sourceID string) (int64, error) {
	if len(embedding) != v.dims {
		return 0, fmt.Errorf("dimension mismatch: got %d, want %d", len(embedding), v.dims)
	}

	sqlDB, err := v.db.DB()
	if err != nil {
		return 0, err
	}

	blob, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return 0, fmt.Errorf("serialize vec: %w", err)
	}
	result, err := sqlDB.Exec(
		fmt.Sprintf("INSERT INTO %s(embedding, scope, source_id) VALUES(?, ?, ?)", v.tableName),
		blob, scope, sourceID,
	)
	if err != nil {
		return 0, fmt.Errorf("insert vec: %w", err)
	}

	rowID, _ := result.LastInsertId()
	return rowID, nil
}

// Search performs ANN search with optional scope filtering.
// Returns results ordered by distance (ascending = most similar first).
func (v *VecStore) Search(queryVec []float32, topK int, scope string) ([]VecSearchResult, error) {
	if len(queryVec) != v.dims {
		return nil, fmt.Errorf("dimension mismatch: got %d, want %d", len(queryVec), v.dims)
	}

	sqlDB, err := v.db.DB()
	if err != nil {
		return nil, err
	}

	blob, err := sqlite_vec.SerializeFloat32(queryVec)
	if err != nil {
		return nil, fmt.Errorf("serialize query vec: %w", err)
	}

	var rows *sql.Rows
	if scope != "" {
		rows, err = sqlDB.Query(
			fmt.Sprintf(
				"SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND scope = ? AND k = ? ORDER BY distance",
				v.tableName,
			),
			blob, scope, topK,
		)
	} else {
		rows, err = sqlDB.Query(
			fmt.Sprintf(
				"SELECT rowid, distance FROM %s WHERE embedding MATCH ? AND k = ? ORDER BY distance",
				v.tableName,
			),
			blob, topK,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("vec search: %w", err)
	}
	defer rows.Close()

	var results []VecSearchResult
	for rows.Next() {
		var r VecSearchResult
		if err := rows.Scan(&r.RowID, &r.Distance); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// Delete removes a vector by rowid.
func (v *VecStore) Delete(rowID int64) error {
	sqlDB, err := v.db.DB()
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", v.tableName),
		rowID,
	)
	return err
}

// DeleteBySource removes all vectors associated with a source ID.
func (v *VecStore) DeleteBySource(sourceID string) error {
	sqlDB, err := v.db.DB()
	if err != nil {
		return err
	}
	_, err = sqlDB.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE source_id = ?", v.tableName),
		sourceID,
	)
	return err
}

// Count returns the total number of vectors in the table.
func (v *VecStore) Count() int {
	sqlDB, err := v.db.DB()
	if err != nil {
		return 0
	}
	var count int
	sqlDB.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", v.tableName)).Scan(&count)
	return count
}

// TableName returns the underlying virtual table name.
func (v *VecStore) TableName() string { return v.tableName }
