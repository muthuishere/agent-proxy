package ws

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/elazarl/goproxy"
)

func TestGoproxyMITMHookPersistentSessionSoak(t *testing.T) {
	const (
		clients             = 8
		messagesPerClient   = 100
		totalExpectedFrames = clients * messagesPerClient
	)

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

	var hookCalls sync.Map

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.WebSocketMessagePredicate = func(ctx *goproxy.ProxyCtx) bool {
		return ctx != nil && ctx.Req != nil && ctx.Req.URL != nil && ctx.Req.URL.Path == "/backend-api/codex/responses"
	}
	proxy.WebSocketMessageHandler = func(ctx *goproxy.ProxyCtx, direction goproxy.WebSocketDirection, frame goproxy.WebSocketFrame) (goproxy.WebSocketFrame, error) {
		if direction == goproxy.WebSocketClientToServer && frame.Opcode == 0x1 {
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), "postgres://user:secret@host:5432/db", "post************************db[GENERIC_CONNECTION_STRING:soak]"))
			if ctx != nil {
				if current, ok := hookCalls.Load(ctx.Session); ok {
					hookCalls.Store(ctx.Session, current.(int)+1)
				} else {
					hookCalls.Store(ctx.Session, 1)
				}
			}
		}
		return frame, nil
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	targetURL := "wss" + strings.TrimPrefix(backend.URL, "https") + "/backend-api/codex/responses"
	payload := []byte(`{"database_url":"postgres://user:secret@host:5432/db","message":"hello"}`)

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, clients)

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

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
				errCh <- err
				return
			}
			defer func() {
				_ = conn.Close(websocket.StatusNormalClosure, "")
			}()

			for j := 0; j < messagesPerClient; j++ {
				if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
					errCh <- err
					return
				}
				_, echoed, err := conn.Read(ctx)
				if err != nil {
					errCh <- err
					return
				}
				if !strings.Contains(string(echoed), "[GENERIC_CONNECTION_STRING:soak]") {
					errCh <- &soakPayloadError{payload: string(echoed)}
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	var observedFrames int
	hookCalls.Range(func(key, value any) bool {
		observedFrames += value.(int)
		return true
	})

	if observedFrames != totalExpectedFrames {
		t.Fatalf("expected %d hook invocations, got %d", totalExpectedFrames, observedFrames)
	}

	elapsed := time.Since(start)
	t.Logf("persistent soak: clients=%d messages_per_client=%d total_messages=%d elapsed=%s avg_per_message=%s", clients, messagesPerClient, totalExpectedFrames, elapsed, elapsed/time.Duration(totalExpectedFrames))
}

type soakPayloadError struct {
	payload string
}

func (e *soakPayloadError) Error() string {
	return "unexpected soak payload: " + e.payload
}

