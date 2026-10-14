# Budget

This doc sets the spend ceilings for Before Yes and says why. The settings file (`before-yes.toml`, copied from `before-yes.toml.example`) carries the numbers; this doc is the reason for them. If the two disagree, fix the file to match this doc, or change this doc with its reason.

Two pools are metered (`internal/spend`):

- **model**: Token Factory spend, in USD nanodollars. Lifetime ceiling.
- **tavily**: Tavily credits, in thousandths of a credit. Monthly ceiling.

Model figures are **measured**: from T3.6b's live run on Token Factory, checked against the recorded replies (`testdata/eval/replies/`). Tavily figures are **fixture-measured**, not live: T3.6b made no live Tavily call. Figures from a transcript or fixture manifest are marked as such.

## Summary

| Ceiling | Value | Pool, period | Reason in one line |
|---|---|---|---|
| Model lifetime ceiling | **$20** of the $25 credit | `model`, lifetime (`cost.None`) | Leaves $5 (20%) for re-runs (T3.9), demo pre-computation and measurement error. |
| Tavily monthly ceiling | **900 credits** of the 1,000 free | `tavily`, monthly UTC (`cost.MonthlyUTC`) | 100 credits (10%) kept for live variance in Tavily's per-call credits, which no live run has measured yet. |
| Daily cap on fresh checks, judging | **5 checks per UTC day** | `checks_daily`, daily UTC | Confirmed by the rule: 5 x 15 x 8 = 600 credits, under 900. |
| Cost per fresh check | **$0.030861 model** (measured, mean of three titles; $0.020564 to $0.040029 by title). **7 to 8 Tavily credits** (fixture-measured, not live). | not a ceiling | Input to every ceiling above. |
| Checks covered | **648** by the model ceiling; **112 to 128 a month** by Tavily | derived | Tavily binds, as it did at the estimate. |

No ceiling or the cap changed from the estimate. Each still holds, and the settings file needs no change.

## Model pool: lifetime ceiling and reserve

