package handlers

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"strconv"
	"time"

	"flash-blip-leaderboard-api/internal/config"
	"flash-blip-leaderboard-api/internal/db"
	"flash-blip-leaderboard-api/internal/middleware"
	"flash-blip-leaderboard-api/internal/models"
	"flash-blip-leaderboard-api/internal/security"
	"flash-blip-leaderboard-api/internal/validator"

	"github.com/gofiber/fiber/v2"
	"github.com/pierrec/lz4/v4"
)

type ScoreHandler struct {
	DB     *db.DB
	Cfg    *config.Config
	ValCfg *validator.Config
	Tokens *security.TokenStore
}

func (h *ScoreHandler) Prepare(c *fiber.Ctx) error {
	var meta security.SessionMetadata
	if err := c.BodyParser(&meta); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
	}

	if meta.Score <= 100 || meta.Score > 1_000_000 {
		return c.Status(400).JSON(fiber.Map{"error": "score out of range"})
	}
	if len(meta.PlayerName) == 0 || len(meta.PlayerName) > 15 {
		return c.Status(400).JSON(fiber.Map{"error": "invalid player name"})
	}
	if meta.TotalTicks < 60 || meta.TotalTicks > 60*60*60 { // max 1 hour
		return c.Status(400).JSON(fiber.Map{"error": "invalid total_ticks"})
	}

	token, nonce, err := h.Tokens.Create(meta)
	if err != nil {
		log.Printf("ERROR: failed to generate security token: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to generate security token"})
	}

	return c.JSON(fiber.Map{
		"token": token,
		"nonce": nonce,
	})
}

func (h *ScoreHandler) Submit(c *fiber.Ctx) error {
	tokenStr := c.Get("X-Submit-Token")
	if tokenStr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing X-Submit-Token header"})
	}

	sigHeader := c.Get("X-Signature")
	if sigHeader == "" {
		return c.Status(400).JSON(fiber.Map{"error": "missing X-Signature header"})
	}

	bodyBytes := c.Body()
	if len(bodyBytes) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "empty body"})
	}

	session, ok := h.Tokens.Get(tokenStr)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"error": "invalid, expired, or already used token"})
	}

	// Verify HMAC signature using the session nonce as key
	if !security.VerifyHMAC(bodyBytes, session.Nonce, sigHeader) {
		return c.Status(401).JSON(fiber.Map{"error": "invalid signature"})
	}

	// Signature verified successfully; consume token to prevent re-use
	if !h.Tokens.Consume(tokenStr) {
		return c.Status(401).JSON(fiber.Map{"error": "invalid, expired, or already used token"})
	}

	compressedInputs, err := base64.StdEncoding.DecodeString(string(bodyBytes))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid replay encoding"})
	}

	// Decompress LZ4 (LOVE2D block format)
	inputsRaw, err := decompressLove2DLZ4(compressedInputs)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "replay decompression failed"})
	}

	events, err := validator.ParseInputs(inputsRaw)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid replay format: " + err.Error()})
	}

	stats := validator.Analyze(events, session.Metadata.TotalTicks)

	if err := validator.ValidateLight(h.ValCfg, events, stats, session.Metadata.Score, session.Metadata.TotalTicks); err != nil {
		log.Printf("REPLAY_REJECT_LIGHT player=%s score=%d reason=%s ip=%s",
			session.Metadata.PlayerName, session.Metadata.Score, err.Error(), middleware.GetClientIP(c))
		return c.Status(400).JSON(fiber.Map{"error": "replay validation failed: " + err.Error()})
	}

	scoreID, replayID, err := h.DB.InsertScoreWithReplay(c.Context(), db.ScoreRecord{
		PlayerName:     session.Metadata.PlayerName,
		Score:          session.Metadata.Score,
		TotalTicks:     session.Metadata.TotalTicks,
		ClientTS:       time.Now().Unix(),
		IP:             middleware.GetClientIP(c),
		Nonce:          session.Nonce,
		ReplayVersion:  1,
		RNGSeed:        session.Metadata.RNGSeed,
		BaseDifficulty: session.Metadata.Difficulty,
		ReplayData:     compressedInputs,
		InputCount:     len(events),
	})
	if err != nil {
		if db.IsNonceDuplicate(err) {
			return c.Status(409).JSON(fiber.Map{"error": "duplicate request"})
		}
		log.Printf("ERROR: failed to insert score: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "internal server error"})
	}

	// Full asynchronous validation if candidate for leaderboard
	go h.asyncValidateCandidate(scoreID, session.Metadata.Score, events, session.Metadata.TotalTicks, session.Metadata.RNGSeed, session.Metadata.Difficulty)

	return c.Status(201).JSON(fiber.Map{
		"ok":        true,
		"score_id":  scoreID,
		"replay_id": replayID,
	})
}