func TestGoproxyMITMHookLongSoakProfile(t *testing.T) {
	if os.Getenv("AGENTPROXY_LONG_SOAK") != "1" {
		t.Skip("set AGENTPROXY_LONG_SOAK=1 to run long soak profile")
	}

	const (
		gatedClients      = 12
		ungatedClients    = 6
		messagesPerClient = 250
	)

	rawSecret := "postgres://user:secret@host:5432/db"
	placeholder := "post************************db[GENERIC_CONNECTION_STRING:longsoak]"
	largePayload := fmt.Sprintf(`{"database_url":"%s","aws_secret_access_key":"SECRETKEY1234567890","ssh_private_key":"-----BEGIN OPENSSH PRIVATE KEY-----","message":"client","notes":"%s"}`, rawSecret, strings.Repeat("long-soak-payload-", 64))

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
			body := string(payload)
			switch r.URL.Path {
			case "/backend-api/codex/responses":
				if strings.Contains(body, rawSecret) {
					t.Errorf("gated backend saw raw secret during long soak")
					return
				}
				response := []byte(strings.ReplaceAll(body, placeholder, rawSecret))
				response = []byte(strings.ReplaceAll(string(response), `"message":"client"`, `"message":"server"`))
				if err := conn.Write(ctx, mt, response); err != nil {
					return
				}
			default:
				if err := conn.Write(ctx, mt, payload); err != nil {
					return
				}
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
		if frame.Opcode != 0x1 {
			return frame, nil
		}
		switch direction {
		case goproxy.WebSocketClientToServer:
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), rawSecret, placeholder))
		case goproxy.WebSocketServerToClient:
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), placeholder, rawSecret))
		}
		return frame, nil
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	profileDir := os.Getenv("AGENTPROXY_PPROF_DIR")
	if profileDir == "" {
		profileDir = t.TempDir()
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("create profile dir %s: %v", profileDir, err)
	}

	writeProfile := func(name string) {
		path := filepath.Join(profileDir, name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create profile %s: %v", path, err)
		}
		defer f.Close()
		switch {
		case strings.HasPrefix(name, "goroutine-"):
			if err := pprof.Lookup("goroutine").WriteTo(f, 0); err != nil {
				t.Fatalf("write goroutine profile: %v", err)
			}
		case strings.HasPrefix(name, "heap-"):
			runtime.GC()
			if err := pprof.WriteHeapProfile(f); err != nil {
				t.Fatalf("write heap profile: %v", err)
			}
		default:
			t.Fatalf("unknown profile requested: %s", name)
		}
	}

	writeProfile("goroutine-before.pprof")
	writeProfile("heap-before.pprof")

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	baseGoroutines := runtime.NumGoroutine()
	var peakGoroutines atomic.Int64
	peakGoroutines.Store(int64(baseGoroutines))

	stopSampler := make(chan struct{})
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				current := int64(runtime.NumGoroutine())
				for {
					peak := peakGoroutines.Load()
					if current <= peak || peakGoroutines.CompareAndSwap(peak, current) {
						break
					}
				}
			case <-stopSampler:
				return
			}
		}
	}()

	targetGated := "wss" + strings.TrimPrefix(backend.URL, "https") + "/backend-api/codex/responses"
	targetUngated := "wss" + strings.TrimPrefix(backend.URL, "https") + "/other"

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, gatedClients+ungatedClients)

	runClient := func(targetURL string, gated bool) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

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
			errCh <- err
			return
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		for i := 0; i < messagesPerClient; i++ {
			if err := conn.Write(ctx, websocket.MessageText, []byte(largePayload)); err != nil {
				errCh <- err
				return
			}
			_, echoed, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			got := string(echoed)
			if gated {
				if strings.Contains(got, placeholder) || !strings.Contains(got, rawSecret) || !strings.Contains(got, `"message":"server"`) {
					errCh <- &soakPayloadError{payload: got}
					return
				}
			} else if !strings.Contains(got, rawSecret) || !strings.Contains(got, `"message":"client"`) {
				errCh <- &soakPayloadError{payload: got}
				return
			}
		}
	}

	for i := 0; i < gatedClients; i++ {
		wg.Add(1)
		go runClient(targetGated, true)
	}
	for i := 0; i < ungatedClients; i++ {
		wg.Add(1)
		go runClient(targetUngated, false)
	}

	wg.Wait()
	close(errCh)
	close(stopSampler)
	samplerWG.Wait()
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	writeProfile("goroutine-after.pprof")
	writeProfile("heap-after.pprof")

	time.Sleep(250 * time.Millisecond)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	finalGoroutines := runtime.NumGoroutine()
	elapsed := time.Since(start)
	totalMessages := (gatedClients + ungatedClients) * messagesPerClient

	t.Logf(
		"long soak profile: profile_dir=%s gated_clients=%d ungated_clients=%d total_messages=%d elapsed=%s avg_per_message=%s heap_before=%d heap_after=%d heap_delta=%d base_goroutines=%d peak_goroutines=%d final_goroutines=%d",
		profileDir,
		gatedClients,
		ungatedClients,
		totalMessages,
		elapsed,
		elapsed/time.Duration(totalMessages),
		before.HeapAlloc,
		after.HeapAlloc,
		int64(after.HeapAlloc)-int64(before.HeapAlloc),
		baseGoroutines,
		peakGoroutines.Load(),
		finalGoroutines,
	)

	if finalGoroutines-baseGoroutines > 50 {
		t.Fatalf("goroutines did not settle after long soak: base=%d final=%d", baseGoroutines, finalGoroutines)
	}
}

