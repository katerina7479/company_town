package repo

import (
	"database/sql"
	"fmt"
	"time"
)

// QualityMetric is a single recorded quality check result.
type QualityMetric struct {
	ID        int
	CheckName string
	Status    string          // pass | fail | warn | error
	Output    string
	Value     sql.NullFloat64 // set for metric checks; NULL for pass/fail checks
	RunAt     time.Time
	Error     string          // non-empty when the check could not be executed
}

// QualityMetricRepo persists and retrieves quality check results.
type QualityMetricRepo struct {
	db *sql.DB
}

// NewQualityMetricRepo returns a QualityMetricRepo backed by db.
func NewQualityMetricRepo(db *sql.DB) *QualityMetricRepo {
	return &QualityMetricRepo{db: db}
}

// Record inserts a quality check result.
func (r *QualityMetricRepo) Record(m *QualityMetric) error {
	_, err := r.db.Exec(
		`INSERT INTO quality_metrics (check_name, status, output, value, run_at, error)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.CheckName, m.Status, m.Output, m.Value, m.RunAt, m.Error,
	)
	if err != nil {
		return fmt.Errorf("recording quality metric: %w", err)
	}
	return nil
}

// ListRecent returns up to limit quality metrics ordered newest-first.
func (r *QualityMetricRepo) ListRecent(limit int) ([]*QualityMetric, error) {
	rows, err := r.db.Query(
		`SELECT id, check_name, status, output, value, run_at, error
		 FROM quality_metrics
		 ORDER BY run_at DESC
		 LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing recent quality metrics: %w", err)
	}
	defer rows.Close()
	return scanMetrics(rows)
}

// ListByCheck returns up to limit results for a specific check, newest-first.
func (r *QualityMetricRepo) ListByCheck(checkName string, limit int) ([]*QualityMetric, error) {
	rows, err := r.db.Query(
		`SELECT id, check_name, status, output, value, run_at, error
		 FROM quality_metrics
		 WHERE check_name = ?
		 ORDER BY run_at DESC
		 LIMIT ?`, checkName, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing quality metrics for %s: %w", checkName, err)
	}
	defer rows.Close()
	return scanMetrics(rows)
}

// LatestPerCheck returns the single most recent result for each distinct check name.
func (r *QualityMetricRepo) LatestPerCheck() ([]*QualityMetric, error) {
	rows, err := r.db.Query(
		`SELECT id, check_name, status, output, value, run_at, error
		 FROM quality_metrics
		 WHERE id IN (
		   SELECT MAX(id) FROM quality_metrics GROUP BY check_name
		 )
		 ORDER BY check_name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing latest quality metrics per check: %w", err)
	}
	defer rows.Close()
	return scanMetrics(rows)
}

func scanMetrics(rows *sql.Rows) ([]*QualityMetric, error) {
	var metrics []*QualityMetric
	for rows.Next() {
		var m QualityMetric
		if err := rows.Scan(
			&m.ID, &m.CheckName, &m.Status, &m.Output, &m.Value, &m.RunAt, &m.Error,
		); err != nil {
			return nil, fmt.Errorf("scanning quality metric: %w", err)
		}
		metrics = append(metrics, &m)
	}
	return metrics, rows.Err()
}
