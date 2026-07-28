package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"

	"flash-blip-leaderboard-api/internal/config"
	"flash-blip-leaderboard-api/internal/db"
	"flash-blip-leaderboard-api/internal/handlers"
	"flash-blip-leaderboard-api/internal/middleware"
	"flash-blip-leaderboard-api/internal/security"
	"flash-blip-leaderboard-api/internal/validator"
)

var Version = "0.2.4"

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	valCfg := validator.DefaultConfig()

	database, err := db.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatal("DB error: ", err)
	}
	defer database.Close()

	if err := database.RunMigrations(); err != nil {
		log.Fatal("Migrations error: ", err)
	}

	go database.RunNonceCleaner()

	app := fiber.New(fiber.Config{
		BodyLimit:   64 * 1024,
		AppName:     "flash-blip-leaderboard-api v" + Version,
		ProxyHeader: fiber.HeaderXForwardedFor,
	})
	app.Use(cors.New(cors.Config{
		AllowOrigins: cfg.AllowedOrigins,
		AllowMethods: "GET, POST, OPTIONS",
		AllowHeaders: "Content-Type, X-Submit-Token, X-Signature",
	}))
	app.Use(recover.New())
	app.Use(logger.New())

	tokens := security.NewTokenStore()
	sh := &handlers.ScoreHandler{DB: database, Cfg: cfg, ValCfg: valCfg, Tokens: tokens}
	go sh.RunPendingReplaySweeper(2 * time.Minute)
	rh := &handlers.ReplayHandler{DB: database}
	rph := &handlers.ReportHandler{DB: database}

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Get("/scores",
		middleware.UserAgent(cfg.AllowedUserAgents),
		sh.GetTop,
	)

	app.Get("/scores/check/:score?",
		middleware.UserAgent(cfg.AllowedUserAgents),
		sh.Check,
	)

	app.Post("/scores/check",
		middleware.UserAgent(cfg.AllowedUserAgents),
		sh.Check,
	)

	app.Post("/scores/prepare",
		middleware.RateLimit(cfg.RateLimitRPM),
		middleware.UserAgent(cfg.AllowedUserAgents),
		sh.Prepare,
	)

	app.Post("/scores/submit",
		middleware.RateLimit(cfg.RateLimitRPM),
		middleware.UserAgent(cfg.AllowedUserAgents),
		sh.Submit,
	)

	app.Get("/replays/:id",
		rh.Download,
	)

	app.Post("/scores/:id/report",
		rph.Submit,
	)

	port := cfg.Port
	if os.Getenv("PORT") != "" {
		port = os.Getenv("PORT")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("Shutting down server...")
		if err := app.Shutdown(); err != nil {
			log.Printf("Server forced shutdown: %v", err)
		}
	}()

	log.Printf("Server listening on port %s", port)
	if err := app.Listen(":" + port); err != nil {
		log.Printf("Server stopped: %v", err)
	}
}
