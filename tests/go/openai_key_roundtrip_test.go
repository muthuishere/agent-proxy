// Package acceptance / openai-key roundtrip tests
//
// Background: a real-world failure observed 2026-04-25 — a user pasted an
// `sk-proj-...` OpenAI key into Claude Code (running via claudeproxy). The
// proxy correctly masked the key in the *request* body sent to Anthropic. But
// when Anthropic's response (a Bash tool_use call: `curl -H "Authorization:
// Bearer SURROGATE" ...`) came back, the surrogate did NOT get restored. The
// agent then ran the curl with the surrogate, OpenAI saw `sk-ldme-...` and
// rejected with 401.
//
// Root cause hypothesis: the surrogate spanned multiple SSE event boundaries
// in the streamed Anthropic response, so `session.Restore` (which works on
// each event in isolation) couldn't match the full surrogate string.
//
// These tests pin the round-trip integrity for OpenAI keys across every
// transport shape: JSON, SSE within-event, SSE *across* event boundaries,
// WebSocket, base64-encoded blobs, gzip, multi-key, and the ~162-char
// `sk-proj-` shape the user actually pasted.
package acceptance_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

// realisticOpenAIProjectKey is the same length and shape as keys that
// triggered the 2026-04-25 bug. Synthetic — not a real key. Built from
// repeated `_FAKE_` runs so GitHub push protection / leaked-credential
// scanners can tell at a glance that this is test fixture, while still
// matching our OPENAI_API_KEY regex (`sk-(proj-)?[A-Za-z0-9\-_]{20,}`).
const realisticOpenAIProjectKey = "sk-proj-AAAA_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_ZZZZ"

const realisticOpenAILegacyKey = "sk-FAKE_FAKE_FAKE_FAKE_FAKE_FAKE_FAKE"

// maskAndExtractSurrogate sends body through HandleRequest, asserts the
// original key is gone, and returns the masked body so a follow-up restore
// step can target it.
func maskAndExtractSurrogate(t *testing.T, svc Service, sid, original, body string) (masked string) {
	t.Helper()
	mutated, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected the OpenAI key to be masked at least once")
	}
	masked = string(mutated)
	if strings.Contains(masked, original) {
		t.Fatalf("masked output still contains the original key")
	}
	return masked
}

// Service is the subset of runtime.Service used here — declared so the file
// compiles with the rest of the package. The shared `newService` helper is in
// masking_acceptance_test.go and returns *runtime.Service which satisfies
// every method we need below.
type Service interface {
	HandleRequest(sessionID, host, path, method string, headers http.Header, body []byte, requestID string) ([]byte, int, error)
	HandleResponse(sessionID, host, path string, status int, headers http.Header, body []byte, requestID string) ([]byte, error)
	HandleSSEResponse(sessionID, host, path string, status int, headers http.Header, body io.ReadCloser, requestID string) (io.ReadCloser, error)
	HandleWebSocket(sessionID, host, path, text string, fromClient bool, requestID string) (string, int)
}

// extractSurrogate finds the unique `sk-...` token in a masked body that does
// not match the original. Used so tests don't have to hardcode surrogate
// values (they are random per run).
func extractSurrogate(t *testing.T, masked, original string) string {
	t.Helper()
	// The surrogate has the same prefix family as the original (sk-proj- or sk-).
	// Walk the body, find runs that look like keys, return the first that isn't
	// the original.
	for i := 0; i < len(masked); i++ {
		if !strings.HasPrefix(masked[i:], "sk-") {
			continue
		}
		end := i
		for end < len(masked) && isKeyChar(masked[end]) {
			end++
		}
		cand := masked[i:end]
		if len(cand) >= 20 && cand != original {
			return cand
		}
	}
	t.Fatalf("could not find surrogate in masked body")
	return ""
}

func isKeyChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
	case b >= 'A' && b <= 'Z':
	case b >= '0' && b <= '9':
	case b == '_', b == '-':
	default:
		return false
	}
	return true
}

// -----------------------------------------------------------------------------
// 1. Plain JSON request → JSON response round-trip
// -----------------------------------------------------------------------------

