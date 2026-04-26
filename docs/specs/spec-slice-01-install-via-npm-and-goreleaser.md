# Slice 01 — Install via npm and GoReleaser

**Status:** Proposed
**Owner:** _tbd_
**Demo:** A user with no prior knowledge runs *one command* on macOS / Linux / Windows and has a working `agentproxy` binary plus the wrappers on `PATH`.

---

## Why

Today AgentProxy can only be installed by cloning the repo and running `./install.sh`, which requires Go 1.23+. We need two distribution channels that cover both audiences:

- **Developers / Node users:** `npm install -g @muthuishere/agentproxy` (familiar one-command install; no Go toolchain).
- **Everyone else:** GoReleaser-published binaries via `curl … | sh`, Homebrew tap, and Scoop bucket.

The two channels share the *same* underlying release artifacts so we maintain one build pipeline.

---

## What Changes

- New `.goreleaser.yaml` builds versioned archives for `darwin/{amd64,arm64}`, `linux/{amd64,arm64}`, `windows/amd64`.
- New GitHub Actions workflow `.github/workflows/release.yml` runs `goreleaser release --clean` on tag push.
- New npm package `@muthuishere/agentproxy` whose `postinstall` script downloads the matching GoReleaser archive for the host platform, extracts the binary + wrappers, and places them in the npm bin dir.
- New `scripts/install-online.sh` and `scripts/install-online.ps1` for the curl-pipe path.
- Existing `install.sh` repurposed as the *post-extract* installer included **inside** the release archive (no longer calls `go build`). Source-build flow moves to `scripts/install-from-source.sh` and stays as a Taskfile target for contributors.
- Self-hosted Homebrew tap (`muthuishere/homebrew-agent-proxy`) and Scoop bucket (`muthuishere/scoop-agent-proxy`) updated by GoReleaser on release.

---

## Impact

- **Affected:** `install.sh`, `install.bat`, `Taskfile.yml`, `bin/*` wrappers (must remain in archive layout), CI.
- **Breaking:** users currently running `./install.sh` from a clone will need to be told to use the Taskfile target (`task install-from-source`) instead. Document in CHANGELOG.
- **Not affected:** the running proxy, masking pipeline, dashboard, provider parsers.

---

## Tasks

1. Add `.goreleaser.yaml` covering 5 platforms; archives include binary, `bin/*`, default configs, LICENSE, README.
2. Add `cmd/agentproxy/version.go` with build-time `-ldflags` for version/commit/date; surface via `agentproxy version`.
3. Add `.github/workflows/release.yml` triggered on `v*` tag push.
4. Create the npm package skeleton (`packages/npm/`) with `package.json`, `bin/agentproxy.js` shim, `scripts/postinstall.js` that resolves platform → downloads → verifies SHA256 → extracts → places binary.
5. Publish dry-run: `npm publish --dry-run` succeeds; package size < 50 KB (binary fetched at install, not bundled).
6. Add `scripts/install-online.sh` (POSIX) and `.ps1` (Windows) with: arch detection, version override (`AGENTPROXY_VERSION`), checksum verification, user-mode default (`$HOME/.local/bin`), `--system` opt-in.
7. Configure GoReleaser `brews:` and `scoops:` blocks → publish to self-hosted tap + bucket on release.
8. Repurpose `install.sh` as post-extract installer (no `go build`); rename source-build flow to `scripts/install-from-source.sh`.
9. Update README with three install paths: npm one-liner, curl-pipe one-liner, brew/scoop. Keep the source-build path under "Contributing".
10. Add CHANGELOG entry noting the install-flow change.

---

## Done When

- [ ] `goreleaser release --snapshot --clean` produces all 5 archives locally.
- [ ] Tag push to `main` triggers a GitHub release with archives + checksums.
- [ ] On a clean macOS arm64 VM **without Go**: `npm install -g @muthuishere/agentproxy` → `agentproxy version` prints the released version.
- [ ] On the same VM: `curl -fsSL <install-online url> | bash` → same result.
- [ ] On a clean Ubuntu 22.04 VM: both paths succeed.
- [ ] On a clean Windows 11: `iwr … | iex` succeeds; `scoop install agentproxy` succeeds.
- [ ] `brew tap muthuishere/agent-proxy && brew install agentproxy` succeeds on macOS.
- [ ] Bundled wrappers (`claudeproxy`, `codexproxy`, `copilotproxy`, `agentproxy-start`) are on `PATH` after any of the above.
- [ ] CHANGELOG and README reflect the new install flows.

