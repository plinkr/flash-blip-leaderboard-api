package db

import (
	"context"
	"flash-blip-leaderboard-api/internal/models"
	"time"
)

type ScoreRecord struct {
	PlayerName     string
	Score          int64
	TotalTicks     int
	ClientTS       int64
	IP             string
	Nonce          string
	ReplayVersion  int
	RNGSeed        int64
	BaseDifficulty float64
	ReplayData     []byte
	InputCount     int
}

func (db *DB) InsertScoreWithReplay(ctx context.Context, rec ScoreRecord) (scoreID int64, replayID int64, err error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Insert into used_nonces to prevent replay attacks globally
	_, err = tx.Exec(ctx, "INSERT INTO used_nonces (nonce, used_at) VALUES ($1, NOW())", rec.Nonce)
	if err != nil {
		return 0, 0, err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO scores (player_name, score, total_ticks, client_ts, ip_address, nonce, api_key_ver)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, $6, $7)
		RETURNING id
	`, rec.PlayerName, rec.Score, rec.TotalTicks, rec.ClientTS, rec.IP, rec.Nonce, rec.ReplayVersion).Scan(&scoreID)
	if err != nil {
		return 0, 0, err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO replays (score_id, replay_version, rng_seed, base_difficulty, total_ticks, data, input_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, scoreID, rec.ReplayVersion, rec.RNGSeed, rec.BaseDifficulty, rec.TotalTicks, rec.ReplayData, rec.InputCount).Scan(&replayID)
	if err != nil {
		return 0, 0, err
	}

	err = tx.Commit(ctx)
	if err != nil {
		return 0, 0, err
	}

	return scoreID, replayID, nil
}

func (db *DB) GetTopScores(ctx context.Context, limit int) ([]models.ScoreResponse, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, player_name, score, total_ticks, duration_seconds, achieved_at, COALESCE(replay_id, 0)
		FROM top_scores
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var responses []models.ScoreResponse
	for rows.Next() {
		var resp models.ScoreResponse
		var achievedAt time.Time
		var duration float64
		err := rows.Scan(&resp.ID, &resp.PlayerName, &resp.Score, &resp.TotalTicks, &duration, &achievedAt, &resp.ReplayID)
		if err != nil {
			return nil, err
		}
		resp.DurationSeconds = duration
		resp.AchievedAt = achievedAt.Format(time.RFC3339)
		responses = append(responses, resp)
	}

	return responses, nil
}

func (db *DB) IsWithinTopN(ctx context.Context, score int64, n int) (bool, error) {
	if n <= 0 {
		n = 100
	}
	var count int
	err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM scores WHERE (validated = TRUE OR validated IS NULL)").Scan(&count)
	if err != nil {
		return false, err
	}
	if count < n {
		return true, nil
	}

	var minScore int64
	err = db.Pool.QueryRow(ctx, `
		SELECT MIN(score) FROM (
			SELECT score FROM scores
			WHERE (validated = TRUE OR validated IS NULL)
			ORDER BY score DESC LIMIT $1
		) t
	`, n).Scan(&minScore)
	if err != nil {
		return false, err
	}
	return score >= minScore, nil
}

func (db *DB) IsTopTen(ctx context.Context, score int64) (bool, error) {
	return db.IsWithinTopN(ctx, score, 10)
}

func (db *DB) IsTop100(ctx context.Context, score int64) (bool, error) {
	return db.IsWithinTopN(ctx, score, 100)
}

type PendingReplayItem struct {
	ScoreID        int64
	Score          int64
	TotalTicks     int
	RNGSeed        int64
	BaseDifficulty float64
	ReplayData     []byte
}

func (db *DB) GetPendingReplaysInTopN(ctx context.Context, n int) ([]PendingReplayItem, error) {
	if n <= 0 {
		n = 100
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT s.id, s.score, r.total_ticks, r.rng_seed, r.base_difficulty, r.data
		FROM scores s
		JOIN replays r ON r.score_id = s.id
		WHERE s.validated IS NULL
		  AND (
			(SELECT COUNT(*) FROM top_scores) < $1
			OR s.score >= (SELECT COALESCE(MIN(score), 0) FROM top_scores)
		  )
		ORDER BY s.score DESC
		LIMIT $1
	`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []PendingReplayItem
	for rows.Next() {
		var item PendingReplayItem
		if err := rows.Scan(&item.ScoreID, &item.Score, &item.TotalTicks, &item.RNGSeed, &item.BaseDifficulty, &item.ReplayData); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (db *DB) MarkValidated(ctx context.Context, scoreID int64, validated bool, rejectReason string, simulatedScore int64) error {
	var reason *string
	if rejectReason != "" {
		if len(rejectReason) > 128 {
			rejectReason = rejectReason[:128]
		}
		reason = &rejectReason
	}

	_, err := db.Pool.Exec(ctx, `
		UPDATE scores
		SET validated = $1, validated_at = NOW(), reject_reason = $2, simulated_score = $3
		WHERE id = $4
	`, validated, reason, simulatedScore, scoreID)
	return err
}

// CheckNonce checks if a nonce has already been used and inserts it if not.
func (db *DB) CheckNonce(ctx context.Context, nonce string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM used_nonces WHERE nonce = $1)", nonce).Scan(&exists)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	_, err = db.Pool.Exec(ctx, "INSERT INTO used_nonces (nonce, used_at) VALUES ($1, NOW())", nonce)
	if err != nil {
		if IsNonceDuplicate(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
