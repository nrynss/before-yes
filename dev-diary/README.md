# Development Diary: Phase Handoffs

This document breaks down the work for **Before Yes**, the Nebius × NVIDIA Global AI Hackathon entry.

Reference specification:
- [`../BRIEF.md`](../BRIEF.md): product, checklist, architecture, models, budget, guardrails, demo.
- [`ORCHESTRATOR.md`](ORCHESTRATOR.md): how the orchestrator runs the loop of pick, dispatch, review, land and close, with the brief template.
- [`feedback.md`](feedback.md): friction with Token Factory, Nemotron, Tavily, Keel and Chaaya. It feeds the Devpost feedback.
- `~/work/hackathons/nebius-nvidia.md`: event rules, prizes, and why this concept was chosen.

Where a phase document and the brief conflict, the brief wins. Record the discrepancy in the handoff log of the phase that found it.

Each phase document gives an engineer starting cold the complete context for its tasks.

**Current status:** P0 is done. P1 and P2 can start. An earlier backend draft sits in `scratch/` at the repository root, uncommitted and unreviewed. Tasks it covers show `status: drafted`, which means there is code to start from, not that the task is done. See [Starting from the scratch draft](#starting-from-the-scratch-draft).

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

`scratch/` (in the main checkout, `/home/nryn/work/before-yes/scratch/`) holds the earlier backend draft. It is not on git, and nothing in it has been reviewed.

**Move, never copy.** An agent that claims a `drafted` task moves the scratch files its task owns into its own worktree with `mv`. Once a file has left `scratch/`, it is claimed, and no other agent can start from it or overwrite it. A copy would leave a second version behind, and two agents would end up editing the same code apart.

- Move only the files the table below gives your task. Reading another task's scratch file for reference is fine. Moving or editing it is not.
- If the file you need is gone from `scratch/`, another task has claimed it. Its code reaches you through git once that task lands, not through `scratch/`.
- A draft file that spans several tasks is moved by the first task listed. That task keeps its own part and moves the rest back into `scratch/` as a new file named for the next task, for example `scratch/internal/server/server.T4.3.go`. That is still a move, so the code exists in one place only.
- Record every move in the handoff log: which files, into which worktree, for which task.
- A drafted task still goes through review before it closes. The draft is a starting point, not evidence.

| Scratch path | Moved by |
|---|---|
| `go.mod`, `go.sum` | T0.1 |
| `internal/checklist/` | T1.1 |
| `internal/finding/` | T1.2 |
| `internal/pipeline/pipeline.go`, `pipeline_test.go` | T1.3 takes the types into `types.go`, then T3.4 takes the rest |
| `internal/model/` | T2.1 |
| `internal/tavily/` | T2.2 |
| `internal/appstore/` | T2.3 |
| `internal/spend/` | T2.4 |
| `internal/pipeline/gather.go` | T3.1 |
| `internal/pipeline/doc.go`, `read.go` | T3.2 |
| `internal/pipeline/judge.go` | T3.3 |
| `internal/server/server.go` | T4.1 takes the check job and the check and card routes, then T4.2 the evidence cache wiring, then T4.3 the gate, then T4.4 the status, checklist and static routes |
| `internal/cards/` | T4.2 |
| `cmd/before-yes/main.go` | T4.4 |

When `scratch/` is empty, delete it and say so in the handoff log.

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
| **A: Clients** | P2 | `b` tasks only | T2.1 | Model, Tavily, App Store clients and the spend meter. |
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
| P1 | 0 / 4 | Not started. T1.1 and T1.2 drafted. |
| P2 | 2 / 9 | T2.1, T2.3 done. T2.2, T2.4 in review. T2.1a opened from the T2.1 review (deep-copy `Extra`, before T3.3). Live `b` tasks and T2.5 not started. |
| P3 | 0 / 8 | Not started. T3.1–T3.4 drafted. |
| P4 | 0 / 5 | Not started. T4.1, T4.2 and T4.4 drafted. |
| P5 | 0 / 7 | Not started. |
| P6 | 0 / 6 | Not started. |

---

## Handoff Log

Newest first. One entry per closed task, finding, or split.

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
