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
	// FinishReason is the host's finish_reason, so a reply cut off at its
	// token cap is visible in the recording. Recordings made before the
	// field existed leave it empty.
	FinishReason string `json:"finish_reason,omitempty"`
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

// recordedModel is the model id that answered the recordings under dir/role,
// the id each live reply reported when it was recorded. It returns "" when the
// role has no recordings (a role a replay never calls; a call it does make is
// counted as a miss). It fails when a recording names no model, or when two
// names differ, since a card must not name a model that did not run.
func recordedModel(dir, role string) (string, error) {
	names, err := filepath.Glob(filepath.Join(dir, role, "*.json"))
	if err != nil {
		return "", err
	}
	found := ""
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			return "", err
		}
		var rec Recording
		if err := json.Unmarshal(b, &rec); err != nil {
			return "", fmt.Errorf("eval: recording %s: %w", n, err)
		}
		switch {
		case rec.Model == "":
			return "", fmt.Errorf("eval: recording %s names no model", n)
		case found == "":
			found = rec.Model
		case rec.Model != found:
			return "", fmt.Errorf("eval: %s recordings name two models, %s and %s", filepath.Join(dir, role), found, rec.Model)
		}
	}
	return found, nil
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
	return model.Reply{Text: rec.Text, InputTokens: rec.InputTokens, OutputTokens: rec.OutputTokens, Price: rec.PriceNano, Measured: rec.Measured, FinishReason: rec.FinishReason}, nil
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
