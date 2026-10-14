# P2: Clients and meter (Track A)

```yaml
id:       P2
size:     M
requires: [P1]
blocks:   P3 live runs, P4
parallel: high
runs-parallel-with: P3, P4, P5
```

**Goal:** One client per outside service, each tested offline against recorded responses, plus the meter every paid call goes through. Live behaviour is proved in the `b` tasks, behind the `live` build tag.

**Why the meter belongs here:** The $25 Token Factory credit and Tavily's 1,000 credits a month are the project's hardest limits. A call that can be made without the meter will be made without the meter, so the clients and the meter are built together.

---

### T2.1: Model client ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       M · mid
owns:       internal/model/model.go, internal/model/model_test.go
status:     done
```
One client for the OpenAI-style chat API that Token Factory, OpenRouter and NVIDIA's API all speak. Base URL, model name, prices per million tokens, max tokens, a per-minute pace, and an `extra` map merged into every request body all come from config. Moving a role from a free host to Token Factory is two settings.

- Price each call from the reported `usage`. A reply with no usage is marked unmeasured, and the meter settles it at the estimate.
- Retry 429 and 5xx with Keel `throttle`. Honour `Retry-After`, pace starts under the host's per-minute cap, and use a smaller retry budget for 5xx, because each retry may be billed.
- Never retry a 4xx other than 429, or a 200 whose body will not decode. That 200 was billed.
- OpenRouter reports upstream failures inside a 200 with an `error` member. Treat that as a 502.
- `extra` carries the thinking switch, which differs by host. Per-call options can override it, so the judge's conflict pass can turn thinking on.
- The key is read on every call, so a rotated key takes effect without a restart.

**Done when:** `httptest` tests cover pricing from usage, the merge of `extra`, retry on 429, no retry on 400, and OpenRouter's error-in-200.

---

### T2.1a: Deep-copy `Extra` before T3.3
```yaml
requires:   T2.1
fixture-ok: yes
size:       XS · light
owns:       internal/model/model.go, internal/model/model_test.go
status:     done
```
From the T2.1 round-1 review, L1: `Settings()` copies `Extra` one level and `New` stores the caller's map as given, so nested values — the thinking switch object the judge will use — are shared between a caller and every later request body. Clone nested maps in `New` and `Settings()` so no caller's mutation can reach a later request, and pin it with a test. Also carry L3's note from the same review: record in the code comment that an empty-`choices` 200 draws the smaller 5xx budget and may be a second billed call. This must land before T3.3's conflict pass.

---

### T2.1b: Model client, live
```yaml
requires:   T2.1, T0.2, T0.3
fixture-ok: no
size:       S · mid
owns:       internal/model/live_test.go, before-yes.toml.example
status:     done
```
Behind `//go:build live`. For each host (Token Factory, OpenRouter, build.nvidia.com):
- list `GET /v1/models` and record the exact Nemotron 3 Nano and Super ids;
- make one short completion per role, and record usage and price;
- find the request field that turns thinking off and on for that host, and prove it by output token count on the same prompt.

Write the confirmed ids and thinking switches into `before-yes.toml.example`.

**Done when:** The probe is committed, the transcript and the three hosts' settings are in the handoff log, and the example settings file uses confirmed ids.

Transcript of the remediation run: [live/t2.1b-transcript.txt](live/t2.1b-transcript.txt). Token Factory cost $0.00060618; OpenRouter and build.nvidia.com were free.

Forward notes from the T2.1 and T2.1a handoffs, as the round-1 review (M3) found them:
- **Retry-After over 60 s:** not measured. The probe never provokes a 429, so it never sees a `Retry-After`. Token Factory sends dynamic rate-limit headers on every reply (`X-Ratelimit-Limit-Requests` 100 for the reader and 300 for the judge, `X-Ratelimit-Limit-Tokens` 800000 and 200000, `-Remaining-` and `-Reset-` values of 1 s, `X-Ratelimit-Dynamic-Scale-*` 1.00, `X-Ratelimit-Dynamic-Period-Remaining` 525 s). OpenRouter and build.nvidia.com send no `X-Ratelimit-*` headers. Whether any host sends a `Retry-After` above 60 s on a 429 stays open.
- **Empty `choices` on a 200:** not observed on any host in the probe's calls. Deferred.
- **Timeout:** measured, and moved to T2.1c. Rates vary between runs. The round-1 run gave about 65 tokens a second for Token Factory Nano and build.nvidia.com, about 150 for Token Factory Super, and about 43 for OpenRouter's free judge. The [committed transcript](live/t2.1b-transcript.txt) gives Nano about 90 a second and the OpenRouter judge about 35. A 16,000-token thinking call therefore takes between about 180 s and 245 s on Nano, which is at or past the client's 3-minute timeout, and about 375 s to 450 s on OpenRouter, which is past it.
- **Pacer:** the per-model limits seen were 100 a minute for the reader and 300 for the judge, so the cap looks per model. Whether a shared account-wide counter also exists is unknown; the probe does not test it.

