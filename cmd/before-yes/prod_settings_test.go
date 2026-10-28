package main

import (
	"context"
	"path/filepath"
	"testing"
)

// The production settings must carry the judge's sampling switches: at
// temperature 0 Ultra loops until its token cap. A file that loads with the
// maps empty would deploy a judge that never answers.
func TestProductionSettingsCarryJudgeSampling(t *testing.T) {
	// Keys resolve to a dummy; any other variable stays unset, so the file's
	// own values stand.
	dummy := func(name string) (string, bool) {
		if name == "NEBIUS_KEY" || name == "TAVILY_KEY" {
			return "test-value", true
		}
		return "", false
	}
	s, _, err := Load(context.Background(), filepath.Join("..", "..", "deploy", "before-yes.prod.toml"), dummy)
	if err != nil {
		t.Fatal(err)
	}
	if s.Judge.Model != "nvidia/Nemotron-3-Ultra-550b-a55b" || s.Reader.Model != "nvidia/NVIDIA-Nemotron-3-Nano-30B-A3B" {
		t.Errorf("models: judge %q, reader %q", s.Judge.Model, s.Reader.Model)
	}
	think := s.JudgeThink
	if think["temperature"] != 0.6 || think["top_p"] != 0.95 || think["repetition_penalty"] != 1.05 {
		t.Errorf("judge_think %v", think)
	}
	if kw, _ := think["chat_template_kwargs"].(map[string]any); kw["enable_thinking"] != true {
		t.Errorf("judge_think thinking switch %v", think["chat_template_kwargs"])
	}
	fast := s.JudgeFast
	if fast["repetition_penalty"] != 1.1 {
		t.Errorf("judge_fast %v", fast)
	}
	if kw, _ := fast["chat_template_kwargs"].(map[string]any); kw["enable_thinking"] != false {
		t.Errorf("judge_fast thinking switch %v", fast["chat_template_kwargs"])
	}
}
