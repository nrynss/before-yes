package pipeline

// The word lists behind the rule gate (rulegate.go). One block per area, so a
// reader can see everything a rule keys on in one place. Nothing here names a
// title.
//
// How entries match. A quote and every entry are normalised the same way
// (gateWords): case-folded, punctuation removed, hyphens and slashes read as
// spaces, so "In-App Purchases" and "in app purchases" are one phrase. An
// entry is one or more words; a word that ends in "*" matches any word that
// starts with it ("purchas*" matches purchase, purchased, purchases). A
// phrase matches only as consecutive words.
//
// A pattern holds two ways to match, either of which is enough:
//
//	any: one of these phrases appears;
//	all: every group has at least one of its phrases (an AND of ORs).
//
// Risk vocabularies skip a phrase that a nearby negation cancels ("no ads",
// "without in-app purchases", "free from ads"): see negated in rulegate.go.
// Control and failure vocabularies do not.

// pattern is one way a quote can qualify. See the block comment above.
type pattern struct {
	any []string
	all [][]string
}

// control is a parent-set control a mitigation quote can name, and the
// settings label the card shows when the rule caps a grade on it.
type control struct {
	label string
	pat   pattern
}

// controlFailure words say a control did not hold. A cited risk quote
// carrying one of these stops a rule from capping a concern at "needs a
// setting": the control is reported failing, so it is not an answer.
var controlFailure = []string{
	"bypass*", "get around", "gets around", "got around", "work around", "workaround*", // the control is routed around
	"circumvent*", "dodge*", "evade*", "loophole*", // same
	"still able", "still can", "still get", "still got", "still see", "still receive*", // the harm continues despite the control
	"doesnt work", "dont work", "didnt work", "not work*", "isnt working", "stopped working", // the control does not work
	"ineffective", "useless", // same
	"cant be disabled", "cannot be disabled", "cant be turned off", "cannot be turned off", // the control cannot be applied
	"cant disable", "cannot disable", // same
	"even when", "even with", "even though", "despite", // the harm persists with the control on
	"ignore*",                                                                               // the control is overridden
	"didnt catch", "didnt block", "didnt stop", "doesnt stop", "got through", "get through", // the filter or limit let it through
	"slipped through", "let through", "failed to", "fails to", // same
	"cant stop", "cannot stop", "couldnt stop", "unable to stop", "cant prevent", "cannot prevent", "doesnt prevent", // moderation that does not stop it
}

// controlAbsent: a quote that names a control to say it is missing ("there is
// no way to require approval for purchases"). Controls are read from a quote's
// text whatever stance the reader gave it, so these keep an absence from being
// read as a control.
var controlAbsent = []string{
	"no way to", "no option", "no ability", "unable to", "lack*", "doesnt have", "does not have", "dont have",
	"doesnt offer", "does not offer", "not possible", "theres no", "there is no", "no setting", "no control*", "no parental",
}

// controlTopic: words that mark a quote as speaking of a parental control, for
// the area-wide check that a control is reported failing.
var controlTopic = []string{
	"parental control*", "setting", "settings", "limit", "limits", "approv*", "restrict*", "filter*", "password", "passcode",
	"ask to buy", "block*", "turned off", "turn off", "disabled", "family sharing", "family link", "moderat*",
}

// ---------------------------------------------------------------- money

// moneyRisk: a quote names a purchase, in-app purchase, subscription, ad,
// loot box or real spending. A money flag needs at least one cited risk quote
// that matches (a necessary condition: the entailment check still decides
// whether it is enough).
var moneyRisk = []pattern{
	{any: []string{"purchas*", "buy", "buys", "buying", "bought"}},        // a purchase made or offered
	{any: []string{"spend", "spends", "spending", "spent"}},               // spending
	{any: []string{"pay", "pays", "paying", "paid", "payment*"}},          // paying
	{any: []string{"charge", "charges", "charged", "billed", "billing"}},  // being charged
	{any: []string{"subscri*"}},                                           // subscription traps
	{any: []string{"microtransaction*", "micro transaction*"}},            // microtransactions
	{any: []string{"in app"}},                                             // in-app purchases and currency
	{any: []string{"loot box*", "lootbox*", "gacha", "pay to win"}},       // loot boxes and pay-to-win
	{any: []string{"coins", "robux", "minecoins", "v bucks", "currency"}}, // virtual currency
	{any: []string{"real money", "credit card", "cost money", "costs money", "cost real money"}},
	{any: []string{"ad", "ads", "advert*", "advertis*"}}, // advertising shown to the child
	{any: []string{"premium", "paywall*", "dollars"}},    // premium tiers and prices
}

