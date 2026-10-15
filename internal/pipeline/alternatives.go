package pipeline

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

// Safer alternatives (T3.5).
//
// A card that is no or yes with settings gets up to MaxAlternatives
// candidates in the same genre for the same age, found with one Tavily
// search. Each candidate is checked like any title, through Collect (so it
// is cached like any title) and Decide, under its own ref, after the main
// card is made. Only a candidate whose verdict beats the original's is
// kept. The meter can refuse any of these calls; a refusal stops the run,
// marks the candidates not yet checked as refused, and never touches the
// main card.

// MaxAlternatives bounds how many candidates are checked for one card.
const MaxAlternatives = 3

// Alternative statuses.
const (
	// AltPending: found, not checked yet. Internal: a pending name is never
	// put on a card, since the cap may refuse it; a card shown before its
	// alternatives finish carries none.
	AltPending = "pending"
	// AltChecked: checked, with a verdict that beats the original's.
	AltChecked = "checked"
	// AltRefused: not checked, because the meter refused a call or the
	// checks_daily cap refused a fresh check. Kept internally; a card does
	// not show refused names (user decision), only MoreAlternativesNote.
	AltRefused = "refused"
)

// MoreAlternativesNote is the one line a card shows, in place of the
// refused alternatives' names, when at least one was refused.
const MoreAlternativesNote = "More alternatives when today's checks reset"

// Checks is the checks_daily cap: BeginCheck reserves one fresh check and
// refuses with cost.ErrOverBudget when the day has none left; the caller
// ends it with Done or Fail. *spend.Meter has it.
type Checks interface {
	BeginCheck(ctx context.Context, ref string) (*spend.Check, error)
}

// wantsAlternatives reports whether a verdict gets alternatives.
func wantsAlternatives(v checklist.Verdict) bool {
	return v == checklist.No || v == checklist.YesWithSettings
}

// rank orders verdicts for "beats". Unclear never beats anything: it says
// nothing is known, which is not safer.
func rank(v checklist.Verdict) int {
	switch v {
	case checklist.Yes:
		return 3
	case checklist.YesWithSettings:
		return 2
	case checklist.No:
		return 1
	}
	return 0
}

func beats(alt, orig checklist.Verdict) bool {
	return alt != checklist.Unclear && rank(alt) > rank(orig)
}

// alternativesSearch is the one Tavily search that finds candidates.
func alternativesSearch(card Card) tavily.Search {
	genre := "app"
	switch {
	case card.Kind == KindYouTube:
		genre = "YouTube channel"
	case card.Kind == KindConsole:
		genre = "video game"
	case card.App != nil && card.App.Genre != "":
		genre = strings.ToLower(card.App.Genre)
	}
	return tavily.Search{
		Query:      fmt.Sprintf("safer %s like %s for a %d year old", genre, card.Title, card.Age),
		MaxResults: 8, IncludeRaw: true,
	}
}

