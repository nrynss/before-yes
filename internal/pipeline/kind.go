package pipeline

import (
	"regexp"
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
//   - a YouTube channel or video page, or a "- YouTube" page title;
//   - a Google Play listing, or a "- Apps on Google Play" title;
//   - a Steam, PlayStation, Xbox or Nintendo store listing, or an ESRB or
//     PEGI page;
//   - and, weakly, whole words in a hit's text naming any of these.
//
// The kind with the most weight wins (see the signal weights). A title with
// no signal at all is KindUnknown (user decision): nothing placed it, so it
// gets the general plan and the card says "kind unknown".

// allKinds is every kind a plan exists for; GatherVersion hashes each.
var allKinds = []Kind{KindIOS, KindAndroid, KindConsole, KindYouTube, KindUnknown}

// Signal weights. A direct store listing or channel page names the kind
// outright; a rating board's page is strong but lists platforms; a word in
// a hit's text is weak, and counts once per hit per kind, so one review
// page that lists every platform cannot outvote a store listing.
const (
	listingSignal = 5
	videoSignal   = 3
	boardSignal   = 3
	mentionSignal = 1
)

var (
	youtubeTitle = regexp.MustCompile(`\byoutuber?\b`)
	youtubeWords = regexp.MustCompile(`\b(youtube channel|youtuber|subscribers)\b`)
	androidWords = regexp.MustCompile(`\b(android|google play)\b`)
	consoleWords = regexp.MustCompile(`\b(playstation|xbox|nintendo switch|steam|pc game)\b`)
)

// urlKind reads a hit's URL alone: a host and path that is a store
// listing, a channel or video page, or a rating board.
func urlKind(raw string) (Kind, int) {
	host := strings.TrimPrefix(strings.ToLower(hostOf(raw)), "www.")
	path := strings.ToLower(urlPath(raw))
	has := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(path, p) {
				return true
			}
		}
		return false
	}
	yt := host == "youtube.com" || host == "m.youtube.com"
	switch {
	case host == "play.google.com" && has("/store/apps"):
		return KindAndroid, listingSignal
	case yt && has("/@", "/channel/", "/c/", "/user/"):
		return KindYouTube, listingSignal
	case yt && has("/watch", "/shorts/", "/playlist"), host == "youtu.be":
		return KindYouTube, videoSignal
	case host == "store.steampowered.com" && has("/app/"),
		host == "store.playstation.com",
		host == "xbox.com" && strings.Contains(path, "/games/store"),
		strings.HasSuffix(host, "nintendo.com") && strings.Contains(path, "/store/"):
		return KindConsole, listingSignal
	case registrable(raw) == "esrb.org" || registrable(raw) == "pegi.info":
		return KindConsole, boardSignal
	}
	return "", 0
}

// titleKind reads a hit's page title: the suffix a store or YouTube puts
// on its own pages.
func titleKind(title string) (Kind, int) {
	t := strings.ToLower(strings.TrimSpace(title))
	switch {
	case strings.HasSuffix(t, "- apps on google play"):
		return KindAndroid, listingSignal
	case strings.HasSuffix(t, "- youtube"):
		return KindYouTube, videoSignal
	}
	return "", 0
}

// DetectKind picks the kind of a title the App Store did not match.
func DetectKind(title string, hits []tavily.Result) Kind {
	score := map[Kind]int{}
	if youtubeTitle.MatchString(strings.ToLower(title)) {
		score[KindYouTube] += listingSignal
	}
	words := map[Kind]*regexp.Regexp{KindYouTube: youtubeWords, KindAndroid: androidWords, KindConsole: consoleWords}
	for _, h := range hits {
		if k, w := urlKind(h.URL); k != "" {
			score[k] += w
		}
		if k, w := titleKind(h.Title); k != "" {
			score[k] += w
		}
		text := strings.ToLower(h.Title + " " + h.Content)
		for k, re := range words {
			if re.MatchString(text) {
				score[k] += mentionSignal
			}
		}
	}
	best, top := KindUnknown, 0
	for _, k := range []Kind{KindConsole, KindAndroid, KindYouTube} {
		if score[k] > top {
			best, top = k, score[k]
		}
	}
	return best
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

// generalKinds are the source kinds the general plan, for a title of
// unknown kind, keeps from Plan.
var generalKinds = []string{KindNews, KindForum, KindReddit, KindRating}

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
//   - KindUnknown: the general plan, news, forums (Reddit and others) and
//     the general rating search, with no ESRB/PEGI search and no policy or
//     terms search (no store or official site placed it). Those kinds are
//     not planned, so they are not missing.
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
	case KindUnknown:
		var out []Query
		for _, q := range base {
			if contains(generalKinds, q.Kind) {
				out = append(out, q)
			}
		}
		return out
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
