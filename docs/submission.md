# Devpost submission: Before Yes

Draft of every Devpost field for the Nebius × NVIDIA Global AI Hackathon. Paste each section into its field. Each number carries an HTML comment naming the repository file it comes from, so it can be checked. Remove the comments if the editor shows them.

---

## Project name

Before Yes

## Tagline

Before a child gets an app, game or channel, a parent gets a verdict for that child's age, with every concern linked to its source.

## Track

Best Apps and Agents

## Side award

Best Use of Tavily

## Description

### Inspiration

I am a father of two, aged 9 and 7. I work deep in technology, and I still find it hard to know what my kids play. My wife and relatives find it much harder. Protecting our children in this day and age is important. A healthy child needs boundaries. Getting hooked on the wrong app or game can give a child trauma and open them to abuse.

News shows why parents worry. [NBC Chicago](https://www.nbcchicago.com/consumer/fortnite-video-game-maker-hit-with-largest-ftc-fine-after-allegedly-putting-kids-at-risk-using-unfair-payment-practices/3025838/), 19 Dec 2022, reports the FTC's record Epic Games settlement over Fortnite. Status: settled. [FOX 4](https://www.fox4news.com/news/ken-paxton-texas-ag-lawsuit-roblox-childrens-safety), 6 Nov 2025, reports Texas's lawsuit against Roblox over child safety. Status: an allegation, not a finding. [The Associated Press via WTOP](https://wtop.com/national/2026/03/new-mexico-jury-finds-meta-violated-consumer-protection-law-at-trial-about-child-safety), 25 Mar 2026, reports a New Mexico jury found Meta violated child safety law and ordered $375 million. Meta says it will appeal.

A parent like me gets asked "can I have Roblox?" several times a week, each time about a different title. The answers are spread across a privacy policy nobody reads, the terms of service, hundreds of reviews, parent forums and the occasional regulator's fine. A chatbot gives a fluent summary of a few of those pages, differently each time, and does not say where any claim came from.

Before Yes reads the whole public record for one title and one child's age. It returns a verdict, and each concern links to the quote it rests on.

<!-- source: the author's own words; ~/work/demo-shoot/before-yes/evidence/sources.md (cases 02, 05, 06); docs/video-script.md, "Opening screenshots"; BRIEF.md, "Problem" and "Against just use ChatGPT" -->

### What it does

1. The parent enters a title and the child's age, from 3 to 17.
<!-- source: internal/pipeline/pipeline.go, MinAge and MaxAge -->
2. The gather step collects the App Store listing and its reviews, the publisher's privacy policy and terms, Reddit threads, a parent forum, news and regulator coverage, rating pages, and the title's own parental-controls help pages.
3. Nemotron-3 Nano reads every source in chunks. It returns findings, each with a verbatim quote and the source it came from. A finding whose quote does not appear in its source is dropped before the judge sees it.
4. Nemotron-3 Ultra grades six areas against a fixed checklist: contact with strangers, purchases and spending, privacy and data, age restrictions, content, and developer history. It also writes a plain-language reason for each standing grade.
5. Code checks the grades and computes the verdict. The verdict is one of **yes**, **yes with settings**, **no** or **unclear**. Missing evidence never reads as yes.
<!-- source: README.md, "What it does" and "How Nemotron is used"; internal/checklist/checklist.go -->

The card shows the verdict, the six areas, the settings to turn on, each concern with its quotes, and what was read and when. Three demo cards open from the home page without spending anything: Roblox (9) is **no**, Minecraft (9) is **yes with settings**, and Khan Academy Kids (5) is **yes**.
<!-- source: README.md, demo table; testdata/demo-cards/ -->

### How Nemotron and Token Factory are used

Both halves of the platform rule run on Nebius Token Factory. The check calls no other model host.

- **Nemotron-3 Nano (30B-A3B) reads.** Each source is split into chunks, and each chunk becomes quoted findings. Thinking is off for this role, because Token Factory bills thinking as output tokens. Nano does the high-volume work, so every source is read in full.
<!-- source: README.md, "How Nemotron is used"; before-yes.toml.example, [reader.extra] -->
- **Nemotron-3 Ultra (550B-A55B) judges.** The first pass grades all six areas with thinking off and repetition penalty 1.1. Where an area's findings pull both ways, a risk and a control for example, a second pass runs with thinking on, for those areas only, at temperature 0.6, top_p 0.95 and repetition penalty 1.05.
<!-- source: before-yes.toml.example, judge_fast and judge_think -->
- **Why those sampling settings.** Ultra looped at temperature 0. Thinking-on conflict passes repeated a line until their token guard, and thinking-off first passes repeated finding ids until their cap. The sampling settings above stopped both. The client sends temperature 0 by default, and the judge overrides it per call.
<!-- source: dev-diary/README.md, handoff log, "Judge on Nemotron-3-Ultra"; internal/model/model.go, temperature 0 default -->
- **Why Ultra.** We tried Nemotron-3.5 Lightning, Nemotron-3.5 Super-VL and Ultra on the same evidence. Ultra was the first to reach the expected verdict on Khan Academy Kids without a rule override.
<!-- source: README.md, "How Nemotron is used" -->
- **Quote verification.** Before the judge sees anything, every finding's quote must appear in the source it names, after normalisation. That is a string match in code. A finding that fails it is dropped.
<!-- source: README.md, "How Nemotron is used" -->
- **Checks the model cannot skip.** Citation pruning removes grades that cite nothing valid. Grounding requires a named setting to appear in a cited quote. Entailment asks Ultra, with thinking off, whether the cited quotes meet the definition and whether the reason says only what they support. A deterministic rule gate in Go can veto, cap, confirm or raise a grade. Every action is listed on the card under "Rule checks". Then `checklist.Decide` computes the verdict from the six grades.
<!-- source: README.md, "How Nemotron is used" (checks 1 to 5); internal/pipeline/rulegate.go -->
- **Measured cost.** Model spend per fresh check averages $0.106976 over nine checks: Roblox $0.116922, Minecraft $0.110137, Khan Academy Kids $0.093868. Nano is priced at $0.06 in and $0.24 out per million tokens. Ultra's price is an assumption of $1 in and $3 out, because its model page did not show one.
<!-- source: docs/budget.md, "Current plan-6 figures"; before-yes.toml.example, [reader] and [judge] -->

Token Factory's OpenAI-style API made the judge switch a settings change: the host, model id, prices and rate limits are in `before-yes.toml`. Thinking and sampling are per-call switches, so the reader and judge differ in one client.
<!-- source: README.md, "Where Token Factory helped" -->

### How we built it

- **Server:** Go, built on [Keel](https://github.com/nrynss/keel) v0.7.3. Keel supplies the job runner (a check outlives its request), progress streaming with replay, the cost meter, the cache, the gate, the feature flag, config, ids, error format and SQLite storage.
<!-- source: go.mod; README.md, "Architecture" -->
- **Web app:** Svelte 5 on Vite, using [Chaaya](https://github.com/nrynss/chaaya) 0.4.2 for the job stream and the theme and accessibility tokens, and Bits UI for dialogs and tooltips.
<!-- source: web/package.json; README.md, "Dependencies" -->
- **Sources:** one Tavily client for search and extract, and an Apple review and listing fetcher. Each check runs eight Tavily searches and a listing read.
<!-- source: internal/pipeline/gather.go, Plan (eight searches) -->
- **Spend limits:** a $20 model ceiling from the $25 Token Factory credit, 900 Tavily credits a month, and 5 fresh checks per UTC day. Cached cards and evidence cost nothing. Visitors may use their own keys, which are held for that request only.
<!-- source: docs/budget.md, "Summary"; before-yes.toml.example, [budget]; dev-diary/README.md, handoff log T4.5 (visitor keys) -->

- **Deployment:** one container image with a distroless runtime, behind Traefik on a Hetzner host, with Cloudflare in front and a nightly backup of the databases.
<!-- source: Dockerfile; deploy/README.md; dev-diary/README.md, handoff log T6.1 -->
- **Tests:** the Go gate runs gofmt, go vet and `go test -race`, and the web app runs lint, type check and browser tests. An offline replay (`go run ./tools/eval -models replay`) reruns the recorded model replies and needs no keys.
<!-- source: tools/check.sh; README.md, "Run it locally" and "Measurements" -->

### Challenges

- **The Apple review feed went dark.** It returns HTTP 200 with no entries, the same answer as an app nobody reviewed. We read reviews from the App Store web page through Tavily instead. That gives three or four reviews per title, with no rating or date.
<!-- source: dev-diary/feedback.md, "Apple App Store" -->
- **Ultra looped at temperature 0.** Sampling settings fixed it. See the model section above.
- **Verdicts move between gathers.** On the three demo titles, Roblox is **no** on all three recorded gathers. Minecraft is yes with settings on one and unclear on two. Khan Academy Kids is yes on two and unclear on one. We recorded the movement rather than tuning it away.
<!-- source: docs/budget.md, "Verdicts after the re-record"; README.md, "Known limits" -->
- **Many raw findings do not survive the quote check.** On the offline replay, 36.7% of Roblox's raw findings, 35.7% of Minecraft's and 27.7% of Khan Academy Kids' were dropped, mostly because the quote is not in the source text.
<!-- source: README.md, Measurements table (replay of testdata/eval/replies/, gather 0); reproduced with go run ./tools/eval -models replay -->
- **A matching quote does not prove a sound reason.** The quote check confirms that the words exist. Entailment and the rule gate exist because a grade's reason can say more than its quotes do.
<!-- source: README.md, "How Nemotron is used" (checks 2 to 4); dev-diary/feedback.md -->

### Accomplishments

- Every concern on a card links to a verbatim quote and its source. The quote is checked by string match in code, not by the model's word.
- On the offline replay of the recorded evidence, all three verdicts match the expected results, and 12 of 18 area grades do. The replay also shows the limit: only 3 of 16 decisive quotes were recovered. We report both.
<!-- source: README.md, Measurements table (replay agreement, gather 0) -->
- A check costs about 11 cents of model calls, measured on recordings at an assumed Ultra price.
<!-- source: docs/budget.md, "Current plan-6 figures" -->
- The hosted app ran a live check on a title outside the demo set, Duolingo ABC for a 6-year-old, on Token Factory and Tavily. It cost $0.078 of model calls and 10 Tavily credits, and returned unclear.
<!-- source: dev-diary/README.md, handoff log, T6.1 "Live check" -->
- Missing evidence never reads as yes. The card says "unclear" and names what was missing.
<!-- source: README.md, "What it does" and "Why it isn't just ask ChatGPT" -->

### What we learned

- The verdict depends on how much of the evidence the model actually reads, so the pipeline reads every chunk and does not sample.
- A model's grade is not evidence. Code checks the quotes, the citations, the grounding and the verdict rule. The entailment step is the one check that is itself a model call, and it can be wrong too: on one Minecraft gather it accepted reasons its quotes did not support, and it once refused an earned flag until its instruction was clarified.
<!-- source: dev-diary/feedback.md, "The entailment check can refuse an earned flag" and "The entailment check is permissive in the accept direction" -->

- Thinking is a cost decision made per call. On one Nano prompt, thinking-on used 183 output tokens and thinking-off used 4, about 45 times the output tokens.
<!-- source: dev-diary/feedback.md, "Token Factory and Nemotron", "Thinking is on by default" -->
- "Unclear" is a useful answer. A parent is better served by a verdict that says what is missing than by a confident guess.

### What's next

- Show a checked safer alternative on the demo cards. The alternatives step is built and tested, and it runs one Tavily search and a full check on up to three candidates. No demo card shows one yet, because that needs live checks.
<!-- source: internal/pipeline/alternatives.go, MaxAlternatives = 3; README.md, "Known limits" -->
- Make verdicts stable across gathers, and check whether the quote capture for reviews can improve.
- Read Google Play. It has no official API, so it is out of scope for now.
<!-- source: README.md, "Known limits" -->

## Built with

- Nemotron-3 Nano 30B-A3B and Nemotron-3 Ultra 550B-A55B, on Nebius Token Factory
- Tavily search and extract
- Apple's public iTunes search API and App Store pages
- Go, Keel v0.7.3, SQLite
- Svelte 5, Vite, Chaaya 0.4.2, Bits UI
- Docker with a distroless runtime, Traefik, Hetzner, Cloudflare
- Apache License 2.0
<!-- source: README.md, "Dependencies" and "Licence"; LICENSE; dev-diary/README.md, handoff log T6.1 -->

## Why Tavily (Best Use of Tavily)

Tavily does two jobs in every check, and each one changes the verdict.

**Gathering the evidence.** Each check runs eight Tavily searches for the title:

- the privacy policy and the terms of service, each narrowed to the publisher's own site when the listing names one, with the pages read in full by extract;
- two Reddit searches, restricted to reddit.com, with raw content;
- a parent-forum search, restricted to Common Sense Media, Mumsnet and Netmums;
- a news search for fines, lawsuits, regulator action and breaches;
- a rating search for ESRB and PEGI pages;
- a search for the title's own parental-controls pages. It is not restricted to one domain, because those pages often sit on a platform's support site.

<!-- source: internal/pipeline/gather.go, Plan; internal/pipeline/gather.go, maxDocChars -->

Apple's review feed returns nothing now, so a basic Tavily extract of the App Store listing page supplies the reviews. The listing facts (price, age rating, privacy label, Kids category and advisories) come from Apple's public listing data, not from Tavily.
<!-- source: README.md, "What it does" step 2; dev-diary/feedback.md, "Apple App Store" -->

Each check spends 8 to 10 Tavily credits, measured on the replay: 9, 8 and 9 credits for the three demo titles.
<!-- source: docs/budget.md, "Tavily pool"; replay output, gather 0 -->

**Finding safer alternatives.** For a no or yes-with-settings verdict, one more search asks for a safer app of the same kind for the child's age. Up to three candidates get a full check of their own. A candidate reaches the card only if its verdict beats the original's.
<!-- source: internal/pipeline/alternatives.go, alternativesSearch and MaxAlternatives; README.md, "What it does" -->

Feedback on Tavily is in `dev-diary/feedback.md`. The main points: extract credits did not follow the published price list in either direction; a 404 page came back as a successful extract; Reddit hits returned no raw content, so only snippets were usable; and a dead link and a blocked site both fail with the same bare message. Metering with `include_usage` was exact, and `include_raw_content` on a basic search added no credit cost.
<!-- source: dev-diary/feedback.md, "Tavily" entries -->

## Feedback on Token Factory and the NVIDIA models

What worked:

- **The OpenAI-style API.** Changing the judge was a settings change, not a code change.
- **Per-call switches.** Thinking (`chat_template_kwargs.enable_thinking`) and the sampling values are honoured per call, so the reader and judge differ in one client.
- **Rate-limit headers.** Of the hosts we used, Token Factory was the only one that sent `X-Ratelimit-*` headers, so the pacing could be read from the host rather than guessed.

<!-- source: README.md, "Where Token Factory helped"; dev-diary/feedback.md, "Only Token Factory sends rate-limit headers" -->

What to fix:

1. **Thinking is on by default and billed as output.** The same Nano prompt used 183 output tokens with no switch and 4 with `enable_thinking` set to false. The reader is the high-volume role, so the default is the expensive path. Ask: thinking off for non-reasoning requests, or a documented per-model default.
<!-- source: dev-diary/feedback.md, "Thinking is on by default and billed as output" -->
2. **The thinking switches differ by host.** On Token Factory, `reasoning.enabled` is ignored, and `reasoning_effort` set to "none" returns empty text. A client written for another host's switch leaves thinking on, with no error. Ask: honour `reasoning.enabled`, or reject unknown fields.
<!-- source: dev-diary/feedback.md, "reasoning.enabled is ignored" -->
3. **Model ids are case-sensitive, and the Nemotron ids are not consistent.** The lowercase Nano id returns 404, and the four Nemotron ids use different casing and punctuation. Ask: accept ids case-insensitively, or publish one exact spelling per model.
<!-- source: dev-diary/feedback.md, "Model ids are case-sensitive"; before-yes.toml.example, [reader] -->
4. **Ultra loops at temperature 0.** Without the sampling settings in our config, its first and conflict passes repeated lines or finding ids until the cap. The reasoning values follow NVIDIA's recommendation. Ask: publish the recommended sampling next to each model id.
<!-- source: dev-diary/README.md, handoff log, "Judge on Nemotron-3-Ultra"; before-yes.toml.example, judge_think -->
5. **Ultra's price was not on its model page.** We priced Ultra at $1 and $3 per million tokens, the highest listing we saw. Every cost figure above is an estimate until the host shows a price. Ask: show prices per model.
<!-- source: before-yes.toml.example, [judge] comment; README.md, Measurements table -->
6. **A timed-out call may be billed with no usage returned.** The client cannot record the cost of a call it cut off, so it prices that call at its estimate. Ask: report partial usage on a closed connection.
<!-- source: dev-diary/feedback.md, "A call that the host cuts or the client times out may still be billed" -->
7. **A thinking pass can use its whole cap and return no answer.** On one conflict pass, thinking used all 16,000 output tokens and the visible answer was empty. Another was cut off mid-JSON. Both were discarded, at $0.0189 and $0.0206. Ask: a budget that leaves room for the answer after thinking.
<!-- source: dev-diary/feedback.md, "A thinking conflict pass can run to its whole 16,000-token cap" and "Two of three conflict passes bought nothing"; before-yes.toml.example, [judge] max_tokens -->
8. **Nano repeats itself at volume.** One Khan Academy Kids reply held 84 findings, and 37 were exact repeats of earlier lines. The client removes them, but the model should not emit them. Nano also accepted entailment prompts it should have refused on one Minecraft gather, so the entailment check runs on Ultra.
<!-- source: dev-diary/feedback.md, "Nano repeats the same finding several times", "Nano rubber-stamps a long entailment prompt" -->

## Links

- Repository: https://github.com/nrynss/before-yes
- Live demo: https://beforeyes.nryn.dev
- Video: https://www.youtube.com/watch?v=n2nyM6GFvAY

---

## Before you submit (not for Devpost)

- [ ] The repository is public at https://github.com/nrynss/before-yes. This draft cannot check it.
- [ ] GitHub's About panel shows the Apache-2.0 licence. The `LICENSE` file is in the repository.
<!-- source: LICENSE; BRIEF.md, "Event facts" (licence required) -->
- [x] The video is public on YouTube, runs 2 minutes 48 seconds, and its link is above.
- [ ] The Token Factory account is active and the credit has landed, since the Devpost FAQ says some countries cannot onboard to billing.
<!-- source: BRIEF.md, "From the Devpost FAQ" -->
- [ ] https://beforeyes.nryn.dev loads, and the three demo cards open from "Previous checks".
- [ ] The track and side award above match the names on the Devpost form.