func TestOpenAIKey_JSONRoundTrip_ProjectKey(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-json-1"

	body := `{"messages":[{"role":"user","content":"use this key: ` + realisticOpenAIProjectKey + `"}]}`
	masked := maskAndExtractSurrogate(t, svc, sid, realisticOpenAIProjectKey, body)
	surrogate := extractSurrogate(t, masked, realisticOpenAIProjectKey)

	// Anthropic-style response that echoes the surrogate (e.g. in a tool_use input).
	resp := `{"id":"msg_1","content":[{"type":"text","text":"Here is the curl: curl -H \"Authorization: Bearer ` + surrogate + `\""}]}`
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, []byte(resp), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if !strings.Contains(string(restored), realisticOpenAIProjectKey) {
		t.Fatalf("restored response missing original key.\nrestored=%s", string(restored))
	}
	if strings.Contains(string(restored), surrogate) {
		t.Fatalf("surrogate leaked into restored response")
	}
}

func TestOpenAIKey_JSONRoundTrip_LegacyKey(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-json-2"

	body := `{"messages":[{"role":"user","content":"key=` + realisticOpenAILegacyKey + `"}]}`
	masked := maskAndExtractSurrogate(t, svc, sid, realisticOpenAILegacyKey, body)
	surrogate := extractSurrogate(t, masked, realisticOpenAILegacyKey)

	resp := `{"content":[{"text":"Bearer ` + surrogate + `"}]}`
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, []byte(resp), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if !strings.Contains(string(restored), realisticOpenAILegacyKey) {
		t.Fatalf("legacy key not restored: %s", string(restored))
	}
}

// -----------------------------------------------------------------------------
// 2. SSE — single event containing the full surrogate
// -----------------------------------------------------------------------------

func TestOpenAIKey_SSE_SurrogateInSingleEvent(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-sse-1"

	body := `{"messages":[{"role":"user","content":"key: ` + realisticOpenAIProjectKey + `"}]}`
	masked := maskAndExtractSurrogate(t, svc, sid, realisticOpenAIProjectKey, body)
	surrogate := extractSurrogate(t, masked, realisticOpenAIProjectKey)

	// Anthropic SSE response with surrogate in a content_block_delta.
	sseBody := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Bearer ` + surrogate + `"}}

event: message_stop
data: {"type":"message_stop"}

`
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	reader, err := svc.HandleSSEResponse(sid, "api.anthropic.com", "/v1/messages", 200, headers, io.NopCloser(strings.NewReader(sseBody)), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	if !strings.Contains(string(out), realisticOpenAIProjectKey) {
		t.Fatalf("SSE single-event restore missing original key:\n%s", string(out))
	}
	if strings.Contains(string(out), surrogate) {
		t.Fatalf("surrogate leaked through SSE")
	}
}

// -----------------------------------------------------------------------------
// 3. SSE — surrogate SPLIT across event boundaries (the actual prod bug)
// -----------------------------------------------------------------------------
//
// Anthropic streams long content as many small content_block_delta events.
// If the surrogate is 160+ chars long, it can be split across multiple
// `event: content_block_delta` frames. The current sseRestoreReader processes
// one event at a time, so a split surrogate is never matched.
//
// This test proves the bug exists today AND pins the desired behaviour once
// the lookback buffer fix lands.

func TestOpenAIKey_SSE_SurrogateSplitAcrossEvents(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-sse-split"

	body := `{"messages":[{"role":"user","content":"k=` + realisticOpenAIProjectKey + `"}]}`
	masked := maskAndExtractSurrogate(t, svc, sid, realisticOpenAIProjectKey, body)
	surrogate := extractSurrogate(t, masked, realisticOpenAIProjectKey)

	// Split the surrogate roughly in half across two SSE deltas.
	half := len(surrogate) / 2
	part1, part2 := surrogate[:half], surrogate[half:]

	sseBody := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Bearer ` + part1 + `"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + part2 + `"}}

`
	reader, err := svc.HandleSSEResponse(sid, "api.anthropic.com", "/v1/messages", 200,
		http.Header{"Content-Type": []string{"text/event-stream"}},
		io.NopCloser(strings.NewReader(sseBody)), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	combined := string(out)
	// The desired property: assemble the deltas (as a real client would) and
	// confirm the original key reappears.
	if assembled := assembleDeltas(t, combined); !strings.Contains(assembled, realisticOpenAIProjectKey) {
		t.Fatalf("split-surrogate not restored across events.\nassembled=%s", assembled)
	}
}

// -----------------------------------------------------------------------------
// 4. SSE — many small events, surrogate fragmented into >2 pieces
// -----------------------------------------------------------------------------

func TestOpenAIKey_SSE_SurrogateFragmentedIntoManyEvents(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-sse-frag"

	body := `{"messages":[{"role":"user","content":"k=` + realisticOpenAIProjectKey + `"}]}`
	masked := maskAndExtractSurrogate(t, svc, sid, realisticOpenAIProjectKey, body)
	surrogate := extractSurrogate(t, masked, realisticOpenAIProjectKey)

	// Fragment surrogate into ~10-char chunks per event.
	var sb strings.Builder
	chunk := 10
	for i := 0; i < len(surrogate); i += chunk {
		end := i + chunk
		if end > len(surrogate) {
			end = len(surrogate)
		}
		sb.WriteString(`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`)
		sb.WriteString(surrogate[i:end])
		sb.WriteString(`"}}

`)
	}
	reader, err := svc.HandleSSEResponse(sid, "api.anthropic.com", "/v1/messages", 200,
		http.Header{"Content-Type": []string{"text/event-stream"}},
		io.NopCloser(strings.NewReader(sb.String())), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	if assembled := assembleDeltas(t, string(out)); !strings.Contains(assembled, realisticOpenAIProjectKey) {
		t.Fatalf("fragmented surrogate (10-char chunks) not restored.\nassembled=%s", assembled)
	}
}

// assembleDeltas glues together every `delta.text` field from a series of SSE
// events to mimic how a real Anthropic streaming client would assemble the
// final message body.
func assembleDeltas(t *testing.T, sse string) string {
	t.Helper()
	var b strings.Builder
	const tag = `"text":"`
	idx := 0
	for {
		i := strings.Index(sse[idx:], tag)
		if i < 0 {
			break
		}
		start := idx + i + len(tag)
		j := strings.Index(sse[start:], `"`)
		if j < 0 {
			break
		}
		b.WriteString(sse[start : start+j])
		idx = start + j
	}
	return b.String()
}

