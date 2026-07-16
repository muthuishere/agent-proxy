package scanner

import (
	"path/filepath"
	"testing"

	"github.com/muthuishere/agent-proxy/internal/patterns"
)

func TestNewFromFileLoadsAndFiltersPatterns(t *testing.T) {
	path := filepath.Join("..", "patterns", "testdata", "patterns.yaml")

	s, err := NewFromFile(path, 4, []string{"custom_secret"})
	if err != nil {
		t.Fatalf("new from file: %v", err)
	}

	if s.PatternCount() != 1 {
		t.Fatalf("expected 1 compiled pattern, got %d", s.PatternCount())
	}
	if s.Workers() != 4 {
		t.Fatalf("expected worker count 4, got %d", s.Workers())
	}
}

func TestNewSkipsInvalidDefinitionsAndNormalizesWorkers(t *testing.T) {
	s := New([]patterns.Definition{
		{
			Name:       "VALID",
			Matcher:    patterns.MatcherContains,
			Value:      "secret",
			Category:   "password",
			Confidence: "high",
		},
		{
			Name:       "BROKEN",
			Matcher:    patterns.MatcherRegex,
			Pattern:    `(`,
			Category:   "password",
			Confidence: "high",
		},
	}, 0)

	if s.PatternCount() != 1 {
		t.Fatalf("expected 1 valid pattern, got %d", s.PatternCount())
	}
	if s.Workers() != 1 {
		t.Fatalf("expected worker count normalized to 1, got %d", s.Workers())
	}
}

func TestScanTextReturnsMetadataAndOffsets(t *testing.T) {
	s := New([]patterns.Definition{
		{
			Name:       "OPENAI_API_KEY",
			Matcher:    patterns.MatcherRegex,
			Pattern:    `sk-(proj-)?[A-Za-z0-9\-_]{20,}`,
			Category:   "apikey",
			Confidence: "certain",
		},
		{
			Name:       "GITHUB_TOKEN",
			Matcher:    patterns.MatcherPrefix,
			Value:      `ghp_`,
			Category:   "apikey",
			Confidence: "certain",
		},
	}, 1)

	text := "OPENAI=sk-proj-abcdefghijklmnopqrstuvwxyz1234\nGITHUB=ghp_abcdefghijklmnopqrstuvwxyz1234567890"
	matches := s.ScanText(text)

	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].Name != "OPENAI_API_KEY" || matches[0].Category != "apikey" || matches[0].Confidence != "certain" {
		t.Fatalf("unexpected first match metadata: %#v", matches[0])
	}
	if matches[0].Value != "sk-proj-abcdefghijklmnopqrstuvwxyz1234" {
		t.Fatalf("unexpected first match value: %q", matches[0].Value)
	}
	if matches[1].Value != "ghp_abcdefghijklmnopqrstuvwxyz1234567890" {
		t.Fatalf("unexpected prefix match value: %q", matches[1].Value)
	}
}

func TestScanTextContainsBetweenMasksFullBlock(t *testing.T) {
	s := New([]patterns.Definition{
		{
			Name:       "OPENSSH_PRIVATE_KEY",
			Matcher:    patterns.MatcherContainsBetween,
			BlockStart: "-----BEGIN OPENSSH PRIVATE KEY-----",
			BlockEnd:   "-----END OPENSSH PRIVATE KEY-----",
			Category:   "key",
			Confidence: "certain",
		},
	}, 1)

	text := "before\n-----BEGIN OPENSSH PRIVATE KEY-----\nabc123\n-----END OPENSSH PRIVATE KEY-----\nafter"
	matches := s.ScanText(text)
	if len(matches) != 1 {
		t.Fatalf("expected 1 block match, got %d", len(matches))
	}
	if matches[0].Value != "-----BEGIN OPENSSH PRIVATE KEY-----\nabc123\n-----END OPENSSH PRIVATE KEY-----" {
		t.Fatalf("unexpected block match: %q", matches[0].Value)
	}
}

func TestScanTextContainsBetweenIgnoresPartialBlock(t *testing.T) {
	s := New([]patterns.Definition{
		{
			Name:       "OPENSSH_PRIVATE_KEY",
			Matcher:    patterns.MatcherContainsBetween,
			BlockStart: "-----BEGIN OPENSSH PRIVATE KEY-----",
			BlockEnd:   "-----END OPENSSH PRIVATE KEY-----",
			Category:   "key",
			Confidence: "certain",
		},
	}, 1)

	text := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc123"
	matches := s.ScanText(text)
	if len(matches) != 0 {
		t.Fatalf("expected no match for partial block, got %#v", matches)
	}
}

func TestThreadedScanMatchesSingleWorkerScan(t *testing.T) {
	defs := []patterns.Definition{
		{
			Name:       "OPENAI_API_KEY",
			Matcher:    patterns.MatcherRegex,
			Pattern:    `sk-(proj-)?[A-Za-z0-9\-_]{20,}`,
			Category:   "apikey",
			Confidence: "certain",
		},
		{
			Name:       "GITHUB_TOKEN",
			Matcher:    patterns.MatcherPrefix,
			Value:      `ghp_`,
			Category:   "apikey",
			Confidence: "certain",
		},
		{
			Name:       "PRIVATE_KEY",
			Matcher:    patterns.MatcherContainsBetween,
			BlockStart: "-----BEGIN PRIVATE KEY-----",
			BlockEnd:   "-----END PRIVATE KEY-----",
			Category:   "cert",
			Confidence: "high",
		},
	}

	text := "OPENAI=sk-proj-abcdefghijklmnopqrstuvwxyz1234\nGITHUB=ghp_abcdefghijklmnopqrstuvwxyz1234567890\n-----BEGIN PRIVATE KEY-----\nABCDEF1234567890\n-----END PRIVATE KEY-----"

	single := New(defs, 1).ScanText(text)
	threaded := New(defs, 8).ScanText(text)

	if len(single) != len(threaded) {
		t.Fatalf("match count mismatch: single=%d threaded=%d", len(single), len(threaded))
	}
	for i := range single {
		if single[i] != threaded[i] {
			t.Fatalf("mismatch at %d: single=%#v threaded=%#v", i, single[i], threaded[i])
		}
	}
}
