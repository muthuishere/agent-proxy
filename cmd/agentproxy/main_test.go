package main

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunStatusReturnsSuccessWhenProxyListening(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	err = run([]string{"status", "--host", addr.IP.String(), "--port", strconv.Itoa(addr.Port), "-q"})
	if err != nil {
		t.Fatalf("status should succeed, got %v", err)
	}
}

func TestRunStatusReturnsSentinelWhenProxyNotListening(t *testing.T) {
	err := run([]string{"status", "--host", "127.0.0.1", "--port", "1", "-q"})
	if err == nil {
		t.Fatal("expected status failure")
	}
	if err != errProxyNotRunning {
		t.Fatalf("expected errProxyNotRunning, got %v", err)
	}
}

func TestRunStartCheckFailsWhenCACertMissing(t *testing.T) {
	t.Setenv(defaultCACertEnv, filepath.Join(t.TempDir(), "missing-cert.pem"))

	err := run([]string{"start", "--check", "--config", filepath.Join("..", "..", "config", "agentproxy.yaml")})
	if err == nil {
		t.Fatal("expected missing cert error")
	}
	if !strings.Contains(err.Error(), "Run: agentproxy ca-setup") {
		t.Fatalf("expected actionable ca-setup hint, got %v", err)
	}
}