// moneyControls: a quote that names a working way to stop a child completing
// a purchase — rule D's purchase controls, read from the text like rule C's.
// Label is the setting the card shows. The list order matters: the first
// entry whose pattern matches names the control and its quotes.
var moneyControls = []control{
	{"Ask to Buy (purchase approval)", pattern{any: []string{"ask to buy"}}}, // Apple's purchase approval
	{"Ask a parent to approve purchases", pattern{all: [][]string{ // "ask a parent to buy things", Microsoft Family Safety's purchase switch
		{"ask*", "need*"},
		{"parent*", "guardian*", "adult*", "family*"},
		{"buy", "buys", "buying", "bought", "purchas*", "spend*", "payment*", "money"},
	}}},
	{"Require approval for purchases", pattern{all: [][]string{ // approval, passcode or password to buy
		{"approv*", "permission", "passcode", "password", "pin"},
		{"purchas*", "buy", "buying", "spend*", "payment*"},
		{"parent*", "guardian*", "you", "your", "adult*", "family", "child*", "kid*"}, // approval by a parent, not by the platform
	}}},
	{"Microsoft Family Safety purchase controls", pattern{all: [][]string{ // Microsoft's family settings over a child's buying
		{"family safety"},
		{"purchas*", "spend*", "buy", "buying", "payment*"},
	}}},
	{"Purchase approval", pattern{any: []string{ // the control named outright
		"purchase approval*", "purchase control*", "approv* purchas*",
	}}},
	{"Set a spending limit", pattern{any: []string{ // a cap on spending
		"spending limit*", "spend limit*", "purchase limit*", "monthly limit*", "spending cap*", "spending control*",
	}}},
	{"Turn off in-app purchases", pattern{any: []string{ // purchases switched off
		"disable in app purchas*", "turn off in app purchas*", "restrict in app purchas*",
		"block in app purchas*", "disable purchas*", "restrict purchas*", "block purchas*",
	}}},
}

// ---------------------------------------------------------------- data

// dataRisk: a quote about selling personal data, ad tracking or behavioural
// advertising, sharing for others' marketing, a breach or leak, collecting
// from children without consent, or no way to delete. Sharing with service
// providers who run the app is deliberately absent: none of these patterns
// matches it unless the quote also names advertising or marketing.
var dataRisk = []pattern{
	{any: []string{"data used to track you"}}, // Apple's privacy-label heading for cross-app tracking
	{all: [][]string{ // selling or monetising personal data
		{"sell*", "sold", "resell*", "sale", "monetiz*", "monetis*"},
		{"data", "information", "personal", "details", "identifier*", "profile*"},
	}},
	{all: [][]string{ // ad tracking
		{"track*"},
		{"advertis*", "ads", "ad", "marketing", "profile*", "across"},
	}},
	{any: []string{ // behavioural advertising
		"behavioral advertis*", "behavioural advertis*", "targeted ad", "targeted ads", "targeted advertis*",
		"personalized ad*", "personalised ad*", "interest based ad*", "data broker*",
	}},
	{all: [][]string{{"target*"}, {"advertis*", "ads"}}}, // targeting children with advertising
	{all: [][]string{ // using personal information to choose or show ads
		{"personal information", "information we collect", "your information", "your data", "user data", "data we collect"},
		{"display ads", "which ads", "show you ads", "show ads", "ads to show", "serve ads", "ads based"},
	}},
	{all: [][]string{ // sharing for others' marketing
		{"share*", "sharing", "disclos*", "provid*", "send*", "transfer*", "licens*"},
		{"advertis*", "marketing", "advertiser*", "marketers"},
	}},
	{any: []string{ // a reported breach
		"data breach*", "security breach*", "was breached", "were breached", "leaked", "hacked",
	}},
	{all: [][]string{ // collected from children without consent
		{"collect*", "gather*", "harvest*"},
		{"without consent", "without parental consent", "without parents consent", "without their parents consent", "without permission"},
	}},
	{all: [][]string{ // no way to delete what was collected
		{"no way to", "cant", "cannot", "unable to", "impossible to"},
		{"delete*"},
		{"data", "account", "information", "personal"},
	}},
}

