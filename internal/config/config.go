package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port              string
	DatabaseURL       string
	RateLimitRPM      int
	LeaderboardDepth  int
	AllowedUserAgents []string
	AllowedOrigins    string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "13337"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/flash-blip-leaderboard?sslmode=disable"
	}

	rateLimitRPM, _ := strconv.Atoi(os.Getenv("RATE_LIMIT_RPM"))
	if rateLimitRPM <= 0 {
		rateLimitRPM = 5
	}

	leaderboardDepth, _ := strconv.Atoi(os.Getenv("LEADERBOARD_DEPTH"))
	if leaderboardDepth <= 0 {
		leaderboardDepth = 100
	}

	allowedUAs := []string{"LuaSocket 3.1.0"}
	if uas := os.Getenv("ALLOWED_USER_AGENTS"); uas != "" {
		allowedUAs = strings.Split(uas, ",")
		for i := range allowedUAs {
			allowedUAs[i] = strings.TrimSpace(allowedUAs[i])
		}
	}

	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	if allowedOrigins == "" {
		allowedOrigins = "*"
	}

	return &Config{
		Port:              port,
		DatabaseURL:       dbURL,
		RateLimitRPM:      rateLimitRPM,
		LeaderboardDepth:  leaderboardDepth,
		AllowedUserAgents: allowedUAs,
		AllowedOrigins:    allowedOrigins,
	}
}