var (
	listicle = regexp.MustCompile(`(?i)^(the )?(\d+|best|top|safe|safer|good|great|apps?|games?|channels?)\b|\b(like|alternatives?|vs\.?)\b`)
	// siteSuffix is the tail a site puts on its own page titles. Only these
	// are cut; a ": " is kept, so "Animal Crossing: New Horizons" stays
	// whole.
	siteSuffix = regexp.MustCompile(`(?i)(\s+[|–—-]\s+.*|\s+on the app store|\s+on steam|\s+(app|game|tv|website|youtube)?\s*review)$`)
	// rankedLine is a list page's numbered entry on its own line:
	// "1. Minecraft", "2) **Toca Life World**", "**3) PBS Kids Games**".
	// bitdefender's picks are bold around the rank and the name together.
	rankedLine = regexp.MustCompile(`(?im)^[ \t]*(?:\*\*)?[ \t]*(?:no\.?[ \t]*)?\d{1,2}[.):][ \t]+(.+)[ \t]*$`)
	// rankedHeading is a numbered entry as a heading, the way blog lists mark
	// a pick: "### 1. Mysh — collaborative storytelling", "## No. 4: ABC
	// Mouse". The heading marks the name; the rank orders the list.
	rankedHeading = regexp.MustCompile(`(?im)^[ \t]*#{1,4}[ \t]+(?:\*\*)?[ \t]*(?:no\.?[ \t]*)?\d{1,2}[.):][ \t]+(.+)[ \t]*$`)
	// markedHeading is an unnumbered heading that is only a link or bold
	// text, the way Common Sense Media's "Games Like Minecraft" marks each
	// pick: "### [Lego Worlds](url)".
	markedHeading = regexp.MustCompile(`(?m)^[ \t]*#{2,4}[ \t]+(\*\*[^*]+\*\*|\[[^\]]+\]\([^)]*\))[ \t]*$`)
	// tableRow is a table's row whose first cell is bold, the way
	// kidsaitools' comparison lays its picks out: "| **Toca Builders** | 4-7
	// | ... |". The whole row is captured, so a header row (every cell bold,
	// the way blog.khanacademy.org's "| **App** | **Ages** | **Cost** |")
	// can be told from a pick's row.
	tableRow = regexp.MustCompile(`(?m)^\|[ \t]*\*\*([^*]+)\*\*.*$`)
	// markedLead is a name marked as a title at the start of an entry: a
	// bold span or a link, as list pages mark each pick ("**Minecraft** -
	// great for builders", "[Toca Life World](url)", "[**Toca**](url)").
	markedLead = regexp.MustCompile(`^(?:\*\*([^*]+)\*\*|__([^_]+)__|\[([^\]]+)\]\([^)]*\))`)
	// mdLink, mdEmph and mdCode are markdown around an entry's name.
	mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdEmph = regexp.MustCompile(`\*{1,2}([^*]+)\*{1,2}|__([^_]+)__`)
	mdCode = regexp.MustCompile("`([^`]*)`")
	// entryLink is a named link inside an entry, whose address can evidence
	// the name: "[Blox 3D Junior](https://www.commonsensemedia.org/app-reviews/blox-3d-junior)".
	entryLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	// leadRank is the rank a lead carries when a page bolds the rank and the
	// name together: "1) Theme Park Tycoon 2".
	leadRank = regexp.MustCompile(`(?i)^[ \t]*(?:no\.?[ \t]*)?\d{1,2}[.):][ \t]+`)
	// genericTail is a collection's name, not a title's: hit titles like
	// "Teacher-Recommended Apps" name a page, not an app.
	genericTail = regexp.MustCompile(`(?i)\b(apps?|games?|channels?|websites?|sites?|picks?)$`)
)

// genericWhole is a name that is only a collection word: a table column or a
// section label ("App", "Games", "Picks"), never a title. A real title may
// end in one ("PBS Kids Games" on the captured Khan blog table); it is never
// only one.
var genericWhole = map[string]bool{
	"app": true, "apps": true, "game": true, "games": true,
	"channel": true, "channels": true, "website": true, "websites": true,
	"site": true, "sites": true, "pick": true, "picks": true,
}

// genericName reports whether a list entry's whole name is such a collection
// word.
func genericName(name string) bool {
	return genericWhole[nameFold(name)]
}

// minorWords are the closed-class words a title does not capitalise: "Build
// A Boat for Treasure", "Hide and Seek Extreme", "Work at a Pizza Place",
// "The Robot Factory by Tinybop".
var minorWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"nor": true, "for": true, "by": true, "to": true, "of": true, "in": true,
	"on": true, "at": true, "with": true, "from": true, "as": true, "vs": true,
}

