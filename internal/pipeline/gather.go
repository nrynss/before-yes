package pipeline

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/net/publicsuffix"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// Searcher is the Tavily surface the gatherer uses. *tavily.Client has it.
type Searcher interface {
	Search(ctx context.Context, s tavily.Search) ([]tavily.Result, tavily.Credits, error)
	ExtractWithFailures(ctx context.Context, urls []string) (tavily.Extracted, error)
}

// Store is the App Store surface the gatherer uses. *appstore.Client has it.
type Store interface {
	Find(ctx context.Context, title string, limit int) ([]appstore.App, error)
	Reviews(ctx context.Context, appID int64, pages int) ([]appstore.Review, error)
	FeedURL(appID int64, page int) string
}

// Meter is the spend surface every paid call goes through. *spend.Meter has
// it.
type Meter interface {
	Spend(ctx context.Context, pool spend.Pool, kind, ref string, estimate cost.Price, work cost.Work) (cost.Usage, error)
}

// Doc is one source text the reader reads, with the source a finding quoting
// it will cite.
type Doc struct {
	Source finding.Source
	Text   string
}

// PlanVersion names the source plan. It is part of the evidence cache key,
// so a change to the extract rules, the error-page rule, host narrowing or
// the clean-up below must bump it. (The plan's queries and the Gathered
// shape are hashed into the key on their own; see pipeline.ReaderVersion.)
// plan-3: round-2 error-page, host and link rules.
const PlanVersion = "plan-3"

// Source kinds a plan query or the store step fills. A kind with no usable
// document is reported in Gathered.Missing, for the card's small print.
const (
	KindReview = "review"
	KindStore  = "store"
	KindPolicy = "policy"
	KindTerms  = "terms"
	KindReddit = "reddit"
	KindForum  = "forum"
	KindNews   = "news"
	KindRating = "rating"
)

// Query is one planned search. Kind is the finding source kind its documents
// carry. Extract marks a search whose hits stand for a page that must be read
// whole: the policy and the terms.
type Query struct {
	Name    string
	Kind    string
	Search  tavily.Search
	Extract bool
}

// Plan is the fixed source list for one title: seven basic searches and one
// extract call, about eight Tavily credits. officialHost narrows the policy
// and terms searches; empty leaves them open.
//
// The names match the fixture files under testdata/titles, which
// tools/capture recorded with the same request shapes.
func Plan(title, officialHost string) []Query {
	policy := tavily.Search{Query: title + " privacy policy", MaxResults: 5}
	terms := tavily.Search{Query: title + " terms of service", MaxResults: 5}
	if officialHost != "" {
		policy.IncludeDomains = []string{officialHost}
		terms.IncludeDomains = []string{officialHost}
	}
	reddit := func(q string) tavily.Search {
		return tavily.Search{Query: q, MaxResults: 8, IncludeDomains: []string{"reddit.com"}, IncludeRaw: true}
	}
	return []Query{
		{Name: "policy", Kind: KindPolicy, Search: policy, Extract: true},
		{Name: "terms", Kind: KindTerms, Search: terms, Extract: true},
		{Name: "reddit-parents", Kind: KindReddit, Search: reddit(title + " parents kids safety experience")},
		{Name: "reddit-community", Kind: KindReddit, Search: reddit(title + " chat scams moderation players")},
		{Name: "forum", Kind: KindForum, Search: tavily.Search{
			Query:      title + " parents experience kids chat strangers purchases",
			MaxResults: 8, IncludeRaw: true,
			IncludeDomains: []string{"commonsensemedia.org", "mumsnet.com", "netmums.com"},
		}},
		{Name: "news", Kind: KindNews, Search: tavily.Search{
			Query: title + " children safety fine lawsuit FTC regulator data breach", Topic: tavily.TopicNews,
			MaxResults: 8, IncludeRaw: true,
		}},
		{Name: "rating", Kind: KindRating, Search: tavily.Search{
			Query: title + " age rating ESRB PEGI parental controls", MaxResults: 6, IncludeRaw: true,
		}},
	}
}

const (
	// maxDocChars caps the text taken from one page. A forum page's raw
	// content can run to megabytes of navigation; the reader needs the
	// thread.
	maxDocChars = 60000
	// extractTries is how many hits per policy or terms search go into the
	// one extract call, so a homepage or a failed read falls through to the
	// next hit. Four URLs still fit the price list's one-credit block of five.
	extractTries = 2
	storeHits    = 5
)

