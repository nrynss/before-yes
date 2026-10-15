# Budget

This doc sets the spend ceilings for Before Yes and says why. The settings file (`before-yes.toml`, copied from `before-yes.toml.example`) carries the numbers; this doc is the reason for them. If the two disagree, fix the file to match this doc, or change this doc with its reason.

Two pools are metered (`internal/spend`):

- **model**: Token Factory spend, in USD nanodollars. Lifetime ceiling.
- **tavily**: Tavily credits, in thousandths of a credit. Monthly ceiling.

Model figures are **measured**: the T3.11 fixture replay on Token Factory (reader and judge passes from the T3.10 recordings, entailment re-recorded on Super), checked against the recorded replies (`testdata/eval/replies/`). Tavily figures are fixture-measured and live-measured: T3.11's six gathers reported 7 or 8 credits each. Figures from a transcript or fixture manifest are marked as such.

## Summary

| Ceiling | Value | Pool, period | Reason in one line |
|---|---|---|---|
| Model lifetime ceiling | **$20** of the $25 credit | `model`, lifetime (`cost.None`) | Leaves $5 (20%) for re-runs (T3.9), demo pre-computation and measurement error. |
| Tavily monthly ceiling | **900 credits** of the 1,000 free | `tavily`, monthly UTC (`cost.MonthlyUTC`) | 100 credits (10%) kept for a live check above the measured 7 to 8. |
| Daily cap on fresh checks, judging | **5 checks per UTC day** | `checks_daily`, daily UTC | Confirmed by the rule: 5 x 15 x 8 = 600 credits, under 900. |
| Cost per fresh check | **$0.042598 model** (fixture replay after T3.11, mean of three titles; $0.039498 to $0.044894 by title; T3.10 measured $0.040580, T3.9b $0.035349, T3.6b $0.030861). **7 to 8 Tavily credits** (fixture-measured, and the same range on T3.11's six live gathers). | not a ceiling | Input to every ceiling above. |
| Checks covered | **469** by the model ceiling (492 at T3.10, 565 at T3.9b, 648 at T3.6b); **112 to 128 a month** by Tavily | derived | Tavily binds, as it did at the estimate. |

No ceiling or the cap changed. The per-check model cost rose 14.8% at T3.10 ($0.035349 to $0.040580: the definitions and the reader's larger finding sets made the judge prompt longer, two conflict passes ran to their cap, and the entailment check added Nano calls), which cut the checks the $20 ceiling covers from 565 to 492 and the reserve's fresh checks from 141 to 123. T3.11 runs that check on Super instead of Nano. The fixture replay mean is $0.042598, which cuts the model pool from 492 checks to 469 and the reserve's fresh checks from 123 to 117. Neither forces a change: Tavily still binds by a wide margin, and the settings file needs no change.

## Model pool: lifetime ceiling and reserve

**Ceiling: $20.** The Token Factory credit is $25 in total and is not renewed (the Devpost FAQ and the brief's Budget section). The $20 ceiling is 80% of it.

**Reserve: $5.** It covers:
- re-runs of the live eval on real Nano and Super, which book their spend through the same meter. A three-title run costs $0.127795 on the T3.11 fixture replay (T3.10: $0.121741, T3.9b: $0.1060, T3.6b: $0.0926), so $5 covers about 39 such runs (`5 / 0.127795`). Reader output alone, at the 8,192-token reply cap, bounds a three-title run at $0.197 (100 reader calls x 8,192 x $0.24 per million), so even that worst case leaves about 25 runs (`5 / 0.196608`);
- pre-computing the demo titles and alternatives. At the measured mean, $5 pays for about 117 fresh checks (`5 / 0.042598`);
- error in the per-check figure. The figure is the T3.11 fixture replay, so this error is small. A reader or judge prompt change re-opens it (see the T3.11 section).

**Spent so far.** About $0.7778 of the $20 ceiling: about $0.0018 across the three T2.1b live runs (handoff log; the probe alone booked $0.00060618, `dev-diary/live/t2.1b-transcript.txt`), $0.092584 for the three titles in T3.6b, $0.106048 for the T3.9b re-record of the same three titles, $0.209723 for T3.10's re-record rounds (the task's live ledger; the final recordings those rounds ended with replay at $0.121741, the rest paid for superseded rounds and the entailment clarification), and $0.367638 on T3.11's ledger (six live gathers at $0.236605, then entailment re-records and round-3 and round-4 classification attempts on the judge model, including superseded paid replies). The rest is unspent headroom.

**Why a lifetime pool and not a monthly one.** The credit is one lump sum. A monthly window on the model pool would let the account spend $20 a month, which the credit cannot pay for. `cost.None` is the right period for this pool, and it is the default when `PoolConfig.Period` is unset.

## Tavily pool: monthly ceiling

**Ceiling: 900 credits a month.** The free tier is 1,000 credits a month (the brief, and `docs/keys.md`).

**Reason for the 100-credit margin.** Tavily's own usage reports vary from the price list:
- an extract of two successful URLs reported **0** credits, where the price list says 1 per five URLs (`dev-diary/live/t2.2b-transcript.txt`, call 3, measured);
- search with `include_raw_content` reported the same 1 credit as without it (T2.2b, measured);
- the T0.4 fixture captures ran 5 to 6 credits per title for an earlier search plan, 3 to 4 for the Common Sense extracts and 1 to 2 for the advanced extracts (`testdata/titles/README.md`, `dev-diary/PHASE-0-ground.md`, measured). Those captures are not the pipeline's plan.

**The per-check figure, 7 to 8 credits, is fixture-measured and now live-measured.** It is the pipeline's plan (seven basic searches, plus one extract only when a policy or terms candidate exists) replayed against the saved fixtures through the eval's Tavily column (`dev-diary/PHASE-3-pipeline.md`, T3.1 stand-in baseline): Roblox 8, Minecraft 7, Khan Academy Kids 7. T3.11's six live gathers reported the same range: Roblox 8, Minecraft 7, Khan Academy Kids 8, each twice. T3.6b made zero live Tavily calls, and T3.8a's storepage capture spent 1 credit in total, which is not a check's cost.

The margin absorbs a live per-check cost above the 8-credit fixture figure without touching the free tier's limit.

**Period must be `cost.MonthlyUTC`.** `Open` pins it (T2.4b): the tavily pool is always monthly, whatever the caller passes. Opened as a lifetime pool, it would be 900 credits for life, not 900 a month. Keel's `MonthlyUTC` resets at the UTC month start. Tavily's own reset point is not confirmed; see open items.

**Spent so far.** 46 credits recorded across the fixture captures and T2.2b (22 + 13 + 7 + 4, measured), and 0 live credits in T3.6b. T3.11's six live gathers reported 46 more (Roblox 8, Minecraft 7, Khan Academy Kids 8, each twice). The entailment re-record made no further Tavily call. These are development credits; the monthly pool resets, so they do not reduce the judging month.

## Cost of one fresh check (measured)

A fresh check is one run of the pipeline on a title and age with no cache hit. A cache hit costs nothing and does not count against the daily cap.

### Formula

Model cost per check, in USD:

```
C_model = (R_in * 0.06 + R_out * 0.24 + J_in * 0.30 + J_out * 0.90 + E_in * 0.30 + E_out * 0.90) / 1,000,000
```

- `R_in`, `R_out`: reader (Nano) input and output tokens per check, summed over all reader calls.
- `J_in`, `J_out`: judge (Super) input and output tokens per check, summed over all judge calls. **The conflict pass is inside these totals**: the judge count is 2 calls per title, the main pass and one conflict pass (`K = 1`). Do not add `K * C_conflict` on top; that double-counts it.
- `E_in`, `E_out`: entailment-check tokens per check (Super, the judge model, one call per standing non-age grade; age flags check cited risks independently; T3.11 moved the check off Nano).
- Prices, dollars per million tokens, from `before-yes.toml.example`: Nano input 0.06, output 0.24; Super input 0.30, output 0.90.

Tavily cost per check, in credits:

```
C_tavily = sum over the check's search and extract calls of usage.credits, as Tavily reports it
```

Use Tavily's reported `usage.credits`, not the price list (PHASE-3, T3.1). Measured 7 to 8 on the fixtures and on T3.11's live gathers (see the Tavily pool section).

### Measured figures

From the T3.11 fixture replay on Token Factory (the recorded replies in `testdata/eval/replies/` reproduce every token count and price below). Reader and judge-pass tokens are the T3.10 recordings; entailment was re-recorded on Super. T3.10's mean was $0.040580. T3.9b's figures were: Roblox $0.033910, Minecraft $0.047094, Khan $0.025044, mean $0.035349. T3.6b's: Roblox $0.031991, Minecraft $0.040029, Khan $0.020564, mean $0.030861.

| Title | Reader calls | Reader tokens in / out | Judge calls (conflict) | Judge tokens in / out | Entail calls (Super) | Entail tokens in / out | Model $ | Tavily credits (fixture) |
|---|---|---|---|---|---|---|---|---|
| Roblox | 36 | 134,885 / 31,774 | 2 (1) | 30,802 / 16,680 | 7 | 10,977 / 153 | 0.043402 | 8 |
| Minecraft | 45 | 167,562 / 45,567 | 2 (1) | 42,995 / 9,493 | 4 | 8,015 / 64 | 0.044894 | 7 |
| Khan Academy Kids | 19 | 76,765 / 29,118 | 2 (1) | 41,472 / 16,463 | 1 | 2,105 / 16 | 0.039498 | 7 |
| **Mean per check** | 33.3 | **126,404 / 35,486** | 2 (1) | **38,423 / 14,212** | 4 | **7,032 / 78** | **0.042598** | 7.3 (range 7 to 8) |

The entailment check's Super calls are shown apart from the judge pass because they are a separate step with its own count; the judge column is the two grade calls only, and the reader column is read calls only. Entailment tokens are the live judge totals minus those two grade calls (Roblox 41,779 in / 16,833 out on 7 entailment calls plus 2 recorded grade calls, Minecraft 51,010 / 9,557 on 4, Khan 43,577 / 16,479 on 1). The event-extraction reply length varies; other entailment replies here have 16 output tokens. Both conflict passes that hit their 16,000-token cap (Roblox, Khan Academy Kids) were discarded by the parser, so their output tokens bought no grade change; Minecraft's conflict pass answered inside the cap and its grades are its second pass's. T3.11's six live gathers, a different evidence set, averaged $0.042011 model after the entailment re-record (Roblox $0.040024 and $0.045821, Minecraft $0.044690 and $0.032604, Khan Academy Kids $0.041987 and $0.046940) and 7 or 8 Tavily credits. The re-record made no Tavily call.

### Per-check arithmetic

| Term | Tokens per check | Price, $ per million | $ per check |
|---|---|---|---|
| Reader input | 126,404 | 0.06 | 0.007584 |
| Reader output | 35,486 | 0.24 | 0.008517 |
| Entail input (Super) | 7,032 | 0.30 | 0.002110 |
| Entail output (Super) | 78 | 0.90 | 0.000070 |
| Judge input | 38,423 | 0.30 | 0.011527 |
| Judge output | 14,212 | 0.90 | 0.012791 |
| **Total** | | | **0.042598** |

The doc uses **$0.042598**, rounded from the measured $0.04259826 mean. Exact fixture totals are $0.04340226, $0.04489410 and $0.03949842; their sum is $0.12779478. Displayed token rows are rounded. Entailment averages $0.00217960, about 5% of the check. Age-gating flag classification checks cited risks independently and stops on the first verified event; the count therefore varies with the citations, rather than being bounded to one call per area.

Tavily: **7 to 8 credits per check, fixture-measured**. The doc uses 8 for the worst case, and 7 where the best case is shown.

## Checks the budget covers

With the measured figures, and the lower of the two pools binding:

| Limit | Arithmetic | Checks |
|---|---|---|
| Model, $20 ceiling, mean check | 20 / 0.042598 | **469** |
| Model, $20 ceiling, Minecraft-sized check | 20 / 0.044894 | 445 |
| Model, $20 ceiling, Khan-sized check | 20 / 0.039498 | 505 |
| Model, full $25 credit, mean check | 25 / 0.042598 | 586 |
| Tavily, 900 a month, 8 credits per check | 900 / 8 = 112.5 | **112 a month** |
| Tavily, 900 a month, 7 credits per check | 900 / 7 = 128.6 | 128 a month |

Tavily binds at both per-check costs, so the monthly figure is 112 to 128 checks. The model pool does not bind for a month of checks: 469 checks is more than four months at 112 a month.

Checks in the table are fresh checks. Cache hits (`card_max_age_hours`, `evidence_max_age_hours` in the settings) add none to either pool.

## Daily cap on fresh checks during judging

**Decision: 5 fresh checks per UTC day, for the 15-day judging window. Confirmed; no change.** Visitors who paste their own Token Factory and Tavily keys are not counted (the brief, Guardrails).

**Rule.** `cap x judging days x C_tavily <= 900`, with 15 judging days.

**Derivation.**
- Largest cap the rule allows: `floor(900 / (15 x C_tavily))`. At 8 credits, `900 / 120 = 7.5`, so 7. At 7 credits, `900 / 105 = 8.6`, so 8.
- The cap of 5 at the worst measured figure: `5 x 15 x 8 = 600` credits, under 900, with **300 credits spare** for the month.
- Whole window at 5 a day: 75 checks, 600 credits at 8 each (525 at 7).
- Model side: 75 x $0.042598 = **$3.19** at the mean, or 75 x $0.044894 = **$3.37** at Minecraft size. Both far under the $20 ceiling. The model pool is not the reason for the cap.
- The cap holds for any live per-check Tavily cost up to 12 credits (`5 x 15 x 12 = 900`).

**Why not 7.** The rule allows 7 at 8 credits, and 840 of 900 credits would go to judging. That leaves 60 credits for a check above the measured 7 to 8, and for any re-run in the month. T3.11's six live gathers stayed at 7 or 8. 5 holds with 300 to spare. If a live check measures above 8 credits, lower the cap to `floor(900 / (15 x C_tavily))`.

**What the cap protects.** Cached checks are free, and the demo titles are pre-computed. The cap stops a burst of fresh checks from draining the month before judging ends.

### A separate counter, not a daily model-dollar pool

A daily model-dollar pool would be the wrong unit. The limit that binds is checks, or Tavily credits. Daily model spend at the cap is about $0.21 (5 x $0.042598), so a daily dollar cap would never trip and would not express "5 checks a day".

The counter is built in T2.4b: `Meter.BeginCheck(ctx, ref)` returns a `*Check`, which is ended with `Done()` (books one check) or `Fail()` (releases it). `Open` pins `checks_daily` to `cost.DailyUTC`. A cache hit never calls `BeginCheck`. Tests: `internal/spend/checks_test.go`.

## Settings and the example file

`before-yes.toml.example`, `[budget]` section, matches this doc:

- `model_usd = 20`: matches the model ceiling.
- `tavily_credits_monthly = 900`: matches the Tavily ceiling.
- `daily_fresh_checks = 5`: matches the daily cap, read by `BeginCheck` (T2.4b).
- The period for each pool is not a settings key. The code pins it (`cost.None` for model, `cost.MonthlyUTC` for tavily, `cost.DailyUTC` for `checks_daily`).

## Sensitivity: Minecraft-sized titles dominate

Minecraft is the costliest title: 45 reader calls, $0.044894 per check, and 7 Tavily credits. If every check is Minecraft-sized:

- **Model:** 20 / 0.044894 = **445 checks** in the $20 ceiling (5% fewer than the 469 at the mean), 556 of the $25 credit. The $5 reserve covers 111 such checks (`5 / 0.044894`).
- **Tavily:** Minecraft's fixture figure is 7 credits, and its two live gathers were 7 as well, so 900 / 7 = **128 a month**. Tavily still binds, and the monthly ceiling does not change.
- **Daily cap:** 75 checks x $0.044894 = **$3.37** over the 15-day window, under $20. The cap is unchanged.

**Conclusion: no ceiling or cap changes.** Minecraft-sized checks cut the checks the model pool covers by 5%, and the reserve covers about 111 of them. The three titles' costs now sit close together ($0.039498 to $0.044894, a 14% spread): the entailment calls scale with standing grades, and the conflict pass's 16,000-token cap, not the title's size, decides the judge cost.

**Nano output sensitivity.** Each 10,000 Nano output tokens adds $0.0024 per check. Measured Nano output is the reader column: 35,486 tokens per check at the mean ($0.0085), and 45,567 for Minecraft ($0.0109). Entailment output is Super, not Nano. Raising the reader cap from 4,096 to 8,192 moved it by 1.2% at T3.9b (27,774 to 28,096); at T3.10, 2 of 100 reader replies reach the cap (one Roblox, one Minecraft), and most output is not truncation.

## T3.9b (measured): what it changed

T3.9b made the judge cite findings by id, and raised the reader's `max_tokens` to 8,192 for the live set. Its per-title figures are in the "Measured figures" intro above (mean $0.035349); the deltas against T3.6b:

- **Reader.** Input is unchanged (129,654 tokens a check; the reader prompt did not change). Output went from 27,774 to 28,096 tokens a check (+$0.0001). Replies at a cap: 4 of 103 at 4,096 before; 1 of 103 after (Minecraft, at 8,192).
- **Judge.** The longer citation prompt raised input from 27,650 to 34,073 tokens a check (+$0.0019). Output rose from 9,024 to 11,784 (+$0.0025), mostly the thinking-on conflict pass: Minecraft's conflict pass ran to its 16,000-token cap and was discarded, so that title's first-pass grades stood and its conflict call cost $0.0144 for nothing.
- **Net.** +$0.0045 a check, $0.030861 to $0.035349.
- **No ceiling changed.** The model pool has 565 checks at the mean and 424 at Minecraft size, against a Tavily limit of 112 to 128 a month. The reserve covers 141 mean checks. A ceiling or the daily cap would change only if the per-check cost passed the point where 75 judging-window checks neared $20, about $0.27, or if Tavily credits per check rose above 12.

## T3.10 (measured): what it changed

T3.10 gave the judge each area's own flag and ok definitions, added the entailment check (one Nano call per standing flag or ok_with_setting), and re-recorded the reader. The reader and judge columns in the table above are these recordings. The entailment column and the dollar column are the later Super re-record; T3.10's own entailment was Nano and its mean was $0.040580. The deltas against T3.9b (mean $0.035349):

- **Reader.** The re-recorded reader finds more per title (165, 259 and 212 verified findings), so output rose from 28,096 to 35,486 tokens a check ($0.0067 to $0.0085). Input fell from 129,654 to 126,404: the product-identity filter drops sibling pages before reading, so Khan's reader makes 19 calls, not 22, and Roblox and Minecraft are unchanged. Reader replies at the 8,192 cap: 2 of 100 (one Roblox, one Minecraft).
- **Judge.** The definitions lengthen the prompt and the larger finding sets lengthen the findings block: input 34,073 to 38,423 tokens a check. Output 11,784 to 14,212, mostly because two of the three conflict passes now run to their 16,000-token cap and are discarded (Roblox and Khan Academy Kids; Minecraft's answered inside the cap at T3.10 after being the capped one at T3.9b).
- **Entailment.** 3.7 Nano calls a check at the mean (6 Roblox, 4 Minecraft, 1 Khan), $0.000162 a check, 0.4% of the check. One call per standing grade, and only standing grades are checked. After the first entailment round refused Roblox's developer_record flag on a filed-lawsuit citation, one sentence of clarification (an enforcement action filed against the developer is the record, outcome or no outcome) was added and the 11 entailment calls were re-recorded for $0.000485; no reader or judge call was re-made.
- **Net.** +$0.005231 a check, $0.035349 to $0.040580.
- **No ceiling changed.** The model pool has 492 checks at the mean and 469 at Minecraft size, against a Tavily limit of 112 to 128 a month. The reserve covers 123 mean checks. The T3.9b threshold for a change ($0.27 a check) is still far away.

## T3.11 (measured): what it changed

T3.11 kept the reader and judge recordings and re-recorded entailment on Super, thinking off. The fixture replay is the table above. Against T3.10 (mean $0.040580):

- **Reader and judge passes.** Unchanged. Their recordings were replayed.
- **Entailment.** The current fixture replay uses 4 Super calls a check at the mean (7 Roblox, 4 Minecraft, 1 Khan), $0.002180 a check, 5% of the check after the canonical-claim/source-only prompt (7,032 in / 78 out). Nano had been $0.000162. The mean went from $0.040580 to $0.042598.
- **Live gathers.** Six separate gathers (two each of Roblox, Minecraft, and Khan Academy Kids) cost $0.236605 of model spend before the entailment re-records. After the re-records the six replay at $0.042011 mean. The whole task ledger is $0.367638. Tavily reported 8, 7, and 8 credits, twice each. The re-record made no Tavily call.
- **No ceiling changed.** The model pool has 469 checks at the mean and 445 at Minecraft size, against a Tavily limit of 112 to 128 a month. The reserve covers 117 mean checks. The $20, 900, and 5 ceilings are unchanged.

## Open items

1. **Tavily's reset point.** The brief says the free tier resets monthly. Whether it resets at the UTC month start, which `cost.MonthlyUTC` assumes, is not confirmed. If it resets at another time, the pool must use that boundary, or the pool will drift from Tavily's own count near the reset.
2. **Live Tavily credits.** T3.11 recorded `usage.credits` on six live gathers: 8, 7, and 8, twice each. None exceeded 8, so the daily cap stays 5. A later check above 8 still lowers the cap by the rule above.
3. **Conflict-pass cap.** A conflict pass hit its 16,000-token cap at T3.9b (Minecraft) and two did at T3.10 (Roblox and Khan Academy Kids, $0.0189 and $0.0206 each for a discarded reply). If that recurs, raising the cap costs up to $0.0144 a pass (16,000 x $0.90 per million). Not done: the arithmetic does not force it, but three capped passes in two tasks says the thinking pass often will not fit; a cheaper answer is to ask the conflict pass for less.
4. **Pre-computed demo titles.** They are fresh checks. Run them before the judging window so they count against the earlier month, not the judging month.

Resolved by T3.6b: the Nano output tokens and the conflict-pass cost, both now measured and in the figures above. The cap is re-derived and confirmed at 5.

## Formula for re-measuring

Fill from a live run, and do not reuse the figures above:

```
C_model   = (R_in*0.06 + R_out*0.24 + J_in*0.30 + J_out*0.90 + E_in*0.30 + E_out*0.90) / 1e6   [USD per check; J includes the conflict pass, E is the entailment calls on Super]
C_tavily  = sum of usage.credits over the check                        [credits per check; live, not fixture]
checks_model   = 20 / C_model
checks_tavily  = 900 / C_tavily        (per month)
daily_cap_max  = floor(900 / (15 * C_tavily))
```

Report the per-title figures and the three-title mean in the handoff. Then update the summary table and the settings file together.
