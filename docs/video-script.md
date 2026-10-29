# Before Yes: demo video script

The demo video is music with captions and no voiceover. It runs 1 minute 51 seconds as rendered, under the 3-minute limit. It shows the real hosted app at https://beforeyes.nryn.dev, recorded at 1920×1080, plus title cards drawn for the film. No third-party logos or branded footage appear. App names appear only as data on the cards.

The film is built from the working folder `~/work/demo-shoot/before-yes/`, which is outside the repository. Its `script.md` is the full script with the source of every number. This document gives the beats, the shot list and the rebuild steps.

## Story

A child asks for three things. One is a yes. One is a yes with settings. One is a no. The side-by-side compare shows two cards at once. A short beat explains how a check works. The film closes on what one check costs in model spend, and on the fact that it works on any title now.

## Beats

Captions are at most 25 words, with no semicolons and no em dashes. Timings are the rendered length of each beat. Card beats carry their text on the card, not as a caption.

| # | Beat | Length | Picture | Caption or card text |
|---|---|---|---|---|
| 00 | title | 6.1s | Title card | "A child asks for an app or a game. The parent checks it before saying yes." |
| 01 | asks | 8.1s | Card: three requests, for ages 5 and 9 | "Before Yes gives each request one answer for that child's age." |
| 02 | how | 14.1s | Card: four steps and the models | "Tavily gathers the sources." "Nemotron-3 Nano reads every source." "Nemotron-3 Ultra judges." "Code checks every quote." Footer: "Every model call runs on Nebius Token Factory." |
| 03 | form, Khan Academy Kids | 8.8s | Live home page, form filled, not submitted | "The parent types the title and the child's age, then starts the check." |
| 04 | Khan Academy Kids card | 10.2s | Live card, verdict boxed | "Khan Academy Kids for age 5. Five areas are OK, so the answer is yes." |
| 05 | form, Minecraft | 7.6s | Live home page, form filled, not submitted | "The next request is Minecraft, for a 9-year-old." |
| 06 | Minecraft card | 11.1s | Live card, "Settings to change" boxed | "Minecraft for age 9 is yes, with these settings. The card names three settings to turn on." |
| 07 | form, Roblox | 7.4s | Live home page, form filled, not submitted | "The last request is Roblox, also for a 9-year-old." |
| 08 | Roblox card | 11.1s | Live card, "Why this is a concern" boxed in red | "Roblox for age 9 gets a no. The App Store listing says games may contain optional in-game purchases, including loot boxes." |
| 09 | compare | 11.1s | Live compare page, purchases row boxed | "Side by side, the same six areas. On purchases, Roblox is a concern and Minecraft needs a setting." |
| 10 | cost | 9.1s | Card: the cost and the claim | "$0.107", "Model spend for one fresh check, the mean of nine measured checks.", "It works on any title, now." |
| 11 | outro | 6.5s | Card: name, tagline, address, licence | "beforeyes.nryn.dev", "Apache-2.0". Music fades out. |

Total: 111.4 seconds.

## Numbers on screen

| Number | Where it comes from |
|---|---|
| Five areas are OK (04) | The Khan Academy Kids card: five of six area rows read "OK". |
| Three settings (06) | The Minecraft card's "Settings to change" list. |
| Six areas (09) | The compare page's six area rows, and "What it does" in `README.md`. |
| Eight Tavily searches (02) | `README.md`, "What it does": "Each check runs eight Tavily searches, plus extracts." |
| $0.107 and nine checks (10) | `README.md`, "Measurements": mean model cost per fresh check of $0.106976, the mean of nine checks, rounded for the card. Also `docs/budget.md`, "Current plan-6 figures". |

## Shot list

The three demo cards are stored on the hosted site. Their ids come from `GET /api/cards`.

| Title (age) | Card id | Verdict |
|---|---|---|
| Khan Academy Kids (5) | `c_9ed68a4856013cae0e5c9d915ce2eb85` | Yes |
| Minecraft (9) | `c_5e0e0212770a089dbe25827782909513` | Yes, with these settings |
| Roblox (9) | `c_0ea8b8f1a19ba6588a12f6e3a437906c` | No |

- **Form beats (03, 05, 07):** the home page loads. The title and the age are typed one character at a time, through the input event only. The submit button is never pressed and no Enter is sent, so no check starts and nothing is spent.
- **Card beats (04, 06, 08):** `/card/{id}` loads. The page zooms onto the evidence over about 1.5 seconds, and a box draws around it.
- **Compare beat (09):** `/compare/{roblox id}/{minecraft id}` loads, and the purchases row is boxed.
- **Title, how, cost and outro (00, 01, 02, 10, 11):** cards from `slides/cards.html` in the working folder.

## Rebuilding the video

Run these from `~/work/demo-shoot/before-yes/`. `README.md` in that folder has the details.

1. `python3 gen_music.py` writes `music/before-yes-music.mp3`. Set `MUSIC_LENGTH_MS` to override its length. The ElevenLabs key comes from the environment or from `.env` files the script names. It is never printed.
2. `node record.mjs` records every beat into `takes/`. Pass beat ids to record only some, for example `node record.mjs 04-khan,08-roblox`. This needs Google Chrome and the Playwright package in `/home/nryn/work/lambo/scripts/recording/`.
3. `node assemble.mjs before-yes-demo.mp4` joins the takes, lays the music under them and writes `video/before-yes-demo.mp4`. It fails above 175 seconds.
4. `ffprobe -v error -show_entries format=duration -of csv=p=0 video/before-yes-demo.mp4` confirms the length.

Re-recording a live beat is safe at any time. The cards and the three stored demo cards are read only.
