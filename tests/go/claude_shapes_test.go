// Tests covering specific Anthropic message shapes that are easy to get wrong:
// tool_use round-trip, image blocks (must pass through untouched), and error
// responses (must pass through untouched).
//
// See spec-slice-04-claude-end-to-end.md for the parent slice.
package acceptance_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

func nopCloser(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("readAll: %v", err)
	}
	return string(b)
}

// -----------------------------------------------------------------------------
// tool_use round-trip
// -----------------------------------------------------------------------------
//
// Real-world flow: user pastes a secret, Anthropic responds with a tool_use
// (e.g. Bash) whose `input.command` contains the surrogate. The proxy's
// response-side restore must put the original back so the agent runs the
// real command.
//
// This is the same correctness bar as the SSE round-trip but for non-streaming
// JSON (some clients prefer non-stream).

func TestClaudeShape_ToolUseInputRoundTrip(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "tool-use-1"

	// Request: user pastes an OpenAI key.
	body := `{"messages":[{"role":"user","content":"run: curl -H 'Authorization: Bearer sk-proj-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' https://api.openai.com/v1/chat/completions"}]}`
	mutated, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected mask")
	}
	masked := string(mutated)
	if !strings.Contains(masked, "sk-") {
		t.Fatalf("expected surrogate in masked body, got: %s", masked)
	}

	// Build a tool_use response that echoes the surrogate inside Bash input.
	// Find the surrogate that was substituted into the masked body.
	surr := extractSurrogate(t, masked, "sk-proj-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	resp := `{"id":"msg","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"curl -H 'Authorization: Bearer ` + surr + `' https://api.openai.com/v1/chat/completions"}}]}`

	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, []byte(resp), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	got := string(restored)
	if !strings.Contains(got, "sk-proj-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Fatalf("tool_use input did not restore original key:\n%s", got)
	}
	if strings.Contains(got, surr) {
		t.Fatalf("surrogate leaked into tool_use input")
	}
}

// -----------------------------------------------------------------------------
// Image block — large base64 image data must NOT be touched by the masking
// pipeline. Even though the encoded-blob path can detect base64, an image
// payload (JPG/PNG bytes) shouldn't contain anything resembling a secret, so
// the round trip must preserve the bytes byte-for-byte.
// -----------------------------------------------------------------------------

func TestClaudeShape_ImageBlockPassThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "image-1"

	// Synthesize a non-secret-looking 1 KB byte sequence, base64-encoded.
	raw := make([]byte, 1024)
	for i := range raw {
		raw[i] = byte((i * 7) & 0xff)
	}
	imgB64 := base64.StdEncoding.EncodeToString(raw)

	body := `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + imgB64 + `"}}]}]}`
	mutated, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{"Content-Type": []string{"application/json"}}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected zero masks for non-secret image, got %d", count)
	}
	if string(mutated) != body {
		t.Fatalf("image-only body must pass through unchanged.\nwant len=%d, got len=%d", len(body), len(mutated))
	}
}

// -----------------------------------------------------------------------------
// Error responses must pass through untouched.
// -----------------------------------------------------------------------------

func TestClaudeShape_ErrorResponsePassThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "err-1"

	// Pre-populate a session by masking a request.
	if _, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(`{"x":"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`), ""); err != nil {
		t.Fatalf("warmup HandleRequest: %v", err)
	}

	errBody := `{"type":"error","error":{"type":"overloaded_error","message":"Service is overloaded, please retry."}}`
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 529, http.Header{}, []byte(errBody), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if string(restored) != errBody {
		t.Fatalf("error body modified.\nwant: %s\ngot:  %s", errBody, string(restored))
	}
}

// -----------------------------------------------------------------------------
// SSE error event (e.g. mid-stream overloaded_error) must pass through
// without consuming the body or mangling other event fields.
// -----------------------------------------------------------------------------

func TestClaudeShape_SSEErrorEventPassThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "err-sse-1"

	// Warm up vault session so Restore has surrogates to scan for.
	if _, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(`{"x":"sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`), ""); err != nil {
		t.Fatalf("warmup: %v", err)
	}

	sseBody := `event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"Try again later."}}

`
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	reader, err := svc.HandleSSEResponse(sid, "api.anthropic.com", "/v1/messages", 200, headers, nopCloser(sseBody), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	out := readAll(t, reader)
	if !strings.Contains(out, `"type":"error"`) || !strings.Contains(out, "overloaded_error") {
		t.Fatalf("error event corrupted in SSE pass-through:\n%s", out)
	}
}
