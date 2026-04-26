package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	goproxy "github.com/elazarl/goproxy"
)

func runCASetup(args []string) error {
	fs := flag.NewFlagSet("ca-setup", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	certDirFlag := fs.String("cert-dir", "", "directory to write the CA cert (default: ~/.agentproxy/certs)")
	noTrustInstructions := fs.Bool("no-trust-instructions", false, "do not print manual OS trust instructions")
	if err := fs.Parse(args); err != nil {
		return err
	}

	certDir := *certDirFlag
	if certDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("locate home dir: %w", err)
		}
		certDir = filepath.Join(home, ".agentproxy", "certs")
	}

	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return fmt.Errorf("create cert dir %s: %w", certDir, err)
	}

	certPath := filepath.Join(certDir, "agentproxy-ca-cert.pem")
	existing, err := os.ReadFile(certPath)
	switch {
	case err == nil && bytes.Equal(existing, goproxy.CA_CERT):
		fmt.Printf("CA cert already present ✓ %s\n", certPath)
	case err == nil || os.IsNotExist(err):
		if err := os.WriteFile(certPath, goproxy.CA_CERT, 0o644); err != nil {
			return fmt.Errorf("write CA cert: %w", err)
		}
		fmt.Printf("CA cert written ✓ %s\n", certPath)
	default:
		return fmt.Errorf("read existing CA cert: %w", err)
	}

	// Create local runtime directories relative to the current working dir
	// (the repo root when called from install.sh / install.bat).
	for _, dir := range []string{"certs", "logs"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Printf("warning: could not create ./%s: %v\n", dir, err)
		} else {
			fmt.Printf("Directory ready: ./%s\n", dir)
		}
	}

	if !*noTrustInstructions {
		printTrustInstructions(certPath)
	}
	return nil
}

func printTrustInstructions(certPath string) {
	fmt.Println("")
	fmt.Println("To trust the CA cert run the command for your OS:")
	fmt.Println("")
	fmt.Printf("  macOS:\n    sudo security add-trusted-cert -d -r trustRoot \\\n      -k /Library/Keychains/System.keychain \\\n      %s\n", certPath)
	fmt.Println("")
	fmt.Printf("  Linux (Debian/Ubuntu):\n    sudo cp %s /usr/local/share/ca-certificates/agentproxy-ca.crt\n    sudo update-ca-certificates\n", certPath)
	fmt.Println("")
	fmt.Printf("  Linux (RHEL/Fedora):\n    sudo cp %s /etc/pki/ca-trust/source/anchors/agentproxy-ca.crt\n    sudo update-ca-trust\n", certPath)
	fmt.Println("")
	fmt.Printf("  Windows (run as Administrator):\n    certutil -addstore \"Root\" %s\n", certPath)
	fmt.Println("")
}
