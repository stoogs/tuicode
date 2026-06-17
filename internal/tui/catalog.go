package tui

import (
	"sort"

	"tuicode/internal/hw"
)

// catalogModel is a popular Ollama model offered on the pull screen.
type catalogModel struct {
	Tag     string
	ParamsB float64 // parameter count in billions (for the Q4_K_M fit estimate)
	Note    string  // short descriptor
}

// trendingCatalog is a curated set of popular, tool-capable models on Ollama
// (OpenCode needs tool support, so non-tool models like gemma3 are omitted).
// Roughly ordered by popularity. Footprints are estimated at Q4_K_M.
var trendingCatalog = []catalogModel{
	{"qwen3:4b", 4, "general · fast"},
	{"qwen3:8b", 8, "general"},
	{"qwen3:14b", 14, "general"},
	{"qwen3:30b-a3b", 30, "MoE · fast"},
	{"qwen2.5-coder:1.5b", 1.5, "coding · tiny"},
	{"qwen2.5-coder:3b", 3, "coding"},
	{"qwen2.5-coder:7b", 7, "coding"},
	{"qwen2.5-coder:14b", 14, "coding"},
	{"qwen2.5-coder:32b", 32, "coding · strong"},
	{"qwen3-coder:30b", 30, "coding · agentic"},
	{"llama3.2:1b", 1, "tiny starter"},
	{"llama3.2:3b", 3, "general · fast"},
	{"llama3.1:8b", 8, "general"},
	{"mistral:7b", 7, "general"},
	{"mistral-nemo:12b", 12, "general · 128k"},
	{"devstral:24b", 24, "coding · agentic"},
	{"phi4:14b", 14, "reasoning"},
	{"deepseek-r1:7b", 7, "reasoning"},
	{"deepseek-r1:8b", 8, "reasoning"},
	{"deepseek-r1:14b", 14, "reasoning"},
	{"deepseek-r1:32b", 32, "reasoning · strong"},
}

// catalogEntry is a catalog model with its computed fit for the current memory.
type catalogEntry struct {
	catalogModel
	EstGB float64
	Fits  bool
}

// fittingModels returns up to `limit` catalog models that fit in the
// authoritative memory pool at Q4_K_M (weights + a nominal context), preferring
// the largest that still fit (more capable) then smaller ones. When memory is
// unknown it returns the first `limit` entries unfiltered.
func fittingModels(det hw.Detection, limit int) []catalogEntry {
	mem := det.Authoritative()
	totalGB := float64(mem.Total) / gib
	reserveGB := float64(det.Reserve()) / gib

	entries := make([]catalogEntry, 0, len(trendingCatalog))
	for _, c := range trendingCatalog {
		u := hw.EstimateUsage(c.ParamsB, "Q4_K_M", 8192, 0)
		fits := totalGB <= 0 || u.TotalGB+reserveGB <= totalGB
		entries = append(entries, catalogEntry{catalogModel: c, EstGB: u.TotalGB, Fits: fits})
	}

	// Keep only fitting models when memory is known.
	if totalGB > 0 {
		kept := entries[:0:0]
		for _, e := range entries {
			if e.Fits {
				kept = append(kept, e)
			}
		}
		entries = kept
		// Largest-that-fits first (most capable), then by name for stability.
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].ParamsB != entries[j].ParamsB {
				return entries[i].ParamsB > entries[j].ParamsB
			}
			return entries[i].Tag < entries[j].Tag
		})
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}
