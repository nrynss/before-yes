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
status:     in-review
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


**Implementation notes (in review).** The scratch draft was not reachable from the implementing session (it lives only in the main checkout), so `internal/checklist/` was written fresh from this spec and the brief.
- **Verdict question: decided by the user. `unclear` is kept, and the decision is final.** It stops a title with no evidence on contact or content from computing to "yes". Rules in `Decide`, in order: refuse an incomplete, duplicated or invalid set (`ErrGrades`); any `flag` is `no`; contact or content `unknown` is `unclear`; three or more of six `unknown` is `unclear`; any `ok_with_setting` is `yes_with_settings`; otherwise `yes`. T1.3 keeps `card-unclear.json`.
- `Decide` takes grades only. The child's age reaches the verdict through the judge's grades, not through a rule here.
- `Enforce(grades, map[id]Cited)` takes a small `Cited{Area, Risk}` view of each verified finding, so `checklist` does not import `finding` (T1.2 requires T1.1). It returns six grades in card order plus a `Demotion` list. Beyond the four spec rules it also discards a grade with an unknown area or level, and a second grade for an area, and clears `setting` on any grade demoted to `unknown`.
- `Decide` also refuses `ok_with_setting` with no setting, so `Enforce` output is the only safe input.

**Remediation, round 1** ([review](adversarial-review/t1.1-round1.md)). `checklist.Version` is bumped to `v2`, because an `Enforce` rule changed.

| Finding | Fix | Test |
|---|---|---|
| H1 | The user decided to keep `unclear`, and that decision is final. Recorded above. | none (a decision) |
| M1 | `Enforce`: contact or content graded `ok` with no remaining verified finding becomes `unknown` (`ok_without_finding`), so `Decide` gives `unclear`, never `yes`. Other areas may still be `ok` with no finding. | `TestEvidenceFreeOKIsNotYes`, plus `TestEnforce` cases for contact and content `ok` with and without findings |
| L1 | A setting is trimmed. A blank setting counts as no setting in both `Enforce` and `Decide`. | `TestSettingTrimmed`, `TestEnforce` "blank setting demoted" |
| L2 | A finding id cited twice in one grade is kept once. Added a case for a grade whose area is unknown. | `TestEnforce` "repeated id kept once", "unknown area discarded" |
---

### T1.2: Finding format and the quote check ★
```yaml
requires:   T1.1
fixture-ok: yes
size:       M · frontier
owns:       internal/finding/
status:     in-review
```
A finding is one claim: `area`, `claim`, `stance` (`risk`, `mitigation`, `neutral`), a verbatim `quote`, and a `source` (url, kind, title, review index, fetched at). It gets a stable id: a hash of source URL, area and normalized quote, so the same quote keeps its id across runs and duplicates collapse.

The model writes a `ref` for each finding, naming the input segment its quote comes from (`R312`, `D3`). The check then runs against that segment's whole source text, so a quote that crosses a chunk edge still verifies.

A finding survives only if its quote appears in that source text. Normalization forgives case, runs of whitespace, typographic quotes and dashes, and the ellipsis character. It does not forgive a changed word. A quote under 12 normalized characters is dropped, because it proves nothing.

Count every drop by reason (bad area, bad stance, no quote, unquoted). The card shows the counts, and T3.6 uses them to judge Nano.

The parser accepts a JSON object with a `findings` array, a bare array, or either one inside a code fence.

**Done when:** Tests prove a faithful quote with curly apostrophes verifies, a quote with one word changed does not, an invented quote is counted as unquoted, and the same quote from the same source gets the same id.


**Implementation notes (in review).** Written fresh; the scratch draft was not reachable.
- `Verify(raws, segments)` keys segments by ref (`R312`, `D3`) and matches the normalized quote against the segment's whole text. A ref that names no segment counts as `unquoted`.
- Drops: `bad_area`, `bad_stance`, `no_quote` (empty or under 12 normalized runes), `unquoted`. Duplicates are counted separately in `Drops.Duplicates`, not as drops.
- **Id choice:** every App Store review shares one feed URL, so the id key is the URL plus `#<review index>` when the index is set. Without that, the same sentence in two reviews would collapse into one finding. Ids are `f_` plus 16 hex characters of SHA-256.
- Normalization maps the ellipsis character to `...`, typographic single and double quotes to ASCII, the dash family to `-`, collapses whitespace and lowercases. Nothing else.
- `Cited(findings)` gives `checklist.Enforce` its evidence map.

**Remediation, round 1** ([review](adversarial-review/t1.2-round1.md)).

| Finding | Fix | Test |
|---|---|---|
| H1 | The match respects word boundaries. Where the quote starts or ends with a letter or digit, the text beside it must not be a letter or digit, so "safe for young kids" no longer verifies inside "unsafe for young kids". Every occurrence is tried, so a whole-word match later in the text still counts. | `TestWordBoundaries` |
| M1 | Decided: when one quote is read twice (same id), a `risk` reading wins over `mitigation` or `neutral`. It replaces the earlier finding in place, keeping input order. The duplicate is still counted. | `TestDuplicateRiskWins` |
| M2 | Tests now pin H1 and M1. | as above |
| L1 | `Normalize` applies Unicode NFC first, so composed and decomposed accents match. | `TestNFC` |
| L2 | `Parse` also accepts prose before and after unfenced JSON. This goes beyond the spec's three forms, so T3.2 can rely on it. | `TestParseProse` |

**New dependency: `golang.org/x/text` v0.30.0** (for `unicode/norm`, which the standard library lacks). This is outside `owns`: `go.mod` and `go.sum` change by this one module. v0.30.0 is the newest version that bumps nothing else in the graph; later versions pull a newer `golang.org/x/sync`. The orchestrator copies this reason into the handoff log, as AGENTS.md requires.

`TestFixtureReview` now cuts its quote at a word boundary. The old byte cut ended mid-word, which the H1 fix correctly rejects.
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
requires:   T1.3
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

With Keel v0.7.3 ([nrynss/keel#83](https://github.com/nrynss/keel/issues/83)), the job fails with `job.Failure{Code: "budget_exhausted", Detail: ..., Err: err}`. The Runner puts the code on the stream's terminal error event, and `GET /api/checks/{id}` reports it from `Result.Code` after a restart. Before Yes's codes travel there. Do not put them in the job's result data.

The progress event's `detail` carries `{"message": "..."}`, for example "reading 312 reviews".

**Done when:** `docs/api.md` covers every route, code and event, with one example request and response per route in `testdata/contracts/api/`.

---

## Exit Criteria

- [ ] Checklist v1 frozen, with the verdict question decided.
- [ ] The quote check rejects a changed word and an invented quote, by test.
- [ ] Example evidence and card payloads committed, one card per verdict.
- [ ] API and error codes documented, with examples.
- [ ] Error codes ride the job terminal through `job.Failure` (Keel v0.7.3, nrynss/keel#83).
