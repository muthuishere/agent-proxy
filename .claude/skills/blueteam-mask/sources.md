# Community-sourced regex catalogs

Every regex that ships in `config/patterns.yaml` or `config/pii_patterns.yaml` via the blueteam-mask skill must trace back to one of these sources. Cite the URL in a YAML comment. No LLM-invented patterns. No "I think the format is…".

Order is preferred-source-first. Stop at the first catalog that has a clean rule for the token in question.

## Tier 1 — primary sources

### Gitleaks
- **URL:** https://github.com/gitleaks/gitleaks
- **Rules file:** `config/gitleaks.toml`
- **Why first:** comprehensive, well-curated, MIT-licensed, the de-facto reference for secret scanners.
- **How to lookup:** clone or `curl -sS https://raw.githubusercontent.com/gitleaks/gitleaks/master/config/gitleaks.toml | grep -i <vendor> -A 5`
- **Citation format:** `# source: https://github.com/gitleaks/gitleaks/blob/master/config/gitleaks.toml#L<line> — fetched YYYY-MM-DD`

### TruffleHog
- **URL:** https://github.com/trufflesecurity/trufflehog
- **Detectors:** `pkg/detectors/<vendor>/<vendor>.go` — each detector file has the regex constant near the top.
- **Why second:** more vendor coverage than Gitleaks for niche APIs; validates by hitting the real API (we only borrow the regex, not the validator).
- **Lookup:** `gh search code --repo trufflesecurity/trufflehog --filename "<vendor>.go" 'regexp.MustCompile'`
- **Citation format:** `# source: https://github.com/trufflesecurity/trufflehog/blob/main/pkg/detectors/<vendor>/<vendor>.go — fetched YYYY-MM-DD`

### secretlint
- **URL:** https://github.com/secretlint/secretlint
- **Rules dir:** `packages/@secretlint/secretlint-rule-*`
- **Why third:** strong on JS/TS-flavoured tokens (npm, Vercel, Stripe restricted keys) and PII.
- **Citation format:** `# source: https://github.com/secretlint/secretlint/tree/master/packages/@secretlint/secretlint-rule-<rule> — fetched YYYY-MM-DD`

## Tier 2 — PII catalogs

### Microsoft Presidio
- **URL:** https://github.com/microsoft/presidio
- **Recognizers:** `presidio-analyzer/presidio_analyzer/predefined_recognizers/`
- **Why:** strongest open catalog for PII (SSN, IBAN, credit cards, phone numbers by country, IPv4/IPv6, email).
- **Note:** Presidio has ML-backed recognizers — **ignore those**. Only use regex-based ones.
- **Citation:** `# source: https://github.com/microsoft/presidio/blob/main/presidio-analyzer/presidio_analyzer/predefined_recognizers/<file>.py — fetched YYYY-MM-DD`

### libpostal / commonregex
- **commonregex (Python):** https://github.com/madisonmay/CommonRegex
- **Use for:** US-centric PII fallback (phone, SSN, date).
- **Limit:** US-only. For non-US, prefer Presidio.

## Tier 3 — vendor-published

Some vendors publish their own token format:

- **GitHub:** https://github.blog/2021-04-05-behind-githubs-new-authentication-token-formats/
- **OpenAI:** https://help.openai.com/en/articles/4936850-where-do-i-find-my-openai-api-key
- **Stripe:** https://stripe.com/docs/keys
- **Anthropic:** https://docs.anthropic.com/en/api/getting-started — token prefix and length documented.

When Tier 1 lacks a rule and the vendor publishes its format, cite the vendor doc directly. Prefer vendor doc over inventing.

## What to do when no catalog has the rule

1. Mark the finding `deferred` with `reason: no-community-source` and the vendor name.
2. Open an issue in this repo titled `pattern-request: <vendor>` describing the leak case and what the token looks like (shape only — never paste a real one).
3. Wait for a community PR upstream (Gitleaks accepts new rules quickly). Re-run blueteam-mask once it lands.

Do **not** ship an invented regex even if it "looks right". Bad regex either over-masks (breaks legit calls) or under-masks (leaks). Both worse than the gap itself.

## Refresh cadence

Catalogs evolve. Re-fetch the sources you cite at least monthly. When the upstream regex changes, sync the local copy and bump the `# fetched` date in the comment. A staleness check is a good `task` target to add later: `task patterns-source-refresh`.
