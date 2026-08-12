package validator

import (
	"bytes"
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

const (
	ReplayVersionV1    = 1
	ReplayVersionV2    = 2
	MaxMultiplierTicks = 30 * 60

	// V2 replay format header constants (8 bytes total):
	// [0-3]: Magic bytes "FBRP" (FlashBlip RePlay)
	// [4]:   Replay version (2 for V2)
	// [5]:   Format subversion (1 = initial V2 format)
	// [6-7]: Reserved for future use (must be 0)
	replayV2HeaderSize   = 8
	replayV2EventSize    = 5
	replayV2FormatSubver = 1
	replayV2Reserved1    = 0
	replayV2Reserved2    = 0
)

var replayV2Magic = [4]byte{'F', 'B', 'R', 'P'}

func DefaultConfig() *Config {
	return &Config{
		MaxReplayVersion:     ReplayVersionV2,
		MinTicksPerScore:     0.5, // score cannot be > ticks * this_factor on average
		MaxBlipsPerMinute:    120, // 2 BLIPs/sec
		MaxPingsPerMinute:    240, // 4 PINGs/sec
		MinTicksBetweenBlips: 0,   // disabled (was 6) to allow instant jumps with Phase Shift/invulnerability
		MaxPerfectRatio:      0.8, // >80% identical intervals = suspicious
		MaxPerfectPingRatio:  0.9, // >90% identical intervals = suspicious
	}
}

// ParseReplay selects the parser from signed session metadata so old rows keep
// their original event interpretation after the v2 format is introduced.
func ParseReplay(version int, rawBytes []byte) ([]models.InputEvent, error) {
	switch version {
	case ReplayVersionV1:
		return ParseInputs(rawBytes)
	case ReplayVersionV2:
		return parseReplayV2(rawBytes)
	default:
		return nil, fmt.Errorf("unsupported replay version %d", version)
	}
}

func parseReplayV2(rawBytes []byte) ([]models.InputEvent, error) {
	if len(rawBytes) < replayV2HeaderSize {
		return nil, fmt.Errorf("v2 replay is missing its header")
	}

	if !bytes.Equal(rawBytes[:4], replayV2Magic[:]) ||
		rawBytes[4] != ReplayVersionV2 ||
		rawBytes[5] != replayV2FormatSubver ||
		rawBytes[6] != replayV2Reserved1 ||
		rawBytes[7] != replayV2Reserved2 {
		return nil, fmt.Errorf("invalid v2 replay header")
	}

	if len(rawBytes[replayV2HeaderSize:])%replayV2EventSize != 0 {
		return nil, fmt.Errorf("invalid v2 event stream length %d", len(rawBytes)-replayV2HeaderSize)
	}

	events := make([]models.InputEvent, (len(rawBytes)-replayV2HeaderSize)/replayV2EventSize)
	for i := range events {
		offset := replayV2HeaderSize + i*replayV2EventSize
		events[i] = models.InputEvent{
			Tick:    binary.LittleEndian.Uint32(rawBytes[offset : offset+4]),
			InputID: rawBytes[offset+4],
		}
		if events[i].InputID < models.INPUT_BLIP || events[i].InputID > models.INPUT_MULTIPLIER_ENDED {
			return nil, fmt.Errorf("unknown input id %d at index %d", events[i].InputID, i)
		}
	}

	for i := 1; i < len(events); i++ {
		if events[i].Tick < events[i-1].Tick {
			return nil, fmt.Errorf("events must be ordered by tick (event %d: tick %d < previous tick %d)", i, events[i].Tick, events[i-1].Tick)
		}
	}

	return events, nil
}

// ValidateV2 checks transitions that are not present in the v1 stream.
func ValidateV2(events []models.InputEvent, totalTicks int) error {
	if totalTicks <= 0 {
		return fmt.Errorf("invalid total_ticks: %d", totalTicks)
	}

	multiplierActive := false
	multiplierStartTick := uint32(0)
	for i, event := range events {
		if i > 0 && event.Tick < events[i-1].Tick {
			return fmt.Errorf("v2 event ticks decreasing at index %d", i)
		}
		if event.Tick == 0 || int(event.Tick) > totalTicks+1 {
			return fmt.Errorf("v2 event tick %d is outside 1..%d at index %d", event.Tick, totalTicks+1, i)
		}

		switch event.InputID {
		case models.INPUT_BLIP, models.INPUT_PING:
		case models.INPUT_MULTIPLIER_STARTED:
			if multiplierActive {
				return fmt.Errorf("multiplier started while already active at index %d", i)
			}
			multiplierActive = true
			multiplierStartTick = event.Tick
		case models.INPUT_MULTIPLIER_ENDED:
			if !multiplierActive {
				return fmt.Errorf("multiplier ended while inactive at index %d", i)
			}
			if uint64(event.Tick)-uint64(multiplierStartTick) > uint64(MaxMultiplierTicks) {
				return fmt.Errorf("multiplier interval exceeds %d ticks at index %d", MaxMultiplierTicks, i)
			}
			multiplierActive = false
		default:
			return fmt.Errorf("unknown v2 input id %d at index %d", event.InputID, i)
		}
	}
	if multiplierActive && uint64(totalTicks)-uint64(multiplierStartTick) > uint64(MaxMultiplierTicks) {
		return fmt.Errorf("multiplier interval exceeds %d ticks without an end event", MaxMultiplierTicks)
	}
	return nil
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
	if cfg == nil {
		cfg = DefaultConfig()
	}
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

	// Events can be recorded with record_next_tick(ticks+1) in the game,
	// so we allow events up to totalTicks + 1
	for _, e := range events {
		if int(e.Tick) > totalTicks+1 {
			return fmt.Errorf("input tick %d exceeds total_ticks+1 %d", e.Tick, totalTicks+1)
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