---

### T2.1c: Model client timeout that fits a thinking call
```yaml
requires:   T2.1
fixture-ok: yes
size:       S · mid
owns:       internal/model/model.go, internal/model/model_test.go
status:     done
```
Split from T2.1b's round-1 review (M3). The client's HTTP timeout is a fixed 3 minutes. Measured output rates vary between runs: about 65 to 90 tokens a second for Token Factory Nano, about 150 for Token Factory Super, and about 35 to 43 for OpenRouter's free judge (T2.1b, both review rounds). So a 16,000-token thinking call takes about 180 s to 245 s on Nano, which is at or past the timeout, and about 375 s to 450 s on OpenRouter, which is past it. Re-measure before choosing the floor and ceiling. The judge's conflict pass (T3.3) asks for 16,000 tokens with thinking on.

Make the per-call deadline scale with the call's `max_tokens`, or make it a setting per role, with a floor and a ceiling. A timed-out call must still never book at $0, keeping T2.1's unmeasured-reply rule.

**Chosen design:** the deadline is derived from the call's `max_tokens`, with no new setting. `deadline = clamp(60 s + max_tokens / 30 tokens-a-second, 90 s, 15 min)`. The per-role `max_tokens` already says how long a call may run, so a second setting would only drift from it. The 30 tokens a second sits below the slowest rate measured, 35.

**Measured:** a live run at T2.1c, four calls with `max_tokens` 4000 and thinking on, 16,000 tokens in all, about $0.006 spent. Rate is output tokens over wall-clock seconds.
- Token Factory Nano, two calls: 4000 tokens in 59.0 s and 59.1 s, about 68 tokens a second.
- Token Factory Super, one call: 4000 tokens in 25.7 s, about 156 tokens a second.
- OpenRouter free judge, one call: 4000 tokens in 43.8 s, about 91 tokens a second. This is faster than the 35 to 43 that T2.1b measured, so the rate moves about 2.5 times between runs.

**Arithmetic:** a 16,000-token call on Nano at 68 tokens a second takes about 236 s, which the old 180 s cut. Its new deadline is 60 + 16,000 / 30 = 593 s. On the slowest measured judge rate, 35 tokens a second, it takes about 457 s, still inside 593 s. A 100-token call gets 60 + 3 = 63 s, which the 90 s floor raises to 90 s. A 4096-token call gets 60 + 137 = 197 s. Calls at or above 25,200 tokens hit the 15-minute ceiling.

**Timed-out calls:** a call that gets no reply before its deadline returns an error that matches `context.DeadlineExceeded`, is not retried, and returns an unmeasured `Reply` priced at the estimate it was reserved at. The meter in `internal/spend` books nothing on a work error, so the caller must book that price.

**Done when:** A test shows a large `max_tokens` call is not cut at 3 minutes and a small one keeps a short deadline. The setting, if there is one, is in `before-yes.toml.example`. Met: `TestDeadlineScalesWithMaxTokens` and `TestLargeCallIsNotCutWhileSmallOneIs` in `internal/model/model_test.go`, and no setting was added, so `before-yes.toml.example` is unchanged. A timed-out call is covered by `TestTimedOutCallIsPricedAtEstimateNotZero`.

---

### T2.1d: Live probe uses the scaled deadline
```yaml
requires:   T2.1c
fixture-ok: yes
size:       XS · light
owns:       internal/model/live_test.go
status:     done
```
Split from T2.1c. The live probe builds its own client with `http.Client{Timeout: 3 * time.Minute}`. Today's probe asks for at most 512 tokens, so the fixed timeout is harmless. A probe of a 16,000-token thinking call would be cut, though, while the production client would let it finish.

Drop the fixed client timeout. Give each probe request the deadline that `model.DefaultTimeouts.For` gives for that request's `max_tokens`, so the probe and the client share one rule. Keep the probe's overall 8-minute context.

**Done when:** `live_test.go` sets no fixed client timeout, and every request it sends carries a deadline from `DefaultTimeouts.For`. `go vet -tags live ./internal/model` and `go build -tags live ./...` pass, and the gate passes. No live run is needed.

**Result:** the probe's fixed client timeout is gone. Chat calls get their deadline from `Client.Chat`, and raw requests get `DefaultTimeouts.For(maxTokens)` in `liveProbe.raw`, inside the 8-minute context. Vet, build and gate pass; no live run.

---

