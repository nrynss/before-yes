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

### T1.1: Checklist and verdict rules ★
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
- **Verdict question: decided by the user. `unclear` is kept, and the decision is final.** It stops a title with no evidence on contact or content from computing to "yes". Rules in `Decide` (as of `v5`), in order: refuse an incomplete, duplicated or invalid set (`ErrGrades`); any `flag` is `no`; contact or content `unknown` is `unclear`; three or more of six `unknown` is `unclear`; any `ok_with_setting` is `yes_with_settings`; otherwise `yes`. T1.3 keeps `card-unclear.json`.
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

**Remediation, round 2** ([review](adversarial-review/t1.1-round2.md)). `checklist.Version` is bumped to `v3`, because an `Enforce` rule changed again.

| Finding | Fix | Test |
|---|---|---|
| M1 | Contact or content `ok` needs at least one cited **mitigation or neutral** finding. If it cites only risk findings, it becomes `unknown` (`ok_without_finding`). A risk finding cited beside a supporting one is kept. | `TestRiskOnlyOKIsNotYes`, `TestEnforce` "contact ok on risk only demoted" and "...risk and mitigation stands" |
| L1 | A setting made only of whitespace or invisible format characters (Unicode Cf, for example U+200B, U+2060, U+FEFF) counts as blank in `Enforce` and `Decide`. | `TestEnforce` "zero-width setting demoted", `TestSettingTrimmed` |
| L2 | The heading and exit criterion no longer say "v1". They name `checklist.Version`. | none (documentation) |

**Remediation, round 3** ([review](adversarial-review/t1.1-round3.md)). `checklist.Version` is bumped to `v4`.

| Finding | Fix | Test |
|---|---|---|
| M1 | Generalized: in **every** area, `ok` needs at least one remaining mitigation or neutral finding. Otherwise it becomes `unknown` (`ok_without_finding`). That covers `ok` with no findings, and `ok` citing only risks, such as money `ok` on a pay-to-win quote. `Decide` gains a rule (5): an `unknown` area that still cites findings makes the verdict `unclear`, because its evidence did not support the grade. So money `ok` on a pay-to-win risk gives `unclear`, not `yes`. Rules 6 and 7 (settings, yes) are renumbered. An evidence-free `unknown` still counts only toward the three-unknowns rule. | `TestOKNeedsSupportEveryArea` (each area in turn, plus the round-3 money probe), `TestEnforce` money cases |
| L1 | Settings are trimmed of whitespace and invisible format characters (Cf) at both ends, with the same predicate `blank` uses. So `"\u200bRestricted mode\u200b"` becomes `"Restricted mode"`, and `Settings` dedupes it with the clean form. | `TestSettingInvisibleEdges` |

**Remediation, round 4: user decision.** `checklist.Version` is bumped to `v5`.

- **Decision (the user's): an `ok` grade that cites only risk findings becomes `flag`**, so the verdict is `no`. The verified evidence contradicts the grade. Rule `ok_on_risk_only`. An `ok` with **no** remaining finding becomes `unknown` (`ok_without_finding`), as before. An `ok` citing a risk beside a mitigation or neutral finding stays `ok`, because weighing mixed evidence is the judge's job.
- **Round 3's extra Decide rule ("an `unknown` that still cites findings is `unclear`") is removed.** It existed only to keep a risk-only `ok` from reaching `yes`, and that case is now a `flag`. The cases still left are an unbacked `flag` or `ok_with_setting` demoted to `unknown`. The spec already says what happens to those (they become `unknown`) and gives them no special verdict, so the spec's own rules apply: contact or content `unknown` is `unclear`, and three or more `unknown` is `unclear`. That is the smallest rule set the spec supports.
- **Final rules.** `Enforce`, per grade: drop cited ids that are missing or belong to another area, and keep repeated ids once. `flag` with no risk finding becomes `unknown`. `ok_with_setting` with a blank setting or no finding becomes `unknown`. `ok` with no finding becomes `unknown`. `ok` on risks only becomes `flag`. A skipped area is added as `unknown`. An invalid or duplicate grade is discarded. `Decide`: invalid set is refused; any `flag` is `no`; contact or content `unknown` is `unclear`; three or more `unknown` is `unclear`; any `ok_with_setting` is `yes_with_settings`; otherwise `yes`.

| Change | Test |
|---|---|
| `ok` on risks only becomes `flag`, in every area. Money `ok` on a pay-to-win risk gives `no`. | `TestOKNeedsSupportEveryArea`, `TestRiskOnlyOKIsNotYes`, `TestEnforce` "...raised to flag" |
| `ok` with no finding (including only missing ids) becomes `unknown`. One evidence-free `unknown` outside contact and content still allows `yes`. | `TestOKNeedsSupportEveryArea`, `TestEnforce` "ok on missing id is unknown" |
| Decide rule "an `unknown` with findings is `unclear`" removed. Rules renumbered 1–6. | `TestDecideRules` |
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

- [ ] Checklist frozen (`checklist.Version`, now `v5`), with the verdict question decided.
- [ ] The quote check rejects a changed word and an invented quote, by test.
- [ ] Example evidence and card payloads committed, one card per verdict.
- [ ] API and error codes documented, with examples.
- [ ] Error codes ride the job terminal through `job.Failure` (Keel v0.7.3, nrynss/keel#83).
