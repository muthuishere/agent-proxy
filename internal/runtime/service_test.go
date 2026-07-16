package runtime

import (
	"encoding/base64"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/agent-proxy/internal/config"
	"github.com/muthuishere/agent-proxy/internal/vault"
)

func TestHandleRequestMasksAndRestoresResponse(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
	body := []byte(`{"messages":[{"content":"` + secret + `"}]}`)

	mutated, maskedCount, err := service.HandleRequest("s1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, body, "")
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if maskedCount == 0 || strings.Contains(string(mutated), secret) {
		t.Fatalf("expected masked request, got %q", string(mutated))
	}
	if strings.Contains(string(mutated), "*") || strings.Contains(string(mutated), "[ANTHROPIC_API_KEY:") {
		t.Fatalf("expected shape-preserving surrogate, got %q", string(mutated))
	}
	if strings.Contains(strings.ToLower(string(mutated)), "dummy") || strings.Contains(strings.ToLower(string(mutated)), "mask") {
		t.Fatalf("surrogate should not use dummy/mask labels, got %q", string(mutated))
	}

	restored, err := service.HandleResponse("s1", "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("handle response: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("expected restored response, got %q", string(restored))
	}
}

func TestHandleRequestUsesShapePreservingSurrogateForPasswordAssignment(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	secret := `PASSWORD=real-secret-value`
	body := []byte(`{"content":"run with ` + secret + `"}`)

	mutated, maskedCount, err := service.HandleRequest("semantic1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, body, "")
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	out := string(mutated)
	if maskedCount == 0 || strings.Contains(out, secret) {
		t.Fatalf("expected secret assignment masked, count=%d body=%q", maskedCount, out)
	}
	if !strings.Contains(out, `PASSWORD=`) || strings.Contains(out, "*") {
		t.Fatalf("expected assignment-shaped surrogate, got %q", out)
	}
	if strings.Contains(strings.ToLower(out), "dummy") || strings.Contains(strings.ToLower(out), "mask") {
		t.Fatalf("surrogate should not use dummy/mask labels, got %q", out)
	}

	restored, err := service.HandleResponse("semantic1", "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("handle response: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("expected restored response, got %q", string(restored))
	}
}

func TestHandleRequestLeavesUnlistedDomainUntouched(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	body := []byte(`{"messages":[{"content":"sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"}]}`)
	mutated, maskedCount, err := service.HandleRequest("s1", "stripe.com", "/v1/charges", http.MethodPost, http.Header{}, body, "")
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if maskedCount != 0 || string(mutated) != string(body) {
		t.Fatalf("expected passthrough body, got count=%d body=%q", maskedCount, string(mutated))
	}
}

func TestHandleRequestMasksEncodedBase64Secrets(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	secret := "postgres://user:pass@localhost:5432/prod"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	body := []byte(`{"content":"` + encoded + `"}`)

	mutated, maskedCount, err := service.HandleRequest("s1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, body, "")
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if maskedCount == 0 || strings.Contains(string(mutated), secret) {
		t.Fatalf("expected encoded secret to be masked, got %q", string(mutated))
	}

	restored, err := service.HandleResponse("s1", "api.anthropic.com", "/v1/messages", 200, http.Header{}, mutated, "")
	if err != nil {
		t.Fatalf("handle response: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("expected encoded body to round trip, got %q", string(restored))
	}
}

func TestHandleRequestPIIDisabledLeavesEmailUntouched(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	body := []byte(`{"content":"email alice@example.com"}`)
	mutated, maskedCount, err := service.HandleRequest("s1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, body, "")
	if err != nil {
		t.Fatalf("handle request: %v", err)
	}
	if maskedCount != 0 || !strings.Contains(string(mutated), "alice@example.com") {
		t.Fatalf("expected email to remain, got count=%d body=%q", maskedCount, string(mutated))
	}
}

func TestHandleSSEResponseRestoresTokensPerEvent(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"
	body := []byte(`{"messages":[{"content":"` + secret + `"}]}`)

	// Mask the secret in a request so the vault holds the token.
	masked, maskedCount, err := service.HandleRequest("sse1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, body, "")
	if err != nil || maskedCount == 0 {
		t.Fatalf("expected masked request, count=%d err=%v", maskedCount, err)
	}

	// Build a synthetic SSE response where the masked token appears across two events.
	event1 := "event: message\ndata: " + string(masked)
	event2 := "event: done\ndata: [DONE]"
	sseBody := event1 + "\n\n" + event2 + "\n\n"

	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	rc := io.NopCloser(strings.NewReader(sseBody))

	sseReader, err := service.HandleSSEResponse("sse1", "api.anthropic.com", "/v1/messages", 200, headers, rc, "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	defer sseReader.Close()

	out, err := io.ReadAll(sseReader)
	if err != nil {
		t.Fatalf("read SSE output: %v", err)
	}
	result := string(out)

	if strings.Contains(result, string(masked)) {
		t.Fatalf("expected vault token to be restored, still masked in output:\n%s", result)
	}
	if !strings.Contains(result, secret) {
		t.Fatalf("expected original secret in output, got:\n%s", result)
	}
	if !strings.Contains(result, "[DONE]") {
		t.Fatalf("expected second event to pass through, got:\n%s", result)
	}
}

func TestHandleSSEResponsePassesThroughUnlistedDomain(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	sseBody := "event: message\ndata: hello\n\n"
	rc := io.NopCloser(strings.NewReader(sseBody))
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}

	out, err := service.HandleSSEResponse("s1", "stripe.com", "/events", 200, headers, rc, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unlisted domain: original ReadCloser is returned unchanged.
	if out != rc {
		t.Fatalf("expected original body to be returned for unlisted domain")
	}
}

func TestHandleSSEResponsePreservesEventStructure(t *testing.T) {
	service := newTestService(t, false)
	defer service.Close()

	sseBody := "id: 1\nevent: ping\ndata: keep-alive\n\nid: 2\nevent: message\ndata: hello world\n\n"
	rc := io.NopCloser(strings.NewReader(sseBody))
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}

	reader, err := service.HandleSSEResponse("s1", "api.anthropic.com", "/v1/messages", 200, headers, rc, "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	defer reader.Close()

	out, _ := io.ReadAll(reader)
	result := string(out)

	for _, want := range []string{"id: 1", "event: ping", "data: keep-alive", "id: 2", "event: message", "data: hello world"} {
		if !strings.Contains(result, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, result)
		}
	}
}

func newTestService(t *testing.T, piiEnabled bool) *Service {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "..", "config", "agentproxy.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.PII.Enabled = piiEnabled
	cfg.Logging.LogFile = filepath.Join(t.TempDir(), "traffic.jsonl")
	service, err := New(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

// fakeSink captures sink calls for assertions.
type fakeSink struct {
	reqCalls  int
	respCalls int
	lastReq   struct {
		reqID, host, path, method, original, masked string
		reps                                        []vault.Replacement
	}
	lastResp struct {
		reqID                  string
		status                 int
		original, restored     string
		reps                   []vault.Replacement
	}
}

func (f *fakeSink) RecordRequest(reqID, host, path, method, original, masked string, reps []vault.Replacement) {
	f.reqCalls++
	f.lastReq.reqID = reqID
	f.lastReq.host = host
	f.lastReq.path = path
	f.lastReq.method = method
	f.lastReq.original = original
	f.lastReq.masked = masked
	f.lastReq.reps = reps
}

func (f *fakeSink) RecordResponse(reqID string, status int, original, restored string, reps []vault.Replacement) {
	f.respCalls++
	f.lastResp.reqID = reqID
	f.lastResp.status = status
	f.lastResp.original = original
	f.lastResp.restored = restored
	f.lastResp.reps = reps
}

func TestServiceEmitsToSinkWithOriginalsAndReplacements(t *testing.T) {
	svc := newTestService(t, false)
	defer svc.Close()
	sink := &fakeSink{}
	svc.SetSink(sink)

	const reqID = "99-1"
	const sid = "sink-test"
	body := []byte(`{"messages":[{"role":"user","content":"key=sk-ant-api03-realsecretkeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}`)
	headers := http.Header{"Content-Type": []string{"application/json"}}

	masked, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, headers, body, reqID)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected masking to happen")
	}
	if sink.reqCalls != 1 {
		t.Fatalf("sink.RecordRequest calls = %d, want 1", sink.reqCalls)
	}
	if sink.lastReq.reqID != reqID {
		t.Errorf("reqID = %q", sink.lastReq.reqID)
	}
	if !strings.Contains(sink.lastReq.original, "sk-ant-api03-realsecret") {
		t.Errorf("original missing secret: %q", sink.lastReq.original)
	}
	if strings.Contains(sink.lastReq.masked, "sk-ant-api03-realsecret") {
		t.Errorf("masked still contains secret: %q", sink.lastReq.masked)
	}
	if len(sink.lastReq.reps) == 0 {
		t.Errorf("expected replacements in sink call")
	}

	if _, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, masked, reqID); err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if sink.respCalls != 1 {
		t.Fatalf("sink.RecordResponse calls = %d, want 1", sink.respCalls)
	}
	if sink.lastResp.reqID != reqID {
		t.Errorf("response reqID = %q", sink.lastResp.reqID)
	}
}
