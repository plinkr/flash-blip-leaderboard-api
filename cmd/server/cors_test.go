package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func TestCORS(t *testing.T) {
	app := fiber.New()
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET, POST, OPTIONS",
		AllowHeaders: "Content-Type, X-Submit-Token, X-Signature",
	}))

	app.Get("/scores", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Test OPTIONS preflight
	reqOpt := httptest.NewRequest("OPTIONS", "/scores", nil)
	reqOpt.Header.Set("Origin", "http://localhost:1337")
	reqOpt.Header.Set("Access-Control-Request-Method", "POST")
	reqOpt.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Submit-Token, X-Signature")

	respOpt, err := app.Test(reqOpt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if respOpt.StatusCode != 204 && respOpt.StatusCode != 200 {
		t.Errorf("expected 204 or 200 on OPTIONS preflight, got %d", respOpt.StatusCode)
	}

	if h := respOpt.Header.Get("Access-Control-Allow-Origin"); h != "*" {
		t.Errorf("expected Access-Control-Allow-Origin '*', got '%s'", h)
	}

	if h := respOpt.Header.Get("Access-Control-Allow-Methods"); h == "" {
		t.Errorf("expected Access-Control-Allow-Methods to be set")
	}

	if h := respOpt.Header.Get("Access-Control-Allow-Headers"); h == "" {
		t.Errorf("expected Access-Control-Allow-Headers to be set")
	}

	// Test GET with Origin
	reqGet := httptest.NewRequest("GET", "/scores", nil)
	reqGet.Header.Set("Origin", "http://localhost:1337")
	respGet, err := app.Test(reqGet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h := respGet.Header.Get("Access-Control-Allow-Origin"); h != "*" {
		t.Errorf("expected Access-Control-Allow-Origin '*', got '%s'", h)
	}
}