func (h *ScoreHandler) asyncValidateCandidate(scoreID int64, claimedScore int64, events []models.InputEvent, totalTicks int, rngSeed int64, baseDifficulty float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	depth := 100
	if h.Cfg != nil && h.Cfg.LeaderboardDepth > 0 {
		depth = h.Cfg.LeaderboardDepth
	}

	inTop, err := h.DB.IsWithinTopN(ctx, claimedScore, depth)
	if err != nil || !inTop {
		return // not in top, remains NULL (pending)
	}

	result := validator.SimulateScore(events, totalTicks, rngSeed, claimedScore, baseDifficulty)

	if result.Valid {
		if err := h.DB.MarkValidated(ctx, scoreID, true, "", result.SimulatedScore); err != nil {
			log.Printf("ERROR: failed to mark score %d as validated: %v", scoreID, err)
			return
		}
		log.Printf("REPLAY_VALID scoreID=%d claimed=%d simulated=%d", scoreID, claimedScore, result.SimulatedScore)
	} else {
		reason := fmt.Sprintf("simulated=%d range=[%d,%d] claimed=%d",
			result.SimulatedScore, result.ToleranceLow, result.ToleranceHigh, claimedScore)

		if err := h.DB.MarkValidated(ctx, scoreID, false, reason, result.SimulatedScore); err != nil {
			log.Printf("ERROR: failed to mark score %d as invalid (reason: %s): %v", scoreID, reason, err)
			return
		}
		log.Printf("REPLAY_REJECT_SIM scoreID=%d %s", scoreID, reason)
	}

}

func (h *ScoreHandler) GetTop(c *fiber.Ctx) error {
	depth := 100
	if h.Cfg != nil && h.Cfg.LeaderboardDepth > 0 {
		depth = h.Cfg.LeaderboardDepth
	}
	scores, err := h.DB.GetTopScores(c.Context(), depth)
	if err != nil {
		log.Printf("ERROR: GetTop scores failed: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "internal server error"})
	}
	return c.JSON(fiber.Map{"scores": scores})
}

func (h *ScoreHandler) Check(c *fiber.Ctx) error {
	scoreStr := c.Query("score")
	if scoreStr == "" {
		scoreStr = c.Params("score")
	}

	var score int64
	var err error

	if scoreStr != "" {
		score, err = strconv.ParseInt(scoreStr, 10, 64)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid score"})
		}
	} else {
		var req struct {
			Score int64 `json:"score"`
		}
		if err := c.BodyParser(&req); err != nil || req.Score == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "invalid score payload"})
		}
		score = req.Score
	}

	depth := 100
	if h.Cfg != nil && h.Cfg.LeaderboardDepth > 0 {
		depth = h.Cfg.LeaderboardDepth
	}

	relevant, err := h.DB.IsWithinTopN(c.Context(), score, depth)
	if err != nil {
		log.Printf("ERROR: Check score failed: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "internal server error"})
	}

	return c.JSON(fiber.Map{"relevant": relevant})
}

// RunPendingReplaySweeper periodically checks and validates unverified replays that qualify for top N.
func (h *ScoreHandler) RunPendingReplaySweeper(interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		h.sweepPendingReplays()
	}
}

func (h *ScoreHandler) sweepPendingReplays() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	depth := 100
	if h.Cfg != nil && h.Cfg.LeaderboardDepth > 0 {
		depth = h.Cfg.LeaderboardDepth
	}

	items, err := h.DB.GetPendingReplaysInTopN(ctx, depth)
	if err != nil {
		log.Printf("SWEEPER_ERROR: failed to get pending replays: %v", err)
		return
	}

	for _, item := range items {
		inputsRaw, err := decompressLove2DLZ4(item.ReplayData)
		if err != nil {
			log.Printf("SWEEPER_ERROR: failed lz4 decompression for scoreID %d: %v", item.ScoreID, err)
			continue
		}

		events, err := validator.ParseInputs(inputsRaw)
		if err != nil {
			log.Printf("SWEEPER_ERROR: invalid replay format for scoreID %d: %v", item.ScoreID, err)
			continue
		}

		result := validator.SimulateScore(events, item.TotalTicks, item.RNGSeed, item.Score, item.BaseDifficulty)
		if result.Valid {
			if err := h.DB.MarkValidated(ctx, item.ScoreID, true, "", result.SimulatedScore); err != nil {
				log.Printf("SWEEPER_ERROR: failed to mark score %d validated: %v", item.ScoreID, err)
			} else {
				log.Printf("SWEEPER_VALID scoreID=%d claimed=%d simulated=%d", item.ScoreID, item.Score, result.SimulatedScore)
			}
		} else {
			reason := fmt.Sprintf("simulated=%d range=[%d,%d] claimed=%d",
				result.SimulatedScore, result.ToleranceLow, result.ToleranceHigh, item.Score)
			if err := h.DB.MarkValidated(ctx, item.ScoreID, false, reason, result.SimulatedScore); err != nil {
				log.Printf("SWEEPER_ERROR: failed to mark score %d invalid: %v", item.ScoreID, err)
			} else {
				log.Printf("SWEEPER_REJECT_SIM scoreID=%d %s", item.ScoreID, reason)
			}
		}
	}
}

func decompressLove2DLZ4(compressedData []byte) ([]byte, error) {
	if len(compressedData) < 4 {
		return nil, fmt.Errorf("compressed data too short")
	}

	uncompressedSize := binary.LittleEndian.Uint32(compressedData[:4])
	// 256KB max replay size ceiling to prevent memory exhaustion
	const maxDecompressedSize = 256 * 1024
	if uncompressedSize > maxDecompressedSize {
		return nil, fmt.Errorf("uncompressed size %d exceeds limit of %d", uncompressedSize, maxDecompressedSize)
	}

	dst := make([]byte, uncompressedSize)
	n, err := lz4.UncompressBlock(compressedData[4:], dst)
	if err != nil {
		return nil, fmt.Errorf("lz4 block decompression failed: %w", err)
	}

	return dst[:n], nil
}
