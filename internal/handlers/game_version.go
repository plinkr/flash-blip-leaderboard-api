package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// GitHub rejects API requests without a User-Agent
const githubAPIUserAgent = "flash-blip-leaderboard-api"

const (
	defaultGameVersionTTL = 10 * time.Minute
	githubRequestTimeout  = 5 * time.Second
	maxGitHubResponseSize = 64 * 1024
)

type GameVersionHandler struct {
	URL    string
	TTL    time.Duration
	Client *http.Client

	mu          sync.Mutex
	tagName     string
	cachedAt    time.Time
	lastAttempt time.Time
}

func (h *GameVersionHandler) Get(c *fiber.Ctx) error {
	ttl := h.TTL
	if ttl <= 0 {
		ttl = defaultGameVersionTTL
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: githubRequestTimeout}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	hasValue := h.tagName != ""
	fresh := hasValue && now.Sub(h.cachedAt) < ttl

	// At most one upstream attempt per TTL window
	recentAttempt := !h.lastAttempt.IsZero() && now.Sub(h.lastAttempt) < ttl

	if fresh || recentAttempt {
		if hasValue {
			return c.JSON(fiber.Map{"tag_name": h.tagName})
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
	}

	tag, err := h.fetch(client)
	h.lastAttempt = now
	if err != nil {
		log.Printf("WARN: game version refresh failed: %v", err)
		if hasValue {
			return c.JSON(fiber.Map{"tag_name": h.tagName})
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
	}

	h.tagName = tag
	h.cachedAt = now
	return c.JSON(fiber.Map{"tag_name": h.tagName})
}

func (h *GameVersionHandler) fetch(client *http.Client) (string, error) {
	req, err := http.NewRequest(http.MethodGet, h.URL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", githubAPIUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call github: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubResponseSize)).Decode(&release); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name in response")
	}
	return release.TagName, nil
}
