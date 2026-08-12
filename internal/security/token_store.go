package security

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

type SessionMetadata struct {
	PlayerName    string  `json:"p"`
	Score         int64   `json:"s"`
	RNGSeed       int64   `json:"rng_seed"`
	TotalTicks    int     `json:"total_ticks"`
	Difficulty    float64 `json:"difficulty"`
	ReplayVersion int     `json:"replay_version"`
}

type TokenSession struct {
	Token     string
	Nonce     string
	ExpiresAt time.Time
	Used      bool
	Metadata  SessionMetadata
}

type TokenStore struct {
	mu       sync.Mutex
	sessions map[string]*TokenSession
}

func NewTokenStore() *TokenStore {
	store := &TokenStore{
		sessions: make(map[string]*TokenSession),
	}
	// Start a background cleaner routine
	go store.cleaner(10 * time.Second)
	return store
}

func (s *TokenStore) Create(meta SessionMetadata) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := uuid.NewString()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	nonce := hex.EncodeToString(b)

	expiresAt := time.Now().Add(30 * time.Second)

	s.sessions[token] = &TokenSession{
		Token:     token,
		Nonce:     nonce,
		ExpiresAt: expiresAt,
		Used:      false,
		Metadata:  meta,
	}

	return token, nonce, nil
}

// Get retrieves the session if valid and not expired or used, without marking it used.
func (s *TokenStore) Get(token string) (*TokenSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[token]
	if !exists {
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) || session.Used {
		return nil, false
	}

	return session, true
}

// Consume marks the session as used if valid, unexpired, and not previously used.
func (s *TokenStore) Consume(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[token]
	if !exists {
		return false
	}

	if time.Now().After(session.ExpiresAt) || session.Used {
		return false
	}

	session.Used = true
	return true
}

// GetAndMarkUsed retrieves the session if valid, and immediately marks it used to prevent replay/race conditions.
func (s *TokenStore) GetAndMarkUsed(token string) (*TokenSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[token]
	if !exists {
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) {
		delete(s.sessions, token)
		return nil, false
	}

	if session.Used {
		return nil, false
	}

	session.Used = true

	return session, true
}

func (s *TokenStore) cleaner(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for token, sess := range s.sessions {
			if now.After(sess.ExpiresAt) || sess.Used {
				delete(s.sessions, token)
			}
		}
		s.mu.Unlock()
	}
}
