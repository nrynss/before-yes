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
status:     done
```
`POST /api/checks` normalizes the request. If a fresh card exists it answers at once. Otherwise it starts a Keel `job` of kind `check` and answers `202` with the check id.

The kind has a concurrency limit and is **not** idempotent. A check spends money, so a restart marks it interrupted instead of re-running it. The evidence cache keeps a re-ask cheap.

The job reports progress as Keel `job.Progress`: stage, current and total when counting, and a `detail` message. `GET /api/checks/{id}/events` serves the job's topic through the Keel `stream` broker. `GET /api/checks/{id}` reads the stored result. A failure fails the job with its Before Yes code by returning `job.Failure{Code: ...}` (Keel v0.7.3) ([nrynss/keel#83](https://github.com/nrynss/keel/issues/83)), so the stream's terminal and the stored result carry the same code (T1.4).

**Done when:** A test drives a check to done through the HTTP API with a fake pipeline. Another test reads the events stream and sees progress and the terminal frame. A failure surfaces `budget_exhausted` or `no_sources` from `GET /api/checks/{id}`.

**Implementation notes (T4.1).**
- `Server` struct and `NewRunner` constructor in `internal/server/checks.go`:
  - `NewRunner` configures kind `check` with `Limit` and `Idempotent: false`, ensuring interrupted checks across process restarts are terminated and not rerun.
  - Seam for T4.5: `Checks` interface declared on `Server` without wiring into `startCheck`.
- HTTP Routes in `Handler()`:
  - `POST /api/checks` -> `startCheck`: decodes up to 4KB body, validates with `req.Normalize()` (400 `invalid_request` with message `"title is required and age must be between 3 and 17"`), returns 200 `{"card_id": cid}` on `CardCache` hit, starts Keel job on miss, maps `job.ErrLimit` to 503 `busy` with `Retry-After: 20` header and body detail, returns 202 `{"check_id": "j_" + jid}` on start.
  - `GET /api/checks/{id}` -> `getCheck`: requires `"j_"` prefix + valid 32-hex Keel ID (bare or invalid IDs rejected with 404 before queries), fetches `Runner.Result`. Reports `running`, `queued`, `done` with `card_id: "c_" + cid`, `error` with matched code/message/detail, or `interrupted` with restart message.
  - `GET /api/checks/{id}/events` -> `checkEvents`: validates `"j_"` check ID and streams SSE topic via `Broker.ServeTopic`.
  - `GET /api/cards/{id}` -> `getCard`: requires `"c_"` prefix + valid 32-hex Keel ID, queries `Cards.Get` and returns 200 with card JSON or 404 `not_found`.
  - `GET /api/cards` -> `recentCards`: returns `{"cards": [...]}` with up to 12 recent card summaries, each with `"c_"` ID prefix.
- `work` check job:
  - Reports `gather`, `read`, `judge`, and `alternatives` progress over Keel `stream`.
  - Runs `EvidenceCache.Collect`, `Pipeline.Decide`, checks alternatives if verdict is `no` or `yes_with_settings`, stores with `Cards.PutWithAlternatives` or `Cards.Put`.
  - Translates errors into `job.Failure` (`budget_exhausted` with `detail.pool`, `no_sources`, `check_failed`) so stream error event and stored job record carry the exact same code and detail.
- Contract notes:
  - Wire check IDs carry `j_` prefix, wire card IDs carry `c_` prefix.
  - Bare or malformed IDs return 404 `not_found` before store queries.

---

### T4.2: Card store and caches ★
```yaml
requires:   T1.3
fixture-ok: yes
size:       S · mid
owns:       internal/cards/, internal/server/cache.go, internal/server/cache_test.go
status:     done
```
Cards are stored in the application's SQLite file under Keel `id` values (32 hex characters, unguessable), with title key, age, checklist version, verdict and timestamp for lookup. Migrations go through Keel `sqlite.Migrate` in their own namespace.

Two cache levels, with distinct keys:
- **evidence**, by title, plan version, storefront and reader model, in Keel `cache` with single flight, so a double click pays once;
- **card**, by title, age and checklist version, answered from the card store within its maximum age.

A failed make is never cached.

**Done when:** Tests show an invalid id refused before any query, two concurrent identical checks causing one collect, and a card found again by title and age.

**Implementation notes (T4.2).**
- `internal/cards`:
  - `Store` opened with `Open(ctx, db)` running migrations under namespace `beforeyes_cards`.
  - Cards stored under 32-hex Keel `id.New()` values. `Get(ctx, id)` validates `id.Valid` before executing any query.
  - `PutWithAlternatives(ctx, main, alts)` stores the main card and its checked alternative cards in one transaction, remapping alternative IDs to fresh Keel IDs and dropping dangling links.
  - `Latest(ctx, title, age, checklist, since)` finds the newest card within the since boundary.
  - `Recent(ctx, limit)` orders by `created_at DESC, seq DESC` partitioned by `title_key, age` to yield the latest card per title and age, field-for-field matching `testdata/contracts/api/get-cards.json`.
- `internal/server/cache.go`:
  - `EvidenceCache` implements `pipeline.EvidenceCache` over `cache.Store` and wraps Keel `cache.Cache` for single-flight deduplication via `Collect(ctx, p, title, ref)`.
  - Reuses `pipeline.EvidenceKey` verbatim.
  - Thin / uncacheable evidence produces a private error in the maker so Keel's `GetOrMake` never stores it, while callers sharing the flight unwrap and receive the evidence.
  - `Put` stages the payload in active flight state during `Collect`, letting Keel's `storeResult` perform the sole persistent write on miss, preventing redundant double-writes and detecting thin collects without querying expired store rows.
  - Active flights are cleaned up on `Collect` return via defer matching leader tokens, preventing in-memory map leaks.
  - `CardCache` checks `cards.Latest` within `MaxAge`. Failed makes never write to the card store.
- Contract notes:
  - public card ids are c_ + a 32-hex Keel id; the store keys on the hex part.
  - Age range: `docs/api.md`, `pipeline.MinAge` and the 400 message now all say 3 to 17 (the user chose 3).
  - `storefrontOf` is duplicated in `internal/server/cache.go` because `pipeline.storefrontOf` is unexported.

---

### T4.3: Gate, passcode, live switch
```yaml
requires:   T4.1
fixture-ok: yes
size:       S · mid
owns:       internal/server/gate.go, internal/server/gate_test.go
status:     in-review
```
`POST /api/checks` sits behind Keel `gate`: a per-client and a global token bucket, and an optional shared passcode set in config. Every other route is free.

A `live` setting refuses new checks without a redeploy while cached cards keep working. Decide whether this setting comes from config and a restart or from Keel `flag`, and record the reason.

Behind Traefik the client key comes from forwarded headers only when the peer is a trusted proxy. Set `TrustedProxies` to match the Hetzner tenant.

**Done when:** Tests show 429 with `Retry-After` past the bucket, 403 without the passcode when one is set, and `live_checks_off` for an uncached title while a cached card still returns.

**Implementation notes (T4.3).**
- `Server.GatedHandler(GateConfig)` in `internal/server/gate.go` wraps `Handler()`: `POST /api/checks` goes through Keel `gate.Protect` (passcode, then the per-client and global buckets), then the live switch, then the check handler. Every other route is the plain mux, ungated. `checks.go` is unchanged.
- **Live switch: Keel `flag`, not config and a restart.** The task asks for refusing new checks without a redeploy, and a flag read goes through to SQLite on every request, so one write flips it with no restart. `LiveFlag(def)` declares `live_checks`; the config's `live` setting is the flag's default, which holds until the flag is set. A flag that cannot be read refuses with `live_checks_off` (fails closed).
- Order follows `docs/api.md`: gate, then validation, then the card cache, then `live_checks_off`. The live switch peeks at the body (4 KB, the handler's own limit), lets an invalid body or a fresh cached card through to the handler, and refuses the rest.
- `TrustedProxies` default: `127.0.0.0/8`, `::1/128` and `172.16.0.0/12` (the Docker bridge range Traefik reaches the container from). Narrower than Keel's default, which trusts every private range. The tenant's actual Docker network was not readable from here; T6.1 should confirm it.
- `CheckRule` default: per client burst 3, then 1 every 2 minutes; global burst 10, then 1 every 30 seconds. A cached hit also draws a token, because the gate runs first.
- The passcode uses Keel's default header `X-Passcode` and cookie `passcode`.
- Wiring the passcode secret, the flag store and `GatedHandler` into `serve` is T4.4's.

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

It builds the pipeline the way `tools/eval` does. Set `Judge.Entail` to the reader's Nano client (T3.10). Left nil, the entailment check never runs, and unearned flags and unsupported reasons reach the card. Set `Pipeline.Checks` to the server's `*spend.Meter` (T4.5). A test pins that `serve` and `check` both build a judge with `Entail` set.

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
