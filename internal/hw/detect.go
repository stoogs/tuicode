// Package hw detects host memory (GPU via nvidia-smi, RAM via /proc/meminfo)
// and estimates a safe context length for a model. All detection is best-effort
// and degrades silently to a RAM fallback when no GPU is present.
package hw

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	Source string // "gpu" or "ram"
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
	GPU     *Memory // nil if no GPU / cpu-only
	RAM     Memory
	HasGPU  bool
	GPUName string
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

// Reserve returns the memory headroom (bytes) to leave free for the active mode.
// 2GB for GPU, 4GB for RAM.
func (d Detection) Reserve() int64 {
	if d.Authoritative().Source == "gpu" {
		return 2 * gib
	}
	return 4 * gib
}

// overridable for tests
var (
	runNvidiaSMI = func(ctx context.Context, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "nvidia-smi", args...).Output()
		return string(out), err
	}
	meminfoPath = "/proc/meminfo"
)

// Detect gathers GPU + RAM memory for the given device mode.
func Detect(ctx context.Context, mode DeviceMode) Detection {
	d := Detection{Mode: mode}
	d.RAM = detectRAM()

	if mode != CPUOnly {
		if g, name, ok := detectGPU(ctx); ok {
			d.GPU = &g
			d.HasGPU = true
			d.GPUName = name
		}
	}
	return d
}

// detectGPU queries nvidia-smi for total/free VRAM (MiB). Returns ok=false on
// any error or absence — caller falls back to RAM silently.
func detectGPU(ctx context.Context) (Memory, string, bool) {
	out, err := runNvidiaSMI(ctx,
		"--query-gpu=memory.total,memory.free,name",
		"--format=csv,noheader,nounits")
	if err != nil {
		return Memory{}, "", false
	}
	// Use the first GPU line (multi-GPU aggregation is out of scope).
	line := firstNonEmptyLine(out)
	if line == "" {
		return Memory{}, "", false
	}
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return Memory{}, "", false
	}
	total, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	free, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err1 != nil || err2 != nil {
		return Memory{}, "", false
	}
	name := ""
	if len(parts) >= 3 {
		name = strings.TrimSpace(parts[2])
	}
	const mib = 1024 * 1024
	return Memory{Source: "gpu", Total: total * mib, Free: free * mib}, name, true
}

// detectRAM reads MemTotal/MemAvailable from /proc/meminfo (values in kB).
func detectRAM() Memory {
	m := Memory{Source: "ram"}
	data, err := os.ReadFile(meminfoPath)
	if err != nil {
		return m
	}
	total, avail := ParseMeminfo(string(data))
	m.Total = total
	m.Free = avail
	return m
}

// ParseMeminfo extracts MemTotal and MemAvailable (bytes) from /proc/meminfo.
// Exported for tests.
func ParseMeminfo(content string) (total, available int64) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		val *= 1024 // kB → bytes
		switch key {
		case "MemTotal":
			total = val
		case "MemAvailable":
			available = val
		}
	}
	return total, available
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			return l
		}
	}
	return ""
}