// Gathered is what the gather step found.
type Gathered struct {
	// App is the matched App Store hit, nil when no hit matched the title.
	App *appstore.App
	// Host is the official host the policy and terms were narrowed to, and
	// HostFrom says where it came from: "seller_url", "policy_hit", or empty.
	Host     string
	HostFrom string
	Docs     []Doc
	// Credits is what the Tavily calls cost, as Tavily reported it.
	Credits float64
	// Missing names the source kinds that came back with no usable
	// document, in plan order, each once.
	Missing []string
	// Notes says, in plain words, what was left out and why: a not-found
	// page, a homepage, a failed extract.
	Notes []string
	// Failures names the source calls that errored rather than came back
	// empty: "appstore_search", "review_feed", or "extract <url>" for a page
	// Tavily could not read whose kind then had no document (an unread
	// candidate when another of its kind was kept cost nothing). A kind missing because the source is absent
	// (no listing, an empty feed, a 404 page) is not a failure; a kind
	// missing because a call failed may be there next time, so the
	// pipeline does not cache such evidence.
	Failures []string
}

// Gatherer runs the source plan for one title.
type Gatherer struct {
	Store  Store
	Search Searcher
	Meter  Meter
	// ReviewPages is how many feed pages to read. Zero means
	// appstore.MaxPages.
	ReviewPages int
	Now         func() time.Time
}

// Gather runs the plan for title. ref is the check id every paid call is
// booked under. A failed App Store call is a missing source, not an error,
// because a console title has no App Store page at all. A failed Tavily
// call is an error.
func (g *Gatherer) Gather(ctx context.Context, title, ref string) (Gathered, error) {
	var out Gathered
	if g.Search == nil || g.Meter == nil {
		return out, errors.New("gather: a searcher and a meter are required")
	}
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	at := now().UTC()
	missing := map[string]bool{}
	miss := func(kind, why string) {
		if why != "" {
			out.Notes = append(out.Notes, why)
		}
		if !missing[kind] {
			missing[kind] = true
			out.Missing = append(out.Missing, kind)
		}
	}

	// 1. The App Store. It names the official host, which narrows the
	// policy and terms searches to the developer's own pages.
	var apps []appstore.App
	if g.Store != nil {
		var err error
		apps, err = g.Store.Find(ctx, title, storeHits)
		if err != nil {
			out.Notes = append(out.Notes, "App Store search failed: "+err.Error())
			out.Failures = append(out.Failures, "appstore_search")
		}
	}
	out.App = pickApp(apps, title)
	if out.App != nil {
		if h := officialDomain(out.App.SellerURL); h != "" {
			out.Host, out.HostFrom = h, "seller_url"
		}
	}

	// 2. Policy first. With no host yet, its hits name one: a hit whose
	// domain is the developer's or the title's own name.
	plan := Plan(title, out.Host)
	hits := map[string][]tavily.Result{}
	for i, q := range plan {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if q.Kind == KindTerms && out.Host != "" && len(q.Search.IncludeDomains) == 0 {
			// The host was found from the policy hits after the plan was
			// drawn, so the terms search is narrowed now.
			q.Search.IncludeDomains = []string{out.Host}
			plan[i] = q
		}
		res, used, err := g.search(ctx, ref, q.Search)
		out.Credits += used
		if err != nil {
			return out, fmt.Errorf("gather: %s search: %w", q.Name, err)
		}
		hits[q.Name] = res
		if q.Kind == KindPolicy && out.Host == "" {
			if h, a := hostFromHits(res, apps, out.App, title); h != "" {
				out.Host, out.HostFrom = h, "policy_hit"
				if out.App == nil {
					out.App = a
				}
			}
		}
	}

	// The reviews and the listing, once the app is settled.
	if out.App == nil {
		miss(KindReview, "no App Store hit matches the title")
	} else {
		g.storeDocs(ctx, out.App, at, &out, miss)
	}

	// 3. The policy and terms pages: one extract call, and a second only
	// when a homepage names a privacy or terms link to follow.
	docs, notes, failed, used, err := g.policyPages(ctx, ref, plan, hits, out.Host, at)
	out.Failures = append(out.Failures, failed...)
	out.Credits += used
	out.Notes = append(out.Notes, notes...)
	if err != nil {
		return out, err
	}
	out.Docs = append(out.Docs, docs...)
	for _, q := range plan {
		if q.Extract && !hasKind(docs, q.Kind) {
			miss(q.Kind, "")
		}
	}

	// 4. Reddit snippets, the forum, news and rating pages. The two Reddit
	// searches share a kind, which is missing only when both come back empty.
	found := map[string]int{}
	for _, q := range plan {
		if q.Extract {
			continue
		}
		found[q.Kind] += 0
		for _, r := range hits[q.Name] {
			d, why := hitDoc(q.Kind, r, at)
			if why != "" {
				out.Notes = append(out.Notes, why)
				continue
			}
			out.Docs = append(out.Docs, d)
			found[q.Kind]++
		}
	}
	for _, q := range plan {
		if !q.Extract && found[q.Kind] == 0 {
			miss(q.Kind, "")
		}
	}
	return out, nil
}

