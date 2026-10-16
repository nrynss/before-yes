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
owns:       deploy/, Makefile
status:     not-started
```
One Go binary plus the built Chaaya app, on the existing Hetzner box behind Traefik, using `~/work/hetzner-hosting/tenant.sh`. Compute stays off the Nebius credit. Only the model call runs on Nebius, which the FAQ confirms is enough.

A systemd unit, a settings file that references keys in a `0600` env file on the host, the data directory on persistent disk, and a nightly `sqlite.Backup`. Production settings point both roles at Token Factory.

Read `~/.claude/CLAUDE.md` on systemd before writing the unit: `StartLimit*` keys go in `[Unit]`, and the runtime property is what gets verified.

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

## Exit Criteria

- [ ] Hosted demo live, calling Token Factory at runtime.
- [ ] README leads with Nemotron and Token Factory, using measured numbers.
- [ ] Demo cards computed from the recordings and browsable from "Previous checks".
- [ ] Video public and under three minutes.
- [ ] Submission complete on Devpost.
- [ ] Judging posture set and every switch exercised.