// titleShape reports whether a multi-word name reads as a title rather than
// a sentence: every word starts with a capital (or a digit, or is one of the
// small words). The captured picks are all titled that way ("Theme Park
// Tycoon 2", "Pango Build Safari", "ELSA Speak"); the advice steps an
// advice page bolds under a list's title are sentences — "Set limits before
// bed", "Disable chat", "Turn on parental controls" — and are not titles.
// A name that is one word ("Duolingo") or starts with no letter ("3D
// Blox") is left to the other checks.
//
// Recorded residual (round-2 L4): the same steps in title case — "Set
// Limits Before Bed", "Disable In-App Chat" — pass this check. A title-cased
// noun phrase is shape-identical to a pick ("Build A Boat for Treasure",
// "Hide and Seek Extreme"): every word capitalised, no minor word, no other
// signal on the line. No shape check separates them, and the verb stoplist
// that once tried was removed in T3.5 round 3. No list-titled capture
// produces the shape — every bolded advice step they carry is sentence-cased
// — and the one capture with title-shaped bold labels (educational-games.
// online's numbered entries) never arms E2 at all: its emoji-led title is
// no list's. So the limit is pinned, not hidden:
// TestTitleCasedAdviceStepsAreAdmitted.
func titleShape(name string) bool {
	words := strings.Fields(name)
	if len(words) <= 1 {
		return true
	}
	for _, w := range words {
		if minorWords[strings.ToLower(strings.Trim(w, ".,;:!?'\""))] {
			continue
		}
		r := []rune(w)[0]
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// entryRepeatWindow is how far after an entry a link to it must repeat for
// the link to evidence the name. A list page links a pick twice, its card
// and its read-more link, a few hundred characters apart; a menu or a table
// of contents links a page once.
const entryRepeatWindow = 600

// maxListEntries bounds how many names one list page can add.
const maxListEntries = MaxAlternatives

// listEntry is one entry a page's layout locates as naming a title.
type listEntry struct {
	pos    int    // where the entry sits in the page text
	raw    string // the entry's own line, links included
	name   string // the text the locator captured, unvetted
	marked bool   // the name is bold, linked, or a heading
	ranked bool   // the entry is numbered
	table  bool   // the name is a table row's first cell
}

// cleanName strips invisible format characters (the App Store puts U+200E
// before its page titles) and spaces.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// candidateName is the title a search hit names, or "" when the hit is a
// list rather than one title: "Toca Life World App Review | Common Sense
// Media" names "Toca Life World"; "10 Best Games Like Roblox" names none.
func candidateName(hitTitle string) string {
	name := siteName(hitTitle)
	if name == "" || listicle.MatchString(name) {
		return ""
	}
	return name
}

// siteName is a page title with invisible characters and known site
// suffixes removed. A list entry uses it without the list-title check, so a
// marked "Top Eleven" stays a name.
func siteName(hitTitle string) string {
	name := cleanName(hitTitle)
	for {
		cut := strings.TrimSpace(siteSuffix.ReplaceAllString(name, ""))
		if cut == name {
			break
		}
		name = cut
	}
	return name
}

// stripMarkdown takes the name out of an entry's markdown: links, bold,
// italics, code, leading heading marks and trailing punctuation.
func stripMarkdown(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdEmph.ReplaceAllString(s, "$1$2")
	s = mdCode.ReplaceAllString(s, "$1")
	s = strings.TrimLeft(strings.TrimSpace(s), "# ")
	return strings.TrimRight(strings.TrimSpace(s), ".,;:!*_ ")
}

// listNames are the titles a page names, in page order. The page's layout
// locates an entry: a ranked line or heading ("1. Minecraft", "### 1. Mysh —
// …"), a heading that is only a link or bold text ("### [Lego Worlds](url)"),
// or a table row whose first cell is bold. Markup only locates; something
// must evidence the name:
//   - the entry links to the name's own page, and the link repeats as a
//     read-more link: on its own line, under a read-more label ("See full
//     review"), after the pick's blurb (a pick is linked twice, its card
//     and its read-more link; a menu link names once, and a navigation
//     repeat is inline, reuses the label, or labels its destination),
//   - the page's own title is a list's ("10 Best Games Like Roblox"), and the
//     entry is ranked and marked, or a table's name cell,
//   - another hit in the same search response is titled with the name.
//
// A site or platform's own name is never a name. Questions, collection
// words ("App", "Games", "Picks"), sentence-shaped names ("Set limits
// before bed") and names over six words are dropped. Repeats on the page
// and names skip rejects (the title itself, names already taken) are dropped
// before the bound is applied, so they use no slot; at most maxListEntries
// are returned.
func listNames(h tavily.Result, skip, known func(string) bool) []string {
	text := h.RawContent
	if strings.TrimSpace(text) == "" {
		text = h.Content
	}
	listPage := listicle.MatchString(cleanName(h.Title))
	var out []string
	own := map[string]bool{}
	for _, e := range pageEntries(text) {
		name := entryName(e)
		if !plausibleName(name) || genericName(name) || !titleShape(name) {
			continue
		}
		if !evidenced(e, text, name, listPage, known) {
			continue
		}
		if isSiteName(name, h.Title, h.URL) {
			continue
		}
		if own[appName(name)] || (skip != nil && skip(name)) {
			continue
		}
		own[appName(name)] = true
		out = append(out, name)
		if len(out) == maxListEntries {
			break
		}
	}
	return out
}

// pageEntries locates a page's entries, in page order: ranked lines, ranked
// headings, headings that are only a link or bold text, and bold table name
// cells.
func pageEntries(text string) []listEntry {
	type at struct {
		pos int
		e   listEntry
	}
	var found []at
	add := func(re *regexp.Regexp, ranked, marked, table bool, group int) {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			e := listEntry{
				pos:    m[0],
				raw:    text[m[0]:m[1]],
				name:   text[m[group*2]:m[group*2+1]],
				marked: marked,
				ranked: ranked,
				table:  table,
			}
			found = append(found, at{m[0], e})
		}
	}
	add(rankedLine, true, false, false, 1)
	add(rankedHeading, true, true, false, 1)
	add(markedHeading, false, true, false, 1)
	add(tableRow, false, true, true, 1)
	sort.SliceStable(found, func(i, j int) bool { return found[i].pos < found[j].pos })
	entries := make([]listEntry, 0, len(found))
	for _, f := range found {
		e := f.e
		// A table's header row names the page's columns, not a pick: the
		// captured headers either bold every cell ("| **App** | **Ages** |
		// **Cost** |"), or the table's delimiter row follows. A pick's row
		// bolds only its name cell.
		if e.table && (boldHeaderRow(e.raw) || delimiterFollows(text, e.pos+len(e.raw))) {
			continue
		}
		// A lead marked on the whole line ("**1) Theme Park Tycoon 2**") or
		// at the start of the entry ("3) [Quiet Garden](u) - grow plants")
		// is the name, and marks the entry.
		if m := markedLead.FindStringSubmatch(e.raw); m != nil {
			e.name = m[1] + m[2] + m[3]
			e.marked = true
		} else if m := markedLead.FindStringSubmatch(e.name); m != nil {
			e.name = m[1] + m[2] + m[3]
			e.marked = true
		}
		entries = append(entries, e)
	}
	return entries
}

// columnLabels are the folds of the captured tables' header cells: the
// comparison tables in the captures head themselves with these labels
// (blog.khanacademy.org's "| **App** | **Ages** | **Cost** | **Offline** |
// **Best For** |", educational-games.online's "| **App** | **Best For** |
// **Why It’s Great** | **Link** |", kidsaitools' "| Game | Age Fit |
// Chat/Social | In-App Purchases | First Success | Best For |"). A row of
// them names the page's columns, never a pick.
var columnLabels = map[string]bool{
	"app": true, "ages": true, "cost": true, "offline": true, "bestfor": true,
	"whyitsgreat": true, "link": true, "game": true, "agefit": true,
	"chatsocial": true, "inapppurchases": true, "firstsuccess": true,
}

// cellBold reports whether a table cell is one bold span: "**Ages**".
func cellBold(cell string) bool {
	return len(cell) >= 4 && strings.HasPrefix(cell, "**") &&
		strings.HasSuffix(cell, "**") && !strings.Contains(cell[2:len(cell)-2], "**")
}

// boldHeaderRow reports whether a table row is the table's header, which
// names the page's columns rather than a pick. Two shapes — the first read
// off the captures, the second the rule's first-cell-only form (round-2 L5's
// probe; no capture carries it):
//   - every cell bold: the Khan blog heads its comparison with
//     "| **App** | **Ages** | **Cost** | **Offline** | **Best For** |";
//   - the first cell bold and every later cell a captured column label
//     ("| **Best For** | Ages | Cost |"): the same header with only its
//     first label bolded.
//
// A pick's row bolds its name cell alone, and its later cells are that
// pick's values — "| **PBS Kids Games** | 2–8 | Free | Yes | …" — so a
// bolded badge in a later cell ("| **Toca Builders** | **Best overall** |
// 4-7 | …") or a value word ("Free", "4-7") keeps the row a pick. A header
// that bolds no cell never becomes an entry (tableRow reads bold first
// cells only), and one with unrecorded labels is still caught by
// delimiterFollows when the table marks it markdown's way.
func boldHeaderRow(raw string) bool {
	var cells []string
	for _, c := range strings.Split(raw, "|") {
		if c = strings.TrimSpace(c); c != "" {
			cells = append(cells, c)
		}
	}
	if len(cells) == 0 || !cellBold(cells[0]) {
		return false
	}
	all, labels := true, true
	for _, c := range cells[1:] {
		if !cellBold(c) {
			all = false
		}
		if !columnLabels[nameFold(c)] {
			labels = false
		}
	}
	return all || labels
}

// delimiterFollows reports whether a markdown table's column separator — a
// line of only pipes, dashes, colons and spaces ("| --- | --- |") — follows
// the row that ends at end. In markdown that separator marks the row above
// it as the table's header; both captured tables that carry one put it
// directly under their header rows, and no captured data row is followed by
// one. A blank line first is no separator.
func delimiterFollows(text string, end int) bool {
	rest := text[end:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return false
	}
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	dash, pipe := false, false
	for _, r := range rest {
		switch {
		case r == ' ' || r == '\t' || r == '\r' || r == ':':
		case r == '-':
			dash = true
		case r == '|':
			pipe = true
		default:
			return false
		}
	}
	return dash && pipe
}

// entryName is an entry's name: the captured text with markdown, a lead
// rank, a subtitle ("TruPlay: for Christian families" is "TruPlay"), and
// site tails stripped.
func entryName(e listEntry) string {
	entry := stripMarkdown(e.name)
	if m := leadRank.FindString(entry); m != "" {
		entry = strings.TrimSpace(entry[len(m):])
	}
	entry = cutSubtitle(entry)
	entry, _, _ = strings.Cut(entry, " - ")
	entry, _, _ = strings.Cut(entry, " – ")
	entry, _, _ = strings.Cut(entry, " (")
	return stripMarkdown(siteName(entry))
}

// cutSubtitle cuts an entry's ": " tail when it reads as a description, the
// way blog lists title their picks ("PBS KIDS Games: for preschool and early
// grades"). A subtitle that starts a name keeps its colon:
// "Minecraft: Education Edition" stays whole.
func cutSubtitle(entry string) string {
	i := strings.Index(entry, ": ")
	if i < 0 {
		return entry
	}
	rest := strings.ToLower(strings.TrimSpace(entry[i+2:]))
	if w := strings.Fields(rest); len(w) > 0 && subtitleOpeners[w[0]] {
		return strings.TrimSpace(entry[:i])
	}
	return entry
}

var subtitleOpeners = map[string]bool{
	"for": true, "with": true, "and": true, "your": true, "how": true,
	"why": true, "what": true, "our": true, "by": true, "from": true, "to": true,
}

// evidenced reports whether something beyond the entry's markup names it: a
// link to the name's own page that repeats (E1), the page's own list title
// behind a ranked, marked entry (E2), or another hit titled with the name
// (E3, the T3.5 rule, kept as one form of evidence).
func evidenced(e listEntry, text, name string, listPage bool, known func(string) bool) bool {
	if slugEvidence(e, text, name) {
		return true
	}
	if listPage && (e.ranked || e.table) && e.marked {
		return true
	}
	return known != nil && known(name)
}

// readMoreLabels are the folds of the labels a list page puts on a pick's
// read-more link. The captured evidence is Common Sense Media's: every one
// of its 20 picks repeats its address as "[See full review](…)", on its own
// line, after the blurb. The set holds that phrase and its plain variants
// ("Read the review", "Read our review") — labels about reading the review.
// A navigation repeat labels its destination instead ("Full schedule", the
// page's own name), even on its own line with a blurb between (round-2 L6).
// "Read more" and "See more" stay out: they are the web's generic link
// labels, navigation included. No capture carries one as a whole label —
// samiolearning's "Read more" appearances are tails of longer link labels,
// never a label alone.
var readMoreLabels = map[string]bool{
	"seefullreview": true, "readfullreview": true, "fullreview": true,
	"readthereview": true, "seethereview": true, "readourreview": true,
	"seeourreview": true, "readreview": true, "seereview": true,
}

// slugEvidence reports whether the entry links to a page whose address is
// the name (Common Sense Media's "### [Blox 3D Junior](…/app-reviews/blox-3d-junior)")
// and the link repeats as a read-more link. On the captured page every pick
// carries its blurb and then the same address once more, as its own line,
// under a read-more label: "age 5+", "Kids build block models in
// challenging creative app.", then "[See full review](…)". A navigation
// repeat fails one of those: it sits inside a line of links or text, it
// repeats the page's own label, its label names its destination rather than
// the review ("Full schedule", round-2 L6), or it follows no text at all.
func slugEvidence(e listEntry, text, name string) bool {
	fold := nameFold(name)
	if fold == "" {
		return false
	}
	lead := nameFold(e.name)
	end := e.pos + len(e.raw) + entryRepeatWindow
	if end > len(text) {
		end = len(text)
	}
	for _, m := range entryLink.FindAllStringSubmatch(e.raw, -1) {
		slug := slugFold(m[2])
		if slug == "" || slug != fold && slug != lead {
			continue
		}
		at := strings.Index(text[e.pos:end], m[2])
		if at < 0 {
			continue
		}
		rest := text[e.pos+at+len(m[2]) : end]
		next := strings.Index(rest, m[2])
		if next < 0 || !strings.HasPrefix(rest[next+len(m[2]):], ")") {
			continue // the address does not repeat as a link's own address
		}
		lineStart := strings.LastIndex(rest[:next], "\n") + 1
		prefix := rest[lineStart:next]
		// The repeat is a link that owns its line: "[See full review](".
		if len(prefix) < 4 || prefix[0] != '[' || prefix[len(prefix)-2] != ']' || prefix[len(prefix)-1] != '(' {
			continue
		}
		// The label is about reading the review ("See full review"). A
		// repeat of the page's own label, or a label naming the
		// destination ("Full schedule", round-2 L6), is navigation,
		// however the link sits on the page.
		if a := nameFold(prefix[1 : len(prefix)-2]); a == fold || a == lead || !readMoreLabels[a] {
			continue
		}
		if !blurbBetween(rest[:lineStart]) {
			continue
		}
		return true
	}
	return false
}

// blurbBetween reports whether s holds a line of plain text: on the captured
// pages a pick's age line and description stand between its card link and
// its read-more link, while a menu's links sit next to each other with no
// text at all. A line with link markup in it is another link, not a blurb.
func blurbBetween(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 4 || strings.Contains(line, "](") {
			continue
		}
		if strings.IndexFunc(line, unicode.IsLetter) >= 0 {
			return true
		}
	}
	return false
}

