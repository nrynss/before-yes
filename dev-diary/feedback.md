# Feedback log

Friction and wins with the services and libraries Before Yes uses, recorded when they were hit. T6.5 drafts the Devpost feedback on Token Factory and the NVIDIA models from this file. Tavily entries support the Best Use of Tavily case.

Each entry gives what happened, the evidence, what it cost, and what would fix it. Never paste a key.

## Tavily

- **Extract credits do not follow the price list, in both directions.** The price list says one credit per five extracted URLs. A two-URL basic extract reported `usage.credits` 0 for Minecraft and 1 for Roblox (T0.4). Later, two-page extracts reported 0 (Khan, Minecraft) while all-failed extracts reported 1 (Roblox, Toca reddit) — so the price list would overcount an all-failed extract and undercount a successful one. *Evidence:* the T0.4 MANIFESTs and the T2.2 fixture rows. *Cost:* a meter that prices from the price list drifts from the bill. *Fix:* document the rounding, or report it consistently; the client trusts a genuinely reported `credits` and falls back to the price list only when the number is absent.
- **A 404 page comes back as a successful extract.** Common Sense `/user-reviews/` URLs that return 404 to a plain GET came back as normal results whose text read "Oops! Looks like that page doesn't exist.", with nothing in `failed_results`. *Evidence:* T0.4b, `tavily-extract-csm-advanced.json` for Roblox and Minecraft. *Cost:* a pipeline would read an error page as a source. *Fix:* put the upstream HTTP status on each result, or route non-2xx pages to `failed_results`.
- **Search on reddit.com returns no raw content.** `include_raw_content: true` gave `raw_content: null` on all 64 hits, and an advanced extract of the thread URLs failed with "Failed to fetch url" for all 8. Only the `content` snippet is usable. *Evidence:* T0.4a and T0.4b fixtures. *Fix:* say in the docs which domains cannot be fetched.
- **Script-rendered review sections do not come through, even at advanced depth.** Common Sense parent and kid review pages extract only the page heading, a count and a summary. *Evidence:* T0.4a and T0.4b. *Cost:* 20 credits across both probes.
- **Works well:** `include_usage` on search and extract makes per-call metering exact, and `include_domains` narrowing works as documented.

## Keel

- **`throttle.Note` misreports the attempt count when `Transient`/`TransientAttempts` are set.** Both clients give host faults a smaller retry budget than 429s, so `Note` (which counts `Config.Attempts`) annotates a persistent 500 submitted twice as "still throttled after 4 attempts". *Evidence:* T2.1 and T2.2 round-1 reviews; the clients now annotate the transient case themselves. *Cost:* a misleading error text in the pipeline's failure path. *Fix:* have `Note` report the budget actually spent, or let the caller pass the count.
- **An injected clock gets a private pacer per call.** Any `throttle.Config` that injects `Now` or `Sleep` builds a fresh pacer inside `Retry`, so cross-call pacing is unobservable on a test clock; the pacing tests must sleep for real. *Evidence:* T2.1 and T2.2 reports. *Fix:* keep one pacer per client/key, or expose it for tests.
- **`cost.Meter` cannot drive `cost/sqlitestore.Store`.** `Store.Reserve/Settle/Release` take a context and a `Reservation` handle, while `cost.Account`'s methods are context-free with no handle, so `cost.NewMeter` cannot wrap the durable store and no adapter ships in v0.7.3. *Evidence:* T2.4 report; `spend` drives the store directly, the path Keel's own `ExampleOpen` documents. *Cost:* each application reimplements reserve-hold-settle-release. *Fix:* give `Store` a `cost.Account` adapter, or give `cost.Meter` a context-and-reservation shape.
- **Fixed in v0.7.1–v0.7.3:** cost-store namespaces could not share one file (nrynss/keel#82), and a failed job always carried `job_failed` (nrynss/keel#83). Both were found by this project and fixed upstream.

## Token Factory and Nemotron

- *No entries yet.* T2.1b makes the first live calls. Record model ids, the thinking switch, usage reporting, rate limits and latency there.
