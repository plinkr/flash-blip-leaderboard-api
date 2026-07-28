package validator

import (
	"flash-blip-leaderboard-api/internal/models"
	"math"
)

// FLASH-BLIP constants: extracted from game/game.lua and game/main.lua
// Update if game logic changes (requires replay_version bump)
const (
	BaseScrollSpeed            = 0.08
	TicksPerUnit               = 3600.0
	DifficultyExponent         = 1.25
	DifficultyScale            = 1.5
	BaseDifficulty             = 1.0 // default initial difficulty in game.lua
	ScoreMultiplierFactor      = 4.0
	MultiplierDurationSecs     = 30.0
	ConstantDT                 = 1.0 / 60.0 // fixed dt for deterministic replay
	MultiplierSpawnProbability = 0.000344
	HeightBonusFactor          = 0.30
	BlipRecencyWindowTicks     = 120
)

// SimulateScoreResult contains the simulation result
type SimulateScoreResult struct {
	SimulatedScore int64
	ToleranceLow   int64
	ToleranceHigh  int64
	Valid          bool
}

// SimulateScore recalculates the approximate score from the input replay.
// Uses constant dt=1/60 and emulates height physics trajectory (playerY)
// to determine cumulative score with mathematical precision.
func SimulateScore(events []models.InputEvent, totalTicks int, rngSeed int64, claimedScore int64, baseDifficulty float64) SimulateScoreResult {
	if baseDifficulty <= 0 {
		baseDifficulty = BaseDifficulty
	}

	var (
		score   float64
		playerY = 80.0 // starts in middle of screen (Settings.INTERNAL_HEIGHT / 2 = 80)
	)

	inputIdx := 0

	for tick := 1; tick <= totalTicks; tick++ {
		timeUnits := float64(tick) / TicksPerUnit
		difficulty := baseDifficulty + math.Pow(timeUnits, DifficultyExponent)*DifficultyScale

		// Process inputs for this tick
		hasBlip := false
		for inputIdx < len(events) && int(events[inputIdx].Tick) == tick {
			if events[inputIdx].InputID == models.INPUT_BLIP {
				hasBlip = true
			}
			inputIdx++
		}

		// Calculate base speed with exact height bonus from game/game.lua:153
		baseSpeed := difficulty * BaseScrollSpeed
		if playerY < 80.0 {
			baseSpeed += (80.0 - playerY) * 0.02
		}

		score += baseSpeed

		// The player descends with the screen scroll speed
		playerY += baseSpeed

		// On BLIP (jump), ascends to next circle which is higher up
		if hasBlip {
			playerY -= 55.0 // vertical average between successive circles (minCircleDist = 40)
			if playerY < -20.0 {
				playerY = -20.0
			}
		}
		if playerY > 160.0 {
			playerY = 160.0
		}
	}

	simulated := int64(score)

	// Calculate absolute theoretical limits for this duration and difficulty
	var minTheoretical, maxTheoretical float64
	for tick := 1; tick <= totalTicks; tick++ {
		timeUnits := float64(tick) / TicksPerUnit
		difficulty := baseDifficulty + math.Pow(timeUnits, DifficultyExponent)*DifficultyScale

		// Absolute minimum: player does not jump (at bottom), without multipliers
		minTheoretical += difficulty * BaseScrollSpeed

		// Absolute maximum: player stays at top (y ≈ -30 -> 2.2 bonus) and has 4x multiplier active 100% of the time
		maxTheoretical += (difficulty*BaseScrollSpeed + 2.2) * ScoreMultiplierFactor
	}

	minTheoreticalVal := int64(minTheoretical)
	maxTheoreticalVal := int64(maxTheoretical)

	// Dynamic and generous bounding:
	// The simulated score realistically estimates base score without multipliers (ratio ≈ 1.08).
	// The claimed score may be higher due to multipliers collected via physical collision.
	low := max(int64(float64(simulated)*0.7)-200, 0)
	high := int64(float64(simulated)*4.5) + 1000 // Supports up to 4x multiplier with extra 12% margin

	// Ensure it never rejects within real physical limits
	if low > minTheoreticalVal {
		low = max(minTheoreticalVal-500, 0)
	}
	if high < maxTheoreticalVal {
		high = int64(float64(maxTheoreticalVal) * 1.5)
	}

	return SimulateScoreResult{
		SimulatedScore: simulated,
		ToleranceLow:   low,
		ToleranceHigh:  high,
		Valid:          claimedScore >= low && claimedScore <= high,
	}
}
