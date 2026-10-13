# P0: Ground

```yaml
id:       P0
size:     S
requires: []
blocks:   everything
parallel: partly (user tasks run beside T0.1)
```

**Goal:** Put the repository on git, confirm the paid accounts actually work, settle the three demo titles, and capture real public material for those titles as fixtures. Every later track then builds offline.

**Why this phase comes first:** Each task here is small, and each one gates something large. Without an active Token Factory account, nothing can satisfy the rule that the runtime call must go to Token Factory. Without fixtures, every track spends money just to run its tests. Without the demo titles, there is nothing to build the fixtures or the video around.

Four tasks are the user's: T0.2, T0.3, T0.5, T0.6. Agents prepare everything around them and stop where an account, a key, or a decision is needed.

---

### T0.1: Repository on git footing ★
```yaml
requires:   []
fixture-ok: no
size:       XS · light
owns:       .gitignore, LICENSE, README.md (stub), go.mod, go.sum
status:     done
```
`git init` has run, but nothing is committed. The working tree holds `BRIEF.md`, `LICENSE` (Apache-2.0, copied from Keel), `dev-diary/`, `scratch/` (the earlier backend draft), and a `.env` the user created.

Commit the brief, the licence and `dev-diary/` as the first commit. Ignore `scratch/`, `.env*` except `.env.example`, `data/`, `*.db`, `web/build/`, `node_modules/` and `before-yes.toml`. Before committing, check that `git status` lists neither `scratch/` nor `.env`.

