package validator

import (
	"flash-blip-leaderboard-api/internal/models"
	"fmt"
	"math"
)

// FLASH-BLIP constants: extracted from game/game.lua and game/main.lua
// Update if game logic changes (requires replay_version bump)
const (
	BaseScrollSpeed        = 0.08
	TicksPerUnit           = 3600.0
	DifficultyScale        = 1.5
	BaseDifficulty         = 1.0 // default initial difficulty in game.lua
	ScoreMultiplierFactor  = 4.0
	InternalHeight         = 160.0
	MinCircleDist          = InternalHeight / 4.0 // 40.0
	AverageCircleDist      = InternalHeight * 0.35 // 56.0 (average spawn dist [0.25..0.45] * InternalHeight)
	InitialPlayerY         = -25.0                // average of -radius where radius ~20..30
	HeightBonusThreshold   = InternalHeight / 2.0 // 80.0
	HeightBonusCoefficient = 0.02
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
		score          float64
		playerY        = InitialPlayerY // starts at first circle position (y ≈ -radius)
		minTheoretical float64
		maxTheoretical float64
	)

	inputIdx := 0

	for tick := 1; tick <= totalTicks; tick++ {
		difficulty := difficultyAtTick(tick, baseDifficulty)

		hasBlip := false
		for inputIdx < len(events) && int(events[inputIdx].Tick) == tick {
			if events[inputIdx].InputID == models.INPUT_BLIP {
				hasBlip = true
			}
			inputIdx++
		}

		baseSpeedForScore := calculateBaseSpeed(difficulty, playerY)

		score += baseSpeedForScore

		// Absolute minimum: player does not jump (at bottom), without multipliers
		minTheoretical += difficulty * BaseScrollSpeed

		// Absolute maximum: player stays at top (y ≈ -25 -> bonus ≈ 2.1) and has 4x multiplier active 100% of the time
		maxTheoretical += (difficulty*BaseScrollSpeed + 2.1) * ScoreMultiplierFactor

		playerY += baseSpeedForScore

		if hasBlip {
			playerY = calculateNextCircleY(playerY)
		}

		if playerY > InternalHeight {
			playerY = InternalHeight
		}
	}

	simulated := int64(score)
	minTheoreticalVal := int64(minTheoretical)

	// Dynamic tolerance based on simulated score.
	// V1 replays lack multiplier events, so we estimate with heuristics.
	// Use a tighter tolerance since the simulation tracks playerY trajectory.
	low := max(int64(float64(simulated)*0.85)-100, 0)
	high := int64(float64(simulated)*1.25) + 300 // Allow variance for missing multiplier data

	// For V1, cap the maximum at a realistic multiplier assumption rather than
	// the theoretical 100% multiplier uptime (which V1 cannot verify).
	// Assume at most 60% multiplier uptime as a generous but realistic cap.
	realisticMaxTheoretical := int64(minTheoretical + (maxTheoretical-minTheoretical)*0.3)

	// Ensure it never rejects within realistic physical limits
	if low > minTheoreticalVal {
		low = max(minTheoreticalVal-100, 0)
	}
	if high < realisticMaxTheoretical {
		high = realisticMaxTheoretical
	} else {
		high = min(high, realisticMaxTheoretical)
	}

	return SimulateScoreResult{
		SimulatedScore: simulated,
		ToleranceLow:   low,
		ToleranceHigh:  high,
		Valid:          claimedScore >= low && claimedScore <= high,
	}
}

// SimulateReplay dispatches simulation by replay version. Version 1 remains
// heuristic for compatibility; version 2 has an exact multiplier timeline.
func SimulateReplay(version int, events []models.InputEvent, totalTicks int, rngSeed int64, claimedScore int64, baseDifficulty float64) (SimulateScoreResult, error) {
	switch version {
	case ReplayVersionV1:
		return SimulateScore(events, totalTicks, rngSeed, claimedScore, baseDifficulty), nil
	case ReplayVersionV2:
		if err := ValidateV2(events, totalTicks); err != nil {
			return SimulateScoreResult{}, err
		}
		if err := ValidateBaseDifficulty(baseDifficulty); err != nil {
			return SimulateScoreResult{}, err
		}
		return simulateScoreV2(events, totalTicks, claimedScore, baseDifficulty), nil
	default:
		return SimulateScoreResult{}, fmt.Errorf("unsupported replay version %d", version)
	}
}

