package handlers

import (
	"strconv"

	"flash-blip-leaderboard-api/internal/db"

	"github.com/gofiber/fiber/v2"
)

type ReplayHandler struct {
	DB *db.DB
}

func (h *ReplayHandler) Download(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid replay ID"})
	}

	replay, err := h.DB.GetReplay(c.Context(), id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "internal database error"})
	}

	if replay == nil {
		return c.Status(404).JSON(fiber.Map{"error": "replay not found"})
	}

	c.Set("Content-Type", "application/octet-stream")
	c.Set("X-Replay-Seed", strconv.FormatInt(replay.RNGSeed, 10))
	c.Set("X-Replay-Version", strconv.Itoa(int(replay.ReplayVersion)))
	c.Set("X-Replay-Ticks", strconv.Itoa(replay.TotalTicks))
	c.Set("X-Replay-Difficulty", strconv.FormatFloat(replay.BaseDifficulty, 'f', 2, 64))

	return c.Send(replay.Data)
}
