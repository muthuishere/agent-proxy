package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Metrics tracks aggregate stats across all requests for the current run.
type Metrics struct {
	TotalRequests int64            `json:"total_requests"`
	SecretsCaught int64            `json:"secrets_caught"`
	Sessions      int64            `json:"sessions"`
	ByPattern     map[string]int64 `json:"by_pattern"`
	StartedAt     string           `json:"started_at"`
	LastUpdated   string           `json:"last_updated"`
}

type TrafficLogger struct {
	mu          sync.Mutex
	file        *os.File
	metricsPath string
	options     Options
	queue       chan queuedEvent
	queueMu     sync.RWMutex
	done        chan struct{}
	closeOnce   sync.Once
	closed      bool

	// metrics (guarded by mu)
	metrics  Metrics
	sessions map[string]struct{}

	// SSE subscribers
	subsMu sync.Mutex
	subs   map[chan string]struct{}
}

type Options struct {
	LogPassthrough  bool
	LogRequestBody  bool
	LogResponseBody bool
	LogOriginals    bool
	QueueSize       int
}

type event struct {
	Event                string            `json:"event"`
	RequestID            string            `json:"id,omitempty"`
	Method               string            `json:"method,omitempty"`
	Host                 string            `json:"host,omitempty"`
	Path                 string            `json:"path,omitempty"`
	Status               int               `json:"status,omitempty"`
	Direction            string            `json:"direction,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Body                 string            `json:"body,omitempty"` // legacy: transformed body for older dashboard readers
	RequestBody          string            `json:"request_body,omitempty"`
	ModifiedRequestBody  string            `json:"modified_request_body,omitempty"`
	ResponseBody         string            `json:"response_body,omitempty"`
	ModifiedResponseBody string            `json:"modified_response_body,omitempty"`
	MaskedCount          int               `json:"masked_count,omitempty"`
	Patterns             []string          `json:"patterns,omitempty"`
	Timestamp            string            `json:"ts"`
}

type queuedEvent struct {
	record      event
	maskedCount int
	patterns    []string
	sessionID   string
}

func New(path string, options Options) (*TrafficLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	metricsPath := filepath.Join(filepath.Dir(path), "metrics.json")
	if options.QueueSize <= 0 {
		options.QueueSize = 4096
	}

	l := &TrafficLogger{
		file:        file,
		metricsPath: metricsPath,
		options:     options,
		queue:       make(chan queuedEvent, options.QueueSize),
		done:        make(chan struct{}),
		metrics: Metrics{
			ByPattern: make(map[string]int64),
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		},
		sessions: make(map[string]struct{}),
		subs:     make(map[chan string]struct{}),
	}
	l.writeMetrics()
	go l.run()
	return l, nil
}

func (l *TrafficLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		l.queueMu.Lock()
		l.closed = true
		close(l.queue)
		l.queueMu.Unlock()
		<-l.done
	})
	return l.file.Close()
}

// Subscribe returns a channel that receives each new log line as it is written.
// Call Unsubscribe when done to avoid a goroutine/channel leak.
func (l *TrafficLogger) Subscribe() chan string {
	ch := make(chan string, 256)
	l.subsMu.Lock()
	l.subs[ch] = struct{}{}
	l.subsMu.Unlock()
	return ch
}

// Unsubscribe removes and closes a previously subscribed channel.
func (l *TrafficLogger) Unsubscribe(ch chan string) {
	l.subsMu.Lock()
	delete(l.subs, ch)
	l.subsMu.Unlock()
	close(ch)
}

// GetMetrics returns a snapshot of the current metrics.
func (l *TrafficLogger) GetMetrics() Metrics {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.metrics
	m.ByPattern = make(map[string]int64, len(l.metrics.ByPattern))
	for k, v := range l.metrics.ByPattern {
		m.ByPattern[k] = v
	}
	return m
}

func (l *TrafficLogger) LogRequest(host, path, method string, headers map[string]string, originalBody, modifiedBody string, maskedCount int, patterns []string, requestID string) error {
	record := event{
		Event:       "request",
		RequestID:   requestID,
		Method:      method,
		Host:        host,
		Path:        path,
		Headers:     headers,
		MaskedCount: maskedCount,
		Patterns:    patterns,
	}
	if l.options.LogRequestBody {
		record.Body = truncate(modifiedBody, 12000)
		record.ModifiedRequestBody = truncate(modifiedBody, 12000)
		if l.options.LogOriginals {
			record.RequestBody = truncate(originalBody, 12000)
		}
	}
	return l.write(record, maskedCount, patterns, "")
}

func (l *TrafficLogger) LogResponse(host, path string, status int, headers map[string]string, originalBody, modifiedBody string, maskedCount int, requestID string) error {
	record := event{
		Event:       "response",
		RequestID:   requestID,
		Host:        host,
		Path:        path,
		Status:      status,
		Headers:     headers,
		MaskedCount: maskedCount,
	}
	if l.options.LogResponseBody {
		record.Body = truncate(modifiedBody, 12000)
		record.ModifiedResponseBody = truncate(modifiedBody, 12000)
		if l.options.LogOriginals {
			record.ResponseBody = truncate(originalBody, 12000)
		}
	}
	return l.write(record, 0, nil, "")
}

func (l *TrafficLogger) LogWebSocket(host, path, direction, originalBody, modifiedBody string, maskedCount int, patterns []string, requestID string) error {
	record := event{
		Event:       "websocket",
		RequestID:   requestID,
		Host:        host,
		Path:        path,
		Direction:   direction,
		MaskedCount: maskedCount,
		Patterns:    patterns,
	}
	if direction == "client->server" && l.options.LogRequestBody {
		record.Body = truncate(modifiedBody, 12000)
		record.ModifiedRequestBody = truncate(modifiedBody, 12000)
		if l.options.LogOriginals {
			record.RequestBody = truncate(originalBody, 12000)
		}
	}
	if direction == "server->client" && l.options.LogResponseBody {
		record.Body = truncate(modifiedBody, 12000)
		record.ModifiedResponseBody = truncate(modifiedBody, 12000)
		if l.options.LogOriginals {
			record.ResponseBody = truncate(originalBody, 12000)
		}
	}
	return l.write(record, maskedCount, patterns, "")
}

func (l *TrafficLogger) LogPassthrough(host, path, method string) error {
	if !l.options.LogPassthrough {
		return nil
	}
	return l.write(event{
		Event:  "passthrough",
		Method: method,
		Host:   host,
		Path:   path,
	}, 0, nil, "")
}

func (l *TrafficLogger) write(record event, maskedCount int, patterns []string, sessionID string) error {
	record.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	l.queueMu.RLock()
	defer l.queueMu.RUnlock()
	if l.closed {
		return nil
	}
	q := queuedEvent{record: record, maskedCount: maskedCount, patterns: patterns, sessionID: sessionID}
	select {
	case l.queue <- q:
	default:
		// Never let disk/dashboard logging apply backpressure to proxied traffic.
	}
	return nil
}

func (l *TrafficLogger) run() {
	defer close(l.done)
	for q := range l.queue {
		_ = l.writeSync(q)
	}
}

func (l *TrafficLogger) writeSync(q queuedEvent) error {
	line, err := json.Marshal(q.record)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	l.mu.Lock()
	_, writeErr := l.file.Write(append(line, '\n'))
	// Update metrics
	if q.record.Event == "request" || q.record.Event == "websocket" {
		l.metrics.TotalRequests++
		if q.maskedCount > 0 {
			l.metrics.SecretsCaught += int64(q.maskedCount)
			for _, p := range q.patterns {
				l.metrics.ByPattern[p]++
			}
		}
	}
	if q.sessionID != "" {
		if _, seen := l.sessions[q.sessionID]; !seen {
			l.sessions[q.sessionID] = struct{}{}
			l.metrics.Sessions++
		}
	}
	l.metrics.LastUpdated = q.record.Timestamp
	l.writeMetrics()
	l.mu.Unlock()

	if writeErr != nil {
		return fmt.Errorf("write event: %w", writeErr)
	}

	// Broadcast to SSE subscribers (non-blocking: drop if buffer full)
	lineStr := string(line)
	l.subsMu.Lock()
	for ch := range l.subs {
		select {
		case ch <- lineStr:
		default:
		}
	}
	l.subsMu.Unlock()

	return nil
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

// writeMetrics writes the current metrics snapshot to metrics.json atomically.
// Caller must hold l.mu.
func (l *TrafficLogger) writeMetrics() {
	data, err := json.MarshalIndent(l.metrics, "", "  ")
	if err != nil {
		return
	}
	tmp := l.metricsPath + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, l.metricsPath)
}
