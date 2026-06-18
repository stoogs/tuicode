//go:build linux

package hw

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// overridable for tests
var (
	runNvidiaSMI = func(ctx context.Context, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "nvidia-smi", args...).Output()
		return string(out), err
	}
	meminfoPath = "/proc/meminfo"
)

// detectGPU queries nvidia-smi for total/free VRAM (MiB). Returns ok=false on
// any error or absence — caller falls back to RAM silently. Linux GPUs are
// discrete, so unified is always false.
func detectGPU(ctx context.Context) (mem Memory, name string, unified, ok bool) {
	out, err := runNvidiaSMI(ctx,
		"--query-gpu=memory.total,memory.free,name",
		"--format=csv,noheader,nounits")
	if err != nil {
		return Memory{}, "", false, false
	}
	// Use the first GPU line (multi-GPU aggregation is out of scope).
	line := firstNonEmptyLine(out)
	if line == "" {
		return Memory{}, "", false, false
	}
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return Memory{}, "", false, false
	}
	total, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	free, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err1 != nil || err2 != nil {
		return Memory{}, "", false, false
	}
	if len(parts) >= 3 {
		name = strings.TrimSpace(parts[2])
	}
	const mib = 1024 * 1024
	return Memory{Source: "gpu", Total: total * mib, Free: free * mib}, name, false, true
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
