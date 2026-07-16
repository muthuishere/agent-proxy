package ui

import (
	"testing"
	"time"

	"github.com/muthuishere/agent-proxy/internal/vault"
)

func TestEventStoreRequestThenResponseSameID(t *testing.T) {
	s, stop := NewEventStore(time.Hour)
	defer stop()

	s.RecordRequest("42-1", "api.anthropic.com", "/v1/messages", "POST",
		"original-secret-body", "masked-body",
		[]vault.Replacement{{Pattern: "ANTHROPIC_API_KEY", Surrogate: "sk-ant-fake", Original: "sk-ant-real"}})
	s.RecordResponse("42-1", 200, "upstream-body-with-surrogate", "restored-body", nil)

	got := s.Get("42-1")
	if got == nil {
		t.Fatal("expected event, got nil")
	}
	if got.OriginalReq != "original-secret-body" {
		t.Errorf("OriginalReq = %q", got.OriginalReq)
	}
	if got.RestoredResp != "restored-body" {
		t.Errorf("RestoredResp = %q", got.RestoredResp)
	}
	if got.Status != 200 {
		t.Errorf("Status = %d", got.Status)
	}
	if len(got.Replacements) != 1 || got.Replacements[0].Pattern != "ANTHROPIC_API_KEY" {
		t.Errorf("Replacements = %+v", got.Replacements)
	}
}

func TestEventStoreTTLEvicts(t *testing.T) {
	s, stop := NewEventStore(time.Hour)
	defer stop()
	frozen := time.Now()
	s.now = func() time.Time { return frozen }

	s.RecordRequest("e1", "h", "/p", "POST", "o", "m", nil)
	if got := s.Get("e1"); got == nil {
		t.Fatal("expected event present")
	}

	// Advance virtual time past TTL.
	s.now = func() time.Time { return frozen.Add(2 * time.Hour) }
	if got := s.Get("e1"); got != nil {
		t.Fatal("expected nil after TTL")
	}
}

func TestEventStoreReturnsNilForUnknownID(t *testing.T) {
	s, stop := NewEventStore(time.Hour)
	defer stop()
	if got := s.Get("never-recorded"); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}
