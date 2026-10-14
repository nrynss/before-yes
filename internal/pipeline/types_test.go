package pipeline

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

var update = flag.Bool("update", false, "rewrite testdata/contracts from the fixtures")

const contracts = "../../testdata/contracts"

var examples = []string{"evidence.json", "card-yes.json", "card-settings.json", "card-no.json", "card-unclear.json"}

// decodeStrict decodes with unknown fields refused.
func decodeStrict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func target(name string) any {
	if name == "evidence.json" {
		return &Evidence{}
	}
	return &Card{}
}

// Every example decodes with no unknown field and re-encodes to the same JSON.
func TestExamplesRoundTrip(t *testing.T) {
	for _, name := range examples {
		b, err := os.ReadFile(filepath.Join(contracts, name))
		if err != nil {
			t.Fatal(err)
		}
		v := target(name)
		if err := decodeStrict(b, v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var a, c any
		json.Unmarshal(b, &a)
		json.Unmarshal(out, &c)
		if !reflect.DeepEqual(a, c) {
			t.Errorf("%s: round trip lost data\nfile: %s\ngot:  %s", name, compact(b), out)
		}
	}
}

func compact(b []byte) string {
	var buf bytes.Buffer
	json.Compact(&buf, b)
	return buf.String()
}

// jsonPaths lists every JSON member path a type can produce, such as
// "grades[].setting".
func jsonPaths(t reflect.Type, prefix string, out map[string]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		if t.Kind() == reflect.Slice {
			prefix += "[]"
		}
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == reflect.TypeOf(time.Time{}) {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			panic("field " + t.Name() + "." + f.Name + " has no json name")
		}
		p := name
		if prefix != "" {
			p = prefix + "." + name
		}
		out[p] = true
		jsonPaths(f.Type, p, out)
	}
}

func valuePaths(v any, prefix string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out[p] = true
			valuePaths(e, p, out)
		}
	case []any:
		for _, e := range x {
			valuePaths(e, prefix+"[]", out)
		}
	}
}