// policyPages reads the policy and terms. Each kind's first two hits on the
// official host that are not homepages go into one extract call, and the
// first page read that is not an error page and is about its kind is kept.
// A kind whose hits are only homepages extracts the homepage instead, and
// when it can be read, the links on it named for the kind, on the same
// host, go into one more extract call.
func (g *Gatherer) policyPages(ctx context.Context, ref string, plan []Query, hits map[string][]tavily.Result, host string, at time.Time) ([]Doc, []string, []string, float64, error) {
	var docs []Doc
	var notes, unread []string
	credits := 0.0
	taken := map[string]string{}
	want := map[string][]string{}
	homes := map[string][]string{}
	var kinds, urls []string
	for _, q := range plan {
		if !q.Extract {
			continue
		}
		kinds = append(kinds, q.Kind)
		c := candidates(hits[q.Name], q.Kind, host, taken)
		for _, u := range c {
			taken[u] = q.Kind
		}
		want[q.Kind] = c
		urls = append(urls, c...)
		if len(c) == 0 {
			for _, h := range hits[q.Name] {
				if host != "" && homepage(h.URL) && registrable(h.URL) == host {
					homes[q.Kind] = append(homes[q.Kind], h.URL)
					if !contains(urls, h.URL) {
						urls = append(urls, h.URL)
					}
					break
				}
			}
			if len(homes[q.Kind]) == 0 && host == "" {
				notes = append(notes, fmt.Sprintf("no official host known, so no homepage is followed for a %s page", q.Kind))
			} else if len(homes[q.Kind]) == 0 {
				notes = append(notes, fmt.Sprintf("no %s page on the official host to read", q.Kind))
			}
		}
	}
	read := func(urls []string) (map[string]tavily.Page, error) {
		pages := map[string]tavily.Page{}
		if len(urls) == 0 {
			return pages, nil
		}
		ex, used, err := g.extract(ctx, ref, urls)
		credits += used
		if err != nil {
			return nil, fmt.Errorf("gather: extract: %w", err)
		}
		for _, p := range ex.Pages {
			pages[p.URL] = p
		}
		for _, f := range ex.Failed {
			if !contains(unread, f.URL) {
				unread = append(unread, f.URL)
			}
		}
		return pages, nil
	}
	got := map[string]bool{}
	// failures are the unread pages that may have cost a kind: a page whose
	// kind still has no document. A failed read of one candidate when
	// another of its kind was kept cost nothing.
	failures := func() []string {
		var out []string
		for _, u := range unread {
			lost := taken[u] != "" && !got[taken[u]]
			for k, hs := range homes {
				if contains(hs, u) && !got[k] {
					lost = true
				}
			}
			if lost {
				out = append(out, "extract "+u)
			}
		}
		return out
	}
	accept := func(kind string, us []string, pages map[string]tavily.Page) {
		for _, u := range us {
			if got[kind] {
				return
			}
			p, ok := pages[u]
			switch {
			case !ok:
				notes = append(notes, fmt.Sprintf("%s page could not be read: %s", kind, u))
			case errorPage(p.Title, p.RawContent):
				notes = append(notes, fmt.Sprintf("%s page is an error page: %s", kind, u))
			case !about(kind, p.URL, p.Title, p.RawContent):
				notes = append(notes, fmt.Sprintf("%s hit is not a %s page: %s", kind, kind, u))
			default:
				docs = append(docs, Doc{
					Source: finding.Source{URL: p.URL, Kind: kind, Title: p.Title, FetchedAt: at},
					Text:   capText(p.RawContent),
				})
				got[kind] = true
			}
		}
	}

	pages, err := read(urls)
	if err != nil {
		return nil, notes, failures(), credits, err
	}
	for _, k := range kinds {
		accept(k, want[k], pages)
	}

	// Links named for the kind on a homepage that could be read.
	var more []string
	links := map[string][]string{}
	for _, k := range kinds {
		for _, h := range homes[k] {
			p, ok := pages[h]
			if !ok || errorPage(p.Title, p.RawContent) {
				notes = append(notes, fmt.Sprintf("homepage could not be read, so no %s link could be followed: %s", k, h))
				continue
			}
			for _, l := range pageLinks(p.RawContent, host, k) {
				if len(links[k]) < extractTries && taken[l] == "" && !contains(urls, l) {
					taken[l] = k
					links[k] = append(links[k], l)
					more = append(more, l)
				}
			}
			if len(links[k]) == 0 {
				notes = append(notes, fmt.Sprintf("homepage names no %s link: %s", k, h))
			}
		}
	}
	if len(more) > 0 {
		pages, err := read(more)
		if err != nil {
			return docs, notes, failures(), credits, err
		}
		for _, k := range kinds {
			accept(k, links[k], pages)
		}
	}
	return docs, notes, failures(), credits, nil
}

