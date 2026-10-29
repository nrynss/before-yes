# Before Yes: demo video script

The demo video is music with captions and title cards, and no voiceover. It runs 2 minutes 48 seconds as rendered (168.4 seconds), under the 3-minute limit. It shows the hosted app at https://beforeyes.nryn.dev, recorded at 1920×1080, plus title cards drawn for the film. It names the sponsors, NVIDIA Nemotron and Nebius Token Factory, as text. The opening shows three news screenshots whose logos and mastheads are blurred, with their headlines left readable. No other third-party logos or branded footage appear. App names appear only as data on the cards.

The film is built from the working folder `~/work/demo-shoot/before-yes/`, which is outside the repository. Its `script.md` holds the full script with the source of every number. This document gives the beats, the live checks, the shot list and the rebuild steps.

## Story

The title and the sponsors come first. A short personal opening follows: three text cards, three news screenshots of harm to children from apps, games and platforms, and two more cards that lead to the tool. A short technical section then explains how a check works: the gather, the reading, the quote check, the judge, the checks the model cannot skip, the cost and the stack. A live check on the hosted site follows, from the form to the verdict and its cost. Three demo cards and the side-by-side compare come after it. The film closes on the sponsors, the author and the links.

## Beats

Captions are at most 25 words, with no semicolons and no em or en dashes. Lengths are the rendered length of each beat.

| # | Beat | Length | Picture | Caption or card text |
|---|---|---|---|---|
| 00 | title | 6.0s | Card | "Before Yes", "Research first. Yes next.", "A child asks for an app or a game. The parent checks it before saying yes.", "Built on NVIDIA Nemotron, served on Nebius Token Factory", "by Narayan SS" |
| 00a | intro, father | 4.5s | Quiet text card, no box | "I am a father of two. They are 9 and 7." |
| 00b | intro, work | 4.5s | Quiet text card | "I work deep in technology, and I still find it hard to know what my kids are playing." |
| 00c | intro, family | 4.5s | Quiet text card | "For my wife and our relatives, it is much harder." |
| 00d | news, FTC 2022 | 3.5s | Blurred screenshot of evidence/02. Zooms from the full page to the headline and standfirst. Tag in the bottom-right corner. | Tag: "FTC settlement, Dec 2022". Headline as on the page. Sources below. |
| 00e | news, Texas 2025 | 3.5s | Blurred screenshot of evidence/05. Zooms to the headline and The Brief. Tag. | Tag: "Lawsuit (allegation), Nov 2025". |
| 00f | news, New Mexico 2026 | 3.5s | Blurred screenshot of evidence/06. Zooms to the headline; this page has no standfirst. Tag. | Tag: "Jury verdict, Mar 2026, Meta says it will appeal". |
| 00g | intro, boundaries | 4.5s | Quiet text card | "A healthy child needs boundaries. The wrong app or game can bring trauma, and open the door to abuse." |
| 00h | intro, built | 4.5s | Quiet text card | "So I built Before Yes." |
| 01 | asks | 5.0s | Card: three requests | "Before Yes gives each request one answer for that child's age." |
| 02 | gather | 8.0s | Card: eight searches by kind | "Tavily runs eight searches, by kind." App Store listing facts. |
| 03 | read | 8.0s | Card: findings with quotes | "Nemotron-3 Nano reads every source, in chunks." Thinking off: 183 output tokens with thinking on, 4 with it off. |
| 04 | quote | 7.0s | Card: drop-rate table | "Code checks every quote before the judge sees it." Dropped: 36.7%, 35.7%, 27.7%. |
| 05 | judge | 8.0s | Card | "Nemotron-3 Ultra grades six areas." Sampling settings for each pass. "Ultra looped at temperature 0, so its calls set their own sampling." |
| 06 | checks | 8.0s | Card | "Five checks the model cannot skip." Citation pruning, grounding, entailment, rule gate, `checklist.Decide`. |
| 07 | cost | 7.0s | Card | "$0.107" model spend per fresh check, the mean of nine. Limits: $20 of model credit, 5 checks a UTC day, 900 Tavily credits a month. |
| 08 | stack | 6.0s | Card | Go on Keel v0.7.3, Svelte 5 on Chaaya 0.4.2, SQLite, Nebius Token Factory. |
| 09 | live check, Subway Surfers (8) | 31.0s | Live: form typed and submitted; progress stages sped up; verdict card | Form: "The parent types "Subway Surfers" and the child's age, then presses the button." Stage one, gather: "Tavily runs eight searches and reads the policy pages in full." Stage two, read: "Nemotron-3 Nano reads every source in chunks and keeps each quote." Stage three, judge: "Nemotron-3 Ultra grades six areas. Then code checks every grade." "The verdict is not a plain yes, so safer alternatives are checked too." Verdict: "The finished card gives one verdict for this age, with the reason behind each grade." |
| 09c | live cost | 6.5s | Stored card of the same check, "AI processing cost" boxed | "About this check lists the model cost of this check: $0.0667664 USD." |
| 10 | khan | 6.5s | Live card: Khan Academy Kids (5) | "Khan Academy Kids for age 5: five areas are OK, so the answer is yes." |
| 11 | minecraft | 6.5s | Live card: Minecraft (9) | "Minecraft for age 9: yes, with settings. The card names three settings to turn on." |
| 12 | roblox | 7.5s | Live card: Roblox (9), deciding quote boxed | "Roblox for age 9: no. The App Store listing says games may contain optional in-game purchases, including lootboxes." |
| 13 | compare | 7.0s | Live compare page, purchases row boxed | "Side by side, on purchases Roblox is a concern and Minecraft needs a setting." |
| 14 | outro | 7.0s | Card: sponsors, author, links | "Built on NVIDIA Nemotron, served on Nebius Token Factory", "by Narayan SS", beforeyes.nryn.dev, github.com/nrynss/before-yes, Apache-2.0. |