**Ceiling: $20.** The Token Factory credit is $25 in total and is not renewed (the Devpost FAQ and the brief's Budget section). The $20 ceiling is 80% of it.

**Reserve: $5.** It covers:
- re-runs of the live eval on real Nano and Super, which book their spend through the same meter. A three-title run cost $0.0926 at the current cap (T3.6b). Reader output alone, at the 4,096-token reply cap, bounds a three-title run at $0.101, so $5 covers about 49 such runs (`5 / 0.1013`);
- pre-computing the demo titles and alternatives. At the measured mean, $5 pays for about 162 fresh checks (`5 / 0.030861`);
- error in the per-check figure. The figure is now measured, so this error is small; T3.9 may change reader output (see the T3.9 note).

**Spent so far.** About $0.0944 of the $20 ceiling: about $0.0018 across the three T2.1b live runs (handoff log; the probe alone booked $0.00060618, `dev-diary/live/t2.1b-transcript.txt`), plus $0.092584 for the three titles in T3.6b. The rest is unspent headroom.

**Why a lifetime pool and not a monthly one.** The credit is one lump sum. A monthly window on the model pool would let the account spend $20 a month, which the credit cannot pay for. `cost.None` is the right period for this pool, and it is the default when `PoolConfig.Period` is unset.

## Tavily pool: monthly ceiling

**Ceiling: 900 credits a month.** The free tier is 1,000 credits a month (the brief, and `docs/keys.md`).

**Reason for the 100-credit margin.** Tavily's own usage reports vary from the price list:
- an extract of two successful URLs reported **0** credits, where the price list says 1 per five URLs (`dev-diary/live/t2.2b-transcript.txt`, call 3, measured);
- search with `include_raw_content` reported the same 1 credit as without it (T2.2b, measured);
- the T0.4 fixture captures ran 5 to 6 credits per title for an earlier search plan, 3 to 4 for the Common Sense extracts and 1 to 2 for the advanced extracts (`testdata/titles/README.md`, `dev-diary/PHASE-0-ground.md`, measured). Those captures are not the pipeline's plan.

**The per-check figure, 7 to 8 credits, is fixture-measured.** It is the pipeline's plan (seven basic searches, plus one extract only when a policy or terms candidate exists) replayed against the saved fixtures through the eval's Tavily column (`dev-diary/PHASE-3-pipeline.md`, T3.1 stand-in baseline): Roblox 8, Minecraft 7, Khan Academy Kids 7. It is not a live measurement. T3.6b made zero live Tavily calls, and T3.8a's storepage capture spent 1 credit in total, which is not a check's cost. The first live check must record Tavily's reported `usage.credits`.

The margin absorbs a live per-check cost above the 8-credit fixture figure without touching the free tier's limit.

**Period must be `cost.MonthlyUTC`.** `Open` pins it (T2.4b): the tavily pool is always monthly, whatever the caller passes. Opened as a lifetime pool, it would be 900 credits for life, not 900 a month. Keel's `MonthlyUTC` resets at the UTC month start. Tavily's own reset point is not confirmed; see open items.

**Spent so far.** 46 credits recorded across the fixture captures and T2.2b (22 + 13 + 7 + 4, measured), and 0 live credits in T3.6b. These are development credits; the monthly pool resets, so they do not reduce the judging month.

## Cost of one fresh check (measured)

A fresh check is one run of the pipeline on a title and age with no cache hit. A cache hit costs nothing and does not count against the daily cap.

### Formula

Model cost per check, in USD:

```
C_model = (R_in * 0.06 + R_out * 0.24 + J_in * 0.30 + J_out * 0.90) / 1,000,000
```

- `R_in`, `R_out`: reader (Nano) input and output tokens per check, summed over all reader calls.
- `J_in`, `J_out`: judge (Super) input and output tokens per check, summed over all judge calls. **The conflict pass is inside these totals**: T3.6b's judge count is 2 calls per title, the main pass and one conflict pass (`K = 1`). Do not add `K * C_conflict` on top; that double-counts it.
- Prices, dollars per million tokens, from `before-yes.toml.example`: Nano input 0.06, output 0.24; Super input 0.30, output 0.90.

Tavily cost per check, in credits:

```
C_tavily = sum over the check's search and extract calls of usage.credits, as Tavily reports it
```

Use Tavily's reported `usage.credits`, not the price list (PHASE-3, T3.1). Measured on fixtures only: 7 to 8 (see the Tavily pool section).

### Measured figures

From T3.6b's live Token Factory run. The recorded replies in `testdata/eval/replies/` reproduce every token count and price below exactly.

| Title | Reader calls | Reader tokens in / out | Judge calls (conflict) | Judge tokens in / out | Model $ | Tavily credits (fixture) |
|---|---|---|---|---|---|---|
| Roblox | 36 | 134,885 / 24,955 | 2 (1) | 26,444 / 11,084 | 0.031991 | 8 |
| Minecraft | 45 | 167,562 / 37,467 | 2 (1) | 40,050 / 9,965 | 0.040029 | 7 |
| Khan Academy Kids | 22 | 86,514 / 20,899 | 2 (1) | 16,456 / 6,022 | 0.020564 | 7 |
| **Mean per check** | 34.3 | **129,654 / 27,774** | 2 (1) | **27,650 / 9,024** | **0.030861** | 7.3 (range 7 to 8) |

### Per-check arithmetic

| Term | Tokens per check | Price, $ per million | $ per check |
|---|---|---|---|
| Reader input | 129,654 | 0.06 | 0.007779 |
| Reader output | 27,774 | 0.24 | 0.006666 |
| Judge input | 27,650 | 0.30 | 0.008295 |
| Judge output | 9,024 | 0.90 | 0.008122 |
| **Total** | | | **0.030862** from the rounded averages |

The doc uses **$0.030861**, the mean of the three measured per-title totals (`(0.031991 + 0.040029 + 0.020564) / 3 = 0.092584 / 3`). The 0.0000003 difference is the rounding of averaged token counts. Shares of model cost: reader input 25%, reader output 22%, judge input 27%, judge output 26%.

Tavily: **7 to 8 credits per check, fixture-measured**. The doc uses 8 for the worst case, and 7 where the best case is shown.

## Checks the budget covers

With the measured figures, and the lower of the two pools binding:

| Limit | Arithmetic | Checks |
|---|---|---|
| Model, $20 ceiling, mean check | 20 / 0.030861 | **648** |
| Model, $20 ceiling, Minecraft-sized check | 20 / 0.040029 | 499 |
| Model, $20 ceiling, Khan-sized check | 20 / 0.020564 | 972 |
| Model, full $25 credit, mean check | 25 / 0.030861 | 810 |
| Tavily, 900 a month, 8 credits per check | 900 / 8 = 112.5 | **112 a month** |
| Tavily, 900 a month, 7 credits per check | 900 / 7 = 128.6 | 128 a month |

Tavily binds at both per-check costs, so the monthly figure is 112 to 128 checks. The model pool does not bind for a month of checks: 648 checks is more than five months at 112 a month.

Checks in the table are fresh checks. Cache hits (`card_max_age_hours`, `evidence_max_age_hours` in the settings) add none to either pool.

## Daily cap on fresh checks during judging

**Decision: 5 fresh checks per UTC day, for the 15-day judging window. Confirmed; no change.** Visitors who paste their own Token Factory and Tavily keys are not counted (the brief, Guardrails).

**Rule.** `cap x judging days x C_tavily <= 900`, with 15 judging days.

**Derivation.**
- Largest cap the rule allows: `floor(900 / (15 x C_tavily))`. At 8 credits, `900 / 120 = 7.5`, so 7. At 7 credits, `900 / 105 = 8.6`, so 8.
- The cap of 5 at the worst measured figure: `5 x 15 x 8 = 600` credits, under 900, with **300 credits spare** for the month.
- Whole window at 5 a day: 75 checks, 600 credits at 8 each (525 at 7).
- Model side: 75 x $0.030861 = **$2.31** at the mean, or 75 x $0.040029 = **$3.00** at Minecraft size. Both far under the $20 ceiling. The model pool is not the reason for the cap.
- The cap holds for any live per-check Tavily cost up to 12 credits (`5 x 15 x 12 = 900`).

**Why not 7.** The rule allows 7 at 8 credits, and 840 of 900 credits would go to judging. That leaves 60 credits for live variance, which no live run has measured, and for any re-run in the month. 5 holds with 300 to spare. If a live check measures above 8 credits, lower the cap to `floor(900 / (15 x C_tavily))`.

**What the cap protects.** Cached checks are free, and the demo titles are pre-computed. The cap stops a burst of fresh checks from draining the month before judging ends.

### A separate counter, not a daily model-dollar pool

A daily model-dollar pool would be the wrong unit. The limit that binds is checks, or Tavily credits. Daily model spend at the cap is about $0.15 (5 x $0.030861), so a daily dollar cap would never trip and would not express "5 checks a day".

The counter is built in T2.4b: `Meter.BeginCheck(ctx, ref)` returns a `*Check`, which is ended with `Done()` (books one check) or `Fail()` (releases it). `Open` pins `checks_daily` to `cost.DailyUTC`. A cache hit never calls `BeginCheck`. Tests: `internal/spend/checks_test.go`.

## Settings and the example file

`before-yes.toml.example`, `[budget]` section, matches this doc:

- `model_usd = 20`: matches the model ceiling.
- `tavily_credits_monthly = 900`: matches the Tavily ceiling.
- `daily_fresh_checks = 5`: matches the daily cap, read by `BeginCheck` (T2.4b).
- The period for each pool is not a settings key. The code pins it (`cost.None` for model, `cost.MonthlyUTC` for tavily, `cost.DailyUTC` for `checks_daily`).

## Sensitivity: Minecraft-sized titles dominate

Minecraft is the costliest title: 45 reader calls, $0.040029 per check, and 7 Tavily credits. If every check is Minecraft-sized:

- **Model:** 20 / 0.040029 = **499 checks** in the $20 ceiling (23% fewer than the 648 at the mean), 624 of the $25 credit. The $5 reserve covers 124 such checks (`5 / 0.040029`).
- **Tavily:** Minecraft's fixture figure is 7 credits, so 900 / 7 = **128 a month**. Tavily still binds, and the monthly ceiling does not change.
- **Daily cap:** 75 checks x $0.040029 = **$3.00** over the 15-day window, under $20. The cap is unchanged.

**Conclusion: no ceiling or cap changes.** Minecraft-sized checks cut the checks the model pool covers by 23%, and the reserve covers about 124 of them.

**Nano output sensitivity.** Each 10,000 Nano output tokens adds $0.0024 per check. Measured Nano output is 27,774 tokens per check at the mean ($0.0067), and 37,467 for Minecraft ($0.0090). This is the term T3.9 could move.

## T3.9 (open): what it may change

T3.9 makes every grade rest on a verified quote, then re-runs the replay. It may raise the reader's `max_tokens`, which is 4,096 today. T3.6b found 15 reader replies across both sets hit that cap. Raising the cap changes reader output cost only, not reader input, judge cost or Tavily. Re-measure rather than guess:

1. Run the three titles live: `go run ./tools/eval -models live`. It is spend-capped and reads the fixtures, so it spends zero Tavily credits.
2. Take the reader output tokens per check from the new recordings (`output_tokens` in `testdata/eval/replies/<slug>/reader/*.json`), summed per title and averaged over the three titles. This is `R_out'`.
3. New model cost per check: `C_model' = C_model + (R_out' - 27,774) * 0.24 / 1,000,000`. Then rerun the checks-covered arithmetic with `C_model'` in place of $0.030861.
4. Bound it before measuring. At reader cap `M`, reader output per check is at most `N x M x 0.24 / 1,000,000`, where `N` is reader calls per check (34.3 mean; 36, 45 and 22 by title). At `M = 4,096` that bound is $0.0338 per check, against the measured $0.0067.
5. Update the summary and the per-check table together. Only a change to the $20 row or the reserve would change a ceiling. The daily cap depends on Tavily credits, which a reader-cap change does not touch.

## Open items

1. **Tavily's reset point.** The brief says the free tier resets monthly. Whether it resets at the UTC month start, which `cost.MonthlyUTC` assumes, is not confirmed. If it resets at another time, the pool must use that boundary, or the pool will drift from Tavily's own count near the reset.
2. **Live Tavily credits.** Every Tavily figure here is fixture-measured. The first live check must record Tavily's `usage.credits`. If it exceeds 8 per check, lower the daily cap by the rule above.
3. **Reader output after T3.9.** See the T3.9 section. Re-measure after T3.9 lands.
4. **Pre-computed demo titles.** They are fresh checks. Run them before the judging window so they count against the earlier month, not the judging month.

Resolved by T3.6b: the Nano output tokens and the conflict-pass cost, both now measured and in the figures above. The cap is re-derived and confirmed at 5.

## Formula for re-measuring

Fill from a live run, and do not reuse the figures above:

```
C_model   = (R_in*0.06 + R_out*0.24 + J_in*0.30 + J_out*0.90) / 1e6   [USD per check; J includes the conflict pass]
C_tavily  = sum of usage.credits over the check                        [credits per check; live, not fixture]
checks_model   = 20 / C_model
checks_tavily  = 900 / C_tavily        (per month)
daily_cap_max  = floor(900 / (15 * C_tavily))
```

Report the per-title figures and the three-title mean in the handoff. Then update the summary table and the settings file together.
