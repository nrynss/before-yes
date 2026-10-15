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
status:     done
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
status:     done
```
`GET /api/status` reports the live switch, the checklist version, and budget remaining in both pools. `GET /api/checklist` serves the areas and their questions. `GET /healthz` answers for the deploy check.

`cmd/before-yes` is the entry point: `serve`, `check` for one check from the terminal, and `config` to print the settings plan.

It builds the pipeline the way `tools/eval` does. Set `Judge.Entail` to the reader's Nano client (T3.10). Left nil, the entailment check never runs, and unearned flags and unsupported reasons reach the card. Set `Server.Checks` and `Pipeline.Checks` to the same `*spend.Meter`; `Handler()` refuses anything else (T4.5). `serve` mounts `GatedHandler` (T4.3), not the bare `Handler()`, resolves `[gate.passcode]`, and gives the live switch its flag store. A test pins that `serve` and `check` both build a judge with `Entail` set, and that `serve` routes `POST /api/checks` through the gate.

The built Chaaya app is served from a directory, and unknown paths fall back to `index.html` so `/card/{id}` loads in a fresh tab.

**Done when:** Tests cover every route, and a deep link to a card path returns the app shell.

**Implementation notes (T4.4).**
- `internal/server/status.go`: `Status{Meter, Flags, Live}` serves `GET /api/status` (live from the same flag the switch reads, failing closed to `false`; `checklist_version`; remaining nanodollars, whole Tavily credits and fresh checks today), `GET /api/checklist` (the body of `get-checklist.json`) and `GET /healthz` (`ok`).
- `internal/server/web.go`: `App(api, status, webFS)` is the whole surface: the status routes, `/api/` to the gated API, and every other path to the built app, with a path that is not a file answered by `index.html` (`no-cache`), so `/card/{id}` loads in a fresh tab. Only GET and HEAD reach the app. A missing `web_dir` serves no app. Composing here keeps `checks.go`'s `Handler()` untouched.
- `cmd/before-yes`: `serve`, `check`, `config`. `Settings` takes every key of `before-yes.toml.example` (the loader is strict); secrets are Keel `config.Secret` references. `buildCore` builds the pipeline as `tools/eval` does on real clients: `Judge.Entail` is the reader's Nano client, and the gatherer, reader and judge all book on one `*spend.Meter` (model, Tavily and daily-check pools) in its own file `data_dir/spend.db`; cards, jobs, the evidence cache and flags share `data_dir/before-yes.db`. The meter gets its own file because it opens its own SQLite handle, and two handles writing one file lost `Done` bookings to `SQLITE_BUSY` under concurrent checks (feedback.md). `NewServeApp` sets `Server.Checks` and `Pipeline.Checks` to that meter, opens the Keel flag store for `LiveFlag(live)`, resolves `[gate.passcode]` (unset means no passcode), and mounts `GatedHandler` inside `App`.
- `check` reserves one daily check like the server (`Done` or `Fail`), runs `Pipeline.Run` and prints the card JSON. It does not look for alternatives and does not use the evidence cache.
- **Outside owns (additive):** `testdata/contracts/api/get-status.json` and `docs/api.md` gain `checklist_version` on `GET /api/status`, which this task's text asks for and the contract lacked; `docs/api.md` also lists `GET /healthz`.
- The scratch draft (`scratch/cmd/before-yes/main.go`, `server.T4.4.go`) was read for reference only, not moved: its APIs predate T4.1 to T4.5, and the code here is written fresh. It stays unclaimed on `draft/scratch`.
- One process owns the meter: `buildCore` takes an exclusive, non-blocking `flock` on `data_dir/spend.db.lock` for the process's lifetime, so `check` refuses (`ErrMeterBusy`, naming `spend.db`) while `serve` runs, and so does a second `serve`. The upstream fix is nrynss/keel#100 (`BEGIN IMMEDIATE`), which the user filed and rated not blocking.
- `check` takes its flags anywhere among the title's words (`check Roblox -age 12`).
- **Outside owns (one line):** `before-yes.toml.example`'s comment on `daily_fresh_checks` no longer says no loader reads it.

**Round-1 remediation (T4.4).**

| Finding | Fix | Pin |
|---|---|---|
| M1 two processes on one `spend.db` lose bookings | Exclusive OS lock on `spend.db.lock` in `buildCore`: a second `check` or `serve` refuses with a clear error and opens no meter. Upstream: nrynss/keel#100 | `TestMeterLockRefusesSecondProcess`, `TestMeterLosesNoBookings` (400 concurrent `BeginCheck`+`Done`, three rounds, 0 lost; the second meter cannot open) |
| L1 `check Roblox -age 12` read as the title | `parseArgs` reads flags anywhere; an unknown trailing flag is refused | `TestCheckFlagsAnywhere` |
| L2 stale `daily_fresh_checks` comment | Comment says `cmd/before-yes` reads it | doc |
| A1 to A4 | Accepted by the reviewer | none |

---

### T4.5: Visitor keys and the fresh-check cap
```yaml
requires:   T4.1, T2.5
fixture-ok: yes
size:       M · frontier
owns:       internal/server/byok.go, internal/server/byok_test.go
status:     done
```
From the brief's guardrails for judging: demo titles are precomputed, fresh checks are capped per day, and a visitor may paste their own Token Factory and Tavily keys for unlimited use. Those keys are held for the request only and never stored.

A check run on visitor keys uses clients built for that job alone and a meter that records but does not draw on our pools. The keys never reach a log line, a job record, a stored card, the evidence cache key, or an error message. Cards made on visitor keys are stored like any other card.

The daily cap follows T2.5's decision: 5 fresh checks per UTC day, `[budget] daily_fresh_checks`. Enforce it with `spend.Meter.BeginCheck` and then `Done` or `Fail` (T2.4b). Call `BeginCheck` only on a cache miss, before the check's first paid call. End every check with `Done` or `Fail`. A visitor-key check does not count against the cap. As T3.4 notes, a cache hit books nothing new. Show its judge cost as new spend and its collect cost as already paid. The pipeline already calls `BeginCheck` for alternatives: T3.5 reserves one check per fresh (uncached) alternative through `Pipeline.Checks`, and cached ones run free. Set `Pipeline.Checks` to the same `*spend.Meter` the server uses, and reserve only the main check here. Leaving it unset runs alternatives uncapped, and reserving them again counts them twice (feedback.md).

**Done when:** A test runs a check on visitor keys and then scans the database file, the job store and the captured logs for the key and finds nothing. The cap refuses with `budget_exhausted` once spent.

**Implementation notes (T4.5).**
- **Visitor keys arrive as two headers on `POST /api/checks`:** `X-Token-Factory-Key` and `X-Tavily-Key`. Both or neither; half a pair, an over-long key (512 bytes) or one with spaces or non-printable bytes is `400 invalid_request` with a fixed message that never quotes the key. T5.7 sends these headers. This adds request headers to the contract; no response field and no contract fixture changed.
- `VisitorPipeline(base, keys)` copies the server's pipeline with new `model.Client`s (reader, judge, and `Entail` when set) and a new `tavily.Client`, each reading the visitor's key, with base's settings and retry tuning, so the evidence cache key is unchanged. Its meter is a `VisitorMeter`, which runs and records each call and draws on none of our pools. `Pipeline.Checks` is nil on the copy, so a visitor's alternatives do not count against the cap either. The App Store client and the evidence cache are shared; neither holds a key. Cards are stored like any other.
- `VisitorKeys` has unexported fields and redacts in `String`, `GoString`, `LogValue` and `MarshalJSON`. A failed visitor job keeps no upstream reply text: its stored and logged failure is its code and detail, the host's HTTP status when one answered, and a fixed sentence, because the clients put the host's body in the error and a host may echo the key cut, escaped or encoded. The sentence also goes through `redact`, which removes any run of 8 or more characters of a key in raw, JSON-escaped (`\/`, `\u00XX`) or URL-encoded form. A panic in a visitor job is recovered into `check_failed` with a fixed message; only a redacted line is logged.
- Visitor key headers are validated with the body, before the card cache, so a half pair is 400 even for a cached title.
- **The daily cap:** on a card-cache miss with no visitor keys, `startCheck` calls `Server.Checks.BeginCheck` before starting the job (the order in `docs/api.md`: `budget_exhausted` before `busy`). A spent cap is `402 budget_exhausted`, `detail.pool` `daily_checks`, before any paid call. The job ends the hold with `Done` on a card and `Fail` on any failure or panic; a job that never starts (`busy`) releases it. Only the main check is reserved here.
- **One meter for the main check and alternatives.** `Handler()` refuses (`ErrInvalidConfig`) unless `Server.Checks` and `Pipeline.Checks` are both nil or the same value. Unset `Pipeline.Checks` would run alternatives uncapped; a second meter would keep a second ledger. T4.4 sets both to the server's `*spend.Meter`.
- **Outside owns (minimal):** `docs/api.md` gains one line naming the visitor headers (round-1 L2). `internal/server/checks.go`: `startCheck` also calls `rejectBadVisitorKeys` after body validation (round-1 L1). `Handler()` calls `checkCapWiring`; `startCheck` calls `admit` after the cache miss and runs the job through `admission.wrap`, releasing the hold if the job does not start; `work` takes the pipeline to run as a parameter instead of reading `s.Pipeline`, so a visitor check runs on its own copy; the `Checks` doc comment. T4.1's seam had no other way in.
- A check interrupted by a restart keeps its hold until `spend.DefaultReservationTTL` lapses, then the slot returns; it is never booked.

**Round-1 remediation (T4.5).**

| Finding | Fix | Pin |
|---|---|---|
| M1 key-echo scrub misses cut and encoded echoes | A visitor failure stores and logs no upstream text: code, detail, HTTP status, fixed sentence. `redact` also removes any 8+ character run of a key, raw, JSON-escaped or URL-encoded | `TestVisitorKeyEchoFormsLeaveNoTrace` (raw, 300-byte cut inside the key, `\/` escape, URL encoding; scans DB, WAL, job record, logs, response), `TestRedactPartialAndEncodedForms` |
| M2 visitor panic not scrubbed | `wrap` recovers a visitor job's panic into `check_failed` with a fixed message and logs a redacted line | `TestVisitorPanicIsScrubbed` |
| L1 half pair checked after the card cache | `startCheck` validates the visitor headers with the body, before the cache | `TestHalfPairRefusedBeforeCardCache` |
| L2 headers missing from docs/api.md | One line under `POST /api/checks` (outside owns, minimal) | doc |

---

### T4.6: A double click joins the running check
```yaml
requires:   T4.5
fixture-ok: yes
size:       S · mid
owns:       internal/server/checks.go (startCheck and its join), internal/server/checks_test.go
status:     done
```
`docs/api.md` step 4: a running check for the same normalized title and age answers `202` with its `check_id`. Nothing implements it. A double click today starts two jobs, reserves two of the day's fresh checks and pays the judge twice; only the evidence collect is shared (T4.2's single flight). Found by the T4.3 reviewer.

Join after the card cache and before the refusals (step 4 comes before `live_checks_off`, `budget_exhausted` and `busy`). A joined request reserves nothing. A visitor-key request never joins a check on our keys, and a check on visitor keys is never joined by anyone else, so one visitor's keys never pay for another request.

**Done when:** Tests show two concurrent identical requests get one `check_id`, one job and one daily-cap reservation; a request after the check finishes gets the card; and the visitor-key cases above stay separate.

**Implementation notes (T4.6).**
- `Server` holds an in-memory `joinTable` (made in `Handler()`): running checks on our keys, keyed by `cards.TitleKey(title)` and age. `startCheck` takes its lock after the card cache and holds it through the lookup, `admit` (the daily-cap reservation), `StartKind` and the insert, so two concurrent identical requests cannot both start. A hit answers `202` with the running `check_id` and reserves nothing. The job removes its own entry when its work returns, after the card is stored, so a later request hits the card cache. The table is not persisted: after a restart the job is interrupted (T4.1) and nothing is joined.
- The lock serializes check starts across all titles. A start is a cache read, a reservation and a job insert, so this was kept simple rather than per key.
- Visitor-key requests never look the table up, and visitor-key checks are never entered, so neither side joins the other.
- **Outside owns (minimal):** `internal/server/gate.go`. The live switch also lets through a request that would join a running check (`joinsRunning`), because step 4 comes before `live_checks_off`.
- **User decision: the evidence collect stays one single flight across visitor and our-key checks of the same title (T4.2).** When a visitor check and one of ours collect the same uncached title together, whichever leads the flight pays Tavily and the reader, and the other rides on it; each check still judges on its own keys. The user confirmed this is acceptable.
- **Live off, join only.** When the live switch is off, the gate passes a would-be joiner marked join-only (a context value). Under its lock, `startCheck` joins it if the check is still running, answers its card if one was just stored, and otherwise refuses `live_checks_off`, so a join target that vanished never becomes a new check.
- On a join miss, `startCheck` reads the card cache again under its lock, so a check that finished between the first cache read and the lock is not started twice.

**Round-1 remediation (T4.6).**

| Finding | Fix | Pin |
|---|---|---|
| M1 a join-only request could start a new check with live off | The gate marks the request join-only; `startCheck` refuses `live_checks_off` under its lock when no join target or card is found | `TestJoinOnlyRequestNeverStartsCheckWhenLiveOff` (the reviewer's reproduction: the target check fails between the gate's peek and the handler's lock) |
| L1 a check finishing between the cache read and the lock was started again | Card cache read again under the join lock | `TestCacheRecheckedUnderJoinLock` |
| A1, A2 | Accepted by the reviewer | none |
| A3 visitor and ours share one evidence collect | User decision: acceptable | `TestVisitorRequestNeverJoinsOurCheck` |

---

### T4.7: Progress a parent can see
```yaml
requires:   T4.6
fixture-ok: yes
size:       M · mid
owns:       internal/server/checks.go (work and getCheck), internal/server/checks_test.go, internal/pipeline/pipeline.go (a progress hook on Collect), internal/pipeline/read.go (chunk counts only), internal/pipeline/gather.go (stage reports only), docs/api.md and testdata/contracts/api/get-check-running.json (the stage fields), web/src/lib/progress/
status:     done
```
On the first live run, the progress page showed only its static list of stages for most of a check. `work` reports `gather` once, at the start; gather and read run as one `Collect` call, and `read` with its count is sent only after reading ends. A page that subscribes after the first event receives no replay, and `GET /api/checks/{id}` carries no stage.

1. **Report as it happens:** a progress hook on `Pipeline.Collect`, so gather reports what it is fetching and read reports `current` of `total` as chunks finish ("reading 120 of 500 reviews").
2. **A late page catches up:** `GET /api/checks/{id}` returns the latest stage, message and counts while running (additive contract fields, fixture updated), and the progress view shows them on load and after a reconnect. If Keel's stream drops events for a late subscriber by design, record it in `feedback.md` as a Keel gap.
3. **No silent failures:** when `FindAlternatives` fails for any reason but the budget, `work` still stores the card, and now logs the error (no key, no upstream body).

T3.11 owns `judge.go` and T3.12 owns `alternatives.go`; this task touches neither. If T3.12 lands first, rebase.

**Done when:** a test with a slow fake pipeline sees gather, then read with rising counts, then judge; a page opened mid-read shows the current stage and count at once; the alternatives failure is logged; the gate passes.

**Implementation notes (T4.7):**
- The hook rides the context: `pipeline.WithProgress(ctx, fn)` and a `Step{Stage, Message, Current, Total}`. `Collect`'s signature is unchanged, so `EvidenceCache` and `tools/eval` are untouched.
- Gather reports the App Store lookup, each plan search ("searching for the privacy policy") and the policy/terms extract. Read reports 0 of N as it starts and one more as each chunk ends, failed or not, under a lock so the counts arrive in order. Counts are reader chunks ("source sections"), not reviews; the message is "reading N sources".
- `work` serializes reports and drops a read count lower than the last one. A cache hit still reports "using sources read …".
- `GET /api/checks/{id}` reads the latest progress from Keel's own durable snapshot (`Runner.Attempts`, the record whose id matches), so no server-side table was added. The fields are only present while queued or running.
- A check that joins another's in-flight evidence collect (single flight) gets no gather or read reports: only the leader's context reaches the pipeline.
- An alternatives failure other than the budget is logged at warn as `alternatives failed; storing the card without them` with `ref` and `cause`: `tavily HTTP <status>`, `model HTTP <status>`, `failure <code>`, `cancelled`, `timed out`, or `error`. The error text is never logged.
- The view keeps the snapshot's stage until a newer stream progress event arrives, on load and on every reconnect.
- Outside owns: `dev-diary/feedback.md` (a Keel gap entry) and this file.

**Round-1 remediation (T4.7):**

| Finding | Severity | Fix | Test |
|---|---|---|---|
| M1: a replayed older read count could replace a newer saved snapshot after a reconnect | Medium | `Progress.svelte` lets a stream frame replace the saved snapshot only for a later stage, or the same stage with `current >=` the saved count | `replayed older read count after the snapshot never moves the count backwards` (Playwright) |
| L1: a panic in the progress hook was not recovered | Low (accepted, hardened) | `pipeline.report` recovers a panicking hook, so a reader chunk goroutine cannot crash the process | `TestProgressHookPanicRecovered` (new `internal/pipeline/progress_test.go`, outside owns) |
| L2: a joiner shows "starting the search" until judge | Low (accepted) | none; matches the stated decision | none |

Two existing progress specs ("exact fixture…" and "reconnect rereads…") now start from a running snapshot with no stage, since under M1's rule a saved read stage correctly outranks a later gather frame.

---

## Exit Criteria

- [x] A check runs through the HTTP API as a job, with progress over SSE and a typed failure code. (T4.1)
- [x] Cards live at unguessable ids, and repeat questions hit the caches. (T4.2; a double click joins the running check, T4.6)
- [x] The paid route is gated, passcode-capable, and can be switched off without a redeploy. (T4.3, a Keel flag; wired into `serve` by T4.4)
- [x] Visitor keys work and leave no trace on disk or in logs. (T4.5, with fake keys; a live visitor-key check waits for the P6 end-to-end run)
- [x] Every Keel gap found is written up as issue text in the handoff log. (Three, in the T4.4/T4.6 entry; `cost/sqlitestore` is filed as nrynss/keel#100)