Total: 168.4 seconds. Beat 09 runs 96.3 s to 127.3 s in the film, after the opening and the eight cards.

Within beat 09, each stage's first 3 seconds play at normal speed with its caption. The rest of the wait plays 20 times faster. The alternatives wait of about ten minutes becomes about 30 seconds of film. The cost caption on the live take is not used, because it read "USD USD". Beat 09c shows the same cost from the stored card instead.

## Opening screenshots

Beats 00d to 00f show three news pages. Each picture is a blurred copy from `evidence/` in the working folder. The raw captures are never used. On each page the logos, mastheads, ads, pop-ups and third-party photos are blurred, so no logo is visible. The headlines, standfirsts, bylines and dates stay readable. Nothing was logged into, no paywall was passed, and no cookie banner was accepted.

| Beat | Outlet and date | Source URL |
|---|---|---|
| 00d | NBC 5 Chicago (NBC Chicago), Dec 19, 2022. Status: settled. | https://www.nbcchicago.com/consumer/fortnite-video-game-maker-hit-with-largest-ftc-fine-after-allegedly-putting-kids-at-risk-using-unfair-payment-practices/3025838/?amp=1 |
| 00e | FOX 4 Dallas-Fort Worth (byline FOX 7 Austin), Nov 6, 2025. Status: allegation. | https://www.fox4news.com/news/ken-paxton-texas-ag-lawsuit-roblox-childrens-safety |
| 00f | The Associated Press, via WTOP News, Mar 25, 2026 (verdict Tuesday, Mar 24). Status: jury verdict; Meta says it will appeal. | https://wtop.com/national/2026/03/new-mexico-jury-finds-meta-violated-consumer-protection-law-at-trial-about-child-safety |

The tags and their dates and statuses come from `evidence/sources.md`. Row 06 says Meta will appeal, so its tag does not say the case is under appeal.

## The live checks

Two fresh checks were run on the hosted site, both typed and submitted from the home page. The limit was two. The first check's recording was lost, because the overlay's green wipe covered every frame. The recorder now removes that layer on injection. Its dry run shows the home page and a stored card with the caption layer and no green panel.

| Check | Title | Age | Verdict | Model cost on the card | Tavily credits | Status |
|---|---|---|---|---|---|---|
| 1 | YouTube Kids | 7 | No | $0.09419692 USD | 9 | Finished. Recording lost to the wipe. Its card is not in the film. |
| 2 | Subway Surfers | 8 | No | $0.0667664 USD | 9 | Finished. In the film as beats 09 and 09c. |

Check 2's card: 45 sources read, 500 reviews from the App Store review feed, 43 source sections with none unread, 58 quotes that could not be verified and 7 findings without a source quote. Its "Why this is a concern" quote is a review: "I only have 31 characters and I can't buy bundles for the paying. There 10.99 per character." The card also lists "Ask App Not to Track" under "Settings to change".

