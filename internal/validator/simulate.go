package validator

import (
	"flash-blip-leaderboard-api/internal/models"
	"fmt"
	"math"
)

// FLASH-BLIP constants
const (
	BaseScrollSpeed        = 0.08
	TicksPerUnit           = 3600.0
	DifficultyScale        = 1.5
	BaseDifficulty         = 1.0 // default initial difficulty
	ScoreMultiplierFactor  = 4.0
	InternalHeight         = 160.0
	MinCircleDist          = InternalHeight / 4.0 // 40.0
	InitialPlayerY         = -25.0                // average of -radius where radius ~20..30
	HeightBonusThreshold   = InternalHeight / 2.0 // 80.0
	HeightBonusCoefficient = 0.02
)

// LoveRNG reproduces Love2D's love.math.RandomGenerator (PCG32)
type LoveRNG struct {
	state uint64
	inc   uint64
}

func NewLoveRNG(seed uint64) *LoveRNG {
	r := &LoveRNG{}
	r.SetSeed(seed)
	return r
}

func (r *LoveRNG) SetSeed(seed uint64) {
	r.state = 0
	r.inc = ((seed >> 32) << 1) | 1
	r.nextUint32()
	r.state += seed & 0xFFFFFFFF
	r.nextUint32()
}

func (r *LoveRNG) nextUint32() uint32 {
	oldstate := r.state
	r.state = oldstate*6364136223846793005 + r.inc
	xorshifted := uint32(((oldstate >> 18) ^ oldstate) >> 27)
	rot := uint32(oldstate >> 59)
	return (xorshifted >> rot) | (xorshifted << ((-rot) & 31))
}

func (r *LoveRNG) RandomFloat() float64 {
	return float64(r.nextUint32()) / 4294967296.0
}

func (r *LoveRNG) CircleDistance() float64 {
	minDist := MinCircleDist
	maxDist := InternalHeight * 0.45
	return minDist + r.RandomFloat()*(maxDist-minDist)
}

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
		playerY        = InitialPlayerY // starts at first circle position (-radius)
		minTheoretical float64
		maxTheoretical float64
		inputIdx       int
		lastActionTick = -1000
	)

	rng := NewLoveRNG(uint64(rngSeed))

	for tick := 1; tick <= totalTicks; tick++ {
		for inputIdx < len(events) && int(events[inputIdx].Tick) == tick {
			switch events[inputIdx].InputID {
			case models.InputBlip:
				lastActionTick = tick
				dist := rng.CircleDistance()
				playerY = math.Max(-50.0, playerY-dist)
			case models.InputPing:
				lastActionTick = tick
				playerY = math.Max(-50.0, playerY-18.0)
			}
			inputIdx++
		}

		effectiveY := playerY
		if tick-lastActionTick < 90 {
			effectiveY = math.Max(-30.0, playerY*0.5)
		}

		difficulty := difficultyAtTick(tick, baseDifficulty)

		baseSpeedForScore := calculateBaseSpeed(difficulty, effectiveY)
		score += baseSpeedForScore

		// Absolute minimum: player does not jump (at bottom), without multipliers
		minTheoretical += difficulty * BaseScrollSpeed

		// Absolute maximum: player stays at top (y is approx -25 -> bonus = 2.1) and has 4x multiplier active 100% of the time
		maxTheoretical += (difficulty*BaseScrollSpeed + 2.1) * ScoreMultiplierFactor

		playerY += baseSpeedForScore
		if playerY > InternalHeight {
			playerY = InternalHeight
		}
	}

	simulated := int64(score)
	minTheoreticalVal := int64(minTheoretical)
	maxTheoreticalVal := int64(maxTheoretical)

	// V1 replays do not log explicit multiplier events, so simulated score estimates base score (1x).
	// Claimed score can be higher due to 4x multiplier powerups.
	low := max(int64(float64(simulated)*0.50)-100, int64(float64(minTheoreticalVal)*0.85))
	high := min(maxTheoreticalVal, int64(float64(simulated)*3.50)+500)

	if low > minTheoreticalVal {
		low = max(minTheoreticalVal-100, 0)
	}

	return SimulateScoreResult{
		SimulatedScore: simulated,
		ToleranceLow:   low,
		ToleranceHigh:  high,
		Valid:          claimedScore >= low && claimedScore <= high,
	}
}

// SimulateReplay dispatches simulation by replay version. Version 1 remains
// heuristic for compatibility, version 2 has an exact multiplier timeline.
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
		return simulateScoreV2(events, totalTicks, rngSeed, claimedScore, baseDifficulty), nil
	default:
		return SimulateScoreResult{}, fmt.Errorf("unsupported replay version %d", version)
	}
}

func simulateScoreV2(events []models.InputEvent, totalTicks int, rngSeed int64, claimedScore int64, baseDifficulty float64) SimulateScoreResult {
	var (
		minimumScore        float64
		estimatedScore      float64
		maximumScore        float64
		inputIdx            int
		playerY             = InitialPlayerY
		multiplierActive    bool
		multiplierStartTick uint32
		lastActionTick      = -1000
	)

	rng := NewLoveRNG(uint64(rngSeed))

	for tick := 1; tick <= totalTicks; tick++ {
		for inputIdx < len(events) && int(events[inputIdx].Tick) == tick {
			switch events[inputIdx].InputID {
			case models.InputMultiplierStarted:
				multiplierActive = true
				multiplierStartTick = events[inputIdx].Tick
			case models.InputMultiplierEnded:
				multiplierActive = false
			case models.InputBlip:
				lastActionTick = tick
				dist := rng.CircleDistance()
				playerY = math.Max(-50.0, playerY-dist)
			case models.InputPing:
				lastActionTick = tick
				playerY = math.Max(-50.0, playerY-18.0)
			}
			inputIdx++
		}
		// A missing end event must not extend a client-declared multiplier
		// beyond the protocol's maximum duration.
		if multiplierActive && uint64(tick)-uint64(multiplierStartTick) >= uint64(MaxMultiplierTicks) {
			multiplierActive = false
		}

		// When player is actively inputting BLIPs/PINGs,
		// player stays in upper region of screen.
		effectiveY := playerY
		if tick-lastActionTick < 90 {
			effectiveY = math.Max(-30.0, playerY*0.5)
		}

		difficulty := difficultyAtTick(tick, baseDifficulty)

		multiplier := 1.0
		if multiplierActive {
			multiplier = ScoreMultiplierFactor
		}

		baseSpeedMin := difficulty * BaseScrollSpeed
		baseSpeedEst := calculateBaseSpeed(difficulty, effectiveY)
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
	// V2 has an exact multiplier timeline from events, and accurate circle spacing physics.
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
