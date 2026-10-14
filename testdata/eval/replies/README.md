# Recorded model replies (T3.6b)

There are no real recordings here yet. Until T3.6b records them, every recorded reply the tests and the eval use is a stand-in:

- The scripted replies in `internal/pipeline/read_test.go`, `judge_test.go` and `pipeline_test.go` are hand-written.
- `tools/eval` falls back to two deterministic stand-ins: a keyword reader and a stance judge. Neither one is Nano or Super.

The user accepted these stand-ins until T3.6b runs. T3.6b must replace them with real recordings from Nano and Super on Token Factory, and from the free stand-in models.

Layout, which `tools/eval -models replay` reads:

    testdata/eval/replies/<slug>/<reader|judge>/<ReplyKey>.json

`ReplyKey` and the `Recording` fields (`text`, `input_tokens`, `output_tokens`, `price_nano`, `measured`, `model`) are defined in `tools/eval/replay.go`. A title directory that exists switches `-models auto` to replay for that title.