The card's own stats record 500 reviews from the App Store review feed. `README.md` says that feed returns nothing now. Check whether that is still true before the next film.

## Numbers on screen

| Number | Where it comes from |
|---|---|
| Eight searches, and their kinds (02) | `README.md`, "What it does" step 2; `internal/pipeline/gather.go`, `Plan` |
| 183 and 4 output tokens (03) | `README.md`, "Where Token Factory helped"; `before-yes.toml.example`, `[reader.extra]` |
| 36.7%, 35.7%, 27.7% (04) | `README.md`, Measurements, "Quotes dropped, gather 0" |
| 60,000 characters per page (03, not on screen) | `README.md`, Measurements, "Text read per page" |
| Repetition penalty 1.1; thinking-on temperature 0.6, top_p 0.95, repetition penalty 1.05 (05) | `before-yes.toml.example`, `judge_fast` and `judge_think` |
| Five checks (06) | `README.md`, "How Nemotron is used", items 1 to 5 |
| $0.107, the mean of nine checks (07) | `README.md`, Measurements; `docs/budget.md`, "Current plan-6 figures" ($0.106976) |
| $20, 5 a UTC day, 900 Tavily credits a month (07) | `README.md`, Measurements, "Spend ceilings" |
| Keel v0.7.3, Chaaya 0.4.2 (08) | `README.md`, "Dependencies"; `go.mod`; `web/package.json` |
| Six areas (05, 13) | `README.md`, "What it does" |
| $0.0667664 USD, 9 credits (09, 09c) | Subway Surfers card's "About this check": `AI processing cost`, `Search credits used` |
| Dates and statuses on the opening tags (00d to 00f) | `evidence/sources.md`, rows 02, 05 and 06 |

## Shot list

The demo cards are stored on the hosted site. Their ids come from `GET /api/cards`.

| Title (age) | Card id | Verdict |
|---|---|---|
| Khan Academy Kids (5) | `c_9ed68a4856013cae0e5c9d915ce2eb85` | Yes |
| Minecraft (9) | `c_5e0e0212770a089dbe25827782909513` | Yes, with these settings |
| Roblox (9) | `c_0ea8b8f1a19ba6588a12f6e3a437906c` | No |
| Subway Surfers (8), the live check of beat 09 | `c_d30e8a78e6943e79e746cba812396be2` | No |

- **Live form and progress (09):** the home page, the title and age typed one key at a time, then "Check this title" pressed. The progress page follows through every stage, and then the card opens.
- **Card beats (10 to 12, 09c):** `/card/{id}` loads. The page zooms onto the evidence, and a box draws around it.
- **Compare beat (13):** `/compare/{roblox id}/{minecraft id}` loads, and the purchases row is boxed.
- **Title, opening, tech, cost, stack and outro (00 to 08, 14):** cards from `slides/cards.html` in the working folder. The opening cards are ids `intro-1` to `intro-5`, and the screenshots are `shot-02`, `shot-05` and `shot-06`. Each screenshot's zoom is set by its `data-box` and `data-s` attributes.

## Rebuilding the video

Run these from `~/work/demo-shoot/before-yes/`. Its `README.md` has the details.

1. `MUSIC_LENGTH_MS=190000 python3 gen_music.py` writes `music/before-yes-music.mp3`. Ask for longer than the film, because the assembly trims it. The ElevenLabs key is read from the environment or from the `.env` files the script names, and is never printed.
2. `node record.mjs` records the card and demo beats into `takes/`. Beats with `"live": true` or `"skip": true` are skipped. Pass beat ids to record only some.
3. `node live.mjs --dry` opens the home page and a stored card, submits nothing, and writes `takes/dry.webm`. Check a frame from it before any live run.
4. `node live.mjs "<title>" <age> [take-id]` records a real check on the hosted site, in one take, with its event log. This spends model and Tavily credit. Only run it when a fresh check is wanted, and check the daily limit first. The take is written to `takes/<take-id>.webm` and `takes/<take-id>.json`.
5. `node assemble.mjs before-yes-demo.mp4` joins the takes and lays the music under them. A beat with `"length"` keeps only its first seconds. A live beat is cut from its event log, with its stage waits sped up. `"skip": true` leaves a beat out. The build fails above 175 seconds.
6. `ffprobe -v error -show_entries format=duration -of csv=p=0 video/before-yes-demo.mp4` confirms the length.

Re-recording a card or demo beat is safe at any time. Those pages are read only.
