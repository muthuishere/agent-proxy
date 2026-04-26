package patterns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileParsesDefinitions(t *testing.T) {
	path := filepath.Join("testdata", "patterns.yaml")

	defs, err := LoadFile(path, nil)
	if err != nil {
		t.Fatalf("load patterns: %v", err)
	}

	if len(defs) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(defs))
	}
	if defs[0].Matcher != MatcherRegex || defs[0].Pattern == "" {
		t.Fatalf("unexpected regex definition: %#v", defs[0])
	}
	if defs[1].Matcher != MatcherPrefix || defs[1].Value != "ghp_" {
		t.Fatalf("unexpected prefix definition: %#v", defs[1])
	}
	if defs[2].Matcher != MatcherContainsBetween || defs[2].BlockEnd == "" {
		t.Fatalf("unexpected block definition: %#v", defs[2])
	}
}

func TestLoadFileIncludeNamesIsCaseInsensitive(t *testing.T) {
	path := filepath.Join("testdata", "patterns.yaml")

	defs, err := LoadFile(path, []string{"github_token", "custom_secret"})
	if err != nil {
		t.Fatalf("load patterns: %v", err)
	}

	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}
	if defs[0].Name != "GITHUB_TOKEN" || defs[1].Name != "CUSTOM_SECRET" {
		t.Fatalf("unexpected definitions: %#v", defs)
	}
}

func TestLoadFileRejectsInvalidDefinitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patterns.yaml")
	content := strings.TrimSpace(`
patterns:
  - name: BROKEN
    type: contains_between
    block_start: BEGIN
    category: cert
    confidence: certain
`)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp patterns: %v", err)
	}

	_, err := LoadFile(path, nil)
	if err == nil || !strings.Contains(err.Error(), "contains_between requires block_end") {
		t.Fatalf("expected actionable validation error, got %v", err)
	}
}
