# Development Diary: Phase Handoffs

This document breaks down the work for **Before Yes**, the Nebius × NVIDIA Global AI Hackathon entry.

Reference specification:
- [`../BRIEF.md`](../BRIEF.md): product, checklist, architecture, models, budget, guardrails, demo.
- [`ORCHESTRATOR.md`](ORCHESTRATOR.md): how the orchestrator runs the loop of pick, dispatch, review, land and close, with the brief template.
- [`feedback.md`](feedback.md): friction with Token Factory, Nemotron, Tavily, Keel and Chaaya. It feeds the Devpost feedback.
- `~/work/hackathons/nebius-nvidia.md`: event rules, prizes, and why this concept was chosen.

Where a phase document and the brief conflict, the brief wins. Record the discrepancy in the handoff log of the phase that found it.

Each phase document gives an engineer starting cold the complete context for its tasks.

**Current status:** P0, P1, P2 and P3 are done. P3 closed with T3.10: every standing grade now rests on its area's own flag definition, enforced by an entailment check; the demo titles read Roblox **no**, Minecraft **yes_with_settings**, Khan Academy Kids **unclear** (per-area table in the T3.10 handoff entry). P4 is 2 of 5 and P5 is 3 of 7; both tracks are live. An earlier backend draft sits in `scratch/` at the repository root, uncommitted and unreviewed. Tasks it covers show `status: drafted`, which means there is code to start from, not that the task is done. See [Starting from the scratch draft](#starting-from-the-scratch-draft).

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
| P2 | 12 / 12 | Done. All six exit criteria are met. Measured model cost is $0.0309 per check. Tavily is 7–8 credits per check, from fixtures, not a live run. |
| P3 | 13 / 13 | Done. Every standing flag and setting now meets its area's own definition, enforced by a per-grade entailment check (T3.10). Verdicts: Roblox **no** (four earned flags), Minecraft **yes_with_settings** (matches T0.5, with a recorded caveat on two settings), Khan Academy Kids **unclear**. Re-pick is the user's call. |
| P4 | 2 / 5 | T4.1 and T4.2 are done: check jobs, the progress stream, card routes, the card store and both caches. Wire ids are `c_` / `j_` + a 32-hex Keel id. T4.3 and T4.5 can start; T4.4 is drafted and must wire `Judge.Entail` (T3.10). |
| P5 | 3 / 7 | T5.1, T5.2 and T5.4 are done: the shell, the Ask form with free examples, and the evidence-linked card. T5.3 is blocked on an upstream Chaaya fix (terminal snapshot recovery), in progress. T5.5 can start; T5.6 waits on T5.3, T5.7 on T4.5. |
| P6 | 0 / 6 | Not started. |

---

## Handoff Log

Newest first. One entry per closed task, finding, or split.

