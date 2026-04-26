# Slice 07 — Marketing & Launch Plan

**Status:** Proposed
**Owner:** _tbd_
**Depends on:** Slices 01–06 (the demo in each slice is an asset for marketing)
**Demo:** Day-one launch artefacts ready: landing page live, README polished, demo videos recorded, launch posts queued for HN / Show HN, Reddit, X, LinkedIn, dev.to.

---

## Why

AgentProxy is technically a niche, trust-sensitive tool. It needs deliberate positioning ("local-first secret-masking proxy for AI coding agents") to differentiate from cloud DLP / AI-firewall vendors (Pangea, Lakera, Protect AI). Without launch hygiene the project will land flat regardless of how good the engineering is.

This slice is the launch package: the messaging, the assets, the channels, and the day-one playbook.

---

## What Changes

- Landing page at `https://agentproxy.dev` (or the chosen domain) — single page, hero + 3 feature blocks + install command + dashboard screenshot + privacy promise.
- README rewrite: lead with the install one-liner from Slice 01, the dashboard screenshot from Slice 03, and the three "Verified Agents" sections from Slices 04–06.
- 3 short demo videos (≤ 60s each):
  - **Install + first run** (uses Slice 01 + 02 demos).
  - **What gets masked** (uses Slice 03 dashboard four-pane view).
  - **One agent, no surprises** (uses a Slice 04/05/06 demo + the non-disruption test).
- Launch posts (drafts) for: Hacker News (Show HN), Reddit (`r/programming`, `r/ChatGPTCoding`, `r/devops`), X/Twitter thread, LinkedIn post, dev.to article.
- A "Trust" page (or README section) covering: runs locally, no cloud, no telemetry, what data is held in memory and for how long, where the cert goes, how to uninstall.

---

## Impact

- **Affected:** README, `docs/marketing/` (already exists per git status), website repo (separate or `docs/site/`), social accounts.
- **Not affected:** code.

---

## Positioning

- **Headline:** "Mask secrets before your AI coding agent sends them. Restore them on the way back. 100% local."
- **Subhead:** "A drop-in HTTP proxy for Claude, Copilot, and Codex CLIs. No cloud account, no API key, no telemetry."
- **Three differentiators:**
  1. **Local-first.** Runs on your machine. Source-available. Nothing leaves the box.
  2. **Shape-preserving.** Surrogates look real to the model — no `[REDACTED]` placeholders that confuse the LLM.
  3. **Provider-agnostic.** Same install, three agents on day one, more after.
- **What we don't do:** No cloud dashboard. No outgoing telemetry. No SaaS upsell.

---

## Tasks

1. Write the landing-page copy (one page) and ship it (Astro / plain HTML — keep it cheap).
2. Rewrite README top-to-bottom with the new flow: TL;DR → install one-liner → 60s demo gif → "Verified Agents" → dashboard screenshot → "How it works" diagram → "Privacy" → contributing.
3. Record the three demo videos (screen capture + voiceover OK; under 60s each).
4. Draft "Show HN: AgentProxy — a local secret-masking proxy for AI coding agents" — ≤ 4 short paragraphs, link to repo + landing page.
5. Draft Reddit posts tailored to each subreddit's tone.
6. Draft a 7-tweet X thread mapping to the three demo videos.
7. Draft LinkedIn post.
8. Write a long-form dev.to article: "How we built a local MITM proxy for AI agents (and why your secrets shouldn't leave your machine)".
9. Write the Trust / Privacy page.
10. Pick a launch date (after Slices 01–06 are all ✅) and queue all of the above for that day.
11. Define one launch metric (default: GitHub stars in first 7 days) and one hygiene metric (issues filed; respond within 24h).

---

## Done When

- [ ] Landing page is live at the chosen domain.
- [ ] README is the new version, with Slice 03 screenshot and Slices 04/05/06 verified-agents sections.
- [ ] All three demo videos are recorded and embedded in README + landing page.
- [ ] Show HN draft is reviewed by at least one outside reader and ready to submit.
- [ ] Reddit / X / LinkedIn / dev.to drafts are written.
- [ ] Trust / Privacy page is written and linked from README + landing page.
- [ ] Launch date is on the calendar and Slices 01–06 are all ✅ at least 48h before that date.

---

## Team Review — 2026-04-25

### Hard gate

- **Slices 01–06 must be `done: true` before this slice can fire.** Show HN in particular collapses if visitors hit a broken install. Add an explicit `pre-launch dry-run on a fresh VM` task 48h before launch.

### Codebase state

- **LICENSE: MIT** (confirmed in repo). Lead with that on the landing page and in every post — directly differentiates from Pangea/Lakera/Protect AI which are proprietary SaaS.
- `THIRD_PARTY_NOTICES.md` exists. Add a task to verify it covers all current dependencies (especially the vendored goproxy fork) before launch.
- `docs/marketing/` directory exists (per `git status`) but its contents weren't audited; verify it's an asset folder we can build on, not just placeholder.

### Tasks added

12. **P0** 48h pre-launch dry-run: install AgentProxy on a fresh macOS VM and a fresh Ubuntu VM, run each of the three agents end-to-end, take screenshots/recordings of the dashboard. If any step fails, *postpone launch*.
13. **P0** Register `agentproxy.dev` (or finalise the domain choice). Set up DNS + TLS (Cloudflare or GH Pages with custom domain). Without a final URL, posts can't be drafted.
14. **P0** Publish a "Trust" page that backs every privacy claim with code references: (a) grep proves no telemetry calls (`internal/` has no analytics imports), (b) listener bound to `127.0.0.1` (point at `internal/proxy/server.go`), (c) vault session is in-memory only (point at `internal/vault/session.go`).
15. **P1** Run `go mod why` / license audit on every dependency; produce `THIRD_PARTY_LICENSES.md` and link from README footer.
16. **P1** Set up GitHub Discussions *before* launch day for issue triage; pin a "Welcome / FAQ" thread.
17. **P1** Expand launch metrics beyond GitHub stars: (a) `npm install` success rate via package telemetry (npm provides install counts), (b) issues filed in 48h (signal of real users), (c) Show HN final rank.
18. **P1** Pre-launch competitive sweep: 7 days before launch, scan HN/Reddit for similar tools that may have just shipped — adjust positioning if needed.
19. **P2** Decide demo video hosting (YouTube unlisted vs. inline MP4 vs. asciinema) and add as task; budget ~$0 if YouTube/asciinema, but call it out either way.