---

## Team Review — 2026-04-25

Findings from a deep audit against the actual codebase. **Verified blockers** must land before slice tasks 1–10 can succeed.

### P0 — Verified Blockers

- **`go.mod` is invalid.** Module path is literally `module github.com/ ` (no name, trailing space). Both npm publish and `goreleaser` will reject this. **Fix `go.mod` to a real module path (e.g. `github.com/muthuishere/agent-proxy`) and update all internal imports.** This is the highest-priority pre-task before anything else.
- **No build-time version injection.** `cmd/agentproxy/main.go` declares `var version = "0.1.0-go-migration"` as a hardcoded literal. GoReleaser cannot inject. Move to `cmd/agentproxy/version.go` exporting `Version`, `Commit`, `BuildDate` and switch builds to `-ldflags "-X main.Version=$VERSION ..."`.
- **Wrapper scripts use repo-relative path resolution.** `bin/claudeproxy` etc. compute `SCRIPT_DIR="$(cd "$(dirname …)/.." && pwd)"`, which only works when the binary lives one level up. When npm or GoReleaser places wrappers next to the binary (or in a different layout), this breaks. **Rewrite resolution to: (a) prefer `command -v agentproxy`, (b) fall back to a sibling binary in the same dir as the wrapper, (c) drop the `..` path entirely.**
- **`install.sh` creates symlinks pointing into the repo clone.** Lines 97–152 do `ln -sf "$REPO_DIR/bin/claudeproxy" …`. After repurposing for post-extract use, this must switch to *copying* the wrappers (not symlinking from the extracted archive directory which the user may delete).

### P1 — Spec Corrections

- **npm package layout: declare `bin` in `package.json`, don't symlink in postinstall.** This makes npm's own lifecycle handle install / upgrade / uninstall symlinks. Postinstall only downloads + verifies + extracts the platform binary into a known package-relative path; the `bin` shim resolves it at runtime.
- **`install.bat` cannot be reused as-is from npm postinstall.** It generates `.bat` wrappers via `echo` lines from a cmd shell. npm postinstall runs Node. Replace with checked-in `.bat` template files in the npm package, copied into place by Node code.
- **CGO confirmed disabled.** No `import "C"` anywhere; `CGO_ENABLED=0` is safe across all 5 targets. Add this explicitly to `.goreleaser.yaml` env block.
- **Vendored `third_party/goproxywss` is a local `replace` directive** — works fine with GoReleaser (no network fetch). Add a comment in `.goreleaser.yaml` noting this so future contributors don't try to delete the directory.
- **`agentproxy version` already exists as a command** but reads the hardcoded literal. Confirm GoReleaser archive layout puts the binary at the path the npm shim expects.

### Tasks added (elevated to P0/P1)

11. ~~**P0** Fix `go.mod` module path and propagate through all internal imports. Add a CI check that `go vet ./...` builds cleanly with the new path.~~ **DONE 2026-04-25** — `go.mod` now declares `module github.com/muthuishere/agentproxy`; existing internal imports already used this path, so no propagation needed. `go build ./...` clean; full test suite passes.
12. **P0** Create `cmd/agentproxy/version.go` with exported build-injectable variables; remove hardcoded literal in `main.go`.
13. **P0** Rewrite `bin/*` resolution logic to be layout-portable (no `../bin` assumption).
14. **P0** Repurposed `install.sh` must copy wrappers from the extracted archive into the install dir (not symlink).
15. **P1** npm `package.json` declares `bin` entries; postinstall is download/verify/extract only.
16. **P1** Replace inline `echo`-generated `.bat` wrappers with checked-in template files copied by the Node postinstall.
17. **P1** Add explicit `env: [CGO_ENABLED=0]` to `.goreleaser.yaml`; document why.
18. **P1** Add an `agentproxy upgrade` no-op (or doc note) clarifying that npm/brew/scoop manage upgrades; the postinstall must be idempotent.
19. **P1** Document that `~/.local/bin` PATH warning surfaces in *both* curl-pipe and npm postinstall paths.
