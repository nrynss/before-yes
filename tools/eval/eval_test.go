package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/before-yes/internal/appstore"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"github.com/nrynss/before-yes/internal/tavily"
)

const root = "../.."

// Every quote an expectation calls decisive must be in the text the
// gatherer collects from the fixtures, so a miss is the reader's or the
// judge's, never an expectation that no pipeline could meet.
func TestExpectationsAreReachable(t *testing.T) {
	exps, err := loadExpectations(filepath.Join(root, "testdata", "eval"))
	if err != nil {
		t.Fatal(err)
	}
	if len(exps) != 3 {
		t.Fatalf("%d expectations, want the three demo titles", len(exps))
	}
	for _, e := range exps {
		docs := gather(t, e)
		areas := map[string]bool{}
		for _, a := range e.Areas {
			areas[string(a.Area)] = true
			for _, q := range append(append([]Quote{}, a.Decisive...), a.Supporting...) {
				if !inDocs(docs, q) {
					t.Errorf("%s %s: quote from %s is not in the gathered text: %q", e.Slug, a.Area, q.Source, q.Quote)
				}
			}
		}
		if len(areas) != 6 {
			t.Errorf("%s: %d areas, want six", e.Slug, len(areas))
		}
	}
}

func inDocs(docs []pipeline.Doc, q Quote) bool {
	want := finding.Normalize(q.Quote)
	for _, d := range docs {
		if sameSource(d.Source, q.Source) && strings.Contains(finding.Normalize(d.Text), want) {
			return true
		}
	}
	return false
}

func gather(t *testing.T, e Expectation) []pipeline.Doc {
	t.Helper()
	_, srv := serveFixture(filepath.Join(root, "testdata", "titles", e.Slug), e.Title)
	t.Cleanup(srv.Close)
	m, err := spend.Open(context.Background(), spend.Config{Path: filepath.Join(t.TempDir(), "s.db"), Pools: map[spend.Pool]spend.PoolConfig{spend.PoolTavily: {Limit: 1_000_000}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	tc := tavily.New(srv.URL, func() (string, error) { return "offline", nil })
	tc.Retry.PerMinute, tc.Retry.Name = 0, ""
	tc.Retry.Sleep = func(context.Context, time.Duration) error { return nil }
	g := &pipeline.Gatherer{Store: publicFeed{appstore.New(srv.URL, "us"), appstore.New("", "us")}, Search: tc, Meter: m}
	got, err := g.Gather(context.Background(), e.Title, "t")
	if err != nil {
		t.Fatal(err)
	}
	return got.Docs
}

// The eval runs offline end to end with the stand-ins and reports every
// title. The numbers are pinned loosely: a change to the plan, the prompts
// or the stand-ins shows up here and in the baseline.
func TestEvalStandinOffline(t *testing.T) {
	rs, err := evaluate(root, "standin")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 3 {
		t.Fatalf("%d results", len(rs))
	}
	for _, r := range rs {
		if r.Err != "" {
			t.Errorf("%s: %s", r.Title, r.Err)
		}
		if len(r.Areas) != 6 || r.Chunks == 0 || r.Credits == 0 || r.ReadNano == 0 || r.JudgeNano == 0 {
			t.Errorf("%s: %+v", r.Title, r)
		}
	}
	var sb strings.Builder
	report(&sb, rs)
	for _, want := range []string{"Roblox, age 9", "Minecraft, age 9", "Khan Academy Kids, age 5", "TOTAL grades agree"} {
		if !strings.Contains(sb.String(), want) {
			t.Errorf("report misses %q", want)
		}
	}
}

// Replay mode with no recordings fails every call, and says so, rather
// than quietly falling back to the stand-ins.
func TestReplayWithoutRecordingsFails(t *testing.T) {
	if _, err := os.Stat(filepath.Join(root, "testdata", "eval", "replies")); err == nil {
		t.Skip("recordings exist")
	}
	rs, err := evaluate(root, "replay")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if r.Err == "" {
			t.Errorf("%s: replay with no recordings did not fail", r.Title)
		}
	}
}