var linkRE = regexp.MustCompile(`https?://[^\s<>()\[\]"'|]+`)

// pageLinks are the absolute links in a page's text that sit on host's
// registrable domain and whose path names kind, in page order, each once.
// With no official host there is nothing to stay on, so nothing is followed.
func pageLinks(text, host, kind string) []string {
	var out []string
	for _, l := range linkRE.FindAllString(text, -1) {
		l = strings.TrimRight(l, ".,;:")
		u, err := url.Parse(l)
		if err != nil || host == "" || registrable(l) != host {
			continue
		}
		if names(kind, u.Path) && !contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func hasKind(docs []Doc, kind string) bool {
	for _, d := range docs {
		if d.Source.Kind == kind {
			return true
		}
	}
	return false
}

func (g *Gatherer) storeDocs(ctx context.Context, a *appstore.App, at time.Time, out *Gathered, miss func(kind, why string)) {
	pages := g.ReviewPages
	if pages <= 0 {
		pages = appstore.MaxPages
	}
	reviews, err := g.Store.Reviews(ctx, a.ID, pages)
	if err != nil {
		// The feed has been dark for every app; the store page (T3.8) is
		// the fallback once it exists.
		miss(KindReview, "App Store review feed failed: "+err.Error())
		out.Failures = append(out.Failures, "review_feed")
	} else if len(reviews) == 0 {
		miss(KindReview, "App Store review feed returned no reviews")
	}
	for _, r := range reviews {
		out.Docs = append(out.Docs, Doc{
			Source: finding.Source{
				URL:       g.Store.FeedURL(a.ID, (r.Index-1)/appstore.PageSize+1),
				Kind:      KindReview,
				Title:     r.Title,
				Index:     r.Index,
				FetchedAt: at,
			},
			Text: r.Text(),
		})
	}
	if strings.TrimSpace(a.Description) != "" {
		out.Docs = append(out.Docs, Doc{
			Source: finding.Source{URL: a.StoreURL, Kind: KindStore, Title: a.Name + " on the App Store", FetchedAt: at},
			Text:   capText(a.Description),
		})
	}
}

// search runs one search through the meter, booked in thousandths of a
// credit. Tavily's reported number settles the charge.
func (g *Gatherer) search(ctx context.Context, ref string, s tavily.Search) ([]tavily.Result, float64, error) {
	var res []tavily.Result
	var used float64
	_, err := g.Meter.Spend(ctx, spend.PoolTavily, "tavily:search", ref, credits(tavily.SearchCredits(s.SearchDepth)),
		func(ctx context.Context) (cost.Usage, error) {
			r, c, err := g.Search.Search(ctx, s)
			if err != nil {
				return cost.Usage{}, err
			}
			res = r
			used = c.Used
			return cost.Usage{Price: credits(c.Used), Measured: c.Reported}, nil
		})
	return res, used, err
}

func (g *Gatherer) extract(ctx context.Context, ref string, urls []string) (tavily.Extracted, float64, error) {
	var ex tavily.Extracted
	var used float64
	_, err := g.Meter.Spend(ctx, spend.PoolTavily, "tavily:extract", ref, credits(tavily.ExtractCredits(len(urls))),
		func(ctx context.Context) (cost.Usage, error) {
			e, err := g.Search.ExtractWithFailures(ctx, urls)
			if err != nil {
				return cost.Usage{}, err
			}
			ex = e
			used = e.Credits.Used
			return cost.Usage{Price: credits(e.Credits.Used), Measured: e.Credits.Reported}, nil
		})
	return ex, used, err
}

// credits is a number of Tavily credits in the tavily pool's unit.
func credits(c float64) cost.Price {
	return cost.Price(math.Round(c * float64(spend.CreditUnit)))
}

// pickApp takes the first hit whose name starts with the title. No match
// means the title is not on the App Store, such as a console game, and a
// near miss is worse than no reviews.
func pickApp(apps []appstore.App, title string) *appstore.App {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return nil
	}
	for i := range apps {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(apps[i].Name)), t) {
			a := apps[i]
			return &a
		}
	}
	return nil
}