func TestGoproxyMITMHookResourceProfile(t *testing.T) {
	const (
		gatedClients      = 8
		ungatedClients    = 4
		messagesPerClient = 120
	)

	rawSecret := "postgres://user:secret@host:5432/db"
	placeholder := "post************************db[GENERIC_CONNECTION_STRING:profile]"
	largePayload := fmt.Sprintf(`{"database_url":"%s","aws_secret_access_key":"SECRETKEY1234567890","ssh_private_key":"-----BEGIN OPENSSH PRIVATE KEY-----","message":"client","notes":"%s"}`, rawSecret, strings.Repeat("profile-payload-", 48))

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
			body := string(payload)
			switch r.URL.Path {
			case "/backend-api/codex/responses":
				if strings.Contains(body, rawSecret) {
					t.Errorf("gated backend saw raw secret during resource profile")
					return
				}
				response := []byte(strings.ReplaceAll(body, placeholder, rawSecret))
				response = []byte(strings.ReplaceAll(string(response), `"message":"client"`, `"message":"server"`))
				if err := conn.Write(ctx, mt, response); err != nil {
					return
				}
			default:
				if err := conn.Write(ctx, mt, payload); err != nil {
					return
				}
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
		if frame.Opcode != 0x1 {
			return frame, nil
		}
		switch direction {
		case goproxy.WebSocketClientToServer:
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), rawSecret, placeholder))
		case goproxy.WebSocketServerToClient:
			frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), placeholder, rawSecret))
		}
		return frame, nil
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	baseGoroutines := runtime.NumGoroutine()
	var peakGoroutines atomic.Int64
	peakGoroutines.Store(int64(baseGoroutines))

	stopSampler := make(chan struct{})
	var samplerWG sync.WaitGroup
	samplerWG.Add(1)
	go func() {
		defer samplerWG.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				current := int64(runtime.NumGoroutine())
				for {
					peak := peakGoroutines.Load()
					if current <= peak || peakGoroutines.CompareAndSwap(peak, current) {
						break
					}
				}
			case <-stopSampler:
				return
			}
		}
	}()

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, gatedClients+ungatedClients)

	runClient := func(path string, gated bool) {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
			errCh <- err
			return
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		for i := 0; i < messagesPerClient; i++ {
			if err := conn.Write(ctx, websocket.MessageText, []byte(largePayload)); err != nil {
				errCh <- err
				return
			}
			_, echoed, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			got := string(echoed)
			if gated {
				if strings.Contains(got, placeholder) || !strings.Contains(got, rawSecret) || !strings.Contains(got, `"message":"server"`) {
					errCh <- &soakPayloadError{payload: got}
					return
				}
			} else if !strings.Contains(got, rawSecret) || !strings.Contains(got, `"message":"client"`) {
				errCh <- &soakPayloadError{payload: got}
				return
			}
		}
	}

	for i := 0; i < gatedClients; i++ {
		wg.Add(1)
		go runClient("/backend-api/codex/responses", true)
	}
	for i := 0; i < ungatedClients; i++ {
		wg.Add(1)
		go runClient("/other", false)
	}

	wg.Wait()
	close(errCh)
	close(stopSampler)
	samplerWG.Wait()
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	elapsed := time.Since(start)
	time.Sleep(250 * time.Millisecond)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	finalGoroutines := runtime.NumGoroutine()

	heapDelta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	peakDelta := peakGoroutines.Load() - int64(baseGoroutines)
	finalDelta := finalGoroutines - baseGoroutines

	t.Logf(
		"resource profile: gated_clients=%d ungated_clients=%d messages_per_client=%d elapsed=%s heap_before=%d heap_after=%d heap_delta=%d base_goroutines=%d peak_goroutines=%d final_goroutines=%d",
		gatedClients,
		ungatedClients,
		messagesPerClient,
		elapsed,
		before.HeapAlloc,
		after.HeapAlloc,
		heapDelta,
		baseGoroutines,
		peakGoroutines.Load(),
		finalGoroutines,
	)

	if finalDelta > 40 {
		t.Fatalf("goroutines did not settle after soak: base=%d final=%d", baseGoroutines, finalGoroutines)
	}
	if peakDelta < 0 {
		t.Fatalf("invalid goroutine sampling result: base=%d peak=%d", baseGoroutines, peakGoroutines.Load())
	}
}

