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
status:     in-review
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

### T2.1b: Model client, live
```yaml
requires:   T2.1, T0.2, T0.3
fixture-ok: no
size:       S · mid
owns:       internal/model/live_test.go
status:     not-started
```
Behind `//go:build live`. For each host (Token Factory, OpenRouter, build.nvidia.com):
- list `GET /v1/models` and record the exact Nemotron 3 Nano and Super ids;
- make one short completion per role, and record usage and price;
- find the request field that turns thinking off and on for that host, and prove it by output token count on the same prompt.

Write the confirmed ids and thinking switches into `before-yes.toml.example`.

**Done when:** The probe is committed, the transcript and the three hosts' settings are in the handoff log, and the example settings file uses confirmed ids.

---

### T2.2: Tavily client ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       S · mid
owns:       internal/tavily/tavily.go, internal/tavily/tavily_test.go
status:     claimed:impl-t2.2
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
status:     not-started
```
One search and one extract against the live API. Record whether `usage.credits` comes back, and whether `include_raw_content` on search costs extra.

**Done when:** The probe is committed and the transcript is in the handoff log.

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
status:     not-started
```
Search and read all pages for one title in `us` and one other storefront. Record how many reviews each storefront serves. This answers the brief's open question about which countries to read.

**Done when:** The probe is committed and the transcript is in the handoff log.

---

### T2.4: Spend meter ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       M · frontier
owns:       internal/spend/
status:     claimed:impl-t2.4
```
Implements `pipeline.Meter` over Keel `cost/sqlitestore`, with two pools in one database file:
- **model:** USD nanodollars, a lifetime ceiling below the $25 credit;
- **tavily:** thousandths of a credit, a monthly ceiling under 1,000 credits.

Each call reserves its estimate first and is refused before it runs if the pool cannot cover it. While the call runs the estimate is held. Afterwards the meter settles what the call reported and writes a charge to the ledger, with the check id as its reference. A failed call releases its hold. A booking whose context was cancelled still lands, because the call ran.

With Keel v0.7.3 ([nrynss/keel#82](https://github.com/nrynss/keel/issues/82)), each pool is a cost store under its own `Config.Namespace`, for example `cost_model` and `cost_tavily`, in the application's one database file. A namespace ending in `_owner` is refused. The draft opens one file per pool; remove that.

**Done when:** Tests show an over-budget estimate refused before the call, a failed call releasing its hold, and a charge read back from the ledger through a fresh SQLite connection, not through the store.

---

### T2.5: Budget settings
```yaml
requires:   T2.4
fixture-ok: yes
size:       S · frontier
owns:       docs/budget.md
status:     not-started
```
Decide and document the ceilings, from the brief's arithmetic and T3.6's measured cost per title:
- the model pool's lifetime ceiling, and how much of the $25 stays in reserve;
- a daily cap on fresh checks while judging runs (needs a daily-period model pool or a separate counter);
- Tavily's monthly ceiling;
- the cost of one fresh check, measured, and how many checks the budget covers.

**Done when:** `docs/budget.md` gives every ceiling with its reason, and the settings file matches it.

---

## Exit Criteria

- [ ] All three clients pass offline tests against recorded responses.
- [ ] Live probes committed for every client, with transcripts in the handoff log.
- [ ] Confirmed Token Factory model ids and thinking switches in the example settings.
- [ ] The meter refuses before a call, releases on failure, and books to a durable ledger.
- [ ] Budget ceilings documented with measured cost per check.
- [ ] Both pools share the application database through cost-store namespaces (Keel v0.7.3, nrynss/keel#82).
