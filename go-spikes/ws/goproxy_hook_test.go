package ws

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/elazarl/goproxy"
)

func TestWebSocketThroughGoproxyMITMHookMutatesFrames(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		ctx := r.Context()
		for {
			mt, payload, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, mt, payload); err != nil {
				return
			}
		}
	}))
	defer backend.Close()

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.WebSocketMessageHandler = func(ctx *goproxy.ProxyCtx, direction goproxy.WebSocketDirection, frame goproxy.WebSocketFrame) (goproxy.WebSocketFrame, error) {
		if direction == goproxy.WebSocketClientToServer && frame.Opcode == 0x1 {
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), "postgres://user:secret@host:5432/db", "post************************db[GENERIC_CONNECTION_STRING:spike4]"))
		}
		return frame, nil
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, backend.URL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("dial websocket via goproxy mitm: %v", err)
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	original := `{"database_url":"postgres://user:secret@host:5432/db","message":"hello"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(original)); err != nil {
		t.Fatalf("write websocket message: %v", err)
	}

	mt, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	if mt != websocket.MessageText {
		t.Fatalf("unexpected message type: %v", mt)
	}
	got := string(payload)
	if !strings.Contains(got, "[GENERIC_CONNECTION_STRING:spike4]") {
		t.Fatalf("expected mutated payload, got %q", got)
	}
	if strings.Contains(got, "postgres://user:secret@host:5432/db") {
		t.Fatalf("raw secret survived mutation: %q", got)
	}
}

func TestWebSocketThroughGoproxyMITMHookCanBePathGated(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		ctx := r.Context()
		for {
			mt, payload, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if err := conn.Write(ctx, mt, payload); err != nil {
				return
			}
		}
	}))
	defer backend.Close()

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.WebSocketMessagePredicate = func(ctx *goproxy.ProxyCtx) bool {
		return ctx != nil && ctx.Req != nil && ctx.Req.URL != nil && ctx.Req.URL.Path == "/backend-api/codex/responses"
	}
	proxy.WebSocketMessageHandler = func(ctx *goproxy.ProxyCtx, direction goproxy.WebSocketDirection, frame goproxy.WebSocketFrame) (goproxy.WebSocketFrame, error) {
		if direction == goproxy.WebSocketClientToServer && frame.Opcode == 0x1 {
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), "postgres://user:secret@host:5432/db", "post************************db[GENERIC_CONNECTION_STRING:gated]"))
		}
		return frame, nil
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	dial := func(path string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		targetURL := "wss" + strings.TrimPrefix(backend.URL, "https") + path
		conn, _, err := websocket.Dial(ctx, targetURL, &websocket.DialOptions{
			HTTPClient: &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("dial websocket via goproxy mitm: %v", err)
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		original := `{"database_url":"postgres://user:secret@host:5432/db","message":"hello"}`
		if err := conn.Write(ctx, websocket.MessageText, []byte(original)); err != nil {
			t.Fatalf("write websocket message: %v", err)
		}

		_, payload, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read websocket message: %v", err)
		}
		return string(payload)
	}

	gated := dial("/backend-api/codex/responses")
	if !strings.Contains(gated, "[GENERIC_CONNECTION_STRING:gated]") {
		t.Fatalf("expected gated path to mutate payload, got %q", gated)
	}

	ungated := dial("/other")
	if strings.Contains(ungated, "[GENERIC_CONNECTION_STRING:gated]") {
		t.Fatalf("expected ungated path to bypass mutation, got %q", ungated)
	}
	if !strings.Contains(ungated, "postgres://user:secret@host:5432/db") {
		t.Fatalf("expected ungated path to preserve original payload, got %q", ungated)
	}
}
