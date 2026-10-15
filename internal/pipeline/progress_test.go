package pipeline

import (
	"context"
	"testing"
)

// TestProgressHookPanicRecovered: a hook that panics does not crash the
// caller, and a later report still reaches the hook.
func TestProgressHookPanicRecovered(t *testing.T) {
	calls := 0
	ctx := WithProgress(context.Background(), func(Step) {
		calls++
		if calls == 1 {
			panic("hook")
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		report(ctx, Step{Stage: "read", Current: 1, Total: 2})
	}()
	<-done
	report(ctx, Step{Stage: "read", Current: 2, Total: 2})
	if calls != 2 {
		t.Fatalf("hook calls = %d, want 2", calls)
	}
}
