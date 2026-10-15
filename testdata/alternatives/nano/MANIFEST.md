# T3.13 Nano alternatives evidence

`capture.go.txt` preserves the historical runner source that ran the production `Pipeline.FindAlternatives` for the three demo titles. Discovery cards use a fixture verdict only to request alternatives; they are not claimed as computed verdicts. `live-roblox/` is a failed harness experiment: it used production `Run` plus `WithAlternatives` and computed the main verdict, but its runner accidentally omitted `Judge.Entail`, which production wires to Super. It does not establish a production live proof or satisfy the live Done clause. Its raw responses and metered spend are retained honestly. No product source was changed during it.

The isolated meter is `/tmp/t313-nano-live/spend.db`, reused across captures and the one live request. Its model limit is 676237000 nanodollars ($0.676237), Tavily limit is 84000 milli-credits (84 credits), and fresh-check limit is 5. Earlier task work spent $0.323763 and 66 Tavily credits; the cumulative authorization is $1.00 and 150 credits. `capture-spend.json` and `live-spend.json` report actual new and cumulative spend; `ledger.json` exports the isolated meter's actual rows. No production budget or database is reused.

Per title:

- `search-01.json` through `search-03.json` are the three real Tavily response objects, in production query order.
- `nano-NN.json` holds the actual parsed model reply with its exact source result and URL. Page metadata is derived from the first recorded result for that URL, matching production deduplication. An initial recorder map retained the last duplicate result; this derived association was corrected from the original recorded search responses. Raw HTTP bodies and model replies were not changed. The recorder now retains first occurrences itself.
- `lookup-NN.json` holds the actual free App Store response and the public product name it confirms. The first lookup is the original title, to populate its App Store identity and primary genre.
- `pending.json` is the production discovery output, before any full candidate check.
- `responses/` holds raw HTTP response bodies, with sequence, service kind and status in filenames. Requests, authorization headers and keys are never recorded. Raw model bodies retain usage and finish reasons.

The response recorder leaves each response body intact for the production client. Its wrapper loads keys outside the repository. Live calls are metered, including unsuccessful Nano JSON responses; invalid quotes or malformed replies never become candidates.

The three discoveries spent $0.01034862 and 9 Tavily credits (37 metered Nano calls; three Tavily searches each). They confirmed:

| Original | Confirmed App Store products |
| --- | --- |
| Roblox (9) | Stardew Valley; Toca Boca World: Game & Play; LEGO® Builder's Journey |
| Minecraft (9) | Skip-Bo™: Solitaire Card Game; Tsuro - The Game of the Path; Similo: The Card Game |
| Khan Academy Kids (5) | ABCmouse: Kids Learning Games |

Nano produced two truncated JSON replies. Among 139 product claims in parsable replies, 81 lines were not complete verbatim page lines and were rejected. Exact character matching intentionally keeps those names out even where a human can recognize stripped Markdown. All accepted names have a literal name witness in their complete original page line and a matching real App Store product. `TestRecordedNanoAndAppStoreDiscovery` replays the genuine responses without a key, including free rejection of non-products and unrelated search hits.

The literal witness proves text presence and word boundaries. Whether the page recommends a product and whether a multiword proper name is complete remain Nano judgments, constrained by the prompt and conservative App Store identity matching. No rule-based page name parser remains.

The failed harness trial settled at $0.13111506 and 34 Tavily credits. Together with the discovery captures, its meter holds $0.14146368 and 43 credits, with no reservations remaining. Including the earlier work, the task stood at $0.46522668 and 109 credits before any actual-server retry. This leaves $0.53477332 and 41 credits. Its three candidate checks were metered, but the missing entailment wiring makes their outcomes invalid as production proof; none was shown even by that harness.

## Actual production request

`production-roblox/` records exactly one request to the actual unmodified `cmd/before-yes serve` binary: POST `/api/checks` with Roblox and age 9, then only GET polling and card reads. Its binary hash and public settings are saved beside the API responses. The production builder wires `Judge.Entail` to Super and shares the main/alternative fresh-check meter. No overlay, proxy, model override, manual candidate, or verdict substitution was used. Product source matches `source-sha256.txt`; the unchanged builder and judge hashes are also saved.

The actual request completed with card `c_b42857bfb2dfd6e7d12eabc32d8c3b90`, verdict **unclear**, all six areas unknown and no alternatives. The existing rule requests alternatives only for no or yes-with-settings, so it correctly made no alternatives search or candidate check. This **does not meet the live Done clause**; no further paid request was made.

Observable diagnostics: 41 public sources, 500 reviews, 41 reader chunks, one failed chunk, 138 card findings, 89 unquoted drops, eight bad-area drops, one no-quote drop, and no missing source kind. The actual evidence cache has no row because the existing collection policy does not cache any failed reader chunk. The card and database export preserve the retained findings and stats. The production server did not record intermediate judge/entail replies, so these artifacts do not establish a semantic cause for every area becoming unknown.

Actual production spend was $0.04126986 and 8 Tavily credits, with one fresh main check. No reservations remain. Cumulative spend across the earlier work, discoveries, invalid trial and actual production request is **$0.50649654 and 117 credits**; remaining authorization is $0.49350346 and 33 credits. `production-roblox/cumulative-spend.json` gives the exact components, and both isolated ledgers remain available under their recorded paths.

## Known cache limitation for review

Adding and propagating the primary genre does not invalidate prior evidence cache entries. `GatherVersion` hashes the `Gathered.App` pointer type, not the App struct's fields. An older cached `AppInfo` with an empty Genre can therefore still select the fallback app query until refreshed. Fresh captured/live collection contains the correct primary genre. No production behavior was changed after the live source freeze; this concern is explicitly handed to the fresh reviewer.

## Safe replay

Run `go test ./internal/pipeline -run 'TestRecordedNanoAndAppStoreDiscovery|TestNanoQuoteWitness|TestNanoDiscoveryConfirmsBeforeChecking'` without keys or network access (local test sockets may be required by test fixtures). `capture.go.txt` is historical source, deliberately not runnable: its live branch omitted entailment. Do not use it to regenerate a production proof. The actual proof attempt used the unmodified server binary instead.
