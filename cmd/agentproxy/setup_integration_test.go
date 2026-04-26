package main

import (
	"os"
	"testing"
)

// TestSetupNoTrustExportsCert exercises `agentproxy setup --no-trust` and
// asserts that the CA cert ends up at the expected on-disk path.
//
// Gated behind INTEGRATION_TRUST=1 so it doesn't run in CI by default.
//
// Why no automatic-trust integration test:
//
// The full trust path shells out to `security add-trusted-cert` /
// `update-ca-certificates` / `certutil -addstore Root`, all of which require
// sudo (Unix) or UAC elevation (Windows). CI runners typically can't satisfy
// either non-interactively, and even when they can, modifying the host trust
// store from a test is destructive. We therefore only verify the export side
// of `setup` here; the trust step is covered manually on real machines.
func TestSetupNoTrustExportsCert(t *testing.T) {
	if os.Getenv("INTEGRATION_TRUST") != "1" {
		t.Skip("set INTEGRATION_TRUST=1 to run setup integration tests")
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Ensure defaultCACertPath() falls back to the HOME-derived location.
	t.Setenv(defaultCACertEnv, "")

	if err := runSetup([]string{"--no-trust"}); err != nil {
		t.Fatalf("runSetup --no-trust: %v", err)
	}

	expected, err := defaultCACertPath()
	if err != nil {
		t.Fatalf("defaultCACertPath: %v", err)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected cert at %s: %v", expected, err)
	}
}