- **T3.10 closed: judge calibration.** Implemented by two ZCode sessions (the first died on a usage limit mid-task; the second finished it offline and ran the user-approved entailment re-record). Reviews: [round 1](adversarial-review/t3.10-round1.md) REMEDIATE (M 1, L 4), remediated; [round 2](adversarial-review/t3.10-round2.md) **APPROVE** (C 0 · H 0 · M 0 · L 2, both prose, closed by the orchestrator and recorded in the review file). Zero residue.
  - **What landed:**
    - Per-area flag and ok definitions (`areaDefs` in judge.go, told to the judge in the prompt and read by the entailment step from the same text, so the two cannot drift). The proportion rule: a flag is a serious concern, not whatever can be cited; a feature that merely sits in the area, a price, an email field, a sibling product or another company's story meets no definition.
    - `offProduct` (gather.go): documents whose subject names a sibling App Store product of the same seller, without naming the matched product, are dropped before reading and counted in `Gathered.OffProduct` → additive `Stats.off_product` (all seven contract examples). The title's own reviews and store page are never dropped. Khan drops 10 pages (the general-Khan reddit threads, the Internet Matters and Common Sense "Khan Academy" guides); Roblox and Minecraft drop 0 (brand is the product). Limitation recorded: a prefix sibling ("Minecraft Education" beside "Minecraft") is never dropped, because its name contains the product's.
    - The entailment check: one metered Nano call per standing flag or ok_with_setting (`Judge.Entail`, step `entail`, wired to the reader client in tools/eval only — **T4 must wire it in the server**). It asks whether the cited findings meet the area's definition for this age and whether the reason says only what the quotes support; either false → unknown, thin area, demotion `flag_not_earned` / `reason_unsupported`. A failed call keeps the grade (`EntailErr`), so a rate limit cannot sink earned flags.
  - **The entailment clarification (user-approved spend).** The first round's entailment replies refused Roblox's earned developer_record flag (the LA County lawsuit naming Roblox Corp) — a misread of "record of enforcement" as requiring an outcome. One sentence was added to the entailment instruction (a fine, breach, lawsuit or regulatory action *filed* against the developer is the record; the quote need not show its outcome), scoped to that one definition, and only the 11 entailment replies were re-recorded: **$0.0005**. The flag returned; every other answer was unchanged. No blind re-rolls: had the clarification not moved the model, the demotion would have stood.
  - **Verdicts and the per-area table** (grade — cited quote, short — human check):
    - **Roblox (9): no.** money flag — "Games may contain optional in-game purchases, including lootboxes" — earned. age_gating flag — "can simply enter any birthdate before September 2007" — earned (the weighting rule). content flag — first-hand reports of predators and a kidnapping attempt — earned (weakest). developer_record flag — the LA County civil lawsuit naming Roblox Corporation — earned. contact unknown — honest fall: the round's cited quotes show predators on the platform, not strangers reaching this child (T3.9b's R355 quote was not re-found; its finding was dropped as unquoted because the reader joined sentences across a markdown link). data unknown — correct fall (email field and moderation lines are excluded by the definition). Thin: contact, data.
    - **Minecraft (9): yes_with_settings** — matches T0.5 for the first time. Thin: none. contact "friends only", money "Control Spending", age_gating "multiplayer starts off" (on a child account), content "single player" — each setting named by a cited mitigation quote. **Caveat (recorded in feedback.md):** the contact and age_gating risk-side citations are weak or off-topic (a neutral gameplay paragraph; "Play with your friends on your own personal server.") and the entailment check accepted both grades (`meets_definition`/`reason_supported` true, before and after the clarification); the card's reasons for those two areas say more than their cited quotes show, and the verdict rests on that acceptance. Defending them would be a further prompt change and re-record — not asked.
    - **Khan Academy Kids (5): unclear** (T0.5 expected yes). Thin: age_gating, content. All six areas unknown, each honestly: four ok grades cited nothing (`ok_without_finding`), the age_gating setting was named by no quote (`setting_without_quote`), and the content flag on two odd reviews was refused by the entailment check — fault 1 working as specified. `off_product` 10; no sibling price is cited anywhere.
  - **Numbers (replay of the final recordings, 0 misses):** grades agree 9/18, decisive quotes 2/16. Per check, Token Factory, mean of three: **$0.040580** (Roblox $0.040243, Minecraft $0.042595, Khan $0.038903); the entailment calls are $0.000162 of it (0.4%). Budget: 492 checks under the $20 ceiling, reserve 123; no ceiling or cap changed. Live spend for T3.10 in all: **$0.209723** (three recording rounds from the first session's prompt sharpening, then the $0.0005 clarification round), over the $0.10–$0.15 estimate; lifetime live spend about $0.4102. Two conflict passes (Roblox, Khan) ran to the 16,000-token cap and were discarded.
  - **Reader findings worth knowing (feedback.md has all seven):** Khan's reader emitted 51 duplicate findings from one reply re-emitted up to 6×; two whole-cap conflict passes returned no visible text; the judge now copies the area definition verbatim as its reason for standing flags (no title-specific reason on the card); one Roblox reader reply joined sentences across a markdown link, costing the R355 quote.
  - **Open items:** the demo titles' re-pick is the user's decision (Roblox agrees with T0.5; Minecraft now matches; Khan reads unclear, not yes — the evidence on the captured fixtures does not earn its expected grades). T4 wires `Judge.Entail`. The P3 exit criterion "Alternatives found with Tavily" stays open: T3.5 is tested with a fake search only; no live Tavily alternatives search has run. The other three stale criteria (eval results recorded, four kinds, store-page reviews) are ticked now that T3.10 closed.
  - **Validation:** gate rc 0 in the worktree (three times: pre-review, post-remediation, post-rebase) and on main after the merge, with `scratch/` and `.env` present. All four key values grep 0 hits over the tree. Reviewers re-ran the eval, mapped every entailment reply to its grade, and diffed the recordings against the round-1 branch (only the 11 entailment replies changed). Lambo tools were not available, so the landing is recorded here only.
  - **Process note:** main moved three times during landing (T5.2, T5.4 and their follow-ups from the other orchestrators). Each time the branch was rebased and the ff-only merge redone atomically; no conflict ever reached code.

- **T5.4 closed (PR #23).** The frontend orchestrator implemented it; round 1 found one low, round 2 approved, and the orchestrator then held it on card ids. Round 3 approved with zero residue.
  - **What landed:** `web/src/lib/card/` and `/card/[id]`: the verdict, six areas with their lead quotes, sources you can follow, checked alternatives, and sharing below the evidence. `contract.ts` parses a card strictly and accepts only `c_` + 32 lowercase hex, the server's shape. A malformed id shows "Card not found" with no request.
  - **Contract ids re-minted:** every `c_` / `j_` id in `testdata/contracts/` is now real-shaped (`c_0000…0001`), with the Go and web tests that quote them. The 22-character ids remain only in rejection tests. This closes the contract-id gap from the T4.1/T4.2 entry.
  - **Pins:** the gate passed on main after the merge (64 Playwright tests). The reviewer served a fixture card under a real-shaped id at 360px and saw the legacy id refused without a request.
  - **Leftover for the frontend:** T5.2's example list still accepts age 2; harmless now that no card can carry it.
- **T4.1 and T4.2 closed (PRs #22 and #20).** A remote orchestrator implemented both; fresh reviewers approved T4.2 in round 3 and T4.1 in round 1. The gate passed on main after each merge.
  - **T4.2:** `internal/cards` stores cards under 32-hex Keel ids, with `Put`, `PutWithAlternatives`, `Get`, `Latest` and `Recent`, migrations in namespace `beforeyes_cards`. `internal/server/cache.go` has the evidence cache (single flight over `pipeline.EvidenceKey`; thin or failed evidence is never stored) and the card cache.
    - Round 1 found thin evidence written over an expired row (High), fixed and pinned with a direct sqlite read.
    - The orchestrator held the first version: it served bare 32-hex ids, against the frozen contract (`docs/api.md`: a card id starts `c_`). Round 3 made public ids `c_` + hex everywhere they leave the package.
  - **T4.1:** `internal/server/checks.go` serves `POST /api/checks`, `GET /api/checks/{id}`, its events stream, `GET /api/cards` and `GET /api/cards/{id}`. Check ids are `j_` + hex. Bad ids are a 404 before any query. Failures carry `budget_exhausted` (with `detail.pool`), `no_sources` or `check_failed`, never `job_failed`. The `Checks` seam for T4.5 is declared, not wired.
  - **Scratch moves** on `draft/scratch`: T4.2 took `scratch/internal/cards/` (868d653). T4.1 took `scratch/internal/server/server.go` (604fdac) and split the remainder into `server.T4.3.go` (gate) and `server.T4.4.go` (status, checklist, static routes) for those tasks.
  - **Age range, resolved:** `docs/api.md` said 2 to 17, `pipeline.MinAge` is 3, and the 400 message said "between 2 and 17". The user chose 3 to 17; the orchestrator aligned the doc, the message, its test and `post-checks-400.json`.
  - **Contract gap, open:** the T1.4 fixtures use 22-character made-up ids (`c_7Hq2mXv9pK3wLr8sTn4bYe`); the server emits `c_` + 32 hex. Frontend id checks must accept the server's shape.
  - **Feedback:** two Keel entries: `cache.GetOrMake` hides whether a caller joined a flight, and `wire.WriteError` sets `Retry-After` only for 429.
- **T5.2 closed (PR #19).** The frontend orchestrator implemented it; a fresh reviewer returned APPROVE with zero findings (`adversarial-review/t5.2-round1.md`).
  - **What landed:** `web/src/lib/ask/`: the Ask form (title, whole age 3–17, one button) and the free example cards from `GET /api/cards`. A `200` opens the card, a `202` the progress view, `passcode_required` opens a Bits UI passcode dialog that keeps the question, and `live_checks_off` points at the examples. Refusal messages live in `messages.ts`.
  - **Pins:** the gate passed in the task checkout and on main after the merge, including 30 Playwright tests and the axe gate in both themes. The reviewer drove each T1.4 fixture in a browser and checked 360px.
  - **Note for T5.3 and T4.5:** the form accepts ages 3–17; the frozen API still accepts 2–17, and the example-card check accepts 2–17 so older cards still show.
- **T3.10 filed: judge calibration**, from the orchestrator's reading of T3.9b's replay. Three faults:
  - Flags that don't meet their area's definition (Khan: comments, an email address, two odd reviews).
  - Pages about a sibling product counted as evidence (Khan: another product's subscription and school prices).
  - An invented reason on a real citation (Minecraft: "regulatory fines" from a marketing line).

  The fix needs a live re-record, about $0.10 to $0.15. Demo titles are not re-picked until it lands.
- **Process note:** the frontend orchestrator commits its claims ("Claim T5.1 shell", "Claim T5.2 ask") on `main` in the main checkout, which the local orchestrator also uses. Both were pushed with the orchestrator's next push. A claim belongs on the task's own branch, and an agent works in its own worktree (AGENTS.md: two agents never edit one checkout).
- **T3.9b closed.** Sonnet implemented it, and the orchestrator validated it with no review loop.
  - **What landed:**
    - The judge's prompt asks it to cite the few findings that decide each area.
    - `checkCitations` (judge.go): a `flag` or `ok_with_setting` stands only if it cites at least one id, and every id is a verified finding in that area with a fitting stance. Otherwise the grade becomes `unknown`, and the area is listed in `thin_areas`.
    - The setting-naming check from T3.9 stays. T3.9's verbatim-run check on the reason is dropped: on the recordings it rejected 7 of 11 correctly cited grades.
  - **Re-recorded live on Token Factory,** with the reader at 8192 tokens: $0.106.
    - Replay has 0 misses.
    - Replies at the cap went from 4 to 1, and unquoted drops from 23.3% to 21.5%, within run-to-run noise, so the cap barely matters.
    - Cost per check is now **$0.0353** (Roblox $0.034, Minecraft $0.047, Khan $0.025). `docs/budget.md` is updated: 565 checks under $20, reserve 141. No ceiling changed.
  - **Results:**

    | Title | T3.6b / T3.9 / T3.9b | Grades agreed | Notes |
    |---|---|---|---|
    | Roblox, 9 | no / unclear / **no** | 3/6 | Matches T0.5. Age gating and developer record flags are cited. Contact and money are `ok_with_setting`. |
    | Minecraft, 9 | no / unclear / **no** | 1/6 | The developer_record flag cites a marketing line, but its reason claims "regulatory fines": invented. |
    | Khan Academy Kids, 5 | no / unclear / **no** | 2/6 | Four flags rest on real but weak quotes: comments, an email address, two odd reviews, and a subscription price likely from another Khan product. |

  - **Orchestrator assessment:** citations are now structurally sound. The remaining error is in judging.
    - The judge flags whatever it can cite, with no proportion.
    - An invented reason can still sit on a real id (L1).
    - Gathering can pull in pages about a sibling product.
    - Demo titles are not re-picked on these verdicts.
  - **Validation:**
    - The replay was reproduced here, and the per-area reasons were read.
    - Gate rc 0 in the worktree after the rebase, and on main. No key values found. No dates.
    - Outside owns: the `ruleJudge` test stub, `feedback.md` entries, and a replies README note.
- **T5.1 closed (PR #18).** Implemented by a remote orchestrator. Round 1 was APPROVE, with no findings and no residue.
  - **What landed:** `web/` with Svelte 5 (runes), SvelteKit on Vite with `adapter-static`, Chaaya 0.4.1 and Bits UI.
    - Light and dark themes, plus system-following, persisted.
    - Four placeholder routes: `/`, `/check/[id]`, `/card/[id]`, `/compare/[a]/[b]`. They make no API calls.
    - The static build writes `web/build/` with an `index.html` fallback. T4.4 serves the assets and uses that fallback for app URLs.
  - **The gate's web steps:**
    - `lint` (ESLint) and `check` (svelte-check: 0 errors, 0 warnings).
    - `test`: Node theme tests, including Chaaya's contrast gate for both themes; the build; `playwright install chromium`; and 10 Playwright tests. Every route renders at 360 px in both themes with no horizontal scroll and passes Chaaya's accessibility gate.
  - **Dependencies:** the SvelteKit toolchain, Playwright (real browser layout) and axe-core (Chaaya's accessibility peer). No other production dependency.
  - **Orchestrator verification:**
    - The full gate ran on the branch on this machine (rc 0, 46 s; Node 26, pnpm 11), and again on `main` after the merge (rc 0). No key values found. No dates.
    - Playwright warns that this OS is not officially supported and uses its Ubuntu fallback browser, which passed.
    - `playwright install chromium` runs in every gate. It is cached after the first run, but a fresh machine needs network once.
- **T3.9 closed (PR #17); T3.9b filed.** Implemented by a remote orchestrator. Rounds 1 and 2 were REMEDIATE, and round 3 was APPROVE with zero residue and 3 Lows accepted.
  - **What landed:**
    - `ground` runs after `checklist.Enforce` and before `Decide`.
      - A `flag` needs a named risk finding. An `ok_with_setting` needs a named risk or mitigation finding, and a cited mitigation quote that contains every content word of the setting.
      - A reason "names" a finding when it carries a run of 4 consecutive words of the verified quote, at least 2 of them content words, NFC-normalised.
      - A failing grade becomes `unknown`, and the area goes in the new additive card field `thin_areas`.
    - The verdict rule is unchanged. The judge's prompt and request are unchanged, so the recordings replay with 0 misses.
    - The reader's `max_tokens` example goes from 4096 to 8192. That is not measured yet.
  - **Replay:** Roblox, Minecraft and Khan Academy Kids all read **unclear**, with grades 5/18 and quotes 2/16. Super paraphrases and never quotes, so no recorded flag or setting survives. This includes Roblox's contact area, which a real quote backs.
  - **Outside owns, accepted:** `tools/eval/standin.go` (the stand-in judge now quotes), the `ruleJudge` test stub, and one line in `pipeline.go` copying `thin_areas` onto the card. The stand-in baseline is unchanged at 7/18 and 6/16.
  - **Orchestrator verification:** gate rc 0 on the branch and after the merge into main, with `scratch/` and `.env` present. Replay was reproduced here. No key values found. No dates.
  - **T3.9b filed:**
    - The judge cites finding ids per area, and a grade stands only on valid, verified, in-area citations with a fitting stance.
    - Then a live re-record on Token Factory (about $0.10 with the reader at 8192 tokens) and a budget update.
    - The demo titles are re-picked only after that, by the user.
- **T3.8, T3.7 and T3.5 follow-ups closed** (PRs #13, #14, #15, stacked, implemented by Grok). They carry out the user's four decisions. Final reviews: T3.8 follow-up APPROVE; T3.7 round 3 REMEDIATE, fixed; T3.5 rounds 6 and 7 REMEDIATE, fixed. #15 merged into `task-t3.7-kinds`, and #14 carried both into `main` after a merge with `main`. The full handoffs are in the PR descriptions.
  - **#13, T3.8:** a review feed that errors falls back to the store page. `review_source` says `page`. The feed error stays a failure, so that evidence is not cached.
  - **#14, T3.7:**
    - An explicit `unknown` kind with a general plan: news, forums and the shared rating search, with no ESRB/PEGI search and no policy or terms search.
    - `Card.kind_label` ("kind unknown") is new and additive, with a new example, `card-unknown-kind.json`. `GatherVersion` covers every kind.
    - The policy search still runs, and is paid for, before the kind is known.
  - **#15, T3.5:**
    - Each fresh alternative check reserves one `checks_daily` check through `Pipeline.Checks`, with `Done` or `Fail`. Cached ones run free. A cap refusal marks the candidate `refused`.
    - A card carries only checked alternatives, plus `alternatives_note` when any was refused. An `Alternatives` slice type filters the JSON, so pending and refused names never reach a card or an API response.
  - **Outside owns, accepted:** `types.go` (the kind label, the note and the `Alternatives` type), `pipeline.go` (the `Checks` field and the kind label in `Decide`), and the contract fixtures, all additive.
  - **For P4:** T4.5 sets `Pipeline.Checks` to the server's `*spend.Meter` and reserves only the main check (PHASE-4 T4.5, feedback.md).
- **T3.8, T3.7 and T3.5 closed** (PRs #10, #11, #12, stacked, implemented by Grok). Final reviews: T3.8 round 2 APPROVE, T3.7 round 2 APPROVE, T3.5 round 5 APPROVE. The full handoffs are in the PR descriptions.
  - **What landed:**
    - **T3.8:** App Store page reviews as documents, deduplicated, with editorial blurbs and developer responses dropped. They are used only when the feed comes back empty. `review_source` (`feed`, `page` or empty) is on Evidence and Card, added to the contract fixtures.
    - **T3.7:** `DetectKind` and `PlanFor(kind, …)`. The kind is in the evidence and in the evidence-cache version.
      - YouTube question answered from documentation: made-for-kids status is a narrow signal, not adopted. If used later, it is small print beside the kind and is never graded.
    - **T3.5:** `WithAlternatives` checks up to 3 candidates after the main card, each a full cached check under its own ref, with statuses `pending`, `checked` or `refused`. A budget refusal leaves the main card byte-identical.
  - **Orchestrator verification:**
    - Merged locally in order. Gate rc 0 after each merge, with `scratch/` and `.env` present. No key values found. No dates.
    - Edits outside owns were accepted as hooks: `pipeline.go` passes `ReviewSource` (T3.8), and carries the kind and a kind-aware `GatherVersion` (T3.7).
    - One conflict in `feedback.md`, where both sides only added entries, was resolved by keeping both.
  - **Decided by the user** (landed in PRs #13–#15; see the entry above):
    1. A feed error falls back to the store page, as an empty feed does.
    2. A title the detector cannot place gets an explicit `unknown` kind, not `console_pc`.
    3. Fresh alternatives count toward `checks_daily`, and cached ones run free.
    4. Refused alternatives are hidden. The card shows one line instead: "More alternatives when today's checks reset".
- **T2.5 closed; P2 done.** Haiku implemented it, and the orchestrator validated it with no review loop.
  - **What changed:** `docs/budget.md` uses T3.6b's measured figures. Model cost per check is $0.030861, the mean of Roblox $0.031991, Minecraft $0.040029 and Khan $0.020564, re-summed from the 109 recorded replies.
    - The $20 ceiling covers 648 checks, and the $5 reserve covers 162.
    - Tavily's 900 credits cover 112–128 checks a month, at 7–8 credits each. That figure is the pipeline's plan replayed on fixtures, not a live run.
  - **No ceiling changed:** $20, 900 Tavily credits a month, and 5 fresh checks per UTC day. 5 × 15 × 8 = 600, within 900. At Minecraft's cost ($0.040) the model covers 499 checks.
  - **Corrected:** the judge's totals already include the conflict pass, so the formula no longer adds it twice. T3.9's re-measure formula is in the doc.
  - **Validation:**
    - The arithmetic was re-checked.
    - The diff stays inside owns. Gate rc 0 in the worktree and on main. No key values found. No dates.
- **T3.6b closed; finding T3.9 filed.** Haiku implemented it, and the orchestrator validated it with no review loop.
  - **What landed:**
    - `go run ./tools/eval -models live` runs every reader and judge call on a real host. It has a spend cap, a ledger outside the repository, and a dry run. It never runs in the gate.
    - Token Factory replies are recorded in `testdata/eval/replies/<slug>/`, and the OpenRouter stand-in replies in `testdata/eval/replies/openrouter-free/<slug>/`. Replay reproduces the live numbers exactly.
    - `go run ./tools/eval` now replays the Token Factory recordings by default.
    - A small hook in `tools/eval/main.go`, outside owns, was accepted at landing: the judge's thinking switches are passed in, so replay keys match production.
  - **Results:**

    | Title | Token Factory | OpenRouter free | T0.5 expected |
    |---|---|---|---|
    | Roblox, 9 | no; grades 3/6, quotes 1/6 | no; 1/6, 3/6 | no |
    | Minecraft, 9 | no; 0/6, 1/5 | no; 3/6, 1/5 | yes with settings |
    | Khan Academy Kids, 5 | no; 2/6, 0/5 | no; 0/6, 0/5 | yes |
    | Total | 5/18, 2/16 | 4/18, 4/16 | |

    The stand-in baseline was 7/18 and 6/16.
  - **Measured cost:** $0.0926 in all on Token Factory, of a $1.50 cap. No live Tavily credits were spent; the runs read fixtures.
    - Per check: reader about 129.7k tokens in and 27.8k out; judge about 27.7k in and 9.0k out, with one conflict pass.
    - That is **$0.0309 of model spend per check**, against `docs/budget.md`'s estimate of $0.0255.
    - Fixture captures put Tavily at 7–8 credits per title.
  - **Orchestrator finding:** replaying the recordings shows the **no** verdicts are not evidence-backed.
    - Most areas' grades cite no verified quote.
    - The reasons invent details, such as a forum Khan Academy Kids does not have, and Minecraft data "cannot be deleted" with no quote.
    - Many findings were dropped as unquoted (Khan 34, Minecraft 70, Roblox 60), and 15 replies hit the 4,096-token cap.
    - So the demo titles are **not** re-picked yet. T3.9 makes every grade rest on a verified finding, then re-runs the replay.
  - **Validation:**
    - The diff stays inside owns plus the accepted `main.go` hook.
    - Live code runs only under `-models live`.
    - Gate rc 0 in the worktree after the rebase, and on main. No key values found. No dates.
- **P3 landing verified on this machine.** After the remote close-out, the orchestrator ran the checks the remote box could not.
  - **Pins:**
    - The gate passes in the main checkout with `scratch/` and `.env` present (rc 0).
    - The leak check found 0 tracked and 0 working-tree files containing any `.env` value.
    - All five round-3 reviews read APPROVE.
    - No dates in the dev-diary.
  - **Integration with work that landed meanwhile:** T3.8a's `-only storepage` capture survives in `tools/capture`. The pipeline's model calls go through `Meter.Spend`, so a timed-out call books at its estimate (T2.4b).
  - **Docs corrected:**
    - T2.5 is blocked on T3.6b, not T3.6. The measured cost per check comes from the live quality pass, as T3.6b's block says. `docs/budget.md` now says so too.
    - T4.5 now says how to wire the daily cap: `BeginCheck` on a cache miss, before the first paid call. No pipeline code calls it yet.
  - **Remote branches:** `task-t3.1-gather` to `task-t3.6-eval` all point at `main`'s tip and can be deleted.
- **T3.1, T3.2, T3.3, T3.4 and T3.6 closed** (PRs #5–#9, stacked). Three review rounds: round 1 and round 2 were remediated, and round 3 approved all five.
  - **What landed in `internal/pipeline`:**
    - Gather: a 7-search plan; the official host from the App Store seller URL or a policy hit, using the public suffix list, with shared platforms refused; policy and terms extraction that follows homepage links only on the official host; a not-found page rule pinned to the captured Common Sense 404s; `Gathered.Failures` separates failed calls from absent sources.
    - Read: chunked and metered; quotes checked by `finding.Verify`; unique review refs.
    - Judge: the last grades block wins, and a truncated final block is an error; a conflict pass runs with thinking on.
    - Pipeline: `Collect`/`Decide`, and an evidence cache. The key holds hashed gather and reader versions, the storefront taken from the store, and the reader model. Evidence is not cached when any reader chunk failed or any source call errored.
    - `tools/eval`: stand-in models and a replay mode.
  - **Baseline (stand-ins, offline; pinned by `TestStandinBaselinePinned`):**

    | Title | Grades agree | Decisive quotes found | Chunks |
    |---|---|---|---|
    | Roblox, 9 | 4/6 | 3/6 | 36 |
    | Minecraft, 9 | 0/6 | 2/5 | 45 |
    | Khan Academy Kids, 5 | 3/6 | 1/5 | 22 |
    | **Total** | **7/18** | **6/16** | |

    These are stand-in numbers. The replies in `testdata/eval/replies` are hand-written, which the user accepted, and T3.6b must replace them with recorded ones.
  - **New dependency:** `golang.org/x/net` (for `publicsuffix`). A hand-kept two-label rule gets multi-label suffixes and shared hosts wrong. It was already in the module graph at v0.58.0 through keel, so go.mod gains one require line and go.sum two lines, with no version changes.
  - **Open for T4:**
    - Evidence for a title whose page never reads (Khan's homepage) is never cached. It needs a short TTL rather than none.
    - Cache-hit cost: a hit books nothing new. Its `Stats` keep the collect cost paid earlier, so T4 should show judge cost as the new spend and collect cost as "already paid".
  - **Low notes from the T3.1 round-3 review:** support and link hosts (helpshift.com, zendesk.com, bit.ly, onelink.me, page.link) are not yet on the shared-platform denylist. `errorPage` drops a real page that carries a stray standalone "Page not found" line (the safe direction; no fixture hits it).
  - **Not run:** the main-checkout pin with `scratch/` and `.env`, and the `.env` leak grep. Neither was reachable from the remote box, so both must run on the user's machine.
  - **PRs:** to get GitHub to mark the stacked PRs #6–#9 merged, each task branch was fast-forwarded (not forced) to `main`. The branches are kept, and all now point at `main`'s tip.
- **P2 exit criteria checked; branches and worktrees cleaned up.**
  - **P2:** five of six exit criteria are met and ticked in `PHASE-2-clients.md`. The sixth, the measured cost per check, waits on T3.6, which T2.5 is blocked on.
  - **Removed at the user's request:**
    - OpenCode's four P2 worktrees and branches (`task-t2.1b-model-live`, `task-t2.2-tavily`, `task-t2.2b-tavily-live`, `task-t2.3b-appstore-live`). Each was clean, and `git cherry` showed no commit missing from `main`. Every file difference was newer work on `main`.
    - The local branch `task-t2.1a-extra`, which `main` already contains.
    - The P1 branches on GitHub (`task-t1.1-checklist` to `task-t1.4-api`), which `main` already contains.
  - **Left alone:** `draft/scratch`, and the five P3 branches on GitHub (T3.1–T3.4 and T3.6), which are active remote work.
- **T2.4b closed.** Haiku implemented it, and the orchestrator validated it with no review loop. It went back once.
  - **What landed in `internal/spend`:**
    - A `checks_daily` pool (unit `check`, `cost.DailyUTC`), with `BeginCheck(ctx, ref)` and then `Done` or `Fail`. A cache hit never calls it.
    - `Open` pins tavily to `MonthlyUTC` and the checks pool to `DailyUTC`, whatever the caller passes.
    - `Spend` books a work that hit its own `context.DeadlineExceeded` at the estimate, unmeasured, while the caller's context is live. A caller's cancel or expired context still releases.
    - `Config.Now` injects the clock for tests.
    - `daily_fresh_checks = 5` is live in the example settings.
  - **Sent back once:** holds used Keel's default 10-minute reservation TTL. A model call can run up to 15 minutes (T2.1c), and a check can run longer, so a hold could lapse mid-call. A sixth check could then start, or concurrent calls could pass the dollar ceiling. Keel's `settleBooks` books a lapsed hold correctly (read from source), so only the in-flight protection was lost. The fix is `Config.ReservationTTL`, default 30 minutes, on every pool. Two clock-injected tests prove holds still block at 20 minutes and lapse after 30.
  - **Orchestrator validation:**
    - Read the diff.
    - Confirmed Keel's expiry and settle behaviour in its source.
    - All 18 spend tests pass under `-race`, with no sleeps.
    - The diff stays inside owns, plus one line in the settings example.
    - Gate rc 0 in the worktree after the rebase, and on main. No key values found. No dates.
  - **Left open, for the pipeline task that wires the meter (T3.4):** call `BeginCheck` only on a cache miss, and end every check with `Done` or `Fail`.
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