func simulateScoreV2(events []models.InputEvent, totalTicks int, claimedScore int64, baseDifficulty float64) SimulateScoreResult {
	var (
		minimumScore        float64
		estimatedScore      float64
		maximumScore        float64
		inputIdx            int
		playerY             = InitialPlayerY
		multiplierActive    bool
		multiplierStartTick uint32
	)

	for tick := 1; tick <= totalTicks; tick++ {
		for inputIdx < len(events) && int(events[inputIdx].Tick) == tick {
			switch events[inputIdx].InputID {
			case models.INPUT_MULTIPLIER_STARTED:
				multiplierActive = true
				multiplierStartTick = events[inputIdx].Tick
			case models.INPUT_MULTIPLIER_ENDED:
				multiplierActive = false
		case models.INPUT_BLIP:
			playerY = calculateNextCircleY(playerY)
		}
			inputIdx++
		}
		// A missing end event must not extend a client-declared multiplier
		// beyond the protocol's maximum duration.
		if multiplierActive && uint64(tick)-uint64(multiplierStartTick) >= uint64(MaxMultiplierTicks) {
			multiplierActive = false
		}

		difficulty := difficultyAtTick(tick, baseDifficulty)

		multiplier := 1.0
		if multiplierActive {
			multiplier = ScoreMultiplierFactor
		}

		baseSpeedMin := difficulty * BaseScrollSpeed
		baseSpeedEst := calculateBaseSpeed(difficulty, playerY)
		baseSpeedMax := difficulty*BaseScrollSpeed + 2.10

		minimumScore += baseSpeedMin * multiplier
		estimatedScore += baseSpeedEst * multiplier
		maximumScore += baseSpeedMax * multiplier

		playerY += baseSpeedEst
		if playerY > InternalHeight {
			playerY = InternalHeight
		}
	}

	simulated := int64(estimatedScore)
	// V2 has an exact multiplier timeline from events, and accurate circle spacing physics (AverageCircleDist = 56.0).
	// Low bound allows for low-altitude play without height bonus (relative to physical minimum).
	// High bound accommodates legitimate high-altitude play up to 1.40x simulated score,
	// strictly bounded by the maximum theoretical 100% top-screen limit.
	low := max(int64(float64(simulated)*0.70)-100, int64(minimumScore*0.85))
	high := min(int64(maximumScore), int64(float64(simulated)*1.40)+300)

	return SimulateScoreResult{
		SimulatedScore: simulated,
		ToleranceLow:   low,
		ToleranceHigh:  high,
		Valid:          claimedScore >= low && claimedScore <= high,
	}
}

func ValidateBaseDifficulty(baseDifficulty float64) error {
	if math.IsNaN(baseDifficulty) || math.IsInf(baseDifficulty, 0) || baseDifficulty < 1 || baseDifficulty > 3.5 {
		return fmt.Errorf("invalid base difficulty %v", baseDifficulty)
	}
	return nil
}

func difficultyAtTick(tick int, baseDifficulty float64) float64 {
	timeUnits := float64(tick) / TicksPerUnit
	return baseDifficulty + (timeUnits*math.Sqrt(math.Sqrt(timeUnits)))*DifficultyScale
}

func calculateBaseSpeed(difficulty, playerY float64) float64 {
	baseSpeed := difficulty * BaseScrollSpeed
	if playerY < HeightBonusThreshold {
		baseSpeed += (HeightBonusThreshold - playerY) * HeightBonusCoefficient
	}
	return baseSpeed
}

func calculateNextCircleY(currentPlayerY float64) float64 {
	return math.Max(-50.0, currentPlayerY-AverageCircleDist)
}