// nameFold is a name or an address slug folded to its lowercase letters and
// digits: "RaceCraft - Build & Race" and "racecraft-build-race" fold alike.
func nameFold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// slugFold is a link's last path segment, folded.
func slugFold(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Path == "" {
		return ""
	}
	seg := u.Path
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	return nameFold(seg)
}

// platformNames is the second net behind the domain check: the fold of a
// platform or shared-site name is never a candidate.
var platformNames = map[string]bool{
	"medium": true, "reddit": true, "facebook": true, "youtube": true,
	"quora": true, "pinterest": true, "wikipedia": true, "tumblr": true,
	"instagram": true, "tiktok": true, "twitter": true, "discord": true,
	"substack": true, "mumsnet": true,
}

// isSiteName reports whether a candidate is the publishing site's own name
// rather than a title: the hit URL's registrable domain and its label
// ("Medium" on sa-liberty.medium.com, "Samio Learning" on samiolearning.com,
// "Mysh" inside playmysh.com), a known platform, or the brand in the page
// title's tail ("… 8 Picks (2026) | Mysh Blog"). The domain rule is one-way:
// a candidate is the site when its name is the label or sits inside it. A
// name that merely contains the label is a title the site shares letters
// with — "Duolingo Kids" on lingokids.com, "Minecraft Dungeons" on
// minecraft.net — and stays a candidate.
func isSiteName(name, hitTitle, hitURL string) bool {
	fold := nameFold(name)
	if fold == "" {
		return true
	}
	if platformNames[fold] {
		return true
	}
	if dom := registrable(hitURL); dom != "" {
		d := nameFold(label(dom))
		if d != "" && (fold == d || len(fold) >= 4 && len(d) >= 4 && strings.Contains(d, fold)) {
			return true
		}
	}
	segs := titleSep.Split(cleanName(hitTitle), -1)
	if len(segs) > 1 {
		tail := segs[len(segs)-1]
		words := strings.Fields(strings.ToLower(tail))
		for i := range words {
			for j := i + 1; j <= len(words); j++ {
				if fold == nameFold(strings.Join(words[i:j], " ")) {
					return true
				}
			}
		}
	}
	return false
}

