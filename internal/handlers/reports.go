package handlers

import (
	"errors"
	"fmt"
	"log"
	"strconv"

	"flash-blip-leaderboard-api/internal/db"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

type ReportHandler struct {
	DB *db.DB
}

type ReportPayload struct {
	Reporter string `json:"reporter"`
	Reason   string `json:"reason"`
}

func (h *ReportHandler) Submit(c *fiber.Ctx) error {
	idStr := c.Params("id")
	scoreID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid score ID"})
	}

	var payload ReportPayload
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid payload"})
	}

	if len(payload.Reporter) > 32 {
		payload.Reporter = payload.Reporter[:32]
	}
	if len(payload.Reason) > 256 {
		payload.Reason = payload.Reason[:256]
	}

	err = h.DB.InsertReport(c.Context(), scoreID, payload.Reporter, payload.Reason)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
			return c.Status(404).JSON(fiber.Map{"error": "score not found"})
		}
		log.Printf("ERROR: InsertReport failed: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "internal server error"})
	}

	// Community report threshold check (>= 3 reports)
	count, err := h.DB.CountReports(c.Context(), scoreID)
	if err == nil && count >= 3 {
		_ = h.DB.MarkValidated(c.Context(), scoreID, false, fmt.Sprintf("community report threshold exceeded (%d reports)", count), 0)
	}

	return c.Status(201).JSON(fiber.Map{
		"ok":      true,
		"message": "report submitted successfully",
	})
}
