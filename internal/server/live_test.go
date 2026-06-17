package server

import (
	"context"
	"os"
	"testing"
)

// Gated live test against a running Ollama daemon. Run with LIVE=1.
func TestLiveDerivedContext(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE=1 to run against a real daemon")
	}
	o := NewOllama("")
	ctx := context.Background()
	base := "llama3.2:1b"
	derived := "tuicode/llama32-1b:c8192g0"

	// Create derived with baked ctx + CPU.
	if err := o.Create(ctx, derived, base, map[string]any{"num_ctx": 8192, "num_gpu": 0}); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer o.Delete(ctx, derived)

	// Warm-load it.
	if err := o.Load(ctx, derived, "1m"); err != nil {
		t.Fatalf("load: %v", err)
	}
	defer o.Unload(ctx, derived)

	loaded, err := o.Loaded(ctx)
	if err != nil {
		t.Fatalf("loaded: %v", err)
	}
	var found *LoadedModel
	for i := range loaded {
		if loaded[i].Tag == derived {
			found = &loaded[i]
		}
	}
	if found == nil {
		t.Fatalf("derived model not in ps: %+v", loaded)
	}
	if found.Context != 8192 {
		t.Errorf("ps context = %d, want 8192 (baked)", found.Context)
	}
	if found.GPUPercent() != 0 {
		t.Errorf("gpu%% = %d, want 0 (num_gpu:0 forces CPU)", found.GPUPercent())
	}
	t.Logf("derived %s: ctx=%d gpu%%=%d ✓", derived, found.Context, found.GPUPercent())
}
