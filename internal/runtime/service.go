package runtime

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/muthuishere/agentproxy/internal/codec"
	"github.com/muthuishere/agentproxy/internal/config"
	"github.com/muthuishere/agentproxy/internal/logger"
	"github.com/muthuishere/agentproxy/internal/provider"
	"github.com/muthuishere/agentproxy/internal/scanner"
	"github.com/muthuishere/agentproxy/internal/vault"
)

var defaultDomains = map[string]struct{}{
	"api.anthropic.com":                   {},
	"api.openai.com":                      {},
	"chatgpt.com":                         {},
	"api.githubcopilot.com":               {},
	"api.individual.githubcopilot.com":    {},
	"copilot-proxy.githubusercontent.com": {},
}

// MaskingSink receives per-request masking events. The dashboard's in-memory
// store implements this. Originals are sensitive and must never be persisted
// to disk by implementations.
type MaskingSink interface {
	RecordRequest(reqID, host, path, method, originalReq, maskedReq string, replacements []vault.Replacement)
	RecordResponse(reqID string, status int, originalResp, restoredResp string, replacements []vault.Replacement)
}

type Service struct {
	secretScanner *scanner.Scanner
	piiScanner    *scanner.Scanner
	vaultManager  *vault.Manager
	logger        *logger.TrafficLogger
	sink          MaskingSink
	domains       map[string]struct{}
	registry      *provider.Registry
}

// SetSink installs a MaskingSink. nil disables sink calls. Safe to call once
// before traffic starts; not safe to swap concurrently with active requests.
func (s *Service) SetSink(sink MaskingSink) {
	s.sink = sink
}

func New(cfg config.Config) (*Service, error) {
	secretScanner, err := scanner.NewFromFile(config.ResolvePath(cfg.SourcePath, cfg.Detection.PatternFile), cfg.Detection.ScanWorkers, nil)
	if err != nil {
		return nil, err
	}

	var piiScanner *scanner.Scanner
	if cfg.PII.Enabled {
		include := make([]string, 0, len(cfg.PII.Entities))
		for _, entity := range cfg.PII.Entities {
			if mapped, ok := piiEntityMap[entity]; ok {
				include = append(include, mapped)
			}
		}
		if len(include) > 0 {
			piiScanner, err = scanner.NewFromFile(config.ResolvePath(cfg.SourcePath, cfg.PII.PatternFile), cfg.PII.ScanWorkers, include)
			if err != nil {
				return nil, err
			}
		}
	}

	log, err := logger.New(config.ResolvePath(cfg.SourcePath, cfg.Logging.LogFile), logger.Options{
		LogPassthrough:  cfg.Logging.LogPassthrough,
		LogRequestBody:  cfg.Logging.LogRequestBody,
		LogResponseBody: cfg.Logging.LogResponseBody,
		LogOriginals:    cfg.Logging.LogOriginals,
	})
	if err != nil {
		return nil, err
	}

	domains := make(map[string]struct{})
	if len(cfg.Detection.InterceptedDomains) == 0 {
		for domain := range defaultDomains {
			domains[domain] = struct{}{}
		}
	} else {
		for _, domain := range cfg.Detection.InterceptedDomains {
			domains[strings.ToLower(domain)] = struct{}{}
		}
	}

	reg := provider.NewRegistry(
		provider.NewAnthropicProvider(),
		provider.NewOpenAIProvider(),
		provider.NewCopilotProvider(),
	)

	return &Service{
		secretScanner: secretScanner,
		piiScanner:    piiScanner,
		vaultManager: vault.NewManager(vault.Options{
			PrefixChars: cfg.Masking.ShowPrefixChars,
			SuffixChars: cfg.Masking.ShowSuffixChars,
			StarLength:  cfg.Masking.StarLength,
		}),
		logger:   log,
		domains:  domains,
		registry: reg,
	}, nil
}

var piiEntityMap = map[string]string{
	"email":       "EMAIL_ADDRESS",
	"phone":       "PHONE_NUMBER",
	"ssn":         "US_SSN",
	"credit_card": "CREDIT_CARD",
	"ip_address":  "IPV4_ADDRESS",
}

