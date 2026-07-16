package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/elazarl/goproxy"
	"github.com/muthuishere/agent-proxy/internal/config"
	agentruntime "github.com/muthuishere/agent-proxy/internal/runtime"
)

var requestCounter atomic.Uint64

// newRequestID mints a per-request id correlatable across log entries.
// Format: <session>-<monotonic> — readable, unique per process.
func newRequestID(ctx *goproxy.ProxyCtx) string {
	n := requestCounter.Add(1)
	if ctx == nil {
		return fmt.Sprintf("0-%d", n)
	}
	return fmt.Sprintf("%d-%d", ctx.Session, n)
}

func requestIDFromCtx(ctx *goproxy.ProxyCtx) string {
	if ctx == nil || ctx.UserData == nil {
		return ""
	}
	if s, ok := ctx.UserData.(string); ok {
		return s
	}
	return ""
}

// Options configures optional proxy server behaviour.
type Options struct {
	// Verbose enables per-request/response debug logging from goproxy.
	// Activate with: agentproxy start --verbose
	Verbose bool
}

type Server struct {
	httpServer *http.Server
	service    *agentruntime.Service
}

func New(cfg config.Config, service *agentruntime.Service, opts Options) *Server {
	handler := goproxy.NewProxyHttpServer()
	handler.Verbose = opts.Verbose

	// Force HTTP/1.1 for upstream WebSocket requests.
	// Why: goproxy hijacks the 101 response by type-asserting resp.Body to
	// io.ReadWriter, which Go's net/http only exposes on HTTP/1.1 upgrades.
	// chatgpt.com (Codex) advertises HTTP/2 via ALPN, so Go's default
	// Transport negotiates h2 and the WS upgrade fails intermittently
	// ("Unable to use Websocket connection" / "Handshake not finished").
	// Empty TLSNextProto disables HTTP/2 ALPN for THIS transport only;
	// non-WS traffic still uses the default h2-capable handler.Tr.
	handler.WebSocketUpstreamTransport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		// Pin ALPN to http/1.1 so the upstream TLS handshake never offers h2.
		// Without this, even with TLSNextProto={} on the Transport, the
		// TLS layer can negotiate h2 if NextProtos is left at Go's default
		// ["h2","http/1.1"], and the WS upgrade silently fails.
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"http/1.1"},
		},
		// Belt-and-braces: empty (but non-nil) TLSNextProto also disables
		// HTTP/2 dispatch at the http.Transport level.
		TLSNextProto: map[string]func(authority string, c *tls.Conn) http.RoundTripper{},
	}

	handler.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		if service.ShouldIntercept(host) {
			return goproxy.MitmConnect, host
		}
		return goproxy.OkConnect, host
	})
	handler.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return req, nil
		}
		_ = req.Body.Close()
		reqID := newRequestID(ctx)
		ctx.UserData = reqID
		mutated, _, err := service.HandleRequest(sessionID(ctx), hostFromRequest(req), req.URL.Path, req.Method, req.Header, body, reqID)
		if err != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			return req, nil
		}
		req.Body = io.NopCloser(bytes.NewReader(mutated))
		req.ContentLength = int64(len(mutated))
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(mutated)))
		return req, nil
	})
	handler.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil || resp.Request == nil || resp.Body == nil {
			return resp
		}
		// WebSocket 101 upgrade: do NOT read or replace the body. resp.Body
		// is the hijacked TLS conn (implements io.ReadWriter); goproxy's
		// upgrade flow type-asserts it to splice the WS connection. If we
		// io.ReadAll it here we'd block forever (no Content-Length) and
		// destroy the cast — observed as "Unable to use Websocket connection"
		// for Codex requests. WS frame masking happens later via
		// WebSocketMessageHandler, not here.
		if isWebSocketUpgrade(resp) {
			return resp
		}
		reqID := requestIDFromCtx(ctx)
		if isSSE(resp) {
			sseBody, err := service.HandleSSEResponse(
				sessionID(ctx),
				hostFromRequest(resp.Request),
				resp.Request.URL.Path,
				resp.StatusCode,
				resp.Header,
				resp.Body,
				reqID,
			)
			if err != nil {
				return resp
			}
			resp.Body = sseBody
			resp.ContentLength = -1
			resp.Header.Del("Content-Length")
			return resp
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return resp
		}
		_ = resp.Body.Close()
		mutated, err := service.HandleResponse(sessionID(ctx), hostFromRequest(resp.Request), resp.Request.URL.Path, resp.StatusCode, resp.Header, body, reqID)
		if err != nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return resp
		}
		resp.Body = io.NopCloser(bytes.NewReader(mutated))
		resp.ContentLength = int64(len(mutated))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(mutated)))
		return resp
	})
	handler.WebSocketMessagePredicate = func(ctx *goproxy.ProxyCtx) bool {
		return ctx != nil && ctx.Req != nil && service.ShouldIntercept(hostFromRequest(ctx.Req))
	}
	handler.WebSocketMessageHandler = func(ctx *goproxy.ProxyCtx, direction goproxy.WebSocketDirection, frame goproxy.WebSocketFrame) (goproxy.WebSocketFrame, error) {
		if ctx == nil || ctx.Req == nil {
			return frame, nil
		}
		// Process TEXT (0x1) and BINARY (0x2) frames. Codex/chatgpt.com sends
		// JSON wrapped in binary frames; restricting to opcode 0x1 alone makes
		// masking silently skip every Codex prompt. Only valid UTF-8 binary
		// payloads are routed through the scanner; truly binary frames (image
		// bytes, audio, etc.) pass unchanged because the masker won't match
		// anyway and we forward the original bytes on substitution miss.
		if frame.Opcode != 0x1 && frame.Opcode != 0x2 {
			return frame, nil
		}
		if frame.Opcode == 0x2 && !utf8.Valid(frame.Payload) {
			return frame, nil
		}
		reqID := requestIDFromCtx(ctx)
		if reqID == "" {
			reqID = newRequestID(ctx)
			ctx.UserData = reqID
		}
		updated, _ := service.HandleWebSocket(
			sessionID(ctx),
			hostFromRequest(ctx.Req),
			ctx.Req.URL.Path,
			string(frame.Payload),
			direction == goproxy.WebSocketClientToServer,
			reqID,
		)
		frame.Payload = []byte(updated)
		return frame, nil
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              fmt.Sprintf("%s:%d", cfg.Proxy.Host, cfg.Proxy.Port),
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
		service: service,
	}
}

func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func hostFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	if req.URL != nil && req.URL.Hostname() != "" {
		return req.URL.Hostname()
	}
	return req.Host
}

func sessionID(ctx *goproxy.ProxyCtx) string {
	if ctx == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", ctx.Session)
}

func isSSE(resp *http.Response) bool {
	return strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
}

// isWebSocketUpgrade returns true for the 101 Switching Protocols response
// that completes a WebSocket handshake. Detected as: status=101 AND
// Upgrade header contains "websocket". We do not read or rewrite the body
// of such responses — goproxy's hijack flow needs resp.Body to still be the
// raw io.ReadWriter for splicing WS frames.
func isWebSocketUpgrade(resp *http.Response) bool {
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return false
	}
	return strings.EqualFold(resp.Header.Get("Upgrade"), "websocket")
}
