package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReplayRecord struct {
	ID             int64
	ScoreID        int64
	ReplayVersion  int16
	RNGSeed        int64
	BaseDifficulty float64
	TotalTicks     int
	Data           []byte
	InputCount     int
	CreatedAt      time.Time
}

func (db *DB) GetReplay(ctx context.Context, replayID int64) (*ReplayRecord, error) {
	var rec ReplayRecord
	err := db.Pool.QueryRow(ctx, `
		SELECT id, score_id, replay_version, rng_seed, base_difficulty, total_ticks, data, input_count, created_at
		FROM replays
		WHERE id = $1
	`, replayID).Scan(&rec.ID, &rec.ScoreID, &rec.ReplayVersion, &rec.RNGSeed, &rec.BaseDifficulty, &rec.TotalTicks, &rec.Data, &rec.InputCount, &rec.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}
