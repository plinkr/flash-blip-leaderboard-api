package models

// InputEvent an input event recorded during the game
type InputEvent struct {
	Tick    uint32
	InputID uint8
}

const (
	InputNone              = uint8(0)
	InputBlip              = uint8(1)
	InputPing              = uint8(2)
	InputMultiplierStarted = uint8(3)
	InputMultiplierEnded   = uint8(4)
)

// ReplayMeta replay metadata inside client JSON payload
type ReplayMeta struct {
	Version        int     `json:"v"`
	RNGSeed        int64   `json:"rng_seed"`
	TotalTicks     int     `json:"total_ticks"`
	BaseDifficulty float64 `json:"base_difficulty"`
	Inputs         string  `json:"inputs"`
}

// ScorePayload full body sent by the client
type ScorePayload struct {
	PlayerName string     `json:"p"`
	Score      int64      `json:"s"`
	Timestamp  int64      `json:"t"`
	Nonce      string     `json:"n"`
	Replay     ReplayMeta `json:"r"`
}

// ScoreResponse returned in GET /scores
type ScoreResponse struct {
	ID              int64   `json:"id"`
	PlayerName      string  `json:"player"`
	Score           int64   `json:"score"`
	TotalTicks      int     `json:"total_ticks"`
	DurationSeconds float64 `json:"duration_seconds"`
	AchievedAt      string  `json:"achieved_at"`
	ReplayID        int64   `json:"replay_id"`
}

// ReplayStats statistical analysis result
type ReplayStats struct {
	TotalInputs          int
	BlipCount            int
	PingCount            int
	MinTickInterval      int
	MaxTickInterval      int
	AvgTickInterval      float64
	PerfectIntervals     int
	PerfectPingIntervals int
}
