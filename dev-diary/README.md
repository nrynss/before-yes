# Development Diary: Phase Handoffs

This document breaks down the work for **Before Yes**, the Nebius × NVIDIA Global AI Hackathon entry.

Reference specification:
- [`../BRIEF.md`](../BRIEF.md): product, checklist, architecture, models, budget, guardrails, demo.
- `~/work/hackathons/nebius-nvidia.md`: event rules, prizes, and why this concept was chosen.

Where a phase document and the brief conflict, the brief wins. Record the discrepancy in the handoff log of the phase that found it.

Each phase document gives an engineer starting cold the complete context for its tasks.

**Current status:** P0 in progress. T0.1–T0.3 are done. An earlier backend draft sits in `scratch/` at the repository root, uncommitted and unreviewed. Tasks it covers show `status: drafted`, which means there is code to start from, not that the task is done. See [Starting from the scratch draft](#starting-from-the-scratch-draft).

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
| P0 | 3 / 7 | In progress. T0.1–T0.3 are done. T0.4–T0.7 are open. T0.5 and T0.6 are the user's. |
| P1 | 0 / 4 | Not started. T1.1 and T1.2 drafted. |
| P2 | 0 / 8 | Not started. T2.1–T2.4 drafted. |
| P3 | 0 / 8 | Not started. T3.1–T3.4 drafted. |
| P4 | 0 / 5 | Not started. T4.1, T4.2 and T4.4 drafted. |
| P5 | 0 / 7 | Not started. |
| P6 | 0 / 6 | Not started. |

---

## Handoff Log

Newest first. One entry per closed task, finding, or split.

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
  - Possible Keel issue: `cost/sqlitestore` uses fixed table names, so two budget pools cannot share one database file. Workaround in the draft: one file per pool.
  - Possible Keel issue: a failed job's terminal event always carries `job_failed`. The draft returns a typed outcome in the job's data, which Keel keeps on failure, and `GET /api/checks/{id}` surfaces it.
