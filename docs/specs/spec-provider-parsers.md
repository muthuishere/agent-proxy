# Spec: Provider Parser Architecture

**Status:** Planned  
**Priority:** Medium (enabler for Codex/Copilot fixes and future providers)

---

## Problem

All three AI providers (Anthropic, OpenAI/Codex, GitHub Copilot) go through the same generic `Service.HandleRequest` / `HandleResponse` / `HandleWebSocket` / `HandleSSEResponse` path. There is no provider-aware logic. This means:

- We cannot apply provider-specific transport settings (e.g. force HTTP/1.1 for Codex WebSocket)
- We cannot parse provider-specific body shapes (Anthropic SSE vs OpenAI SSE differ in event names and structure)
- We cannot add provider-specific headers, retries, or token formats
- Adding a new provider requires touching `service.go` and `server.go` directly

This spec introduces a `Provider` interface so each provider is a self-contained unit that can be plugged in without modifying core runtime code.

---

## Design

### Interface

```go
// internal/provider/provider.go

package provider

import (
    "io"
    "net/http"
)

// Provider encapsulates all knowledge about a single AI provider's API
// surface, transport requirements, and body handling.
type Provider interface {
    // Name returns the canonical provider name (e.g. "anthropic", "openai", "copilot").
    Name() string

    // Domains returns the set of hostnames this provider handles.
    // Compared case-insensitively, port stripped.
    Domains() []string

    // Transport returns the http.RoundTripper to use for upstream connections
    // to this provider. Returning nil falls back to the default proxy transport.
    // Providers that need HTTP/1.1 (e.g. Codex WebSocket) return a transport
    // with TLSNextProto empty.
    Transport() http.RoundTripper

    // ParseRequest decodes the raw request body into a canonical form that
    // the scanner can work with. Returns the text to scan and a function to
    // re-encode the masked text back into a valid body for this provider.
    // If the body is not a recognised format, ParseRequest returns the raw
    // body as text and an identity re-encoder.
    ParseRequest(body []byte, headers http.Header) (text string, reEncode func(masked string) []byte, err error)

    // ParseResponse decodes the raw response body for vault token restoration.
    // Returns the text to restore and a re-encoder.
    ParseResponse(body []byte, headers http.Header) (text string, reEncode func(restored string) []byte, err error)

    // IsSSE returns true if the response headers indicate a streaming SSE response.
    IsSSE(headers http.Header) bool

    // IsWebSocket returns true if the request headers indicate a WebSocket upgrade.
    IsWebSocket(headers http.Header) bool
}
```

### Registry

```go
// internal/provider/registry.go

package provider

// Registry maps hostnames to their Provider.
type Registry struct {
    providers []Provider
    byDomain  map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry

// For returns the Provider for the given host, or nil if no provider matches.
// Falls back to a DefaultProvider for unlisted intercepted domains.
func (r *Registry) For(host string) Provider
```

### Concrete providers

```
internal/provider/
├── provider.go        ← interface + registry
├── default.go         ← DefaultProvider (current generic logic, used as fallback)
├── anthropic.go       ← AnthropicProvider
├── openai.go          ← OpenAIProvider (covers api.openai.com + chatgpt.com)
└── copilot.go         ← CopilotProvider
```

---

## Provider Implementations

### `DefaultProvider` (`default.go`)

Wraps the current behaviour exactly. All other providers embed or delegate to it for unrecognised paths.

```go
type DefaultProvider struct{ name string }

func (p *DefaultProvider) ParseRequest(body []byte, headers http.Header) (string, func(string) []byte, error) {
    return string(body), func(s string) []byte { return []byte(s) }, nil
}
```

### `AnthropicProvider` (`anthropic.go`)

Domains: `api.anthropic.com`

- `ParseRequest`: Lazy JSON decode (from `spec-json-aware-masking.md`) — scan raw, if hits found and JSON, decode string leaves, re-scan decoded, re-encode
- `IsSSE`: `Content-Type: text/event-stream`
- Transport: default (HTTP/2 fine for SSE)

```go
type AnthropicProvider struct{ *DefaultProvider }

func (p *AnthropicProvider) Name() string    { return "anthropic" }
func (p *AnthropicProvider) Domains() []string { return []string{"api.anthropic.com"} }
func (p *AnthropicProvider) Transport() http.RoundTripper { return nil } // use default
```

### `OpenAIProvider` (`openai.go`)

Domains: `api.openai.com`, `chatgpt.com`

- `ParseRequest`: same lazy JSON decode as Anthropic
- `IsWebSocket`: detects Codex WebSocket path (`/backend-api/codex/responses`)
- `Transport()`: returns an HTTP/1.1-only transport when the request is a WebSocket upgrade; default transport otherwise

