// Synthetic-fixture tests for GitHub Copilot end-to-end masking.
//
// No real fixture has been captured yet (Slice 05 task 1 / 12 still open),
// so these tests model Copilot's request/response on the OpenAI
// chat-completion shape that `api.githubcopilot.com` is known to expose.
// The point is to lock in masking pipeline correctness for
// `api.githubcopilot.com` and to assert that `api.github.com` (bare GitHub
// REST API, used by `gh` commands) is NOT intercepted — see
// docs/specs/spec-slice-05-copilot-end-to-end.md and CLAUDE.md.
package acceptance_test

import (
	"net/http"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Suggest-style request: a chat-completions-shaped body with a fake-but-valid
// OpenAI project key embedded in the user message.
// -----------------------------------------------------------------------------

func TestCopilotSuggestRoundTrip(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "copilot-suggest-1"

	const fakeKey = "sk-proj-FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE"
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"explain how to authenticate with key ` + fakeKey + `"}],"stream":false}`

	mutated, count, err := svc.HandleRequest(sid, "api.githubcopilot.com", "/chat/completions", http.MethodPost, http.Header{"Content-Type": []string{"application/json"}}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected secret masked, got count=0\nbody: %s", body)
	}
	masked := string(mutated)
	if strings.Contains(masked, fakeKey) {
		t.Fatalf("OpenAI key still present after masking: %s", masked)
	}

	// Synthesize a chat-completion response that echoes the surrogate, then
	// verify HandleResponse restores the original key.
	surr := extractSurrogate(t, masked, fakeKey)
	resp := `{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"your key ` + surr + ` looks fine"}}]}`
	restored, err := svc.HandleResponse(sid, "api.githubcopilot.com", "/chat/completions", 200, http.Header{}, []byte(resp), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	got := string(restored)
	if !strings.Contains(got, fakeKey) {
		t.Fatalf("expected original key restored in response, got: %s", got)
	}
	if strings.Contains(got, surr) {
		t.Fatalf("surrogate leaked into restored response: %s", got)
	}
}

// -----------------------------------------------------------------------------
// Chat stream: SSE-shaped response carrying the surrogate inside a
// content_block_delta-style event. Modeled on Anthropic's streaming shape;
// the assertion is that the streaming reader restores the original.
// -----------------------------------------------------------------------------

func TestCopilotChatStreamRoundTrip(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	const sid = "copilot-stream-1"

	const fakeKey = "sk-proj-FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE"
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"key=` + fakeKey + `"}],"stream":true}`

	mutated, count, err := svc.HandleRequest(sid, "api.githubcopilot.com", "/chat/completions", http.MethodPost, http.Header{"Content-Type": []string{"application/json"}}, []byte(body), "")
	if err != nil || count == 0 {
		t.Fatalf("mask step: count=%d err=%v", count, err)
	}
	masked := string(mutated)
	surr := extractSurrogate(t, masked, fakeKey)

	// Two SSE events; the first carries the surrogate, the second is a stop.
	sseBody := "event: content_block_delta\ndata: {\"delta\":{\"text\":\"" + surr + "\"}}\n\n" +
		"event: message_stop\ndata: [DONE]\n\n"

	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	reader, err := svc.HandleSSEResponse(sid, "api.githubcopilot.com", "/chat/completions", 200, headers, nopCloser(sseBody), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	defer reader.Close()
	out := readAll(t, reader)
	if strings.Contains(out, surr) {
		t.Fatalf("surrogate should have been restored in SSE output:\n%s", out)
	}
	if !strings.Contains(out, fakeKey) {
		t.Fatalf("original key should appear in restored SSE output:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// Non-disruption: `api.github.com` (bare GitHub REST API) must NOT be
// intercepted. Modeled on TestUnlistedDomainPassesThrough.
// -----------------------------------------------------------------------------

func TestCopilotApiGitHubComPassesThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	if svc.ShouldIntercept("api.github.com") {
		t.Fatalf("api.github.com must not be in the intercepted domain set; intercepting it MITMs every regular `gh` command")
	}

	// A `gh` command body containing what looks like a token shape — passthrough
	// must leave it byte-for-byte intact.
	const fakeKey = "sk-proj-FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE"
	body := `{"query":"viewer { login }","variables":{"token":"` + fakeKey + `"}}`
	mutated, count, err := svc.HandleRequest("gh-passthrough", "api.github.com", "/graphql", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected count=0 passthrough for api.github.com, got %d", count)
	}
	if string(mutated) != body {
		t.Fatalf("api.github.com body must pass through unchanged.\nwant: %s\ngot:  %s", body, string(mutated))
	}
}
