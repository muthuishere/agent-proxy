package vaultscope

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkTTLVaultPutGet(b *testing.B) {
	vault := NewTTLVault(5 * time.Minute)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		token := fmt.Sprintf("tok-%d", i)
		vault.Put(token, "secret")
		if _, ok := vault.Get(token); !ok {
			b.Fatalf("missing token %s", token)
		}
	}
}

func BenchmarkSessionVaultPutGet(b *testing.B) {
	vault := NewSessionVault()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		token := fmt.Sprintf("tok-%d", i)
		vault.Put("session-1", token, "secret")
		if _, ok := vault.Get("session-1", token); !ok {
			b.Fatalf("missing token %s", token)
		}
	}
}
