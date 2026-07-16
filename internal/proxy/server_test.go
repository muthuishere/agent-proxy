package proxy

import (
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/agent-proxy/internal/config"
	"github.com/muthuishere/agent-proxy/internal/runtime"
)

// TestConditionalConnectMITM verifies that CONNECT requests to hosts NOT in the
// intercepted_domains list pass through without MITM, so unrelated apps that
// happen to share the proxy (Firebase, Razorpay redirects, etc.) are not
// disrupted by AgentProxy's local CA.
//
// Regression test for the 2026-04-08 huddle: previously HandleConnect was
// goproxy.AlwaysMitm which intercepted every CONNECT regardless of domain.
func TestConditionalConnectMITM(t *testing.T) {
	cfg := config.Default()
	// Resolve pattern file paths relative to repo root (test runs in package dir).
	cfg.Detection.PatternFile = "../../config/patterns.yaml"
	cfg.PII.PatternFile = "../../config/pii_patterns.yaml"
	cfg.Detection.InterceptedDomains = []string{"api.anthropic.com"}
	cfg.Logging.LogFile = t.TempDir() + "/traffic.jsonl"

	svc, err := runtime.New(cfg)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	defer svc.Close()

	srv := New(cfg, svc, Options{})

	// Pick a free port for the proxy.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.httpServer.Addr = ln.Addr().String()
	go func() { _ = srv.httpServer.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })

	// Wait for proxy ready.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Run("non-intercepted host passes through (no MITM)", func(t *testing.T) {
		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		defer conn.Close()
		// Use a TCP server we control as the upstream "non-intercepted" host
		// so we don't depend on the public internet. The host string in the
		// CONNECT line is what triggers ShouldIntercept; the actual dial goes
		// to upstream after passthrough.
		upstream, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("upstream listen: %v", err)
		}
		defer upstream.Close()
		go func() {
			c, _ := upstream.Accept()
			if c != nil {
				_ = c.Close()
			}
		}()

		req := "CONNECT " + upstream.Addr().String() + " HTTP/1.1\r\nHost: " + upstream.Addr().String() + "\r\n\r\n"
		if _, err := conn.Write([]byte(req)); err != nil {
			t.Fatalf("write CONNECT: %v", err)
		}
		buf := make([]byte, 256)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read CONNECT response: %v", err)
		}
		resp := string(buf[:n])
		if !strings.HasPrefix(resp, "HTTP/1.0 200") && !strings.HasPrefix(resp, "HTTP/1.1 200") {
			t.Fatalf("expected 200 OK passthrough, got: %q", resp)
		}
		// After OkConnect, the proxy bridges the TCP stream and does NOT present
		// a TLS handshake. If we had MITM'd, attempting a TLS handshake on this
		// raw TCP stream against a non-TLS upstream would fail with the proxy's
		// cert. Confirm by trying — should error (peer never sent ServerHello).
		tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
		_ = tlsConn.SetDeadline(time.Now().Add(500 * time.Millisecond))
		if err := tlsConn.Handshake(); err == nil {
			t.Fatal("TLS handshake unexpectedly succeeded — proxy MITM'd a non-intercepted host")
		}
	})
}
