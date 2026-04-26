package provider

import (
	"net/http"
	"strings"
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
	Transport() http.RoundTripper

	// ParseRequest decodes the raw request body into a canonical form that
	// the scanner can work with. Returns the text to scan and a function to
	// re-encode the masked text back into a valid body for this provider.
	ParseRequest(body []byte, headers http.Header) (text string, reEncode func(masked string) []byte, err error)

	// ParseResponse decodes the raw response body for vault token restoration.
	// Returns the text to restore and a re-encoder.
	ParseResponse(body []byte, headers http.Header) (text string, reEncode func(restored string) []byte, err error)

	// IsSSE returns true if the response headers indicate a streaming SSE response.
	IsSSE(headers http.Header) bool

	// IsWebSocket returns true if the request headers indicate a WebSocket upgrade.
	IsWebSocket(headers http.Header) bool
}

// WebSocketProvider is an optional extension of Provider for providers that
// require a custom transport for WebSocket connections.
type WebSocketProvider interface {
	Provider
	WSTransport() http.RoundTripper
}

// Registry maps hostnames to their Provider.
type Registry struct {
	providers []Provider
	byDomain  map[string]Provider
	fallback  Provider
}

// NewRegistry builds a Registry from the given providers. The DefaultProvider
// is automatically used as a fallback for unrecognised domains.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{
		providers: providers,
		byDomain:  make(map[string]Provider),
		fallback:  &DefaultProvider{name: "default"},
	}
	for _, p := range providers {
		for _, domain := range p.Domains() {
			r.byDomain[strings.ToLower(domain)] = p
		}
	}
	return r
}

// For returns the Provider for the given host, or the DefaultProvider if no
// provider matches. The host is compared case-insensitively with any port stripped.
func (r *Registry) For(host string) Provider {
	host = strings.ToLower(host)
	// strip port if present
	if idx := strings.IndexByte(host, ':'); idx != -1 {
		host = host[:idx]
	}
	if p, ok := r.byDomain[host]; ok {
		return p
	}
	return r.fallback
}
