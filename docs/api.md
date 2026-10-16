# HTTP API

The contract between the Go server (P4) and the Chaaya app (P5). Frozen at the end of P1. A change here updates its example in [`testdata/contracts/api/`](../testdata/contracts/api/) in the same commit.

Every example file holds one `request` (method, path, body) and the `response` (status, body). The two `events-*.txt` files are raw server-sent event streams.

All bodies are JSON. Ids are Keel `id` values: a card id starts `c_`, a check id `j_`. Both are unguessable, so a card URL can be shared without exposing other cards.

## Routes

| Route | Answers | Examples |
|---|---|---|
| `POST /api/checks` `{title, age}` | `200 {card_id}` if a fresh card exists for this title, age and checklist version, else `202 {check_id}` | `post-checks-200.json`, `post-checks-200-cached-while-live-off.json`, `post-checks-202.json`, and the error examples below |
| `GET /api/checks/{id}` | `200 {id, status, stage?, message?, current?, total?, card_id?, code?, detail?}` | `get-check-running.json`, `get-check-done.json`, `get-check-error.json`, `get-check-404.json` |
| `GET /api/checks/{id}/events` | Server-sent events in Keel's wire vocabulary | `events-done.txt`, `events-error.txt` |
| `GET /api/cards/{id}` | A card, exactly the shape of [`card-*.json`](../testdata/contracts/) | `get-card.json`, `get-card-404.json` |
| `GET /api/cards` | `{cards: [{id, title, age, verdict, checklist_version, checked_at}]}`: recent cards, one per title and age, newest first | `get-cards.json` |
| `GET /api/checklist` | `{version, areas: [{id, label, question}], levels, verdicts}` in card order | `get-checklist.json` |
| `GET /api/status` | `{live, checklist_version, budget: {token_factory_remaining_nano, tavily_credits_remaining, fresh_checks_remaining_today}}` | `get-status.json` |
| `GET /healthz` | `200 ok` (text), for the deploy check | none |

### `POST /api/checks`

- `title`: non-empty string, trimmed. `age`: integer from 3 to 17. Anything else is `400 invalid_request`.
- Optional visitor keys, as request headers `X-Token-Factory-Key` and `X-Tavily-Key`: both or neither. With both, the check runs on the visitor's own accounts for this request only, the keys are never stored, and it does not count against the daily cap. Half a pair, or an unusable key, is `400 invalid_request` (validation, step 2 below).
- "Fresh" means a card for the same normalized title, age and `checklist.Version`. The cache window is the server's setting (T4.2), not part of this contract.
- A double-click joins the running check: the second request gets the same `check_id`.
- **Order of answers.** The server checks in this order, and the first match answers:
  1. Keel's gate: `passcode_required`, then `rate_limited`. The gate wraps the route, so it runs before anything else.
  2. Validation: `invalid_request`.
  3. **The card cache.** If a fresh card exists, the answer is `200 {card_id}`. This holds even when live checks are off, the budget is spent, or the server is busy, because serving a cached card costs nothing. `post-checks-200-cached-while-live-off.json` shows that exchange: it looks the same as any cache hit.
  4. A running check for the same title and age: `202` with its `check_id`.
  5. Refusals before any paid call, in this order: `live_checks_off`, `budget_exhausted`, `busy`.
  6. Otherwise a new check starts: `202 {check_id}`.

### `GET /api/checks/{id}`

`status` is Keel's job status: `queued`, `running`, `done`, `error`, `cancelled` or `interrupted`.

While a check is `queued` or `running`, `GET /api/checks/{id}` also carries its latest progress, when it has reported any: `stage` (`gather`, `read`, `judge` or `alternatives`), `message` (a parent-facing sentence) and, when the stage counts, `current` and `total`. In `read` they count the source sections read so far, not reviews. A page opened mid-check, or reconnecting, shows these at once; the event stream replays only a retained terminal to a subscriber with no `Last-Event-ID`. A finished check carries none of them.
- `done` carries `card_id`.
- `error` carries `code`, `message` and, when the failure named one, `detail`. `code` is the job's `Result.Code` and `detail` is `Result.Detail` (Keel v0.7.3, nrynss/keel#83), so both survive a restart and a reconnect, and the app keeps `detail.pool`. `message` is the server's fixed sentence for that code. It is not `Result.Err`'s text, which can carry internal causes. An empty `Result.Code` is reported as `job_failed`.
- An unknown id is `404 not_found`.

The app reads this route on load and after a reconnect whose `Last-Event-ID` fell out of the stream's ring, because the ring is best-effort.

### `GET /api/checks/{id}/events`

`text/event-stream`, written with `wire.WriteEvent`. Each frame is `event:`, then `id:`, then one `data:` line, in that order. The example streams are produced by `wire.WriteEvent` itself (see `internal/pipeline/api_examples_test.go`). The stream honours `Last-Event-ID` and replays what its ring still holds.

