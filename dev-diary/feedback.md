# Feedback log

Friction and wins with the services and libraries Before Yes uses, recorded when they were hit. T6.5 drafts the Devpost feedback on Token Factory and the NVIDIA models from this file. Tavily entries support the Best Use of Tavily case.

Each entry gives what happened, the evidence, what it cost, and what would fix it. Never paste a key.

## Tavily

- **Extract credits do not follow the price list.** The price list says one credit per five extracted URLs. A two-URL basic extract reported `usage.credits` 0 for Minecraft and 1 for Roblox. *Evidence:* the T0.4 MANIFESTs. *Fix:* document the rounding, or report it consistently.
- **A 404 page comes back as a successful extract.** Common Sense `/user-reviews/` URLs that return 404 to a plain GET came back as normal results whose text read "Oops! Looks like that page doesn't exist.", with nothing in `failed_results`. *Evidence:* T0.4b, `tavily-extract-csm-advanced.json` for Roblox and Minecraft. *Cost:* a pipeline would read an error page as a source. *Fix:* put the upstream HTTP status on each result, or route non-2xx pages to `failed_results`.
- **Search on reddit.com returns no raw content.** `include_raw_content: true` gave `raw_content: null` on all 64 hits, and an advanced extract of the thread URLs failed with "Failed to fetch url" for all 8. Only the `content` snippet is usable. *Evidence:* T0.4a and T0.4b fixtures. *Fix:* say in the docs which domains cannot be fetched.
- **Script-rendered review sections do not come through, even at advanced depth.** Common Sense parent and kid review pages extract only the page heading, a count and a summary. *Evidence:* T0.4a and T0.4b. *Cost:* 20 credits across both probes.
- **Works well:** `include_usage` on search and extract makes per-call metering exact, and `include_domains` narrowing works as documented.

## Keel

- **Fixed in v0.7.1–v0.7.3:** cost-store namespaces could not share one file (nrynss/keel#82), and a failed job always carried `job_failed` (nrynss/keel#83). Both were found by this project and fixed upstream.

- **An error event nests the envelope one level deeper than its doc comment suggests.** `wire.ErrorEvent.Error` holds the whole `{"error": {code, message, detail}}` body, so the frame reads `"error":{"error":{...}}`. It is consistent (one parser for responses and events), but easy to document wrongly; T1.4 did. *Evidence:* `testdata/contracts/api/events-error.txt`, written by `wire.WriteEvent` with `wire.NewErrorEvent`. *Fix:* show a full example frame in the `ErrorEvent` doc.

## Token Factory and Nemotron

- *No entries yet.* T2.1b makes the first live calls. Record model ids, the thinking switch, usage reporting, rate limits and latency there.
