package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/muthuishere/agentproxy/internal/config"
	agentruntime "github.com/muthuishere/agentproxy/internal/runtime"
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
		if ctx == nil || ctx.Req == nil || frame.Opcode != 0x1 {
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
