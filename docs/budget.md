# Budget

This doc sets the spend ceilings for Before Yes and says why. The settings file (`before-yes.toml`, copied from `before-yes.toml.example`) carries the numbers; this doc is the reason for them. If the two disagree, fix the file to match this doc, or change this doc with its reason.

Two pools are metered (`internal/spend`):

- **model**: Token Factory spend, in USD nanodollars. Lifetime ceiling.
- **tavily**: Tavily credits, in thousandths of a credit. Monthly ceiling.

Figures marked **estimate** come from the brief's arithmetic and the per-call figures measured so far. They are pending T3.6b, the live quality pass on Token Factory. Figures marked **measured** come from a transcript or a fixture manifest.

## Summary

| Ceiling | Value | Pool, period | Reason in one line |
|---|---|---|---|
| Model lifetime ceiling | **$20** of the $25 credit | `model`, lifetime (`cost.None`) | Leaves $5 (20%) for the T3.6b live eval, demo pre-computation and estimate error. |
| Tavily monthly ceiling | **900 credits** of the 1,000 free | `tavily`, monthly UTC (`cost.MonthlyUTC`) | 100 credits (10%) kept for measured variance in Tavily's per-call credits. |
| Daily cap on fresh checks, judging | **5 checks per UTC day** | new counter, daily UTC (see the daily cap section) | Holds the 15-day judging window inside the Tavily month even at 12 credits per check. |
| Cost per fresh check | **about $0.026 and 8 Tavily credits** (estimate, pending T3.6b) | not a ceiling | Input to every ceiling above. |
| Checks covered | **about 780** by the model pool; **about 112 a month** by Tavily at 8 credits, so Tavily binds | derived | Tavily is the tighter limit, as the brief said. |

## Model pool: lifetime ceiling and reserve

