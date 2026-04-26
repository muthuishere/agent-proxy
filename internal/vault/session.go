package vault

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"
)

type Options struct {
	PrefixChars int
	SuffixChars int
	StarLength  int
}

type Manager struct {
	mu       sync.RWMutex
	options  Options
	sessions map[string]*Session
}

type Session struct {
	mu       sync.RWMutex
	options  Options
	store    map[string]string // surrogate -> original
	patterns map[string]string // surrogate -> pattern name
}

// Replacement is a single surrogate↔original mapping captured during masking.
// Callers (e.g. the dashboard) are responsible for truncating Original before
// rendering — the vault holds full values to make Restore exact.
type Replacement struct {
	Pattern   string `json:"pattern"`
	Surrogate string `json:"surrogate"`
	Original  string `json:"original"`
}

var assignmentRE = regexp.MustCompile(`(?i)^(\s*[A-Z0-9_]*(?:password|passwd|secret|api_key|apikey|access_token|auth_token|private_key|client_secret)[A-Z0-9_]*\s*[=:]\s*)(\\?["']?)(.*?)(\\?["']?)$`)

type surrogatePolicy struct {
	prefixes           []string
	preserveURIScheme  bool
	preservePEM        bool
	preserveAssignment bool
}

var defaultSurrogatePolicy = surrogatePolicy{
	preserveURIScheme:  true,
	preserveAssignment: true,
}

var surrogatePolicies = map[string]surrogatePolicy{
	"ANTHROPIC_API_KEY": {prefixes: []string{"sk-ant-api03-"}},
	"OPENAI_API_KEY":    {prefixes: []string{"sk-"}},
	"GOOGLE_API_KEY":    {prefixes: []string{"AIza"}},
	"HUGGINGFACE_TOKEN": {prefixes: []string{"hf_"}},
	"GITHUB_TOKEN": {
		prefixes: []string{"github_pat_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_"},
	},
	"GITLAB_TOKEN":      {prefixes: []string{"glpat-"}},
	"NPM_TOKEN":         {prefixes: []string{"npm_"}},
	"STRIPE_SECRET_KEY": {prefixes: []string{"sk_live_", "sk_test_"}},
	"SLACK_BOT_TOKEN":   {prefixes: []string{"xoxb-", "xoxa-", "xoxp-", "xoxr-", "xoxs-"}},
	"AWS_ACCESS_KEY":    {prefixes: []string{"AKIA", "ASIA"}},

	"GENERIC_CONNECTION_STRING": {preserveURIScheme: true},

	"OPENSSH_PRIVATE_KEY": {preservePEM: true},
	"RSA_PRIVATE_KEY":     {preservePEM: true},
	"EC_PRIVATE_KEY":      {preservePEM: true},
	"PKCS8_PRIVATE_KEY":   {preservePEM: true},
}

func NewManager(options Options) *Manager {
	options = normalizeOptions(options)
	return &Manager{
		options:  options,
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) Session(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[id]; ok {
		return session
	}

	session := &Session{
		options:  m.options,
		store:    make(map[string]string),
		patterns: make(map[string]string),
	}
	m.sessions[id] = session
	return session
}

func (m *Manager) CloseSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

func (m *Manager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (s *Session) Mask(value, name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.store[value]; exists {
		return value
	}

	placeholder := makePlaceholder(value, name, s.options)
	if _, exists := s.store[placeholder]; !exists {
		s.store[placeholder] = value
		s.patterns[placeholder] = name
	}
	return placeholder
}

// Surrogates returns a snapshot of every surrogate string in this session,
// sorted by length descending. Used by the streaming restore path to detect
// when a buffered tail might be the start of a surrogate that continues into
// a not-yet-arrived chunk.
func (s *Session) Surrogates() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.store))
	for surrogate := range s.store {
		out = append(out, surrogate)
	}
	// Sort by length descending so callers can do longest-first prefix checks.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len(out[j]) > len(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Replacements returns a snapshot of every surrogate↔original mapping recorded
// in this session. Order is unspecified.
func (s *Session) Replacements() []Replacement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Replacement, 0, len(s.store))
	for surrogate, original := range s.store {
		out = append(out, Replacement{
			Pattern:   s.patterns[surrogate],
			Surrogate: surrogate,
			Original:  original,
		})
	}
	return out
}

func (s *Session) Restore(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	restored := text
	for placeholder, original := range s.store {
		restored = strings.ReplaceAll(restored, placeholder, original)
	}
	return restored
}

func (s *Session) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.store)
}

func normalizeOptions(options Options) Options {
	if options.PrefixChars <= 0 {
		options.PrefixChars = 4
	}
	if options.SuffixChars < 0 {
		options.SuffixChars = 0
	}
	if options.StarLength <= 0 {
		options.StarLength = 24
	}
	return options
}

func makePlaceholder(value, name string, options Options) string {
	policy := policyFor(name)
	if strings.Contains(name, "ASSIGNMENT") && policy.preserveAssignment {
		return replaceAssignmentValue(value, name)
	}
	if policy.preservePEM {
		return randomizePrivateKey(value, name)
	}
	return randomLikeWithPolicy(value, name, policy)
}

func replaceAssignmentValue(value, name string) string {
	matches := assignmentRE.FindStringSubmatch(value)
	if len(matches) == 5 {
		return matches[1] + matches[2] + randomLikeWithPolicy(matches[3], name, policyFor(name)) + matches[4]
	}
	return randomLikeWithPolicy(value, name, policyFor(name))
}

func policyFor(name string) surrogatePolicy {
	if policy, ok := surrogatePolicies[name]; ok {
		return policy
	}
	return defaultSurrogatePolicy
}

func randomLikeWithPolicy(value, name string, policy surrogatePolicy) string {
	if policy.preserveURIScheme {
		if idx := strings.Index(value, "://"); idx > 0 {
			return value[:idx+3] + randomLike(value[idx+3:], name)
		}
	}

	for _, prefix := range policy.prefixes {
		if strings.HasPrefix(value, prefix) {
			return prefix + randomLike(value[len(prefix):], name)
		}
	}

	return randomLike(value, name)
}

func randomizePrivateKey(value, name string) string {
	lines := strings.Split(value, "\n")
	if len(lines) < 3 {
		return randomLike(value, name)
	}
	out := make([]string, len(lines))
	copy(out, lines)
	for i := 1; i < len(lines)-1; i++ {
		out[i] = randomLike(lines[i], name)
	}
	return strings.Join(out, "\n")
}

func randomLike(value, name string) string {
	stream := newByteStream(value + "\x00" + name)
	var out strings.Builder
	out.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteByte(byte('a' + stream.Next()%26))
		case r >= 'A' && r <= 'Z':
			out.WriteByte(byte('A' + stream.Next()%26))
		case r >= '0' && r <= '9':
			out.WriteByte(byte('0' + stream.Next()%10))
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

type byteStream struct {
	seed    string
	block   []byte
	counter byte
	index   int
}

func newByteStream(seed string) *byteStream {
	return &byteStream{seed: seed}
}

func (s *byteStream) Next() byte {
	if s.index >= len(s.block) {
		sum := sha256.Sum256([]byte(s.seed + string([]byte{s.counter})))
		s.block = sum[:]
		s.counter++
		s.index = 0
	}
	b := s.block[s.index]
	s.index++
	return b
}
