# Spec: Pattern Matchers + Default Secret Coverage

**Status:** Planned  
**Priority:** High  
**Depends on:** Existing scanner + `config/patterns.yaml`

---

## Problem

The current pattern system is regex-only. That works for many single-line tokens, but it is awkward for two important classes of secrets:

1. **Prefix-based tokens** such as `sk-`, `ghp_`, `hf_`, `xoxb-`, where a simple prefix match is easier to read and cheaper to evaluate than a full regex.
2. **Multiline secret blocks** such as SSH private keys and PEM private keys, where a block start/end match is clearer and safer than a large multiline regex.

At the same time, the default pattern set should cover the high-value secrets users expect AgentProxy to catch out of the box:

- AI/LLM provider keys
- Cloud credentials
- Git/dev platform tokens
- JWTs and bearer-style tokens
- Database/connection URLs
- SSH/private key blocks
- Common `.env` secret assignments

The goal is not "catch all sensitive data." The goal is "catch the common secret formats that should never be sent upstream by default."

---

## Goals

- Support a small matcher model that is easy to understand and validate.
- Improve readability and maintainability of `config/patterns.yaml`.
- Cover the most common real-world secret formats in the default config.
- Keep startup validation strict: invalid pattern config must fail fast.
- Preserve existing scanner performance characteristics as much as possible.

---

## Non-Goals

- Generic high-entropy secret detection
- PII detection beyond the separate PII config
- Arbitrary boolean matcher expressions
- User-defined scripting or external validators
- Every SaaS provider on the internet

---

## Matcher Model

Support exactly four matcher types:

### 1. `regex`

For structured formats with a stable syntax.

Examples:
- AWS access key IDs
- JWTs
- database URLs
- env assignment patterns

### 2. `prefix`

For token families with a stable starting marker.

Examples:
- `sk-`
- `ghp_`
- `hf_`
- `xoxb-`

`prefix` is line-bounded by definition:

- match begins at the configured prefix
- match ends at the next newline boundary or end of string

In JSON bodies, this must be applied to decoded string values, not raw escaped JSON bytes.

### 3. `contains`

For literal string markers found anywhere in a payload.

Examples:
- exact env key labels
- webhook markers
- special credential labels

This should be used sparingly in the default set, because broad substring matching can overmatch.

### 4. `contains_between`

For multiline block secrets. Match from `block_start` through `block_end`, inclusive.

Examples:
- `-----BEGIN OPENSSH PRIVATE KEY----- ... -----END OPENSSH PRIVATE KEY-----`
- `-----BEGIN RSA PRIVATE KEY----- ... -----END RSA PRIVATE KEY-----`
- `-----BEGIN PRIVATE KEY----- ... -----END PRIVATE KEY-----`

`contains_between` is multiline by definition. No extra `multiline` flag is needed in v1.

---

## YAML Schema

Each pattern uses exactly one matcher type.

### `regex`

```yaml
- name: AWS_ACCESS_KEY
  type: regex
  pattern: '\b(?:AKIA|ASIA)[0-9A-Z]{16}\b'
  category: cloud
  confidence: certain
```

### `prefix`

```yaml
- name: OPENAI_API_KEY
  type: prefix
  value: 'sk-'
  category: ai
  confidence: medium
```

### `contains`

```yaml
- name: OPENAI_ENV_ASSIGNMENT
  type: contains
  value: 'OPENAI_API_KEY='
  category: env
  confidence: high
```

### `contains_between`

```yaml
- name: OPENSSH_PRIVATE_KEY
  type: contains_between
  block_start: '-----BEGIN OPENSSH PRIVATE KEY-----'
  block_end: '-----END OPENSSH PRIVATE KEY-----'
  category: key
  confidence: certain
```

### Common fields

Keep these fields:

- `name`
- `type`
- matcher-specific field(s)
- `category`
- `confidence`

Remove matcher-specific ambiguity from the current file shape. Do not allow both `regex:` and `type: prefix` in the same entry.

---

## Validation Rules

Startup validation must fail if any pattern is invalid.

### Required

- Every pattern must have `name`
- `type` must be one of:
  - `regex`
  - `prefix`
  - `contains`
  - `contains_between`

### Matcher-specific

- `regex`
  - requires non-empty `pattern`
  - must compile successfully at startup
- `prefix`
  - requires non-empty `value`
- `contains`
  - requires non-empty `value`
- `contains_between`
  - requires non-empty `block_start`
  - requires non-empty `block_end`
  - `block_start` and `block_end` must not be identical

### Reject invalid shapes

- unknown `type`
- missing matcher field
- incompatible extra matcher fields for the chosen type
- duplicate pattern names
- empty strings for required fields

