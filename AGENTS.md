# Agent protocol

Rules for any agent working in this repository. The product is in [`BRIEF.md`](BRIEF.md). The task rules are in [`dev-diary/README.md`](dev-diary/README.md). The orchestrator's loop is in [`dev-diary/ORCHESTRATOR.md`](dev-diary/ORCHESTRATOR.md). This file links to those rules and adds what they do not cover.

## Task rules

Read these in the dev-diary README. Do not copy them.

- [Task pattern](dev-diary/README.md#task-pattern): task fields, status values, classes.
- [Developer guidelines](dev-diary/README.md#developer-guidelines): claim first, stay in `owns`, quote checks, the meter, handoffs.
- [Keel v0.7.3](dev-diary/README.md#keel-v073): the Keel version and the two fixes this project relies on.
- [Starting from the scratch draft](dev-diary/README.md#starting-from-the-scratch-draft): move, never copy.
- [Scope](dev-diary/README.md#scope): a blocked task becomes a finding, not a cut.
- [Swarm coordination rule](dev-diary/README.md#swarm-coordination-rule): task dependencies are binding.
- [Handoff log](dev-diary/README.md#handoff-log): newest first, one entry per closed task.

## Stack

- **Go.** The version is the `go` line in `go.mod`. Read it there.
- **Frontend.** Svelte 5 with runes, on Vite.
- **Node and pnpm.** Use pnpm. Chaaya's `package.json` `engines` field requires Node `>=24`. This machine has Node 26.11.1 and pnpm 11.28.5.
- **Dependencies.** Keel (Go) and Chaaya (npm) are declared. Otherwise, the standard library first. Bits UI for dialogs and tooltips. A new dependency needs a reason in the handoff log.

## Worktrees

- One task, one branch: `task-<id>-<slug>`, for example `task-t2.4-spend-meter`.
- Each branch works in its own worktree.
- Two agents never edit one checkout.

## File ownership

- Edit only the paths in your task's `owns` field.
- Dev-diary status lines and the handoff log are the only shared files a task touches.
- A change outside `owns` is a new task or a handoff note.

## The gate

- `./tools/check.sh` must pass on every commit.
- It runs gofmt, go vet, and `go test -race` over the Go packages, and the web lint, check and test scripts once `web/` exists.
- CI runs the same script on push to `main` and on pull requests.

## Live tests

- Live probes (the `b` tasks) sit behind the `live` build tag: `//go:build live`.
- They never run in the gate or in CI.
- Run them with `go test -tags live ./...`.

## Secrets

- Keys live in `.env` (mode 0600, gitignored).
- `before-yes.toml` (gitignored) refers to them. Setup is in [`docs/keys.md`](docs/keys.md).
- Never print, log, commit, or paste a key value. That includes test output and the handoff log.

## Commits and pull requests

- Plain messages.
- No `Co-Authored-By` trailer.
- No Claude or Anthropic attribution in commits or PR text.

## Dev-diary

- No dates or timelines anywhere in it.
- Work is ordered by task dependencies only.
- An implementer sets its task to `in-review`, never `done`. The orchestrator sets `done` after an APPROVE.
- Only the orchestrator edits `dev-diary/README.md`, which holds the status board and the handoff log. Put your handoff text in your report.
- Anything a service or library does that surprises you goes in [`dev-diary/feedback.md`](dev-diary/feedback.md), when you hit it.

## Review

Four roles are kept separate: orchestrator, implementation, review, remediation. There is a fresh agent per role per round, and a reviewer never implemented or remediated the task. See [ORCHESTRATOR.md](dev-diary/ORCHESTRATOR.md#four-roles-kept-separate).

A reviewer:

1. Reads the diff against the task and the brief.
2. Runs every pin itself, in a checkout of the branch **and** against the main checkout's tree, where gitignored `scratch/` and `.env` exist.
3. Records findings in `dev-diary/adversarial-review/<task-id>-round<N>.md`: the verdict `APPROVE` or `REMEDIATE`, the counts per severity, and one row per finding with its pin.
4. Fixes nothing.

A pin is a measurement taken outside the code under review: query the file, send the request, grep the output. Program output reporting its own success is not a pin. Remediation rounds repeat until a fresh reviewer returns `APPROVE` with zero residue against every earlier round.