// hostFromHits finds the official host among the policy hits when the
// matched app's listing names none. A hit's registrable domain counts only
// when an App Store listing names that exact domain as its seller URL, and
// that listing is tied to the title:
//   - with an app matched by name, the listing has the same developer
//     (Minecraft's own listing has no URL, Minecraft Education by Mojang AB
//     names education.minecraft.net);
//   - with none, the listing's developer name is the domain's name and its
//     store name starts with the title's first word (Toca Boca AB names
//     tocaboca.com for "Toca Boca World"), and that listing becomes the app.
//
// The domain must be a listing's own, so a same-name domain on another TLD
// (minecraft.wiki, roblox.app, tocaboca.co.uk) or a look-alike name never
// passes: no listing names it.
func hostFromHits(hits []tavily.Result, apps []appstore.App, picked *appstore.App, title string) (string, *appstore.App) {
	want := appName(title)
	for _, h := range hits {
		dom := officialDomain(h.URL)
		if dom == "" {
			continue
		}
		for i := range apps {
			a := apps[i]
			if registrable(a.SellerURL) != dom {
				continue
			}
			if picked != nil {
				if strings.EqualFold(strings.TrimSpace(a.Seller), strings.TrimSpace(picked.Seller)) && a.Seller != "" {
					return dom, picked
				}
				continue
			}
			// With no picked app, the developer must be named for the
			// domain, and the listing must be the title itself, or the
			// developer named for the whole title, or a listing that opens
			// with the developer's full name where that name is more than
			// the title's first word ("Toca Boca World" by Toca Boca AB
			// for "Toca Life World"). A first word alone lets "Toca Ltd"
			// at toca.xyz pass for "Toca Boca World".
			dev := developer(a.Seller)
			if want != "" && dev == label(dom) && (appName(a.Name) == want || dev == want ||
				(dev != firstName(title) && strings.HasPrefix(appName(a.Name), dev) && firstName(a.Name) == firstName(title))) {
				return dom, &a
			}
		}
	}
	return "", nil
}

// appName folds a store name or title to its letters and digits, without a
// store subtitle ("Roblox: Play with friends" is "roblox").
// firstName is the folded first word of s.
func firstName(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return developer(f[0])
}

func appName(s string) string {
	for _, sep := range []string{":", " - ", " – ", " — "} {
		s, _, _ = strings.Cut(s, sep)
	}
	return developer(s)
}

