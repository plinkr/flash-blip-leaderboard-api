-- migrations/001_init.sql

CREATE TABLE IF NOT EXISTS scores (
    id              BIGSERIAL    PRIMARY KEY,
    player_name     VARCHAR(32)  NOT NULL,
    score           BIGINT       NOT NULL CHECK (score >= 0),
    total_ticks     INTEGER      NOT NULL CHECK (total_ticks > 0),
    client_ts       BIGINT       NOT NULL,
    server_ts       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    ip_address      INET,
    api_key_ver     SMALLINT     NOT NULL DEFAULT 1,
    nonce           VARCHAR(64)  NOT NULL,
    validated       BOOLEAN,                   -- NULL=pending, TRUE=ok, FALSE=rejected
    validated_at    TIMESTAMPTZ,
    simulated_score BIGINT,
    reject_reason   VARCHAR(128),
    CONSTRAINT uq_nonce UNIQUE (nonce)
);

CREATE TABLE IF NOT EXISTS replays (
    id              BIGSERIAL    PRIMARY KEY,
    score_id        BIGINT       NOT NULL UNIQUE REFERENCES scores(id) ON DELETE CASCADE,
    replay_version  SMALLINT     NOT NULL DEFAULT 1,
    rng_seed        BIGINT       NOT NULL,
    base_difficulty REAL NOT NULL DEFAULT 1.0,
    total_ticks     INTEGER      NOT NULL,
    data            BYTEA        NOT NULL,
    input_count     INTEGER      NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS reports (
    id              BIGSERIAL    PRIMARY KEY,
    score_id        BIGINT       NOT NULL REFERENCES scores(id) ON DELETE CASCADE,
    reporter_name   VARCHAR(32),
    reason          VARCHAR(256),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_reports_score_reporter UNIQUE (score_id, reporter_name)
);

CREATE TABLE IF NOT EXISTS used_nonces (
    nonce       VARCHAR(64)  PRIMARY KEY,
    used_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_nonces_used_at ON used_nonces (used_at);

CREATE INDEX IF NOT EXISTS idx_scores_validated_score
ON scores (validated, score DESC);

CREATE OR REPLACE VIEW top_scores AS
SELECT
    s.id,
    s.player_name,
    s.score,
    s.total_ticks,
    s.server_ts   AS achieved_at,
    r.id          AS replay_id,
    ROUND(s.total_ticks::numeric / 60, 1) AS duration_seconds
FROM scores s
LEFT JOIN replays r ON r.score_id = s.id
WHERE s.validated = TRUE
ORDER BY s.score DESC;