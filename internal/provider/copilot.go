package provider

import (
	"net/http"
)

// CopilotProvider handles GitHub Copilot API domains.
type CopilotProvider struct {
	*DefaultProvider
}

// NewCopilotProvider constructs a CopilotProvider.
func NewCopilotProvider() *CopilotProvider {
	return &CopilotProvider{
		DefaultProvider: &DefaultProvider{name: "copilot"},
	}
}

func (p *CopilotProvider) Name() string { return "copilot" }

func (p *CopilotProvider) Domains() []string {
	// api.github.com is intentionally excluded — it's the bare GitHub REST
	// API used by regular `gh` commands and should pass through. Copilot's
	// chat/completion traffic targets the *.githubcopilot.com hosts.
	return []string{
		"api.githubcopilot.com",
		"api.individual.githubcopilot.com",
		"copilot-proxy.githubusercontent.com",
	}
}

// Transport returns nil — default transport for now.
// Updated once the root cause of TLS failures is known (see spec-copilot-fix.md).
func (p *CopilotProvider) Transport() http.RoundTripper { return nil }
