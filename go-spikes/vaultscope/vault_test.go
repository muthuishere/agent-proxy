package vaultscope

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTTLVaultExpires(t *testing.T) {
	start := time.Unix(0, 0)
	vault := NewTTLVault(1 * time.Minute)
	vault.now = func() time.Time { return start }
	vault.Put("tok-1", "secret")

	if got, ok := vault.Get("tok-1"); !ok || got != "secret" {
		t.Fatalf("expected token to resolve before expiry")
	}

	vault.now = func() time.Time { return start.Add(61 * time.Second) }
	if _, ok := vault.Get("tok-1"); ok {
		t.Fatalf("expected token to expire")
	}
}

func TestTTLVaultLongStreamMissWhenTTLTooShort(t *testing.T) {
	start := time.Unix(0, 0)
	vault := NewTTLVault(60 * time.Second)
	vault.now = func() time.Time { return start }
	vault.Put("tok-stream", "secret")

	vault.now = func() time.Time { return start.Add(90 * time.Second) }
	if _, ok := vault.Get("tok-stream"); ok {
		t.Fatalf("expected restore miss after ttl during long stream")
	}
}

func TestSessionVaultIsolationAndCleanup(t *testing.T) {
	vault := NewSessionVault()
	vault.Put("s1", "tok", "secret-1")
	vault.Put("s2", "tok", "secret-2")

	if got, ok := vault.Get("s1", "tok"); !ok || got != "secret-1" {
		t.Fatalf("expected session s1 token")
	}
	if got, ok := vault.Get("s2", "tok"); !ok || got != "secret-2" {
		t.Fatalf("expected session s2 token")
	}

	vault.CloseSession("s1")
	if _, ok := vault.Get("s1", "tok"); ok {
		t.Fatalf("expected closed session to be removed")
	}
	if count := vault.SessionCount(); count != 1 {
		t.Fatalf("expected one remaining session, got %d", count)
	}
}

func TestConcurrentSoakNoMisses(t *testing.T) {
	const goroutines = 100
	const tokensPerGoroutine = 10

	ttlVault := NewTTLVault(5 * time.Minute)
	sessionVault := NewSessionVault()

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*tokensPerGoroutine*2)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", id)
			for j := 0; j < tokensPerGoroutine; j++ {
				token := fmt.Sprintf("tok-%d-%d", id, j)
				value := fmt.Sprintf("secret-%d-%d", id, j)

				ttlVault.Put(token, value)
				if got, ok := ttlVault.Get(token); !ok || got != value {
					errs <- fmt.Errorf("ttl miss for %s", token)
				}

				sessionVault.Put(sessionID, token, value)
				if got, ok := sessionVault.Get(sessionID, token); !ok || got != value {
					errs <- fmt.Errorf("session miss for %s", token)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if ttlVault.Size() != goroutines*tokensPerGoroutine {
		t.Fatalf("unexpected ttl vault size: %d", ttlVault.Size())
	}
	if sessionVault.SessionCount() != goroutines {
		t.Fatalf("unexpected session count: %d", sessionVault.SessionCount())
	}
}