// developer is a seller name reduced to letters and digits, without a
// trailing company form: "Toca Boca AB" is "tocaboca".
func developer(seller string) string {
	fs := strings.FieldsFunc(strings.ToLower(seller), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for len(fs) > 1 && companyForms[fs[len(fs)-1]] {
		fs = fs[:len(fs)-1]
	}
	return strings.Join(fs, "")
}

var companyForms = map[string]bool{
	"ab": true, "inc": true, "llc": true, "ltd": true, "limited": true, "corp": true, "corporation": true,
	"co": true, "gmbh": true, "sa": true, "sas": true, "bv": true, "oy": true, "ou": true, "plc": true, "pty": true,
	"company": true, "as": true, "aps": true, "srl": true, "spa": true, "kk": true,
}

// registrable is a URL's host without "www." and without subdomains: the
// last two labels, or three under a two-letter country's second level such
// as co.uk. It is what include_domains takes, so help.roblox.com matches
// roblox.com.
func registrable(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	dom, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."))
	if err != nil {
		return ""
	}
	return dom
}

// officialDomain is raw's registrable domain when it can be a title's
// official host, and "" when it cannot: a domain under a shared platform's
// suffix on the public suffix list's private section (foo.github.io,
// foo.blogspot.com), or a platform where anyone has a page
// (facebook.com/tocaboca, apps.apple.com). Narrowing to such a host would
// read the platform's own policy as the title's.
func officialDomain(raw string) string {
	dom := registrable(raw)
	if dom == "" || sharedPlatforms[dom] {
		return ""
	}
	if _, icann := publicsuffix.PublicSuffix(dom); !icann {
		return ""
	}
	return dom
}

var sharedPlatforms = map[string]bool{
	"apple.com": true, "google.com": true, "github.com": true, "github.io": true, "gitlab.io": true,
	"facebook.com": true, "instagram.com": true, "x.com": true, "twitter.com": true, "tiktok.com": true,
	"youtube.com": true, "linkedin.com": true, "reddit.com": true, "discord.com": true, "discord.gg": true,
	"medium.com": true, "substack.com": true, "wordpress.com": true, "blogspot.com": true, "wixsite.com": true,
	"squarespace.com": true, "notion.site": true, "linktr.ee": true, "itch.io": true, "fandom.com": true,
	"steampowered.com": true, "carrd.co": true, "weebly.com": true, "tumblr.com": true, "amazon.com": true,
}

// label is a registrable domain's name, "roblox" for roblox.com.
func label(dom string) string {
	name, _, _ := strings.Cut(dom, ".")
	return name
}

// candidates are the hits worth extracting for a policy or terms search:
// on the official host when there is one, not a homepage, not taken by an
// earlier kind, and those that name their kind first.
func candidates(hits []tavily.Result, kind, host string, taken map[string]string) []string {
	var named, other []string
	for _, h := range hits {
		if host != "" && registrable(h.URL) != host {
			continue
		}
		if homepage(h.URL) || taken[h.URL] != "" {
			continue
		}
		if names(kind, urlPath(h.URL)+" "+h.Title) {
			named = append(named, h.URL)
		} else {
			other = append(other, h.URL)
		}
	}
	c := append(named, other...)
	if len(c) > extractTries {
		c = c[:extractTries]
	}
	return c
}

func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Path
}

func homepage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.Trim(u.Path, "/") == "" && u.RawQuery == ""
}

var kindWords = map[string][]string{
	KindPolicy: {"privacy"},
	KindTerms:  {"terms", "eula", "conditions", "tos", "terms-of"},
}

