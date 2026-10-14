# Recorded model replies (T3.6b)

These are real replies from the hosts, recorded by `go run ./tools/eval -models live`. They replace the hand-written stand-ins for the three demo titles.

Layout, which `tools/eval -models replay` reads:

    testdata/eval/replies/<slug>/<reader|judge>/<ReplyKey>.json

- `<slug>/` holds the Token Factory replies: Nano (reader) and Super (judge) on Token Factory. A title directory that exists switches `-models auto` to replay for that title.
- `openrouter-free/<slug>/` holds the free OpenRouter stand-ins (`nvidia/nemotron-3.5-lightning:free` as the reader and `nvidia/nemotron-3-super-120b-a12b:free` as the judge). They are not Nano or Super. Replay does not read this directory; the live run does, and it answers from these files without a call.

`ReplyKey` and the `Recording` fields (`text`, `input_tokens`, `output_tokens`, `price_nano`, `measured`, `model`) are defined in `tools/eval/replay.go`. The key hashes the role, the messages and the options, so a replay sends the same prompts and gets the same answers as the live run.

The recordings are built from the saved gather fixtures, so no Tavily call was made to produce them. A recording of an unparseable reply is kept: the reader counts it as a failed chunk, the same way the live run did.

To re-record, delete a title's directory and run `go run ./tools/eval -models live -live-only <slug> -live-ledger <file outside the repo>` with the keys exported. A live run with recordings present makes no call.

T3.9b re-recorded the Token Factory replies for roblox, minecraft and khan-academy-kids: the judge prompt now asks for citations by finding id (see `judgeSystem` in `internal/pipeline/judge.go`), so the old judge recordings no longer match, and the reader ran at `max_tokens` 8192 (`tools/eval/live.go`). The `openrouter-free/` recordings are from T3.6b and were not re-recorded; they answer the old prompt.