**Ceiling: $20.** The Token Factory credit is $25 in total and is not renewed (the Devpost FAQ and the brief's Budget section). The $20 ceiling is 80% of it.

**Reserve: $5.** It covers:
- the live eval on real Nano and Super (T3.6b), which books its own spend through the same meter. At the estimate below, $5 pays for about 196 title runs;
- pre-computing the demo titles and alternatives;
- error in the per-check estimate. Nano's output tokens are not in the brief's arithmetic (see the estimate), so the estimate may be low.

**Spent so far.** About $0.0018 across the three T2.1b live runs (handoff log). The T2.1b probe alone booked $0.00060618 (`dev-diary/live/t2.1b-transcript.txt`). Development spend is far below the reserve, so the reserve is unused headroom, not a shortfall.

**Why a lifetime pool and not a monthly one.** The credit is one lump sum. A monthly window on the model pool would let the account spend $20 a month, which the credit cannot pay for. `cost.None` is the right period for this pool, and it is the default when `PoolConfig.Period` is unset.

## Tavily pool: monthly ceiling

**Ceiling: 900 credits a month.** The free tier is 1,000 credits a month (the brief, and `docs/keys.md`).

**Reason for the 100-credit margin.** Tavily's own usage reports vary from the price list:
- an extract of two successful URLs reported **0** credits, where the price list says 1 per five URLs (`dev-diary/live/t2.2b-transcript.txt`, call 3, measured);
- search with `include_raw_content` reported the same 1 credit as without it (T2.2b, measured);
- the fixture captures ran 5 to 6 credits per title for the search plan, 3 to 4 for the Common Sense extracts and 1 to 2 for the advanced extracts (`testdata/titles/README.md`, `dev-diary/PHASE-0-ground.md`, measured). The pipeline's plan is different from those captures, so these figures set the range, not the planned cost.

The margin absorbs a per-check cost above the 8-credit plan without touching the free tier's limit.

**Period must be `cost.MonthlyUTC`.** Nothing in `internal/spend` binds the tavily pool to a window: `PoolConfig.Period` is set by the caller, and the zero value is a lifetime pool. Opened with `cost.None`, the pool would become 900 credits for life, not 900 a month. The wiring task must pin `cost.MonthlyUTC` for the tavily pool (the T2.4 forward note). Keel's `MonthlyUTC` resets at the UTC month start. Tavily's own reset point is not confirmed; see open items.

**Spent so far.** 46 credits recorded across the fixture captures and T2.2b (22 + 13 + 7 + 4, measured). These are development credits; the monthly pool resets, so they do not reduce the judging month.

## Cost of one fresh check (estimate, pending T3.6b)

A fresh check is one run of the pipeline on a title and age with no cache hit. A cache hit costs nothing and does not count against the daily cap.

### Formula

Model cost per check, in USD:

```
C_model = (R_in * p_R_in + R_out * p_R_out + J_in * p_J_in + J_out * p_J_out + K * C_conflict) / 1,000,000
```

- `R_in`, `R_out`: Nano (reader) input and output tokens per check, summed over all chunks. Measured: **T3.6b**.
- `J_in`, `J_out`: Super (judge) input and output tokens per check, main pass. Measured: **T3.6b**.
- `K`: number of conflict-pass calls per check (thinking on). Measured: **T3.6b**.
- `C_conflict`: cost of one conflict-pass call. Measured: **T3.6b**.
- Prices, dollars per million tokens, from `before-yes.toml.example`: Nano input 0.06, output 0.24; Super input 0.30, output 0.90.

Tavily cost per check, in credits:

```
C_tavily = sum over the check's search and extract calls of usage.credits, as Tavily reports it
```

Measured: **T3.6b**. Use Tavily's reported `usage.credits`, not the price list (PHASE-3, T3.1).

### Working estimate

| Term | Value used | Source |
|---|---|---|
| Nano input | 200,000 tokens | Brief, Budget table (estimate) |
| Nano output | **not in the brief** | Treated as 0 here; see the sensitivity below |
| Super input | 30,000 tokens | Brief (estimate) |
| Super output | 5,000 tokens | Brief (estimate) |
| Conflict pass | not in the brief; `K * C_conflict` = 0 here | Estimate gap |
| Model per check | **about $0.0255** (0.012 + 0.0045 + 0.009) | Formula above, brief's inputs |
| Tavily per check | **8 credits** | PHASE-3 source plan, "about eight" (estimate, not measured) |

The brief rounds the model figure to "about 3 cents". Its $25 "covers about 800 checks" uses that rounded figure.

**Sensitivity, which the reserve has to absorb until T3.6b measures it:**
- every 10,000 Nano output tokens adds $0.0024 per check;
- the conflict pass adds a judge call, and the only measured figure for one is the T2.1b probe: 46 input and 179 output tokens, $0.0001749 (`dev-diary/live/t2.1b-transcript.txt`, line 33). That prompt is far smaller than a real judge prompt, so it is a floor per call, not a per-check figure.

These are estimates. Do not read them as measurements.

## Checks the budget covers

With the estimate, and the lower of the two pools binding:

| Limit | Arithmetic | Checks |
|---|---|---|
| Model, $20 ceiling | 20 / 0.0255 | about 784 |
| Model, full $25 credit | 25 / 0.0255 | about 980 |
| Tavily, 900 a month, 8 credits per check | 900 / 8 | **about 112 a month** |
| Tavily, 900 a month, 10 credits | 900 / 10 | 90 |
| Tavily, 900 a month, 12 credits | 900 / 12 | 75 |
| Tavily, 900 a month, 15 credits | 900 / 15 | 60 |

Tavily binds at every per-check cost the estimate covers. The model pool does not bind for a month of checks.

Checks in the table are fresh checks. Cache hits (`card_max_age_hours`, `evidence_max_age_hours` in the settings) add none to either pool.

## Daily cap on fresh checks during judging

**Decision: 5 fresh checks per UTC day, for the 15-day judging window.** Visitors who paste their own Token Factory and Tavily keys are not counted (the brief, Guardrails).

**Derivation.** The Tavily monthly pool is the binding limit, so the cap is set from it:

- Daily Tavily allowance: 900 / 15 = **60 credits a day**.
- At 8 credits per check: 5 checks = 40 credits, 20 credits spare each day.
- At 12 credits per check: 5 checks = 60 credits, exactly the allowance.
- Whole window: 5 checks x 15 days = **75 checks**. At 12 credits each that is 900 credits, the whole month, so the cap holds the pool even at the high end of the range.
- Model side: 75 checks x $0.0255 = **about $1.91**, far under the $20 ceiling. The model pool is not the reason for the cap.

**What the cap protects.** Cached checks are free, and the demo titles are pre-computed. The cap stops a burst of fresh checks from draining the month before judging ends.

### Does the daily cap need a daily model pool or a separate counter?

**A separate counter, not a daily model-dollar pool.** Reasons, from reading `internal/spend` and Keel v0.7.3 (`cost/period.go`, `cost/sqlitestore`):

1. **Keel supports a daily window.** `cost.DailyUTC` exists and `sqlitestore` accepts it. The period is set per pool in `PoolConfig`, so a daily pool is possible.
2. **The meter has no daily pool and no way to hold two pools at once.** `spend.Open` accepts only `PoolModel` and `PoolTavily` (`known()`, `poolNames`), and `Spend` reserves against one pool per call. A check that must pass both the lifetime model pool and a daily cap would need two reservations that succeed or fail together. The meter does not do that today.
3. **A daily dollar pool counts the wrong unit.** The daily limit that binds is checks, or Tavily credits. Daily model spend is about $0.13, so a daily model-dollar cap would never trip. It would not express "5 checks a day".

So the cap is a counter in checks per UTC day. It needs new meter code (proposed task below).

### Proposed new task (not written)

**T2.4b: daily fresh-check counter.** `owns: internal/spend/`.

- Add a pool, for example `PoolChecksDaily`, with `cost.DailyUTC`, a denomination of one whole unit named `check` (`Minor: 1`), and a limit of 5. Add it to `known()`, `poolNames`, `unitOf` and `namespaceOf`, with the namespace `cost_checks_daily`.
- Pin the tavily pool to `cost.MonthlyUTC` in `Open`: refuse a config with `PoolTavily` and a non-monthly period, or default it. This closes the T2.4 forward note (a).
- Give `Spend` (or a new call) a way to reserve against two pools for one check, all or nothing, releasing the first if the second refuses. Alternatively, reserve the daily check first and release it on a refusal, which is simpler but must be tested.
- Tests: a sixth fresh check on one UTC day is refused before any call; the count resets at UTC midnight; a failed check releases its counter; a cache hit does not touch the counter; the booked counts are read back through a fresh SQLite connection.
- The pipeline calls this once per fresh check, not once per model or Tavily call.

## Settings and the example file

`before-yes.toml.example`, `[budget]` section, must match this doc:

- `model_usd = 20`: matches the model ceiling.
- `tavily_credits_monthly = 900`: matches the Tavily ceiling.
- A commented `daily_fresh_checks = 5` line, marked as not read by any code until T2.4b lands. It is commented so that no loader treats it as live.
- The period for each pool is not a settings key. The code pins it (`cost.None` for model, `cost.MonthlyUTC` for tavily).

## Open items

1. **Tavily's reset point.** The brief says the free tier resets monthly. Whether it resets at the UTC month start, which `cost.MonthlyUTC` assumes, is not confirmed. If it resets at another time, the pool must use that boundary, or the pool will drift from Tavily's own count near the reset.
2. **Nano output tokens.** The brief's arithmetic has none. T3.6b must measure them, because findings JSON comes from output. Until then the estimate is low by an amount the reserve must cover.
3. **Conflict-pass cost.** T3.6b must count the conflict pass's calls per check (`K`) and their tokens.
4. **Re-derive the cap from T3.6b.** The daily cap of 5 holds for per-check Tavily costs up to 12 credits. If T3.6b measures more, lower the cap so that `cap x 15 x C_tavily <= 900`.
5. **Pre-computed demo titles.** They are fresh checks. Run them before the judging window so they count against the earlier month, not the judging month.

## Formula for T3.6b

Fill from T3.6b's measured run, and do not reuse the estimates above:

```
C_model   = (R_in*0.06 + R_out*0.24 + J_in*0.30 + J_out*0.90 + K*C_conflict) / 1e6   [USD per check]
C_tavily  = sum of usage.credits over the check                                      [credits per check]
checks_model   = 20 / C_model
checks_tavily  = 900 / C_tavily        (per month)
daily_cap_max  = floor(900 / (15 * C_tavily))
```

Report the measured values for each title, and the averages over the three demo titles, in the T3.6b handoff. Then update the summary table and the settings file together.