func TestGoproxyMITMHookSoakMatrix(t *testing.T) {
	const (
		gatedClients       = 6
		ungatedClients     = 6
		messagesPerClient  = 50
		totalGatedMessages = gatedClients * messagesPerClient
	)

	rawSecret := "postgres://user:secret@host:5432/db"
	placeholder := "post************************db[GENERIC_CONNECTION_STRING:matrix]"
	largePayload := fmt.Sprintf(`{"database_url":"%s","aws_secret_access_key":"SECRETKEY1234567890","ssh_private_key":"-----BEGIN OPENSSH PRIVATE KEY-----","message":"client","notes":"%s"}`, rawSecret, strings.Repeat("codex-payload-", 32))

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

			body := string(payload)
			switch r.URL.Path {
			case "/backend-api/codex/responses":
				if strings.Contains(body, rawSecret) {
					t.Errorf("gated backend saw raw secret: %q", body)
					return
				}
				if !strings.Contains(body, placeholder) {
					t.Errorf("gated backend missing placeholder: %q", body)
					return
				}
				response := []byte(strings.ReplaceAll(body, `"message":"client"`, `"message":"server"`))
				if err := conn.Write(ctx, mt, response); err != nil {
					return
				}
			default:
				if !strings.Contains(body, rawSecret) {
					t.Errorf("ungated backend did not receive raw secret: %q", body)
					return
				}
				if err := conn.Write(ctx, mt, payload); err != nil {
					return
				}
			}
		}
	}))
	defer backend.Close()

	var clientToServerCount atomic.Int64
	var serverToClientCount atomic.Int64

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.WebSocketMessagePredicate = func(ctx *goproxy.ProxyCtx) bool {
		return ctx != nil && ctx.Req != nil && ctx.Req.URL != nil && ctx.Req.URL.Path == "/backend-api/codex/responses"
	}
	proxy.WebSocketMessageHandler = func(ctx *goproxy.ProxyCtx, direction goproxy.WebSocketDirection, frame goproxy.WebSocketFrame) (goproxy.WebSocketFrame, error) {
		if frame.Opcode != 0x1 {
			return frame, nil
		}

		switch direction {
		case goproxy.WebSocketClientToServer:
			if strings.Contains(string(frame.Payload), rawSecret) {
				frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), rawSecret, placeholder))
				clientToServerCount.Add(1)
			}
		case goproxy.WebSocketServerToClient:
			if strings.Contains(string(frame.Payload), placeholder) {
				frame.Payload = []byte(strings.ReplaceAll(string(frame.Payload), placeholder, rawSecret))
				serverToClientCount.Add(1)
			}
		}
		return frame, nil
	}

	proxyServer := httptest.NewServer(proxy)
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, gatedClients+ungatedClients)

	runClient := func(path string, expectPlaceholder bool) {
		defer wg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
			errCh <- err
			return
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()

		for i := 0; i < messagesPerClient; i++ {
			payload := []byte(largePayload)
			if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
				errCh <- err
				return
			}
			_, echoed, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			got := string(echoed)
			if expectPlaceholder {
				if strings.Contains(got, placeholder) {
					errCh <- &soakPayloadError{payload: got}
					return
				}
				if !strings.Contains(got, rawSecret) || !strings.Contains(got, `"message":"server"`) {
					errCh <- &soakPayloadError{payload: got}
					return
				}
			} else {
				if !strings.Contains(got, rawSecret) {
					errCh <- &soakPayloadError{payload: got}
					return
				}
			}
		}
	}

	for i := 0; i < gatedClients; i++ {
		wg.Add(1)
		go runClient("/backend-api/codex/responses", true)
	}
	for i := 0; i < ungatedClients; i++ {
		wg.Add(1)
		go runClient("/other", false)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	if clientToServerCount.Load() != int64(totalGatedMessages) {
		t.Fatalf("expected %d client->server hook calls, got %d", totalGatedMessages, clientToServerCount.Load())
	}
	if serverToClientCount.Load() != int64(totalGatedMessages) {
		t.Fatalf("expected %d server->client hook calls, got %d", totalGatedMessages, serverToClientCount.Load())
	}

	elapsed := time.Since(start)
	t.Logf("matrix soak: gated_clients=%d ungated_clients=%d messages_per_client=%d total_messages=%d elapsed=%s avg_per_message=%s", gatedClients, ungatedClients, messagesPerClient, (gatedClients+ungatedClients)*messagesPerClient, elapsed, elapsed/time.Duration((gatedClients+ungatedClients)*messagesPerClient))
}
