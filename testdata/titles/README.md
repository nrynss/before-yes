# Demo titles

Three demo titles and one safer alternative, chosen by the user. Each has a fixture directory, `<slug>/`, captured by `go run ./tools/capture` (see `tools/capture/main.go`). Re-running the capture reproduces the file set; the MANIFEST in each directory records the URLs, status codes, fetch times and Tavily credits.

| Title | Slug | Role | Demo age | iOS trackId | Expected verdict | Reason |
|---|---|---|---|---|---|---|
| Khan Academy Kids | `khan-academy-kids` | yes | 5 | 1378467217 | yes | No ads, no chat, no purchases. |
| Minecraft | `minecraft` | yes with settings | 9 | 479516143 | yes with settings | Multiplayer chat and whispers, fixed by multiplayer off or friends-only, chat Hidden, and whispers off. |
| Roblox | `roblox` | no | 9 | 431946152 | no | Chat filter claims versus a university audit and lawsuits. Age 9 because under-9 chat is off by default. |
| Toca Life World | `toca-life-world` | safer alternative to Roblox | 6 | 1208138685 | yes with settings | Same play-world genre, no chat, in-app purchases. |

The expected verdicts are guesses from public reporting, made for the shortlist. They are not results. The pipeline decides (T3.6b). If it lands a title on a different verdict, pick again from the shortlist rather than tuning the checklist toward the wanted answer.

## What the capture found

- **Toca Life World.** The App Store trackName for trackId 1208138685 is `Toca Boca World: Game & Play`. It does not start with the title, so the official-host rule finds no host and the policy and terms searches run without `include_domains`.
- **Minecraft.** The matched hit, `Minecraft: Play with Friends!`, has no `sellerUrl`, so there is no official host and the policy and terms searches run unrestricted.
- **Khan Academy Kids.** The top policy and terms hits are both `https://www.khankids.org`, the homepage. Tavily's extract failed for both (`failed_results`), so this fixture holds no policy or terms text.
- **Reviews.** Each title's feed served all 10 pages (500 reviews). The feed may hold more; the pipeline also caps at 10 pages.

## Tavily credits per title

| Title | Searches | Extract | Total |
|---|---|---|---|
| Khan Academy Kids | 5 | 0 (reported; both URLs failed) | 5 |
| Minecraft | 5 | 0 (reported, 2 pages read) | 5 |
| Roblox | 5 | 1 | 6 |
| Toca Life World | 5 | 1 | 6 |
| **All four** | | | **22** |

Tavily's reported `usage.credits` on extract is not one credit per five URLs as the price list suggests: Minecraft's two successful pages reported 0. The totals above are what Tavily reported.