```go
type OpenAIProvider struct {
    *DefaultProvider
    wsTransport http.RoundTripper // HTTP/1.1 only, for WebSocket connections
}

func (p *OpenAIProvider) Transport() http.RoundTripper {
    // Returned to the proxy server; the server selects based on IsWebSocket.
    return nil // server checks IsWebSocket and calls WSTransport() separately
}

func (p *OpenAIProvider) WSTransport() http.RoundTripper {
    return p.wsTransport // HTTP/1.1-only transport
}
```

> Note: The `server.go` WebSocket transport override (from `spec-codex-websocket-fix.md`) is implemented here as a provider capability rather than a hardcoded field on the goproxywss handler.

### `CopilotProvider` (`copilot.go`)

Domains: `api.githubcopilot.com`, `api.individual.githubcopilot.com`, `copilot-proxy.githubusercontent.com`, `api.github.com`

- `ParseRequest`: JSON decode (same pattern)
- `Transport()`: initially nil (default); updated once root cause of TLS failures is known (see `spec-copilot-fix.md`)
- Placeholder for any Copilot-specific token or header handling

---

## Integration with `runtime/service.go`

```go
type Service struct {
    // ... existing fields ...
    registry *provider.Registry
}

func (s *Service) HandleRequest(sessionID, host, path, method string, headers http.Header, body []byte) ([]byte, int, error) {
    if !s.ShouldIntercept(host) {
        _ = s.logger.LogPassthrough(host, path, method)
        return body, 0, nil
    }

    p := s.registry.For(host) // never nil — falls back to DefaultProvider

    text, reEncode, err := p.ParseRequest(body, headers)
    if err != nil {
        text = string(body) // safe fallback
        reEncode = func(s string) []byte { return []byte(s) }
    }

    masked, maskedCount, patterns := s.maskText(sessionID, text)
    _ = s.logger.LogRequest(host, path, method, sanitizeHeaders(headers), masked, maskedCount, patterns)
    if maskedCount == 0 {
        return body, 0, nil
    }
    return reEncode(masked), maskedCount, nil
}
```

---

## Integration with `proxy/server.go`

```go
handler.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
    // ...
    p := service.Registry().For(hostFromRequest(req))

    // Select upstream transport per provider + request type
    if ws, ok := p.(provider.WebSocketProvider); ok && ws.IsWebSocket(req.Header) {
        ctx.RoundTripper = ws.WSTransport()
    }
    // ...
})
```

---

## File Structure

```
internal/provider/
├── provider.go          ← Provider interface, Registry, WebSocketProvider interface
├── default.go           ← DefaultProvider
├── anthropic.go         ← AnthropicProvider
├── openai.go            ← OpenAIProvider
├── copilot.go           ← CopilotProvider
├── provider_test.go     ← Registry lookup, domain matching
├── anthropic_test.go
├── openai_test.go
└── copilot_test.go
```

---

## Test Plan

### Unit tests

| Test | Description |
|---|---|
| `TestRegistryReturnsCorrectProvider` | `For("api.anthropic.com")` returns `AnthropicProvider` |
| `TestRegistryFallsBackToDefault` | Unknown domain returns `DefaultProvider` |
| `TestRegistryCaseInsensitive` | `For("API.ANTHROPIC.COM")` works |
| `TestRegistryStripPort` | `For("api.anthropic.com:443")` works |
| `TestOpenAIProviderIsWebSocket` | Detects WebSocket upgrade headers |
| `TestOpenAIProviderWSTransport` | WSTransport has empty TLSNextProto |
| `TestAnthropicProviderIsSSE` | Detects `text/event-stream` |
| `TestCopilotProviderDomains` | All four Copilot domains registered |
| `TestDefaultProviderParseRequestIdentity` | Returns body as-is |

### Integration (acceptance tests)

- All existing 45 tests pass after wiring `Registry` into `Service`
- Per-provider parse path exercises JSON decode (from JSON-aware masking spec) via `AnthropicProvider.ParseRequest`

---

## Migration Path

1. Implement `Provider` interface + `Registry` + `DefaultProvider` — no behaviour change
2. Wire `Registry` into `Service` — all providers use `DefaultProvider` — all tests pass
3. Implement `AnthropicProvider` with lazy JSON decode — replaces current raw-scan path
4. Implement `OpenAIProvider` with WSTransport — enables Codex WebSocket fix
5. Implement `CopilotProvider` — after root cause from `spec-copilot-fix.md` is known

Each step is independently shippable and testable.

---

## Acceptance Criteria

- [ ] `Provider` interface defined in `internal/provider/provider.go`
- [ ] `Registry` correctly routes all current intercepted domains
- [ ] `Service` uses `Registry` for all request/response handling
- [ ] All existing 45 tests pass with zero behaviour change
- [ ] New provider unit tests all pass
- [ ] New providers can be registered in config without touching `service.go`
