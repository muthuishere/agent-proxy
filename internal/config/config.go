package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	SourcePath string

	Proxy struct {
		Port int
		Host string
	}
	TLS struct {
		CertDir string
	}
	Detection struct {
		PatternFile        string
		ScanWorkers        int
		InterceptedDomains []string
	}
	PII struct {
		Enabled     bool
		PatternFile string
		ScanWorkers int
		Entities    []string
	}
	Masking struct {
		ShowPrefixChars int
		ShowSuffixChars int
		StarLength      int
	}
	Logging struct {
		LogFile         string
		LogRequestBody  bool
		LogResponseBody bool
		LogOriginals    bool
		LogPassthrough  bool
	}
	UpstreamProxy struct {
		AutoDetect bool
		URL        string
		Username   string
		Password   string
		PACFile    string
	}
	Org struct {
		Name string
	}
}

func Default() Config {
	var cfg Config
	cfg.Proxy.Port = 7717
	cfg.Proxy.Host = "127.0.0.1"
	cfg.TLS.CertDir = "./certs"
	cfg.Detection.PatternFile = "./config/patterns.yaml"
	cfg.Detection.ScanWorkers = 1
	cfg.PII.PatternFile = "./config/pii_patterns.yaml"
	cfg.PII.ScanWorkers = 1
	cfg.Masking.ShowPrefixChars = 4
	cfg.Masking.ShowSuffixChars = 4
	cfg.Masking.StarLength = 24
	cfg.Logging.LogFile = "./logs/traffic.jsonl"
	cfg.Logging.LogRequestBody = true
	cfg.Logging.LogResponseBody = true
	cfg.UpstreamProxy.AutoDetect = true
	return cfg
}

func ResolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}

	candidates := []string{
		"./agentproxy.yaml",
		"./config/agentproxy.yaml",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".agentproxy", "agentproxy.yaml"))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Abs(candidate)
		}
	}
	return "", errors.New("no config file found; tried ./agentproxy.yaml, ./config/agentproxy.yaml, and ~/.agentproxy/agentproxy.yaml")
}