// plausibleName is the cheap check, with no call, that a name could be a
// title before it costs a Collect: not empty, not a question, at most six
// words, and starting with a capital letter or a digit.
func plausibleName(name string) bool {
	if name == "" || strings.Contains(name, "?") || len(strings.Fields(name)) > 6 {
		return false
	}
	r := []rune(name)[0]
	return unicode.IsUpper(r) || unicode.IsDigit(r)
}

// sameTitle reports whether a hit is about the title itself in any form:
// its words, in order, inside the hit's ("Roblox on the App Store",
// "Roblox Parents Guide", "Is Roblox Safe for Kids?").
func sameTitle(hitText, title string) bool {
	words := func(s string) string {
		return " " + strings.Join(strings.FieldsFunc(strings.ToLower(cleanName(s)), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}), " ") + " "
	}
	t := words(title)
	return strings.TrimSpace(t) != "" && strings.Contains(words(hitText), t)
}

// FindAlternatives runs the search and returns up to MaxAlternatives
// candidates, pending. A card that does not want alternatives gets none and
// no search. The search is metered under ref, and asks for page text so a
// list page's entries can be read without another call.
func (p *Pipeline) FindAlternatives(ctx context.Context, card Card, ref string) ([]Alternative, error) {
	if !wantsAlternatives(card.Verdict) {
		return nil, nil
	}
	if p.Gatherer == nil {
		return nil, errors.New("pipeline: a gatherer is required")
	}
	hits, _, err := p.Gatherer.search(ctx, ref, alternativesSearch(card))
	if err != nil {
		return nil, fmt.Errorf("alternatives: search: %w", err)
	}
	seen := map[string]bool{}
	var out []Alternative
	taken := func(name string) bool {
		key := appName(name)
		return name == "" || key == "" || seen[key] || sameTitle(name, card.Title)
	}
	add := func(name, reason string) bool {
		if taken(name) {
			return false
		}
		key := appName(name)
		seen[key] = true
		out = append(out, Alternative{Title: name, Reason: reason, Status: AltPending})
		return len(out) == MaxAlternatives
	}
	// titled is every single-title name in this response, for list entries
	// with no other evidence (the T3.5 corroboration, kept as one form).
	titled := map[string]bool{}
	for _, h := range hits {
		if name := candidateName(h.Title); name != "" {
			titled[appName(name)] = true
		}
	}
	known := func(name string) bool { return titled[appName(name)] }
	for _, h := range hits {
		site := registrable(h.URL)
		if name := candidateName(h.Title); name != "" {
			switch {
			case sameTitle(h.Title, card.Title):
				continue // the title's own page, guide or safety article
			case !plausibleName(name):
				continue // a question post, a headline, an unpunctuated thread
			case genericTail.MatchString(name):
				continue // "Teacher-Recommended Apps" names a page, not a title
			case isSiteName(name, h.Title, h.URL):
				continue // "Medium": the publishing site's own name
			}
			if add(name, "Suggested by "+site+": "+cleanName(h.Title)) {
				break
			}
			continue
		}
		full := false
		for _, name := range listNames(h, taken, known) {
			if full = add(name, "Listed by "+site+": "+cleanName(h.Title)); full {
				break
			}
		}
		if full {
			break
		}
	}
	return out, nil
}

