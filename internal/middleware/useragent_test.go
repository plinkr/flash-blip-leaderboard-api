package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestUserAgentMiddleware(t *testing.T) {
	app := fiber.New()
	allowed := []string{"LuaSocket 3.1.0", "CustomUA 1.0"}

	app.Get("/test", UserAgent(allowed), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	tests := []struct {
		name           string
		userAgent      string
		expectedStatus int
	}{
		{
			name:           "Valid user agent LuaSocket",
			userAgent:      "LuaSocket 3.1.0",
			expectedStatus: 200,
		},
		{
			name:           "Valid user agent CustomUA",
			userAgent:      "CustomUA 1.0",
			expectedStatus: 200,
		},
		{
			name:           "Valid user agent Mozilla browser",
			userAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			expectedStatus: 200,
		},
		{
			name:           "Invalid user agent",
			userAgent:      "curl/7.68.0",
			expectedStatus: 403,
		},
		{
			name:           "Missing user agent",
			userAgent:      "",
			expectedStatus: 403,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tc.userAgent != "" {
				req.Header.Set("User-Agent", tc.userAgent)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("failed to test request: %v", err)
			}

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}
		})
	}
}

func TestUserAgentMiddleware_WildcardAndOptions(t *testing.T) {
	app := fiber.New()
	app.Get("/test", UserAgent([]string{"*"}), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	app.Options("/test", UserAgent([]string{"LuaSocket 3.1.0"}), func(c *fiber.Ctx) error {
		return c.SendStatus(204)
	})

	reqGet := httptest.NewRequest("GET", "/test", nil)
	reqGet.Header.Set("User-Agent", "Browser/1.0")
	respGet, err := app.Test(reqGet)
	if err != nil || respGet.StatusCode != 200 {
		t.Fatalf("expected 200 for wildcard UA, got %v, err %v", respGet.StatusCode, err)
	}

	reqOpt := httptest.NewRequest("OPTIONS", "/test", nil)
	respOpt, err := app.Test(reqOpt)
	if err != nil || respOpt.StatusCode != 204 {
		t.Fatalf("expected 204 for OPTIONS request, got %v, err %v", respOpt.StatusCode, err)
	}
}

func TestUserAgentMiddleware_EmptyAllowed(t *testing.T) {
	app := fiber.New()
	app.Get("/test", UserAgent([]string{}), func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("User-Agent", "AnyUA")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("failed to test request: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200 when allowed list is empty, got %d", resp.StatusCode)
	}
}