// Adding a field to a contract type without showing it in an example fails here.
func TestExamplesCoverEveryField(t *testing.T) {
	for typ, files := range map[reflect.Type][]string{
		reflect.TypeOf(Evidence{}): {"evidence.json"},
		reflect.TypeOf(Card{}):     {"card-yes.json", "card-settings.json", "card-no.json", "card-unclear.json"},
	} {
		want := map[string]bool{}
		jsonPaths(typ, "", want)
		have := map[string]bool{}
		for _, name := range files {
			b, _ := os.ReadFile(filepath.Join(contracts, name))
			var v any
			json.Unmarshal(b, &v)
			valuePaths(v, "", have)
		}
		var missing []string
		for p := range want {
			if !have[p] {
				missing = append(missing, p)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s: no example shows %v; update testdata/contracts (go test ./internal/pipeline -update)", typ.Name(), missing)
		}
	}
}

// The examples obey the contract's own rules: verdict computed by Decide,
// grades enforced, settings from the grades, every cited id present, and
// every risk finding on the card.
func TestExamplesObeyRules(t *testing.T) {
	want := map[string]checklist.Verdict{"card-yes.json": checklist.Yes, "card-settings.json": checklist.YesWithSettings, "card-no.json": checklist.No, "card-unclear.json": checklist.Unclear}
	for name, verdict := range want {
		b, _ := os.ReadFile(filepath.Join(contracts, name))
		var c Card
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		got, err := checklist.Decide(c.Grades)
		if err != nil || got != verdict || c.Verdict != verdict {
			t.Errorf("%s: Decide %q %v, card says %q, want %q", name, got, err, c.Verdict, verdict)
		}
		enf, dem := checklist.Enforce(c.Grades, finding.Cited(c.Findings))
		if len(dem) != 0 || !reflect.DeepEqual(enf, c.Grades) {
			t.Errorf("%s: grades not enforced: %v", name, dem)
		}
		if !reflect.DeepEqual(checklist.Settings(c.Grades), c.Settings) {
			t.Errorf("%s: settings %v do not follow the grades", name, c.Settings)
		}
		if c.ChecklistVersion != checklist.Version {
			t.Errorf("%s: version %q", name, c.ChecklistVersion)
		}
		for _, f := range c.Findings {
			if f.ID != finding.ID(f.Source, f.Area, f.Quote) {
				t.Errorf("%s: finding %s id is not its hash", name, f.ID)
			}
		}
	}
}

// --- generator: builds the examples from real fixture text ---
//
// Every quote is run through finding.Verify against the T0.4 fixtures, and
// every grade through checklist.Enforce and Decide, so the examples cannot
// show a finding or verdict the real rules would refuse. App info is the
// first line of each listing's App Store fields. Stats, ids and times are
// illustrative values.

type spec struct {
	ref           string // "R355" for a review, or "<file>|<url>" for a search or extract hit
	area          checklist.Area
	stance        finding.Stance
	claim, quote  string
	kind, srcName string
}

func fixtureSegments(t *testing.T, slug string, specs []spec) map[string]finding.Segment {
	dir := filepath.Join("../../testdata/titles", slug)
	segs := map[string]finding.Segment{}
	fetched := time.Date(2026, 10, 13, 18, 57, 30, 0, time.UTC)
	var reviews []string
	for p := 1; p <= 10; p++ {
		b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("reviews-page-%02d.json", p)))
		if err != nil {
			t.Fatal(err)
		}
		var feed struct {
			Feed struct {
				Entry []struct {
					Title   struct{ Label string } `json:"title"`
					Content struct{ Label string } `json:"content"`
				} `json:"entry"`
			} `json:"feed"`
		}
		json.Unmarshal(b, &feed)
		for _, e := range feed.Feed.Entry {
			reviews = append(reviews, e.Content.Label)
		}
	}
	feedURL := map[string]string{"roblox": "431946152", "minecraft": "479516143", "khan-academy-kids": "1378467217", "toca-life-world": "1208138685"}[slug]
	for _, s := range specs {
		if strings.HasPrefix(s.ref, "R") {
			var n int
			fmt.Sscanf(s.ref, "R%d", &n)
			segs[s.ref] = finding.Segment{Text: reviews[n-1], Source: finding.Source{
				URL: "https://itunes.apple.com/us/rss/customerreviews/id=" + feedURL + "/sortby=mostrecent/json", Kind: "review", Index: n, FetchedAt: fetched}}
			continue
		}
		file, url, _ := strings.Cut(s.ref, "|")
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Results []struct {
				URL        string  `json:"url"`
				Title      string  `json:"title"`
				Content    string  `json:"content"`
				RawContent *string `json:"raw_content"`
			} `json:"results"`
		}
		json.Unmarshal(b, &res)
		for _, r := range res.Results {
			if r.URL == url {
				text := r.Content
				if r.RawContent != nil {
					text += "\n" + *r.RawContent
				}
				segs[s.ref] = finding.Segment{Text: text, Source: finding.Source{URL: url, Kind: s.kind, Title: r.Title, FetchedAt: fetched}}
			}
		}
		if _, ok := segs[s.ref]; !ok {
			t.Fatalf("%s: no hit %s in %s", slug, url, file)
		}
	}
	return segs
}

type cardSpec struct {
	file, slug, title, cardID string
	age                       int
	app                       *AppInfo
	specs                     []spec
	grades                    []checklist.Grade // Findings hold spec indexes as strings
	alts                      []Alternative
	missing                   []string
	conflicted                []checklist.Area
}

