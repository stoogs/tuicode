package server

import (
	"context"
	"time"
)

// DiskModel is a model present on disk (from `ollama list` / /api/tags).
type DiskModel struct {
	Tag       string    // e.g. "qwen3-coder:30b"
	Size      int64     // bytes on disk
	Modified  time.Time // last modified
	ParamSize string    // e.g. "30B" (from details, may be empty)
	Quant     string    // e.g. "Q4_K_M" (from details, may be empty)
	Family    string    // e.g. "qwen3" (from details, may be empty)
}

// LoadedModel is a model currently resident in memory (from `ollama ps` / /api/ps).
type LoadedModel struct {
	Tag       string    // model tag
	Size      int64     // total size in bytes (RAM+VRAM)
	SizeVRAM  int64     // bytes resident in VRAM
	Context   int       // loaded context length (num_ctx), 0 if unknown
	ExpiresAt time.Time // when the model auto-unloads (zero = unknown/never)
	ParamSize string
	Quant     string
}

// GPUPercent returns the share of the model resident on the GPU (0-100).
// Mirrors Ollama's "100% GPU" / "30%/70% CPU/GPU" processor column.
func (m LoadedModel) GPUPercent() int {
	if m.Size <= 0 {
		return 0
	}
	p := float64(m.SizeVRAM) / float64(m.Size) * 100
	if p > 100 {
		p = 100
	}
	return int(p + 0.5)
}

// Processor describes where the model runs: "100% GPU", "100% CPU", or a split.
func (m LoadedModel) Processor() string {
	g := m.GPUPercent()
	switch {
	case g >= 100:
		return "100% GPU"
	case g <= 0:
		return "100% CPU"
	default:
		return itoa(100-g) + "%/" + itoa(g) + "% CPU/GPU"
	}
}

// Until returns the duration until auto-unload, relative to now. Zero if unknown.
func (m LoadedModel) Until(now time.Time) time.Duration {
	if m.ExpiresAt.IsZero() {
		return 0
	}
	d := m.ExpiresAt.Sub(now)
	if d < 0 {
		return 0
	}
	return d
}

// Held reports whether the model is pinned indefinitely (keep_alive: -1).
// Ollama reports a far-future expiry (~years out) for held models.
func (m LoadedModel) Held(now time.Time) bool {
	return !m.ExpiresAt.IsZero() && m.ExpiresAt.Sub(now) > 365*24*time.Hour
}

// ModelDetails is the subset of `ollama show` we use.
type ModelDetails struct {
	Tag           string
	ParamSize     string // "30B"
	Quant         string // "Q4_K_M"
	Family        string
	ContextLength int      // model's max context, 0 if unknown
	BlockCount    int      // transformer layers (for GPU-offload split estimates), 0 if unknown
	SlidingWindow int      // local-attention window (0 = full attention); shrinks KV at long ctx
	Capabilities  []string // e.g. ["completion","tools","vision"], from `ollama show`
}

// HasCapability reports whether the model advertises capability c (e.g. "tools").
func (d ModelDetails) HasCapability(c string) bool {
	for _, x := range d.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// DaemonStatus describes Ollama daemon reachability.
type DaemonStatus struct {
	Reachable bool
	Version   string
	Endpoint  string
	Err       string // human-readable error when unreachable
}

// Backend abstracts a model server (Ollama in the MVP). A second backend
// (e.g. LM Studio) can implement this interface without UI rework.
type Backend interface {
	// Status reports whether the daemon is reachable.
	Status(ctx context.Context) DaemonStatus
	// List returns models on disk.
	List(ctx context.Context) ([]DiskModel, error)
	// Loaded returns models currently resident in memory.
	Loaded(ctx context.Context) ([]LoadedModel, error)
	// Load warm-loads a model with a residency policy (keep_alive). Context and
	// GPU-layer pinning are baked into derived models via Create (Ollama's
	// OpenAI-compatible /v1 endpoint ignores per-request num_ctx/num_gpu).
	Load(ctx context.Context, tag string, keepAlive string) error
	// Unload force-unloads a model now (frees VRAM).
	Unload(ctx context.Context, tag string) error
	// Create derives a model from `from` with baked parameters (e.g. num_ctx,
	// num_gpu). Idempotent: re-creating an identical model is a cheap no-op that
	// shares the base model's blobs.
	Create(ctx context.Context, name, from string, params map[string]any) error
	// Delete removes a model from disk.
	Delete(ctx context.Context, tag string) error
	// Pull downloads a model, reporting progress lines.
	Pull(ctx context.Context, tag string, progress func(PullProgress)) error
	// Show returns model details.
	Show(ctx context.Context, tag string) (ModelDetails, error)
}

// PullProgress is one progress update during a pull.
type PullProgress struct {
	Status    string // e.g. "pulling manifest", "downloading"
	Completed int64
	Total     int64
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
