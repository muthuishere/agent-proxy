package patterns

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const (
	MatcherRegex           = "regex"
	MatcherPrefix          = "prefix"
	MatcherContains        = "contains"
	MatcherContainsBetween = "contains_between"
)

type Definition struct {
	Name       string
	Matcher    string
	Pattern    string
	Value      string
	BlockStart string
	BlockEnd   string
	Category   string
	Confidence string
}

func LoadFile(path string, includeNames []string) ([]Definition, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	defs, err := Load(file, includeNames)
	if err != nil {
		return nil, err
	}
	if err := ValidateDefinitions(defs); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return defs, nil
}

func Load(r io.Reader, includeNames []string) ([]Definition, error) {
	allowed := make(map[string]struct{}, len(includeNames))
	for _, name := range includeNames {
		allowed[strings.ToUpper(strings.TrimSpace(name))] = struct{}{}
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var defs []Definition
	var current *Definition
	inPatterns := false

	flush := func() {
		if current == nil {
			return
		}
		if current.Name == "" {
			current = nil
			return
		}
		if len(allowed) > 0 {
			if _, ok := allowed[strings.ToUpper(current.Name)]; !ok {
				current = nil
				return
			}
		}
		defs = append(defs, *current)
		current = nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !inPatterns {
			if trimmed == "patterns:" {
				inPatterns = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			current = &Definition{}
			key, value, ok := parseKeyValue(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			if ok {
				assign(current, key, value)
			}
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := parseKeyValue(trimmed)
		if ok {
			assign(current, key, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return defs, nil
}

func ValidateDefinitions(defs []Definition) error {
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		if err := def.Validate(); err != nil {
			return err
		}
		name := strings.ToUpper(def.Name)
		if _, ok := seen[name]; ok {
			return fmt.Errorf("pattern %s: duplicate pattern name", def.Name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func parseKeyValue(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", false
	}
	return key, unquote(value), true
}

func assign(def *Definition, key, value string) {
	switch key {
	case "name":
		def.Name = value
	case "type":
		if isMatcherType(value) {
			def.Matcher = value
		} else {
			def.Category = value
		}
	case "pattern":
		def.Pattern = value
	case "value":
		def.Value = value
	case "block_start":
		def.BlockStart = value
	case "block_end":
		def.BlockEnd = value
	case "category":
		def.Category = value
	case "confidence":
		def.Confidence = value
	case "regex":
		// Backwards-compatible alias during migration.
		def.Pattern = value
		if def.Matcher == "" {
			def.Matcher = MatcherRegex
		}
	}
}

func isMatcherType(value string) bool {
	switch value {
	case MatcherRegex, MatcherPrefix, MatcherContains, MatcherContainsBetween:
		return true
	default:
		return false
	}
}

func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
		return value[1 : len(value)-1]
	}
	return value
}

func (d Definition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("pattern %s: missing name", d.Name)
	}
	if d.Matcher == "" {
		return fmt.Errorf("pattern %s: missing type", d.Name)
	}
	if d.Category == "" {
		return fmt.Errorf("pattern %s: missing category", d.Name)
	}
	if d.Confidence == "" {
		return fmt.Errorf("pattern %s: missing confidence", d.Name)
	}

	switch d.Matcher {
	case MatcherRegex:
		if d.Pattern == "" {
			return fmt.Errorf("pattern %s: regex requires pattern", d.Name)
		}
		if _, err := regexp.Compile(d.Pattern); err != nil {
			return fmt.Errorf("pattern %s: invalid regex: %w", d.Name, err)
		}
		if d.Value != "" || d.BlockStart != "" || d.BlockEnd != "" {
			return fmt.Errorf("pattern %s: regex pattern may not define value or block markers", d.Name)
		}
	case MatcherPrefix, MatcherContains:
		if d.Value == "" {
			return fmt.Errorf("pattern %s: %s requires value", d.Name, d.Matcher)
		}
		if d.Pattern != "" || d.BlockStart != "" || d.BlockEnd != "" {
			return fmt.Errorf("pattern %s: %s pattern may not define pattern or block markers", d.Name, d.Matcher)
		}
	case MatcherContainsBetween:
		if d.BlockStart == "" {
			return fmt.Errorf("pattern %s: contains_between requires block_start", d.Name)
		}
		if d.BlockEnd == "" {
			return fmt.Errorf("pattern %s: contains_between requires block_end", d.Name)
		}
		if d.BlockStart == d.BlockEnd {
			return fmt.Errorf("pattern %s: contains_between block_start and block_end must differ", d.Name)
		}
		if d.Pattern != "" || d.Value != "" {
			return fmt.Errorf("pattern %s: contains_between may not define pattern or value", d.Name)
		}
	default:
		return fmt.Errorf("pattern %s: unknown type %q", d.Name, d.Matcher)
	}

	return nil
}