func build(t *testing.T, cs cardSpec) (Card, Evidence) {
	segs := fixtureSegments(t, cs.slug, cs.specs)
	var raws []finding.Raw
	for _, s := range cs.specs {
		raws = append(raws, finding.Raw{Ref: s.ref, Area: string(s.area), Stance: string(s.stance), Claim: s.claim, Quote: s.quote})
	}
	fs, drops := finding.Verify(raws, segs)
	if len(fs) != len(cs.specs) {
		t.Fatalf("%s: %d of %d quotes verified: %+v", cs.file, len(fs), len(cs.specs), drops)
	}
	grades := make([]checklist.Grade, len(cs.grades))
	for i, g := range cs.grades {
		ids := []string{}
		for _, ix := range g.Findings {
			var n int
			fmt.Sscan(ix, &n)
			ids = append(ids, fs[n].ID)
		}
		g.Findings = ids
		grades[i] = g
	}
	grades, dem := checklist.Enforce(grades, finding.Cited(fs))
	if len(dem) != 0 {
		t.Fatalf("%s: demotions %v", cs.file, dem)
	}
	verdict, err := checklist.Decide(grades)
	if err != nil {
		t.Fatal(err)
	}
	cited := map[string]bool{}
	for _, g := range grades {
		for _, id := range g.Findings {
			cited[id] = true
		}
	}
	onCard := []finding.Finding{}
	for _, f := range fs {
		if cited[f.ID] || f.Stance == finding.Risk {
			onCard = append(onCard, f)
		}
	}
	reviews := 0
	if cs.app != nil {
		reviews = 500
	}
	missing := cs.missing
	if missing == nil {
		missing = []string{}
	}
	conflicted := cs.conflicted
	if conflicted == nil {
		conflicted = []checklist.Area{}
	}
	collected := time.Date(2026, 10, 13, 18, 58, 0, 0, time.UTC)
	stats := Stats{Sources: 9, Reviews: reviews, Chunks: 31, FailedChunks: 1, Drops: finding.Drops{BadArea: 1, BadStance: 0, NoQuote: 2, Unquoted: 4, Duplicates: 3},
		ConflictedAreas: conflicted, MissingKinds: missing, TavilyCredits: 8, ReadCostNano: 11_800_000, JudgeCostNano: 13_600_000,
		ReaderModel: "nvidia/nvidia-nemotron-3-nano-30b-a3b", JudgeModel: "nvidia/nemotron-3-super-120b-a12b"}
	alts := cs.alts
	if alts == nil {
		alts = []Alternative{}
	}
	card := Card{ID: cs.cardID, Title: cs.title, Kind: KindIOS, Age: cs.age, ChecklistVersion: checklist.Version,
		CheckedAt: collected.Add(40 * time.Second), CollectedAt: collected, Verdict: verdict, Grades: grades,
		Settings: checklist.Settings(grades), Findings: onCard, Alternatives: alts, App: cs.app, Stats: stats}
	evStats := stats
	evStats.ConflictedAreas, evStats.JudgeCostNano, evStats.JudgeModel = []checklist.Area{}, 0, ""
	ev := Evidence{Title: cs.title, Kind: KindIOS, CollectedAt: collected, App: cs.app, Findings: fs, Stats: evStats}
	return card, ev
}

func g(a checklist.Area, l checklist.Level, setting, reason string, ix ...string) checklist.Grade {
	if ix == nil {
		ix = []string{}
	}
	return checklist.Grade{Area: a, Level: l, Setting: setting, Reason: reason, Findings: ix}
}

