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

**Remediation, round 2** ([review](adversarial-review/t1.1-round2.md)). `checklist.Version` is bumped to `v3`, because an `Enforce` rule changed again.

| Finding | Fix | Test |
|---|---|---|
| M1 | Contact or content `ok` needs at least one cited **mitigation or neutral** finding. If it cites only risk findings, it becomes `unknown` (`ok_without_finding`). A risk finding cited beside a supporting one is kept. | `TestRiskOnlyOKIsNotYes`, `TestEnforce` "contact ok on risk only demoted" and "...risk and mitigation stands" |
| L1 | A setting made only of whitespace or invisible format characters (Unicode Cf, for example U+200B, U+2060, U+FEFF) counts as blank in `Enforce` and `Decide`. | `TestEnforce` "zero-width setting demoted", `TestSettingTrimmed` |
| L2 | The heading and exit criterion no longer say "v1". They name `checklist.Version`. | none (documentation) |
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

**New dependency: `golang.org/x/text`** (for `unicode/norm`, which the standard library lacks). This is outside `owns`: `go.mod` and `go.sum` change by this one module. The pinned version and the reason are in the round-2 table below. The round-1 text gave a wrong reason. The orchestrator copies this reason into the handoff log, as AGENTS.md requires.

`TestFixtureReview` now cuts its quote at a word boundary. The old byte cut ended mid-word, which the H1 fix correctly rejects.

**Remediation, round 2** ([review](adversarial-review/t1.2-round2.md)).

| Finding | Fix | Test |
|---|---|---|
| H1 | An apostrophe, straight or curly (curly is normalized to `'`), with a word rune on each side is part of the word. "Parents say strangers can" no longer verifies inside "strangers can't", and "The chat isn" no longer verifies inside "isn't". | `TestWordBoundariesRound2` (straight and curly) |
| L1 | Each end of a match must touch the text's edge, whitespace, or a punctuation rune in the text beside a word rune in the quote. A quote that starts or ends with punctuation can no longer begin or end inside a token ("-safe" in "un-safe"). Underscore is a word rune ("safe" in "filter_safe"). | `TestWordBoundariesRound2` |
| L2 | The round-1 reason for the pin was wrong. **Corrected:** the graph already requires `golang.org/x/sync v0.22.0`, and every `x/text` up to v0.41.0 requires sync v0.22.0 or older (checked against the proxy's `.mod` files), so none of them bumps anything. v0.42.0 needs sync v0.23.0. The pin is now **v0.41.0**, the newest version that changes no other requirement. Only the `golang.org/x/text` line in `go.mod` and its `go.sum` lines change. | `git diff main -- go.mod` |
| T1.3 round 2 L1 | The `Source.Kind` vocabulary comment (`article`, `store` added) is now made here in T1.2, which owns `internal/finding/`, rather than on T1.3's branch. Comment only. | `git diff task-t1.2-finding task-t1.3-payloads -- internal/finding` is empty after the merge |
---

### T1.3: Evidence and card payloads ★
```yaml
requires:   T1.1, T1.2
fixture-ok: yes
size:       S · frontier
owns:       internal/pipeline/types.go, internal/pipeline/types_test.go, testdata/contracts/evidence.json, testdata/contracts/card-*.json
status:     in-review
```
**Evidence** is everything read about a title before any age applies: title, collected at, App Store info, verified findings, stats. It is cached by title and reused across ages.

**Card** is one judgement for one age: title, age, checklist version, checked at, collected at, verdict, six grades in card order, settings to turn on, the findings the grades cite plus every risk finding, alternatives (T3.5), app info, and stats. Stats cover sources, reviews, chunks, failed chunks, drops by reason, conflicted areas, missing source kinds, Tavily credits, read and judge cost in nanodollars, and both model names.

Commit one example card per verdict (`card-yes.json`, `card-settings.json`, `card-no.json`, plus `card-unclear.json` if T1.1 keeps it) and one `evidence.json`. P5 builds against these files before any live data exists. `card-no.json` is the review 312 moment.

**Done when:** The example payloads round-trip through the Go types with no loss, and a test fails if a field is added to a type without updating the examples.


**Implementation notes (in review).**
- **Outside `owns`:** `internal/pipeline/types_test.go`. The Done-when needs a test and `pipeline_test.go` belongs to T3.4, so the test lives in its own file. Raise it if a reviewer wants it moved.
- `card-unclear.json` is committed because T1.1 provisionally keeps `unclear`.
- **The examples are generated, not hand-written.** `go test ./internal/pipeline -run TestGenerate -update` builds them from the T0.4 fixtures: every quote goes through `finding.Verify` against real review or Tavily text, and every grade through `Enforce` and `Decide`. Without `-update` the same test fails if a committed file is stale. Stats, card ids and times are illustrative.
- **Review 312:** in the captured Roblox feed, review 312 is not about contact. `card-no.json` uses review 355 ("if your kid plays Roblox high chance a child predator might ask where you are") as the deciding contact quote. The brief's "review 312" is the illustration; the index will be whatever the live feed holds.
- `card-unclear.json` uses Toca Life World with reviews withheld, as a shape example only. It is not a prediction (the README expects yes with settings).
- Tests: strict decode with unknown fields refused and re-encode compared as JSON (round trip); every JSON member path of `Card` and `Evidence` must appear in at least one example (adding a field fails); examples obey `Decide`, `Enforce`, `Settings`, `checklist.Version` and the finding id hash.
- Money in nanodollars (`read_cost_nano`, `judge_cost_nano`). `app` is `null` when no App Store hit matched. Optional values are present-but-empty rather than omitted, except `grades[].setting` and `grades[].reason`.

**Remediation, round 1** ([review](adversarial-review/t1.3-round1.md)). The examples were regenerated on top of the T1.1 and T1.2 fixes, under checklist `v2`.

| Finding | Fix | Test |
|---|---|---|
| M1 | The orchestrator added `internal/pipeline/types_test.go` to T1.3's `owns` (above). It holds the contract tests and the example generator, and only T1.3 edits it. T3.4 keeps `pipeline_test.go` and must not touch this file. | `git diff --stat` shows only owned paths plus the merges from T1.1 and T1.2 |
| M2 | Every example `ok` on contact or content now cites a real, verified finding. `card-yes`: contact cites the App Store listing's "Made For Ages 0–5" (Kids category); content cites review 311. `card-settings`: content cites the official Minecraft parents' guide ("some elements of mild violence"). The examples pass `Enforce` with zero demotions under the new `ok_without_finding` rule. | `TestExamplesObeyRules`, `TestGenerate` |
| L1 | Handoff note: `card-no.json`'s deciding contact quote is review 355. The captured feed's review 312 is not about contact. BRIEF's "review 312" is an illustration, and T6.3 and T6.4 should cite whichever index the live run holds. | none (a note) |
| L2 | The Common Sense Media parents' guide is now kind `article`, not `forum`. Source kinds are: `review`, `policy`, `terms`, `reddit`, `forum` (a discussion board), `article` (an editorial guide or explainer), `news`, `rating` (a rating board or official parents' guide), `store` (an app-store listing). T3.1 assigns them. The `finding.Source.Kind` comment lists them (a comment, not behaviour). | `card-no.json` |