func names(kind, s string) bool {
	s = strings.ToLower(s)
	for _, w := range kindWords[kind] {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// about reports whether an extracted page is the kind it was fetched as:
// its URL or title names the kind, or its first lines do. A download page
// with "privacy" in its footer is not a policy.
func about(kind, u, title, text string) bool {
	head := text
	if len(head) > 300 {
		head = head[:300]
	}
	path := u
	if pu, err := url.Parse(u); err == nil {
		path = pu.Path // a query string such as ?ref=privacy names nothing
	}
	return names(kind, path+" "+title+" "+head)
}

// errorPage reports whether a page Tavily read as a success is a
// not-found or error page. It goes by the page's signals, not its length:
// the title, or any line of the text, that is wholly an error heading
// ("Page Not Found | Common Sense Media", "Oops! Looks like that page
// doesn't exist."). The captured Common Sense 404 pages carry their heading
// after ~2400 characters of navigation and run 5.5k to 9k characters, so
// neither position nor size can decide. A sentence that only mentions a
// missing page has other words in it and is not a heading. Tavily reports
// no HTTP status for an extract, so there is no status signal to read.
func errorPage(title, text string) bool {
	if strings.TrimSpace(text) == "" {
		return true
	}
	for _, seg := range titleSep.Split(title, -1) {
		if errorHeading(seg) {
			return true
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if errorHeading(line) {
			return true
		}
	}
	return false
}

// errorHeading reports whether line is an error sign and nothing else but
// filler ("oops", "sorry", "404", "go home").
func errorHeading(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	if l == "" || len(l) > 120 {
		return false
	}
	l = strings.ReplaceAll(l, "’", "'")
	for _, sign := range errorSigns {
		i := strings.Index(l, sign)
		if i < 0 {
			continue
		}
		rest := l[:i] + " " + l[i+len(sign):]
		only := true
		for _, w := range strings.FieldsFunc(rest, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '\'') }) {
			if !errorFiller[w] {
				only = false
				break
			}
		}
		if only {
			return true
		}
	}
	return false
}

var titleSep = regexp.MustCompile(`\s[|–—-]\s`)

var errorFiller = map[string]bool{
	"oops": true, "whoops": true, "uh": true, "oh": true, "sorry": true, "error": true, "404": true, "410": true, "500": true,
	"go": true, "home": true, "back": true, "we're": true, "we": true, "are": true, "this": true, "the": true,
}

var errorSigns = []string{
	"looks like that page doesn't exist", "file not found",
	"page not found", "404 not found", "error 404", "404 error", "page doesn't exist", "page does not exist",
	"this page isn't available", "the page you requested could not be found",
}

// hitDoc turns one search hit into a document. A Reddit hit is its snippet,
// with the page chrome taken out; any other hit is its raw content when
// Tavily read the page, capped, and its snippet otherwise.
func hitDoc(kind string, r tavily.Result, at time.Time) (Doc, string) {
	text := r.RawContent
	if kind == KindReddit || strings.TrimSpace(text) == "" {
		text = r.Content
	}
	if kind == KindReddit {
		text = stripChrome(text)
	}
	if registrable(r.URL) == "commonsensemedia.org" {
		text = dropSummaries(text)
	}
	text = strings.TrimSpace(text)
	if errorPage(r.Title, text) {
		return Doc{}, fmt.Sprintf("%s hit is an error page or empty: %s", kind, r.URL)
	}
	return Doc{
		Source: finding.Source{URL: r.URL, Kind: kind, Title: r.Title, FetchedAt: at},
		Text:   capText(text),
	}, ""
}

// redditChrome is the page furniture Reddit's snippets carry, removed
// wherever it appears.
var redditChrome = []string{
	"Skip to main content", "Open menu", "Open navigation", "Go to Reddit Home", "Get App", "Get the Reddit app",
	"Log in to Reddit", "Log In", "Expand user menu", "Open settings menu", "Sign Up", "Advertise on Reddit",
	"A chip A close button",
}

func stripChrome(s string) string {
	for _, c := range redditChrome {
		s = replaceChrome(s, c, " ")
	}
	lines := strings.Split(s, "\n")
	keep := lines[:0]
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// replaceChrome replaces every exact match of old that is not the start of
// a longer word: "Log In" goes, "Log Ins" stays. Reddit glues its chrome to
// the text before and after ("Skip to main contentParents should..."), so
// only the end is checked.
func replaceChrome(s, old, repl string) string {
	var b strings.Builder
	for {
		j := strings.Index(s, old)
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := j + len(old)
		if end < len(s) && isLower(s[end]) {
			b.WriteString(s[:end])
		} else {
			b.WriteString(s[:j])
			b.WriteString(repl)
		}
		s = s[end:]
	}
}

func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

// dropSummaries removes Common Sense's generated "Parents say" and "Kids
// say" summaries, which no parent wrote and which must never be quoted as
// a parent's report.
func dropSummaries(s string) string {
	lines := strings.Split(s, "\n")
	keep := lines[:0]
	for _, l := range lines {
		t := strings.ToLower(strings.TrimLeft(l, " #*-\t"))
		if strings.HasPrefix(t, "parents say") || strings.HasPrefix(t, "kids say") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

// capText cuts a text to maxDocChars on a rune boundary.
func capText(s string) string {
	if len(s) <= maxDocChars {
		return s
	}
	cut := maxDocChars
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