func (s *Service) Close() error {
	return s.logger.Close()
}

// Logger returns the underlying TrafficLogger so callers (e.g. the UI server)
// can subscribe to live events and read metrics.
func (s *Service) Logger() *logger.TrafficLogger {
	return s.logger
}

func (s *Service) ShouldIntercept(host string) bool {
	host = strings.ToLower(host)
	if _, ok := s.domains[host]; ok {
		return true
	}
	if idx := strings.IndexByte(host, ':'); idx != -1 {
		_, ok := s.domains[host[:idx]]
		return ok
	}
	return false
}

func (s *Service) HandleRequest(sessionID string, host, path, method string, headers http.Header, body []byte, requestID string) ([]byte, int, error) {
	if !s.ShouldIntercept(host) {
		_ = s.logger.LogPassthrough(host, path, method)
		return body, 0, nil
	}
	text := codec.DecodeBody(body, headers.Get("Content-Encoding"))
	masked, maskedCount, patterns := s.maskText(sessionID, text)
	if maskedCount == 0 {
		_ = s.logger.LogRequest(host, path, method, sanitizeHeaders(headers), text, masked, maskedCount, patterns, requestID)
		s.emitRequest(requestID, host, path, method, sessionID, text, masked)
		return body, 0, nil
	}
	if isJSONContent(headers, text) && !json.Valid([]byte(masked)) {
		fallbackMaskedCount := 0
		var fallbackPatterns []string
		enriched, changed, err := codec.WalkJSONStrings([]byte(text), func(str string) string {
			remasked, n, matchedPatterns := s.maskText(sessionID, str)
			fallbackMaskedCount += n
			fallbackPatterns = append(fallbackPatterns, matchedPatterns...)
			return remasked
		})
		if err == nil {
			fallbackMasked := string(enriched)
			if changed && fallbackMaskedCount > 0 {
				_ = s.logger.LogRequest(host, path, method, sanitizeHeaders(headers), text, fallbackMasked, fallbackMaskedCount, fallbackPatterns, requestID)
				s.emitRequest(requestID, host, path, method, sessionID, text, fallbackMasked)
				return codec.EncodeBody(fallbackMasked, headers.Get("Content-Encoding")), fallbackMaskedCount, nil
			}
		}
	}
	_ = s.logger.LogRequest(host, path, method, sanitizeHeaders(headers), text, masked, maskedCount, patterns, requestID)
	s.emitRequest(requestID, host, path, method, sessionID, text, masked)
	return codec.EncodeBody(masked, headers.Get("Content-Encoding")), maskedCount, nil
}

func (s *Service) emitRequest(reqID, host, path, method, sessionID, original, masked string) {
	if s.sink == nil || reqID == "" {
		return
	}
	reps := s.vaultManager.Session(sessionID).Replacements()
	s.sink.RecordRequest(reqID, host, path, method, original, masked, reps)
}

func (s *Service) emitResponse(reqID, sessionID string, status int, original, restored string) {
	if s.sink == nil || reqID == "" {
		return
	}
	reps := s.vaultManager.Session(sessionID).Replacements()
	s.sink.RecordResponse(reqID, status, original, restored, reps)
}

func isJSONContent(headers http.Header, body string) bool {
	ct := headers.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		return true
	}
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func (s *Service) HandleResponse(sessionID string, host, path string, status int, headers http.Header, body []byte, requestID string) ([]byte, error) {
	if !s.ShouldIntercept(host) {
		return body, nil
	}
	session := s.vaultManager.Session(sessionID)
	text := codec.DecodeBody(body, headers.Get("Content-Encoding"))
	restored := session.Restore(text)
	restored = codec.RestoreEncodedBlobs(restored, session, codec.EncodedBlobOptions{Strategy: codec.EmbeddedTokens})
	_ = s.logger.LogResponse(host, path, status, flattenHeaders(headers), text, restored, 0, requestID)
	s.emitResponse(requestID, sessionID, status, text, restored)
	if restored == text {
		return body, nil
	}
	return codec.EncodeBody(restored, headers.Get("Content-Encoding")), nil
}

