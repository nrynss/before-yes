# Orchestrator

How the orchestrator runs the agentic loop in this repository. Workers read [`../AGENTS.md`](../AGENTS.md). The task rules are in [`README.md`](README.md). This file covers what only the orchestrator does.

The orchestrator is the one session the user talks to. It picks tasks, dispatches agents, lands their work, and keeps the board true. It does not write product code and it does not judge its own dispatches.

---

## Four roles, kept separate

| Role | Does | Never does |
|---|---|---|
| **Orchestrator** | Picks tasks. Dispatches the other three roles. Refuses to advance on a REMEDIATE verdict. Lands commits on `main`. Updates the status board and handoff log. Closes exempt documentation findings. Asks the user for decisions. | Implement a task. Write a verdict. Decide a finding is not worth fixing. Treat an agent's message as the user's approval. |
| **Implementation** | Builds the task inside its `owns` paths, on its own branch in its own worktree. Sets its task to `in-review`. Reports. | Review its own work. Mark its task `done`. Edit `dev-diary/README.md`. |
| **Review** | Reads the diff against the task and the brief, and runs every pin itself. Writes `adversarial-review/<task>-round<N>.md` with the verdict, the counts and one row per finding. | Fix what it found. Soften a finding because the fix looks expensive. |
| **Remediation** | Fixes every finding in the review, as new commits on the task branch, with one row per finding recorded in its report. | Change the verdict. Fix things nobody found; that arrives unreviewed. |

**Use a fresh agent per role per round.** A reviewer never implemented or remediated the task, and the round N+1 reviewer is not the round N reviewer. Remediation may go to the implementer, never to a reviewer. An agent that reviews its own work stops being adversarial. One that both finds and fixes a defect narrows the finding until its fix looks sufficient.

**The one exemption.** The orchestrator may close a finding itself, with no remediation round, when all four hold:
1. It lives in documentation: markdown under `dev-diary/` or `docs/`, or a comment that does not describe behaviour.
2. The fix touches no code, configuration, schema or test.
3. Confirming the fix needs reading, not running.
4. The review file records it by name and file.

If any condition is arguable, it is not exempt. A comment that misdescribes behaviour carries the severity of that behaviour.

---

## The loop

### 1. Pick

A task is **ready** when its status is `not-started` or `drafted`, every task in `requires` is `done`, and it is not `blocked`.

- Prefer ★ tasks on the critical path.
- Run ready tasks in parallel when their `owns` paths do not overlap.
- If a task's description says a decision is the user's, ask the user before dispatching, with options and a recommendation. Examples: the "unclear" verdict in T1.1, demo titles, dropping a source.
- Never schedule. Order work by `requires` only: no dates, windows or target days, in any document or message.

### 2. Prepare

- `main` is clean, and `./tools/check.sh` passes in the main checkout.
- Set the task to `status: claimed:<agent>` on `main` and commit. That claim is how a second orchestrator, or a later session, sees the task is taken.
- Choose the model from the task's class, unless the user says otherwise:

| Class | Implementation | Review |
|---|---|---|
| light | Haiku | Haiku or Sonnet |
| mid | Sonnet, or Haiku when the user asks | Sonnet |
| frontier | Opus | Opus |
| user | none; the orchestrator prepares, the user acts | — |

A reviewer is never a smaller model than the work calls for. A frontier task reviewed by Haiku is not reviewed.

### 3. Dispatch the implementer

Always in the background, always in its own worktree (`isolation: worktree`). Use the brief template below. A worker starts cold, so the brief carries everything it needs: what to read, what to build, what it may touch, how to prove it, and what to report.

### 4. Never trust the report

When the implementer reports, check before anything else:
- every **Done when** clause is met, not just most of them. An agent once set `done` with its own report saying a clause was unmet;
- the branch holds only the task's `owns` paths and its own status lines;
- no secret is in any file (see Secrets);
- the gate passes in the worktree.

A report that is short of the task goes back to the implementer, or the task is split (`T0.4a`) and recorded. It is never landed as done.

### 5. Dispatch the reviewer

A fresh agent, in the background, given the branch name, the task id, the round number, and every earlier review file for the task. It runs the pins itself, in a checkout of the branch **and against the main checkout's tree**. It commits its review file on the task branch.

A worktree has no gitignored directories (`scratch/`, `.env`, `data/`). The T0.7 gate passed twice in a worktree and failed on `main`. Any change to the gate, the build, or anything that walks the tree is pinned in the main checkout before APPROVE.

### 6. Remediate and re-review

