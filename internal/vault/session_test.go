package vault

import (
	"strings"
	"sync"
	"testing"
)

func TestManagerReturnsSameSessionInstance(t *testing.T) {
	manager := NewManager(Options{})

	left := manager.Session("claude-1")
	right := manager.Session("claude-1")

	if left != right {
		t.Fatal("expected same session instance for identical session id")
	}
}

func TestSessionMaskAndRestore(t *testing.T) {
	session := NewManager(Options{}).Session("codex-1")
	secret := "postgres://user:pass@localhost:5432/prod"

	placeholder := session.Mask(secret, "GENERIC_CONNECTION_STRING")
	if strings.Contains(placeholder, "*") || strings.Contains(placeholder, "[GENERIC_CONNECTION_STRING:") {
		t.Fatalf("expected shape-preserving surrogate, got %q", placeholder)
	}
	if strings.Contains(strings.ToLower(placeholder), "dummy") || strings.Contains(strings.ToLower(placeholder), "mask") {
		t.Fatalf("surrogate should not use dummy/mask labels, got %q", placeholder)
	}
	if len(placeholder) != len(secret) || !strings.HasPrefix(placeholder, "postgres://") {
		t.Fatalf("expected same-length connection string surrogate, got %q", placeholder)
	}

	restored := session.Restore("payload=" + placeholder)
	if restored != "payload="+secret {
		t.Fatalf("unexpected restored payload: %q", restored)
	}
}

func TestSessionRestoreIsExactOnly(t *testing.T) {
	session := NewManager(Options{}).Session("exact-1")
	secret := "password=correct-horse-battery"

	placeholder := session.Mask(secret, "ENV_SECRET_ASSIGNMENT")
	mutated := placeholder[:len(placeholder)-1] + "X"
	if restored := session.Restore("payload=" + mutated); restored == "payload="+secret {
		t.Fatal("mutated surrogate must not restore")
	}
}

func TestSessionMaskUsesStableSurrogate(t *testing.T) {
	session := NewManager(Options{}).Session("stable-1")
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890ab"

	first := session.Mask(secret, "ANTHROPIC_API_KEY")
	second := session.Mask(secret, "ANTHROPIC_API_KEY")
	if first != second {
		t.Fatalf("expected stable surrogate, got %q and %q", first, second)
	}
	if strings.Contains(first, "abcdefghijklmnopqrstuvwxyz") || strings.Contains(first, "*") {
		t.Fatalf("surrogate should not expose prefix or visible mask: %q", first)
	}
	if strings.Contains(strings.ToLower(first), "dummy") || strings.Contains(strings.ToLower(first), "mask") {
		t.Fatalf("surrogate should not use dummy/mask labels: %q", first)
	}
	if restored := session.Restore(first); restored != secret {
		t.Fatalf("expected exact restore, got %q", restored)
	}
}

func TestAssignmentSurrogatePreservesKey(t *testing.T) {
	session := NewManager(Options{}).Session("assign-1")
	secret := `PASSWORD="real-secret-value"`

	placeholder := session.Mask(secret, "ENV_SECRET_ASSIGNMENT")
	if !strings.HasPrefix(placeholder, `PASSWORD="`) || !strings.HasSuffix(placeholder, `"`) || len(placeholder) != len(secret) {
		t.Fatalf("expected assignment-shaped surrogate, got %q", placeholder)
	}
	if strings.Contains(placeholder, "real-secret-value") || strings.Contains(strings.ToLower(placeholder), "dummy") {
		t.Fatalf("assignment surrogate should replace secret value without dummy label: %q", placeholder)
	}
	if restored := session.Restore(placeholder); restored != secret {
		t.Fatalf("expected restore to original assignment, got %q", restored)
	}
}

func TestMaskDoesNotRemaskKnownSurrogate(t *testing.T) {
	session := NewManager(Options{}).Session("remask-1")
	secret := "postgres://user:pass@localhost:5432/prod"

	placeholder := session.Mask(secret, "GENERIC_CONNECTION_STRING")
	if remasked := session.Mask(placeholder, "GENERIC_CONNECTION_STRING"); remasked != placeholder {
		t.Fatalf("known surrogate should not be remasked: %q -> %q", placeholder, remasked)
	}
	if restored := session.Restore(placeholder); restored != secret {
		t.Fatalf("expected original mapping preserved, got %q", restored)
	}
}

func TestCloseSessionRemovesState(t *testing.T) {
	manager := NewManager(Options{})
	session := manager.Session("claude-2")
	placeholder := session.Mask("sk-ant-api03-abc", "ANTHROPIC_API_KEY")

	manager.CloseSession("claude-2")

	if manager.SessionCount() != 0 {
		t.Fatalf("expected no live sessions, got %d", manager.SessionCount())
	}

	if restored := session.Restore(placeholder); restored == placeholder {
		t.Fatal("existing session pointer should still restore until callers drop it")
	}

	recreated := manager.Session("claude-2")
	if recreated == session {
		t.Fatal("expected new session after close")
	}
	if restored := recreated.Restore(placeholder); restored != placeholder {
		t.Fatal("new session must not inherit prior placeholders")
	}
}

func TestSessionConcurrentMasking(t *testing.T) {
	session := NewManager(Options{}).Session("copilot-1")

	const workers = 50
	const secretsPerWorker = 10

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < secretsPerWorker; j++ {
				value := strings.Repeat("x", worker+j+5)
				session.Mask(value, "TOKEN")
			}
		}(i)
	}
	wg.Wait()

	if session.Count() == 0 {
		t.Fatal("expected placeholders to be stored")
	}
}

func TestSessionReplacementsExposesPatternAndOriginal(t *testing.T) {
	mgr := NewManager(Options{})
	sess := mgr.Session("rep-1")
	sess.Mask("sk-ant-api03-realsecret123", "ANTHROPIC_API_KEY")
	sess.Mask("ghp_abcdef1234567890", "GITHUB_TOKEN")

	reps := sess.Replacements()
	if len(reps) != 2 {
		t.Fatalf("Replacements len = %d, want 2", len(reps))
	}
	byPattern := map[string]Replacement{}
	for _, r := range reps {
		byPattern[r.Pattern] = r
	}
	if r, ok := byPattern["ANTHROPIC_API_KEY"]; !ok || r.Original != "sk-ant-api03-realsecret123" || r.Surrogate == "" {
		t.Fatalf("anthropic replacement wrong: %+v", r)
	}
	if r, ok := byPattern["GITHUB_TOKEN"]; !ok || r.Original != "ghp_abcdef1234567890" || r.Surrogate == "" {
		t.Fatalf("github replacement wrong: %+v", r)
	}
}
