package tui

import (
	"sort"

	"tuicode/internal/hw"
	"tuicode/internal/store"
)

// catalogEntry is a trending model with its computed fit for the current memory.
type catalogEntry struct {
	store.TrendingModel
	EstGB   float64
	Fits    bool // runnable: fits in system RAM (possibly via a CPU/GPU split)
	VRAMFit bool // fits entirely in VRAM (runs fully on the GPU — fastest)
}

// runCeilingGB is the memory a model can occupy and still run: system RAM, since
// Ollama offloads to RAM whatever doesn't fit in VRAM (a CPU/GPU split). VRAM
// alone is too strict — it would hide models that run fine, just slower.
func runCeilingGB(det hw.Detection) float64 {
	if det.RAM.Total > 0 {
		return float64(det.RAM.Total) / gib
	}
	return float64(det.Authoritative().Total) / gib
}

func vramGB(det hw.Detection) float64 {
	if det.GPU != nil {
		return float64(det.GPU.Total) / gib
	}
	return 0
}

// fittingModels returns up to `limit` trending models that can run on this
// machine — i.e. fit in system RAM at Q4_K_M (weights + a nominal context),
// possibly via a CPU/GPU split — largest first. Entries that don't fit entirely
// in VRAM are kept (they run on a split) and flagged VRAMFit=false. When memory
// is unknown the first `limit` entries are returned unfiltered (preserving the
// popularity ordering from the daily refresh).
func fittingModels(trending []store.TrendingModel, det hw.Detection, limit int) []catalogEntry {
	ceil := runCeilingGB(det)
	reserveGB := float64(det.Reserve()) / gib
	vram := vramGB(det)

	entries := make([]catalogEntry, 0, len(trending))
	for _, c := range trending {
		u := hw.EstimateUsage(c.ParamsB, "Q4_K_M", 8192, 0)
		fits := ceil <= 0 || u.TotalGB+reserveGB <= ceil
		entries = append(entries, catalogEntry{
			TrendingModel: c, EstGB: u.TotalGB, Fits: fits,
			VRAMFit: vram <= 0 || u.TotalGB+reserveGB <= vram,
		})
	}

	// Keep only runnable models when memory is known.
	if ceil > 0 {
		kept := entries[:0:0]
		for _, e := range entries {
			if e.Fits {
				kept = append(kept, e)
			}
		}
		entries = kept
		// Largest-that-runs first (most capable), then by name for stability.
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

// recEntry is a benchmark-reference model with its fit for the current memory.
type recEntry struct {
	store.RecModel
	Fits bool
}

// fittingRecommended returns the benchmark-reference models that can run on this
// machine — fitting in system RAM (the benchmark's own mem_gb footprint covers
// the CPU/GPU split), fastest-likely first (more on GPU ⇒ higher gpu_percent ⇒
// generally faster). When memory is unknown, all are returned in file order.
func fittingRecommended(rec store.Recommended, det hw.Detection, limit int) []recEntry {
	totalGB := runCeilingGB(det)
	reserveGB := float64(det.Reserve()) / gib

	entries := make([]recEntry, 0, len(rec.Models))
	for _, r := range rec.Models {
		fits := totalGB <= 0 || r.MemGB+reserveGB <= totalGB
		if totalGB > 0 && !fits {
			continue
		}
		entries = append(entries, recEntry{RecModel: r, Fits: fits})
	}
	if totalGB > 0 {
		// Higher GPU share first (faster), then smaller footprint.
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].GPUPercent != entries[j].GPUPercent {
				return entries[i].GPUPercent > entries[j].GPUPercent
			}
			return entries[i].MemGB < entries[j].MemGB
		})
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

// recFor returns the benchmark-reference entry for a model tag, matching the
// exact tag first, then the base name (before ':'). ok=false when none.
func recFor(rec store.Recommended, tag string) (store.RecModel, bool) {
	base := tag
	if i := indexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	var baseHit *store.RecModel
	for idx := range rec.Models {
		r := rec.Models[idx]
		if r.Tag == tag {
			return r, true
		}
		rb := r.Tag
		if i := indexByte(rb, ':'); i >= 0 {
			rb = rb[:i]
		}
		if rb == base && baseHit == nil {
			baseHit = &rec.Models[idx]
		}
	}
	if baseHit != nil {
		return *baseHit, true
	}
	return store.RecModel{}, false
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