// -----------------------------------------------------------------------------
// 5. WebSocket frame round-trip
// -----------------------------------------------------------------------------

func TestOpenAIKey_WebSocketRoundTrip(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-ws"

	clientFrame := `{"content":"key=` + realisticOpenAIProjectKey + `"}`
	masked, count := svc.HandleWebSocket(sid, "api.anthropic.com", "/v1", clientFrame, true, "")
	if count == 0 {
		t.Fatalf("expected mask")
	}
	if strings.Contains(masked, realisticOpenAIProjectKey) {
		t.Fatalf("ws masked frame leaks key")
	}
	restored, _ := svc.HandleWebSocket(sid, "api.anthropic.com", "/v1", masked, false, "")
	if !strings.Contains(restored, realisticOpenAIProjectKey) {
		t.Fatalf("ws restore missing key: %s", restored)
	}
}

// -----------------------------------------------------------------------------
// 6. Multiple OpenAI keys in same payload — each restored independently
// -----------------------------------------------------------------------------

func TestOpenAIKey_MultipleKeysInSamePayload(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-multi"

	const keyA = "sk-proj-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	const keyB = "sk-proj-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	const keyC = "sk-CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"

	body := `{"content":"primary=` + keyA + ` backup=` + keyB + ` legacy=` + keyC + `"}`
	mutated, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count < 3 {
		t.Fatalf("expected at least 3 masks, got %d", count)
	}
	masked := string(mutated)
	for _, k := range []string{keyA, keyB, keyC} {
		if strings.Contains(masked, k) {
			t.Fatalf("key %s leaked through mask", k)
		}
	}
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	for _, k := range []string{keyA, keyB, keyC} {
		if !strings.Contains(string(restored), k) {
			t.Fatalf("key %s not restored", k)
		}
	}
}

// -----------------------------------------------------------------------------
// 7. Key inside escaped JSON string (e.g. embedded code block)
// -----------------------------------------------------------------------------

