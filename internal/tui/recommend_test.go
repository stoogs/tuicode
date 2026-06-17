package tui

import (
	"testing"

	"tuicode/internal/store"
)

func TestReorderByPopularity(t *testing.T) {
	list := []store.TrendingModel{
		{Tag: "llama3.2:1b"}, {Tag: "qwen3-coder:30b"}, {Tag: "obscure:7b"},
	}
	popular := []string{"qwen3-coder", "llama3.2"} // obscure not present
	got := reorderByPopularity(list, popular)
	if got[0].Tag != "qwen3-coder:30b" || got[1].Tag != "llama3.2:1b" {
		t.Errorf("order = %v, want qwen3-coder then llama3.2 then obscure", tags(got))
	}
	if got[2].Tag != "obscure:7b" {
		t.Errorf("unranked entry should sink to the end, got %v", tags(got))
	}
	if len(got) != len(list) {
		t.Errorf("reorder dropped entries: %d -> %d", len(list), len(got))
	}
}

func tags(l []store.TrendingModel) []string {
	out := make([]string, len(l))
	for i, m := range l {
		out[i] = m.Tag
	}
	return out
}

func TestRecForMatch(t *testing.T) {
	rec := store.Recommended{Models: []store.RecModel{
		{Tag: "qwen3-coder:30b", GPUPercent: 75, MemGB: 20},
		{Tag: "gpt-oss:20b", GPUPercent: 100, MemGB: 14},
	}}
	if r, ok := recFor(rec, "qwen3-coder:30b"); !ok || r.GPUPercent != 75 {
		t.Errorf("exact match failed: %+v ok=%v", r, ok)
	}
	// base-name fallback when the size tag differs
	if r, ok := recFor(rec, "gpt-oss:120b"); !ok || r.Tag != "gpt-oss:20b" {
		t.Errorf("base fallback failed: %+v ok=%v", r, ok)
	}
	if _, ok := recFor(rec, "nonesuch:7b"); ok {
		t.Error("unexpected match for unknown tag")
	}
}

func TestSplitLabel(t *testing.T) {
	cases := map[int]string{100: "100% GPU", 0: "100% CPU", 75: "25%/75% CPU/GPU"}
	for pct, want := range cases {
		if got := splitLabel(pct); got != want {
			t.Errorf("splitLabel(%d) = %q, want %q", pct, got, want)
		}
	}
}