func Load(path string) (Config, error) {
	absPath, err := ResolveConfigPath(path)
	if err != nil {
		return Config{}, err
	}

	file, err := os.Open(absPath)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	cfg := Default()
	cfg.SourcePath = absPath

	scanner := bufio.NewScanner(file)
	section := ""
	listKey := ""
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if idx := strings.Index(trimmed, "#"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
			if trimmed == "" {
				continue
			}
		}

		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			listKey = ""
			continue
		}

		if strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if err := applyListValue(&cfg, section, listKey, value); err != nil {
				return Config{}, fmt.Errorf("%s: %w", absPath, err)
			}
			continue
		}

		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return Config{}, fmt.Errorf("%s: malformed config line %q", absPath, trimmed)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if value == "" {
			listKey = key
			continue
		}
		listKey = ""
		if err := applyScalar(&cfg, section, key, unquote(value)); err != nil {
			return Config{}, fmt.Errorf("%s: %w", absPath, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func ValidateStartup(cfg Config) error {
	var problems []string

	if cfg.Proxy.Host == "" {
		problems = append(problems, "proxy.host must not be empty")
	}
	if cfg.Proxy.Port < 1 || cfg.Proxy.Port > 65535 {
		problems = append(problems, "proxy.port must be between 1 and 65535")
	}
	if cfg.Detection.ScanWorkers < 1 {
		problems = append(problems, "detection.scan_workers must be >= 1")
	}
	if cfg.PII.ScanWorkers < 1 {
		problems = append(problems, "pii.scan_workers must be >= 1")
	}
	if err := requirePath(cfg.SourcePath, cfg.TLS.CertDir, false); err != nil {
		problems = append(problems, err.Error())
	}
	if err := requirePath(cfg.SourcePath, cfg.Detection.PatternFile, true); err != nil {
		problems = append(problems, err.Error())
	}
	if cfg.PII.Enabled {
		if err := requirePath(cfg.SourcePath, cfg.PII.PatternFile, true); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if err := requireParentDir(cfg.SourcePath, cfg.Logging.LogFile); err != nil {
		problems = append(problems, err.Error())
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func ResolvePath(configPath, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return value
	}
	configDir := filepath.Dir(configPath)

	candidates := []string{
		value,
		filepath.Join(configDir, strings.TrimPrefix(value, "./")),
		filepath.Join(filepath.Dir(configDir), strings.TrimPrefix(value, "./")),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			if abs, absErr := filepath.Abs(candidate); absErr == nil {
				return abs
			}
			return candidate
		}
	}
	if abs, err := filepath.Abs(candidates[0]); err == nil {
		return abs
	}
	return candidates[0]
}

func requirePath(configPath, value string, fileExpected bool) error {
	resolved := ResolvePath(configPath, value)
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("required path missing: %s", resolved)
	}
	if fileExpected && info.IsDir() {
		return fmt.Errorf("expected file but found directory: %s", resolved)
	}
	if !fileExpected && !info.IsDir() {
		return fmt.Errorf("expected directory but found file: %s", resolved)
	}
	return nil
}

func requireParentDir(configPath, value string) error {
	resolved := ResolvePath(configPath, value)
	parent := filepath.Dir(resolved)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("log parent directory missing: %s", parent)
	}
	if !info.IsDir() {
		return fmt.Errorf("log parent path is not a directory: %s", parent)
	}
	return nil
}

func applyScalar(cfg *Config, section, key, value string) error {
	switch section + "." + key {
	case "proxy.port":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %s.%s", section, key)
		}
		cfg.Proxy.Port = v
	case "proxy.host":
		cfg.Proxy.Host = value
	case "tls.cert_dir":
		cfg.TLS.CertDir = value
	case "detection.pattern_file":
		cfg.Detection.PatternFile = value
	case "detection.scan_workers":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %s.%s", section, key)
		}
		cfg.Detection.ScanWorkers = v
	case "pii.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s.%s", section, key)
		}
		cfg.PII.Enabled = v
	case "pii.pattern_file":
		cfg.PII.PatternFile = value
	case "pii.scan_workers":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %s.%s", section, key)
		}
		cfg.PII.ScanWorkers = v
	case "masking.show_prefix_chars":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %s.%s", section, key)
		}
		cfg.Masking.ShowPrefixChars = v
	case "masking.show_suffix_chars":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %s.%s", section, key)
		}
		cfg.Masking.ShowSuffixChars = v
	case "masking.star_length":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid integer for %s.%s", section, key)
		}
		cfg.Masking.StarLength = v
	case "logging.log_file":
		cfg.Logging.LogFile = value
	case "logging.log_request_body":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s.%s", section, key)
		}
		cfg.Logging.LogRequestBody = v
	case "logging.log_response_body":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s.%s", section, key)
		}
		cfg.Logging.LogResponseBody = v
	case "logging.log_originals":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s.%s", section, key)
		}
		cfg.Logging.LogOriginals = v
	case "logging.log_passthrough":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s.%s", section, key)
		}
		cfg.Logging.LogPassthrough = v
	case "upstream_proxy.auto_detect":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid boolean for %s.%s", section, key)
		}
		cfg.UpstreamProxy.AutoDetect = v
	case "upstream_proxy.url":
		cfg.UpstreamProxy.URL = value
	case "upstream_proxy.username":
		cfg.UpstreamProxy.Username = value
	case "upstream_proxy.password":
		cfg.UpstreamProxy.Password = value
	case "upstream_proxy.pac_file":
		cfg.UpstreamProxy.PACFile = value
	case "org.name":
		cfg.Org.Name = value
	default:
	}
	return nil
}

func applyListValue(cfg *Config, section, listKey, value string) error {
	switch section + "." + listKey {
	case "detection.intercepted_domains":
		cfg.Detection.InterceptedDomains = append(cfg.Detection.InterceptedDomains, value)
	case "pii.entities":
		cfg.PII.Entities = append(cfg.PII.Entities, value)
	default:
	}
	return nil
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}
