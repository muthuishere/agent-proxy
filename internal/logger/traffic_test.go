package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrafficLoggerWritesBodyPairsAndGatesOriginals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.jsonl")
	l, err := New(path, Options{
		LogRequestBody:  true,
		LogResponseBody: true,
		LogOriginals:    true,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	if err := l.LogRequest("api.anthropic.com", "/v1/messages", "POST", nil, "secret", "surrogate", 1, []string{"OPENAI_API_KEY"}, ""); err != nil {
		t.Fatalf("log request: %v", err)
	}
	if err := l.LogResponse("api.anthropic.com", "/v1/messages", 200, nil, "surrogate", "secret", 0, ""); err != nil {
		t.Fatalf("log response: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	lines := readJSONLines(t, path)
	if got := lines[0]["request_body"]; got != "secret" {
		t.Fatalf("request_body = %v", got)
	}
	if got := lines[0]["modified_request_body"]; got != "surrogate" {
		t.Fatalf("modified_request_body = %v", got)
	}
	if got := lines[1]["response_body"]; got != "surrogate" {
		t.Fatalf("response_body = %v", got)
	}
	if got := lines[1]["modified_response_body"]; got != "secret" {
		t.Fatalf("modified_response_body = %v", got)
	}
}

func TestTrafficLoggerOmitsOriginalsWhenDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.jsonl")
	l, err := New(path, Options{
		LogRequestBody: true,
		LogOriginals:   false,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	if err := l.LogRequest("api.anthropic.com", "/v1/messages", "POST", nil, "secret", "surrogate", 1, []string{"OPENAI_API_KEY"}, ""); err != nil {
		t.Fatalf("log request: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	lines := readJSONLines(t, path)
	if _, ok := lines[0]["request_body"]; ok {
		t.Fatal("request_body should be omitted when LogOriginals is false")
	}
	if got := lines[0]["modified_request_body"]; got != "surrogate" {
		t.Fatalf("modified_request_body = %v", got)
	}
}

func readJSONLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(data)), "\n")
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		var record map[string]any
		if err := json.Unmarshal([]byte(part), &record); err != nil {
			t.Fatalf("unmarshal %q: %v", part, err)
		}
		out = append(out, record)
	}
	return out
}

func TestTrafficLoggerEmitsRequestID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.jsonl")
	l, err := New(path, Options{LogRequestBody: true, LogResponseBody: true})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	const reqID = "42-7"
	if err := l.LogRequest("api.anthropic.com", "/v1/messages", "POST", nil, "", "masked", 0, nil, reqID); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}
	if err := l.LogResponse("api.anthropic.com", "/v1/messages", 200, nil, "", "restored", 0, reqID); err != nil {
		t.Fatalf("LogResponse: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	lines := readJSONLines(t, path)
	if len(lines) < 2 {
		t.Fatalf("expected 2 events, got %d", len(lines))
	}
	if lines[0]["id"] != reqID {
		t.Fatalf("request id = %v, want %q", lines[0]["id"], reqID)
	}
	if lines[1]["id"] != reqID {
		t.Fatalf("response id = %v, want %q", lines[1]["id"], reqID)
	}
}
