// Package pipeline turns a title and an age into a card.
//
// This file holds the frozen contract types: Evidence, read once per title
// and reused across ages, and Card, one judgement for one age. The example
// payloads in testdata/contracts/ must round-trip through these types; a
// change here updates them in the same commit.
package pipeline

import (
	"time"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
)

// Kind is the kind of title.
type Kind string

// Kinds of title, from the brief.
const (
	KindIOS     Kind = "ios"
	KindAndroid Kind = "android"
	KindConsole Kind = "console_pc"
	KindYouTube Kind = "youtube"
	// KindUnknown is a title no signal placed (user decision, T3.7).
	KindUnknown Kind = "unknown"
)

// KindLabel is the card's words for a kind: "kind unknown" for a title no
// signal placed.
func KindLabel(k Kind) string {
	switch k {
	case KindIOS:
		return "iOS app"
	case KindAndroid:
		return "Android app"
	case KindConsole:
		return "console or PC game"
	case KindYouTube:
		return "YouTube channel"
	}
	return "kind unknown"
}

// AppInfo is the matched App Store listing. It is nil when no hit matched.
type AppInfo struct {
	TrackID       int64  `json:"track_id"`
	Name          string `json:"name"`
	Seller        string `json:"seller"`
	SellerURL     string `json:"seller_url"`
	StoreURL      string `json:"store_url"`
	ContentRating string `json:"content_rating"`
	Price         string `json:"price"`
	Genre         string `json:"genre"`
	Description   string `json:"description"`
}

// Stats are the card's small print.
type Stats struct {
	// Sources is how many documents were read, reviews excluded.
	Sources int `json:"sources"`
	Reviews int `json:"reviews"`
	// ReviewSource says where the reviews came from: "feed" (the App Store
	// review feed), "page" (the App Store web page, when the feed returned
	// nothing), or "" when there are none.
	ReviewSource string `json:"review_source"`
	Chunks       int    `json:"chunks"`
	// FailedChunks is how many reader calls returned nothing usable.
	FailedChunks int           `json:"failed_chunks"`
	Drops        finding.Drops `json:"drops"`
	// ConflictedAreas are the areas the judge re-graded with thinking on.
	ConflictedAreas []checklist.Area `json:"conflicted_areas"`
	// MissingKinds are the source kinds that came back empty, such as "policy".
	MissingKinds  []string `json:"missing_kinds"`
	TavilyCredits int      `json:"tavily_credits"`
	// ReadCostNano and JudgeCostNano are in nanodollars (1e-9 USD).
	ReadCostNano  int64  `json:"read_cost_nano"`
	JudgeCostNano int64  `json:"judge_cost_nano"`
	ReaderModel   string `json:"reader_model"`
	JudgeModel    string `json:"judge_model"`
}

// Evidence is everything read about a title before any age applies. It is
// cached by title and reused across ages. Its Stats leave the judge fields
// zero.
type Evidence struct {
	Title       string            `json:"title"`
	Kind        Kind              `json:"kind"`
	CollectedAt time.Time         `json:"collected_at"`
	App         *AppInfo          `json:"app"`
	Findings    []finding.Finding `json:"findings"`
	Stats       Stats             `json:"stats"`
}

// Alternative is a safer title found with Tavily and checked the same way.
// CardID and Verdict are empty until its own check has run.
type Alternative struct {
	Title   string            `json:"title"`
	Reason  string            `json:"reason"`
	CardID  string            `json:"card_id"`
	Verdict checklist.Verdict `json:"verdict"`
	// Status is "pending" (found, not checked yet), "checked" (its verdict
	// beats the card's), or "refused" (the meter refused its check).
	Status string `json:"status"`
}

// Card is one judgement for one age.
type Card struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
	// KindLabel is KindLabel(Kind), the words the card shows.
	KindLabel        string            `json:"kind_label"`
	Age              int               `json:"age"`
	ChecklistVersion string            `json:"checklist_version"`
	CheckedAt        time.Time         `json:"checked_at"`
	CollectedAt      time.Time         `json:"collected_at"`
	Verdict          checklist.Verdict `json:"verdict"`
	// Grades holds six grades in card order.
	Grades []checklist.Grade `json:"grades"`
	// Settings are the settings to turn on, from ok_with_setting grades.
	Settings []string `json:"settings"`
	// Findings holds the findings the grades cite plus every risk finding.
	Findings     []finding.Finding `json:"findings"`
	Alternatives []Alternative     `json:"alternatives"`
	// AlternativesNote is MoreAlternativesNote when at least one
	// alternative was refused (its name is not shown), else empty.
	AlternativesNote string   `json:"alternatives_note,omitempty"`
	App              *AppInfo `json:"app"`
	Stats            Stats    `json:"stats"`
}