func cardSpecs() []cardSpec {
	const (
		rbxForum = "tavily-search-forum.json|https://www.commonsensemedia.org/articles/parents-ultimate-guide-to-roblox"
	)
	return []cardSpec{
		{file: "card-no.json", slug: "roblox", title: "Roblox", cardID: "c_7Hq2mXv9pK3wLr8sTn4bYe", age: 9,
			app: &AppInfo{TrackID: 431946152, Name: "Roblox", Seller: "Roblox Corporation", SellerURL: "http://www.roblox.com/",
				StoreURL: "https://apps.apple.com/us/app/roblox/id431946152?uo=4", ContentRating: "12+", Price: "Free", Genre: "Games",
				Description: "ROBLOX – MILLIONS OF GAMES ACROSS EVERY GENRE. PLAY FREE."},
			specs: []spec{
				{ref: "R355", area: checklist.Contact, stance: finding.Risk, claim: "A parent reports that a child predator may ask a child where they are",
					quote: "if your kid plays Roblox high chance a child predator might ask where you are"},
				{ref: "R242", area: checklist.Contact, stance: finding.Risk, claim: "A player reports moderation does not stop predators",
					quote: "they can’t stop the child predators and E-Daters"},
				{ref: rbxForum, kind: "article", area: checklist.Contact, stance: finding.Mitigation, claim: "Chat can be limited to friends or turned off",
					quote: `consider setting chat to "friends only" or turning it off completely`},
				{ref: "R1", area: checklist.Money, stance: finding.Risk, claim: "A child reports a Robux purchase that did not arrive in full",
					quote: "I got Robux and I got only 246 Robux instead of 1800"},
				{ref: "R147", area: checklist.AgeGating, stance: finding.Risk, claim: "A 14-year-old reports the age check placed them at 9-12",
					quote: "Im 14 but it said 9-12 on the age check"},
				{ref: "R491", area: checklist.Content, stance: finding.Risk, claim: "A reviewer reports inappropriate horror games",
					quote: "It’s also a very inappropriate game with all the horror games"},
			},
			grades: []checklist.Grade{
				g(checklist.Contact, checklist.Flag, "", "First-hand reports that strangers reach children outweigh the chat settings.", "0", "1", "2"),
				g(checklist.Money, checklist.Flag, "", "Children buy Robux; a purchase dispute shows the pressure.", "3"),
				g(checklist.AgeGating, checklist.Flag, "", "The age check misplaces users.", "4"),
				g(checklist.Content, checklist.Flag, "", "Reviewers report inappropriate games.", "5"),
				g(checklist.Data, checklist.Unknown, "", "No policy finding was verified."),
				g(checklist.DeveloperRecord, checklist.Unknown, "", "No news finding was verified."),
			},
			alts:       []Alternative{{Title: "Toca Life World", Reason: "Same open play-world genre with no chat with strangers.", CardID: "c_Tc6vRb1nQ8zKp2dWm5sLhA", Verdict: checklist.YesWithSettings}},
			missing:    []string{"news"},
			conflicted: []checklist.Area{checklist.Contact},
		},
		{file: "card-yes.json", slug: "khan-academy-kids", title: "Khan Academy Kids", cardID: "c_Kh3nAc9dYm2sWq7vLp4tRx", age: 5,
			app: &AppInfo{TrackID: 1378467217, Name: "Khan Academy Kids", Seller: "Khan Academy", SellerURL: "http://khankids.org",
				StoreURL: "https://apps.apple.com/us/app/khan-academy-kids/id1378467217?uo=4", ContentRating: "4+", Price: "Free", Genre: "Education",
				Description: "Make screen time more meaningful with Khan Academy Kids—the award-winning, educational app for kids ages 2–8."},
			specs: []spec{
				{ref: "tavily-search-rating.json|https://www.khanacademy.org/kids", kind: "rating", area: checklist.Money, stance: finding.Neutral,
					claim: "The app is free with no ads or subscriptions", quote: "Yes — the mobile app is 100% free, with no ads or subscriptions."},
				{ref: "tavily-search-policy.json|https://www.khankids.org", kind: "policy", area: checklist.Data, stance: finding.Mitigation,
					claim: "No ads and no third-party tracking", quote: "We don't show ads in the app, and we don't let third parties track your child across other apps or websites."},
				{ref: "tavily-search-rating.json|https://apps.apple.com/mx/app/khan-academy-kids/id1378467217?l=en-GB", kind: "store", area: checklist.Contact, stance: finding.Neutral,
					claim: "The App Store lists the app in its Kids category, made for ages 0–5", quote: "Made For Ages 0–5"},
				{ref: "tavily-search-rating.json|https://www.khanacademy.org/kids", kind: "rating", area: checklist.AgeGating, stance: finding.Neutral,
					claim: "The developer describes the app as made for ages 2–8", quote: "A free educational app for children ages 2–8"},
				{ref: "R311", area: checklist.Content, stance: finding.Neutral, claim: "A parent reports the app is calm and ad-free for a 4-year-old",
					quote: "My 4 year old loves this. It has everything and isn’t overstimulating AND it has no ads."},
			},
			grades: []checklist.Grade{
				g(checklist.Contact, checklist.OK, "", "A Kids-category app; no source describes chat or messaging.", "2"),
				g(checklist.Money, checklist.OK, "", "Free, with no ads or subscriptions.", "0"),
				g(checklist.Data, checklist.OK, "", "No ads and no third-party tracking.", "1"),
				g(checklist.AgeGating, checklist.OK, "", "Made for ages 2 to 8; nothing to gate.", "3"),
				g(checklist.Content, checklist.OK, "", "Parents report calm, age-appropriate content.", "4"),
				g(checklist.DeveloperRecord, checklist.Unknown, "", "No developer record found."),
			},
			missing: []string{"terms", "news"},
		},
		{file: "card-settings.json", slug: "minecraft", title: "Minecraft", cardID: "c_Mn8cRf4tZq1xHw6pLs3kVb", age: 9,
			app: &AppInfo{TrackID: 479516143, Name: "Minecraft: Play with Friends!", Seller: "Mojang AB", SellerURL: "",
				StoreURL: "https://apps.apple.com/us/app/minecraft-play-with-friends/id479516143?uo=4", ContentRating: "9+", Price: "$6.99", Genre: "Games",
				Description: "Dive into an open world of building, crafting and survival."},
			specs: []spec{
				{ref: "tavily-search-rating.json|https://www.esrb.org/blog/what-parents-need-to-know-about-minecraft", kind: "rating", area: checklist.Contact, stance: finding.Mitigation,
					claim: "Child accounts let parents manage multiplayer access and chat",
					quote: "offer child accounts that empower parents to manage and monitor gameplay, multiplayer access, chat settings, and friend requests"},
				{ref: "R127", area: checklist.Money, stance: finding.Neutral, claim: "A player reports no ads",
					quote: "there is no ads to interrupt me and I can get friends"},
				{ref: "tavily-search-rating.json|https://www.minecraft.net/en-us/article/parents--guide-minecraft", kind: "rating", area: checklist.Content, stance: finding.Neutral,
					claim: "The official parents' guide describes mild, cartoony violence",
					quote: "Because combat is part of the game, there are some elements of mild violence."},
				{ref: "tavily-search-rating.json|https://virtual-addiction.com/online-safety/minecraft", kind: "article", area: checklist.AgeGating, stance: finding.Mitigation,
					claim: "On a child account, multiplayer starts off",
					quote: "On a child account, multiplayer starts off"},
			},
			grades: []checklist.Grade{
				g(checklist.Contact, checklist.OKWithSetting, "Child account: multiplayer friends-only, chat hidden", "Multiplayer chat exists; family settings limit it.", "0"),
				g(checklist.Money, checklist.OK, "", "Paid up front; reviewers report no ads.", "1"),
				g(checklist.Data, checklist.Unknown, "", "No policy text could be read."),
				g(checklist.AgeGating, checklist.OK, "", "Child accounts start with multiplayer off.", "3"),
				g(checklist.Content, checklist.OK, "", "Mild cartoon violence only; reviewers report bugs, not content.", "2"),
				g(checklist.DeveloperRecord, checklist.Unknown, "", "No developer record found."),
			},
			missing: []string{"policy"},
		},
		{file: "card-unclear.json", slug: "toca-life-world", title: "Toca Life World", cardID: "c_Tc6vRb1nQ8zKp2dWm5sLhA", age: 6,
			app: nil,
			specs: []spec{
				{ref: "tavily-extract.json|https://www.tocaboca.com/docs/en/terms-of-service", kind: "terms", area: checklist.Money, stance: finding.Risk,
					claim: "The game sells in-app purchases billed to a card",
					quote: "bill your credit card or use other payment methods if you make an In-App Purchase"},
			},
			grades: []checklist.Grade{
				g(checklist.Contact, checklist.Unknown, "", "No source settled whether players can message each other."),
				g(checklist.Money, checklist.OKWithSetting, "Ask to Buy (Family Sharing)", "In-app purchases are sold; Ask to Buy stops them.", "0"),
				g(checklist.Data, checklist.Unknown, "", ""),
				g(checklist.AgeGating, checklist.Unknown, "", ""),
				g(checklist.Content, checklist.Unknown, "", "No reviews were read."),
				g(checklist.DeveloperRecord, checklist.Unknown, "", "No developer record found."),
			},
			missing: []string{"review", "reddit", "news"},
		},
	}
}

func write(t *testing.T, name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contracts, name), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGenerate rebuilds the examples from fixture text with -update, and
// otherwise checks the committed files match what the generator builds.
func TestGenerate(t *testing.T) {
	for _, cs := range cardSpecs() {
		card, ev := build(t, cs)
		files := map[string]any{cs.file: card}
		if cs.file == "card-no.json" {
			files["evidence.json"] = ev
		}
		for name, v := range files {
			if *update {
				write(t, name, v)
				continue
			}
			want, _ := json.MarshalIndent(v, "", "  ")
			got, err := os.ReadFile(filepath.Join(contracts, name))
			if err != nil || !bytes.Equal(bytes.TrimSpace(got), want) {
				t.Errorf("%s is stale; run go test ./internal/pipeline -update", name)
			}
		}
	}
}
