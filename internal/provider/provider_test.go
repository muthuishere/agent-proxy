package provider_test

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/muthuishere/agentproxy/internal/provider"
)

func buildRegistry() *provider.Registry {
	return provider.NewRegistry(
		provider.NewAnthropicProvider(),
		provider.NewOpenAIProvider(),
		provider.NewCopilotProvider(),
	)
}

func TestRegistryReturnsCorrectProvider(t *testing.T) {
	r := buildRegistry()

	cases := []struct {
		host     string
		wantName string
	}{
		{"api.anthropic.com", "anthropic"},
		{"api.openai.com", "openai"},
		{"chatgpt.com", "openai"},
		{"api.githubcopilot.com", "copilot"},
		{"api.individual.githubcopilot.com", "copilot"},
		{"copilot-proxy.githubusercontent.com", "copilot"},
		// api.github.com intentionally falls through to default — see
		// CopilotProvider.Domains() and config/agentproxy.yaml.
		{"api.github.com", "default"},
	}

	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			p := r.For(tc.host)
			if p == nil {
				t.Fatalf("For(%q) returned nil", tc.host)
			}
			if p.Name() != tc.wantName {
				t.Errorf("For(%q).Name() = %q, want %q", tc.host, p.Name(), tc.wantName)
			}
		})
	}
}

func TestRegistryFallsBackToDefault(t *testing.T) {
	r := buildRegistry()
	p := r.For("some.unknown.provider.com")
	if p == nil {
		t.Fatal("For unknown domain returned nil, want DefaultProvider")
	}
	if p.Name() != "default" {
		t.Errorf("fallback provider name = %q, want \"default\"", p.Name())
	}
}

func TestRegistryCaseInsensitive(t *testing.T) {
	r := buildRegistry()
	p := r.For("API.ANTHROPIC.COM")
	if p == nil || p.Name() != "anthropic" {
		t.Errorf("For(\"API.ANTHROPIC.COM\") = %v, want AnthropicProvider", p)
	}
}

func TestRegistryStripsPort(t *testing.T) {
	r := buildRegistry()
	p := r.For("api.anthropic.com:443")
	if p == nil || p.Name() != "anthropic" {
		t.Errorf("For(\"api.anthropic.com:443\") = %v, want AnthropicProvider", p)
	}
}

func TestOpenAIIsWebSocketProvider(t *testing.T) {
	p := provider.NewOpenAIProvider()
	if _, ok := any(p).(provider.WebSocketProvider); !ok {
		t.Error("OpenAIProvider does not implement WebSocketProvider")
	}
}

func TestOpenAIWSTransportIsHTTP1Only(t *testing.T) {
	p := provider.NewOpenAIProvider()
	ws, ok := any(p).(provider.WebSocketProvider)
	if !ok {
		t.Fatal("OpenAIProvider does not implement WebSocketProvider")
	}
	tr := ws.WSTransport()
	if tr == nil {
		t.Fatal("WSTransport() returned nil")
	}
	httpTr, ok := tr.(*http.Transport)
	if !ok {
		t.Fatalf("WSTransport() type = %T, want *http.Transport", tr)
	}
	if httpTr.TLSNextProto == nil {
		t.Error("TLSNextProto is nil, want empty map (HTTP/1.1-only)")
	}
	if len(httpTr.TLSNextProto) != 0 {
		t.Errorf("TLSNextProto len = %d, want 0 (no HTTP/2 upgrade)", len(httpTr.TLSNextProto))
	}
	// Verify the map is not capable of upgrading to h2
	if _, h2present := httpTr.TLSNextProto["h2"]; h2present {
		t.Error("TLSNextProto contains h2, transport is NOT HTTP/1.1 only")
	}
	// Additional type assertion confirming the map value type
	var _ map[string]func(string, *tls.Conn) http.RoundTripper = httpTr.TLSNextProto
}
