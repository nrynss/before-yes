# P6: Ship

```yaml
id:       P6
size:     M
requires: [P3, P4, P5]
blocks:   submission
parallel: low
```

**Goal:** A hosted demo that holds up through judging on what remains of $25, a README that leads with Nemotron and Token Factory, a video under three minutes, and a complete Devpost submission.

**What the rules require** (from the brief): a public repository with an OSI licence visible in About, a README that highlights the Nemotron use and where Token Factory helped, a hosted demo URL, a public YouTube video under three minutes, the chosen track, and feedback on Token Factory and the NVIDIA models. The runtime model call must go to Token Factory. OpenRouter does not satisfy the rule.

---

### T6.1: Deploy to the Hetzner tenant ★
```yaml
requires:   T4.4, T5.1, T0.2
fixture-ok: no
size:       M · mid
owns:       deploy/, Dockerfile, .dockerignore, the backup command in cmd/before-yes
status:     in-review (files built and checked locally; the server step is the user's)
```
One container on the existing Hetzner box behind Traefik, started by `~/work/hetzner-hosting/tenant.sh` with `--restart unless-stopped`, like the box's other apps. The image holds the Go binary and the built web app. Compute stays off the Nebius credit; only the model calls run on Token Factory. The host's keys sit in a `0600` env file (`NEBIUS_KEY`, `TAVILY_KEY`); the data directory is a Docker volume; a host cron runs `before-yes backup` nightly into the volume and keeps the newest 14. Production settings (`deploy/before-yes.prod.toml`) point the reader (Nano) and the judge (Ultra, with its sampling switches, pinned by a test) at Token Factory.

The URL is `beforeyes.nryn.dev`: a proxied Cloudflare A record to the box, already added. The box's address stays out of the repo; `deploy/deploy.sh` reads it from the gitignored `deploy/origin.env`. `deploy/README.md` is the runbook.

**Done when:** The public URL serves the app, `/healthz` answers, a live check on a non-demo title completes on Token Factory, and the ledger shows the charge.

---

### T6.2: README ★
```yaml
requires:   T3.6b, T6.1
fixture-ok: no
size:       M · frontier
owns:       README.md, docs/
status:     not-started
```
The rules ask for the Nemotron use and the Token Factory help to be highlighted, so they come first:
- **How Nemotron is used:** Nano reads every source into quoted findings, and Super judges the checklist, with thinking on for conflicted areas. Use real numbers from T3.6b: findings per title, quotes dropped, cost per check.
- **Where Token Factory helped:** cost per check, the OpenAI-style API that made the switch two settings, and anything T2.1b found.

Then: what it does, the checklist, why it is not "just use ChatGPT", the architecture diagram, how to run it locally, Keel and Chaaya as declared dependencies, and the Apache-2.0 licence.

**Done when:** The README covers every rule requirement, every number in it traces to a recorded measurement, and a fresh clone runs locally by following it.

---

### T6.3: Demo cards from the recordings ★
```yaml
requires:   T6.1 (to load them on the host)
fixture-ok: yes (recorded gather and judge replies; no live call)
size:       S · mid
owns:       testdata/demo-cards/, the seed-cards command in cmd/before-yes, internal/cards (seeding only)
status:     blocked (cards built and pinned; loading them on the host needs T6.1)
```
Rewritten by user decision. Visitors and judges browse cards from the home page's "Previous checks" list without spending. Those cards are the three the current pipeline computes on the eval's recordings set (gather 0): Roblox **no**, Minecraft **yes with settings**, Khan Academy Kids **yes**. They come from the recorded evidence and judge replies, through the rule gate and `checklist.Decide`, never hand-written; a test recomputes them and fails if a stored file differs. A `seed-cards` command loads them into the host's card store, once, at unguessable ids. Nothing is re-run live: the user stopped further runs, and a live judge samples, so a re-run could disagree with the recordings. Alternatives and extra common titles are dropped, since they would need live runs. Old stored cards are not deleted; any replacement backs the store file up first.

