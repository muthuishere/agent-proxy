package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	_ "embed"

	"github.com/muthuishere/agentproxy/internal/logger"
)

//go:embed dashboard.html
var dashboardHTML []byte

// Server serves the web dashboard and API endpoints.
type Server struct {
	lg      *logger.TrafficLogger
	store   *EventStore
	logPath string
	httpSrv *http.Server
}

// New creates a UI server that reads from lg and tails logPath. The optional
// EventStore exposes per-request originals + replacements via /api/traffic/{id}.
// Pass nil to disable that endpoint.
func New(lg *logger.TrafficLogger, store *EventStore, logPath string, listenAddr string) *Server {
	mux := http.NewServeMux()
	s := &Server{
		lg:      lg,
		store:   store,
		logPath: logPath,
		httpSrv: &http.Server{Addr: listenAddr, Handler: mux},
	}
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/metrics", s.handleMetrics)
	mux.HandleFunc("/api/logs/stream", s.handleLogStream)
	// Go 1.22+ pattern matching: distinct exact and wildcard routes (avoids
	// the net/http subtree-redirect that turns /api/traffic into 301 → /api/traffic/).
	mux.HandleFunc("GET /api/traffic", s.handleTrafficList)
	mux.HandleFunc("GET /api/traffic/{id}", s.handleTrafficDetail)
	return s
}

// ListenAndServe starts the UI HTTP server. It blocks until the server stops.
func (s *Server) ListenAndServe() error {
	return s.httpSrv.ListenAndServe()
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	m := s.lg.GetMetrics()
	_ = json.NewEncoder(w).Encode(m)
}

// handleLogStream streams the existing log file contents followed by live
// events pushed from the logger, as Server-Sent Events.
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()

	// Subscribe to live events before opening the file so we don't miss
	// any events that arrive while we are replaying history.
	liveCh := s.lg.Subscribe()
	defer s.lg.Unsubscribe(liveCh)

	// Replay recent history from the log file (last ~100 KB).
	if err := s.replayHistory(w, flusher); err != nil {
		// Non-fatal — file may not exist yet on first run.
		_ = fmt.Sprintf("ui: replay history: %v", err)
	}

	// Stream live events until the client disconnects.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-liveCh:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		case <-ticker.C:
			// Keepalive comment so the browser doesn't drop the connection.
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// replayHistory sends the last ~100 KB of the log file as SSE events so the
// dashboard is populated immediately on page load.
func (s *Server) replayHistory(w http.ResponseWriter, flusher http.Flusher) error {
	f, err := os.Open(s.logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	const tail = 100 * 1024
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		if _, err := f.Seek(-tail, io.SeekEnd); err != nil {
			return err
		}
		// Discard the partial first line.
		if _, err := bufio.NewReader(f).ReadString('\n'); err != nil && err != io.EOF {
			return err
		}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	flusher.Flush()
	return scanner.Err()
}

// handleTrafficList returns a summary of every event currently in the store
// (newest first). Originals are not included — for full detail use
// /api/traffic/{id}.
func (s *Server) handleTrafficList(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "event store disabled", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(s.store.List())
}

// handleTrafficDetail serves a single MaskingEvent by request id at
// GET /api/traffic/{id}. Originals are sensitive — they live in memory only
// and disappear after the store's TTL.
func (s *Server) handleTrafficDetail(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "event store disabled", http.StatusNotFound)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		// Fall back to path stripping for tests that build the request directly.
		id = r.URL.Path[len("/api/traffic/"):]
	}
	if id == "" {
		http.Error(w, "missing request id", http.StatusBadRequest)
		return
	}
	ev := s.store.Get(id)
	if ev == nil {
		http.Error(w, "not found or expired", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(ev)
}

// FreePort finds a free TCP port on localhost. Used in tests.
func FreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}