Validation errors should include:
- config file path
- pattern name
- reason

Example:

```text
config/patterns.yaml: pattern OPENSSH_PRIVATE_KEY: contains_between requires block_end
```

---

## Default Coverage Inventory

The default pattern set should cover these categories.

### AI / LLM keys

- Anthropic API keys
- OpenAI API keys
- Google API / Gemini-style keys
- Hugging Face tokens
- Cohere API keys
- Mistral API keys
- Groq API keys
- OpenRouter API keys
- Together API keys
- Replicate API keys

### Cloud credentials

- AWS access key IDs
- AWS secret access key assignments
- AWS session token assignments
- GCP API keys
- GCP service account private key markers
- Azure key/token assignments
- Cloudflare API tokens

### Git / developer platform secrets

- GitHub PATs
- GitLab PATs
- npm tokens
- Stripe secret keys
- SendGrid keys
- Slack bot tokens
- Slack webhook URLs

### Auth / session material

- JWTs
- bearer token assignments
- access token assignments
- refresh token assignments

### Database / connection strings

- Postgres URLs
- MySQL URLs
- MariaDB URLs
- MongoDB URLs
- Redis URLs
- AMQP / RabbitMQ URLs

### Key blocks

- OpenSSH private key blocks
- RSA private key blocks
- EC private key blocks
- PKCS8 private key blocks
- generic PEM private key blocks

### `.env` / config assignments

- `*_API_KEY=`
- `*_SECRET=`
- `*_TOKEN=`
- `*_PASSWORD=`
- `DATABASE_URL=`
- `ACCESS_KEY=`
- `PRIVATE_KEY=`

---

## Default Matcher Guidance

Use these rules when defining defaults:

- Use `contains_between` for multiline key blocks.
- Use `prefix` for token families when the stable prefix is strong and low-noise.
- Use `regex` for precise formats or assignment patterns.
- Use `contains` only for narrow literal markers that are unlikely to overmatch.

Examples:

| Pattern | Matcher |
|---|---|
| OpenSSH private key | `contains_between` |
| RSA private key | `contains_between` |
| JWT | `regex` |
| AWS access key ID | `regex` |
| OpenAI key family | `prefix` or `regex` |
| GitHub PAT | `prefix` or `regex` |
| `.env` secret assignment | `regex` |

---

## Scanner Behavior

No change to masking semantics:

- matched secrets are replaced with the existing vault placeholder flow
- restored on response path as today

Matcher-specific behavior:

- `regex`: current behavior
- `prefix`: find candidate spans beginning with `value`; mask from the prefix through the next newline boundary or end of string
- `contains`: mask the configured matched literal span
- `contains_between`: mask the full block from `block_start` to `block_end`, inclusive

Important:

- for JSON request bodies, matcher evaluation should operate on decoded string values wherever the current provider/body path already performs JSON-aware scanning
- `contains_between` must not stop at newline boundaries
- if `block_start` is found without `block_end`, do not panic; skip masking that partial block

---

## Scope

### In scope

- Extend pattern config loader to support matcher-specific schema
- Extend scanner to support `prefix`, `contains`, and `contains_between`
- Add strict startup validation for pattern config
- Migrate default `config/patterns.yaml` to the new schema
- Expand default patterns to cover the categories listed above
- Add unit tests for matcher parsing, validation, and scanning

### Out of scope

- PII config migration
- entropy heuristics
- dashboard pattern editing
- remote pattern downloads

---

## Test Plan

### Config validation

- valid `regex` pattern loads successfully
- invalid `regex` fails startup
- `prefix` without `value` fails startup
- `contains` without `value` fails startup
- `contains_between` without `block_start` or `block_end` fails startup
- duplicate pattern names fail startup

### Scanner behavior

- `prefix` masks a token beginning with the configured prefix
- `contains` masks the configured literal
- `contains_between` masks a full multiline SSH key block
- `contains_between` ignores partial blocks missing the end marker
- regex patterns continue to work unchanged

### Default coverage smoke tests

- OpenAI key sample matches
- Anthropic key sample matches
- AWS access key sample matches
- GitHub token sample matches
- JWT sample matches
- Postgres URL sample matches
- OpenSSH private key block sample matches
- `.env` assignment sample matches

---

## Acceptance Criteria

- [ ] Pattern config supports `regex`, `prefix`, `contains`, and `contains_between`
- [ ] Invalid matcher config fails startup with actionable errors
- [ ] Multiline SSH/private key blocks are masked correctly via `contains_between`
- [ ] Existing regex-based patterns continue to work after migration
- [ ] Default pattern set covers the listed high-value secret categories
- [ ] Unit tests cover matcher parsing, validation, and scan behavior
- [ ] `go test ./...` passes after implementation
