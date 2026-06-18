// Package hw detects host memory and estimates a safe context length for a
// model. Detection is platform-specific (see detect_linux.go / detect_darwin.go)
// and best-effort: it degrades silently to a RAM fallback when no accelerator is
// present. All cross-platform types and the detection orchestration live here.
package hw

import (
	"context"
)

// DeviceMode selects which memory source feeds the estimate.
type DeviceMode string

const (
	Auto    DeviceMode = "auto"
	CPUOnly DeviceMode = "cpu-only"
	GPUOnly DeviceMode = "gpu-only"
)

// Memory is a detected memory pool, in bytes.
type Memory struct {
	Source string // "gpu", "ram", or "unified" (Apple Silicon)
	Total  int64
	Free   int64
}

const gib = 1024 * 1024 * 1024

// GB returns total memory in GB (decimal-ish, /1024^3).
func (m Memory) TotalGB() float64 { return float64(m.Total) / gib }
func (m Memory) FreeGB() float64  { return float64(m.Free) / gib }

// Detection is the full hardware picture used by the dashboard + estimator.
type Detection struct {
	Mode    DeviceMode
	GPU     *Memory // nil if no GPU / cpu-only; on Apple Silicon this is the unified pool
	RAM     Memory
	HasGPU  bool
	GPUName string
	Unified bool // GPU shares system RAM (Apple Silicon) — one pool, not two
}

// Authoritative returns the memory pool that drives estimation for the mode.
func (d Detection) Authoritative() Memory {
	switch d.Mode {
	case CPUOnly:
		return d.RAM
	case GPUOnly:
		if d.GPU != nil {
			return *d.GPU
		}
		return d.RAM
	default: // Auto
		if d.GPU != nil {
			return *d.GPU
		}
		return d.RAM
	}
}

// unifiedReserveFraction is the share of unified (Apple Silicon) memory kept
// free for the OS, browser, and CPU-side work. The GPU can only address the rest
// (~70% — Metal's default wired limit), so this fraction doubles as the practical
// model+context ceiling and scales with the machine instead of a flat figure.
const unifiedReserveFraction = 0.30

// Reserve returns the memory headroom (bytes) to leave free for the active mode:
// 2GB for a discrete GPU, 4GB for system RAM, and ~30% of the pool for unified
// memory (where the same RAM runs the OS, the browser, and the model).
func (d Detection) Reserve() int64 {
	mem := d.Authoritative()
	switch mem.Source {
	case "unified":
		return int64(float64(mem.Total) * unifiedReserveFraction)
	case "gpu":
		return 2 * gib
	default: // ram
		return 4 * gib
	}
}

// Detect gathers GPU + RAM memory for the given device mode. detectRAM and
// detectGPU are provided per-platform.
func Detect(ctx context.Context, mode DeviceMode) Detection {
	d := Detection{Mode: mode}
	d.RAM = detectRAM()

	if mode != CPUOnly {
		if g, name, unified, ok := detectGPU(ctx); ok {
			d.GPU = &g
			d.HasGPU = true
			d.GPUName = name
			d.Unified = unified
		}
	}
	return d
}