**Done when:** the three cards are committed and pinned by the recompute test; `seed-cards` is idempotent and tested; on the deployed host each card opens from "Previous checks"; no spend.

---

### T6.4: Video ★
```yaml
requires:   T6.3, T0.6
fixture-ok: no
size:       M · user
owns:       docs/video-script.md
status:     not-started
```
An agent writes the script and the shot list. The user records and uploads.

The brief's structure, under three minutes: a child asks for three things. One passes. One is **yes, with these settings**, and the card names them. One is **no**, and the card shows the review quote that decided it. Then the side-by-side with the safer alternative. Close on the cost of a check and the fact that it works on any title, now.

No third-party logos or branded footage. App names appear only as data on the card. Show the real hosted app, not a mockup.

**Done when:** The video is public on YouTube, under three minutes, and the script is committed.

---

### T6.5: Submit ★
```yaml
requires:   T6.2, T6.4
fixture-ok: no
size:       S · user
owns:       docs/submission.md
status:     not-started
```
An agent drafts every field in `docs/submission.md`: the description, the track (**Best Apps and Agents**), the side award (**Best Use of Tavily**), the repository and demo URLs, the video link, and the feedback on Token Factory and the NVIDIA models, drawn from the handoff log. The user reviews and submits.

Check the repository is public and the licence shows in About before submitting.

**Done when:** Devpost shows the submission as complete.

---

### T6.6: Judging posture
```yaml
requires:   T6.3, T4.3, T4.5
fixture-ok: no
size:       S · mid
owns:       docs/operations.md
status:     not-started
```
Judging runs on our key. Set the host for it: demo cards precomputed, the daily fresh-check cap on, visitor keys offered once the cap is spent, the live switch ready, the passcode ready if the URL is abused, and the backups running.

`docs/operations.md` says how to read remaining budget, how to turn live checks off, how to set the passcode, and how to restore from backup.

**Done when:** Each switch in `docs/operations.md` is exercised once on the deployed host, and the result is recorded.

---

### T6.7: Alternatives on the demo cards (optional)
```yaml
requires:   T6.3
fixture-ok: no (live checks)
size:       S · mid
owns:       testdata/demo-cards/, tools/eval (alternatives only)
status:     not-started (optional; the user decides whether to run it)
```
The demo cards carry no safer alternatives: an alternative needs a live check, and the user stopped live runs. This task is the one exception, if the user chooses it.

How alternatives work on `main` (T3.12): for a "no" or "yes with settings" card, one Tavily search ("safer <genre> like <title> for a <age> year old", about 1 credit) finds pages; a rule-based parser takes product names from them; each candidate, up to three, gets a full check (gather, Nano read, Ultra judge, about $0.11 and 9 Tavily credits each); only a candidate whose verdict beats the original's reaches the card (yes beats yes with settings beats no; unclear never does). Earlier live runs found candidates for Roblox and Khan but none that beat the original, and Roblox's candidates were Roblox experiences. The Nano-read finder (T3.13) was superseded and never merged.

1. Run the alternatives step live for the two cards that can show one: Roblox (9) and Minecraft (9). Spend cap about $0.80 and 60 Tavily credits.
2. A kept alternative and its own card are stored with the demo cards and loaded by `seed-cards`; the recompute test covers the original cards only, since an alternative's check is a live result.
3. If no candidate beats the original, record that and change nothing.

**Done when:** the run is reported with its spend, and any kept alternative opens from its demo card; or the user leaves this task unrun.

---

## Exit Criteria

- [ ] Hosted demo live, calling Token Factory at runtime.
- [ ] README leads with Nemotron and Token Factory, using measured numbers.
- [ ] Demo cards computed from the recordings and browsable from "Previous checks".
- [ ] Video public and under three minutes.
- [ ] Submission complete on Devpost.
- [ ] Judging posture set and every switch exercised.
- [ ] Optional, if the user runs T6.7: alternatives on the demo cards.