// dataLabelNotLinked is Apple's privacy-label heading that, alone, says the
// developer reports no data linked to the user and none used to track them.
// storefacts.go writes the finding only when neither of the other headings is
// on the label.
var dataLabelNotLinked = []string{"data not linked to you"}

// accountHacked: a user's report of their own account being hacked, stolen
// or compromised. One such report is about the account's security, not about
// how the company handles children's data, so alone it does not hold a data
// flag up (rule F). Negated hits ("never got hacked") do not count.
var accountHacked = []string{
	"got hacked", "getting hacked", "was hacked", "been hacked", "were hacked", // hacked
	"account stolen", "accounts stolen", "account was stolen", "account got stolen", "stole my account", "stolen my account", // stolen
	"account compromised", "accounts compromised", "account was compromised", "account got compromised", "got compromised", "was compromised", "been compromised", // compromised
}

// companyBreach: a breach or leak at the company itself — "data breach",
// "leaked", "exposed records" — the words a regulator or news report of a
// breach carries. Such a quote keeps a data flag standing whatever hack
// reports sit beside it (rule F). A person's own account being hacked is
// accountHacked, not this.
var companyBreach = []string{
	"data breach*", "security breach*", "was breached", "were breached", "breached",
	"leaked", "data leak*",
	"exposed record*", "records exposed", "exposed the data", "data exposed",
}

// dataMisuse is dataRisk without its breach group and without the no-delete
// group: data being sold, tracked for ads, shared for marketing, or collected
// without consent. Any such quote keeps a data flag standing beside a hack
// report (rule F).
var dataMisuse = append(append([]pattern{}, dataRisk[:7]...), dataRisk[8:9]...)

// ---------------------------------------------------------------- content

// contentHarm: a quote names a kind of material areaDefs calls unsuitable:
// violence or gore, sexual content, self-harm, predators and grooming,
// gambling, drugs, frightening material, or harassment. A parent's objection
// to a belief, value or theme (religion, yoga, gender roles, politics) names
// none of these and so never matches; there is deliberately no list of such
// topics, because a topic is not a harm.
var contentHarm = append(append([]pattern{}, contentSerious...), contentMild...)

// contentSerious: harm that flags a title on one first-hand report.
var contentSerious = []pattern{
	{any: []string{"violen*", "gore", "gory", "gruesome", "blood", "bloody", "bloodshed", "kill*", "murder*", "shoot*", "gun", "guns", "weapon*"}},                                    // violence and gore
	{any: []string{"sexual*", "sex", "sexy", "nude", "nudity", "naked", "porn*", "erotic*", "explicit", "condo", "adult content", "adult themes", "mature content", "mature themes"}}, // sexual content
	{any: []string{"self harm", "selfharm", "suicid*", "eating disorder*"}},                                                                                                           // self-harm
	{any: []string{"predator*", "preds", "groom*", "pedophil*", "paedophil*", "child abuse", "sextortion", "exploitation"}},                                                           // predators and grooming
	{any: []string{"gambl*", "casino*", "slot machine*", "betting", "wager*"}},                                                                                                        // gambling
	{any: []string{"drug*", "alcohol*", "cannabis", "marijuana", "vaping"}},                                                                                                           // drugs
	{any: []string{"harass*", "bully*", "bullies", "bullying", "hate speech", "racist", "racism"}},                                                                                    // harassment
}

// contentMild: brief frightening or scary imagery, mild cartoon peril, crude
// humour. One such report, from one source, does not flag a title whose store
// rating already suits the child (content.single_mild_report); two do.
var contentMild = []pattern{
	{any: []string{"scary", "frighten*", "terrif*", "horror", "creepy", "disturbing", "nightmare*", "jump scare*", "traumat*", "devil", "demon*"}}, // frightening imagery
	{any: contentMildPhrases}, // mild peril
	{any: []string{"crude", "crude humor", "crude humour", "potty humor", "potty humour", "toilet humor", "toilet humour"}}, // crude humour
}

