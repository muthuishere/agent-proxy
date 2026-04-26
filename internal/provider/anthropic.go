package provider

import (
	"net/http"
)

// AnthropicProvider handles api.anthropic.com.
type AnthropicProvider struct {
	*DefaultProvider
}

// NewAnthropicProvider constructs an AnthropicProvider.
func NewAnthropicProvider() *AnthropicProvider {
	return &AnthropicProvider{
		DefaultProvider: &DefaultProvider{name: "anthropic"},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Domains() []string {
	return []string{"api.anthropic.com"}
}

// Transport returns nil — HTTP/2 is fine for Anthropic SSE.
func (p *AnthropicProvider) Transport() http.RoundTripper { return nil }
