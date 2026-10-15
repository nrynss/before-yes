package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/checklist"
	"github.com/nrynss/before-yes/internal/finding"
	"github.com/nrynss/before-yes/internal/model"
	"github.com/nrynss/before-yes/internal/spend"
)

// Chatter is the model surface the reader and the judge use. *model.Client
// has it.
type Chatter interface {
	Chat(ctx context.Context, msgs []model.Message, o model.Options) (model.Reply, error)
	Estimate(msgs []model.Message, o model.Options) cost.Price
	Settings() model.Settings
}

// chat runs one model call through the meter on spend.PoolModel. The kind
// names the provider and the step, such as "tokenfactory:read". It returns
// the price the meter settled at, which is what the card reports.
func chat(ctx context.Context, m Meter, c Chatter, step, ref string, msgs []model.Message, o model.Options) (model.Reply, cost.Price, error) {
	var reply model.Reply
	kind := c.Settings().Provider + ":" + step
	usage, err := m.Spend(ctx, spend.PoolModel, kind, ref, c.Estimate(msgs, o), func(ctx context.Context) (cost.Usage, error) {
		r, err := c.Chat(ctx, msgs, o)
		if err != nil {
			return cost.Usage{}, err
		}
		reply = r
		return cost.Usage{Price: r.Price, Measured: r.Measured}, nil
	})
	if err != nil {
		return model.Reply{}, 0, err
	}
	return reply, usage.Price, nil
}

func readerSystem() string {
	var b strings.Builder
	b.WriteString(`You read source material about an app, game or channel a parent is deciding on, and extract findings for a fixed checklist.

Areas:
`)
	for _, a := range checklist.Areas() {
		fmt.Fprintf(&b, "- %s: %s\n", a.ID, a.Question)
	}
	b.WriteString(`
Each input segment starts with a reference like [R12] or [D3]. For every claim in the text that bears on an area, write one finding:
- "ref": the segment reference the quote is in, exactly as given;
- "area": one of the area names above;
- "claim": one short plain sentence a parent would understand;
- "stance": "risk" (something a parent should worry about), "mitigation" (a setting, control or protection), or "neutral";
- "quote": the exact words from the segment that support the claim, copied character for character, 10 to 40 words. Never paraphrase, never join sentences, never fix spelling.

Rules:
- One finding per claim.
- Only extract what the text says. No outside knowledge.
- Skip bug reports and gameplay talk, and praise or complaints that do not bear on an area.
- A child's or parent's first-hand report counts; say so in the claim ("A parent reports ...").
- A summary a site wrote about what parents or kids say is not a first-hand report.
- If nothing in a segment bears on an area, write nothing for it.

Reply with one JSON object: {"findings": [ ... ]}. An empty list is a valid answer.`)
	return b.String()
}

func readerUser(title string, c chunk, docs []Doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n\n", title)
	for _, s := range c.Segments {
		src := docs[s.Doc].Source
		fmt.Fprintf(&b, "[%s] (%s", s.Ref, src.Kind)
		if src.Title != "" {
			fmt.Fprintf(&b, ": %s", src.Title)
		}
		b.WriteString(")\n")
		b.WriteString(s.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// Reader runs the reader model over the gathered documents.
type Reader struct {
	Model Chatter
	Meter Meter
	// Concurrency bounds the calls in flight. Zero or less means 4.
	Concurrency int
}

// Read is what one read produced.
type Read struct {
	Findings []finding.Finding
	Drops    finding.Drops
	Chunks   int
	// Failed counts the chunks whose call or reply failed and were skipped.
	Failed int
	// Cost is what the reader calls cost, as the meter settled them.
	Cost cost.Price
}

// Read reads docs in chunks with bounded concurrency and checks every quote
// against the whole text of the document its ref names. A ref outside the
// chunk it came back from counts as unquoted. A chunk whose call fails, or
// whose reply will not parse, is counted and skipped: one rate-limited chunk
// should not sink the card. Read fails only when every chunk fails, or the
// context ends.
func (r *Reader) Read(ctx context.Context, title, ref string, docs []Doc) (Read, error) {
	if r.Model == nil || r.Meter == nil {
		return Read{}, errors.New("read: a model and a meter are required")
	}
	chunks := chunkDocs(docs)
	segs := map[string]finding.Segment{}
	for i, ref := range assignRefs(docs) {
		d := docs[i]
		segs[ref] = finding.Segment{Text: d.Text, Source: d.Source}
	}
	type result struct {
		raws     []finding.Raw
		unquoted int
		price    cost.Price
		err      error
	}
	results := make([]result, len(chunks))
	n := r.Concurrency
	if n <= 0 {
		n = 4
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	system := readerSystem()
	// Chunk counts for the progress hook: one report as reading starts and
	// one as each chunk ends, whether it succeeded or not.
	// The count and its report share a lock, so the hook sees 1, 2, 3 in
	// order however the chunks race.
	var progMu sync.Mutex
	finished := 0
	total := len(chunks)
	report(ctx, Step{Stage: "read", Message: fmt.Sprintf("reading %d sources", len(docs)), Current: 0, Total: total})
	for i, c := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				progMu.Lock()
				finished++
				report(ctx, Step{Stage: "read", Message: fmt.Sprintf("reading %d sources", len(docs)), Current: finished, Total: total})
				progMu.Unlock()
			}()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			reply, price, err := chat(ctx, r.Meter, r.Model, "read", ref, []model.Message{
				{Role: "system", Content: system},
				{Role: "user", Content: readerUser(title, c, docs)},
			}, model.Options{JSON: true})
			results[i].price = price
			if err != nil {
				results[i].err = err
				return
			}
			raws, err := finding.Parse(reply.Text)
			if err != nil {
				results[i].err = err
				return
			}
			in := map[string]bool{}
			for _, s := range c.Segments {
				in[s.Ref] = true
			}
			for _, raw := range raws {
				raw.Ref = strings.Trim(strings.TrimSpace(raw.Ref), "[]")
				if !in[raw.Ref] {
					results[i].unquoted++
					continue
				}
				results[i].raws = append(results[i].raws, raw)
			}
		}()
	}
	wg.Wait()

	out := Read{Chunks: len(chunks)}
	var raws []finding.Raw
	var firstErr error
	for _, res := range results {
		out.Cost += res.price
		if res.err != nil {
			out.Failed++
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		raws = append(raws, res.raws...)
		out.Drops.Unquoted += res.unquoted
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(chunks) > 0 && out.Failed == len(chunks) {
		return out, fmt.Errorf("read: every reader call failed: %w", firstErr)
	}
	fs, drops := finding.Verify(raws, segs)
	out.Findings = fs
	out.Drops.Add(drops)
	return out, nil
}
