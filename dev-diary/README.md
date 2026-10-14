# Development Diary: Phase Handoffs

This document breaks down the work for **Before Yes**, the Nebius × NVIDIA Global AI Hackathon entry.

Reference specification:
- [`../BRIEF.md`](../BRIEF.md): product, checklist, architecture, models, budget, guardrails, demo.
- [`ORCHESTRATOR.md`](ORCHESTRATOR.md): how the orchestrator runs the loop of pick, dispatch, review, land and close, with the brief template.
- [`feedback.md`](feedback.md): friction with Token Factory, Nemotron, Tavily, Keel and Chaaya. It feeds the Devpost feedback.
- `~/work/hackathons/nebius-nvidia.md`: event rules, prizes, and why this concept was chosen.

Where a phase document and the brief conflict, the brief wins. Record the discrepancy in the handoff log of the phase that found it.

Each phase document gives an engineer starting cold the complete context for its tasks.

**Current status:** P0 and P1 are done. P2, P3, P4 and P5 can start (each by its own `requires`). An earlier backend draft sits in `scratch/` at the repository root, uncommitted and unreviewed. Tasks it covers show `status: drafted`, which means there is code to start from, not that the task is done. See [Starting from the scratch draft](#starting-from-the-scratch-draft).

---

## Keel v0.7.3

Keel v0.7.3 is the version this project builds on, and the last Keel change for it. `go.mod` on `main` already requires it. No task bumps Keel again.

It carries the fixes for the two gaps this project found, which landed in v0.7.1. v0.7.2 and v0.7.3 hardened them:
- **[nrynss/keel#82](https://github.com/nrynss/keel/issues/82):** cost stores share one database file. `cost/sqlitestore` `Config.Namespace` now names the store's own tables. A name ending in `_owner` is refused with `ErrInvalid`.
- **[nrynss/keel#83](https://github.com/nrynss/keel/issues/83):** a failed job names its own code. A work function returns `job.Failure{Code, Detail, Err}`, bare or wrapped. The Runner publishes `Code` and `Detail` in the error terminal. `Result.Code` and `Record.Code` keep the code across a restart. A chain without a `Failure` keeps `job_failed`.

Do not build the old workarounds: one database file per cost pool, or error codes carried in a job's result data.

Tasks that import Keel: T1.4, T2.1, T2.2, T2.4, T2.5, T3.1–T3.5, T3.7, T4.1–T4.5, T6.1.

---

## Starting from the scratch draft

The earlier backend draft lives on the branch **`draft/scratch`** on GitHub, under `scratch/`. It is not on `main`, and nothing in it has been reviewed. A task can start from it on any machine, local or remote.

**Move, never copy. A claim is a pushed deletion.** To claim a file, an agent:
1. Takes the file into its task branch: `git checkout origin/draft/scratch -- scratch/<path>`, then `git mv scratch/<path> <real path>`.
2. Claims it on the draft branch: in a checkout of `draft/scratch`, `git rm scratch/<path>`, then commit "claim <task>: <path>" and **push**. Push the claim before building on the file. If the push is rejected because the branch moved, fetch it. If the file is gone, another task claimed it first, and its code reaches you through `main` when that task lands.

A file still on `draft/scratch` is unclaimed. Reading a file for reference is fine. Editing it on the draft branch is not.

- Move only the files the table gives your task.
- A draft file that spans several tasks is claimed by the first task listed. That task keeps its own part and, in the same claim commit, adds the remainder back to `draft/scratch` as a new file named for the next task, for example `scratch/internal/server/server.T4.3.go`.
- Record every claim in your report: which files, for which task.
- A drafted task still goes through review. The draft is a starting point, not evidence.

| Draft path on `draft/scratch` | Claimed by |
|---|---|
| `scratch/internal/pipeline/pipeline.go`, `pipeline_test.go` | T3.4 (T1.3 already wrote `types.go` fresh) |
| `scratch/internal/pipeline/gather.go` | T3.1 |
| `scratch/internal/pipeline/doc.go`, `read.go` | T3.2 |
| `scratch/internal/pipeline/judge.go` | T3.3 |
| `scratch/internal/server/server.go` | T4.1 takes the check job and the check and card routes, then T4.2 the evidence cache wiring, then T4.3 the gate, then T4.4 the status, checklist and static routes |
| `scratch/internal/cards/` | T4.2 |
| `scratch/cmd/before-yes/main.go` | T4.4 |

Already claimed or superseded: `go.mod` (T0.1), `internal/model` (T2.1), `internal/tavily` (T2.2), `internal/appstore` (T2.3), `internal/spend` (T2.4). `internal/checklist` and `internal/finding` were written fresh by T1.1 and T1.2. The checklist draft is deleted, and the finding draft stays only in the main checkout's local `scratch/`.

When `draft/scratch` holds no files, delete the branch and say so in the handoff log.

---

## Swarm Coordination Rule

> **Phase-level dependencies are advisory. Task-level dependencies are binding.**

Do not wait for a whole phase to finish. If a task requires only `T1.2`, start it the moment `T1.2` closes.

**Fixtures make most work offline.** [T0.4](PHASE-0-ground.md#t04-fixtures-from-three-real-titles-) captures real public material for the three demo titles into `testdata/`: App Store review pages, Tavily results, and extracted policy text. Tasks marked `fixture-ok: yes` run against those files with no key and no spend. Only `b` tasks call a live service.

---

## Phase Graph

| Phase | Document | Requires | Runs parallel with | Blocks |
|---|---|---|---|---|
| **P0** Ground | [PHASE-0-ground.md](PHASE-0-ground.md) | none | none | everything |
| **P1** Contracts | [PHASE-1-contracts.md](PHASE-1-contracts.md) | T0.1 | none | P2–P5 |
| **P2** Clients and meter | [PHASE-2-clients.md](PHASE-2-clients.md) | P1 | P3, P4, P5 | P3 live runs, P4 |
| **P3** Pipeline | [PHASE-3-pipeline.md](PHASE-3-pipeline.md) | P1, T0.4 | P2, P4, P5 | P4 jobs, P6 |
| **P4** Server | [PHASE-4-server.md](PHASE-4-server.md) | T1.3, T1.4, T2.4 | P2, P3, P5 | P5 live wiring, P6 |
| **P5** Frontend | [PHASE-5-frontend.md](PHASE-5-frontend.md) | T1.3, T1.4 | P2, P3, P4 | P6 |
| **P6** Ship | [PHASE-6-ship.md](PHASE-6-ship.md) | P3, P4, P5 | none | submission |

```text
  P0 ──▶ P1 ──┬──▶ P2 clients + meter ──┐
              ├──▶ P3 pipeline ─────────┤
              ├──▶ P4 server ───────────┼──▶ P6 ship
              └──▶ P5 frontend ─────────┘
```

P0 and P1 are the serial bottleneck. Once the contracts freeze, four tracks run at once without touching each other's files.

---

## Parallel Tracks

| Track | Phase | Network required? | First task | Notes |
|---|---|---|---|---|
| **A: Clients** | P2 | `b` tasks only | T2.1 | Model, Tavily and App Store clients, the meter and budgets. Progress is on the status board. |
| **B: Pipeline** | P3 | `b` tasks only | T3.1 | Gather, read, judge, alternatives. The product's depth lives here. |
| **C: Server** | P4 | No | T4.1 | Jobs, progress stream, card store, gate. |
| **D: Frontend** | P5 | No | T5.1 | Chaaya app against the example payloads from T1.3. |

If capacity is short, run B and D first. B makes the card true and D makes it visible. Both are on the critical path.

---

## Task Pattern

```markdown
### T3.2: Reader ★
requires:   T1.2, T3.1
fixture-ok: yes
size:       M · frontier
owns:       internal/pipeline/read.go, internal/pipeline/read_test.go
status:     not-started
```

- **requires:** Binding task ids.
- **fixture-ok:** Whether the task can be built and tested against `testdata/` alone.
- **size:** Scope from XS to XL, then the class to send.
- **owns:** The paths this task alone edits.
- **status:** `not-started`, `drafted`, `claimed:<id>`, `in-review`, `done`, or `blocked`.

| Class | Send it for |
|---|---|
| **frontier** | A decision others build on. Prompt and judgement work verified by measurement. A spec that needs interpreting. |
| **mid** | Well specified implementation, or a spec precise enough to follow literally. |
| **light** | Mechanical, low ambiguity work that its own test verifies. |
| **user** | Only the user can do it: an account, a key, a recording, a submission, a decision. |

A star (★) marks a task on the critical demo path.

A `b` task (T2.1b) is the live sibling of its parent. It owns only the live probe files, behind the `live` build tag. A live probe never runs in CI and never gates a build. A live call is evidence: commit the probe, and paste its transcript into the handoff log.

---

## Developer Guidelines

1. **Claim before starting.** Set `status: claimed:<id>` in the phase document.
2. **Stay inside `owns`.** A change outside it is a new task or a handoff note.
3. **Contracts freeze at the end of P1.** A change to a contract updates its example payload in `testdata/` in the same commit.
4. **Every claim on a card links to a quote.** No grade, flag, or setting reaches a card without a verified finding behind it. The quote check is a string match, never the model's word.
5. **The verdict is computed, not generated.** The judge grades areas. `checklist.Decide` turns grades into the verdict.
6. **Every paid call goes through the meter.** A model call or Tavily call that bypasses `spend` is a C finding. The $25 has to cover development, the demo, and judging.
7. **Keel gaps become Keel issues.** If Keel is missing something, write the issue text in the handoff log. Do not work around it here. The user files it.
8. **Public material only.** Free tiers may log prompts. Before Yes reads only public pages, so nothing private ever reaches a model.
9. **No third-party logos or branded footage** in the video or the UI chrome. A title's name appears only as data on a card.
10. **Document handoffs.** A task without a handoff log entry is unfinished. Record what exists, what surprised you, and what the next agent should not have to work out again.

---

## Scope

Every task here ships. A task that turns out to be blocked becomes a finding, not a cut. Record it, name every task that depends on it, and mark it `blocked`. If a task is too large, split it into `T3.2a` beside `T3.2` and record the split.

---

## Critical Path

```
Token Factory active (T0.2) + keys (T0.3) + demo titles (T0.5)
  -> fixtures (T0.4) -> contracts (T1.1–T1.4)
  -> gather (T3.1) -> read with Nano (T3.2) -> judge with Super (T3.3) -> alternatives (T3.5)
  -> quality pass on the three titles (T3.6, T3.6b)
  -> job + stream (T4.1) -> card store (T4.2)
  -> request, progress, card views (T5.2, T5.3, T5.4) -> side by side (T5.5)
  -> deploy (T6.1) -> precompute demo cards (T6.3) -> video (T6.4) -> submit (T6.5)
```

The demo is built around one moment. The policy says chat is filtered, but review 312 says the filter failed for a 7-year-old, and that review decides the card. Protect everything that moment depends on before anything else.

---

## Status Board

| Phase | Tasks complete | Status |
|---|---|---|
| P0 | 9 / 9 | Done. Two exit criteria wait on later tasks: a live Nano call (T2.1b) and the config check (T4.4). CI is green. |
| P1 | 4 / 4 | Done. The `job.Failure` exit criterion waits on T4.1. The main-checkout pins and `.env` leak check, waived at landing, ran at the P1–P2 integration and passed. |
| P2 | 10 / 12 | T2.1, T2.1a, T2.1b, T2.1c, T2.1d, T2.2, T2.2b, T2.3, T2.3b and T2.4 are done. T2.5 is landed but blocked on T3.6 for the measured cost per check. T2.4b (the daily cap, and booking timed-out calls) is in progress. |
| P3 | 1 / 10 | T3.8a is done: App Store page fixtures for all four titles. T3.1–T3.4 are drafted on `draft/scratch`. |
| P4 | 0 / 5 | Not started. T4.1, T4.2 and T4.4 drafted. |
| P5 | 0 / 7 | Not started. |
| P6 | 0 / 6 | Not started. |

---

## Handoff Log

Newest first. One entry per closed task, finding, or split.

- **T2.1d closed.** Haiku implemented it, and the orchestrator validated it with no review loop.
  - **What changed:** the live probe has no fixed client timeout now. `liveProbe.raw`, the only place the probe sends a request itself, gives each request `DefaultTimeouts.For(max_tokens)`: 0 for the model list, which gets the floor; 8 for the refused-id checks; the role's cap for the rate-limit call. Chat calls get the same rule through `Client.Chat`. The 8-minute overall context stays.
  - **Orchestrator validation:**
    - The diff stays inside owns.
    - The only `http.Do` in the file is in `raw`, and chat builds its client with `New`.
    - `go vet -tags live`, `go build -tags live ./...` and the gate pass in the worktree and on main. No key values found. No live run.
- **T3.8a closed.** Haiku implemented it, and the orchestrator validated it with no review loop, by the user's ruling.
  - **What landed:** `tools/capture -only storepage` makes one basic Tavily extract of each title's `trackViewUrl`, taken from the saved search fixture. It moves to advanced only when basic shows no review text, and stops at 3 credits per title. There is also a `-depth` flag.
  - **Spend:** 1 credit in total (Khan 1, the other three 0, as Tavily reported). Advanced was never needed.
  - **What the page holds:** 3 or 4 user reviews per title (Roblox 4, Minecraft 3, Khan 3, Toca 3). Title and body come through verbatim. Rating, date and reviewer name do not. Each block appears twice. Toca Life World is listed as "Toca Boca World: Game & Play", so the lookup goes by trackId, not by name. The page is usable for T3.8, but thin.
  - **Orchestrator validation:**
    - Opened all four saved extracts myself: one result each, no failures, credits as reported. Read the review blocks for Roblox and Khan Academy Kids.
    - The capture tests pass under `-race`. Gate rc 0 in the worktree after the rebase, and on main. No key values found. No dates.
  - **Found by the orchestrator and added to T3.8:** a developer response first appears with no label, and only its repeat carries "Developer Response". T3.8's block now lists the page's shape.
- **T2.1c closed.** Haiku implemented it, and the orchestrator validated it with no review loop, by the user's ruling.
  - **What changed:** the fixed 3-minute client timeout is gone. Each attempt now gets `clamp(60 s + max_tokens / 30 tok/s, 90 s, 15 min)`, so a 16,000-token thinking call gets about 593 s and a short call keeps 90 s. The numbers are in `model.DefaultTimeouts`. There is no new setting, because the role's `max_tokens` already sets how long its calls run.
  - **Timed-out calls:** a timed-out call returns `context.DeadlineExceeded`. It is not retried, since a retry could bill twice. Its reply is unmeasured and priced at the estimate, never $0.
  - **Re-measured:** 4 live calls at 4,000 tokens, about $0.006 spent. Nano ran at about 68 tok/s, Super at about 156, and OpenRouter's free judge at 91 (35–43 at T2.1b). A floor of 30 sits below the slowest rate seen.
  - **Orchestrator validation:**
    - The diff stays inside owns.
    - The four Done-when tests pass under `-race`, in about 2 s.
    - `Retryable` refuses `DeadlineExceeded`.
    - Gate rc 0 in the worktree after the rebase, and on main. No key values found. No dates.
  - **Found and moved to T2.4b:** `spend.Meter.Spend` releases the hold on any error from the call, so a timed-out model call books $0 today.
  - **Left open:** `internal/model/live_test.go` builds its own probe client with a 3-minute timeout. A live 16,000-token probe would be cut. Whichever task next touches that file should fix it.
- **T2.5 landed, blocked on T3.6; T2.4b split out.** Haiku implemented it. By the user's ruling, T2.1c, T2.5 and T3.8a have no review loop: the orchestrator validates each one and sends the same agent back if something is wrong.
  - **Decided** in [`docs/budget.md`](../docs/budget.md):
    - The model pool has a lifetime ceiling of $20, with $5 of the $25 credit in reserve.
    - Tavily has a monthly ceiling of 900 of the 1,000 free credits.
    - Fresh checks are capped at 5 per UTC day while judging runs: 5 × 12 credits × 15 days = 900.
  - **Estimated, pending T3.6:** one fresh check costs about $0.0255 of model spend, from the brief's token counts at the brief's prices, plus about 8 Tavily credits, from T3.1's source plan. Tavily is the tighter limit, at about 112 checks a month. The doc gives the formula T3.6b fills in. The brief leaves out Nano output and the conflict pass, so the model figure may be low.
  - **Orchestrator validation:**
    - The prices and arithmetic match BRIEF.md.
    - The claim that `spend.Open` knows only two pools, and leaves each pool's period to the caller, matches the code.
    - The diff stays inside owns. Gate rc 0 in the worktree and on main. No key values found. No dates.
  - **Split:** T2.4b, the daily checks pool, which also pins the tavily pool to `MonthlyUTC`.
- **T2.3b closed.** Reviews: [round 1](adversarial-review/t2.3b-round1.md) (Sonnet, REMEDIATE: H 1, M 3, L 1), [round 2](adversarial-review/t2.3b-round2.md) (a fresh Sonnet reviewer, REMEDIATE: L 2), and [round 3](adversarial-review/t2.3b-round3.md) (a fresh Sonnet reviewer, APPROVE, zero residue). A fresh Sonnet remediator ran after each REMEDIATE.
  - **What the probe shows:** the search half works (us 3 hits, gb 5 for Roblox). The review feed is dark in both storefronts, so the probe FAILs loudly and every audit prints "not run". The transcript is at [`live/t2.3b-transcript.txt`](live/t2.3b-transcript.txt). It needs no key and spends no credits.
  - **Still passes on a populated feed:** reviewers in rounds 2 and 3 each pointed the probe at a local server serving the T0.4 Roblox fixtures. It read 500 reviews in 10 pages per storefront, and the raw-body pin compared. Both reverted the edit.
  - **Unconfirmed while the feed is dark:** the single-entry page shape, the countries question and the field audit. The PHASE-2 block lists them. T3.8a and T3.8 stand in for the feed.
  - **Orchestrator documentation fix:** dates were removed from the handoff log and the T2.2b transcript (`Date:` headers withheld).
  - OpenCode's `task-t2.3b-appstore-live` branch and worktree are superseded by `main`. They are OpenCode's to remove.
- **Apple's review feed is dark; store-page fallback chosen; scratch draft moved to `draft/scratch`.**
  - **The feed** returns HTTP 200 with no entries for every app tried, Facebook as a control included. That holds from this machine in two probes, and from the Hetzner box (167.233.247.107), so it is not throttling of one host.
  - **The user chose a fallback:** extract the title's App Store web page through Tavily (T3.8a captures it, T3.8 parses it). The feed stays first in T3.1's plan in case it returns. The 500-review T0.4 fixtures still drive offline work and the demo cards.
  - **T2.3b** stays at REMEDIATE. Its probe needs to fail loudly on an empty feed (M4) and record the dark-feed result.
  - **The scratch draft:** 10 draft files are now on the `draft/scratch` branch on GitHub, so remote sessions such as Grok can claim them. Local copies were checked byte for byte against the branch and removed. The superseded `internal/finding` draft stays local only. The claim rule above is rewritten as a pushed deletion.
- **T2.1b closed.** Reviews: [round 1](adversarial-review/t2.1b-round1.md) (Sonnet, REMEDIATE: M 3, L 3), remediation by a fresh Sonnet agent, and [round 2](adversarial-review/t2.1b-round2.md) (a fresh Sonnet reviewer, APPROVE: L 2). The orchestrator closed both round-2 Ls on the branch.
  - **Settings now confirmed live.** Token Factory ids are case-sensitive: `nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B` works and the lowercase id 404s. `[reader.extra]` turns the reader's thinking off. Nano thinks by default, at about 45 times the billed output. The thinking switch is `chat_template_kwargs.enable_thinking` on Token Factory and build.nvidia.com, and `reasoning.enabled` on OpenRouter. The rate limits are 100 a minute for Nano and 300 for Super. A round-2 reviewer decoded `before-yes.toml.example`, and both alternative tables, with go-toml.
  - Seven Token Factory and Nemotron entries are in [feedback.md](feedback.md), the first for the Devpost feedback. The transcript is at [`live/t2.1b-transcript.txt`](live/t2.1b-transcript.txt), with no account id.
  - **Spend:** about $0.0018 on Token Factory across three live runs (implementation, the round-1 re-run and remediation). OpenRouter and build.nvidia.com are free.
  - **Split:** T2.1c, because the client's 3-minute timeout is at or past the limit for 16,000-token thinking calls.
  - **Orchestrator slip, recorded:** the first round-2 closing commit was made while `gofmt` was failing, because piping `check.sh` through `tail` hid its exit code. The commit was amended before landing. Check the gate's exit status, not the last line of its output.
  - OpenCode's `task-t2.1b-model-live` branch and worktree are superseded by `main`. They are OpenCode's to remove.
- **T2.2b closed.** Reviews: [t2.2b-round1.md](adversarial-review/t2.2b-round1.md). The Sonnet reviewer gave REMEDIATE: M 2, L 2. All four findings were documentation or non-load-bearing, so the orchestrator closed them on the branch and recorded each in the review file.
  - **Measured:** `usage.credits` came back on 3 of 3 calls. Raw content on search costs 0 extra credits but adds about 110 KB for 5 hits. Tavily sends no rate-limit headers.
  - **The rate limit is not measured.** The documented 100 a minute is unverified, and the client paces at 90. It stays open until the deployed host shows a 429.
  - The transcript is at [`live/t2.2b-transcript.txt`](live/t2.2b-transcript.txt). Spend: 4 Tavily credits (2 for the probe, 2 for the reviewer's re-run).
  - OpenCode's `task-t2.2b-tavily-live` branch and worktree are now superseded by `main`. They are OpenCode's to remove.
- **Risk: Apple's review feed is returning no reviews.** Found by the T2.3b reviewer and confirmed independently by the orchestrator.
  - Every request returns HTTP 200 with a feed skeleton and no `entry`: all four demo titles plus Facebook (284882215) as a control, in both `us` and `gb`, and in the JSON, XML and no-sort URL variants.
  - The same URL returned 50 reviews in an earlier probe, and the T0.4 fixtures hold 500 per title.
  - Public reports call this feed intermittently empty. It may be an outage, a retirement, or throttling of this host after T0.4's 40-page capture.
  - **T2.3b is not landed.** Its review stands at REMEDIATE (H1, M3, L1), and its branch `review-t2.3b` is kept.
  - Offline work is unaffected, because the fixtures hold real reviews. A live check depends on the feed coming back. This is the user's decision.
- **Keel issues filed; scratch checklist deleted.**
  - The two Keel gaps from P2 are filed as [nrynss/keel#98](https://github.com/nrynss/keel/issues/98) (`throttle.Note` reports `Attempts` for an error that spent the `TransientAttempts` budget) and [nrynss/keel#99](https://github.com/nrynss/keel/issues/99) (a durable `sqlitestore` budget cannot back a `cost.Meter`).
  - Both are already worked around here. The model and Tavily clients annotate transient exhaustion themselves, and `spend` drives the store directly. Nothing waits on either.
  - `scratch/internal/checklist/` is deleted, at the user's request. `scratch/internal/finding/` was superseded the same way by T1.2 and is still there.
  - **T2.1b, T2.2b and T2.3b** are in review by three fresh Sonnet reviewers, one per probe. Each rebases OpenCode's branch onto `main` in a review branch of its own, so OpenCode's branches stay untouched.
- **P1 and P2 integrated.** Phase 1 landed on GitHub from a remote session, through stacked PRs. Phase 2 landed on the local `main` from OpenCode and was never pushed. The two `main`s diverged: local was 25 commits ahead and 97 behind.
  - The orchestrator merged `origin/main` into local `main`, without rewriting OpenCode's history. The only conflict was this README: the status board and the handoff log. Both sides were kept.
  - The gate and the `.env` key-leak grep ran in the main checkout, which settles P1's waived landing checks.
  - **Still on OpenCode's branches, unlanded:** `task-t2.1b-model-live` (in review), `task-t2.2b-tavily-live` and `task-t2.3b-appstore-live`. They are committed and clean, with no review yet. OpenCode was not running when this was found.
  - **Stale and fully merged:** `task-t2.2-tavily`, which still has a worktree, and `task-t2.1a-extra`. Both are OpenCode's to remove.
- **T2.1a closed.** Review: [t2.1a-round1.md](adversarial-review/t2.1a-round1.md), APPROVE, C 0 · H 0 · M 0 · L 2.
  - **The fix:** `New` and `Settings()` now deep-copy `Extra` through every nested `map[string]any` level, so a caller mutating the thinking-switch object after construction (or after reading `Settings()`) can no longer change a later request body. The T2.1 round-1 L1 hazard is closed on both paths; `Options.Extra` was already safe and stays marshalled per call. Public API unchanged.
  - **Orchestrator note:** the review's L1 (the copy comment claimed more than the code does) was reworded directly in `New`, `Settings` and `cloneExtra` on the branch, comment-only, and recorded in the review file.
  - **Recorded, no task:** review L2 — `cloneExtra` has no cycle check, so a cyclic `Extra` map dies at `New` with a stack overflow instead of failing cleanly at `Chat`. Unreachable from TOML and a caller bug; a depth bound if the helper is ever lifted.
  - **Also carried from T2.1's review:** the empty-`choices` 200 comment now says it draws the smaller two-attempt budget and that its one retry may be a second billed call.
- **T2.2 closed.** Reviews: [t2.2-round1.md](adversarial-review/t2.2-round1.md) (REMEDIATE, one M and one L) and [t2.2-round2.md](adversarial-review/t2.2-round2.md) (APPROVE, zero residue).
  - **Move:** `scratch/internal/tavily/tavily.go` moved from the main checkout into worktree `task-t2.2-tavily` for T2.2; `scratch/internal/tavily/` is gone. New `internal/tavily/tavily_test.go`.
  - **API:** `New(baseURL, key func() (string, error))`, `Search(ctx, tavily.Search) ([]Result, Credits, error)`, `Extract(ctx, []string) ([]Page, Credits, error)`, plus `ExtractWithFailures` naming which URLs Tavily could not read and why; `Credits{Used, Reported}` tells the meter whether the number is Tavily's or the price list's. Search carries depth, topic, max results, include/exclude domains and raw content; empty fields are omitted.
  - **M1, the review's one M:** a `usage` object without a usable numeric `credits` is no longer a measured zero. Absent, null, `{}` or `credits:null` falls back to the price list (basic 1, advanced 2, extract 1 per 5 successful URLs, rounded up); a genuinely reported number, including `0`, wins.
  - **Retry behaviour:** 429 draws four attempts and honours `Retry-After`; a host fault draws two; a non-429 4xx and an undecodable 200 are never retried. An exhausted host fault now states the budget it spent ("still failing after 2 attempts") instead of Keel's rate-limit count.
  - **New L2 note:** in a mixed storm the annotation names the host-fault budget, not total submissions (`429,500,500` reads "after 2 attempts" after three). Log text only; nothing branches on it.
  - **For T2.2b:** measure the live rate limit, whether `usage.credits` always comes back, and whether `include_raw_content` costs extra.
  - **Keel gap, issue text for the user to file:** *"throttle.Note misreports the attempt count when Transient/TransientAttempts are set: with Attempts 4 and TransientAttempts 2, an exhausted 5xx reads 'still throttled after 4 attempts' after two submissions. Have Retry/Note count the class the surviving error belongs to, or document that callers annotate transient exhaustion themselves."*
  - **Spend:** Tavily credits in P2 so far: 0 (all offline). Token Factory: none yet; T2.1b makes the first live calls.
- **T2.4 closed.** Review: [t2.4-round1.md](adversarial-review/t2.4-round1.md), APPROVE, C 0 · H 0 · M 0 · L 5.
  - **Move:** `scratch/internal/spend/{spend.go,spend_test.go}` moved from the main checkout into worktree `task-t2.4-spend` for T2.4; `scratch/internal/spend/` is gone. API: `Open(ctx, Config)`, `Spend(ctx, pool, kind, ref, estimate, work)`, `Status`, `Spent`, `Close`; `PoolModel` (USD nanodollars) and `PoolTavily` (thousandths of a credit, `CreditUnit` 1000) live in `internal/spend` until the pipeline package exists.
  - **Behaviour the wiring must not re-derive:** reserve-then-run-then-settle; an estimate the pool cannot cover refuses before work runs; a failed call frees its hold and books nothing; a measured negative price is refused; a cancelled context still books because the call ran; every charge carries the check id as its `Ref`. Two pools share the application's one database file through cost-store namespaces (`cost_model`, `cost_tavily`); a namespace ending `_owner` is refused.
  - **Forward notes:** **(a)** nothing binds `PoolTavily` to `cost.MonthlyUTC` — T2.5's settings must carry the period, and the wiring task must pin it, or 1,000 credits a month becomes 1,000 for life. **(b)** Keel's store logs to `slog.Default`; T4.4's wiring can set it. **(c)** a refused second pool leaves the first pool's tables on disk (inert). **(d)** no `Once`/idempotency: two `Spend` calls on one ref book twice; add it when T3.x has a resume path.
  - **Keel gap, issue text for the user to file:** *"cost/sqlitestore: give Store a cost.Account adapter (or give cost.Meter a context-and-reservation shape), so a durable store can back a Meter without each application reimplementing reserve-hold-settle-release by hand."* The package drives the store directly, the path Keel's own `ExampleOpen` documents.
- **T2.1 closed; T2.1a split out.** Review: [t2.1-round1.md](adversarial-review/t2.1-round1.md), APPROVE, C 0 · H 0 · M 0 · L 3.
  - **Move:** `scratch/internal/model/{model.go,model_test.go}` moved from the main checkout into worktree `task-t2.1-model` for T2.1; `scratch/internal/model/` is gone. The draft was reworked, not carried over: integer-nanodollar pricing, an unmeasured reply carries the client's estimate, 17 test functions replace the two draft ones.
  - **Behaviour T2.1b and T3.2/T3.3 must not re-derive:** price from the reported `usage`; absent, null, empty or all-zero usage is unmeasured (a paid reply must never book at $0); `extra` is merged per request and `Options.Extra` overrides it without mutating settings; a 4xx other than 429 is never retried; an undecodable 200 is never retried; OpenRouter's `error` member inside a 200 is a retryable 502 on the smaller two-attempt 5xx budget; 429 draws four attempts and honours `Retry-After` (hints clamped to 60s); the key is re-read on every attempt.
  - **Split, recorded:** review L1 — nested `Extra` values are shared between caller and client. The reviewer deferred it to "before T3.3's conflict pass", so it is opened as **T2.1a** (XS, light) rather than left implicit; it also carries L3's code-comment note.
  - **Forward notes for T2.1b:** the reviewer's L2 (a `Retry-After` above 60s is clamped and the attempt retried) and L3 (empty-`choices` 200 costs a second billed call) are to be measured and recorded there; the pacer is named per `provider/model`, which may not match an account-wide host cap; the HTTP timeout is a fixed 3 minutes against 16k-token thinking calls.
  - **Consumer pin:** `*model.Client` satisfies the scratch `Chatter` interface verbatim, and the client's estimate arithmetic matches the draft pipeline's.
- **T2.3 closed.** Review: [t2.3-round1.md](adversarial-review/t2.3-round1.md), APPROVE, C 0 · H 0 · M 0 · L 0.
  - **Move:** `scratch/internal/appstore/appstore.go` moved from the main checkout into worktree `task-t2.3-appstore` for T2.3. `scratch/internal/appstore/` is gone. New `internal/appstore/appstore_test.go`.
  - **API:** `New`, `Find`, `Reviews`, `FeedURL`, `Review.Text`; `DefaultBaseURL`, `MaxPages` (10), `PageSize` (50). Reviews carry a running `Index` from 1 across pages, so a card can say "review 312".
  - **Semantics T3.1 must not re-derive:** an empty page ends the read with what was read; a *later* page that fails (HTTP error or undecodable body) also ends the read with the reviews already gathered and no error; a *first* page failure is an error; a pre-cancelled context errors; `pages <= 0` means all ten. Reviews with no text (the app entry) take no index.
  - **Consumer pin:** the reviewer compiled the scratch `pipeline.Store` interface verbatim against `*appstore.Client`, so T3.1's gather can consume this surface as drafted.
  - **Surprising:** no captured T0.4 page has `entry` as a bare object (all 40 are 50-element arrays), so the single-entry page is pinned by wrapping a real recorded entry; T2.3b confirms the live shape. Minecraft's `sellerUrl` is null and decodes to empty, which is one half of T3.1's missing-official-host problem.
  - **Ruling recorded:** the phase graph says P2 requires P1, but the user ruled that P1 is not needed for P2: each P2 task requires only T0.1, which is done, so P2 ran alone. The P2 tasks' own `requires` lines are the binding ones.
- **T1.4 closed; P1 done.** Reviews: rounds 1–6, APPROVE in round 6. PR #4.
  - `docs/api.md` covers seven routes, every code and every stream event, plus the order of answers for `POST /api/checks`: gate, validation, card cache (a fresh card wins over `live_checks_off`, `budget_exhausted` and `busy`), join a running check, refusals, new check. `testdata/contracts/api/` holds the examples.
  - **`owns` widened** to include `internal/pipeline/api_examples_test.go`. That file generates the event streams with `wire.WriteEvent`, and generates `get-card`, `get-cards` and `get-checklist` from the contracts. Regenerate with `go test ./internal/pipeline -run TestAPIGenerate -update`.
  - Surprise, also in feedback.md: Keel's `error` event nests the whole envelope, `"error":{"error":{code,...}}`.
  - `GET /api/checks/{id}` exposes `detail`, from `Result.Detail`. `message` is the server's fixed sentence per code, never `Result.Err`.
  - **Open, owned by T4.1:** proving the answer order and the `job.Failure` exit criterion.
  - Noted: `go mod tidy -diff` is non-empty on `main`, because `modernc.org/sqlite` is not imported yet. Phase 1 did not introduce it.
- **T1.3 closed.** Reviews: rounds 1–6, APPROVE in round 6. PR #3.
  - `internal/pipeline/types.go` holds Evidence, Card, AppInfo, Stats and Alternative. `testdata/contracts/` holds `evidence.json`, `card-yes`, `card-settings`, `card-no` and `card-unclear`.
  - **`owns` widened** to include `internal/pipeline/types_test.go`. T3.4 must not edit it.
  - The examples are **generated** from the T0.4 fixtures through the real `Verify`, `Enforce` and `Decide`: `go test ./internal/pipeline -run TestGenerate -update`. Never hand-edit them. The test fails when they are stale, and a struct field missing from every example fails `TestExamplesCoverEveryField`.
  - **The user accepted `card-yes` as an example only.** Its Khan contact quote is thin, and the Khan content risk in review 17 is left out. Khan's real verdict belongs to T3.6b.
  - `card-no`'s deciding quote is review 355: in the captured feed, review 312 is not about contact. Source kinds: review, policy, terms, reddit, forum, article, news, rating, store.
- **T1.2 closed.** Reviews: rounds 1–6, APPROVE in round 6. PR #2.
  - `internal/finding/`: `Verify` matches quotes on word boundaries after `Normalize`. `Normalize` does NFC, case, whitespace, typographic quotes, every Unicode Pd dash plus U+2027, U+2043 and U+2212 folded to `-`, hyphen runs collapsed, Cf characters dropped, and the ellipsis character. An apostrophe or hyphen between word runes joins them. A risk reading wins a duplicate. Ids hash the URL, the review index, the area and the normalized quote. `Parse` also accepts prose around unfenced JSON. T3.2 can rely on that.
  - **New dependency: `golang.org/x/text` v0.41.0**, for `unicode/norm` (NFC), which the standard library lacks. v0.41.0 is the newest version that changes no other requirement in the graph: it needs `golang.org/x/sync` v0.22.0 or older, which the graph already holds, and v0.42.0 needs sync v0.23.0. Checked against the proxy `.mod` files. The round-1 note gave a wrong reason, and round 2 corrected it.
  - Surprise: the boundary rule took four review rounds (prefixes, apostrophes, hyphens, invisible characters, dash variants). `TestWordBoundaryTable` pins every case. Extend it, do not fork it.
- **T1.1 closed.** Reviews: [round 1](adversarial-review/t1.1-round1.md) through [round 6](adversarial-review/t1.1-round6.md), APPROVE in round 6. PR #1.
  - `internal/checklist/` was written fresh from the spec. The scratch draft was not reachable from the implementing session, so nothing moved out of `scratch/`. `scratch/internal/checklist/` is still there, unclaimed: delete it or compare it against the landed code.
  - **The user decided:** `unclear` is kept, and that is final. **Also the user's:** an `ok` that cites only risk findings becomes `flag`.
  - `checklist.Version` is `v7`. Each rule change in remediation bumped it, and nothing is cached yet. The final rules are in PHASE-1 T1.1, round 4 onward.
  - **Process deviations, recorded:**
    - The orchestrator session also implemented and remediated all four P1 tasks. No separate implementer agent was available. Reviews were independent.
    - Each task went through a stacked PR on GitHub (the user's preference) rather than a local-only worktree.
    - **Waived by the user:** the main-checkout pins (with `scratch/` and `.env` present), and the `.env` key-leak grep at landing. Neither could run from the remote box that landed P1. Run `./tools/check.sh` in `/home/nryn/work/before-yes` on the next pull.
    - Lambo `lambo_record_action` and `lambo_derive` were not available, so the landing is recorded here only.
- **Repository pushed.** `github.com/nrynss/before-yes` is private, and becomes public before submission (T6.5). Before the push, a scan of all 27 commits found none of the four `.env` values. The first CI run of `tools/check.sh` passed.
- **Orchestrator protocol written.**
  - [ORCHESTRATOR.md](ORCHESTRATOR.md) sets out the loop and the four separate roles. The `AGENTS.md` review section now points to it.
  - **From here on:** implementers set `in-review`, never `done`; only the orchestrator edits this README; every review goes to a fresh agent.
  - **Correction:** the T0.4, T0.4a, T0.4b and T0.7 verdicts were written by the orchestrator itself, not by an independent reviewer. The tasks stand, because their pins were run. Every later task gets an independent reviewer for load-bearing findings.
  - **The user widened the orchestrator's role:** it may amend and update review files, and fix documentation and non-load-bearing findings itself with no further review. See [What the orchestrator takes over itself](ORCHESTRATOR.md#four-roles-kept-separate).
  - [feedback.md](feedback.md) starts with the Tavily findings from Phase 0.
- **Keel v0.7.3 adopted.** v0.7.1 landed the fixes for #82 and #83, and v0.7.2 and v0.7.3 hardened them. Keel #87, #88, #91 and #94 are closed. `go.mod` on `main` now requires v0.7.3, bumped in one commit before any Keel task started, so parallel tasks cannot race on it. The "Keel v0.7.3" section names the API.
- **T0.4b closed; T0.4a closed; P0 done.** Review: [t0.4b-round1.md](adversarial-review/t0.4b-round1.md).
  - The advanced extract (7 credits) still returned no individual Common Sense reviews.
  - The Roblox and Minecraft review pages no longer exist. They redirect to parent guides, and Tavily reported the 404 text as a successful result.
  - Every Reddit thread extract failed ("Failed to fetch url").
  - **The user's decision:** snippets only. Common Sense reviews are out of the plan. The two Reddit searches stay, and each snippet is a document. The plan costs about eight credits per title.
  - Tavily spent across P0: 42 credits (T0.4 22, T0.4a 13, T0.4b 7).
  - `tools/capture`'s `csm` and `advanced` kinds are retired probes. T3.1 does not reuse them.
- **T0.4a merged as partial; T0.4b added.** Review: [t0.4a-round1.md](adversarial-review/t0.4a-round1.md).
  - `tools/capture -only csm,reddit` captures Common Sense and Reddit for each title without repeating earlier captures. 13 credits were spent.
  - A basic Tavily extract returns no individual Common Sense reviews: only a count and an unattributed summary, which must never be used as a review.
  - Searching finds the right Common Sense page only for Khan.
  - Reddit search results carry no thread text, only snippets.
  - The user chose to probe Tavily's advanced extract (T0.4b) before deciding how T3.1 uses either source. The Reddit snippets in `tavily-search-reddit-*.json` are still verbatim text and quotable.
- **T0.4 and T0.5 closed.** Review: [t0.4-round1.md](adversarial-review/t0.4-round1.md), APPROVE.
  - `testdata/titles/` holds four titles: Roblox, Khan Academy Kids, Minecraft and the Toca Life World alternative. Each has the App Store search, 10 review pages (500 reviews), five Tavily searches, an extract and a MANIFEST. `testdata/titles/README.md` holds the chosen set and its ages.
  - Tavily credits: 22 in total (Roblox 6, Toca 6, Khan 5, Minecraft 5).
  - `tools/capture` is the capture program. It copies gather.go's `plan()` rather than importing it, so keep the two in step until T3.1 can share the plan.
  - Toca's App Store name is "Toca Boca World: Game & Play".
  - The official-host rule and Khan's missing policy text are filed against T3.1 as OOS1 and OOS2.
  - Tavily's reported extract credits do not follow the price list (OOS3). Trust `usage.credits` over the price list.
  - The credit totals and hosts are in each MANIFEST.md. Do not re-derive them.
- **Sources widened; T0.4 split.** The user added Common Sense Media parent and kid reviews and wider Reddit coverage to the gather plan (T3.1, items 3 and 4). Google Play stays out: no official API, and no scraping. T0.4 was already capturing under the old plan, so the new fixtures are T0.4a. The plan now costs about ten Tavily credits per title instead of six. T2.5 sizes the budget from the measured figure.
- **T0.7 closed.** `AGENTS.md`, `tools/check.sh` and `.github/workflows/check.yml` are committed in `d0db00d` on branch `worktree-agent-a919bfa6edbb4fd6e`.
  - **AGENTS.md** links to the task rules in this README and copies none of them. It adds the stack, worktrees, file ownership, the gate, the `live` tag, secrets, commit rules, the no-dates rule and the review process.
  - **check.sh.** It runs `gofmt -l` over Go files outside `scratch/`. It runs `go vet` and `go test -race` over `go list ./...` with `scratch/` filtered out. It runs the web `lint`, `check` and `test` scripts only when `web/package.json` exists. It ends with one PASS or FAIL line.
  - **Gate proof.** Each case ran in this worktree with a throwaway package, and both were deleted afterwards.
    - Clean tree: PASS, exit 0.
    - `tmpgate/bad.go`, badly formatted: FAIL at gofmt, exit 1. `go vet` and `go test` were not reached.
    - `tmpgate/x_test.go` asserting `1 != 2`, correctly formatted: FAIL at `go test -race`, exit 1.
    - `tmpgate/x_test.go` passing: PASS, exit 0.
    - `scratch/zz/` with badly formatted code and a failing test: PASS, exit 0. The draft is excluded from gofmt, vet and test.
    - After both were removed: PASS, exit 0.
  - **Surprising: no packages.** With no Go packages, `go vet ./...` and `go test -race ./...` both exit 1 ("no packages to vet" and "no packages to test"). `go list ./...` exits 0 with a warning. The script skips vet and test only when the filtered package list is empty. A real vet or test failure still fails the gate.
  - **Surprising: `scratch/` is in scope for `./...`.** `go list ./...` returns `scratch/` packages when the directory exists. Without the filter, the gate would vet and test the unreviewed draft.
  - **Surprising: a draft import can break listing.** Once the real draft sits in `scratch/`, its files import `github.com/nrynss/before-yes/internal/...`, which does not resolve at the root. A plain `go list ./...` then exits non-zero before the filter runs, and the gate failed on main. The script now lists with `go list -e`, filters `scratch/`, and vets and tests what remains. A load or compile error in a real package still fails the gate.
  - **CI has not run.** There is no remote. The YAML parses. `actions/setup-go` reads `go.mod`, so it must supply Go 1.27.1. That version was not checked against the action's manifest. Node 24 and pnpm 11 are set up only when `web/package.json` exists.
  - **Branch name.** This task's branch is `worktree-agent-a919bfa6edbb4fd6e`. The `task-<id>-<slug>` rule in AGENTS.md would name it `task-t0.7-repo-conventions`. Rename it if the convention should hold for this task.
  - **Keel version.** `go.mod` still names Keel v0.7.0, and BRIEF.md says v0.7.0. The v0.7.1 bump belongs to the first Keel task. T0.7 did not change `go.mod`.

- **T0.6 closed.** The user checked the name. "Before Yes" stays.
- **Keel v0.7.1 adopted.** The user confirmed v0.7.1 will fix #82 and #83 and be the last Keel change. The "Keel v0.7.1" section above gives the bump rule. T1.4, T2.4 and T4.1 now target the fixes, not the workarounds.
- **T0.1, T0.2, T0.3 closed.**
  - **T0.1 (repository).** `go.mod` and `go.sum` moved from `scratch/` to the repository root in the main checkout at `/home/nryn/work/before-yes`, for T0.1. Both are committed in `1aa4bf9`. `scratch/go.mod` and `scratch/go.sum` are gone. Nothing under `scratch/` and no `.env` is tracked. `.gitignore` covers `scratch/`, `.env`, `.env.*` with `!.env.example`, `data/`, `*.db`, `*.db-wal`, `*.db-shm`, `web/build/`, `node_modules/` and `before-yes.toml`. No entry was missing.
  - **`.env`.** Mode `0600`, and ignored by git. Variable names only: `NVIDIA_KEY`, `NEBIUS_KEY`, `TAVILY_KEY`, `OPENROUTER_KEY`. Quoted-value lines: 4 (all four names above). `export`-prefixed lines: 0. The user must remove the surrounding quotes from those four lines before any secret reference resolves. Keel's `env_file` expects bare `NAME=value` lines. The user should also remove any `export ` prefix, if one is added later. No value was printed or committed.
  - **T0.3 (settings and keys).** `before-yes.toml.example` and `docs/keys.md` committed in `f710055`. The example gained top-level `judge_fast` and `judge_think` keys, which had been missing. Their values are UNCONFIRMED until T2.1b. The commented OpenRouter tables now say to repoint `[secrets.reader]` and `[secrets.judge]` at `OPENROUTER_KEY`. `docs/keys.md` now says the same.
  - **T0.3 deferred.** The check that every secret resolves through `before-yes config` cannot run yet. `before-yes config` is scratch code owned by T4.4. Deferred to T4.4.
  - **T0.2 rests on the user's confirmation.** The user confirmed the Token Factory account is active. The credit balance is not yet recorded. T2.1b's live call will prove Nano works.

- **Planning.** Phase documents written. The backend draft moved to `scratch/` (see [Starting from the scratch draft](#starting-from-the-scratch-draft)). Notes from that draft, for whoever picks up the tasks it touches:
  - Token Factory base URL is `https://api.tokenfactory.nebius.com/v1`. Model ids `nvidia/nvidia-nemotron-3-nano-30b-a3b` and `nvidia/nemotron-3-super-120b-a12b` come from public examples, not Nebius docs. T2.1b confirms them against `GET /v1/models`.
  - Token Factory serves Nano with a 32k context and 4k output tokens, so reader chunks stay near 4k input tokens.
  - Apple's review feed was checked live (Roblox, id 431946152): 50 entries a page, review text in `content.label`, rating in `im:rating.label`.
  - Keel gap, filed as [nrynss/keel#82](https://github.com/nrynss/keel/issues/82): `cost/sqlitestore` uses fixed table names, so two budget pools cannot share one database file. The draft keeps one file per pool. Keel v0.7.1 fixes it, and T2.4 removes that workaround.
  - Keel gap, filed as [nrynss/keel#83](https://github.com/nrynss/keel/issues/83): a failed job's terminal event always carries `job_failed`. Keel v0.7.1 fixes it, and T1.4 and T4.1 use the fix. The draft's result-data workaround below is obsolete. The draft returns a typed outcome in the job's data, which Keel keeps on failure, and `GET /api/checks/{id}` surfaces it.
