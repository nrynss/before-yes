package main

import (
	"context"
	"github.com/nrynss/before-yes/internal/model"
	"strings"
	"testing"
)

func TestEntailOnlyRefusesUncachedReaderAndGradeWithoutClient(t *testing.T) {
	before := *liveEntailOnly
	*liveEntailOnly = true
	defer func() { *liveEntailOnly = before }()
	for _, role := range []string{"reader", "judge"} {
		l := &liveChatter{role: role, dir: t.TempDir()}
		_, err := l.Chat(context.Background(), []model.Message{{Role: "system", Content: "You grade a title"}}, model.Options{JSON: true})
		if err == nil || !strings.Contains(err.Error(), "entail-only refuses") {
			t.Fatalf("%s: %v", role, err)
		}
		if l.calls.Load() != 0 {
			t.Fatal("guard allowed a paid call")
		}
	}
}