// contentMildPhrases name violence in the mild register ("cartoon violence").
// They count as mild harm, and their words are set aside before the serious
// vocabulary is read, so "mild cartoon violence" is not serious violence.
var contentMildPhrases = []string{"mild violence", "cartoon violence", "fantasy violence", "cartoon peril", "mild peril", "mild cartoon*", "infrequent mild"}

// contentBenign: a non-store quote saying the material suits children. Used
// only with a store rating at or below the child's age, as the second witness
// the rating needs (a rating is the developer's own questionnaire answer). A
// bare "age appropriate" is left out: reviewers say it of a lesson's difficulty.
var contentBenign = []pattern{
	{any: []string{"appropriate content", "content is appropriate", "content was appropriate", "content is age appropriate"}}, // the material is judged suitable
	{any: []string{"appropriate for kids", "appropriate for children", "appropriate for young", "safe for kids", "safe for children", "safe for young"}},
	{any: []string{"kid friendly", "child friendly", "family friendly", "wholesome"}},
	{any: []string{"no violence", "no inappropriate", "no scary", "no graphic", "no blood", "free of violence", "nothing inappropriate"}}, // the harms are said to be absent
}

// ---------------------------------------------------------------- developer record

// legalAction: terms for an enforcement action. A developer-record flag needs
// a news or regulator quote with one of these that names the developer.
var legalAction = []pattern{
	{any: []string{"sued", "sue", "suing", "lawsuit*", "class action", "settlement*", "settled"}},                                                            // suits
	{any: []string{"fined", "fines", "penalty", "penalties", "civil penalty"}},                                                                               // fines
	{any: []string{"ftc", "federal trade commission", "attorney general*", "attorneys general", "regulator*", "regulatory", "consent decree", "injunction"}}, // enforcers
	{any: []string{"data breach*", "security breach*", "was breached", "were breached", "breached"}},                                                         // breaches
	{all: [][]string{{"coppa"}, {"violat*", "violation*"}}},                                                                                                  // COPPA violations
	{any: []string{"indicted", "indictment", "subpoena*"}},                                                                                                   // criminal and investigative process
}

// developerClean: an explicit statement that the developer has no enforcement
// record. Absence of news is not such a statement.
var developerClean = []pattern{
	{any: []string{
		"no known data breach*", "no data breach*", "no reported data breach*", "no known breach*",
		"no history of data breach*", "no history of violations", "no known violations",
		"has not been fined", "hasnt been fined", "never been fined", "no fines", "no known fines",
		"no lawsuits", "no regulatory action*", "no enforcement action*", "clean record",
	}},
}

// platformSellers: the companies whose stores and platforms carry thousands
// of titles. A fine, settlement, lawsuit or regulator action that names one
// of them, and not the title, is about their platform or another product —
// Microsoft's Xbox fine on a Minecraft check — so it is information, not
// evidence (rule E). The store's own seller field names the developer's
// trading company; these are the sellers a store listing does not give.
var platformSellers = []struct{ display, term string }{
	{"Microsoft", "microsoft"},
	{"Xbox", "xbox"},
	{"Google", "google"},
	{"Google Play", "google play"},
	{"Play Store", "play store"},
	{"Apple", "apple"},
	{"App Store", "app store"},
	{"Sony", "sony"},
	{"PlayStation", "playstation"},
	{"Nintendo", "nintendo"},
	{"Amazon", "amazon"},
	{"Meta", "meta"},
	{"Epic Games", "epic games"},
	{"TikTok", "tiktok"},
}

// ---------------------------------------------------------------- contact

// contactChannel: a way for people to reach a child (message, chat, talk,
// friend requests).
var contactChannel = []string{
	"messag*", "chat", "chats", "chatting", "chatted", "dm", "dms", "voice chat", "talk", "talks", "talking", // messaging and chat
	"contact", "contacted", "contacts", "communicat*", "friend request*", "text message*", "texted", "texting", // other ways in
	"speak*", "whisper*", "interact with",
	"ask", "asks", "asked", "asking", "request*", "approach*", "reach", "reached", "reaches", "reaching", // a stranger approaching a child
	"invite*", "phone number",
}

// contactStranger: a reference to people the child does not know.
var contactStranger = []string{
	"stranger*", "anyone", "any user", "any users", "everyone", "random", "unknown", "predator*", "groom*",
	"adults", "other players", "other users", "other people", "dont know", "pedophil*", "paedophil*",
	"someone", "somebody", "a man", "an adult", "creep*", "weirdo*",
}