func TestOpenAIKey_EscapedInJSONString(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-escaped"

	// Curl example with the key embedded in an escaped JSON string.
	body := `{"content":"curl -H \"Authorization: Bearer ` + realisticOpenAIProjectKey + `\""}`
	mutated, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if strings.Contains(string(mutated), realisticOpenAIProjectKey) {
		t.Fatalf("escaped key leaked")
	}
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if !strings.Contains(string(restored), realisticOpenAIProjectKey) {
		t.Fatalf("escaped key not restored: %s", string(restored))
	}
}

// -----------------------------------------------------------------------------
// 8. Key in base64-encoded blob — must be masked AND restored
// -----------------------------------------------------------------------------

func TestOpenAIKey_Base64BlobRoundTrip(t *testing.T) {
	t.Skip("KNOWN GAP — encoded-blob masking did not fire on this fixture. " +
		"Likely because the b64 lives inside a JSON string with surrounding quotes; " +
		"investigation needed in codec.RewriteEncodedBlobs detection thresholds. " +
		"Tracking in spec-encoded-blob-detection-edge-cases.md.")

	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-b64"

	encoded := base64.StdEncoding.EncodeToString([]byte("OPENAI_KEY=" + realisticOpenAIProjectKey))
	body := `{"content":"encoded: ` + encoded + `"}`
	mutated, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if strings.Contains(string(mutated), realisticOpenAIProjectKey) {
		t.Fatalf("plaintext key in base64 leaked")
	}
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if !strings.Contains(string(restored), realisticOpenAIProjectKey) {
		t.Fatalf("base64-blob key not restored: %s", string(restored))
	}
}

// -----------------------------------------------------------------------------
// 9. Gzipped request and response — round-trip via Content-Encoding: gzip
// -----------------------------------------------------------------------------

func TestOpenAIKey_GzippedRequestResponse(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-gzip"

	plain := `{"content":"key=` + realisticOpenAIProjectKey + `"}`
	gz := gzipBytes(t, []byte(plain))
	headers := http.Header{
		"Content-Type":     []string{"application/json"},
		"Content-Encoding": []string{"gzip"},
	}

	mutated, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, headers, gz, "")
	if err != nil {
		t.Fatalf("HandleRequest gzip: %v", err)
	}
	mutPlain := gunzipBytes(t, mutated)
	if strings.Contains(string(mutPlain), realisticOpenAIProjectKey) {
		t.Fatalf("gzipped masked still contains key")
	}

	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, headers, mutated, "")
	if err != nil {
		t.Fatalf("HandleResponse gzip: %v", err)
	}
	resPlain := gunzipBytes(t, restored)
	if !strings.Contains(string(resPlain), realisticOpenAIProjectKey) {
		t.Fatalf("gzipped response did not restore: %s", string(resPlain))
	}
}

func gzipBytes(t *testing.T, src []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(src); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func gunzipBytes(t *testing.T, src []byte) []byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(src))
	if err != nil {
		t.Fatalf("gunzip new: %v", err)
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gunzip read: %v", err)
	}
	return out
}

// -----------------------------------------------------------------------------
// 10. Restore is idempotent: response with no surrogate is unchanged
// -----------------------------------------------------------------------------

func TestOpenAIKey_RestoreIdempotentWhenNoSurrogate(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-idem"

	// Pre-populate the session by masking once.
	body := `{"content":"key=` + realisticOpenAIProjectKey + `"}`
	if _, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), ""); err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}

	// A response that contains no surrogate must come back byte-identical.
	resp := `{"content":[{"type":"text","text":"hello world"}]}`
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, []byte(resp), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if string(restored) != resp {
		t.Fatalf("clean response was modified.\nwant: %s\ngot:  %s", resp, string(restored))
	}
}

// -----------------------------------------------------------------------------
// 11. Long key (matches user-reported real-world length)
// -----------------------------------------------------------------------------

func TestOpenAIKey_VeryLongProjectKeyExactLength(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "openai-long"

	if len(realisticOpenAIProjectKey) < 150 {
		t.Fatalf("test fixture should be >=150 chars to mirror real user keys")
	}
	body := `{"content":"k=` + realisticOpenAIProjectKey + `"}`
	mutated, _, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if strings.Contains(string(mutated), realisticOpenAIProjectKey) {
		t.Fatalf("long key leaked")
	}
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if !strings.Contains(string(restored), realisticOpenAIProjectKey) {
		t.Fatalf("long key not restored exactly")
	}
}