**Known gap in `card-yes`:** the Khan contact evidence is thin. The listing says only that the app is in Apple's Kids category, and no fixture says outright "no chat". The Khan fixtures also hold a content risk that the example leaves out: review 17 ("scary devil faces ... popped up"). The examples are contract shapes. Khan's real verdict belongs to T3.6b.

**Remediation, round 2** ([review](adversarial-review/t1.3-round2.md)). The examples were regenerated on top of the T1.1 and T1.2 round-2 fixes, under checklist `v3`.

| Finding | Fix | Test |
|---|---|---|
| M1 | **User decision: `card-yes.json` is accepted as it is.** It is a contract example, not evidence about Khan Academy Kids. Its thin contact quote ("Made For Ages 0–5") and the content risk it leaves out (review 17) are known and accepted. Neither T3.3's prompt nor P5 should treat it as a model of good evidence. Khan's real verdict belongs to T3.6b. The card still passes `Enforce` under `v3`: its contact finding is neutral, so it counts as support. | `TestExamplesObeyRules` |
| L1 | The `Source.Kind` comment edit now lands in T1.2 (`task-t1.2-finding`), which owns `internal/finding/`. After the merge, T1.3's diff against T1.2 touches nothing under `internal/finding/`. | `git diff task-t1.2-finding -- internal/finding` is empty |
| inherited | The examples were regenerated after T1.1 round 2 M1 (`v3`, supporting finding needed) and T1.2 round 2 H1 and L1 (stricter word boundaries). Every quote still verifies, and every card passes `Enforce` with zero demotions. | `TestGenerate`, `TestExamplesObeyRules` |
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

- [ ] Checklist frozen (`checklist.Version`, now `v3`), with the verdict question decided.
- [ ] The quote check rejects a changed word and an invented quote, by test.
- [ ] Example evidence and card payloads committed, one card per verdict.
- [ ] API and error codes documented, with examples.
- [ ] Error codes ride the job terminal through `job.Failure` (Keel v0.7.3, nrynss/keel#83).