| `event` | `data` | Terminal |
|---|---|---|
| `progress` | `wire.ProgressEvent`: `{job_id, stage, current?, total?, detail: {"message": "..."}}` | no |
| `done` | `{job_id, status: "done"}`. Then read `GET /api/checks/{id}` for the `card_id` | yes |
| `error` | `wire.ErrorEvent`: `{job_id, status: "error", error: {"error": {code, message, detail?}}}`. The `error` member holds the whole envelope that `wire.WriteError` writes, `error` key included, so the app passes it to the same parser it uses for a failed response | yes |
| `cancelled` | `{job_id, status: "cancelled"}` | yes |
| `interrupted` | `{job_id, status: "interrupted"}`: a restart ended the job; start it again | yes |

`stage` is one of `gather`, `read`, `judge`, `alternatives`. `detail.message` is for display only, for example "reading 500 reviews". The app never branches on it.

Comment frames (`: ping`) keep the connection open and carry nothing.

### The card's `gate` field

`GET /api/cards/{id}` may carry `gate`, added in checklist `v8`: what the pipeline's rule gate did to the grades. The gate is plain code that runs after the model's grades and their checks and before the verdict is computed (`internal/pipeline/rulegate.go`); it makes no model call. It moves a grade only on verified quotes, and every grade it confirms, raises or caps cites at least one verified finding that satisfies its rule. `gate` is omitted when the gate did nothing, and a card saved before `v8` has none, so a client treats a missing or `null` `gate` as an empty list.

Each member is one action, in the order they happened:

| Field | Meaning |
|---|---|
| `area` | The checklist area id. |
| `rule` | A stable rule id, `<area>.<rule>`, such as `money.free_no_in_app_purchases`. The rule table is in `internal/pipeline/rulegate.go`. |
| `from`, `to` | Grade levels before and after. Equal levels mean the grade met its rule and stands. A veto goes to `unknown`, which also lists the area in `thin_areas`. A cap goes from `flag` to `ok_with_setting` and names its setting on the grade. A raise goes from `unknown` to `ok`. |
| `findings` | Ids of the verified findings the action rests on, all on the card. A veto cites none. |
| `note` | The rule's finding, as a plain sentence. When an action changes a grade, the grade's `reason` is set to a deterministic sentence from the cited quote's source, so the reason and the level agree. |

A client never recomputes the verdict from `gate`. It shows the actions under "About this check".

## Errors

Every non-2xx JSON response is Keel's `wire` envelope, written with `wire.WriteError`:

```json
{"error": {"code": "budget_exhausted", "message": "today's budget for fresh checks is spent", "detail": {"pool": "daily_checks"}}}
```

A client branches on `code`, never on `message`. The same envelope, `{"error": {...}}`, rides an `error` event's `error` member, so the app has one parser.

| Code | From | HTTP status | Where | Meaning |
|---|---|---|---|---|
| `invalid_request` | Keel `wire` | 400 | `POST /api/checks` | Bad title or age |
| `not_found` | Keel `wire` | 404 | `GET` by id | No such check or card |
| `internal_error` | Keel `wire` | 500 | any | A dependency fault |
| `rate_limited` | Keel `gate` | 429 | gated routes | `detail.retry_after_seconds` says when to retry |
| `passcode_required` | Keel `gate` | 403 | gated routes | The deployment needs the shared passcode |
| `budget_exhausted` | Before Yes | 402 at request time; job terminal otherwise | `POST /api/checks`, `error` event, `GET /api/checks/{id}` | A spend pool (Token Factory, Tavily, the daily cap) refused. `detail.pool` names it |
| `live_checks_off` | Before Yes | 503 | `POST /api/checks` | The live switch (`flag`) is off. A title with a fresh cached card still gets `200 {card_id}` (see the order above) |
| `busy` | Before Yes | 503 | `POST /api/checks` | Too many checks running. `detail.retry_after_seconds` |
| `no_sources` | Before Yes | job terminal | `error` event, `GET /api/checks/{id}` | Nothing could be read about the title |
| `check_failed` | Before Yes | job terminal | `error` event, `GET /api/checks/{id}` | The check failed for another named reason |
| `job_failed` | Keel `job` | job terminal | `error` event, `GET /api/checks/{id}` | Fallback when a job failed without a `job.Failure` |

### How a job's code reaches the client

The check's work function fails with Keel's `job.Failure` (v0.7.3, nrynss/keel#83), bare or wrapped:

```go
return job.Failure{Code: "budget_exhausted", Detail: map[string]string{"pool": "token_factory"}, Err: err}
```

The Runner puts `Code` and `Detail` on the stream's terminal `error` event, and keeps `Code` in `Result.Code` and `Record.Code`, which `GET /api/checks/{id}` reports after a restart. Before Yes's codes travel only there: never in the job's result data, and never as a workaround for the pre-v0.7.1 `job_failed`-only terminal.

## Decisions taken in this contract

- **402 for `budget_exhausted` at request time**, **503 for `live_checks_off` and `busy`**. HTTP status is advisory; the code is the contract.
- **`passcode_required` is 403**, matching Keel's `gate`.
- **Age range 3 to 17.** The brief's audience is 5 to 14; the wider range keeps a sibling's age valid. The user chose 3 as the floor, matching `pipeline.MinAge` and the Ask form.
- **`GET /api/status` budget fields** use nanodollars for Token Factory, matching card stats.
