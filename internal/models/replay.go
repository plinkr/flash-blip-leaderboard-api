package models

// InputEvent: an input event recorded during the game
type InputEvent struct {
	Tick    uint32 // absolute tick from start of the run
	InputID uint8  // see constants below
}

// Input IDs: must match the Lua client exactly
const (
	INPUT_NONE = uint8(0)
	INPUT_BLIP = uint8(1) // jump to next circle
	INPUT_PING = uint8(2) // powerup detection expansion
)

// ReplayMeta: replay metadata inside client JSON payload
type ReplayMeta struct {
	Version        int     `json:"v"`
	RNGSeed        int64   `json:"rng_seed"`
	TotalTicks     int     `json:"total_ticks"`
	BaseDifficulty float64 `json:"base_difficulty"` // dynamic difficulty sent by the client based on the level
	Inputs         string  `json:"inputs"`          // base64(lz4(binary_stream))
}

// ScorePayload: full body sent by the client
type ScorePayload struct {
	PlayerName string     `json:"p"`
	Score      int64      `json:"s"`
	Timestamp  int64      `json:"t"`
	Nonce      string     `json:"n"`
	Replay     ReplayMeta `json:"r"`
}

// ScoreResponse: returned in GET /scores
type ScoreResponse struct {
	ID              int64   `json:"id"`
	PlayerName      string  `json:"player"`
	Score           int64   `json:"score"`
	TotalTicks      int     `json:"total_ticks"`
	DurationSeconds float64 `json:"duration_seconds"`
	AchievedAt      string  `json:"achieved_at"`
	ReplayID        int64   `json:"replay_id"`
}

// ReplayStats: statistical analysis result
type ReplayStats struct {
	TotalInputs          int
	BlipCount            int
	PingCount            int
	MinTickInterval      int // minimum ticks between consecutive BLIPs
	MaxTickInterval      int
	AvgTickInterval      float64
	PerfectIntervals     int // identical consecutive BLIP intervals (TAS pattern)
	PerfectPingIntervals int // identical consecutive PING intervals (TAS pattern)
}
