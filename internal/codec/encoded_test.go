package codec

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/muthuishere/agent-proxy/internal/vault"
)

type scanner struct {
	secrets map[string]string
}

func (s scanner) ScanText(text string) []Match {
	var matches []Match
	for name, value := range s.secrets {
		if strings.Contains(text, value) {
			matches = append(matches, Match{Name: name, Value: value})
		}
	}
	return matches
}

func TestRewriteEncodedBlobsEmbeddedTokensBase64RoundTrip(t *testing.T) {
	session := vault.NewManager(vault.Options{}).Session("s1")
	secret := "postgres://user:pass@localhost:5432/prod"
	body := `{"data":"` + base64.StdEncoding.EncodeToString([]byte(secret)) + `"}`

	rewritten, masked := RewriteEncodedBlobs(body, scanner{secrets: map[string]string{
		"GENERIC_CONNECTION_STRING": secret,
	}}, session, EncodedBlobOptions{Strategy: EmbeddedTokens})

	if masked != 1 {
		t.Fatalf("expected one mask, got %d", masked)
	}
	if rewritten == body {
		t.Fatal("expected rewritten body to change")
	}

	restored := RestoreEncodedBlobs(rewritten, session, EncodedBlobOptions{Strategy: EmbeddedTokens})
	if restored != body {
		t.Fatalf("expected round-trip restore, got %q", restored)
	}
}

func TestRewriteEncodedBlobsWholeBlobTokenRoundTrip(t *testing.T) {
	session := vault.NewManager(vault.Options{}).Session("s2")
	secret := "sk-ant-api03-abcdefghijklmnop"
	encoded := base64.StdEncoding.EncodeToString([]byte("token=" + secret))

	rewritten, masked := RewriteEncodedBlobs(encoded, scanner{secrets: map[string]string{
		"ANTHROPIC_API_KEY": secret,
	}}, session, EncodedBlobOptions{Strategy: WholeBlobTokens})

	if masked != 1 {
		t.Fatalf("expected whole-blob mask count of 1, got %d", masked)
	}
	if strings.Contains(rewritten, secret) {
		t.Fatal("rewritten text must not contain raw secret")
	}

	restored := RestoreEncodedBlobs(rewritten, session, EncodedBlobOptions{Strategy: WholeBlobTokens})
	if restored != encoded {
		t.Fatalf("expected whole blob restore, got %q want %q", restored, encoded)
	}
}

func TestRewriteEncodedBlobsHexAndURL(t *testing.T) {
	session := vault.NewManager(vault.Options{}).Session("s3")
	hexSecret := "ghp_1234567890abcdefghijklmnopqrst"
	urlSecret := "postgres://user:pass@db.example.com:5432/prod"

	body := strings.Join([]string{
		"hex=" + hex.EncodeToString([]byte(hexSecret)),
		"url=" + escapeAll(urlSecret),
	}, "&")

	rewritten, masked := RewriteEncodedBlobs(body, scanner{secrets: map[string]string{
		"GITHUB_TOKEN":              hexSecret,
		"GENERIC_CONNECTION_STRING": urlSecret,
	}}, session, EncodedBlobOptions{Strategy: EmbeddedTokens})

	if masked != 2 {
		t.Fatalf("expected two masks, got %d", masked)
	}

	restored := RestoreEncodedBlobs(rewritten, session, EncodedBlobOptions{Strategy: EmbeddedTokens})
	if restored != body {
		t.Fatalf("unexpected restore: %q", restored)
	}
}

func TestRewriteEncodedBlobsPreservesJSONForURLEncodedPayload(t *testing.T) {
	session := vault.NewManager(vault.Options{}).Session("s4")
	secret := "postgres://user:pass@localhost:5432/prod"
	payload := `{"data":"` + escapeAll(secret) + `"}`

	rewritten, masked := RewriteEncodedBlobs(payload, scanner{secrets: map[string]string{
		"GENERIC_CONNECTION_STRING": secret,
	}}, session, EncodedBlobOptions{Strategy: EmbeddedTokens})

	if masked != 1 {
		t.Fatalf("expected one mask, got %d", masked)
	}
	if _, err := url.ParseQuery("data=" + rewritten[9:len(rewritten)-2]); err != nil {
		t.Fatalf("expected URL-encoded payload to remain parseable: %v", err)
	}
}

func TestRewriteEncodedBlobsNestedDepthLimit(t *testing.T) {
	session := vault.NewManager(vault.Options{}).Session("s5")
	secret := "aws-secret-value-1234567890-abcdefghijklmnopqrstuvwxyz"

	level1 := base64.StdEncoding.EncodeToString([]byte(secret))
	level2 := base64.StdEncoding.EncodeToString([]byte(level1))
	level3 := base64.StdEncoding.EncodeToString([]byte(level2))
	level4 := base64.StdEncoding.EncodeToString([]byte(level3))

	rewritten, masked := RewriteEncodedBlobs(level4, scanner{secrets: map[string]string{
		"AWS_SECRET_ACCESS_KEY": secret,
	}}, session, EncodedBlobOptions{Strategy: EmbeddedTokens, MaxDepth: 4})

	if masked != 1 {
		t.Fatalf("expected one nested mask, got %d", masked)
	}

	restored := RestoreEncodedBlobs(rewritten, session, EncodedBlobOptions{Strategy: EmbeddedTokens, MaxDepth: 4})
	if restored != level4 {
		t.Fatalf("expected nested restore to round trip")
	}
}
