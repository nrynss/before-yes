package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/nrynss/keel/cost"

	"github.com/nrynss/before-yes/internal/model"
)

// Recording is one recorded model reply, as T3.6b saves it under
// testdata/eval/replies/<slug>/<role>/<ReplyKey>.json.
type Recording struct {
	Text         string     `json:"text"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	PriceNano    cost.Price `json:"price_nano"`
	Measured     bool       `json:"measured"`
	Model        string     `json:"model"`
}

// ReplyKey names one call: the role, the messages and the options. The same
// prompt with thinking on and off are different calls. No key or secret is
// part of a prompt, so none reaches the file name.
func ReplyKey(role string, msgs []model.Message, o model.Options) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00", role)
	for _, m := range msgs {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00", m.Role, m.Content)
	}
	b, _ := json.Marshal(o)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))[:24]
}

var errNoRecording = errors.New("eval: no recorded reply for this call")

// replayer answers from recordings. A call with no recording fails, which
// the reader counts as a failed chunk and the judge as a failed check.
type replayer struct {
	dir, role, model string
	misses, hits     atomic.Int64
}

func (r *replayer) load(msgs []model.Message, o model.Options) (Recording, error) {
	var rec Recording
	b, err := os.ReadFile(filepath.Join(r.dir, r.role, ReplyKey(r.role, msgs, o)+".json"))
	if err != nil {
		return rec, errNoRecording
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, fmt.Errorf("eval: recording: %w", err)
	}
	return rec, nil
}

func (r *replayer) Chat(_ context.Context, msgs []model.Message, o model.Options) (model.Reply, error) {
	rec, err := r.load(msgs, o)
	if err != nil {
		r.misses.Add(1)
		return model.Reply{}, err
	}
	r.hits.Add(1)
	return model.Reply{Text: rec.Text, InputTokens: rec.InputTokens, OutputTokens: rec.OutputTokens, Price: rec.PriceNano, Measured: rec.Measured}, nil
}

// Estimate is the recorded price, so the meter holds and, for an
// unmeasured recording, settles exactly what the live call cost.
func (r *replayer) Estimate(msgs []model.Message, o model.Options) cost.Price {
	rec, err := r.load(msgs, o)
	if err != nil {
		return 0
	}
	return rec.PriceNano
}
func (r *replayer) Settings() model.Settings {
	return model.Settings{Provider: "replay", Model: r.model}
}
