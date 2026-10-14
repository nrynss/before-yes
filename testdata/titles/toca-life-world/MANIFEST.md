# Capture manifest: Toca Life World

Written by `tools/capture`. The Tavily key is read from `TAVILY_KEY` and is never recorded here.

- Slug: `toca-life-world`
- Title searched: Toca Life World
- iOS trackId (reviews): 1208138685
- App Store trackName for that trackId: Toca Boca World: Game & Play
- Official host used for policy and terms: none
- Reviews captured: 500
- Tavily credits for this title: 6

## Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| appstore-search.json | https://itunes.apple.com/search?country=us&entity=software&limit=5&term=Toca+Life+World | 200 | 2026-10-13T18:58:52Z |  |  |
| reviews-page-01.json | https://itunes.apple.com/us/rss/customerreviews/page=1/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:53Z |  | 50 entries, 50 reviews |
| reviews-page-02.json | https://itunes.apple.com/us/rss/customerreviews/page=2/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:54Z |  | 50 entries, 50 reviews |
| reviews-page-03.json | https://itunes.apple.com/us/rss/customerreviews/page=3/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:55Z |  | 50 entries, 50 reviews |
| reviews-page-04.json | https://itunes.apple.com/us/rss/customerreviews/page=4/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:56Z |  | 50 entries, 50 reviews |
| reviews-page-05.json | https://itunes.apple.com/us/rss/customerreviews/page=5/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:57Z |  | 50 entries, 50 reviews |
| reviews-page-06.json | https://itunes.apple.com/us/rss/customerreviews/page=6/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:58Z |  | 50 entries, 50 reviews |
| reviews-page-07.json | https://itunes.apple.com/us/rss/customerreviews/page=7/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:58:59Z |  | 50 entries, 50 reviews |
| reviews-page-08.json | https://itunes.apple.com/us/rss/customerreviews/page=8/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:59:00Z |  | 50 entries, 50 reviews |
| reviews-page-09.json | https://itunes.apple.com/us/rss/customerreviews/page=9/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:59:01Z |  | 50 entries, 50 reviews |
| reviews-page-10.json | https://itunes.apple.com/us/rss/customerreviews/page=10/id=1208138685/sortby=mostrecent/json | 200 | 2026-10-13T18:59:01Z |  | 50 entries, 50 reviews |
| tavily-search-policy.json | https://api.tavily.com/search | 200 | 2026-10-13T18:59:03Z | 1 |  |
| tavily-search-terms.json | https://api.tavily.com/search | 200 | 2026-10-13T18:59:04Z | 1 |  |
| tavily-search-forum.json | https://api.tavily.com/search | 200 | 2026-10-13T18:59:07Z | 1 |  |
| tavily-search-news.json | https://api.tavily.com/search | 200 | 2026-10-13T18:59:11Z | 1 |  |
| tavily-search-rating.json | https://api.tavily.com/search | 200 | 2026-10-13T18:59:13Z | 1 |  |
| tavily-extract.json | https://api.tavily.com/extract | 200 | 2026-10-13T18:59:13Z | 1 |  |

## Notes

- no App Store hit's trackName starts with "Toca Life World", so policy and terms run without include_domains
- reviews stopped at the feed's page limit of 10

## T0.4a

Written by `tools/capture -only`. A run rewrites the files of the kinds it ran and keeps the other rows. The credits of replaced calls stay in the total. The Tavily key is never recorded here.

- Common Sense review page: none found
- Credits on calls replaced by later runs: 0
- T0.4a Tavily credits for this title: 3

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-search-csm.json | https://api.tavily.com/search | 200 | 2026-10-13T19:09:19Z | 1 |  |
| tavily-search-reddit-community.json | https://api.tavily.com/search | 200 | 2026-10-13T19:09:24Z | 1 | 8 results, 0 with raw_content |
| tavily-search-reddit-parents.json | https://api.tavily.com/search | 200 | 2026-10-13T19:09:22Z | 1 | 8 results, 0 with raw_content |

### Notes

- review pages for other titles, not used: https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories, https://www.commonsensemedia.org/app-reviews/toca-life-city
- no Common Sense Media review page for slug "toca-life-world" among 5 search hits; csm extract skipped

## T0.4b

Written by `tools/capture -only advanced`. A run rewrites this section. The Tavily key is never recorded here.

- T0.4b Tavily credits for this title: 1

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-extract-csm-advanced.json | https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories/user-reviews/adult https://www.commonsensemedia.org/app-reviews/toca-life-world-build-stories/user-reviews/child | 200 | 2026-10-13T19:16:39Z | 1 | advanced; 2 results with text; failed_results: none |
| tavily-extract-reddit-advanced.json | https://www.reddit.com/r/tocaboca/comments/1t2ue21/toca_world_should_have_a_monthly_subscription https://www.reddit.com/r/tocaboca/comments/1qkq5hs/can_you_tell_me_about_toca_boca | 200 | 2026-10-13T19:16:50Z | 0 | advanced; 0 results with text; failed_results: https://www.reddit.com/r/tocaboca/comments/1t2ue21/toca_world_should_have_a_monthly_subscription (Failed to fetch url); https://www.reddit.com/r/tocaboca/comments/1qkq5hs/can_you_tell_me_about_toca_boca (Failed to fetch url) |

### Notes

- none

## T3.8a

Written by `tools/capture -only storepage`. A run rewrites this section. The Tavily key is never recorded here.

- T3.8a Tavily credits for this title: 0

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-extract-storepage.json | https://apps.apple.com/us/app/toca-boca-world-game-play/id1208138685?uo=4 | 200 | 2026-10-14T08:45:40Z | 0 | basic; 1 results with text; review text: yes; failed_results: none |

### Notes

- none
