package handlers

import (
	"bytes"
	"encoding/binary"
	"net/http/httptest"
	"strings"
	"testing"

	"flash-blip-leaderboard-api/internal/security"

	"github.com/gofiber/fiber/v2"
	"github.com/pierrec/lz4/v4"
)

func TestLZ4Decompression(t *testing.T) {
	originalData := []byte("test binary replay stream data")

	buf := make([]byte, 4+lz4.CompressBlockBound(len(originalData)))
	binary.LittleEndian.PutUint32(buf[:4], uint32(len(originalData)))

	var ht [65536]int
	n, err := lz4.CompressBlock(originalData, buf[4:], ht[:])
	if err != nil {
		t.Fatalf("failed to compress lz4 block: %v", err)
	}
	compressed := buf[:4+n]

	decompressed, err := decompressLove2DLZ4(compressed)
	if err != nil {
		t.Fatalf("failed to decompress lz4 data: %v", err)
	}

	if !bytes.Equal(decompressed, originalData) {
		t.Errorf("expected %q, got %q", originalData, decompressed)
	}
}

func TestCheckScoreValidation(t *testing.T) {
	app := fiber.New()
	sh := &ScoreHandler{}

	app.Get("/scores/check", sh.Check)
	app.Post("/scores/check", sh.Check)

	req := httptest.NewRequest("GET", "/scores/check?score=invalid", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error testing endpoint: %v", err)
	}

	if resp.StatusCode != 400 {
		t.Errorf("expected status 400 for invalid score parameter, got %d", resp.StatusCode)
	}

	reqPost := httptest.NewRequest("POST", "/scores/check", strings.NewReader(`{"score": 0}`))
	reqPost.Header.Set("Content-Type", "application/json")
	respPost, err := app.Test(reqPost)
	if err != nil {
		t.Fatalf("unexpected error testing POST endpoint: %v", err)
	}

	if respPost.StatusCode != 400 {
		t.Errorf("expected status 400 for score=0 payload, got %d", respPost.StatusCode)
	}
}

func TestPrepareHandlerValidation(t *testing.T) {
	app := fiber.New()
	tokens := security.NewTokenStore()
	sh := &ScoreHandler{Tokens: tokens}

	app.Post("/scores/prepare", sh.Prepare)

	req := httptest.NewRequest("POST", "/scores/prepare", strings.NewReader(`{"s": 50, "p": "Player1", "total_ticks": 300}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error testing prepare endpoint: %v", err)
	}

	if resp.StatusCode != 400 {
		t.Errorf("expected status 400 for score out of range, got %d", resp.StatusCode)
	}
}
