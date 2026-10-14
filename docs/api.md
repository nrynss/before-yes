# HTTP API

The contract between the Go server (P4) and the Chaaya app (P5). Frozen at the end of P1. A change here updates its example in [`testdata/contracts/api/`](../testdata/contracts/api/) in the same commit.

Every example file holds one `request` (method, path, body) and the `response` (status, body). The two `events-*.txt` files are raw server-sent event streams.

All bodies are JSON. Ids are Keel `id` values: a card id starts `c_`, a check id `j_`. Both are unguessable, so a card URL can be shared without exposing other cards.

## Routes

| Route | Answers | Examples |
|---|---|---|
| `POST /api/checks` `{title, age}` | `200 {card_id}` if a fresh card exists for this title, age and checklist version, else `202 {check_id}` | `post-checks-200.json`, `post-checks-202.json`, and the error examples below |
| `GET /api/checks/{id}` | `200 {id, status, card_id?, code?, message?}` | `get-check-running.json`, `get-check-done.json`, `get-check-error.json`, `get-check-404.json` |
| `GET /api/checks/{id}/events` | Server-sent events in Keel's wire vocabulary | `events-done.txt`, `events-error.txt` |
| `GET /api/cards/{id}` | A card, exactly the shape of [`card-*.json`](../testdata/contracts/) | `get-card.json`, `get-card-404.json` |
| `GET /api/cards` | `{cards: [{id, title, age, verdict, checklist_version, checked_at}]}`: recent cards, one per title and age, newest first | `get-cards.json` |
| `GET /api/checklist` | `{version, areas: [{id, label, question}], levels, verdicts}` in card order | `get-checklist.json` |
| `GET /api/status` | `{live, budget: {token_factory_remaining_nano, tavily_credits_remaining, fresh_checks_remaining_today}}` | `get-status.json` |

### `POST /api/checks`

- `title`: non-empty string, trimmed. `age`: integer from 2 to 17. Anything else is `400 invalid_request`.
- "Fresh" means a card for the same normalized title, age and `checklist.Version`. The cache window is the server's setting (T4.2), not part of this contract.
- A double-click joins the running check: the second request gets the same `check_id`.
- Refusals happen here, before any paid call: `live_checks_off`, `budget_exhausted`, `busy`, and the gate's `rate_limited` and `passcode_required`.

### `GET /api/checks/{id}`

`status` is Keel's job status: `queued`, `running`, `done`, `error`, `cancelled` or `interrupted`.
- `done` carries `card_id`.
- `error` carries `code` and `message`, read from the job's `Result.Code` (Keel v0.7.3, nrynss/keel#83), so they survive a restart.
- An unknown id is `404 not_found`.

The app reads this route on load and after a reconnect whose `Last-Event-ID` fell out of the stream's ring, because the ring is best-effort.

### `GET /api/checks/{id}/events`

`text/event-stream`, written with `wire.WriteEvent`. Each frame carries `id`, `event` and one `data` line. The stream honours `Last-Event-ID` and replays what its ring still holds.

| `event` | `data` | Terminal |
|---|---|---|
| `progress` | `wire.ProgressEvent`: `{job_id, stage, current?, total?, detail: {"message": "..."}}` | no |
| `done` | `{job_id, status: "done"}`. Then read `GET /api/checks/{id}` for the `card_id` | yes |
| `error` | `wire.ErrorEvent`: `{job_id, status: "error", error: {code, message, detail?}}` | yes |
| `cancelled` | `{job_id, status: "cancelled"}` | yes |
| `interrupted` | `{job_id, status: "interrupted"}`: a restart ended the job; start it again | yes |

`stage` is one of `gather`, `read`, `judge`, `alternatives`. `detail.message` is for display only, for example "reading 500 reviews". The app never branches on it.

Comment frames (`: ping`) keep the connection open and carry nothing.

## Errors

Every non-2xx JSON response is Keel's `wire` envelope, written with `wire.WriteError`:

```json
{"error": {"code": "budget_exhausted", "message": "today's budget for fresh checks is spent", "detail": {"pool": "daily_checks"}}}
```

A client branches on `code`, never on `message`. The same envelope body rides an `error` event's `error` member, so the app has one parser.

| Code | From | HTTP status | Where | Meaning |
|---|---|---|---|---|
| `invalid_request` | Keel `wire` | 400 | `POST /api/checks` | Bad title or age |
| `not_found` | Keel `wire` | 404 | `GET` by id | No such check or card |
| `internal_error` | Keel `wire` | 500 | any | A dependency fault |
| `rate_limited` | Keel `gate` | 429 | gated routes | `detail.retry_after_seconds` says when to retry |
| `passcode_required` | Keel `gate` | 403 | gated routes | The deployment needs the shared passcode |
| `budget_exhausted` | Before Yes | 402 at request time; job terminal otherwise | `POST /api/checks`, `error` event, `GET /api/checks/{id}` | A spend pool (Token Factory, Tavily, the daily cap) refused. `detail.pool` names it |
| `live_checks_off` | Before Yes | 503 | `POST /api/checks` | The live switch (`flag`) is off; cached cards still serve |
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
- **Age range 2 to 17.** The brief's audience is 5 to 14; the wider range keeps a sibling's age valid. Narrow it here if the user prefers.
- **`GET /api/status` budget fields** use nanodollars for Token Factory, matching card stats.
