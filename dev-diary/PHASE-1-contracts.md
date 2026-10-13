# P1: Contracts

```yaml
id:       P1
size:     M
requires: [T0.1]
blocks:   P2, P3, P4, P5
parallel: no
```

**Goal:** Freeze the four shapes every track compiles or renders against: the checklist and its verdict rules, the finding, the card, and the HTTP API. Each gets a committed example payload in `testdata/contracts/`.

**Why this phase runs serially:** The pipeline writes findings and cards, the server stores and serves them, and the frontend renders them. If any of those shapes moves after the tracks split, four tracks rework at once.

---

### T1.1: Checklist v1 and verdict rules ★
```yaml
requires:   T0.1
fixture-ok: yes
size:       M · frontier
owns:       internal/checklist/
status:     drafted
```
The brief says it plainly: the checklist is the product. It is fixed, versioned, and the same for every title.

Six areas: contact, money, data, age gating, content, developer record. Each has a question as it appears on the card. Each grade is `ok`, `ok_with_setting` (the setting named), `flag` (the quote that raised it), or `unknown`.

The judge grades. Code decides. `Decide(grades) → verdict` is pure, deterministic, and documented rule by rule. `Enforce(grades, evidence)` holds the judge to the evidence:
- a cited finding id that does not exist, or belongs to another area, is dropped;
- a flag left with no verified risk finding becomes `unknown`;
- a setting grade with no setting named, or no remaining finding, becomes `unknown`;
- an area the judge skipped is added as `unknown`.

**Decision for the user:** the brief names three verdicts (yes, yes with settings, no). The draft adds a fourth, `unclear`, for when contact or content has no evidence or half the areas are unknown. That avoids answering "yes" on no evidence. Record the decision here before this task closes.

Bump `checklist.Version` whenever an area's wording or a rule changes, because cached cards record the version they were judged under.

**Done when:** A table test covers every rule and every demotion, `Decide` refuses an incomplete or duplicated grade set, and the verdict question is decided and recorded.

---

### T1.2: Finding format and the quote check ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · frontier
owns:       internal/finding/
status:     drafted
```
A finding is one claim: `area`, `claim`, `stance` (`risk`, `mitigation`, `neutral`), a verbatim `quote`, and a `source` (url, kind, title, review index, fetched at). It gets a stable id: a hash of source URL, area and normalized quote, so the same quote keeps its id across runs and duplicates collapse.

The model writes a `ref` for each finding, naming the input segment its quote comes from (`R312`, `D3`). The check then runs against that segment's whole source text, so a quote that crosses a chunk edge still verifies.

A finding survives only if its quote appears in that source text. Normalization forgives case, runs of whitespace, typographic quotes and dashes, and the ellipsis character. It does not forgive a changed word. A quote under 12 normalized characters is dropped, because it proves nothing.

Count every drop by reason (bad area, bad stance, no quote, unquoted). The card shows the counts, and T3.6 uses them to judge Nano.

The parser accepts a JSON object with a `findings` array, a bare array, or either one inside a code fence.

**Done when:** Tests prove a faithful quote with curly apostrophes verifies, a quote with one word changed does not, an invented quote is counted as unquoted, and the same quote from the same source gets the same id.

---

### T1.3: Evidence and card payloads ★
```yaml
requires:   T1.1, T1.2
fixture-ok: yes
size:       S · frontier
owns:       internal/pipeline/types.go, testdata/contracts/evidence.json, testdata/contracts/card-*.json
status:     not-started
```
**Evidence** is everything read about a title before any age applies: title, collected at, App Store info, verified findings, stats. It is cached by title and reused across ages.

**Card** is one judgement for one age: title, age, checklist version, checked at, collected at, verdict, six grades in card order, settings to turn on, the findings the grades cite plus every risk finding, alternatives (T3.5), app info, and stats. Stats cover sources, reviews, chunks, failed chunks, drops by reason, conflicted areas, missing source kinds, Tavily credits, read and judge cost in nanodollars, and both model names.

Commit one example card per verdict (`card-yes.json`, `card-settings.json`, `card-no.json`, plus `card-unclear.json` if T1.1 keeps it) and one `evidence.json`. P5 builds against these files before any live data exists. `card-no.json` is the review 312 moment.

**Done when:** The example payloads round-trip through the Go types with no loss, and a test fails if a field is added to a type without updating the examples.

---

### T1.4: HTTP API and error codes ★
```yaml
requires:   T1.3, Keel v0.7.1
fixture-ok: yes
size:       S · frontier
owns:       docs/api.md, testdata/contracts/api/
status:     not-started
```
Write down the routes and their bodies:

| Route | Answers |
|---|---|
| `POST /api/checks` `{title, age}` | `200 {card_id}` if a fresh card exists, else `202 {check_id}` |
| `GET /api/checks/{id}` | `{id, status, card_id?, code?, message?}` |
| `GET /api/checks/{id}/events` | Server-sent events in Keel's wire vocabulary |
| `GET /api/cards/{id}` | A card |
| `GET /api/cards` | Recent cards, one per title and age |
| `GET /api/checklist` | Version, areas, labels, questions |
| `GET /api/status` | Live switch, budget remaining |

Errors use Keel's `wire` envelope. Before Yes adds its own codes: `budget_exhausted`, `no_sources`, `check_failed`, `live_checks_off`, `busy`. Keel's gate adds `rate_limited` and `passcode_required`.

With Keel v0.7.1 ([nrynss/keel#83](https://github.com/nrynss/keel/issues/83)), a failed job carries its own code and detail on the stream's terminal error event, and `GET /api/checks/{id}` reports the same code after a restart. Before Yes's codes travel there. Do not put them in the job's result data. Document the exact v0.7.1 API this relies on.

The progress event's `detail` carries `{"message": "..."}`, for example "reading 312 reviews".

**Done when:** `docs/api.md` covers every route, code and event, with one example request and response per route in `testdata/contracts/api/`.

---

## Exit Criteria

- [ ] Checklist v1 frozen, with the verdict question decided.
- [ ] The quote check rejects a changed word and an invented quote, by test.
- [ ] Example evidence and card payloads committed, one card per verdict.
- [ ] API and error codes documented, with examples.
- [ ] Error codes ride the job terminal through Keel v0.7.1 (nrynss/keel#83).
