package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestGetClientIP(t *testing.T) {
	app := fiber.New()

	app.Get("/test-ip", func(c *fiber.Ctx) error {
		return c.SendString(GetClientIP(c))
	})

	tests := []struct {
		name       string
		headers    map[string]string
		expectedIP string
	}{
		{
			name: "Cloudflare Header",
			headers: map[string]string{
				"CF-Connecting-IP": "203.0.113.195",
				"X-Real-IP":        "198.51.100.1",
			},
			expectedIP: "203.0.113.195",
		},
		{
			name: "X-Real-IP Header",
			headers: map[string]string{
				"X-Real-IP": "198.51.100.1",
			},
			expectedIP: "198.51.100.1",
		},
		{
			name: "X-Forwarded-For Multiple IPs",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.50, 198.51.100.1, 10.0.0.1",
			},
			expectedIP: "203.0.113.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test-ip", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected test error: %v", err)
			}

			buf := make([]byte, 100)
			n, _ := resp.Body.Read(buf)
			ip := string(buf[:n])

			if ip != tt.expectedIP {
				t.Errorf("expected IP %q, got %q", tt.expectedIP, ip)
			}
		})
	}
}
