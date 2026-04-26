package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadParsesProjectConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config", "agentproxy.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Proxy.Port != 7717 {
		t.Fatalf("unexpected port: %d", cfg.Proxy.Port)
	}
	if got := len(cfg.Detection.InterceptedDomains); got == 0 {
		t.Fatalf("expected intercepted domains, got %d", got)
	}
	if !contains(cfg.PII.Entities, "email") {
		t.Fatalf("expected pii entities to include email")
	}
}

func TestValidateStartupPassesWithRealProjectPaths(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config", "agentproxy.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if err := ValidateStartup(cfg); err != nil {
		t.Fatalf("validate startup: %v", err)
	}
}

func TestValidateStartupReportsMissingPatternFile(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "certs"))
	mustMkdirAll(t, filepath.Join(root, "logs"))

	configPath := filepath.Join(root, "agentproxy.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(`
proxy:
  port: 7717
  host: 127.0.0.1
tls:
  cert_dir: ./certs
detection:
  pattern_file: ./missing-patterns.yaml
  scan_workers: 2
logging:
  log_file: ./logs/traffic.jsonl
`)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	err = ValidateStartup(cfg)
	if err == nil || !strings.Contains(err.Error(), "required path missing") {
		t.Fatalf("expected missing path error, got %v", err)
	}
}

func TestResolvePathFallsBackToConfigDirectory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "nested", "agentproxy.yaml")
	mustMkdirAll(t, filepath.Dir(configPath))
	target := filepath.Join(root, "nested", "certs")
	mustMkdirAll(t, target)

	resolved := ResolvePath(configPath, "./certs")
	if resolved != target {
		t.Fatalf("expected %s, got %s", target, resolved)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
