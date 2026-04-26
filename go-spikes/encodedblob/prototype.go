package encodedblob

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type Strategy string

const (
	OptionB Strategy = "option_b"
	OptionC Strategy = "option_c"
)

type Prototype struct {
	secrets      []string
	maxDepth     int
	tokenCounter int
	vault        map[string]string
}

func NewPrototype(secrets []string, maxDepth int) *Prototype {
	return &Prototype{
		secrets:  secrets,
		maxDepth: maxDepth,
		vault:    make(map[string]string),
	}
}

func (p *Prototype) Mask(input string, strategy Strategy) string {
	return p.maskText(input, strategy, p.maxDepth)
}

func (p *Prototype) Restore(input string, strategy Strategy) string {
	switch strategy {
	case OptionC:
		return replaceAll(input, p.vault)
	case OptionB:
		return p.restoreText(input, p.maxDepth)
	default:
		return input
	}
}

func (p *Prototype) maskText(input string, strategy Strategy, depth int) string {
	if depth <= 0 {
		return p.maskRawSecrets(input)
	}

	masked := p.maskRawSecrets(input)
	masked = p.maskCandidates(masked, strategy, depth)
	return masked
}

func (p *Prototype) maskRawSecrets(input string) string {
	masked := input
	for _, secret := range p.secrets {
		if strings.Contains(masked, secret) {
			token := p.tokenFor(secret)
			masked = strings.ReplaceAll(masked, secret, token)
		}
	}
	return masked
}

func (p *Prototype) maskCandidates(input string, strategy Strategy, depth int) string {
	masked := input
	for _, candidate := range findCandidates(masked) {
		replaced, ok := p.maskCandidate(candidate, strategy, depth)
		if !ok || candidate == replaced {
			continue
		}
		masked = strings.Replace(masked, candidate, replaced, 1)
	}
	return masked
}

func (p *Prototype) maskCandidate(candidate string, strategy Strategy, depth int) (string, bool) {
	decoded, encoding, ok := decodeCandidate(candidate)
	if !ok {
		return candidate, false
	}

	switch strategy {
	case OptionB:
		transformed := p.maskText(decoded, strategy, depth-1)
		if transformed == decoded {
			return candidate, false
		}
		return encodeCandidate(transformed, encoding), true
	case OptionC:
		transformed := p.maskText(decoded, OptionB, depth-1)
		if transformed == decoded {
			return candidate, false
		}
		token := p.tokenFor(candidate)
		return token, true
	default:
		return candidate, false
	}
}

func (p *Prototype) restoreText(input string, depth int) string {
	restored := replaceAll(input, p.vault)
	if depth <= 0 {
		return restored
	}
	for _, candidate := range findCandidates(restored) {
		decoded, encoding, ok := decodeCandidate(candidate)
		if !ok {
			continue
		}
		decodedRestored := p.restoreText(decoded, depth-1)
		if decodedRestored == decoded {
			continue
		}
		restored = strings.Replace(restored, candidate, encodeCandidate(decodedRestored, encoding), 1)
	}
	return restored
}

func (p *Prototype) tokenFor(original string) string {
	for token, value := range p.vault {
		if value == original {
			return token
		}
	}
	p.tokenCounter++
	token := fmt.Sprintf("[MASKED:%03d]", p.tokenCounter)
	p.vault[token] = original
	return token
}

var (
	base64Pattern = regexp.MustCompile(`[A-Za-z0-9+/=]{12,}`)
	hexPattern    = regexp.MustCompile(`[0-9a-fA-F]{12,}`)
	urlPattern    = regexp.MustCompile(`[A-Za-z0-9%._~\-]{12,}%[0-9A-Fa-f]{2}[A-Za-z0-9%._~\-]*`)
)

func findCandidates(input string) []string {
	seen := make(map[string]bool)
	var candidates []string
	for _, pattern := range []*regexp.Regexp{urlPattern, base64Pattern, hexPattern} {
		for _, match := range pattern.FindAllString(input, -1) {
			if seen[match] {
				continue
			}
			seen[match] = true
			candidates = append(candidates, match)
		}
	}
	return candidates
}

func decodeCandidate(candidate string) (string, string, bool) {
	if decoded, err := base64.StdEncoding.DecodeString(candidate); err == nil && isPrintable(decoded) {
		return string(decoded), "base64", true
	}
	if decoded, err := hex.DecodeString(candidate); err == nil && isPrintable(decoded) {
		return string(decoded), "hex", true
	}
	if decoded, err := url.QueryUnescape(candidate); err == nil && decoded != candidate && isPrintable([]byte(decoded)) {
		return decoded, "url", true
	}
	return "", "", false
}

func encodeCandidate(value, encoding string) string {
	switch encoding {
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(value))
	case "hex":
		return hex.EncodeToString([]byte(value))
	case "url":
		return url.QueryEscape(value)
	default:
		return value
	}
}

func isPrintable(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data {
		if b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}

func replaceAll(input string, values map[string]string) string {
	output := input
	for token, original := range values {
		output = strings.ReplaceAll(output, token, original)
	}
	return output
}
