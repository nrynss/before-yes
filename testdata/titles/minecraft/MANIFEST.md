# Capture manifest: Minecraft

Written by `tools/capture`. The Tavily key is read from `TAVILY_KEY` and is never recorded here.

- Slug: `minecraft`
- Title searched: Minecraft
- iOS trackId (reviews): 479516143
- App Store trackName for that trackId: Minecraft: Play with Friends!
- Official host used for policy and terms: none
- Reviews captured: 500
- Tavily credits for this title: 5

## Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| appstore-search.json | https://itunes.apple.com/search?country=us&entity=software&limit=5&term=Minecraft | 200 | 2026-10-13T18:58:29Z |  |  |
| reviews-page-01.json | https://itunes.apple.com/us/rss/customerreviews/page=1/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:30Z |  | 50 entries, 50 reviews |
| reviews-page-02.json | https://itunes.apple.com/us/rss/customerreviews/page=2/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:30Z |  | 50 entries, 50 reviews |
| reviews-page-03.json | https://itunes.apple.com/us/rss/customerreviews/page=3/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:31Z |  | 50 entries, 50 reviews |
| reviews-page-04.json | https://itunes.apple.com/us/rss/customerreviews/page=4/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:32Z |  | 50 entries, 50 reviews |
| reviews-page-05.json | https://itunes.apple.com/us/rss/customerreviews/page=5/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:33Z |  | 50 entries, 50 reviews |
| reviews-page-06.json | https://itunes.apple.com/us/rss/customerreviews/page=6/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:34Z |  | 50 entries, 50 reviews |
| reviews-page-07.json | https://itunes.apple.com/us/rss/customerreviews/page=7/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:35Z |  | 50 entries, 50 reviews |
| reviews-page-08.json | https://itunes.apple.com/us/rss/customerreviews/page=8/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:36Z |  | 50 entries, 50 reviews |
| reviews-page-09.json | https://itunes.apple.com/us/rss/customerreviews/page=9/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:37Z |  | 50 entries, 50 reviews |
| reviews-page-10.json | https://itunes.apple.com/us/rss/customerreviews/page=10/id=479516143/sortby=mostrecent/json | 200 | 2026-10-13T18:58:38Z |  | 50 entries, 50 reviews |
| tavily-search-policy.json | https://api.tavily.com/search | 200 | 2026-10-13T18:58:40Z | 1 |  |
| tavily-search-terms.json | https://api.tavily.com/search | 200 | 2026-10-13T18:58:41Z | 1 |  |
| tavily-search-forum.json | https://api.tavily.com/search | 200 | 2026-10-13T18:58:43Z | 1 |  |
| tavily-search-news.json | https://api.tavily.com/search | 200 | 2026-10-13T18:58:47Z | 1 |  |
| tavily-search-rating.json | https://api.tavily.com/search | 200 | 2026-10-13T18:58:48Z | 1 |  |
| tavily-extract.json | https://api.tavily.com/extract | 200 | 2026-10-13T18:58:49Z | 0 |  |

## Notes

- the matched hit "Minecraft: Play with Friends!" has no usable sellerUrl host, so policy and terms run without include_domains
- reviews stopped at the feed's page limit of 10

## T0.4b

Written by `tools/capture -only advanced`. A run rewrites this section. The Tavily key is never recorded here.

- T0.4b Tavily credits for this title: 2

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-extract-csm-advanced.json | https://www.commonsensemedia.org/app-reviews/minecraft/user-reviews/adult https://www.commonsensemedia.org/app-reviews/minecraft/user-reviews/child | 200 | 2026-10-13T19:16:23Z | 1 | advanced; 2 results with text; failed_results: none |
| tavily-extract-reddit-advanced.json | https://www.reddit.com/r/Minecraft/comments/1qfz5go/i_am_a_parent_who_does_not_play_anything_my_7 https://www.reddit.com/r/Minecraft/comments/1k7lbl8/help_a_tech_challenged_parent | 200 | 2026-10-13T19:16:30Z | 1 | advanced; 0 results with text; failed_results: https://www.reddit.com/r/Minecraft/comments/1k7lbl8/help_a_tech_challenged_parent (Failed to fetch url); https://www.reddit.com/r/Minecraft/comments/1qfz5go/i_am_a_parent_who_does_not_play_anything_my_7 (Failed to fetch url) |

### Notes

- none

## T3.8a

Written by `tools/capture -only storepage`. A run rewrites this section. The Tavily key is never recorded here.

- T3.8a Tavily credits for this title: 0

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-extract-storepage.json | https://apps.apple.com/us/app/minecraft-play-with-friends/id479516143?uo=4 | 200 | 2026-10-14T08:45:06Z | 0 | basic; 1 results with text; review text: yes; failed_results: none |

### Notes

- none

## T0.4a

Written by `tools/capture -only`. A run rewrites the files of the kinds it ran and keeps the other rows. The credits of replaced calls stay in the total. The Tavily key is never recorded here.

- Common Sense review page: none found
- Credits on calls replaced by later runs: 0
- T0.4a Tavily credits for this title: 4

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-search-controls.json | https://api.tavily.com/search | 200 | 2026-10-16T14:08:29Z | 1 | 6 results, 4 with raw_content |
| tavily-search-csm.json | https://api.tavily.com/search | 200 | 2026-10-13T19:08:51Z | 1 |  |
| tavily-search-reddit-community.json | https://api.tavily.com/search | 200 | 2026-10-13T19:08:57Z | 1 | 8 results, 0 with raw_content |
| tavily-search-reddit-parents.json | https://api.tavily.com/search | 200 | 2026-10-13T19:08:54Z | 1 | 8 results, 0 with raw_content |

### Notes

- none
