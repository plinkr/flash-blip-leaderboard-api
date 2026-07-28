package validator

import (
	"encoding/binary"
	"flash-blip-leaderboard-api/internal/models"
	"testing"
)

func TestParseInputs(t *testing.T) {
	// Create binary stream for 2 events
	// Event 1: Tick 100, Input ID 1 (BLIP)
	// Event 2: Tick 120, Input ID 2 (PING)
	raw := make([]byte, 10)
	binary.LittleEndian.PutUint32(raw[0:4], 100)
	raw[4] = 1
	binary.LittleEndian.PutUint32(raw[5:9], 120)
	raw[9] = 2

	events, err := ParseInputs(raw)
	if err != nil {
		t.Fatalf("unexpected error parsing inputs: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Tick != 100 || events[0].InputID != models.INPUT_BLIP {
		t.Errorf("event 0 incorrect: %+v", events[0])
	}

	if events[1].Tick != 120 || events[1].InputID != models.INPUT_PING {
		t.Errorf("event 1 incorrect: %+v", events[1])
	}
}

func TestValidateLightAndAnalyze(t *testing.T) {
	cfg := DefaultConfig()

	// Create a valid list of inputs (intervals of 20 ticks, which is 333ms)
	events := []models.InputEvent{
		{Tick: 10, InputID: models.INPUT_BLIP},
		{Tick: 30, InputID: models.INPUT_BLIP},
		{Tick: 50, InputID: models.INPUT_BLIP},
		{Tick: 70, InputID: models.INPUT_BLIP},
	}

	totalTicks := 100
	stats := Analyze(events, totalTicks)

	if stats.BlipCount != 4 {
		t.Errorf("expected 4 blips, got %d", stats.BlipCount)
	}

	err := ValidateLight(cfg, events, stats, 100, totalTicks)
	if err != nil {
		t.Errorf("unexpected error validation light: %v", err)
	}

	// Decreasing ticks should fail
	badEvents := []models.InputEvent{
		{Tick: 10, InputID: models.INPUT_BLIP},
		{Tick: 9, InputID: models.INPUT_BLIP},
	}
	badStats := Analyze(badEvents, totalTicks)
	err = ValidateLight(cfg, badEvents, badStats, 100, totalTicks)
	if err == nil {
		t.Error("expected validation to fail for decreasing ticks")
	}

	// Same-tick/overlapping events should pass
	sameTickEvents := []models.InputEvent{
		{Tick: 10, InputID: models.INPUT_BLIP},
		{Tick: 10, InputID: models.INPUT_PING},
	}
	sameTickStats := Analyze(sameTickEvents, totalTicks)
	err = ValidateLight(cfg, sameTickEvents, sameTickStats, 100, totalTicks)
	if err != nil {
		t.Errorf("unexpected error for same-tick events: %v", err)
	}

	// Ticks exceeding totalTicks should fail
	badEvents2 := []models.InputEvent{
		{Tick: 150, InputID: models.INPUT_BLIP},
	}
	badStats2 := Analyze(badEvents2, totalTicks)
	err = ValidateLight(cfg, badEvents2, badStats2, 100, totalTicks)
	if err == nil {
		t.Error("expected validation to fail when tick exceeds totalTicks")
	}

	// Zero or negative totalTicks should fail
	err = ValidateLight(cfg, events, stats, 100, 0)
	if err == nil {
		t.Error("expected validation to fail for totalTicks = 0")
	}
	err = ValidateLight(cfg, events, stats, 100, -10)
	if err == nil {
		t.Error("expected validation to fail for negative totalTicks")
	}
}

func TestSimulateScore(t *testing.T) {
	// Let's simulate a run with some inputs
	events := []models.InputEvent{
		{Tick: 10, InputID: models.INPUT_BLIP},
		{Tick: 150, InputID: models.INPUT_BLIP},
		{Tick: 300, InputID: models.INPUT_BLIP},
	}

	totalTicks := 600 // 10 seconds of gameplay
	rngSeed := int64(42)

	// Since simulated score depends on constant params and heuristic height bonus,
	// let's run simulation and verify we get a positive score.
	result := SimulateScore(events, totalTicks, rngSeed, 100, 1.0)

	if result.SimulatedScore <= 0 {
		t.Errorf("expected a positive simulated score, got %d", result.SimulatedScore)
	}

	// Check tolerance works
	if result.ToleranceLow >= result.SimulatedScore || result.ToleranceHigh <= result.SimulatedScore {
		t.Errorf("invalid tolerance bounds: low=%d simulated=%d high=%d", result.ToleranceLow, result.SimulatedScore, result.ToleranceHigh)
	}

	// Verify higher base difficulty yields a higher score
	resultHigher := SimulateScore(events, totalTicks, rngSeed, 100, 2.5)
	if resultHigher.SimulatedScore <= result.SimulatedScore {
		t.Errorf("expected higher difficulty (2.5) to yield a higher score than default (1.0), got %d <= %d", resultHigher.SimulatedScore, result.SimulatedScore)
	}
}

func TestMechanicalTimingSeparation(t *testing.T) {
	cfg := DefaultConfig()
	// Set thresholds explicitly for testing
	cfg.MaxPerfectRatio = 0.50
	cfg.MaxPerfectPingRatio = 0.80

	// Create a replay with 25 BLIPs having identical 10-tick intervals.
	// This should exceed cfg.MaxPerfectRatio (0.50) and fail.
	blipEvents := make([]models.InputEvent, 25)
	for i := range blipEvents {
		blipEvents[i] = models.InputEvent{
			Tick:    uint32(i * 10),
			InputID: models.INPUT_BLIP,
		}
	}
	statsBlip := Analyze(blipEvents, 1000)
	err := ValidateLight(cfg, blipEvents, statsBlip, 100, 1000)
	if err == nil {
		t.Error("expected validation to fail for high perfect blip ratio")
	}

	// Create a replay with 25 PINGs having identical 10-tick intervals.
	// This should exceed cfg.MaxPerfectPingRatio (0.80) and fail.
	pingEvents := make([]models.InputEvent, 25)
	for i := range pingEvents {
		pingEvents[i] = models.InputEvent{
			Tick:    uint32(i * 10),
			InputID: models.INPUT_PING,
		}
	}
	statsPing := Analyze(pingEvents, 1000)
	err = ValidateLight(cfg, pingEvents, statsPing, 100, 1000)
	if err == nil {
		t.Error("expected validation to fail for high perfect ping ratio")
	}

	// Create a mixed replay with:
	// - 25 BLIPs with different intervals (no perfect intervals)
	// - 25 PINGs with identical 10-tick intervals (perfect ratio = 100%)
	// But if we set cfg.MaxPerfectPingRatio to 1.1 (disabled), it should pass because blips are clean!
	mixedEvents := make([]models.InputEvent, 50)
	// Add 25 clean BLIPs (different intervals)
	tick := uint32(0)
	for i := range 25 {
		mixedEvents[i] = models.InputEvent{
			Tick:    tick,
			InputID: models.INPUT_BLIP,
		}
		tick += uint32(i + 5) // interval varies: 5, 6, 7, ...
	}
	// Add 25 perfect PINGs
	for i := range 25 {
		mixedEvents[25+i] = models.InputEvent{
			Tick:    tick + uint32(i*10),
			InputID: models.INPUT_PING,
		}
	}
	statsMixed := Analyze(mixedEvents, 2000)

	cfg.MaxPerfectPingRatio = 1.1
	err = ValidateLight(cfg, mixedEvents, statsMixed, 100, 2000)
	if err != nil {
		t.Errorf("expected validation to pass when MaxPerfectPingRatio is relaxed, got error: %v", err)
	}
}

func TestMaxBlipsAndPingsPerMinute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxPerfectRatio = 1.1
	cfg.MaxPerfectPingRatio = 1.1

	// Valid case within bounds (e.g. 2 blips/sec and 4 pings/sec in a 10s run)
	// 10 seconds = 600 ticks
	// 2 blips/sec * 10s = 20 blips (which is 120 blips/min, max is 180)
	// 4 pings/sec * 10s = 40 pings (which is 240 pings/min, max is 300)
	events := make([]models.InputEvent, 0, 60)
	for i := range 60 {
		inputId := models.INPUT_PING
		if i%3 == 0 {
			inputId = models.INPUT_BLIP
		}
		events = append(events, models.InputEvent{
			Tick:    uint32(i * 10), // distributed across 600 ticks
			InputID: inputId,
		})
	}

	stats := Analyze(events, 600)
	if err := ValidateLight(cfg, events, stats, 100, 600); err != nil {
		t.Errorf("expected validation to pass for human rates, got: %v", err)
	}

	// Case where blips exceed limits (> 180 per minute)
	// e.g. 4 blips/sec * 10s = 40 blips (which is 240 blips/min)
	badBlipEvents := make([]models.InputEvent, 0, 40)
	for i := range 40 {
		badBlipEvents = append(badBlipEvents, models.InputEvent{
			Tick:    uint32(i * 15),
			InputID: models.INPUT_BLIP,
		})
	}
	badBlipStats := Analyze(badBlipEvents, 600)
	if err := ValidateLight(cfg, badBlipEvents, badBlipStats, 100, 600); err == nil {
		t.Error("expected validation to fail when blip rate exceeds MaxBlipsPerMinute")
	}

	// Case where pings exceed limits (> 300 per minute)
	// e.g. 6 pings/sec * 10s = 60 pings (which is 360 pings/min)
	badPingEvents := make([]models.InputEvent, 0, 60)
	for i := range 60 {
		badPingEvents = append(badPingEvents, models.InputEvent{
			Tick:    uint32(i * 10),
			InputID: models.INPUT_PING,
		})
	}
	badPingStats := Analyze(badPingEvents, 600)
	if err := ValidateLight(cfg, badPingEvents, badPingStats, 100, 600); err == nil {
		t.Error("expected validation to fail when ping rate exceeds MaxPingsPerMinute")
	}
}