// AltResult is the outcome of checking a card's candidates.
type AltResult struct {
	// Alternatives are what a card may show: only the candidates checked
	// to beat the original. A refused name is never here (user decision).
	Alternatives []Alternative
	// Note is MoreAlternativesNote when at least one candidate was refused.
	Note string
	// outcomes is every candidate's outcome, refused ones included, in
	// order. It stays inside the package.
	outcomes []Alternative
	// Cards are the kept alternatives' own cards, to store beside the
	// main card so each alternative links to its card.
	Cards []Card
	// Refused reports that the meter refused a call (the run stopped) or
	// the checks_daily cap refused a fresh check.
	Refused bool
}

// CheckAlternatives checks each pending candidate for the card's age, in
// order, under ref + "/alt<n>". A candidate that fails for any other
// reason is dropped and the run goes on; a budget refusal stops it.
//
// User decision: each fresh (uncached) candidate check counts as one check
// against the checks_daily cap, through p.Checks (BeginCheck, then Done
// when it ends with a card or Fail when it does not). A candidate whose
// evidence is cached runs free and reserves nothing. When the cap refuses,
// that candidate is refused and so is every later fresh one; later cached
// ones still run.
func (p *Pipeline) CheckAlternatives(ctx context.Context, card Card, pending []Alternative, ref string) AltResult {
	var res AltResult
	capped := false
	for i, alt := range pending {
		if res.Refused && !capped {
			alt.Status = AltRefused
			res.outcomes = append(res.outcomes, alt)
			continue
		}
		altRef := fmt.Sprintf("%s/alt%d", ref, i+1)
		var hold *spend.Check
		if p.Checks != nil && !p.cached(ctx, alt.Title) {
			var err error
			if !capped {
				hold, err = p.Checks.BeginCheck(ctx, altRef)
			}
			if capped || err != nil {
				if capped || errors.Is(err, cost.ErrOverBudget) {
					capped, res.Refused = true, true
					alt.Status = AltRefused
					res.outcomes = append(res.outcomes, alt)
				}
				continue
			}
		}
		ev, _, err := p.Collect(ctx, alt.Title, altRef)
		var c Card
		if err == nil {
			c, err = p.Decide(ctx, ev, card.Age, altRef)
		}
		if hold != nil {
			if err == nil {
				err = hold.Done()
			} else {
				_ = hold.Fail()
			}
		}
		if errors.Is(err, cost.ErrOverBudget) {
			res.Refused = true
			capped = false
			alt.Status = AltRefused
			res.outcomes = append(res.outcomes, alt)
			continue
		}
		if err != nil || !beats(c.Verdict, card.Verdict) {
			continue
		}
		alt.CardID, alt.Verdict, alt.Status = c.ID, c.Verdict, AltChecked
		res.outcomes = append(res.outcomes, alt)
		res.Cards = append(res.Cards, c)
	}
	res.Alternatives, res.Note = shown(res.outcomes)
	return res
}

