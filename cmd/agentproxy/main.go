// Command agentproxy is a local MITM proxy that sits between AI coding
// agents and their remote APIs, masking secrets in outbound traffic and
// restoring them in the response.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/muthuishere/agent-proxy/internal/config"
	"github.com/muthuishere/agent-proxy/internal/proxy"
	agentruntime "github.com/muthuishere/agent-proxy/internal/runtime"
	"github.com/muthuishere/agent-proxy/internal/ui"
)

var version = "0.1.0-go-migration"
var errProxyNotRunning = errors.New("proxy not listening")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errProxyNotRunning) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "start":
		return runStart(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "config":
		return runConfig(args[1:])
	case "ca-setup":
		return runCASetup(args[1:])
	case "setup":
		return runSetup(args[1:])
	case "status":
		return runStatus(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "path to config file")
	checkOnly := fs.Bool("check", false, "validate startup dependencies without launching proxy")
	host := fs.String("host", "", "host to listen on (overrides config)")
	port := fs.Int("port", 0, "port to listen on (overrides config)")
	uiPort := fs.Int("ui-port", defaultDashboardPort, "port for the web dashboard (0 to disable)")
	verbose := fs.Bool("verbose", false, "enable verbose proxy debug logging (logs every request/response/handshake)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port < 0 || *port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.ValidateStartup(cfg); err != nil {
		return err
	}
	if *host != "" {
		cfg.Proxy.Host = *host
	}
	if *port != 0 {
		cfg.Proxy.Port = *port
	}

	certPath, err := ensureCACertFileExists()
	if err != nil {
		return err
	}
	warnIfCACertNotTrusted(certPath)

	fmt.Printf("config: %s\n", cfg.SourcePath)
	fmt.Printf("listen: %s:%d\n", cfg.Proxy.Host, cfg.Proxy.Port)
	fmt.Printf("domains: %d intercepted\n", len(cfg.Detection.InterceptedDomains))
	if *checkOnly {
		fmt.Println("startup validation passed")
		return nil
	}

	service, err := agentruntime.New(cfg)
	if err != nil {
		return err
	}
	defer service.Close()

	// Per-request in-memory store for the dashboard's mask/restore detail view.
	// Originals live here only — never persisted to disk.
	eventStore, stopStore := ui.NewEventStore(10 * time.Minute)
	defer stopStore()
	service.SetSink(eventStore)

	// Start web dashboard if enabled.
	logPath := config.ResolvePath(cfg.SourcePath, cfg.Logging.LogFile)
	if *uiPort > 0 {
		uiAddr := fmt.Sprintf("127.0.0.1:%d", *uiPort)
		uiServer := ui.New(service.Logger(), eventStore, logPath, uiAddr)
		go func() {
			if err := uiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "ui server: %v\n", err)
			}
		}()
		fmt.Printf("dashboard: http://127.0.0.1:%d\n", *uiPort)
	}

	server := proxy.New(cfg, service, proxy.Options{Verbose: *verbose})
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	fmt.Println("proxy runtime started")
	// Single eyeball-friendly banner. Per-line prints above remain so
	// existing scripts that grep for `dashboard:` keep working.
	if *uiPort > 0 {
		fmt.Printf("AgentProxy ready — proxy: http://%s:%d   dashboard: http://127.0.0.1:%d\n",
			cfg.Proxy.Host, cfg.Proxy.Port, *uiPort)
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		fmt.Printf("received %s, shutting down\n", sig)
		return nil
	case err := <-errCh:
		return err
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if err := config.ValidateStartup(cfg); err != nil {
		return err
	}

	fmt.Printf("config valid: %s\n", cfg.SourcePath)
	return nil
}

func runConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	fmt.Printf("source=%s\n", cfg.SourcePath)
	fmt.Printf("proxy=%s:%d\n", cfg.Proxy.Host, cfg.Proxy.Port)
	fmt.Printf("pattern_file=%s\n", config.ResolvePath(cfg.SourcePath, cfg.Detection.PatternFile))
	fmt.Printf("pii_enabled=%t\n", cfg.PII.Enabled)
	fmt.Printf("intercepted_domains=%d\n", len(cfg.Detection.InterceptedDomains))
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "path to config file")
	host := fs.String("host", "", "host to check")
	port := fs.Int("port", 0, "port to check")
	quiet := fs.Bool("q", false, "suppress status output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port < 0 || *port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}

	_, resolvedHost, resolvedPort, err := checkProxyStatus(*configPath, *host, *port, time.Second)
	if err != nil {
		if resolvedHost == "" && resolvedPort == 0 {
			return err
		}
		if !*quiet {
			fmt.Printf("AgentProxy is not running on %s:%d\n", resolvedHost, resolvedPort)
		}
		return errProxyNotRunning
	}

	if !*quiet {
		fmt.Printf("AgentProxy is running on %s:%d\n", resolvedHost, resolvedPort)
		// Match the format printed by `start` so scripts can grep for it.
		fmt.Printf("dashboard: http://127.0.0.1:%d\n", defaultDashboardPort)
	}
	return nil
}

// defaultDashboardPort mirrors the default `--ui-port` on `agentproxy start`.
// Kept here so `status` can print a consistent banner without re-parsing
// runtime state from a process that is already running.
const defaultDashboardPort = 7718

func printUsage() {
	fmt.Println("usage: agentproxy <command> [flags]")
	fmt.Println("")
	fmt.Println("commands:")
	fmt.Println("  start      start the Go proxy runtime")
	fmt.Println("  validate   validate config and startup file paths")
	fmt.Println("  config     print a concise loaded-config summary")
	fmt.Println("  ca-setup   export the CA cert and create required directories")
	fmt.Println("  setup      one-click bootstrap: export cert, trust in OS store, optionally start (--start, --open, --no-trust)")
	fmt.Println("  status     check whether the proxy is listening")
	fmt.Println("  version    print build version")
}