// contactRisk: strangers can reach a child by message, chat or voice. Either a
// channel together with a stranger reference, or a channel together with a
// report that the control for it fails (chat that cannot be switched off is
// chat strangers can use).
var contactRisk = []pattern{
	{all: [][]string{contactChannel, contactStranger}},
	{all: [][]string{contactChannel, controlFailure}},
}

// contactControls: a cited mitigation names a control that shuts the door.
var contactControls = []control{
	{"Friends-only chat", pattern{any: []string{"friends only", "only friends", "only your friends", "approved friends", "approved contacts"}}},
	{"Turn chat off or restrict it", pattern{all: [][]string{
		{"chat", "chats", "chatting"},
		{"off", "disabl*", "restrict*", "blocked", "hidden", "limited", "no one"},
	}}},
	{"Turn off or limit messaging", pattern{all: [][]string{
		{"messag*", "communicat*", "contact*", "multiplayer"},
		{"off", "disabl*", "restrict*", "blocked", "limited"},
	}}},
	{"Approve friend requests", pattern{all: [][]string{
		{"friend*"},
		{"approv*", "permission", "consent"},
	}}},
}

// contactNoChat: a first-party statement that the title has no chat or
// messaging between players.
var contactNoChat = []pattern{
	{any: []string{
		"no chat", "no in app chat", "no chat feature*", "no messaging", "does not include chat", "doesnt include chat",
		"does not have chat", "doesnt have chat", "does not offer chat", "no communication between users",
		"no communication between players", "no social features", "no user to user", "no online interaction*",
		"no interaction with other", "no multiplayer", "chat is not available", "without chat", "chat free",
		"no player to player",
	}},
}

// ---------------------------------------------------------------- age gating

// ageGateBypass: a quote about the age check being bypassed, or a child placed
// in the wrong age band. It needs an age word together with a bypass or
// misplacement word. A complaint about grade level ("only goes up to second
// grade"), a recommended age, or a wish for harder content has neither.
var ageGateBypass = []pattern{
	{all: [][]string{
		{"age", "birthday", "birthdate", "birth", "born", "dob", "verification", "verify", "verified", "gate"},
		{
			"bypass*", "dodge*", "circumvent*", "get around", "got around", "gets around", "work around", // getting past the check
			"fake", "fictitious", "lie", "lied", "lying", "pretend*", "made up", // false answers
			"any birthdate", "any birthday", "any birth date", "any date", "any age", "enter any", // a check that accepts anything
			"simply enter", "just enter", "just type", //
			"wrong age", "wrong group", "wrong age group", "wrong age band", "incorrect age", "incorrectly", // misplacement
			"miscategor*", "misclassif*", "mistake*", "placed in", "put in", "categorized as", "categorised as",
			"classified as", "marked as", "treated as",
			"let me in", "let in", "got in", "get in", "no age verification", "no verification", // getting in
			"without verification", "unverified", "not verified", "accepted",
		},
	}},
	// A named age group with a number in the same quote: "my friend is 9 and
	// she is in the 21+ age group". "#" stands for any number. "age range" is
	// left out on purpose: reviewers use it for a lesson's level.
	{all: [][]string{{"age group*", "age band*", "age bracket*"}, {"#"}}},
	// An age check that reports the wrong age for a child: "the face
	// verification said my four year old is twenty five".
	{all: [][]string{
		{"verification", "age check", "age estimation", "age estimate*", "face scan", "face check", "selfie", "age gate"},
		{"says", "said", "thinks", "thought", "estimated", "assigned", "gave", "puts", "put", "placed", "categori*", "classif*"},
		{"year old", "years old", "yo", "kid", "child", "#"}, // #: "it said 9-12 on the age check"
	}},
}

// ageGateControls: a cited mitigation says the account's age cannot be changed
// without a parent, so the right band holds.
var ageGateControls = []control{
	{"Lock the account's birth date", pattern{all: [][]string{
		{"age", "birthday", "birthdate", "birth"},
		{
			"lock*", "cannot be changed", "cant be changed", "only a parent", "parent must", "parental consent",
			"parent approval", "parental approval", "parent verif*", "verified by a parent",
		},
	}}},
}
