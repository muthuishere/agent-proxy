# Spec: GitHub Copilot Fix (TLS Handshake Failures)

**Status:** Partially resolved (2026-04-25) — see Status section below.
**Priority:** P0 (Copilot non-functional through proxy)

---

## Status (2026-04-25)

Resolved as part of Slice 05:

- **Domain scope corrected.** `api.github.com` removed from
  `detection.intercepted_domains` in `config/agentproxy.yaml` and from
  `CopilotProvider.Domains()` in `internal/provider/copilot.go`. Bare GitHub
  REST traffic (`gh repo view`, `gh pr create`, etc.) now passes through
  unmodified. Covered by `TestCopilotApiGitHubComPassesThrough` in
  `tests/go/copilot_shapes_test.go` and the registry case in
  `internal/provider/provider_test.go`.
- **Masking pipeline correctness on `api.githubcopilot.com`** verified end-to-end
  (request mask + response restore + SSE round-trip) via
  `TestCopilotSuggestRoundTrip` and `TestCopilotChatStreamRoundTrip`. These
  use synthetic OpenAI-chat-shaped fixtures since no real Copilot capture
  is in-tree yet.
- **Conditional MITM** (Slice 04 P0) means non-Copilot traffic from a shell
  without `HTTP_PROXY` set is no longer intercepted at all, so the
  "non-disruption" risk for unrelated `gh` commands is bounded by the
  domain list above.

Still open (need real Copilot traffic to validate or refute):

- TLS handshake failures against `api.individual.githubcopilot.com` (the
  `EOF` / `connection reset by peer` symptoms in the original report).
  Without a real `gh copilot` session through the proxy we cannot tell
  whether the residual issue is cert pinning, ALPN mismatch, or VS Code
  extension env-var inheritance. Fix candidates A–D below remain valid;
  pick one once a capture exists.
- ALPN audit on the MITM listener (Fix C) — not yet verified to advertise
  `h2`.
- VS Code extension proxy settings documentation (Fix A).

Follow-up: capture a real `gh copilot suggest` / `gh copilot explain`
session against `api.githubcopilot.com` (Slice 05 task 1 / 12) and re-open
this spec with concrete data.

---

## Problem

Live log shows consistent TLS handshake failures for Copilot domains:

```
WARN: Cannot handshake client api.individual.githubcopilot.com:443 EOF
WARN: Cannot handshake client api.individual.githubcopilot.com:443 EOF
WARN: Cannot read request from mitm'd client api.individual.githubcopilot.com:443
       read: connection reset by peer
```

Copilot traffic is intercepted (domain is in the allowlist) but the TLS MITM handshake with the Copilot client is failing before any request is read.

---

## Hypothesis: Why Copilot Fails While Claude Works

Claude Code is a Node.js application. It respects `NODE_EXTRA_CA_CERTS` and `SSL_CERT_FILE`. The proxy's CA cert is added via these env vars by `bin/claudeproxy`.

GitHub Copilot (VS Code extension, `gh copilot` CLI) may:
1. **Use its own certificate store** — VS Code extensions often bundle their own Node.js and may not inherit `NODE_EXTRA_CA_CERTS` from the shell
2. **Use certificate pinning** — Copilot may pin the expected certificate chain for `api.individual.githubcopilot.com` and reject our MITM cert
3. **Send a ClientHello with specific TLS extensions** that our per-host cert generation does not satisfy (e.g., specific ALPN protocols, SNI mismatch)
4. **Reset the connection immediately** after receiving our generated certificate, before completing the handshake

The `EOF` error on handshake vs `connection reset by peer` suggests different failure modes from different Copilot clients/versions.

---

## Investigation Steps (Before Coding)

These must be completed before implementing a fix, as the root cause determines the solution.

### Step 1: Capture TLS ClientHello

Add verbose TLS logging to goproxywss to capture what the Copilot client sends:
- ALPN protocols requested
- TLS version (1.2 vs 1.3)
- SNI hostname
- Any certificate request extensions

### Step 2: Check if cert pinning is in play

```bash
# Launch copilotproxy, connect VS Code to a repo, trigger Copilot suggestion
# Then check:
openssl s_client -connect api.individual.githubcopilot.com:443 -servername api.individual.githubcopilot.com
# Compare the real cert's Subject Alternative Names with what our CA generates
```

