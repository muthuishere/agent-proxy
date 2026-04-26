package main

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/muthuishere/agentproxy/internal/config"
)

const defaultCACertEnv = "AGENTPROXY_CA_CERT"

func defaultCACertPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv(defaultCACertEnv)); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".agentproxy", "certs", "agentproxy-ca-cert.pem"), nil
}

func ensureCACertFileExists() (string, error) {
	certPath, err := defaultCACertPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(certPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("CA cert not found at %s\n       Run: agentproxy ca-setup", certPath)
		}
		return "", fmt.Errorf("stat CA cert %s: %w", certPath, err)
	}
	return certPath, nil
}

func isCACertTrusted(certPath string) (bool, string, error) {
	switch runtime.GOOS {
	case "darwin":
		fingerprint, err := certSHA1(certPath)
		if err != nil {
			return false, manualTrustCommand(certPath), err
		}
		cmd := exec.Command("security", "find-certificate", "-Z", "-a", "/Library/Keychains/System.keychain")
		out, err := cmd.CombinedOutput()
		if err != nil {
			if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
				return false, manualTrustCommand(certPath), nil
			}
			return false, manualTrustCommand(certPath), err
		}
		if strings.Contains(strings.ToUpper(string(out)), fingerprint) {
			return true, "", nil
		}
		return false, manualTrustCommand(certPath), nil
	case "linux":
		for _, candidate := range linuxTrustTargets() {
			if _, err := os.Stat(candidate); err == nil {
				return true, "", nil
			}
		}
		return false, manualTrustCommand(certPath), nil
	case "windows":
		cmd := exec.Command("certutil", "-verify", certPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
				return false, manualTrustCommand(certPath), nil
			}
			return false, manualTrustCommand(certPath), err
		}
		text := strings.ToLower(string(out))
		if strings.Contains(text, "cannot find object or property") || strings.Contains(text, "cannot find the requested object") {
			return false, manualTrustCommand(certPath), nil
		}
		return true, "", nil
	default:
		return false, "", nil
	}
}

func certSHA1(certPath string) (string, error) {
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return "", fmt.Errorf("decode PEM cert %s: no PEM block found", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse PEM cert %s: %w", certPath, err)
	}
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

func manualTrustCommand(certPath string) string {
	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain %s", shellQuote(certPath))
	case "windows":
		return fmt.Sprintf("certutil -addstore \"Root\" %s", certPath)
	default:
		targets := linuxTrustTargets()
		updateCmd := "sudo update-ca-certificates"
		if strings.Contains(targets[0], "/etc/pki/") {
			updateCmd = "sudo update-ca-trust"
		}
		return fmt.Sprintf("sudo cp %s %s\n         %s", shellQuote(certPath), shellQuote(targets[0]), updateCmd)
	}
}

func linuxTrustTargets() []string {
	if _, err := os.Stat("/usr/local/share/ca-certificates"); err == nil {
		return []string{"/usr/local/share/ca-certificates/agentproxy-ca.crt", "/etc/pki/ca-trust/source/anchors/agentproxy-ca.crt"}
	}
	return []string{"/etc/pki/ca-trust/source/anchors/agentproxy-ca.crt", "/usr/local/share/ca-certificates/agentproxy-ca.crt"}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func warnIfCACertNotTrusted(certPath string) {
	trusted, command, err := isCACertTrusted(certPath)
	if err != nil || trusted || command == "" {
		return
	}

	fmt.Fprintln(os.Stderr, "WARNING: CA cert is not trusted by the OS trust store.")
	fmt.Fprintln(os.Stderr, "         Rust-based agents (e.g. codex) will fail TLS verification.")
	fmt.Fprintf(os.Stderr, "         To trust: %s\n", command)
}

func statusAddress(configPath, host string, port int) (string, string, int, error) {
	if strings.TrimSpace(host) != "" && port > 0 {
		return net.JoinHostPort(host, fmt.Sprintf("%d", port)), host, port, nil
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return "", "", 0, err
	}
	if strings.TrimSpace(host) == "" {
		host = cfg.Proxy.Host
	}
	if port == 0 {
		port = cfg.Proxy.Port
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port)), host, port, nil
}

func checkProxyStatus(configPath, host string, port int, timeout time.Duration) (string, string, int, error) {
	addr, resolvedHost, resolvedPort, err := statusAddress(configPath, host, port)
	if err != nil {
		return "", "", 0, err
	}

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return addr, resolvedHost, resolvedPort, err
	}
	_ = conn.Close()
	return addr, resolvedHost, resolvedPort, nil
}