### T2.2: Tavily client ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       S · mid
owns:       internal/tavily/tavily.go, internal/tavily/tavily_test.go
status:     done
```
Search (`query`, depth, topic, max results, include and exclude domains, raw content) and extract (basic depth, text format). Ask for `include_usage` and report the credits each call cost. When usage is absent, fall back to the price list: basic search 1, advanced 2, extract 1 per 5 successful URLs.

Pace under the key's per-minute cap and retry 429 and 5xx. A URL Tavily could not extract is left out, not raised as an error.

**Done when:** Tests against the T0.4 fixture responses decode every field the pipeline reads, and the credit fallback matches the price list.

---

### T2.2b: Tavily client, live
```yaml
requires:   T2.2, T0.3
fixture-ok: no
size:       XS · mid
owns:       internal/tavily/live_test.go
status:     done
```
One search and one extract against the live API. Record whether `usage.credits` comes back, and whether `include_raw_content` on search costs extra.

**Done when:** The probe is committed and the transcript is in the handoff log.

**Measured** ([transcript](live/t2.2b-transcript.txt); reproduced by the round-1 reviewer):
- `usage.credits` came back on 3 of 3 calls: search 1, search with raw content 1, and a basic extract 0 (1 page read, 2 failed).
- `include_raw_content` on a basic search costs nothing extra. It adds about 110 KB for 5 hits (Content-Length 7,592 against 120,811).
- **Rate limit: not measured.** Three calls in five seconds saw no 429, and Tavily sends no rate-limit, quota or retry headers. Tavily documents 100 requests a minute for a development key, which is unverified here. The client paces at 90 a minute. Finding the real limit means a burst of about 100 paid calls, so it stays open until the deployed host shows a 429. The retry-storm question from T2.2's round-1 review (Keel gives a rate slot only to a call's first attempt) is also open.
- Spend: 2 Tavily credits, plus 2 for the reviewer's re-run.

---

### T2.3: App Store client ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       S · mid
owns:       internal/appstore/appstore.go, internal/appstore/appstore_test.go
status:     done
```
Apple's iTunes search API finds the app: id, name, seller, seller URL, store URL, age rating, description. The customer-review feed serves up to 10 pages of 50 recent reviews per storefront country. Each review gets a running index from 1, so a card can say "review 312".

The feed's `entry` member can be an array or, on a single-review page, a bare object. A later page that fails ends the read. A first page that fails is an error.

Android-only titles have no review source. Google Play reviews have no official API, and the brief says not to scrape them.

**Done when:** Tests against the T0.4 review pages index reviews across pages, handle a single-entry page, and stop at an empty page.

---

### T2.3b: App Store client, live
```yaml
requires:   T2.3
fixture-ok: no
size:       XS · light
owns:       internal/appstore/live_test.go
status:     done
```
Search and read all pages for one title in `us` and one other storefront. Record how many reviews each storefront serves. This answers the brief's open question about which countries to read.

**Done when:** The probe is committed and the transcript is in the handoff log.

**Result (round-1 remediation).** Transcript: [dev-diary/live/t2.3b-transcript.txt](live/t2.3b-transcript.txt) (run once, keyless; the search half works: us 3 hits, gb 5).
- **Reviews each storefront serves now: 0 everywhere.** `us` 0 in 1 page, `gb` 0 in 1 page, HTTP 200 with no `entry`. The orchestrator confirmed it is not the probe or throttling: Apple's customer-review feed returned HTTP 200 with no `entry` for every app tried, with Facebook (284882215) as a control, in `us`, `gb` and `in`, in the json, xml and no-sort variants. Two probes from this machine and one from a separate Hetzner host. The feed went dark. T0.4 captured 500 reviews per title earlier, so it worked then. The fallback is the App Store web page through Tavily (T3.8a, T3.8).
- **The brief's countries question is moot while the feed is dark.** The probe cannot say which countries to read. Re-run it if the feed returns.
- **Unconfirmable while the feed is dark:** the single-entry bare-object shape, whether a live first page leads with an app entry, and whether a populated page accepts Go's default User-Agent. The offline tests still pin those shapes from the recorded T0.4 data.
- **The probe now fails on an empty feed.** Each storefront reports "feed empty: 0 entries on page 1" and the run fails. I chose fail over skip because a skip reads as "not applicable" and a dark feed is the thing the probe exists to catch; the raw-body check, the field audit and the shape checks state they had nothing to compare. The transcript is a FAIL for that reason.

---