// cached reports whether the evidence cache holds a title's evidence, so
// its check is not fresh.
func (p *Pipeline) cached(ctx context.Context, title string) bool {
	if p.Cache == nil || p.Gatherer == nil || p.Reader == nil || p.Reader.Model == nil {
		return false
	}
	storefront, err := storefrontOf(p.Gatherer.Store)
	if err != nil {
		return false
	}
	_, ok, err := p.Cache.Get(ctx, EvidenceKey(strings.Join(strings.Fields(title), " "), storefront, p.Reader.Model.Settings().Model))
	return err == nil && ok
}

// shown is what a card exposes of its alternatives: only the checked ones,
// never a refused or pending name, and MoreAlternativesNote when at least
// one was refused. Both hooks, and Card's JSON encoding, use it.
func shown(alts []Alternative) ([]Alternative, string) {
	out := []Alternative{}
	note := ""
	for _, a := range alts {
		switch a.Status {
		case AltChecked:
			out = append(out, a)
		case AltRefused:
			note = MoreAlternativesNote
		}
	}
	return out, note
}

// Apply returns a copy of the card carrying the result's alternatives and
// note. It is the second half of the two-step hook: FindAlternatives, then
// CheckAlternatives, then Apply.
func (r AltResult) Apply(card Card) Card {
	out := card
	out.Alternatives = append([]Alternative{}, r.Alternatives...)
	out.AlternativesNote = r.Note
	return out
}

// WithAlternatives finds and checks a card's alternatives and returns a
// copy of the card carrying them, with the alternatives' own cards. The
// main card is made before this runs and is complete without it: on any
// error, including a refused search, the card comes back as it was, with
// no alternatives.
func (p *Pipeline) WithAlternatives(ctx context.Context, card Card, ref string) (Card, []Card, error) {
	pending, err := p.FindAlternatives(ctx, card, ref)
	if err != nil || len(pending) == 0 {
		return card, nil, err
	}
	res := p.CheckAlternatives(ctx, card, pending, ref)
	return res.Apply(card), res.Cards, nil
}