On REMEDIATE, dispatch remediation with the review file. Then dispatch a fresh reviewer for round N+1, which must state zero residue against every earlier round, severity by severity. Repeat until APPROVE. Every severity gets fixed. "It is only an L" closes nothing.

### 7. Land

In the task's worktree:
1. `git rebase main`. Conflicts in `dev-diary/` status lines or exit criteria are the orchestrator's to resolve, keeping both sides' truth. A conflict in code goes back to remediation.
2. `./tools/check.sh` passes.

Then in the main checkout:

3. `git merge --ff-only <branch>`.
4. `./tools/check.sh` passes, with `scratch/` and `.env` present.
5. For a task that wrote `testdata/` or `tools/`, grep for every `.env` value and confirm it matches nothing (see Secrets).

If step 4 fails, `main` is broken. Record a finding against the task at once, and remediate before anything else lands.

### 8. Close

- Set the task to `status: done` in its phase file, and tick its exit criteria only where they are true.
- Update the README status board and add a handoff-log entry at the top. The entry gives what exists, what surprised, and what the next agent must not have to work out again. Only the orchestrator edits `dev-diary/README.md`, so parallel branches never conflict on it.
- Remove the worktree and delete its branch, after `git cherry main <branch>` shows no `+` lines and the worktree is clean. Never remove a worktree with unmerged or uncommitted work; report it instead.
- Record the landing in Lambo with `lambo_record_action`, and every decision with `lambo_derive`: what was decided and why.
- Tell the user what landed, what was found, and what is ready next.

---

## Brief template

```
You are doing task <id> ("<title>") for Before Yes. You are in your own worktree of
/home/nryn/work/before-yes, branched from main at <hash>. Commit on your branch. Do not
push, merge, or touch the main checkout.

Read first: AGENTS.md; dev-diary/README.md (<sections>); dev-diary/PHASE-<n>-*.md (<id>,
and the tasks it requires); <review files, fixtures, scratch files to read or move>.

What to build: <the task, restated with any decisions the user has made since it was written>.
You own: <owns paths>. Touch nothing else.
<For a drafted task: move (mv, never copy) these scratch files into your worktree: ...>

Hard rules: <secrets>; <budget and the guard that stops at it>; plain commit messages with
no Co-Authored-By or Claude/Anthropic attribution; no dates in the dev-diary.

Prove it: <tests, the gate, the both-sides proof, the pins this task needs>.

Dev-diary: set <id> to `in-review` in its phase file, and add your notes there. Do not edit
dev-diary/README.md; put your handoff-log text in your report.

Report: branch, `git log --oneline main..HEAD`, each Done-when clause with its evidence,
anything unmet, and anything surprising.
```

A reviewer's brief adds: the round number, the earlier review files, "run every pin yourself in the main checkout as well as the branch", and "write `dev-diary/adversarial-review/<id>-round<N>.md` and commit it on the branch; do not fix anything".

---

## Secrets and spend

- **Keys stay in `/home/nryn/work/before-yes/.env`.** A worktree has no `.env`, and the tool guard refuses an inline `. .env`. A worker that needs a key writes a wrapper script outside the repository, under `/tmp/claude-1000/`, that sources `.env` and `exec`s the command. The wrapper is never committed.
- **No key is ever printed.** Check for leaks with `grep -rlF -- "$VALUE"`, which prints only file names, run with the variables loaded from `.env` and never echoed.
- **Every live (`b`) task carries a budget and a guard in its brief.** Run one title first, check its cost, and stop and report if it goes over. The orchestrator keeps the running Tavily and Token Factory totals in the handoff log.
- **Anything outward is the user's call:** filing an issue, pushing, publishing, a purchase, or a spend beyond the brief's budget. Ask first, every time.

---

## Feedback as you go

Anything Token Factory, Nemotron, Tavily, Keel or Chaaya does that surprises an agent goes into [`feedback.md`](feedback.md), in the same session it was hit. Each entry gives what happened, the evidence, what it cost, and what would fix it. The Devpost submission asks for feedback on Token Factory and the NVIDIA models, and T6.5 drafts it from this file. The orchestrator copies friction out of every report it receives.

---

## Asking the user

Ask, with options and a recommendation, when:
- a task names a decision as the user's;
- evidence contradicts the brief or an earlier decision (for example, a source turns out to be unusable);
- a budget would be exceeded;
- a task is blocked and the way forward is a product choice;
- anything outward-facing is next.

Report outcomes plainly: what landed, what failed, and what the orchestrator itself got wrong.
