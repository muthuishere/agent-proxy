package ui

import (
	"sync"
	"time"

	"github.com/muthuishere/agent-proxy/internal/vault"
)

// MaskingEvent is the per-request payload the dashboard reads to show the
// four-pane (original req, masked req, original resp, restored resp) view plus
// the surrogate↔original mapping panel.
//
// Originals are sensitive (they contain secrets). The store evicts entries
// after TTL elapses; originals are never persisted to disk.
type MaskingEvent struct {
	RequestID    string                `json:"id"`
	Host         string                `json:"host"`
	Path         string                `json:"path"`
	Method       string                `json:"method"`
	Status       int                   `json:"status,omitempty"`
	OriginalReq  string                `json:"original_request,omitempty"`
	MaskedReq    string                `json:"masked_request,omitempty"`
	OriginalResp string                `json:"original_response,omitempty"`
	RestoredResp string                `json:"restored_response,omitempty"`
	Replacements []vault.Replacement   `json:"replacements,omitempty"`
	StartedAt    time.Time             `json:"started_at"`
	expiresAt    time.Time
}

// EventStore is an in-memory TTL ring keyed by request id. Originals never
// leave the process.
type EventStore struct {
	mu     sync.RWMutex
	events map[string]*MaskingEvent
	ttl    time.Duration
	now    func() time.Time
}

// NewEventStore creates a store with the given TTL. A janitor goroutine sweeps
// expired entries every ttl/4. The returned cleanup func stops the janitor.
func NewEventStore(ttl time.Duration) (*EventStore, func()) {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	s := &EventStore{
		events: make(map[string]*MaskingEvent),
		ttl:    ttl,
		now:    time.Now,
	}
	stop := make(chan struct{})
	go s.runJanitor(stop)
	return s, func() { close(stop) }
}

// RecordRequest stores (or extends) the request side of an event.
func (s *EventStore) RecordRequest(reqID, host, path, method, originalReq, maskedReq string, replacements []vault.Replacement) {
	if reqID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	ev, ok := s.events[reqID]
	if !ok {
		ev = &MaskingEvent{RequestID: reqID, StartedAt: now}
		s.events[reqID] = ev
	}
	ev.Host = host
	ev.Path = path
	ev.Method = method
	ev.OriginalReq = originalReq
	ev.MaskedReq = maskedReq
	ev.Replacements = replacements
	ev.expiresAt = now.Add(s.ttl)
}

// RecordResponse fills in the response side of an event.
func (s *EventStore) RecordResponse(reqID string, status int, originalResp, restoredResp string, replacements []vault.Replacement) {
	if reqID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	ev, ok := s.events[reqID]
	if !ok {
		ev = &MaskingEvent{RequestID: reqID, StartedAt: now}
		s.events[reqID] = ev
	}
	ev.Status = status
	ev.OriginalResp = originalResp
	ev.RestoredResp = restoredResp
	if len(replacements) > 0 {
		ev.Replacements = replacements
	}
	ev.expiresAt = now.Add(s.ttl)
}

// Get returns a copy of the event for the given id, or nil if not found / expired.
func (s *EventStore) Get(reqID string) *MaskingEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ev, ok := s.events[reqID]
	if !ok {
		return nil
	}
	if s.now().After(ev.expiresAt) {
		return nil
	}
	clone := *ev
	return &clone
}

// Len returns the current number of unexpired entries (best-effort; race-free
// only between sweeps).
func (s *EventStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// MaskingEventSummary is the lightweight per-row payload returned by
// GET /api/traffic — enough to render a list (host, path, status, timestamps,
// counts) without leaking originals.
type MaskingEventSummary struct {
	RequestID        string `json:"id"`
	Host             string `json:"host"`
	Path             string `json:"path"`
	Method           string `json:"method"`
	Status           int    `json:"status,omitempty"`
	StartedAt        int64  `json:"started_at_unix_ms"`
	ReplacementCount int    `json:"replacement_count"`
	HasOriginals     bool   `json:"has_originals"`
}

// List returns one summary row per event currently in the store. Newest
// first. Originals are NOT included — callers must use Get(id) for the full
// detail.
func (s *EventStore) List() []MaskingEventSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	out := make([]MaskingEventSummary, 0, len(s.events))
	for _, ev := range s.events {
		if now.After(ev.expiresAt) {
			continue
		}
		out = append(out, MaskingEventSummary{
			RequestID:        ev.RequestID,
			Host:             ev.Host,
			Path:             ev.Path,
			Method:           ev.Method,
			Status:           ev.Status,
			StartedAt:        ev.StartedAt.UnixMilli(),
			ReplacementCount: len(ev.Replacements),
			HasOriginals:     ev.OriginalReq != "" || ev.OriginalResp != "",
		})
	}
	// Newest first.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].StartedAt > out[j-1].StartedAt; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (s *EventStore) runJanitor(stop <-chan struct{}) {
	tick := time.NewTicker(s.ttl / 4)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			s.sweep()
		}
	}
}

func (s *EventStore) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for id, ev := range s.events {
		if now.After(ev.expiresAt) {
			delete(s.events, id)
		}
	}
}
