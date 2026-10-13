# Before Yes — project brief

> A child asks for an app, a game or a YouTube channel. Before the parent says yes, Before Yes reads everything written about it and returns a verdict for that child's age, with every red flag linked to the sentence that supports it.

*Entry for the [Nebius × NVIDIA Global AI Hackathon](https://nebiusglobalaihackathon.devpost.com/). Draft dated 12 Oct 2026. Concept decided 7 Oct; event rules, prizes and the reasoning behind the choice are in `~/work/hackathons/nebius-nvidia.md`.*

## Event facts

| | |
|---|---|
| Submission close | **30 Oct 2026, 10:00 PT = 22:30 IST** |
| Judging | 1–15 Dec 2026, on our key; winners ~11 Jan 2027 |
| Track | **Best Apps and Agents** (powered by Nemotron) |
| Side award aimed at | **Best Use of Tavily, $3,000**, standalone, on top of the placings |
| Hard requirements | A runtime call to **Nebius Token Factory** and at least one **NVIDIA open model** (Nemotron). Both halves |
| Project | New, under its own name; no "significantly updated" explanation needed |
| Licence | **Apache-2.0** (decided 12 Oct), matching Keel and Chaaya |
| Required | Public repo with an OSI licence (MIT or Apache-2.0) visible in About; README that **highlights the Nemotron use and where Token Factory helped**; hosted demo URL; public YouTube video under 3 minutes; chosen track; feedback on Token Factory and the NVIDIA models |
| Video | No third-party logos or branded footage. App names appear only as data on the card |
| Budget | **$25 of Token Factory credit, total.** Tavily on its free tier (1,000 credits a month) |

The user is away **17–26 Oct inclusive**.

## Problem

A parent gets asked "can I have Roblox?" several times a week, each time about a different title. What they actually need to know is narrow and specific: can strangers message my child, will it push purchases, what does it collect, does the age gate work, what do other parents report seeing. The answers are spread across a privacy policy nobody reads, terms of service, hundreds of reviews, parent forums and the occasional regulator's fine.

Common Sense Media covers a limited catalogue and lags behind new titles. A chatbot gives a fluent summary of a few sources, differently each time, without saying where any claim came from.

**Audience:** parents of children roughly 5–14, deciding on one title at a time.

## What it does

1. **Request.** The parent enters the title and the child's age: "Roblox, 9."
2. **Gather (Tavily).** Privacy policy, terms of service, developer history, parent-forum and Reddit threads, news and regulatory actions. App Store reviews from Apple's public customer-review feed (up to about 500 recent reviews per country).
3. **Read (Nemotron 3 Nano).** Every document, in chunks, becomes structured findings. Each finding carries the exact quote it came from and the source URL. Chunking means no call needs a large context window.
4. **Judge (Nemotron 3 Super).** Scores the findings against a fixed parent checklist and gives an age-specific verdict. Where the evidence conflicts, a second pass runs with thinking on.
5. **Card.** One page with:
   - the verdict: **yes**, **yes with these settings**, or **no**;
   - red flags, each linked to its source quote;
   - the parental settings to turn on if the answer is yes;
   - safer alternatives, found with Tavily and checked the same way;
   - when it was checked.

**The moment the demo is built around:** the policy and most sources say chat is filtered; review 312 says it didn't work for a 7-year-old, and that review decides the card.

## The checklist

The checklist is the product. It is fixed, versioned and the same for every title, which is what makes two cards comparable.

| Area | What it checks |
|---|---|
| Contact | Can strangers message or voice chat? Is it on by default? Can it be turned off? |
| Money | In-app purchases, loot boxes, pay-to-win pressure, subscription traps |
| Data | What is collected, ad tracking, third-party sharing, how to delete data |
| Age gating | Does it work in practice? Reviews saying "my 7-year-old got in" outweigh what the policy claims |
| Content | What reviewers actually report seeing |
| Developer record | Fines, breaches, lawsuits, regulatory actions |

Each area resolves to **ok**, **ok with a setting** (the setting named), or **flag** (the quote that raised it). The verdict follows from the areas and the child's age, by rules written in the code, not left to the model's mood: Super grades each area against the evidence; the verdict is computed from the grades.

## Kinds of title

| Kind | Where the evidence comes from |
|---|---|
| iOS app or game | Apple review feed, plus Tavily for policy, terms, forums, news |
| Android-only app or game | Tavily only. **Google Play reviews have no official API; don't scrape** |
| Console or PC game | Tavily: ESRB/PEGI rating pages, policy, forums, news |
| YouTube channel | Tavily: news, forum threads, channel coverage. Whether the YouTube Data API's made-for-kids status adds anything is an open question |

## Against "just use ChatGPT"

An agent with search gives a fluent summary of a few sources. Before Yes reads the **whole** policy and **every** review, runs the **same** checklist every time, and links **every** claim to its source. A parent can check any line, and two titles can sit side by side. Reading everything is the job Nano is cheap at; that volume is what makes the depth affordable.

## Architecture

```
browser (Chaaya)  ──POST /checks──▶  Go server (Keel)
       ▲                               │ job: gather → read → judge
       │  stream: progress events      ├─ Tavily: search + extract
       └───────────────────────────────┤─ Apple review feed
                                       ├─ Token Factory: Nano (read), Super (judge)
                                       └─ SQLite: cache, ledger, cards
```

A check is a 20–60 second **job** started by a short request. The browser follows it over server-sent events ("reading 312 reviews…") and lands on a card at an unguessable URL.

### Stack

**Backend: [Keel](https://github.com/nrynss/keel) v0.7.0 (Go), Apache-2.0, declared dependency.**

| Before Yes needs | Keel package |
|---|---|
| A check that outlives its request, durable across restarts | `job` |
| Live progress, with replay after a reconnect | `stream` |
| Protecting the $25 during judging: refuse before a call, ledger, per-visitor share, meter on every Token Factory call | `cost` |
| Tavily's 1,000 credits a month as a budget that resets monthly | `cost` (provider-credit budgets, keel#33) |
| Free-tier limits on OpenRouter and Tavily: per-minute pacer, retry-after, full-jitter retry | `throttle` (keel#68) |
| Paid results reused by URL + content hash and by title, with single-flight for double-clicks | `cache` (keel#23) |
| Rate limits and an optional passcode on the public URL | `gate` |
| Token Factory and Tavily keys out of the config file | `config` |
| Turning live checks off without a redeploy | `flag` |
| Unguessable card URLs | `id` |
| One error format across routes | `wire` |
| Storage | `sqlite` |

**Frontend: [Chaaya](https://github.com/nrynss/chaaya) 0.4.1 (Svelte 5), Apache-2.0, declared dependency.** `JobStream` drives the gather/read/judge display; the Keel adapter gives typed errors so "budget refused" and "rate limited" each get a clear message; theme tokens and the contrast and accessibility gates support the Design score. Bits UI for dialogs and tooltips.

**Written in Before Yes itself:**
- One OpenAI-style model client for Token Factory, OpenRouter and NVIDIA's API: base URL and model name in config.
- A Tavily client (search and extract).
- An Apple review-feed fetcher.
- The pipeline: chunking, Nano's quoted-finding format, the checklist, the grading rules, the verdict, the cache layout.

Before Yes is Keel's second user after Reprise. A gap it finds becomes a Keel issue, not a workaround here.

### Repo layout (proposed)

```
before-yes/
├── LICENSE                   Apache-2.0
├── README.md                 Nemotron and Token Factory use up front (rules)
├── BRIEF.md
├── go.mod
├── cmd/before-yes/           server entry point
├── internal/
│   ├── model/                OpenAI-style client
│   ├── tavily/
│   ├── appstore/             Apple review feed
│   ├── pipeline/             gather, chunk, read, judge
│   └── checklist/            areas, grading rules, verdict (versioned)
├── web/                      Chaaya app
└── deploy/                   Hetzner tenant
```

### Formats (first cut)

A **finding**, from Nano, one per claim:

```json
{
  "area": "contact",
  "claim": "Chat filter let through a stranger's request for a phone number",
  "stance": "risk",
  "quote": "the filter didn't catch it when someone asked my 7 year old for her number",
  "source": {"url": "…", "kind": "review", "index": 312, "fetched_at": "…"}
}
```

`stance` is `risk`, `mitigation` (a setting or control) or `neutral`. A finding without a verbatim quote that appears in the source text is dropped before judging: the quote is checked by string match, not trusted.

A **card**, from Super's grades plus the rules: title, kind, age, checklist version, checked-at, verdict, per-area grade with supporting finding ids, settings to turn on, alternatives.

## Models

| Role | Development | Final runs and hosted demo |
|---|---|---|
| Read (Nano) | OpenRouter `nvidia/nemotron-3.5-lightning:free` (stand-in, not Nano 30B) or build.nvidia.com | **Token Factory: Nemotron 3 Nano 30B-A3B** |
| Judge (Super) | OpenRouter `nvidia/nemotron-3-super-120b-a12b:free` or build.nvidia.com | **Token Factory: Nemotron 3 Super 120B-A12B** |

Switching is two settings. Re-check output quality after moving to the real Nano. Free tiers rate-limit and may log prompts: test only on public material, which is all Before Yes reads anyway.

## Budget

| Model (Token Factory, live) | Input / output per 1M tokens |
|---|---|
| Nemotron 3 Nano 30B-A3B | $0.06 / $0.24 |
| Nemotron 3 Super 120B-A12B | $0.30 / $0.90 |

| Step | Tokens | Cost |
|---|---|---|
| Nano reads the sources | ~200k | ~$0.012 |
| Super judges | ~30k in, ~5k out | ~$0.014 |
| **Per title** | | **~3 cents** |

$25 covers about 800 checks. **Tavily is the tighter limit:** at 10–15 search and extract calls per title, the free tier gives roughly 70–100 fresh checks a month.

## Guardrails

- **No Nebius Serverless Jobs or Dedicated Endpoints.** Paid GPU time. A runtime Token Factory call is all "runs on Nebius" needs.
- **Cache everything:** Tavily results by URL and content hash, verdicts by title and checklist version. Roblox gets checked constantly; repeats cost nothing.
- **Thinking off for Nano's reading.** Thinking is billed as output.
- **During judging (1–15 Dec):** demo titles pre-computed; a daily cap on fresh checks; visitors may paste their own Token Factory and Tavily keys for unlimited use (keys held for the request only, never stored).
- **No NVIDIA reranker.** None confirmed on Token Factory; Super weighs the evidence.
- **Advice, not certainty.** Every flag shows its evidence and the card shows when it was checked.

## Hosting

The existing Hetzner box (Traefik, `~/work/hetzner-hosting/tenant.sh`) runs the single Go binary with the built Chaaya app. Compute stays off the Nebius credit.

## Demo (under 3 minutes)

A child asks for three things:
1. One passes.
2. One is **yes, with these settings**, and the card names them.
3. One is **no**, and the card shows the review quote that decided it.

Then a side-by-side with the safer alternative. Close on the cost of a check and the fact that it works on any title, today.

## Why it scores

- **Best Use of Tavily:** Tavily is load-bearing twice, gathering the evidence and finding alternatives.
- **Technological implementation:** Nano does the high-volume reading, Super makes the judgment calls; the split the Best Apps track text asks for. Quotes are verified against source text.
- **Potential impact:** every parent is the audience; works on any title on demand.
- **Design:** a single card a parent can act on.
- **Quality of the idea:** a fixed checklist plus reading everything, not a chatbot answer.

## Dependency status (checked 12 Oct)

Everything the 7 Oct brief listed as "to check" has landed:
- **Keel v0.7.0** (tagged 8 Oct) includes the cache (#23), provider-credit budgets (#33), the throttle pacer with retry-after (#68, PR #77), and the paid-call fixes #57–#60.
- **Chaaya 0.4.1** is tagged and on npm, with `priced`.

## From the Devpost FAQ (published 6 Oct, read 12 Oct)

- **The runtime call must go to Token Factory.** Calling Nemotron through OpenRouter or Hugging Face does not satisfy the platform rule, and there is no approved workaround. Developing on OpenRouter is fine; the submitted app and the demo must call Token Factory.
- **Only the model call has to be on Nebius.** The frontend and the rest of the backend need not run on Nebius AI Cloud, so the Hetzner hosting plan stands.
- **The promo credits are Token Factory credits only;** there is no AI Cloud credit grant.
- **Some countries can't onboard to Token Factory billing yet.** It's a gap on Nebius's side, not an eligibility rule, and Devpost has no workaround: the Nebius Discord handles it case by case, as it does missing promo credits. **Check first that the Token Factory account is active and the $25 has landed**, before anything depends on it.
- No maximum team size. City Winner awards need in-person attendance at the city event.

## Open questions

- Tavily credits per title in practice, measured on the three demo titles, before tuning the source list.
- Real Nano quality on quote extraction versus the free stand-in.
- Whether YouTube channels get anything beyond Tavily (Data API made-for-kids status, comment sampling within quota).
- Which App Store countries to read: one (US) or the parent's own.
- Name check: "Before Yes" for trademark clashes before the video.
