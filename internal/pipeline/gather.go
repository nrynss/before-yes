package pipeline

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
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
// so a change to Plan, the extract rules or the clean-up below must bump it.
const PlanVersion = "plan-2"

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
		}
	}
	out.App = pickApp(apps, title)
	if out.App != nil {
		if h := registrable(out.App.SellerURL); h != "" {
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
			if h, a := hostFromHits(res, apps, title); h != "" {
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

	// 3. One extract call for the policy and terms candidates.
	var urls []string
	kindOf := map[string]string{}
	for _, q := range plan {
		if !q.Extract {
			continue
		}
		c := candidates(hits[q.Name], q.Kind, out.Host, kindOf)
		if len(c) == 0 {
			miss(q.Kind, fmt.Sprintf("no %s page on the official host to read", q.Kind))
			continue
		}
		for _, u := range c {
			kindOf[u] = q.Kind
		}
		urls = append(urls, c...)
	}
	pages := map[string]tavily.Page{}
	if len(urls) > 0 {
		ex, used, err := g.extract(ctx, ref, urls)
		out.Credits += used
		if err != nil {
			return out, fmt.Errorf("gather: extract: %w", err)
		}
		for _, p := range ex.Pages {
			pages[p.URL] = p
		}
	}
	for _, q := range plan {
		if !q.Extract {
			continue
		}
		got := false
		for _, u := range urls {
			if kindOf[u] != q.Kind || got {
				continue
			}
			p, ok := pages[u]
			switch {
			case !ok:
				out.Notes = append(out.Notes, fmt.Sprintf("%s page could not be read: %s", q.Kind, u))
			case errorPage(p.RawContent):
				out.Notes = append(out.Notes, fmt.Sprintf("%s page is an error page: %s", q.Kind, u))
			case !about(q.Kind, p.URL, p.Title, p.RawContent):
				out.Notes = append(out.Notes, fmt.Sprintf("%s hit is not a %s page: %s", q.Kind, q.Kind, u))
			default:
				out.Docs = append(out.Docs, Doc{
					Source: finding.Source{URL: p.URL, Kind: q.Kind, Title: p.Title, FetchedAt: at},
					Text:   capText(p.RawContent),
				})
				got = true
			}
		}
		if !got && len(urls) > 0 && !missing[q.Kind] {
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

// hostFromHits finds the official host among the policy hits when the App
// Store named none. A hit's registrable domain counts only when its name,
// the label before the public suffix, equals the title's letters and digits
// or an App Store developer's name with its company suffix dropped. That is
// an exact match, so minecraftshop.com or roblox-help.net never pass. The
// App Store hit whose developer named the host is returned too, so a store
// name that fails the prefix rule, such as Toca's, still finds its reviews.
func hostFromHits(hits []tavily.Result, apps []appstore.App, title string) (string, *appstore.App) {
	want := squash(title)
	for _, h := range hits {
		dom := registrable(h.URL)
		name := label(dom)
		if name == "" {
			continue
		}
		for i := range apps {
			if name == developer(apps[i].Seller) {
				a := apps[i]
				return dom, &a
			}
		}
		if name == want {
			return dom, nil
		}
	}
	return "", nil
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

func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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
	ls := strings.Split(strings.ToLower(u.Hostname()), ".")
	if len(ls) < 2 {
		return ""
	}
	n := 2
	if len(ls) >= 3 && len(ls[len(ls)-1]) == 2 && secondLevel[ls[len(ls)-2]] {
		n = 3
	}
	if len(ls) < n {
		return ""
	}
	return strings.Join(ls[len(ls)-n:], ".")
}

var secondLevel = map[string]bool{"co": true, "com": true, "org": true, "net": true, "ac": true, "gov": true, "edu": true}

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
		if names(kind, h.URL+" "+h.Title) {
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
	return names(kind, u+" "+title+" "+head)
}

// errorPage reports a not-found or error page that Tavily returned as a
// success. Only a short page that says so near its top counts, so a policy
// that mentions "page not found" in passing is kept.
func errorPage(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return true
	}
	if len(t) > 6000 {
		return false
	}
	head := t
	if len(head) > 1500 {
		head = head[:1500]
	}
	for _, s := range errorSigns {
		if strings.Contains(head, s) {
			return true
		}
	}
	return false
}

var errorSigns = []string{
	"looks like that page doesn't exist", "looks like that page doesn’t exist",
	"page not found", "404 not found", "error 404", "page doesn't exist", "page does not exist",
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
	if errorPage(text) {
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
