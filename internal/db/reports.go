package db

import (
	"context"
	"strings"
)

func (db *DB) InsertReport(ctx context.Context, scoreID int64, reporterName string, reason string) error {
	reporterName = strings.TrimSpace(reporterName)
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO reports (score_id, reporter_name, reason, created_at)
		VALUES ($1, NULLIF($2, ''), $3, NOW())
		ON CONFLICT (score_id, reporter_name) DO NOTHING
	`, scoreID, reporterName, reason)
	return err
}

func (db *DB) CountReports(ctx context.Context, scoreID int64) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM reports WHERE score_id = $1
	`, scoreID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