func (s *Service) HandleWebSocket(sessionID string, host, path, text string, fromClient bool, requestID string) (string, int) {
	if !s.ShouldIntercept(host) {
		return text, 0
	}
	if fromClient {
		masked, maskedCount, patterns := s.maskText(sessionID, text)
		_ = s.logger.LogWebSocket(host, path, "client->server", text, masked, maskedCount, patterns, requestID)
		// Mirror HandleRequest: surface frame-level masking in the dashboard
		// EventStore so /api/traffic/{id} replacements reflect WS prompts the
		// same way it reflects HTTP prompts. Without this, Codex traffic
		// looks "clean" in the dashboard even though masking happened.
		if s.sink != nil && maskedCount > 0 {
			sess := s.vaultManager.Session(sessionID)
			s.sink.RecordRequest(requestID, host, path, "WS", text, masked, sess.Replacements())
		}
		return masked, maskedCount
	}
	session := s.vaultManager.Session(sessionID)
	restored := session.Restore(text)
	restored = codec.RestoreEncodedBlobs(restored, session, codec.EncodedBlobOptions{Strategy: codec.EmbeddedTokens})
	_ = s.logger.LogWebSocket(host, path, "server->client", text, restored, 0, nil, requestID)
	if s.sink != nil && restored != text {
		s.sink.RecordResponse(requestID, 0, text, restored, session.Replacements())
	}
	return restored, 0
}

// HandleSSEResponse wraps body in a streaming SSE reader that restores vault
// tokens one event at a time. The caller must use the returned ReadCloser as
// the response body; ContentLength should be set to -1 and the Content-Length
// header removed before forwarding to the client.
func (s *Service) HandleSSEResponse(sessionID string, host, path string, status int, headers http.Header, body io.ReadCloser, requestID string) (io.ReadCloser, error) {
	if !s.ShouldIntercept(host) {
		return body, nil
	}
	_ = s.logger.LogResponse(host, path, status, flattenHeaders(headers), "[SSE stream]", "[SSE stream]", 0, requestID)
	session := s.vaultManager.Session(sessionID)
	return newSSERestoreReader(body, session), nil
}

func (s *Service) CloseSession(sessionID string) {
	s.vaultManager.CloseSession(sessionID)
}

func (s *Service) maskText(sessionID, text string) (string, int, []string) {
	session := s.vaultManager.Session(sessionID)
	maskedCount := 0
	var patterns []string
	for _, sc := range []*scanner.Scanner{s.secretScanner, s.piiScanner} {
		if sc == nil {
			continue
		}
		for _, match := range sc.ScanText(text) {
			if !strings.Contains(text, match.Value) {
				continue
			}
			text = strings.ReplaceAll(text, match.Value, session.Mask(match.Value, match.Name))
			maskedCount++
			patterns = append(patterns, match.Name)
		}
	}

	adapter := scanAdapter{secretScanner: s.secretScanner, piiScanner: s.piiScanner}
	rewritten, encodedMasked := codec.RewriteEncodedBlobs(text, adapter, session, codec.EncodedBlobOptions{
		Strategy: codec.EmbeddedTokens,
	})
	return rewritten, maskedCount + encodedMasked, patterns
}

type scanAdapter struct {
	secretScanner *scanner.Scanner
	piiScanner    *scanner.Scanner
}

func (a scanAdapter) ScanText(text string) []codec.Match {
	var matches []codec.Match
	for _, sc := range []*scanner.Scanner{a.secretScanner, a.piiScanner} {
		if sc == nil {
			continue
		}
		for _, match := range sc.ScanText(text) {
			matches = append(matches, codec.Match{Name: match.Name, Value: match.Value})
		}
	}
	return matches
}

func sanitizeHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		canonical := http.CanonicalHeaderKey(key)
		if canonical == "Authorization" || canonical == "X-Api-Key" {
			continue
		}
		if len(values) > 0 {
			out[canonical] = values[0]
		}
	}
	return out
}

func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			out[http.CanonicalHeaderKey(key)] = values[0]
		}
	}
	return out
}
