// Package acceptance tests the full masking pipeline against realistic
// Claude and Codex payload shapes from docs/testing-scenarios.md.
//
// These tests are CI-runnable: they use the real pattern files and the full
// runtime.Service stack but do not require a live API or a running proxy.
package acceptance_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/muthuishere/agent-proxy/internal/config"
	agentruntime "github.com/muthuishere/agent-proxy/internal/runtime"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine caller path")
	}
	// tests/go/masking_acceptance_test.go → ../../
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func newService(t *testing.T, piiEnabled bool) *agentruntime.Service {
	t.Helper()
	root := repoRoot(t)
	cfg, err := config.Load(filepath.Join(root, "config", "agentproxy.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.PII.Enabled = piiEnabled
	cfg.Logging.LogFile = filepath.Join(t.TempDir(), "traffic.jsonl")
	svc, err := agentruntime.New(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

// maskRequest sends body through HandleRequest on api.anthropic.com and
// asserts that the given secrets are not present in the output.
func maskRequest(t *testing.T, svc *agentruntime.Service, sid, body string, secrets []string) string {
	t.Helper()
	mutated, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one masked secret, got 0\nbody: %s", body)
	}
	out := string(mutated)
	for _, secret := range secrets {
		if strings.Contains(out, secret) {
			t.Fatalf("secret still present after masking: %q\nmasked body: %s", secret[:min(len(secret), 60)], out)
		}
	}
	return out
}

// roundTrip masks a request and then restores the response, asserting the
// original body is recovered exactly.
func roundTrip(t *testing.T, svc *agentruntime.Service, sid, body string, secrets []string) {
	t.Helper()
	masked := maskRequest(t, svc, sid, body, secrets)
	restored, err := svc.HandleResponse(sid, "api.anthropic.com", "/v1/messages", 200, http.Header{}, []byte(masked), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if string(restored) != body {
		t.Fatalf("round-trip mismatch\nwant: %s\ngot:  %s", body, string(restored))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// maskWebSocketFrame sends text through HandleWebSocket (client→server path)
// and asserts that none of the given secrets appear in the result.
func maskWebSocketFrame(t *testing.T, svc *agentruntime.Service, sid, host, text string, secrets []string) string {
	t.Helper()
	masked, count := svc.HandleWebSocket(sid, host, "/backend-api/codex/responses", text, true, "")
	if count == 0 {
		t.Fatalf("expected at least one masked secret in WebSocket frame, got 0\ntext: %s", text)
	}
	for _, s := range secrets {
		if strings.Contains(masked, s) {
			t.Fatalf("secret still present in masked WebSocket frame: %q", s[:min(len(s), 60)])
		}
	}
	return masked
}

// roundTripWebSocket masks a WebSocket frame and then restores it via the
// server→client path, asserting the original text is recovered exactly.
func roundTripWebSocket(t *testing.T, svc *agentruntime.Service, sid, host, text string, secrets []string) {
	t.Helper()
	masked := maskWebSocketFrame(t, svc, sid, host, text, secrets)
	restored, _ := svc.HandleWebSocket(sid, host, "/backend-api/codex/responses", masked, false, "")
	if restored != text {
		t.Fatalf("WebSocket round-trip mismatch\nwant: %s\ngot:  %s", text, restored)
	}
}

// ---------------------------------------------------------------------------
// Secret masking — Required Regression Scenarios
// ---------------------------------------------------------------------------

func TestMaskAnthropicAPIKey(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	body := `{"messages":[{"role":"user","content":"key is ` + secret + `"}]}`
	roundTrip(t, svc, "t1", body, []string{secret})
}

func TestMaskOpenAIAPIKey(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJ"
	body := `{"messages":[{"role":"user","content":"key ` + secret + `"}]}`
	roundTrip(t, svc, "t2", body, []string{secret})
}

func TestMaskOpenAIProjectKey(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	body := `{"messages":[{"role":"user","content":"` + secret + `"}]}`
	roundTrip(t, svc, "t3", body, []string{secret})
}

func TestMaskGitHubToken(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB"
	body := `{"messages":[{"role":"user","content":"token=` + secret + `"}]}`
	roundTrip(t, svc, "t4", body, []string{secret})
}

func TestMaskAWSAccessKeyIDPlain(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "AKIA1234567890ABCDEF"
	body := `{"messages":[{"role":"user","content":"key ` + secret + `"}]}`
	roundTrip(t, svc, "t5", body, []string{secret})
}

func TestMaskAWSAccessKeyIDAssignment(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "AKIA1234567890ABCDEF"
	body := `{"content":"AWS_ACCESS_KEY_ID=` + secret + `"}`
	roundTrip(t, svc, "t6", body, []string{secret})
}

func TestMaskAWSSecretAccessKeyAssignment(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	body := `{"content":"AWS_SECRET_ACCESS_KEY=` + secret + `"}`
	roundTrip(t, svc, "t7", body, []string{secret})
}

func TestMaskAWSSecretAccessKeyQuotedExport(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	body := `{"content":"export AWS_SECRET_ACCESS_KEY=\"` + secret + `\""}`
	roundTrip(t, svc, "t8", body, []string{secret})
}

func TestMaskAWSSessionToken(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890"
	body := `{"content":"AWS_SESSION_TOKEN=` + secret + `"}`
	roundTrip(t, svc, "t9", body, []string{secret})
}

func TestMaskConnectionStringPlain(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	body := `{"messages":[{"role":"user","content":"db url: ` + secret + `"}]}`
	roundTrip(t, svc, "t10", body, []string{secret})
}

func TestMaskConnectionStringInsideJSON(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	// Secret is nested inside valid JSON — masking must not break the outer structure.
	body := `{"messages":[{"role":"user","content":"url is \"` + secret + `\" in config"}]}`
	masked := maskRequest(t, svc, "t11", body, []string{secret})
	// Outer JSON must remain parseable (no broken escape sequences).
	if !strings.Contains(masked, `"messages"`) {
		t.Fatalf("outer JSON structure broken after masking: %s", masked)
	}
}

func TestMaskOpenSSHPrivateKeyBlock(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	keyBody := strings.Join([]string{
		"b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAlwAAAAdzc2gtcn",
		"NhAAAAAwEAAQAAAIEAwfakeshorttestkeymaterialonlynotreal1234567890abcdefghi",
	}, "\n")
	sshKey := strings.Join([]string{
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		keyBody,
		"-----END OPENSSH PRIVATE KEY-----",
	}, "\n")
	body := `{"content":"` + strings.ReplaceAll(sshKey, "\n", "\\n") + `"}`
	masked := maskRequest(t, svc, "t12", body, []string{keyBody})
	if !strings.Contains(masked, "OPENSSH PRIVATE KEY") {
		t.Fatalf("PEM boundary should remain shape-preserving context: %s", masked)
	}
}

// ---------------------------------------------------------------------------
// Encoded content scenarios
// ---------------------------------------------------------------------------

func TestMaskBase64ConnectionString(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	body := `{"content":"` + encoded + `"}`
	roundTrip(t, svc, "enc1", body, []string{secret})
}

func TestMaskBase64DotenvBlock(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	connStr := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	apiKey := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	dotenv := "DATABASE_URL=" + connStr + "\nOPENAI_API_KEY=" + apiKey + "\nAUTH_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB"
	encoded := base64.StdEncoding.EncodeToString([]byte(dotenv))
	body := `{"content":"` + encoded + `"}`
	maskRequest(t, svc, "enc2", body, []string{connStr, apiKey})
}

func TestMaskHexSecret(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	encoded := hex.EncodeToString([]byte(secret))
	body := `{"content":"` + encoded + `"}`
	maskRequest(t, svc, "enc3", body, []string{secret})
}

func TestMaskURLEncodedSecret(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	encoded := url.QueryEscape(secret)
	body := `{"content":"` + encoded + `"}`
	maskRequest(t, svc, "enc4", body, []string{secret})
}

func TestMaskNestedEncodedContent(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	// Depth 2: base64 of base64
	inner := base64.StdEncoding.EncodeToString([]byte(secret))
	outer := base64.StdEncoding.EncodeToString([]byte(inner))
	body := `{"content":"` + outer + `"}`
	maskRequest(t, svc, "enc5", body, []string{secret})
}

func TestEncodedMaskingPreservesJSONStructure(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	body := `{"messages":[{"role":"user","content":"encoded: ` + encoded + `"}],"model":"claude-3-5-sonnet-20241022"}`
	masked := maskRequest(t, svc, "enc6", body, []string{secret})
	// Outer JSON fields must survive.
	for _, want := range []string{`"messages"`, `"model"`, `"role"`, `"user"`} {
		if !strings.Contains(masked, want) {
			t.Fatalf("JSON field %q missing after masking: %s", want, masked)
		}
	}
}

// ---------------------------------------------------------------------------
// Domain interception
// ---------------------------------------------------------------------------

func TestListedDomainIsIntercepted(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	body := `{"content":"` + secret + `"}`
	mutated, count, err := svc.HandleRequest("dom1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 || strings.Contains(string(mutated), secret) {
		t.Fatalf("expected interception for api.anthropic.com, count=%d", count)
	}
}

func TestUnlistedDomainPassesThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	body := `{"content":"` + secret + `"}`
	mutated, count, err := svc.HandleRequest("dom2", "stripe.com", "/v1/charges", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 || string(mutated) != body {
		t.Fatalf("expected passthrough for stripe.com, count=%d", count)
	}
}

// ---------------------------------------------------------------------------
// PII behaviour
// ---------------------------------------------------------------------------

func TestPIIDisabledLeavesEmailUntouched(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	body := `{"content":"contact alice@example.com for support"}`
	mutated, count, err := svc.HandleRequest("pii1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 || !strings.Contains(string(mutated), "alice@example.com") {
		t.Fatalf("expected email untouched when PII disabled, count=%d body=%s", count, string(mutated))
	}
}

func TestPIIEnabledMasksEmail(t *testing.T) {
	svc := newService(t, true)
	defer svc.Close()
	body := `{"content":"contact alice@example.com for support"}`
	mutated, count, err := svc.HandleRequest("pii2", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 || strings.Contains(string(mutated), "alice@example.com") {
		t.Fatalf("expected email masked when PII enabled, count=%d body=%s", count, string(mutated))
	}
}

func TestPIIAndSecretCoexist(t *testing.T) {
	svc := newService(t, true)
	defer svc.Close()
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	body := `{"content":"contact alice@example.com using key ` + secret + `"}`
	mutated, count, err := svc.HandleRequest("pii3", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count < 2 {
		t.Fatalf("expected both PII and secret masked (count>=2), got %d\nbody: %s", count, string(mutated))
	}
	if strings.Contains(string(mutated), "alice@example.com") || strings.Contains(string(mutated), secret) {
		t.Fatalf("expected both values masked, got: %s", string(mutated))
	}
}

// ---------------------------------------------------------------------------
// Claude-shaped end-to-end scenario (full dotenv + encoded payload)
// ---------------------------------------------------------------------------

func TestClaudeScenarioDotenvAndEncodedPayload(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	connStr := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	accessKey := "AKIA1234567890ABCDEF"
	secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	sessionToken := "IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890"
	apiKey := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	ghToken := "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB"
	encodedConn := base64.StdEncoding.EncodeToString([]byte(connStr))
	urlConn := url.QueryEscape(connStr)

	prompt := strings.Join([]string{
		"Here is a mixed payload that should stay private:",
		"",
		"DATABASE_URL=" + connStr,
		"OPENAI_API_KEY=" + apiKey,
		"AUTH_TOKEN=" + ghToken,
		"AWS_ACCESS_KEY_ID=" + accessKey,
		"AWS_SECRET_ACCESS_KEY=" + secretKey,
		"AWS_SESSION_TOKEN=" + sessionToken,
		"",
		"base64_database_url=" + encodedConn,
		"urlencoded_database_url=" + urlConn,
	}, "\n")

	body := `{"messages":[{"role":"user","content":"` + strings.ReplaceAll(prompt, "\n", "\\n") + `"}]}`
	secrets := []string{connStr, accessKey, secretKey, sessionToken, apiKey, ghToken}
	masked := maskRequest(t, svc, "claude1", body, secrets)

	// Restore must recover original.
	restored, err := svc.HandleResponse("claude1", "api.anthropic.com", "/v1/messages", 200, http.Header{}, []byte(masked), "")
	if err != nil {
		t.Fatalf("HandleResponse: %v", err)
	}
	if string(restored) != body {
		t.Fatalf("round-trip failed for dotenv+encoded scenario")
	}
}

// ---------------------------------------------------------------------------
// SSE response — vault token restoration via streaming reader
// ---------------------------------------------------------------------------

func TestSSEResponseRestoresVaultTokens(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	body := `{"messages":[{"role":"user","content":"` + secret + `"}]}`

	masked, count, err := svc.HandleRequest("sse1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil || count == 0 {
		t.Fatalf("mask step: count=%d err=%v", count, err)
	}

	// Simulate SSE response: two events, the first echoes the masked body.
	event1 := "event: content_block_delta\ndata: " + string(masked)
	event2 := "event: message_stop\ndata: [DONE]"
	sseBody := event1 + "\n\n" + event2 + "\n\n"

	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	reader, err := svc.HandleSSEResponse("sse1", "api.anthropic.com", "/v1/messages", 200, headers, io.NopCloser(strings.NewReader(sseBody)), "")
	if err != nil {
		t.Fatalf("HandleSSEResponse: %v", err)
	}
	defer reader.Close()

	out, _ := io.ReadAll(reader)
	result := string(out)
	if strings.Contains(result, string(masked)) {
		t.Fatalf("masked token should have been restored in SSE output")
	}
	if !strings.Contains(result, secret) {
		t.Fatalf("original secret should appear in restored SSE output")
	}
}

// assertValidJSON fails the test if body is not valid JSON.
func assertValidJSON(t *testing.T, body string) {
	t.Helper()
	if !json.Valid([]byte(body)) {
		// Print a window around the first broken position for easier diagnosis.
		t.Fatalf("masked body is not valid JSON.\nbody (first 500 chars): %.500s", body)
	}
}

// maskRequestValidJSON calls maskRequest and additionally asserts the output is
// valid JSON. Use this everywhere the input is a JSON body.
func maskRequestValidJSON(t *testing.T, svc *agentruntime.Service, sid, body string, secrets []string) string {
	t.Helper()
	out := maskRequest(t, svc, sid, body, secrets)
	assertValidJSON(t, out)
	return out
}

// ---------------------------------------------------------------------------
// JSON validity — the primary regression guard for the `\[` bug
// ---------------------------------------------------------------------------

// TestMaskedBodyIsAlwaysValidJSON_QuotedValue is the direct regression test for
// the bug where ENV_SECRET_ASSIGNMENT matched up to the backslash of a JSON `\"`
// escape, producing a `\[` sequence that is an illegal JSON escape.
func TestMaskedBodyIsAlwaysValidJSON_QuotedValue(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// The .env value is surrounded by double-quotes; in the JSON body these
	// become `\"`. Without the fix the regex consumed the `\` and left `\[...`
	// in the output.
	body := `{"messages":[{"role":"user","content":"password=\"supersecretpassword123\""}]}`
	mutated, _, err := svc.HandleRequest("jv1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	assertValidJSON(t, string(mutated))
}

func TestMaskedBodyIsAlwaysValidJSON_MultiLineEnvInJSON(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Simulate Claude reading a .env file: newlines become \n in the JSON string.
	content := strings.Join([]string{
		`DB_PASSWORD=mypassword123456`,
		`API_SECRET=abcdefghijklmnopqrstuvwxyz`,
		`ACCESS_TOKEN=tok_abcdefghijklmnopqrst`,
	}, `\n`)
	body := `{"messages":[{"role":"user","content":"` + content + `"}]}`
	mutated, count, err := svc.HandleRequest("jv2", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected secrets to be masked, count=0")
	}
	assertValidJSON(t, string(mutated))
}

func TestMaskedBodyIsAlwaysValidJSON_LargeDotenv(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	// Build a realistic large .env file (40+ entries) similar to what a monorepo
	// might have. Embed it as a JSON string with \n separators, as Claude Code
	// would when reading the file and sending it to the API.
	entries := []string{
		`DB_HOST=localhost`,
		`DB_PORT=5432`,
		`DB_NAME=myapp`,
		`DB_PASSWORD=supersecretdbpassword123`,
		`DB_PASSWORD_REPLICA=replicapassword456`,
		`API_SECRET=my-api-secret-value-abcdef`,
		`SECRET_KEY=django-insecure-abcdefghijklmnopqrstuvwxyz1234567890`,
		`ACCESS_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB`,
		`GITHUB_TOKEN=ghp_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`,
		`AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`,
		`AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`,
		`AWS_SESSION_TOKEN=IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890`,
		`DATABASE_URL=postgresql://admin:SuperSecret123@db.internal.company.com:5432/proddb`,
		`REDIS_URL=redis://default:redispassword123@cache.internal:6379/0`,
		`MONGODB_URL=mongodb://user:mongopassword456@mongo.internal:27017/mydb`,
		`STRIPE_SECRET_KEY=sk_test_FAKEFAKEFAKEFAKEFAKEFAKEFAKE`,
		`STRIPE_WEBHOOK_SECRET=whsec_abcdefghijklmnopqrstuvwxyz`,
		`SENDGRID_API_KEY=SG.abcdefghijklmnopqrstuv.wabcdefghijklmnopqrstuvwxyz01234567890abcde`,
		`OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE`,
		`ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12`,
		`SMTP_PASSWORD=smtppassword123456`,
		`SMTP_HOST=smtp.example.com`,
		`JWT_SECRET=my-jwt-secret-key-abcdefghijklmnopqrstuvwxyz`,
		`OAUTH_CLIENT_SECRET=oauth-client-secret-abcdefghijklmnop`,
		`PRIVATE_KEY_PATH=/etc/ssl/private/server.key`,
		`SSL_PASSWORD=sslpassword123456`,
		`ENCRYPTION_KEY=encryption-key-abcdefghijklmnopqrstuvwxyz`,
		`SIGNING_SECRET=signing-secret-abcdefghijklmnopqrstuvwxyz`,
		`APP_SECRET=app-secret-token-abcdefghijklmnopqrstuvwxyz`,
		`SESSION_SECRET=session-secret-abcdefghijklmnopqrstuvwxyz`,
		`WEBHOOK_SECRET=webhook-secret-abcdefghijklmnopqrstuvwxyz`,
		`TWILIO_AUTH_TOKEN=twilio-auth-token-abcdefghijklmnopqrst`,
		`PUSHER_APP_SECRET=pusher-app-secret-abcdefghijklmnopqrstu`,
		`ALGOLIA_API_KEY=algolia-api-key-abcdefghijklmnopqrstuvwxyz`,
		`GOOGLE_API_KEY=AIzaSyAbcdefghijklmnopqrstuvwxyz12345678`,
		`FIREBASE_API_KEY=firebase-api-key-abcdefghijklmnopqrstuvwxyz`,
	}
	content := strings.Join(entries, `\n`)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":8096,"messages":[{"role":"user","content":"Please explain this .env file:\n` + content + `"}]}`

	mutated, count, err := svc.HandleRequest("jv3", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected secrets to be masked in large dotenv, count=0")
	}
	assertValidJSON(t, string(mutated))
	t.Logf("large dotenv: masked %d secrets", count)
}

func TestMaskedBodyIsAlwaysValidJSON_ValueWithEscapedQuote(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Value contains an embedded double-quote: in JSON this becomes \" which
	// is where the \[ bug previously occurred.
	body := `{"content":"api_key=longvalue\"withquote"}`
	mutated, _, err := svc.HandleRequest("jv4", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	assertValidJSON(t, string(mutated))
}

func TestMaskedBodyIsAlwaysValidJSON_ValueWithBackslash(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Value contains a Windows-style backslash path: in JSON \\ represents one \.
	// The mask must not leave a bare \ before [ in the output.
	body := `{"content":"password=C:\\\\Users\\\\admin\\\\secret123456"}`
	mutated, _, err := svc.HandleRequest("jv5", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	assertValidJSON(t, string(mutated))
}

func TestMaskedBodyIsAlwaysValidJSON_MultipleQuotedValues(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Multiple adjacent quoted values in a single JSON string.
	content := `DB_PASSWORD=\"pass1234567890\"\nAPI_SECRET=\"secretabcdefghij\"\nACCESS_TOKEN=\"tokenXYZABCDEFGHIJ\"`
	body := `{"content":"` + content + `"}`
	mutated, _, err := svc.HandleRequest("jv6", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	assertValidJSON(t, string(mutated))
}

func TestMaskedBodyIsAlwaysValidJSON_JSONValueWithSpecialChars(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// The full outer JSON body must remain decodable after masking.
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	body := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"key=` + secret + `"},{"role":"assistant","content":"I see a key in your message."}],"max_tokens":1024}`
	mutated, count, err := svc.HandleRequest("jv7", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected masking, count=0")
	}
	assertValidJSON(t, string(mutated))
	// All non-secret JSON fields must be preserved.
	for _, field := range []string{`"model"`, `"messages"`, `"role"`, `"max_tokens"`} {
		if !strings.Contains(string(mutated), field) {
			t.Fatalf("JSON field %q missing after masking", field)
		}
	}
}

// ---------------------------------------------------------------------------
// Masking correctness — patterns should detect secrets, not over-mask
// ---------------------------------------------------------------------------

func TestShortValueNotMasked(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Value shorter than 6 chars should not trigger ENV_SECRET_ASSIGNMENT.
	body := `{"content":"password=short"}`
	mutated, count, err := svc.HandleRequest("nm1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no masking for short value, count=%d, body=%s", count, string(mutated))
	}
}

func TestMaskPasswordKeywordCaseInsensitive(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	for _, keyword := range []string{"PASSWORD", "password", "Password", "PASSWD", "passwd"} {
		body := `{"content":"` + keyword + `=supersecretvalue123456"}`
		mutated, count, err := svc.HandleRequest("ci1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
		if err != nil {
			t.Fatalf("%s HandleRequest: %v", keyword, err)
		}
		if count == 0 {
			t.Fatalf("expected masking for keyword %q, count=0, body=%s", keyword, string(mutated))
		}
		assertValidJSON(t, string(mutated))
	}
}

func TestMaskAllENVKeywords(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	keywords := map[string]string{
		"password":     "supersecretvalue123456",
		"passwd":       "supersecretvalue123456",
		"secret":       "supersecretvalue123456",
		"api_key":      "supersecretvalue123456",
		"apikey":       "supersecretvalue123456",
		"access_token": "supersecretvalue123456",
		"auth_token":   "supersecretvalue123456",
		"private_key":  "supersecretvalue123456",
	}
	for kw, val := range keywords {
		body := `{"content":"` + kw + `=` + val + `"}`
		_, count, err := svc.HandleRequest("kw_"+kw, "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
		if err != nil {
			t.Fatalf("keyword %q: HandleRequest: %v", kw, err)
		}
		if count == 0 {
			t.Fatalf("keyword %q: expected masking, count=0", kw)
		}
	}
}

func TestMaskENVAssignmentWithColonSeparator(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	body := `{"content":"password: supersecretvalue123456"}`
	_, count, err := svc.HandleRequest("sep1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected masking for colon separator, count=0")
	}
}

// ---------------------------------------------------------------------------
// Vault round-trip correctness after the fix
// ---------------------------------------------------------------------------

func TestRoundTripEnvAssignmentInJSONString(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	body := `{"content":"db password=supersecretdbpassword12345 end"}`
	roundTrip(t, svc, "rt_env1", body, []string{"supersecretdbpassword12345"})
}

func TestRoundTripMultipleSecretsInJSONString(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	anthropicKey := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	openaiKey := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	connStr := "postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb"
	body := `{"messages":[{"role":"user","content":"anthropic=` + anthropicKey + ` openai=` + openaiKey + ` db=` + connStr + `"}]}`
	roundTrip(t, svc, "rt_multi1", body, []string{anthropicKey, openaiKey, connStr})
}

func TestRoundTripConnectionStringFollowedByQuote(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Connection string in a JSON string where the next char after the match is `"`.
	// The GENERIC_CONNECTION_STRING already excludes `\` so this should work.
	connStr := "postgres://admin:SuperSecret123@db.internal:5432/prod"
	body := `{"content":"url=\"` + connStr + `\""}`
	mutated, count, err := svc.HandleRequest("rt_conn1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected masking of connection string, count=0")
	}
	assertValidJSON(t, string(mutated))
	if strings.Contains(string(mutated), connStr) {
		t.Fatalf("connection string still present after masking")
	}
}

// ---------------------------------------------------------------------------
// Placeholder integrity
// ---------------------------------------------------------------------------

func TestPlaceholderDoesNotContainBackslash(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Inject a secret whose value has a backslash (Windows path style).
	// The placeholder must not contain \ before [ which would break JSON.
	body := `{"content":"password=C:\\\\Users\\\\secret12345"}`
	mutated, _, err := svc.HandleRequest("ph1", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	out := string(mutated)
	assertValidJSON(t, out)
	// The placeholder suffix must not have \[ anywhere in the output.
	if strings.Contains(out, `\[`) {
		t.Fatalf("found \\[ in masked output — invalid JSON escape: %s", out)
	}
}

func TestShapePreservingSurrogateIsPresent(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	body := `{"content":"` + secret + `"}`
	mutated, count, err := svc.HandleRequest("ph2", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil || count == 0 {
		t.Fatalf("expected masking: count=%d err=%v", count, err)
	}
	out := string(mutated)
	if strings.Contains(out, "[ANTHROPIC_API_KEY:") || strings.Contains(out, "****") {
		t.Fatalf("old visible mask marker found in output: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "dummy") || strings.Contains(strings.ToLower(out), "mask") {
		t.Fatalf("surrogate should not use dummy/mask labels: %s", out)
	}
	if !strings.Contains(out, "sk-ant-api03-") {
		t.Fatalf("expected provider prefix anchor in output: %s", out)
	}
	if len(out) != len(body) {
		t.Fatalf("expected same-length surrogate body, before=%d after=%d body=%s", len(body), len(out), out)
	}
}

// ---------------------------------------------------------------------------
// Edge cases specific to the dotenv-reading workflow
// ---------------------------------------------------------------------------

func TestDotenvReadWorkflow_SecretNotLeakedAfterMask(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	// Simulate exactly what Claude Code does when a user asks it to read and
	// explain a .env file: the file content is embedded as a JSON string value
	// with literal \n separators.
	fileContent := strings.Join([]string{
		"# Database",
		"DB_HOST=localhost",
		"DB_PASSWORD=productiondbpassword123",
		"DATABASE_URL=postgresql://admin:dbsecret456@db.prod.internal:5432/appdb",
		"",
		"# AWS",
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"",
		"# API keys",
		"OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE",
		"ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12",
		"GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
	}, `\n`)

	prompt := `can you explain this .env file\n\n` + fileContent
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":8096,"messages":[{"role":"user","content":"` + prompt + `"}]}`

	secrets := []string{
		"productiondbpassword123",
		"dbsecret456",
		"postgresql://admin:dbsecret456@db.prod.internal:5432/appdb",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE",
		"sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12",
		"ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
	}

	masked := maskRequestValidJSON(t, svc, "dotenv1", body, secrets)
	t.Logf("dotenv workflow: body length %d → masked length %d", len(body), len(masked))
}

func TestDotenvReadWorkflow_400ErrorNotProduced(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()

	// This test reproduces the exact failure mode seen in production:
	// Claude reads a .env file, proxy masks secrets, but the resulting body
	// is invalid JSON so Anthropic returns 400 "invalid escaped character".
	//
	// The failure was: ENV_SECRET_ASSIGNMENT matched past `\"` in JSON,
	// the suffix ended with `\`, producing `\[` — an illegal escape.

	problematicEntries := []string{
		`DB_PASSWORD=mypassword123456`,
		`GENERIC_CONNECTION_STRING=mongodb://user:mongopass@localhost/mydb`,
		`API_SECRET=abcdefghijklmnopqrstuvwxyz`,
		`AWS_SESSION_TOKEN=IQoJb3JpZ2luX2VjEA4aCXVzLWVhc3QtMSJIMEYCIQDcTokenValue1234567890ABCDE`,
	}
	content := strings.Join(problematicEntries, `\n`)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":8096,"messages":[{"role":"user","content":"` + content + `"}]}`

	mutated, count, err := svc.HandleRequest("dotenv400", "api.anthropic.com", "/v1/messages", http.MethodPost, http.Header{}, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest error: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected secrets to be masked, count=0")
	}

	out := string(mutated)
	assertValidJSON(t, out)

	// Explicitly check for the `\[` pattern that caused the 400 error.
	if strings.Contains(out, `\[`) {
		t.Fatalf("found \\[ (invalid JSON escape) in masked output:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// WebSocket masking
// ---------------------------------------------------------------------------

// TestHandleWebSocket_SecretMaskedInClientFrame verifies that a secret in a
// client-to-server WebSocket frame is masked and the result does not contain
// the original secret.
func TestHandleWebSocket_SecretMaskedInClientFrame(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	frame := `{"type":"conversation.item.create","item":{"content":"key is ` + secret + `"}}`
	maskWebSocketFrame(t, svc, "ws1", "api.anthropic.com", frame, []string{secret})
}

// TestHandleWebSocket_FrameRemainsValidAfterMask verifies that after masking,
// the frame text does not contain \[ (the invalid-JSON-escape bug).
func TestHandleWebSocket_FrameRemainsValidAfterMask(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// password=\"value\" triggers the ENV_SECRET_ASSIGNMENT pattern; before the
	// fix the masked output contained \[ which is an illegal JSON escape.
	frame := `{"content":"password=\"supersecretpassword123\""}`
	masked, _ := svc.HandleWebSocket("ws2", "api.anthropic.com", "/backend-api/codex/responses", frame, true, "")
	if strings.Contains(masked, `\[`) {
		t.Fatalf("found \\[ (invalid JSON escape) in masked WebSocket frame:\n%s", masked)
	}
}

// TestHandleWebSocket_ServerFrameRestoresVaultToken verifies that a
// server-to-client frame containing a vault token (from a previous masked
// request in the same session) has the token restored.
func TestHandleWebSocket_ServerFrameRestoresVaultToken(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	frame := `{"content":"your key is ` + secret + `"}`
	roundTripWebSocket(t, svc, "ws3", "api.anthropic.com", frame, []string{secret})
}

// TestHandleWebSocket_MultiFrameSequence verifies that three consecutive
// client frames are each independently masked.
func TestHandleWebSocket_MultiFrameSequence(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secrets := []string{
		"sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12",
		"ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
		"postgres://admin:SuperSecret123@db.internal.company.com:5432/proddb",
	}
	for i, secret := range secrets {
		frame := `{"seq":` + strings.Repeat("0", i) + `,"content":"` + secret + `"}`
		sid := "ws4_" + strings.Repeat("x", i)
		maskWebSocketFrame(t, svc, sid, "api.anthropic.com", frame, []string{secret})
	}
}

// TestHandleWebSocket_BinaryLikeContentPassedThrough verifies that a call to
// HandleWebSocket with non-secret content is not masked (count == 0).
// Note: actual binary frame filtering is in proxy/server.go (opcode check);
// this test covers the service layer only passing through non-matching content.
func TestHandleWebSocket_BinaryLikeContentPassedThrough(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	// Arbitrary non-secret binary-like payload (no pattern will match).
	frame := "\x00\x01\x02\x03\x04\x05\x06\x07hello world"
	result, count := svc.HandleWebSocket("ws5", "api.anthropic.com", "/backend-api/codex/responses", frame, true, "")
	if count != 0 {
		t.Fatalf("expected count=0 for non-secret frame, got %d", count)
	}
	if result != frame {
		t.Fatalf("expected frame unchanged for non-secret content")
	}
}

// TestHandleWebSocket_LargeFrame verifies no panic or truncation for a
// frame with a 128KB payload.
func TestHandleWebSocket_LargeFrame(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	// Build a 128KB+ frame by padding with harmless text around the secret.
	padding := strings.Repeat("A", 64*1024)
	frame := `{"content":"` + padding + secret + padding + `"}`
	masked, count := svc.HandleWebSocket("ws6", "api.anthropic.com", "/backend-api/codex/responses", frame, true, "")
	if count == 0 {
		t.Fatalf("expected secret masked in large frame, got count=0")
	}
	if strings.Contains(masked, secret) {
		t.Fatalf("secret still present in large masked frame")
	}
	// No truncation: output must still contain both padding blocks.
	if !strings.Contains(masked, padding[:100]) {
		t.Fatalf("frame appears truncated after masking")
	}
}

// TestHandleWebSocket_ChatGPTDomainIntercepted verifies ShouldIntercept returns
// true for chatgpt.com (the Codex WebSocket domain).
func TestHandleWebSocket_ChatGPTDomainIntercepted(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	if !svc.ShouldIntercept("chatgpt.com") {
		t.Fatalf("expected chatgpt.com to be in the intercepted domain set")
	}
	// Also verify that a secret in a frame sent to chatgpt.com is masked.
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"
	frame := `{"content":"` + secret + `"}`
	masked, count := svc.HandleWebSocket("ws7", "chatgpt.com", "/backend-api/codex/responses", frame, true, "")
	if count == 0 || strings.Contains(masked, secret) {
		t.Fatalf("expected secret masked for chatgpt.com, count=%d", count)
	}
}

// TestHandleWebSocket_SecretWithEscapedQuoteInFrame verifies that a frame
// containing password=\"value\" (JSON-style quoted assignment) combined with
// a detectable API key does not produce \[ in the masked output, and the
// API key is properly masked.
func TestHandleWebSocket_SecretWithEscapedQuoteInFrame(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	apiKey := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	// The quoted assignment (DB_PASSWORD=\"...\") exercises the regex boundary
	// where the \[ bug previously occurred; the API key ensures masking fires.
	frame := `{"content":"DB_PASSWORD=\"supersecretpassword123\" ANTHROPIC_API_KEY=\"` + apiKey + `\""}`
	masked, count := svc.HandleWebSocket("ws8", "api.anthropic.com", "/backend-api/codex/responses", frame, true, "")
	if count == 0 {
		t.Fatalf("expected API key masked in frame, count=0\nframe: %s", frame)
	}
	if strings.Contains(masked, `\[`) {
		t.Fatalf("found \\[ (invalid JSON escape) in masked WebSocket frame:\n%s", masked)
	}
	if strings.Contains(masked, apiKey) {
		t.Fatalf("API key still present after masking: %s", masked)
	}
}

// TestHandleWebSocket_NoSecretNoMasking verifies that a clean frame (no
// secrets) is returned unchanged with maskedCount == 0.
func TestHandleWebSocket_NoSecretNoMasking(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	frame := `{"type":"ping","timestamp":1234567890}`
	result, count := svc.HandleWebSocket("ws9", "api.anthropic.com", "/backend-api/codex/responses", frame, true, "")
	if count != 0 {
		t.Fatalf("expected count=0 for frame without secrets, got %d", count)
	}
	if result != frame {
		t.Fatalf("expected frame unchanged when no secrets present\nwant: %s\ngot:  %s", frame, result)
	}
}

// TestHandleWebSocket_SessionIsolation verifies that vault tokens from one
// session are not visible in another session's restore path.
func TestHandleWebSocket_SessionIsolation(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secretA := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	secretB := "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890ABCDE"

	frameA := `{"content":"` + secretA + `"}`
	frameB := `{"content":"` + secretB + `"}`

	// Mask in session A.
	maskedA, countA := svc.HandleWebSocket("wsIsoA", "api.anthropic.com", "/backend-api/codex/responses", frameA, true, "")
	if countA == 0 || strings.Contains(maskedA, secretA) {
		t.Fatalf("session A: expected secretA masked, count=%d", countA)
	}

	// Mask in session B.
	maskedB, countB := svc.HandleWebSocket("wsIsoB", "api.anthropic.com", "/backend-api/codex/responses", frameB, true, "")
	if countB == 0 || strings.Contains(maskedB, secretB) {
		t.Fatalf("session B: expected secretB masked, count=%d", countB)
	}

	// Restoring session A's masked token via session B must NOT recover secretA.
	restoredInB, _ := svc.HandleWebSocket("wsIsoB", "api.anthropic.com", "/backend-api/codex/responses", maskedA, false, "")
	if strings.Contains(restoredInB, secretA) {
		t.Fatalf("session isolation violated: secretA was restored in session B\nresult: %s", restoredInB)
	}
}

// ---------------------------------------------------------------------------
// JSON-aware masking (pass-2 decode)
// ---------------------------------------------------------------------------

// maskRequestJSON sends body through HandleRequest with application/json
// Content-Type header, asserts count>0 and that secrets are absent.
func maskRequestJSON(t *testing.T, svc *agentruntime.Service, sid, body string, secrets []string) string {
	t.Helper()
	headers := http.Header{"Content-Type": []string{"application/json"}}
	mutated, count, err := svc.HandleRequest(sid, "api.anthropic.com", "/v1/messages", http.MethodPost, headers, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one masked secret, got 0\nbody: %s", body)
	}
	out := string(mutated)
	for _, secret := range secrets {
		if strings.Contains(out, secret) {
			t.Fatalf("secret still present after masking: %q\nmasked body: %s", secret[:min(len(secret), 60)], out)
		}
	}
	return out
}

// TestJSONAwareMasking_QuotedPasswordMissedByPass1 verifies that a secret
// inside a JSON-escaped quoted value (password=\"secretvalue123456\") is masked
// by pass-2 (JSON decode walk) and the resulting body is valid JSON.
func TestJSONAwareMasking_QuotedPasswordMissedByPass1(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "secretvalue123456"
	// The raw JSON contains password=\"secretvalue123456\" — the backslash before
	// the quote can stop the raw-byte regex from matching the full value.
	body := `{"messages":[{"role":"user","content":"password=\"` + secret + `\""}]}`
	out := maskRequestJSON(t, svc, "jq1", body, []string{secret})
	assertValidJSON(t, out)
}

// TestJSONAwareMasking_CleanRequestNoJSONParsing verifies that a request with
// no secrets produces count==0 and an unchanged body (no JSON parsing overhead).
func TestJSONAwareMasking_CleanRequestNoJSONParsing(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	body := `{"messages":[{"role":"user","content":"hello world, no secrets here"}]}`
	headers := http.Header{"Content-Type": []string{"application/json"}}
	mutated, count, err := svc.HandleRequest("jq2", "api.anthropic.com", "/v1/messages", http.MethodPost, headers, []byte(body), "")
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected count==0 for clean request, got %d", count)
	}
	if string(mutated) != body {
		t.Fatalf("clean body should be returned unchanged\nwant: %s\ngot:  %s", body, string(mutated))
	}
}

// TestJSONAwareMasking_NestedJSONObject verifies that a secret inside a deeply
// nested JSON value is found and masked by pass-2.
func TestJSONAwareMasking_NestedJSONObject(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "abc123456"
	body := `{"messages":[{"content":{"text":"password=` + secret + `"}}]}`
	out := maskRequestJSON(t, svc, "jq3", body, []string{secret})
	assertValidJSON(t, out)
}

// TestJSONAwareMasking_ArrayOfMessages verifies that a secret in the second
// element of a JSON array is found and masked.
func TestJSONAwareMasking_ArrayOfMessages(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	secret := "secretvalue123456"
	body := `{"messages":[{"role":"user","content":"first message no secrets"},{"role":"user","content":"password=\"` + secret + `\""}]}`
	out := maskRequestJSON(t, svc, "jq4", body, []string{secret})
	assertValidJSON(t, out)
}

// TestJSONAwareMasking_Pass1AndPass2Combined verifies that a body with one
// plain secret (caught by pass-1) and one JSON-quoted secret (caught by
// pass-2 only) both get masked.
func TestJSONAwareMasking_Pass1AndPass2Combined(t *testing.T) {
	svc := newService(t, false)
	defer svc.Close()
	plainSecret := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz12"
	quotedSecret := "secretvalue123456"
	body := `{"messages":[{"role":"user","content":"key is ` + plainSecret + ` and password=\"` + quotedSecret + `\""}]}`
	out := maskRequestJSON(t, svc, "jq5", body, []string{plainSecret, quotedSecret})
	assertValidJSON(t, out)
}
