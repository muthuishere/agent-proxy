package ws

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/gorilla/websocket"
)

func TestWebSocketEchoThroughGoproxyPassThrough(t *testing.T) {
	echoServer := newTLSEchoServer(t)
	defer echoServer.Close()

	proxyURL, proxyServer := newProxyServer(t)
	defer proxyServer.Close()

	dialer := websocket.Dialer{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			RootCAs: echoServer.clientRoots,
		},
		HandshakeTimeout: 5 * time.Second,
	}

	headers := http.Header{}
	headers.Set("X-AgentProxy-Probe", "goproxy-pass-through")

	conn, resp, err := dialer.Dial(echoServer.wsURL, headers)
	if err != nil {
		t.Fatalf("dial websocket via proxy: %v", err)
	}
	defer conn.Close()

	if resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("unexpected upgrade response: %#v", resp)
	}

	payload := `{"kind":"codex","database_url":"postgres://user:secret@host:5432/db","aws_secret_access_key":"SECRETKEY1234567890","ssh_private_key":"-----BEGIN OPENSSH PRIVATE KEY-----"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(payload)); err != nil {
		t.Fatalf("write message: %v", err)
	}

	_, echoed, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read echoed message: %v", err)
	}

	if string(echoed) != payload {
		t.Fatalf("echo mismatch: got %q want %q", string(echoed), payload)
	}

	if got := proxyServer.connectCount(); got == 0 {
		t.Fatalf("expected CONNECT traffic through proxy")
	}
}

type tlsEchoServer struct {
	server      *httptest.Server
	wsURL       string
	clientRoots *x509.CertPool
}

func newTLSEchoServer(t *testing.T) *tlsEchoServer {
	t.Helper()

	upgrader := websocket.Upgrader{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Connection"), "Upgrade") && !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
			http.Error(w, "upgrade required", http.StatusUpgradeRequired)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(messageType, payload); err != nil {
				return
			}
		}
	})

	server := httptest.NewTLSServer(handler)

	certDER := server.TLS.Certificates[0].Certificate[0]
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse server cert: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(cert)

	return &tlsEchoServer{
		server:      server,
		wsURL:       "wss" + strings.TrimPrefix(server.URL, "https"),
		clientRoots: roots,
	}
}

func (s *tlsEchoServer) Close() {
	s.server.Close()
}

type proxyProbeServer struct {
	server *http.Server
	addr   string

	mu            sync.Mutex
	connectEvents int
}

func newProxyServer(t *testing.T) (*url.URL, *proxyProbeServer) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	probe := &proxyProbeServer{
		server: &http.Server{Handler: proxy},
		addr:   listener.Addr().String(),
	}

	proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		probe.mu.Lock()
		probe.connectEvents++
		probe.mu.Unlock()
		return goproxy.OkConnect, host
	})

	go func() {
		_ = probe.server.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = probe.server.Close()
	})

	u, err := url.Parse(fmt.Sprintf("http://%s", probe.addr))
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	return u, probe
}

func (p *proxyProbeServer) Close() {
	_ = p.server.Close()
}

func (p *proxyProbeServer) connectCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connectEvents
}
