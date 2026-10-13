# Capture manifest: Roblox

Written by `tools/capture`. The Tavily key is read from `TAVILY_KEY` and is never recorded here.

- Slug: `roblox`
- Title searched: Roblox
- iOS trackId (reviews): 431946152
- App Store trackName for that trackId: Roblox
- Official host used for policy and terms: roblox.com
- Reviews captured: 500
- Tavily credits for this title: 6

## Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| appstore-search.json | https://itunes.apple.com/search?country=us&entity=software&limit=5&term=Roblox | 200 | 2026-10-13T18:57:25Z |  |  |
| reviews-page-01.json | https://itunes.apple.com/us/rss/customerreviews/page=1/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:26Z |  | 50 entries, 50 reviews |
| reviews-page-02.json | https://itunes.apple.com/us/rss/customerreviews/page=2/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:27Z |  | 50 entries, 50 reviews |
| reviews-page-03.json | https://itunes.apple.com/us/rss/customerreviews/page=3/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:29Z |  | 50 entries, 50 reviews |
| reviews-page-04.json | https://itunes.apple.com/us/rss/customerreviews/page=4/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:30Z |  | 50 entries, 50 reviews |
| reviews-page-05.json | https://itunes.apple.com/us/rss/customerreviews/page=5/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:31Z |  | 50 entries, 50 reviews |
| reviews-page-06.json | https://itunes.apple.com/us/rss/customerreviews/page=6/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:32Z |  | 50 entries, 50 reviews |
| reviews-page-07.json | https://itunes.apple.com/us/rss/customerreviews/page=7/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:34Z |  | 50 entries, 50 reviews |
| reviews-page-08.json | https://itunes.apple.com/us/rss/customerreviews/page=8/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:35Z |  | 50 entries, 50 reviews |
| reviews-page-09.json | https://itunes.apple.com/us/rss/customerreviews/page=9/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:36Z |  | 50 entries, 50 reviews |
| reviews-page-10.json | https://itunes.apple.com/us/rss/customerreviews/page=10/id=431946152/sortby=mostrecent/json | 200 | 2026-10-13T18:57:37Z |  | 50 entries, 50 reviews |
| tavily-search-policy.json | https://api.tavily.com/search | 200 | 2026-10-13T18:57:42Z | 1 |  |
| tavily-search-terms.json | https://api.tavily.com/search | 200 | 2026-10-13T18:57:44Z | 1 |  |
| tavily-search-forum.json | https://api.tavily.com/search | 200 | 2026-10-13T18:57:47Z | 1 |  |
| tavily-search-news.json | https://api.tavily.com/search | 200 | 2026-10-13T18:57:49Z | 1 |  |
| tavily-search-rating.json | https://api.tavily.com/search | 200 | 2026-10-13T18:57:50Z | 1 |  |
| tavily-extract.json | https://api.tavily.com/extract | 200 | 2026-10-13T18:57:51Z | 1 |  |

## Notes

- reviews stopped at the feed's page limit of 10

## T0.4a

Written by `tools/capture -only`. A run rewrites the files of the kinds it ran and keeps the other rows. The credits of replaced calls stay in the total. The Tavily key is never recorded here.

- Common Sense review page: none found
- Credits on calls replaced by later runs: 1
- T0.4a Tavily credits for this title: 4

### Files

| File | Source | HTTP status | Fetched (UTC) | Tavily credits | Note |
|---|---|---|---|---|---|
| tavily-search-csm.json | https://api.tavily.com/search | 200 | 2026-10-13T19:08:42Z | 1 |  |
| tavily-search-reddit-community.json | https://api.tavily.com/search | 200 | 2026-10-13T19:06:15Z | 1 | 8 results, 0 with raw_content |
| tavily-search-reddit-parents.json | https://api.tavily.com/search | 200 | 2026-10-13T19:06:08Z | 1 | 8 results, 0 with raw_content |

### Notes

- review pages for other titles, not used: https://www.commonsensemedia.org/website-reviews/brookhaven-rp, https://www.commonsensemedia.org/website-reviews/adopt-me
- no Common Sense Media review page for slug "roblox" among 5 search hits; csm extract skipped