Then move `scratch/go.mod` and `scratch/go.sum` into the worktree, following [the scratch rule](README.md#starting-from-the-scratch-draft), and commit them. Keel stays pinned to v0.7.0 or later, never v0.6.0. The bump to v0.7.1 belongs to the first Keel task (README, "Keel v0.7.1").

**Done when:** `git log` shows the first commits, a fresh clone holds the brief, the licence, the dev-diary and `go.mod`, `git status` is clean, `git ls-files` shows nothing from `scratch/` and no `.env`, and the handoff log records the move.

---

### T0.2: Token Factory account active, credit landed ★
```yaml
requires:   []
fixture-ok: no
size:       XS · user
owns:       none
status:     done
```
The Devpost FAQ warns that some countries cannot onboard to Token Factory billing yet. There is no workaround on Devpost's side, and the Nebius Discord handles each case individually.

Confirm three things: the account is active, the $25 promo credit shows on it, and one chat completion against Nemotron 3 Nano succeeds from the console or the playground.

If onboarding fails, raise it on the Nebius Discord at once and record it here as a finding. Every task that names Token Factory depends on this one: T2.1b, T3.6b, T6.1, T6.3, T6.5.

**Done when:** One successful Nano completion on Token Factory, and the handoff log records the credit balance it showed.

**Status note:** The user confirmed the account is active. The credit balance is not yet recorded. T2.1b's live call will prove Nano works.

---

### T0.3: Keys and settings file
```yaml
requires:   T0.2
fixture-ok: no
size:       S · user
owns:       before-yes.toml.example, docs/keys.md
status:     done
```
An agent writes `before-yes.toml.example` and `docs/keys.md`. The user creates the keys and puts them on the dev machine.

Keys needed:
- **Token Factory** API key. Paid; the real reader and judge.
- **Tavily** API key, free tier (1,000 credits a month).
- **OpenRouter** API key. Free development stand-ins for Nano and Super.
- **build.nvidia.com** key. Optional second free development host.

Settings files hold references, never values. Keel `config` refuses a literal secret. Keys live in an env file outside the repository, mode `0600`, named from the settings file:

```toml
[secrets.tavily]
source = "env_file"
path   = "/home/nryn/.config/before-yes/env"
var    = "TAVILY_API_KEY"
read   = "at_use"
```

`docs/keys.md` lists each key, where to create it, and the env var it goes in. Never paste a value into chat, a commit, or a log.

**Done when:** `before-yes config` prints a plan line marked resolved for every secret, `git grep` finds no key material, and the env file is `0600`.

---

### T0.4: Fixtures from three real titles ★
```yaml
requires:   T0.3, T0.5
fixture-ok: no
size:       M · mid
owns:       testdata/titles/, tools/capture/
status:     done
```
Capture the public material the pipeline reads, for each of the three demo titles, into `testdata/titles/<slug>/`:

- `appstore-search.json`: the iTunes search response.
- `reviews-page-01.json` to `reviews-page-10.json`: Apple's review feed, every page it serves.
- `tavily-search-<kind>.json`: one per planned search (policy, terms, forum, news, rating), the raw response.
- `tavily-extract.json`: the extract response for the policy and terms pages.
- `MANIFEST.md`: when each file was fetched, from which URL, and how many Tavily credits the capture cost.

The capture is a committed program in `tools/capture/`, not a pasted transcript, so anyone can re-run it. Record the Tavily credits spent per title. This is the first real answer to the brief's open question about credits per title.

Everything captured is public. Do not capture anything that needs a login.

**Credits spent:** Roblox 6, Toca Life World 6, Khan Academy Kids 5, Minecraft 5 (22 in total, reported by Tavily; see [testdata/titles/README.md](../testdata/titles/README.md)).

**Done when:** Three title directories exist with every file listed above, `MANIFEST.md` records credits per title, and re-running the capture reproduces the same file set.

---

### T0.5: Three demo titles chosen ★
```yaml
requires:   []
fixture-ok: no
size:       XS · user
owns:       testdata/titles/README.md
status:     done
```
The video needs one title that passes, one that is **yes with these settings**, and one that is **no** because of a review quote. The brief's anchor is a title where the policy says chat is filtered and a review says the filter failed.

An agent drafts a shortlist of five or six candidates and says why each is a likely pass, settings, or no, based on public reporting. The user picks three, plus one safer alternative for the side-by-side.

The real verdicts come from the pipeline, not from the shortlist. If T3.6 shows a title lands on a different verdict, pick again from the shortlist rather than tune the checklist toward a wanted answer.

**Done when:** `testdata/titles/README.md` names the three titles and the alternative, with the expected verdict and the reason for each.

---

### T0.6: Name check
```yaml
requires:   []
fixture-ok: no
size:       XS · user
owns:       none
status:     done
```
The brief lists "Before Yes" for a trademark clash check before the video. An agent searches the USPTO, EUIPO and app stores and reports what it finds. The user decides.

**Done when:** The handoff log records the search results and the decision to keep or change the name.

---

### T0.7: Repository conventions
```yaml
requires:   T0.1
fixture-ok: yes
size:       S · mid
owns:       AGENTS.md, tools/check.sh, .github/workflows/check.yml
status:     done
```
Write `AGENTS.md`: the Go and Svelte versions, the dependency rule (Keel and Chaaya are declared dependencies, standard library first otherwise), file ownership, the `live` build tag for `b` tasks, and the review process.

`tools/check.sh` is the gate: `gofmt -l`, `go vet`, `go test -race ./...`, and the frontend's lint, type check and tests once P5 exists. CI runs the same script, so a green local run means a green CI run.

**Done when:** `./tools/check.sh` passes on a clean checkout, CI runs it on every push, and `AGENTS.md` covers every item above.

---

### T0.4a: Fixtures for Common Sense reviews and wider Reddit
```yaml
requires:   T0.4
fixture-ok: no
size:       S · mid
owns:       testdata/titles/*/tavily-search-csm.json, testdata/titles/*/tavily-extract-csm.json, testdata/titles/*/tavily-search-reddit-*.json, tools/capture/
status:     not-started
```
Split from T0.4 after the user added two sources to the gather plan (T3.1, items 3 and 4). T0.4 was already capturing under the old plan.

Extend `tools/capture` to make T3.1's new calls for the four titles in `testdata/titles/README.md`, and save each raw response:
- the Common Sense Media search on `commonsensemedia.org`, and the extract of the title's parent-reviews and kid-reviews pages;
- the two Reddit searches, one for parents' experiences and one for the title's own community.

Append each call's URL, status and credits to the title's `MANIFEST.md`. Record what the Common Sense review pages actually look like after extraction: whether reviews separate cleanly, how many each page holds, and whether reviewer type and position survive. T3.1 builds on that. The same key and secrecy rules as T0.4 apply.

**Done when:** All four titles have the new files. MANIFEST.md records the credits per call. The handoff log describes the shape of an extracted Common Sense review page. A grep for the key across `testdata/` and `tools/` finds nothing.

---

## Exit Criteria

- [x] First commit on `main`, with `scratch/` and `.env` ignored.
- [ ] A successful Nano completion on Token Factory, with the credit balance recorded.
- [ ] Every secret resolves through `before-yes config`, and no key is in git.
- [x] Three demo titles and one alternative chosen.
- [x] Fixtures for all three titles committed, with Tavily credits per title recorded.
- [x] `tools/check.sh` green locally.
- [ ] `tools/check.sh` green in CI (needs a remote).
