# Before Yes: demo video script

The demo video is music with captions and title cards, and no voiceover. It runs 1 minute 50 seconds as rendered (109.6 seconds), under the 3-minute limit. It shows the hosted app at https://beforeyes.nryn.dev, recorded at 1920×1080, plus title cards drawn for the film. It names the sponsors, NVIDIA Nemotron and Nebius Token Factory, as text. No third-party logos or branded footage appear. App names appear only as data on the cards.

The film is built from the working folder `~/work/demo-shoot/before-yes/`, which is outside the repository. Its `script.md` holds the full script with the source of every number. This document gives the beats, the shot list, the live check and the rebuild steps.

## Story

Three requests from a child, each answered for that child's age: a yes, a yes with settings and a no. Between the title and the requests, a short technical section explains how a check works, from the gather to the code-side checks and the cost. The side-by-side compare shows two cards at once. The film closes on the sponsors, the author and the links.

## Beats

Captions are at most 25 words, with no semicolons and no em or en dashes. Lengths are the rendered length of each beat.

| # | Beat | Length | Picture | Caption or card text |
|---|---|---|---|---|
| 00 | title | 6.0s | Card | "Before Yes", "Research first. Yes next.", "A child asks for an app or a game. The parent checks it before saying yes.", "Built on NVIDIA Nemotron, served on Nebius Token Factory", "by Narayan SS" |
| 01 | asks | 5.0s | Card: three requests | "Before Yes gives each request one answer for that child's age." |
| 02 | gather | 8.0s | Card: eight searches by kind | "Tavily runs eight searches, by kind." App Store listing facts. |
| 03 | read | 8.0s | Card: findings with quotes | "Nemotron-3 Nano reads every source, in chunks." Thinking off: 183 output tokens with thinking on, 4 with it off. |
| 04 | quote | 7.0s | Card: drop-rate table | "Code checks every quote before the judge sees it." Dropped: 36.7%, 35.7%, 27.7%. |
| 05 | judge | 8.0s | Card | "Nemotron-3 Ultra grades six areas." Sampling settings for each pass. "Ultra looped at temperature 0, so its calls set their own sampling." |
| 06 | checks | 8.0s | Card | "Five checks the model cannot skip." Citation pruning, grounding, entailment, rule gate, `checklist.Decide`. |
| 07 | cost | 7.0s | Card | "$0.107" model spend per fresh check, the mean of nine. Limits: $20 of model credit, 5 checks a UTC day, 900 Tavily credits a month. |
| 08 | stack | 6.0s | Card | Go on Keel v0.7.3, Svelte 5 on Chaaya 0.4.2, SQLite, Nebius Token Factory. |
| 09b | live verdict | 6.0s | Live card: YouTube Kids (7), verdict boxed red | "YouTube Kids for age 7 gets a no. Reviewers report sexual content, horror videos and violence." |
| 09c | live cost | 6.0s | Live card: "AI processing cost" boxed | "About this check lists the model cost of this check: $0.09419692 USD." |
| 10 | khan | 6.5s | Live card: Khan Academy Kids (5) | "Khan Academy Kids for age 5: five areas are OK, so the answer is yes." |
| 11 | minecraft | 6.5s | Live card: Minecraft (9) | "Minecraft for age 9: yes, with settings. The card names three settings to turn on." |
| 12 | roblox | 7.5s | Live card: Roblox (9), deciding quote boxed | "Roblox for age 9: no. The App Store listing says games may contain optional in-game purchases, including lootboxes." |
| 13 | compare | 7.0s | Live compare page, purchases row boxed | "Side by side, on purchases Roblox is a concern and Minecraft needs a setting." |
| 14 | outro | 7.0s | Card: sponsors, author, links | "Built on NVIDIA Nemotron, served on Nebius Token Factory", "by Narayan SS", beforeyes.nryn.dev, github.com/nrynss/before-yes, Apache-2.0. |

Total: 109.6 seconds.

## The live check

One real check was run on the hosted site, from the home page: the title and age were typed, and "Check this title" was pressed.

| Field | Value |
|---|---|
| Title | YouTube Kids |
| Age | 7 |
| Verdict | No (content concern: reviewers report sexual content, horror videos and violence) |
| Model cost on the page | $0.09419692 USD |
| Tavily credits on the page | 9 |
| Fresh checks used | 1 of 2 allowed |

The stored card is on the film (beats 09b and 09c). The progress stream is not. The recording of the check was made in one take, but the overlay's green wipe covered every frame of it, because the recorder never ran the wipe-out step. The take was dropped, and no second check was run, because the first one finished. The rule for a second check applies only when the first fails to finish.

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
| $0.09419692 (09c) | The live card's "About this check" (`AI processing cost`), read from the stored card |

## Shot list

The demo cards are stored on the hosted site. Their ids come from `GET /api/cards`.

| Title (age) | Card id | Verdict |
|---|---|---|
| Khan Academy Kids (5) | `c_9ed68a4856013cae0e5c9d915ce2eb85` | Yes |
| Minecraft (9) | `c_5e0e0212770a089dbe25827782909513` | Yes, with these settings |
| Roblox (9) | `c_0ea8b8f1a19ba6588a12f6e3a437906c` | No |
| YouTube Kids (7), the live check | `c_1eb9e27d825c31d5f104a4cc8dd1e64b` | No |

- **Card beats (10 to 12, 09b, 09c):** `/card/{id}` loads. The page zooms onto the evidence, and a box draws around it.
- **Compare beat (13):** `/compare/{roblox id}/{minecraft id}` loads, and the purchases row is boxed.
- **Title, tech, cost, stack and outro (00 to 08, 14):** cards from `slides/cards.html` in the working folder.

## Rebuilding the video

Run these from `~/work/demo-shoot/before-yes/`. Its `README.md` has the details.

1. `MUSIC_LENGTH_MS=190000 python3 gen_music.py` writes `music/before-yes-music.mp3`. Ask for longer than the film, because the assembly trims it. The ElevenLabs key is read from the environment or from the `.env` files the script names, and is never printed.
2. `node record.mjs` records the card and demo beats into `takes/`. Beats with `"live": true` or `"skip": true` are skipped. Pass beat ids to record only some.
3. `node live.mjs "<title>" <age> [take-id]` records a real check on the hosted site, in one take, with its event log. This spends model and Tavily credit. Run it only when a fresh check is wanted, and check the daily limit first. It must also run the overlay's wipe-out step, or the take is covered.
4. `node assemble.mjs before-yes-demo.mp4` joins the takes and lays the music under them. A beat with `"length"` keeps only its first seconds. A live beat is cut from its event log, with its stage waits sped up, and `"skip": true` leaves it out. It fails above 175 seconds.
5. `ffprobe -v error -show_entries format=duration -of csv=p=0 video/before-yes-demo.mp4` confirms the length.

Re-recording a card or demo beat is safe at any time. Those pages are read only.