### T2.4: Spend meter ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       M · frontier
owns:       internal/spend/
status:     done
```
Implements `pipeline.Meter` over Keel `cost/sqlitestore`, with two pools in one database file:
- **model:** USD nanodollars, a lifetime ceiling below the $25 credit;
- **tavily:** thousandths of a credit, a monthly ceiling under 1,000 credits.

Each call reserves its estimate first and is refused before it runs if the pool cannot cover it. While the call runs the estimate is held. Afterwards the meter settles what the call reported and writes a charge to the ledger, with the check id as its reference. A failed call releases its hold. A booking whose context was cancelled still lands, because the call ran.

With Keel v0.7.3 ([nrynss/keel#82](https://github.com/nrynss/keel/issues/82)), each pool is a cost store under its own `Config.Namespace`, for example `cost_model` and `cost_tavily`, in the application's one database file. A namespace ending in `_owner` is refused. The draft opens one file per pool; remove that.

**Done when:** Tests show an over-budget estimate refused before the call, a failed call releasing its hold, and a charge read back from the ledger through a fresh SQLite connection, not through the store.

---

### T2.4b: Daily cap on fresh checks, and timed-out calls booked
```yaml
requires:   T2.4, T2.5
fixture-ok: yes
size:       S · mid
owns:       internal/spend/
status:     done
```
Split from T2.5. [`docs/budget.md`](../docs/budget.md) sets a cap of 5 fresh checks per UTC day while judging runs. The meter cannot enforce it today. `Open` knows only the model and tavily pools, and `Spend` reserves against one pool per call. A daily model-dollar pool would be the wrong unit: five checks spend about $0.13, so it would never trip.

- Add a checks-per-day pool with `cost.DailyUTC`, a one-unit denomination named `check`, and namespace `cost_checks_daily`. Its limit comes from `[budget] daily_fresh_checks`.
- Reserve one check for each fresh check, before the first paid call. A failed check releases it. A cache hit does not count.
- Pin the tavily pool to `cost.MonthlyUTC` inside `Open`, so a caller cannot open it as a lifetime ceiling.
- **Book a timed-out call (from T2.1c).** `Spend` releases the hold and books nothing whenever `work` returns an error. A model call that hits its deadline returns `context.DeadlineExceeded`, plus an unmeasured reply priced at the estimate, because the host may have billed the tokens it wrote. Today that call books $0, which breaks T2.1's unmeasured-reply rule. On a deadline error, `Spend` must settle at the estimate and still return the error. A caller's own cancel keeps releasing.

**Done when:**
- Tests show the sixth check in a UTC day refused before any call, the count reset at the next UTC day, a failed check releasing its count, and the ledger read back through a fresh SQLite connection.
- A test shows a `work` that returns `context.DeadlineExceeded` booked at the estimate in the ledger, and a cancelled one released.
- `daily_fresh_checks` is uncommented in `before-yes.toml.example`.

**Result.** Checks are reserved with `Meter.BeginCheck(ctx, ref)`, which returns a `*Check`; the caller ends it with `Done()` (books one check) or `Fail()` (releases). A cache hit never calls `BeginCheck`. Chosen over a `Spend`-shaped call because one check runs several paid calls, so it cannot be one `work` closure. `Open` pins `tavily` to `cost.MonthlyUTC` and `checks_daily` to `cost.DailyUTC` whatever the caller passes. `Spend` books a `work` that returns `context.DeadlineExceeded` at the estimate while the caller's context is live, and releases on a caller cancel or caller deadline. Tests: `internal/spend/checks_test.go` and the existing spend tests, `-race`. Keel's `sqlitestore` takes a clock (`Now`), so the day reset is tested by moving the clock, not by sleeping.

---

### T2.5: Budget settings
```yaml
requires:   T2.4
fixture-ok: yes
size:       S · frontier
owns:       docs/budget.md
status:     blocked: T3.6b
```
Note: every ceiling is decided in `docs/budget.md`; only the measured cost per check is pending T3.6b, and the doc gives the formula to plug it into.
Decide and document the ceilings, from the brief's arithmetic and T3.6b's measured cost per title:
- the model pool's lifetime ceiling, and how much of the $25 stays in reserve;
- a daily cap on fresh checks while judging runs (needs a daily-period model pool or a separate counter);
- Tavily's monthly ceiling;
- the cost of one fresh check, measured, and how many checks the budget covers.

**Done when:** `docs/budget.md` gives every ceiling with its reason, and the settings file matches it.

---

## Exit Criteria

- [x] All three clients pass offline tests against recorded responses.
- [x] Live probes committed for every client, with transcripts in the handoff log (T2.1b, T2.2b, T2.3b).
- [x] Confirmed Token Factory model ids and thinking switches in the example settings (T2.1b).
- [x] The meter refuses before a call, releases on failure, and books to a durable ledger (T2.4). It also caps fresh checks per day and books a timed-out call at its estimate (T2.4b).
- [ ] Budget ceilings documented with measured cost per check. The ceilings are in `docs/budget.md` (T2.5). The cost per check is still an estimate until T3.6b, the live quality pass, measures it.
- [x] The pools share the application database through cost-store namespaces (Keel v0.7.3, nrynss/keel#82): `cost_model`, `cost_tavily` and `cost_checks_daily`.
