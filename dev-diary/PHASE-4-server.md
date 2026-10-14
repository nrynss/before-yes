# P4: Server (Track C)

```yaml
id:       P4
size:     M
requires: [T1.3, T1.4, T2.4]
blocks:   P5 live wiring, P6
parallel: high
runs-parallel-with: P2, P3, P5
```

**Goal:** A single Go binary that starts a check with a short request, streams its progress, stores the card at an unguessable URL, and protects the budget on a public URL.

**Why jobs:** A check runs for 20 to 60 seconds. Cloudflare ends a proxied request at 100 seconds, and a parent's phone drops connections. The request starts a job, and the browser follows it over server-sent events. A reconnect catches up from the stored job state.

Before Yes is Keel's second user after Reprise. A gap found here becomes Keel issue text in the handoff log, not a workaround (README guideline 7).

---

### T4.1: Check jobs and the progress stream ★
```yaml
requires:   T1.4, T3.4
fixture-ok: yes
size:       M · mid
owns:       internal/server/checks.go, internal/server/checks_test.go
status:     drafted
```
`POST /api/checks` normalizes the request. If a fresh card exists it answers at once. Otherwise it starts a Keel `job` of kind `check` and answers `202` with the check id.

The kind has a concurrency limit and is **not** idempotent. A check spends money, so a restart marks it interrupted instead of re-running it. The evidence cache keeps a re-ask cheap.

The job reports progress as Keel `job.Progress`: stage, current and total when counting, and a `detail` message. `GET /api/checks/{id}/events` serves the job's topic through the Keel `stream` broker. `GET /api/checks/{id}` reads the stored result. A failure fails the job with its Before Yes code by returning `job.Failure{Code: ...}` (Keel v0.7.3) ([nrynss/keel#83](https://github.com/nrynss/keel/issues/83)), so the stream's terminal and the stored result carry the same code (T1.4).

**Done when:** A test drives a check to done through the HTTP API with a fake pipeline. Another test reads the events stream and sees progress and the terminal frame. A failure surfaces `budget_exhausted` or `no_sources` from `GET /api/checks/{id}`.

---

### T4.2: Card store and caches ★
```yaml
requires:   T1.3
fixture-ok: yes
size:       S · mid
owns:       internal/cards/, internal/server/cache.go
status:     drafted
```
Cards are stored in the application's SQLite file under Keel `id` values (32 hex characters, unguessable), with title key, age, checklist version, verdict and timestamp for lookup. Migrations go through Keel `sqlite.Migrate` in their own namespace.

Two cache levels, with distinct keys:
- **evidence**, by title, plan version, storefront and reader model, in Keel `cache` with single flight, so a double click pays once;
- **card**, by title, age and checklist version, answered from the card store within its maximum age.

A failed make is never cached.

**Done when:** Tests show an invalid id refused before any query, two concurrent identical checks causing one collect, and a card found again by title and age.

---

### T4.3: Gate, passcode, live switch
```yaml
requires:   T4.1
fixture-ok: yes
size:       S · mid
owns:       internal/server/gate.go, internal/server/gate_test.go
status:     not-started
```
`POST /api/checks` sits behind Keel `gate`: a per-client and a global token bucket, and an optional shared passcode set in config. Every other route is free.

A `live` setting refuses new checks without a redeploy while cached cards keep working. Decide whether this setting comes from config and a restart or from Keel `flag`, and record the reason.

Behind Traefik the client key comes from forwarded headers only when the peer is a trusted proxy. Set `TrustedProxies` to match the Hetzner tenant.

**Done when:** Tests show 429 with `Retry-After` past the bucket, 403 without the passcode when one is set, and `live_checks_off` for an uncached title while a cached card still returns.

---

### T4.4: Status and the static app
```yaml
requires:   T4.1, T2.4
fixture-ok: yes
size:       XS · light
owns:       internal/server/status.go, internal/server/web.go, cmd/before-yes/
status:     drafted
```
`GET /api/status` reports the live switch, the checklist version, and budget remaining in both pools. `GET /api/checklist` serves the areas and their questions. `GET /healthz` answers for the deploy check.

`cmd/before-yes` is the entry point: `serve`, `check` for one check from the terminal, and `config` to print the settings plan.

The built Chaaya app is served from a directory, and unknown paths fall back to `index.html` so `/card/{id}` loads in a fresh tab.

**Done when:** Tests cover every route, and a deep link to a card path returns the app shell.

---

### T4.5: Visitor keys and the fresh-check cap
```yaml
requires:   T4.1, T2.5
fixture-ok: yes
size:       M · frontier
owns:       internal/server/byok.go, internal/server/byok_test.go
status:     not-started
```
From the brief's guardrails for judging: demo titles are precomputed, fresh checks are capped per day, and a visitor may paste their own Token Factory and Tavily keys for unlimited use. Those keys are held for the request only and never stored.

A check run on visitor keys uses clients built for that job alone and a meter that records but does not draw on our pools. The keys never reach a log line, a job record, a stored card, the evidence cache key, or an error message. Cards made on visitor keys are stored like any other card.

The daily cap follows T2.5's decision: 5 fresh checks per UTC day, `[budget] daily_fresh_checks`. Enforce it with `spend.Meter.BeginCheck` and then `Done` or `Fail` (T2.4b). Call `BeginCheck` only on a cache miss, before the check's first paid call. End every check with `Done` or `Fail`. A visitor-key check does not count against the cap. As T3.4 notes, a cache hit books nothing new. Show its judge cost as new spend and its collect cost as already paid. The pipeline already calls `BeginCheck` for alternatives: T3.5 reserves one check per fresh (uncached) alternative through `Pipeline.Checks`, and cached ones run free. Set `Pipeline.Checks` to the same `*spend.Meter` the server uses, and reserve only the main check here. Leaving it unset runs alternatives uncapped, and reserving them again counts them twice (feedback.md).

**Done when:** A test runs a check on visitor keys and then scans the database file, the job store and the captured logs for the key and finds nothing. The cap refuses with `budget_exhausted` once spent.

---

## Exit Criteria

- [ ] A check runs through the HTTP API as a job, with progress over SSE and a typed failure code.
- [ ] Cards live at unguessable ids, and repeat questions hit the caches.
- [ ] The paid route is gated, passcode-capable, and can be switched off without a redeploy.
- [ ] Visitor keys work and leave no trace on disk or in logs.
- [ ] Every Keel gap found is written up as issue text in the handoff log.
