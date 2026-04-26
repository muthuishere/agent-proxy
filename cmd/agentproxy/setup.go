package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// runSetup is the one-click bootstrap: ensure CA cert is exported, trust it
// in the OS store (idempotent), then either print next-steps or start the
// proxy if --start is passed.
//
// Designed so re-running is safe — every step short-circuits when its
// post-condition already holds.
func runSetup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	startProxy := flags.Bool("start", false, "After setup, start the proxy (foreground)")
	openBrowser := flags.Bool("open", false, "Open the dashboard in the default browser after setup")
	skipTrust := flags.Bool("no-trust", false, "Skip the OS-store trust step (export cert only)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	// 1. Ensure cert exists on disk.
	certPath, err := ensureCACertFileExists()
	if err != nil {
		return fmt.Errorf("ca-setup: %w", err)
	}
	fmt.Printf("CA cert: %s\n", certPath)

	// 2. Trust in OS store, unless told not to.
	if !*skipTrust {
		trusted, _, _ := isCACertTrusted(certPath)
		if trusted {
			fmt.Println("CA already trusted in OS store ✓")
		} else {
			fmt.Println("Trusting CA in OS store (you may be prompted for sudo) ...")
			if err := trustCAInOSStore(certPath); err != nil {
				fmt.Fprintf(os.Stderr, "Automatic trust failed: %v\n\n", err)
				fmt.Fprintln(os.Stderr, manualTrustCommand(certPath))
				return err
			}
			fmt.Println("CA trusted ✓")
		}
	}

	// 3. Optionally open the dashboard. We do this *before* starting the
	// proxy if --start so the browser pops up while the server warms up.
	if *openBrowser {
		_ = openURL("http://127.0.0.1:7718")
	}

	// 4. Print next-steps banner.
	fmt.Println()
	fmt.Println("Setup complete. Next:")
	if *startProxy {
		fmt.Println("  starting proxy now ...")
		// Re-enter main as `agentproxy start`.
		return runStart(nil)
	}
	fmt.Println("  agentproxy-start            # start proxy + dashboard")
	fmt.Println("  claudeproxy / codexproxy / copilotproxy   # run an agent through the proxy")
	fmt.Println("  open http://127.0.0.1:7718  # dashboard")
	return nil
}

// trustCAInOSStore runs the platform-appropriate trust command. On Unix it
// uses sudo when not already root; on Windows it relies on certutil's own
// elevation handling (the user will see a UAC prompt if needed).
func trustCAInOSStore(certPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return runMaybeSudo("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", certPath)
	case "linux":
		// Prefer Debian/Ubuntu path; fall back to RHEL/Fedora.
		if _, err := os.Stat("/usr/local/share/ca-certificates"); err == nil {
			if err := runMaybeSudo("cp", certPath, "/usr/local/share/ca-certificates/agentproxy-ca.crt"); err != nil {
				return err
			}
			return runMaybeSudo("update-ca-certificates")
		}
		if _, err := os.Stat("/etc/pki/ca-trust/source/anchors"); err == nil {
			if err := runMaybeSudo("cp", certPath, "/etc/pki/ca-trust/source/anchors/agentproxy-ca.crt"); err != nil {
				return err
			}
			return runMaybeSudo("update-ca-trust")
		}
		return errors.New("no recognised CA trust store found on this Linux distribution")
	case "windows":
		c := exec.Command("certutil", "-addstore", "-f", "Root", certPath)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		return c.Run()
	default:
		return fmt.Errorf("automatic CA trust not implemented for %s", runtime.GOOS)
	}
}

// runMaybeSudo runs cmd directly if we're already root; otherwise prepends
// sudo. Stdio is wired through so a sudo password prompt works.
func runMaybeSudo(name string, args ...string) error {
	if os.Geteuid() == 0 {
		c := exec.Command(name, args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Stdin = os.Stdin
		return c.Run()
	}
	full := append([]string{name}, args...)
	c := exec.Command("sudo", full...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}

// openURL opens a URL in the user's default browser. Returns an error if no
// platform handler is found; never returns nil unless the OS-level open
// command was invoked successfully.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", strings.ReplaceAll(url, "&", "^&"))
	default:
		return fmt.Errorf("don't know how to open URL on %s", runtime.GOOS)
	}
	return cmd.Start()
}
