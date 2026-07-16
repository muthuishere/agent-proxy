package scanner

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/muthuishere/agent-proxy/internal/patterns"
)

type Match struct {
	Name       string
	Value      string
	Category   string
	Confidence string
	Start      int
	End        int
}

type Pattern struct {
	Name       string
	Matcher    string
	Category   string
	Confidence string
	regex      *regexp.Regexp
	value      string
	blockStart string
	blockEnd   string
}

type Scanner struct {
	patterns []Pattern
	workers  int
}

func NewFromFile(path string, workers int, includeNames []string) (*Scanner, error) {
	defs, err := patterns.LoadFile(path, includeNames)
	if err != nil {
		return nil, err
	}
	return New(defs, workers), nil
}

func New(defs []patterns.Definition, workers int) *Scanner {
	compiled := make([]Pattern, 0, len(defs))
	for _, def := range defs {
		pattern, ok := compilePattern(def)
		if !ok {
			continue
		}
		compiled = append(compiled, pattern)
	}

	if workers < 1 {
		workers = 1
	}

	return &Scanner{
		patterns: compiled,
		workers:  workers,
	}
}

func compilePattern(def patterns.Definition) (Pattern, bool) {
	if err := def.Validate(); err != nil {
		return Pattern{}, false
	}

	p := Pattern{
		Name:       def.Name,
		Matcher:    def.Matcher,
		Category:   def.Category,
		Confidence: def.Confidence,
		value:      def.Value,
		blockStart: def.BlockStart,
		blockEnd:   def.BlockEnd,
	}
	if def.Matcher == patterns.MatcherRegex {
		re, err := regexp.Compile(def.Pattern)
		if err != nil {
			return Pattern{}, false
		}
		p.regex = re
	}
	return p, true
}

func (s *Scanner) PatternCount() int {
	return len(s.patterns)
}

func (s *Scanner) Workers() int {
	return s.workers
}

func (s *Scanner) ScanText(text string) []Match {
	if s.workers == 1 || len(s.patterns) < 2 {
		return sortMatches(s.scanPatterns(text, s.patterns))
	}

	jobs := make(chan Pattern)
	results := make(chan []Match, len(s.patterns))

	var wg sync.WaitGroup
	workerCount := s.workers
	if workerCount > len(s.patterns) {
		workerCount = len(s.patterns)
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pattern := range jobs {
				results <- s.scanPatterns(text, []Pattern{pattern})
			}
		}()
	}

	go func() {
		for _, pattern := range s.patterns {
			jobs <- pattern
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	matches := make([]Match, 0)
	for chunk := range results {
		matches = append(matches, chunk...)
	}
	return sortMatches(matches)
}

func (s *Scanner) scanPatterns(text string, subset []Pattern) []Match {
	matches := make([]Match, 0)
	for _, pattern := range subset {
		matches = append(matches, scanPattern(text, pattern)...)
	}
	return matches
}

func scanPattern(text string, pattern Pattern) []Match {
	switch pattern.Matcher {
	case patterns.MatcherPrefix:
		return scanPrefix(text, pattern)
	case patterns.MatcherContains:
		return scanContains(text, pattern)
	case patterns.MatcherContainsBetween:
		return scanContainsBetween(text, pattern)
	default:
		return scanRegex(text, pattern)
	}
}

func scanRegex(text string, pattern Pattern) []Match {
	indices := pattern.regex.FindAllStringIndex(text, -1)
	matches := make([]Match, 0, len(indices))
	for _, idx := range indices {
		matches = append(matches, newMatch(pattern, text, idx[0], idx[1]))
	}
	return matches
}

func scanPrefix(text string, pattern Pattern) []Match {
	var matches []Match
	offset := 0
	for {
		idx := strings.Index(text[offset:], pattern.value)
		if idx == -1 {
			break
		}
		start := offset + idx
		end := start
		for end < len(text) && text[end] != '\n' && text[end] != '\r' {
			end++
		}
		matches = append(matches, newMatch(pattern, text, start, end))
		offset = end
		if offset < len(text) && (text[offset] == '\n' || text[offset] == '\r') {
			offset++
		}
	}
	return matches
}

func scanContains(text string, pattern Pattern) []Match {
	var matches []Match
	offset := 0
	for {
		idx := strings.Index(text[offset:], pattern.value)
		if idx == -1 {
			break
		}
		start := offset + idx
		end := start + len(pattern.value)
		matches = append(matches, newMatch(pattern, text, start, end))
		offset = end
	}
	return matches
}

func scanContainsBetween(text string, pattern Pattern) []Match {
	var matches []Match
	offset := 0
	for {
		startRel := strings.Index(text[offset:], pattern.blockStart)
		if startRel == -1 {
			break
		}
		start := offset + startRel
		searchFrom := start + len(pattern.blockStart)
		endRel := strings.Index(text[searchFrom:], pattern.blockEnd)
		if endRel == -1 {
			offset = searchFrom
			continue
		}
		end := searchFrom + endRel + len(pattern.blockEnd)
		matches = append(matches, newMatch(pattern, text, start, end))
		offset = end
	}
	return matches
}

func newMatch(pattern Pattern, text string, start, end int) Match {
	return Match{
		Name:       pattern.Name,
		Value:      text[start:end],
		Category:   pattern.Category,
		Confidence: pattern.Confidence,
		Start:      start,
		End:        end,
	}
}

func sortMatches(matches []Match) []Match {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Start != matches[j].Start {
			return matches[i].Start < matches[j].Start
		}
		if matches[i].End != matches[j].End {
			return matches[i].End > matches[j].End
		}
		return matches[i].Name < matches[j].Name
	})
	return matches
}
