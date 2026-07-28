package validator

import (
	"encoding/binary"
	"flash-blip-leaderboard-api/internal/models"
	"fmt"
	"math"
)

// Config adjust according to FLASH-BLIP real mechanics
type Config struct {
	MaxReplayVersion     int
	MinTicksPerScore     float64 // minimum ticks for score to be possible
	MaxBlipsPerMinute    float64 // maximum credible human BLIPs/min (~3/sec = 180/min)
	MaxPingsPerMinute    float64 // maximum credible human PINGs/min (~5/sec = 300/min)
	MinTicksBetweenBlips int     // minimum ticks between BLIPs (~6 ticks = 100ms at 60fps)
	MaxPerfectRatio      float64 // ratio of identical BLIP intervals (indicates TAS)
	MaxPerfectPingRatio  float64 // ratio of identical PING intervals (indicates TAS)
}

func DefaultConfig() *Config {
	return &Config{
		MaxReplayVersion:     1,
		MinTicksPerScore:     0.5,  // score cannot be > ticks * this_factor on average
		MaxBlipsPerMinute:    180,  // 3 BLIPs/sec
		MaxPingsPerMinute:    300,  // 5 PINGs/sec
		MinTicksBetweenBlips: 0,    // disabled (was 6) to allow instant jumps with Phase Shift/invulnerability
		MaxPerfectRatio:      0.80, // >80% identical intervals = suspicious
		MaxPerfectPingRatio:  0.95, // >95% identical intervals = suspicious
	}
}

// ParseInputs parses binary stream (5 bytes per event: 4 tick + 1 input_id)
func ParseInputs(rawBytes []byte) ([]models.InputEvent, error) {
	if len(rawBytes)%5 != 0 {
		return nil, fmt.Errorf("invalid input stream length %d (must be multiple of 5)", len(rawBytes))
	}

	events := make([]models.InputEvent, len(rawBytes)/5)
	for i := range events {
		offset := i * 5
		events[i] = models.InputEvent{
			Tick:    binary.LittleEndian.Uint32(rawBytes[offset : offset+4]),
			InputID: rawBytes[offset+4],
		}
	}
	return events, nil
}

// Analyze calculates statistics over input stream
func Analyze(events []models.InputEvent, totalTicks int) models.ReplayStats {
	stats := models.ReplayStats{
		TotalInputs:     len(events),
		MinTickInterval: math.MaxInt32,
	}

	var lastBlipTick = -1
	var lastPingTick = -1
	blipIntervalCounts := make(map[int]int) // to detect repetitive patterns in BLIPs
	pingIntervalCounts := make(map[int]int) // to detect repetitive patterns in PINGs

	for _, e := range events {
		if e.InputID == models.INPUT_BLIP {
			stats.BlipCount++
			if lastBlipTick != -1 {
				interval := int(e.Tick) - lastBlipTick
				if interval < stats.MinTickInterval {
					stats.MinTickInterval = interval
				}
				if interval > stats.MaxTickInterval {
					stats.MaxTickInterval = interval
				}
				stats.AvgTickInterval = (stats.AvgTickInterval*float64(stats.BlipCount-2) + float64(interval)) / float64(stats.BlipCount-1)
				blipIntervalCounts[interval]++
			}
			lastBlipTick = int(e.Tick)
		} else if e.InputID == models.INPUT_PING {
			stats.PingCount++
			if lastPingTick != -1 {
				interval := int(e.Tick) - lastPingTick
				pingIntervalCounts[interval]++
			}
			lastPingTick = int(e.Tick)
		}
	}

	if stats.MinTickInterval == math.MaxInt32 {
		stats.MinTickInterval = 0
	}

	// Count "perfectly repeated" BLIP intervals
	for _, count := range blipIntervalCounts {
		if count > 1 {
			stats.PerfectIntervals += count
		}
	}

	// Count "perfectly repeated" PING intervals
	for _, count := range pingIntervalCounts {
		if count > 1 {
			stats.PerfectPingIntervals += count
		}
	}

	return stats
}

// ValidateLight fast O(n) validation, runs synchronously in the request
func ValidateLight(cfg *Config, events []models.InputEvent, stats models.ReplayStats, claimedScore int64, totalTicks int) error {
	if totalTicks <= 0 {
		return fmt.Errorf("invalid total_ticks: %d", totalTicks)
	}

	// At least some inputs
	if stats.TotalInputs == 0 && totalTicks > 60 {
		return fmt.Errorf("no inputs recorded for a run of %d ticks", totalTicks)
	}

	// Ticks in increasing order (may coincide on same tick/frame if multiple actions recorded)
	for i := 1; i < len(events); i++ {
		if events[i].Tick < events[i-1].Tick {
			return fmt.Errorf("input ticks decreasing at index %d", i)
		}
	}

	// No tick exceeds total_ticks
	for _, e := range events {
		if int(e.Tick) > totalTicks {
			return fmt.Errorf("input tick %d exceeds total_ticks %d", e.Tick, totalTicks)
		}
	}

	// BLIPs too frequent for a human
	if stats.BlipCount > 0 {
		blipsPerMinute := float64(stats.BlipCount) / (float64(totalTicks) / 60.0 / 60.0)
		if blipsPerMinute > cfg.MaxBlipsPerMinute {
			return fmt.Errorf("inhuman blip rate: %.1f blips/min (max %g)", blipsPerMinute, cfg.MaxBlipsPerMinute)
		}
	}

	// PINGs too frequent for a human
	if stats.PingCount > 0 {
		pingsPerMinute := float64(stats.PingCount) / (float64(totalTicks) / 60.0 / 60.0)
		if pingsPerMinute > cfg.MaxPingsPerMinute {
			return fmt.Errorf("inhuman ping rate: %.1f pings/min (max %g)", pingsPerMinute, cfg.MaxPingsPerMinute)
		}
	}

	// Mechanical pattern (TAS): too many identical BLIP intervals
	if stats.BlipCount > 20 {
		perfectRatio := float64(stats.PerfectIntervals) / float64(stats.BlipCount)
		if perfectRatio > cfg.MaxPerfectRatio {
			return fmt.Errorf("suspicious mechanical timing: %.1f%% identical blip intervals", perfectRatio*100)
		}
	}

	// Mechanical pattern (TAS): too many identical PING intervals
	if stats.PingCount > 20 {
		perfectPingRatio := float64(stats.PerfectPingIntervals) / float64(stats.PingCount)
		if perfectPingRatio > cfg.MaxPerfectPingRatio {
			return fmt.Errorf("suspicious mechanical timing: %.1f%% identical ping intervals", perfectPingRatio*100)
		}
	}

	return nil
}
