package pipeline

import (
	"strings"

	"github.com/nrynss/before-yes/internal/tavily"
)

// Kind detection and the plan for each kind (T3.7).
//
// An App Store match makes a title KindIOS: the store step has already
// found it, and the plan is Plan's. A title with no match is told apart by
// the rating search's hits (with the policy search's), because that search
// runs for every kind and its hits name the rating boards, stores and
// platforms that describe a title:
//
//   - a YouTube channel page, or hits that call it a channel or YouTuber;
//   - a Google Play page, or hits that name Android or Google Play;
//   - an ESRB or PEGI page, or hits that name consoles or PC stores.
//
// The kind with the most signals wins. A title with no signal at all is
// taken as a console or PC game: it is not on the App Store, and no store,
// channel or board was named, and the console plan's rating-board search is
// the one that can still find something.

// DetectKind picks the kind of a title the App Store did not match.
func DetectKind(title string, hits []tavily.Result) Kind {
	score := map[Kind]int{}
	if t := strings.ToLower(title); strings.Contains(t, "youtube") || strings.Contains(t, "youtuber") {
		score[KindYouTube] += 3
	}
	for _, h := range hits {
		dom, path := registrable(h.URL), strings.ToLower(urlPath(h.URL))
		switch {
		case dom == "youtube.com" && (strings.HasPrefix(path, "/@") || strings.HasPrefix(path, "/channel/") || strings.HasPrefix(path, "/c/") || strings.HasPrefix(path, "/user/")):
			score[KindYouTube] += 3
		case dom == "google.com" && strings.HasPrefix(strings.ToLower(hostOf(h.URL)), "play."):
			score[KindAndroid] += 3
		case dom == "esrb.org" || dom == "pegi.info":
			score[KindConsole] += 2
		}
		text := strings.ToLower(h.Title + " " + h.Content)
		score[KindYouTube] += count(text, "youtube channel", "youtuber", "subscribers")
		score[KindAndroid] += count(text, "android", "google play")
		score[KindConsole] += count(text, "playstation", "xbox", "nintendo switch", "steam", "pc game", "console")
	}
	best, top := KindConsole, 0
	for _, k := range []Kind{KindConsole, KindAndroid, KindYouTube} {
		if score[k] > top {
			best, top = k, score[k]
		}
	}
	return best
}

func count(text string, words ...string) int {
	n := 0
	for _, w := range words {
		n += strings.Count(text, w)
	}
	return n
}

func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// KindArticle is the source kind of a YouTube channel's coverage.
const KindArticle = "article"

// Rating boards a console or PC plan weights.
var ratingBoards = []string{"esrb.org", "pegi.info"}

// noPlayStore keeps Google Play out of every search: the brief rules out
// Play Store scraping, so an Android-only title is read through Tavily
// alone.
var noPlayStore = []string{"play.google.com"}

// PlanFor is the source plan for a kind of title.
//
//   - KindIOS: Plan as it stands.
//   - KindAndroid: Plan with Google Play excluded from every search, and
//     its hits dropped, so nothing is read off the Play Store.
//   - KindConsole: Plan plus a rating-board search narrowed to ESRB and
//     PEGI, whose pages carry the descriptors and interactive elements
//     (in-game purchases, users interact) a console title has instead of a
//     store listing.
//   - KindYouTube: news, forums and Reddit, plus a channel-coverage search.
//     A channel has no privacy policy or terms of its own (they are
//     YouTube's), so those are not planned, and are not missing. The rating
//     search stays because it ran to detect the kind.
func PlanFor(kind Kind, title, officialHost string) []Query {
	base := Plan(title, officialHost)
	switch kind {
	case KindAndroid:
		for i := range base {
			if len(base[i].Search.IncludeDomains) == 0 {
				base[i].Search.ExcludeDomains = append([]string{}, noPlayStore...)
			}
		}
		return base
	case KindConsole:
		return append(base, Query{Name: "rating-boards", Kind: KindRating, Search: tavily.Search{
			Query: title + " ESRB PEGI rating content descriptors in-game purchases users interact", MaxResults: 6,
			IncludeRaw: true, IncludeDomains: append([]string{}, ratingBoards...),
		}})
	case KindYouTube:
		var out []Query
		for _, q := range base {
			if q.Kind == KindPolicy || q.Kind == KindTerms {
				continue
			}
			out = append(out, q)
		}
		return append(out, Query{Name: "channel", Kind: KindArticle, Search: tavily.Search{
			Query: title + " YouTube channel kids content parents review controversy", MaxResults: 8, IncludeRaw: true,
		}})
	}
	return base
}

// dropPlayStore removes Google Play hits, for an Android-only title.
func dropPlayStore(hits []tavily.Result) []tavily.Result {
	var out []tavily.Result
	for _, h := range hits {
		if strings.HasPrefix(strings.ToLower(hostOf(h.URL)), "play.google.") {
			continue
		}
		out = append(out, h)
	}
	return out
}
