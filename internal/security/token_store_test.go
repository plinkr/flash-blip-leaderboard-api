package security

import (
	"testing"
	"time"
)

func TestTokenStore(t *testing.T) {
	store := NewTokenStore()

	meta := SessionMetadata{
		PlayerName: "Gamer123",
		Score:      5000,
		RNGSeed:    1337,
		TotalTicks: 300,
		Difficulty: 1.5,
	}

	token, nonce, err := store.Create(meta)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if token == "" {
		t.Error("expected non-empty token")
	}
	if len(nonce) != 64 { // 32 bytes hex-encoded is 64 chars
		t.Errorf("expected 64 char nonce, got %d", len(nonce))
	}

	// First get should succeed and mark used
	sess, ok := store.GetAndMarkUsed(token)
	if !ok {
		t.Fatal("expected session retrieval to succeed")
	}

	if sess.Metadata.PlayerName != "Gamer123" {
		t.Errorf("expected PlayerName Gamer123, got %s", sess.Metadata.PlayerName)
	}

	// Second get should fail as it was marked used
	_, ok2 := store.GetAndMarkUsed(token)
	if ok2 {
		t.Error("expected second retrieval to fail (already used)")
	}
}

func TestTokenStoreExpiration(t *testing.T) {
	store := NewTokenStore()
	meta := SessionMetadata{PlayerName: "Test"}

	token, _, _ := store.Create(meta)

	// Simulate expiration by modifying the internal map directly under lock
	store.mu.Lock()
	store.sessions[token].ExpiresAt = time.Now().Add(-1 * time.Second)
	store.mu.Unlock()

	_, ok := store.GetAndMarkUsed(token)
	if ok {
		t.Error("expected retrieval to fail for expired token")
	}
}

func TestTokenStoreGetAndConsume(t *testing.T) {
	store := NewTokenStore()
	meta := SessionMetadata{
		PlayerName: "Gamer456",
		Score:      10000,
	}

	token, _, err := store.Create(meta)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Get without consuming
	sess, ok := store.Get(token)
	if !ok {
		t.Fatal("expected Get to succeed")
	}
	if sess.Metadata.PlayerName != "Gamer456" {
		t.Errorf("expected PlayerName Gamer456, got %s", sess.Metadata.PlayerName)
	}

	// Calling Get a second time should still succeed (token not consumed yet)
	_, ok = store.Get(token)
	if !ok {
		t.Error("expected second Get to succeed before consume")
	}

	// Consume token
	if !store.Consume(token) {
		t.Error("expected Consume to succeed")
	}

	// Get after Consume should fail
	_, ok = store.Get(token)
	if ok {
		t.Error("expected Get to fail after Consume")
	}

	// Consume a second time should fail
	if store.Consume(token) {
		t.Error("expected second Consume to fail")
	}
}
