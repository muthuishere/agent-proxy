package provider

import (
	"net/http"
	"strings"
)

// DefaultProvider wraps the current generic behaviour and is used as a fallback
// for any intercepted domain that does not have a dedicated provider.
type DefaultProvider struct {
	name string
}

func (p *DefaultProvider) Name() string { return p.name }

func (p *DefaultProvider) Domains() []string { return nil }

func (p *DefaultProvider) Transport() http.RoundTripper { return nil }

func (p *DefaultProvider) ParseRequest(body []byte, headers http.Header) (string, func(string) []byte, error) {
	return string(body), func(s string) []byte { return []byte(s) }, nil
}

func (p *DefaultProvider) ParseResponse(body []byte, headers http.Header) (string, func(string) []byte, error) {
	return string(body), func(s string) []byte { return []byte(s) }, nil
}

// IsSSE returns true if the Content-Type header contains "text/event-stream".
func (p *DefaultProvider) IsSSE(headers http.Header) bool {
	return strings.Contains(headers.Get("Content-Type"), "text/event-stream")
}

// IsWebSocket returns true if the request carries a WebSocket upgrade.
func (p *DefaultProvider) IsWebSocket(headers http.Header) bool {
	return strings.EqualFold(headers.Get("Connection"), "upgrade") &&
		strings.EqualFold(headers.Get("Upgrade"), "websocket")
}
