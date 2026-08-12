package validator

import (
	"encoding/binary"
	"flash-blip-leaderboard-api/internal/models"
	"math"
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

	totalTicks := 120
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

func TestParseAndValidateReplayV2(t *testing.T) {
	raw := []byte{'F', 'B', 'R', 'P', 2, 1, 0, 0}
	appendEvent := func(tick uint32, inputID uint8) {
		entry := make([]byte, 5)
		binary.LittleEndian.PutUint32(entry[:4], tick)
		entry[4] = inputID
		raw = append(raw, entry...)
	}
	appendEvent(10, models.INPUT_MULTIPLIER_STARTED)
	appendEvent(100, models.INPUT_MULTIPLIER_ENDED)

	events, err := ParseReplay(ReplayVersionV2, raw)
	if err != nil {
		t.Fatalf("failed to parse v2 replay: %v", err)
	}
	if err := ValidateV2(events, 600); err != nil {
		t.Fatalf("valid v2 replay rejected: %v", err)
	}

	bad := append([]models.InputEvent(nil), events...)
	bad[0].InputID = models.INPUT_MULTIPLIER_ENDED
	if err := ValidateV2(bad, 600); err == nil {
		t.Fatal("expected inactive multiplier transition to be rejected")
	}
}

func TestValidateV2MultiplierLifetime(t *testing.T) {
	withinWindow := []models.InputEvent{{Tick: 1, InputID: models.INPUT_MULTIPLIER_STARTED}}
	if err := ValidateV2(withinWindow, MaxMultiplierTicks+1); err != nil {
		t.Fatalf("expected an omitted end event within the lifetime to pass: %v", err)
	}

	tooLong := []models.InputEvent{{Tick: 1, InputID: models.INPUT_MULTIPLIER_STARTED}}
	if err := ValidateV2(tooLong, MaxMultiplierTicks+2); err == nil {
		t.Fatal("expected an omitted end event past the lifetime to fail")
	}

	refresh := []models.InputEvent{
		{Tick: 1, InputID: models.INPUT_MULTIPLIER_STARTED},
		{Tick: 2, InputID: models.INPUT_MULTIPLIER_STARTED},
	}
	if err := ValidateV2(refresh, 60); err == nil {
		t.Fatal("expected multiplier refresh without an end event to fail")
	}
}

func TestParseReplayV2RejectsReservedHeaderAndNoneInput(t *testing.T) {
	baseHeader := []byte{'F', 'B', 'R', 'P', 2, 1, 0, 0}
	reserved := append([]byte(nil), baseHeader...)
	reserved[6] = 1
	if _, err := ParseReplay(ReplayVersionV2, reserved); err == nil {
		t.Fatal("expected non-zero reserved header byte to fail")
	}

	withNone := append([]byte(nil), baseHeader...)
	withNone = append(withNone, []byte{1, 0, 0, 0, models.INPUT_NONE}...)
	if _, err := ParseReplay(ReplayVersionV2, withNone); err == nil {
		t.Fatal("expected INPUT_NONE to fail in v2")
	}
}

func TestSimulateReplayV2MultiplierWindow(t *testing.T) {
	baseEvents := []models.InputEvent{{Tick: 1, InputID: models.INPUT_MULTIPLIER_STARTED}, {Tick: 31, InputID: models.INPUT_MULTIPLIER_ENDED}}
	noMultiplier := simulateScoreV2ForTest(t, nil, 60)
	withMultiplier := simulateScoreV2ForTest(t, baseEvents, 60)

	if withMultiplier.SimulatedScore <= noMultiplier.SimulatedScore {
		t.Fatalf("multiplier did not increase score: no=%d with=%d", noMultiplier.SimulatedScore, withMultiplier.SimulatedScore)
	}
	if withMultiplier.ToleranceHigh >= int64(float64(noMultiplier.ToleranceHigh)*4) {
		t.Fatalf("v2 bound incorrectly grants a whole-run multiplier: no=%d with=%d", noMultiplier.ToleranceHigh, withMultiplier.ToleranceHigh)
	}
	// Golden values updated for accurate height-bonus simulation
	if noMultiplier.SimulatedScore != 76 || withMultiplier.SimulatedScore != 225 {
		t.Fatalf("unexpected v2 golden scores: no=%d with=%d", noMultiplier.SimulatedScore, withMultiplier.SimulatedScore)
	}
}

func TestSimulateReplayV2HighAltitudeAndTolerance(t *testing.T) {
	// Generate events where player blips every 30 ticks for 3000 ticks (50 seconds).
	// Player stays high in the screen (playerY < 30), so height bonus average > 1.0.
	totalTicks := 3000
	var events []models.InputEvent
	for tick := 30; tick < totalTicks; tick += 30 {
		events = append(events, models.InputEvent{
			Tick:    uint32(tick),
			InputID: models.INPUT_BLIP,
		})
	}

	// 1. Valid run where claimed score equals simulated score
	// In the old implementation (hardcoded +1.0 max bonus), high altitude runs exceeded maximumScore
	// and were falsely rejected. Centering tolerance on simulatedScore fixes this false rejection.
	simResult, err := SimulateReplay(ReplayVersionV2, events, totalTicks, 42, 0, 1.0)
	if err != nil {
		t.Fatalf("unexpected error simulating V2 high altitude replay: %v", err)
	}

	claimedScore := simResult.SimulatedScore
	resultValid, err := SimulateReplay(ReplayVersionV2, events, totalTicks, 42, claimedScore, 1.0)
	if err != nil {
		t.Fatalf("unexpected error simulating V2 high altitude replay: %v", err)
	}
	if !resultValid.Valid {
		t.Fatalf("legitimate high-altitude replay was falsely rejected: claimed=%d simulated=%d bounds=[%d, %d]",
			claimedScore, resultValid.SimulatedScore, resultValid.ToleranceLow, resultValid.ToleranceHigh)
	}

	// 2. Cheater claiming score far above upper tolerance
	inflatedScore := resultValid.ToleranceHigh + 1000
	resultCheaterHigh, err := SimulateReplay(ReplayVersionV2, events, totalTicks, 42, inflatedScore, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resultCheaterHigh.Valid {
		t.Fatalf("cheater score (%d) was accepted; expected failure against upper tolerance %d",
			inflatedScore, resultCheaterHigh.ToleranceHigh)
	}

	// 3. Score far below lower tolerance
	deflatedScore := resultValid.ToleranceLow - 500
	resultCheaterLow, err := SimulateReplay(ReplayVersionV2, events, totalTicks, 42, deflatedScore, 1.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resultCheaterLow.Valid {
		t.Fatalf("deflated score (%d) was accepted; expected failure against lower tolerance %d",
			deflatedScore, resultCheaterLow.ToleranceLow)
	}
}

func simulateScoreV2ForTest(t *testing.T, events []models.InputEvent, totalTicks int) SimulateScoreResult {
	t.Helper()
	result, err := SimulateReplay(ReplayVersionV2, events, totalTicks, 42, 100, 1)
	if err != nil {
		t.Fatalf("v2 simulation failed: %v", err)
	}
	return result
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

func TestValidateBaseDifficulty(t *testing.T) {
	valid := []float64{1.0, 2.0, 3.5}
	for _, diff := range valid {
		if err := ValidateBaseDifficulty(diff); err != nil {
			t.Errorf("expected difficulty %f to be valid, got: %v", diff, err)
		}
	}

	invalid := []float64{0.9, 3.6, -1.0, math.NaN(), math.Inf(1)}
	for _, diff := range invalid {
		if err := ValidateBaseDifficulty(diff); err == nil {
			t.Errorf("expected difficulty %f to be invalid", diff)
		}
	}
}
