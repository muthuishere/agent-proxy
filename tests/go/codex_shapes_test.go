// Tests covering OpenAI / Codex request and response shapes end-to-end.
//
// These tests exercise the full runtime.Service stack against synthesized
// OpenAI chat-completion payloads (JSON, SSE) and the WebSocket path on
// api.openai.com. No live API or running proxy is required.
//
// See spec-slice-06-codex-end-to-end.md for the parent slice.
package acceptance_test

import (
	"net/http"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// 1. Plain JSON chat completion round-trip (api.openai.com)
// -----------------------------------------------------------------------------

func TestCodexChatCompletionRoundTrip(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "codex-json-1"

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"k=` + realisticOpenAIProjectKey + `"}]}`
	mutated, count, err := svc.HandleRequest(sid, "api.openai.com", "/v1/chat/completions", http.MethodPost, http.Header{"Content-Type": []string{"application/json"}}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected the OpenAI key to be masked")
	}
	masked := string(mutated)
	if strings.Contains(masked, realisticOpenAIProjectKey) {
		t.Fatalf("masked body still contains the original key:\n%s", masked)
	}
	surrogate := extractSurrogate(t, masked, realisticOpenAIProjectKey)

	// Synthesize an OpenAI chat-completion JSON response that echoes the
	// surrogate inside assistant content (e.g. "I see your key: <surrogate>").
	resp := `{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"echoing key: ` + surrogate + `"},"finish_reason":"stop"}]}`
	restored, err := svc.HandleResponse(sid, "api.openai.com", "/v1/chat/completions", 200, http.Header{}, []byte(resp), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	got := string(restored)
	if !strings.Contains(got, realisticOpenAIProjectKey) {
		t.Fatalf("response did not restore original key:\n%s", got)
	}
	if strings.Contains(got, surrogate) {
		t.Fatalf("surrogate leaked into restored response:\n%s", got)
	}
}

// -----------------------------------------------------------------------------
// 2. SSE chat completion stream — surrogate inside delta.content
// -----------------------------------------------------------------------------
//
// OpenAI's streaming response uses {"choices":[{"delta":{"content":"..."}}]}
// — note `delta.content`, NOT `delta.text` / `text_delta` like Anthropic.
// The delta-aware SSE reader in internal/runtime/sse_delta.go currently only
// recognises Anthropic's text_delta shape. For OpenAI events the reader
// treats them as opaque and runs codec.RestoreEncodedBlobs over the raw event
// bytes, which itself invokes session.Restore — so a surrogate that fits in
// a single event IS restored. Cross-event surrogate splits over OpenAI deltas
// are NOT yet handled and need a follow-up spec.
//
// This test verifies the single-event case (which works today) and is marked
// to skip the cross-event split case until OpenAI-shaped delta parsing lands.

func TestCodexChatCompletionSSE(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "codex-sse-1"

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"k=` + realisticOpenAIProjectKey + `"}]}`
	masked := maskAndExtractSurrogate(t, svc, sid, realisticOpenAIProjectKey, body)
	surrogate := extractSurrogate(t, masked, realisticOpenAIProjectKey)

	// OpenAI streaming response: each line is `data: {json}` ending with
	// `data: [DONE]`. Place the full surrogate inside one delta so the
	// opaque-event byte-level Restore can still recover it.
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"echoing key: ` + surrogate + `"}}]}`,
		"",
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
		"",
	}, "\n")

	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	reader, err := svc.HandleSSEResponse(sid, "api.openai.com", "/v1/chat/completions", 200, headers, nopCloser(sse), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	defer reader.Close()

	out := readAll(t, reader)
	if !strings.Contains(out, realisticOpenAIProjectKey) {
		t.Fatalf("original key not restored in OpenAI SSE stream\nout: %s", out)
	}
	if strings.Contains(out, surrogate) {
		t.Fatalf("surrogate leaked into OpenAI SSE output\nout: %s", out)
	}

	// TODO: once OpenAI-shaped delta-aware SSE parsing lands (delta.content
	// instead of delta.text/text_delta), add a dedicated test mirroring
	// TestOpenAIKey_SSE_SurrogateSplitAcrossEvents that splits the surrogate
	// across two `delta.content` chunks. Tracked as a follow-up spec.
}

// -----------------------------------------------------------------------------
// 3. Unlisted domain pass-through (Codex non-disruption guarantee)
// -----------------------------------------------------------------------------

func TestCodexUnlistedDomainPassesThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// stripe.com is not in detection.intercepted_domains; a request carrying
	// what would otherwise be a maskable secret must pass through untouched.
	body := `{"messages":[{"role":"user","content":"k=` + realisticOpenAIProjectKey + `"}]}`
	mutated, count, err := svc.HandleRequest("codex-pass-1", "stripe.com", "/v1/charges", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected count=0 for unlisted domain, got %d", count)
	}
	if string(mutated) != body {
		t.Fatalf("body modified for unlisted domain\nwant: %s\ngot:  %s", body, string(mutated))
	}
}

// -----------------------------------------------------------------------------
// 4. WebSocket round-trip on api.openai.com
// -----------------------------------------------------------------------------
//
// chatgpt.com WebSocket coverage already exists in masking_acceptance_test.go
// (TestHandleWebSocket_ChatGPTDomainIntercepted). Codex itself uses chatgpt.com
// for the WebSocket transport today, not api.openai.com — but the registry
// maps both domains to the OpenAIProvider, and any future api.openai.com
// WebSocket usage must mask the same way. This test pins that behaviour.

func TestCodexWebSocketRoundTrip(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	if !svc.ShouldIntercept("api.openai.com") {
		t.Fatalf("api.openai.com must be in intercepted domain set")
	}
	const sid = "codex-ws-1"
	frame := `{"type":"conversation.item.create","item":{"content":"k=` + realisticOpenAIProjectKey + `"}}`
	roundTripWebSocket(t, svc, sid, "api.openai.com", frame, []string{realisticOpenAIProjectKey})
}
