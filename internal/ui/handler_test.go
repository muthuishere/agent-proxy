package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muthuishere/agent-proxy/internal/vault"
)

func TestTrafficDetailEndpointReturnsRecordedEvent(t *testing.T) {
	store, stop := NewEventStore(time.Hour)
	defer stop()
	store.RecordRequest("9-1", "api.anthropic.com", "/v1/messages", "POST",
		"original body with sk-ant-api03-secret", "masked body with surrogate",
		[]vault.Replacement{{Pattern: "ANTHROPIC_API_KEY", Surrogate: "sk-ant-fake", Original: "sk-ant-real"}})
	store.RecordResponse("9-1", 200, "upstream body", "restored body", nil)

	srv := New(nil, store, "", "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/api/traffic/9-1", nil)
	w := httptest.NewRecorder()
	srv.handleTrafficDetail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got MaskingEvent
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RequestID != "9-1" {
		t.Errorf("RequestID = %q", got.RequestID)
	}
	if got.OriginalReq == "" || got.MaskedReq == "" {
		t.Errorf("missing request panes: %+v", got)
	}
	if got.RestoredResp == "" || got.Status != 200 {
		t.Errorf("missing response panes: %+v", got)
	}
	if len(got.Replacements) != 1 || got.Replacements[0].Pattern != "ANTHROPIC_API_KEY" {
		t.Errorf("Replacements = %+v", got.Replacements)
	}
}

func TestTrafficDetailEndpoint404OnUnknownID(t *testing.T) {
	store, stop := NewEventStore(time.Hour)
	defer stop()
	srv := New(nil, store, "", "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/api/traffic/missing-id", nil)
	w := httptest.NewRecorder()
	srv.handleTrafficDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestTrafficDetailEndpoint404WhenStoreNil(t *testing.T) {
	srv := New(nil, nil, "", "127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/api/traffic/anything", nil)
	w := httptest.NewRecorder()
	srv.handleTrafficDetail(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestTrafficListEndpointReturnsNewestFirst(t *testing.T) {
	store, stop := NewEventStore(time.Hour)
	defer stop()
	frozen := time.Now()
	store.now = func() time.Time { return frozen }

	store.RecordRequest("a-1", "api.anthropic.com", "/v1/messages", "POST", "o1", "m1", []vault.Replacement{{Pattern: "ANTHROPIC_API_KEY"}})
	frozen = frozen.Add(time.Second)
	store.RecordRequest("a-2", "api.openai.com", "/v1/chat", "POST", "o2", "m2", nil)

	srv := New(nil, store, "", "127.0.0.1:0")
	req := httptest.NewRequest(http.MethodGet, "/api/traffic", nil)
	w := httptest.NewRecorder()
	srv.handleTrafficList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got []MaskingEventSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].RequestID != "a-2" {
		t.Errorf("expected newest first, got order: %s, %s", got[0].RequestID, got[1].RequestID)
	}
	if got[1].ReplacementCount != 1 {
		t.Errorf("a-1 replacement_count = %d, want 1", got[1].ReplacementCount)
	}
	if !got[0].HasOriginals {
		t.Errorf("a-2 should have_originals")
	}
}
