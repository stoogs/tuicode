package server

import (
	"testing"
	"time"
)

const tagsFixture = `{
  "models": [
    {
      "name": "qwen3-coder:30b",
      "model": "qwen3-coder:30b",
      "modified_at": "2025-01-02T10:00:00Z",
      "size": 10630000000,
      "details": {"family": "qwen3", "parameter_size": "30B", "quantization_level": "Q4_K_M"}
    },
    {
      "name": "gemma3:270m",
      "model": "gemma3:270m",
      "modified_at": "2025-01-01T08:00:00Z",
      "size": 291000000,
      "details": {"family": "gemma3", "parameter_size": "270M", "quantization_level": "Q8_0"}
    }
  ]
}`

func TestParseTags(t *testing.T) {
	models, err := ParseTags([]byte(tagsFixture))
	if err != nil {
		t.Fatalf("ParseTags: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("want 2 models, got %d", len(models))
	}
	if models[0].Tag != "qwen3-coder:30b" {
		t.Errorf("tag = %q", models[0].Tag)
	}
	if models[0].Size != 10630000000 {
		t.Errorf("size = %d", models[0].Size)
	}
	if models[0].ParamSize != "30B" || models[0].Quant != "Q4_K_M" {
		t.Errorf("details = %q %q", models[0].ParamSize, models[0].Quant)
	}
}

func TestParseTagsEmpty(t *testing.T) {
	models, err := ParseTags([]byte(`{"models":[]}`))
	if err != nil {
		t.Fatalf("ParseTags: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("want 0, got %d", len(models))
	}
}

func TestParsePSLoaded(t *testing.T) {
	expires := time.Now().Add(16 * time.Minute).UTC().Format(time.RFC3339)
	body := `{"models":[{
		"name":"qwen3-coder:30b",
		"size": 10600000000,
		"size_vram": 10600000000,
		"context_length": 65536,
		"expires_at": "` + expires + `",
		"details": {"parameter_size":"30B","quantization_level":"Q4_K_M"}
	}]}`
	models, err := ParsePS([]byte(body))
	if err != nil {
		t.Fatalf("ParsePS: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1, got %d", len(models))
	}
	m := models[0]
	if m.Tag != "qwen3-coder:30b" {
		t.Errorf("tag = %q", m.Tag)
	}
	if m.Context != 65536 {
		t.Errorf("context = %d", m.Context)
	}
	if m.GPUPercent() != 100 {
		t.Errorf("gpu%% = %d", m.GPUPercent())
	}
	if m.Processor() != "100% GPU" {
		t.Errorf("processor = %q", m.Processor())
	}
	now := time.Now()
	if d := m.Until(now); d < 14*time.Minute || d > 16*time.Minute {
		t.Errorf("until = %v", d)
	}
	if m.Held(now) {
		t.Errorf("should not be held")
	}
}

func TestParsePSSplit(t *testing.T) {
	body := `{"models":[{"name":"big:70b","size":40000000000,"size_vram":20000000000}]}`
	models, _ := ParsePS([]byte(body))
	if len(models) != 1 {
		t.Fatal("want 1")
	}
	if g := models[0].GPUPercent(); g != 50 {
		t.Errorf("gpu%% = %d, want 50", g)
	}
	if p := models[0].Processor(); p != "50%/50% CPU/GPU" {
		t.Errorf("processor = %q", p)
	}
}

func TestParsePSHeld(t *testing.T) {
	// keep_alive: -1 → expires far in the future.
	expires := time.Now().Add(1000 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := `{"models":[{"name":"x:1b","size":1,"size_vram":1,"expires_at":"` + expires + `"}]}`
	models, _ := ParsePS([]byte(body))
	if !models[0].Held(time.Now()) {
		t.Errorf("should be held")
	}
}

func TestParsePSEmpty(t *testing.T) {
	models, err := ParsePS([]byte(`{"models":[]}`))
	if err != nil {
		t.Fatalf("ParsePS: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("want 0, got %d", len(models))
	}
}

func TestParseShowContext(t *testing.T) {
	body := `{
		"details": {"parameter_size":"30B","quantization_level":"Q4_K_M","family":"qwen3"},
		"model_info": {"general.architecture":"qwen3","qwen3.context_length": 262144, "qwen3.block_count": 48}
	}`
	d, err := ParseShow([]byte(body))
	if err != nil {
		t.Fatalf("ParseShow: %v", err)
	}
	if d.ContextLength != 262144 {
		t.Errorf("context = %d", d.ContextLength)
	}
	if d.BlockCount != 48 {
		t.Errorf("block_count = %d, want 48", d.BlockCount)
	}
	if d.ParamSize != "30B" || d.Quant != "Q4_K_M" {
		t.Errorf("details = %q %q", d.ParamSize, d.Quant)
	}
}

func TestParseShowMultimodal(t *testing.T) {
	// A multimodal model (gemma4) carries vision/audio block_count keys too — the
	// main text block_count must win, not a submodule's.
	body := `{
		"details": {"parameter_size":"8B","quantization_level":"Q4_K_M","family":"gemma4"},
		"capabilities": ["completion","vision","audio","tools","thinking"],
		"model_info": {
			"general.architecture":"gemma4",
			"gemma4.context_length": 131072,
			"gemma4.block_count": 42,
			"gemma4.vision.block_count": 16,
			"gemma4.audio.block_count": 12,
			"gemma4.attention.sliding_window": 1024
		}
	}`
	d, err := ParseShow([]byte(body))
	if err != nil {
		t.Fatalf("ParseShow: %v", err)
	}
	if d.SlidingWindow != 1024 {
		t.Errorf("sliding_window = %d, want 1024", d.SlidingWindow)
	}
	if d.BlockCount != 42 {
		t.Errorf("block_count = %d, want 42 (text model, not vision/audio)", d.BlockCount)
	}
	if d.ContextLength != 131072 {
		t.Errorf("context = %d, want 131072", d.ContextLength)
	}
	if !d.HasCapability("tools") || !d.HasCapability("vision") {
		t.Errorf("capabilities = %v, want tools+vision", d.Capabilities)
	}
}

func TestParseKeepAlive(t *testing.T) {
	cases := map[string]any{"": 0, "0": 0, "-1": -1, "forever": -1, "20m": "20m"}
	for in, want := range cases {
		if got := parseKeepAlive(in); got != want {
			t.Errorf("parseKeepAlive(%q) = %v, want %v", in, got, want)
		}
	}
}