If Copilot checks a specific cert fingerprint or uses HPKP-style pinning, MITM is not possible without patching the client binary.

### Step 3: Test with `gh copilot` CLI (not VS Code)

`gh copilot explain "..."` is easier to test than VS Code. Run it under `bin/copilotproxy` and observe whether it fails the same way.

### Step 4: Check VS Code extension proxy settings

VS Code has its own proxy configuration (`http.proxyStrictSSL`, `http.proxy`). The proxy env vars set by `bin/copilotproxy` may not reach the extension host process.

---

## Likely Fixes Based on Investigation Outcome

### Fix A: VS Code extension does not inherit env vars

VS Code extensions run in an extension host subprocess. `HTTP_PROXY` and `NODE_EXTRA_CA_CERTS` set in the terminal may not reach the extension host.

**Fix:** Document that users must set proxy settings in VS Code settings:
```json
// .vscode/settings.json or User Settings
{
  "http.proxy": "http://127.0.0.1:7717",
  "http.proxyStrictSSL": false
}
```

`"http.proxyStrictSSL": false` is a stopgap. Better: add the CA cert to VS Code's trusted certificates via `http.proxySupport` and the system keychain (already done by `agentproxy ca-setup --trust`).

### Fix B: Certificate generation missing SANs or extensions

The goproxywss cert generator (`certs.go`) may generate leaf certs that are missing Subject Alternative Names that Copilot clients require.

**Fix:** In `third_party/goproxywss/certs.go`, ensure generated certs include:
- `SubjectAltName` with the target hostname
- `ExtendedKeyUsage: serverAuth`
- `KeyUsage: digitalSignature | keyEncipherment`

Current Go x509 cert generation should include these but verify against what Copilot actually requires.

### Fix C: ALPN protocol mismatch

If Copilot requests `h2` via ALPN and our TLS server in goproxywss only advertises `http/1.1`, the handshake may fail.

**Fix:** In goproxywss TLS config for the MITM listener, advertise both `h2` and `http/1.1` in `NextProtos`. Then handle the negotiated protocol appropriately for the upstream connection.

### Fix D: Certificate pinning (worst case)

If Copilot pins its certificates, we cannot MITM the TLS connection.

**Options:**
- Operate at the HTTP layer only (pass TLS through, no body inspection for Copilot)
- Document as unsupported
- Use a Copilot-specific proxy approach (intercept at the GitHub token level)

---

## Scope of Change (after investigation confirms Fix B or C)

### `third_party/goproxywss/certs.go`
- Audit generated cert structure vs RFC requirements
- Add missing extensions if any

### `third_party/goproxywss/https.go`
- Ensure MITM TLS listener advertises correct ALPN protocols

### `cmd/agentproxy/casetup.go`
- Verify `ca-setup --trust` correctly installs cert in all relevant stores (system keychain, NSS/Firefox db, VS Code)

### `bin/copilotproxy`
- Add VS Code-specific proxy env vars if `code` or `code-insiders` binary is detected

---

## Test Plan

### Manual E2E
```bash
# Terminal test
bin/copilotproxy gh copilot explain "what does this function do"
# Expected: completes without TLS errors, traffic appears in dashboard

# VS Code test (after setting http.proxy in settings)
# Trigger a Copilot suggestion in VS Code with proxy running
# Expected: suggestion loads, dashboard shows intercepted request to api.individual.githubcopilot.com
```

### Unit tests
- `TestCopilotCertHasSAN` — generated cert for `api.individual.githubcopilot.com` includes correct SAN
- `TestCopilotCertHasServerAuthEKU` — cert has serverAuth extended key usage
- `TestMITMListenerAdvertisesH2` — TLS config includes `h2` in NextProtos

---

## Acceptance Criteria

- [ ] Root cause identified (Investigation steps completed)
- [ ] `bin/copilotproxy gh copilot explain "..."` completes without TLS errors
- [ ] Traffic from Copilot CLI appears in the proxy dashboard
- [ ] Secrets in Copilot requests are masked
- [ ] All existing 45 tests pass
- [ ] New cert/TLS tests pass
