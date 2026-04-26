package vaultscope

import (
	"sync"
	"time"
)

type entry struct {
	value     string
	expiresAt time.Time
}

type TTLVault struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]entry
	now     func() time.Time
}

func NewTTLVault(ttl time.Duration) *TTLVault {
	return &TTLVault{
		ttl:     ttl,
		entries: make(map[string]entry),
		now:     time.Now,
	}
}

func (v *TTLVault) Put(token, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.entries[token] = entry{
		value:     value,
		expiresAt: v.now().Add(v.ttl),
	}
}

func (v *TTLVault) Get(token string) (string, bool) {
	v.mu.RLock()
	item, ok := v.entries[token]
	v.mu.RUnlock()
	if !ok {
		return "", false
	}
	if !item.expiresAt.IsZero() && v.now().After(item.expiresAt) {
		v.mu.Lock()
		delete(v.entries, token)
		v.mu.Unlock()
		return "", false
	}
	return item.value, true
}

func (v *TTLVault) SweepExpired() int {
	now := v.now()
	removed := 0
	v.mu.Lock()
	defer v.mu.Unlock()
	for token, item := range v.entries {
		if !item.expiresAt.IsZero() && now.After(item.expiresAt) {
			delete(v.entries, token)
			removed++
		}
	}
	return removed
}

func (v *TTLVault) Size() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.entries)
}

type SessionVault struct {
	mu       sync.RWMutex
	sessions map[string]map[string]string
}

func NewSessionVault() *SessionVault {
	return &SessionVault{
		sessions: make(map[string]map[string]string),
	}
}

func (v *SessionVault) Put(sessionID, token, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.sessions[sessionID]; !ok {
		v.sessions[sessionID] = make(map[string]string)
	}
	v.sessions[sessionID][token] = value
}

func (v *SessionVault) Get(sessionID, token string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	session, ok := v.sessions[sessionID]
	if !ok {
		return "", false
	}
	value, ok := session[token]
	return value, ok
}

func (v *SessionVault) CloseSession(sessionID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.sessions, sessionID)
}

func (v *SessionVault) SessionCount() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.sessions)
}
