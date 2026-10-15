package main

// Small, explicitly sourced judge-only cases complement full gathered demos.
// Their quotes remain anchored in saved public pages; no new gather is needed.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/pipeline"
	"github.com/nrynss/before-yes/internal/spend"
	"os"
	"path/filepath"
	"strings"
)

var reasonCases = flag.String("reason-cases", "", "judge-only outside-demo cases JSON, relative to root")

type reasonCase struct {
	Title      string            `json:"title"`
	Slug       string            `json:"slug"`
	Age        int               `json:"age"`
	SourceFile string            `json:"source_file"`
	Findings   []finding.Finding `json:"findings"`
}

func evaluateReasonCases(root, models string) ([]Result, error) {
	b, err := os.ReadFile(filepath.Join(root, *reasonCases))
	if err != nil {
		return nil, err
	}
	var cases []reasonCase
	if err = json.Unmarshal(b, &cases); err != nil {
		return nil, err
	}
	var out []Result
	for _, c := range cases {
		// Verify each quote against its saved source before the judge can see it.
		raw, err := os.ReadFile(filepath.Join(root, c.SourceFile))
		if err != nil {
			return nil, err
		}
		var page struct {
			Results []struct {
				URL string `json:"url"`
				Raw string `json:"raw_content"`
			} `json:"results"`
		}
		if err = json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		for _, f := range c.Findings {
			found := false
			for _, p := range page.Results {
				if p.URL == f.Source.URL && stringsContains(p.Raw, f.Quote) {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("%s: unverified quote %s", c.Title, f.ID)
			}
		}
		dir, err := os.MkdirTemp("", "before-yes-reason-cases-")
		if err != nil {
			return nil, err
		}
		m, err := spend.Open(context.Background(), spend.Config{Path: filepath.Join(dir, "spend.db"), Pools: map[spend.Pool]spend.PoolConfig{spend.PoolModel: {Limit: 1000000000000}}})
		if err != nil {
			return nil, err
		}
		_, judge := chatters(root, models, c.Slug)
		fast, think := judgeSwitches(models)
		got, err := (&pipeline.Judge{Model: judge, Meter: m, Entail: entailChatter(judge), Fast: fast, Think: think, ThinkMaxTokens: conflictMaxTokens}).Judge(context.Background(), c.Title, c.Age, "reason-case", c.Findings, nil)
		r := Result{Title: c.Title, Age: c.Age, GotVerdict: got.Verdict, Findings: len(c.Findings), JudgeNano: int64(got.Cost)}
		r.Grades = got.Grades
		evidenceJSON, _ := json.Marshal(c.Findings)
		r.EvidenceHash = fmt.Sprintf("%x", sha256.Sum256(evidenceJSON))
		if err != nil {
			r.Err = err.Error()
		}
		if miss := replayMisses(judge); miss != "" {
			r.Err = miss
		}
		if refused := liveRefusals(judge); refused != "" {
			r.Err = refused
		}
		for _, g := range got.Grades {
			r.Areas = append(r.Areas, AreaResult{Area: g.Area, Got: g.Level, GotNote: g.Reason})
		}
		liveSummarise(c.Title, judge)
		m.Close()
		os.RemoveAll(dir)
		out = append(out, r)
	}
	return out, nil
}
func stringsContains(text, quote string) bool { return quote != "" && strings.Contains(text, quote) }
