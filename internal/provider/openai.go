package provider

import (
	"crypto/tls"
	"net/http"
)

// OpenAIProvider handles api.openai.com and chatgpt.com.
// It implements WebSocketProvider to expose an HTTP/1.1-only transport for
// WebSocket connections (required for the Codex endpoint).
type OpenAIProvider struct {
	*DefaultProvider
	wsTransport http.RoundTripper
}

// NewOpenAIProvider constructs an OpenAIProvider with an HTTP/1.1-only WebSocket
// transport pre-configured.
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{
		DefaultProvider: &DefaultProvider{name: "openai"},
		wsTransport: &http.Transport{
			// Disable HTTP/2 by providing an empty TLSNextProto map.
			TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
		},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Domains() []string {
	return []string{"api.openai.com", "chatgpt.com"}
}

// Transport returns nil; the proxy server calls WSTransport when IsWebSocket
// is true.
func (p *OpenAIProvider) Transport() http.RoundTripper { return nil }

// WSTransport returns the HTTP/1.1-only transport used for WebSocket upgrades.
func (p *OpenAIProvider) WSTransport() http.RoundTripper { return p.wsTransport }
