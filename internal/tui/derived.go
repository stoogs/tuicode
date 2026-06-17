package tui

import (
	"context"
	"fmt"
	"strings"

	"tuicode/internal/hw"
	"tuicode/internal/server"
	"tuicode/internal/store"
)

// pruneDerived deletes every tuicode-managed derived model that is not currently
// resident, returning how many were removed. Best-effort: a Delete error skips
// that model rather than aborting.
func pruneDerived(ctx context.Context, be server.Backend) (int, error) {
	disk, err := be.List(ctx)
	if err != nil {
		return 0, err
	}
	loaded, _ := be.Loaded(ctx)
	residency := make(map[string]bool, len(loaded))
	for _, lm := range loaded {
		residency[lm.Tag] = true
	}
	n := 0
	for _, dm := range disk {
		if isDerived(dm.Tag) && !residency[dm.Tag] {
			if err := be.Delete(ctx, dm.Tag); err == nil {
				n++
			}
		}
	}
	return n, nil
}

// Ollama's OpenAI-compatible /v1 endpoint (used by OpenCode) ignores per-request
// num_ctx and num_gpu, and resets them to the daemon defaults. To pin a chosen
// context or GPU-offload for an OpenCode session, tuicode bakes them into a
// lightweight *derived* model (a Modelfile FROM the base with PARAMETERs),
// created on demand. Derived models share the base's blobs, so they're cheap.

const derivedPrefix = "tuicode/"

// effectiveNumGPU resolves the GPU-layer count to apply, honoring device mode:
// cpu-only forces 0 (CPU), otherwise the per-model setting (-1 = auto).
func effectiveNumGPU(cfg store.ModelConfig, mode hw.DeviceMode) int {
	if mode == hw.CPUOnly {
		return 0
	}
	return cfg.NumGPU
}

// needsDerived reports whether the config requires a baked derived model
// (i.e. a non-default context or an explicit GPU-layer setting).
func needsDerived(cfg store.ModelConfig, mode hw.DeviceMode) bool {
	return cfg.ContextLength > 0 || effectiveNumGPU(cfg, mode) >= 0
}

// serveTag returns the tag OpenCode/Ollama should actually serve: the base tag
// when everything is default, otherwise a derived tag encoding the baked params.
func serveTag(base string, cfg store.ModelConfig, mode hw.DeviceMode) string {
	if !needsDerived(cfg, mode) {
		return base
	}
	ctxPart := "d"
	if cfg.ContextLength > 0 {
		ctxPart = fmt.Sprintf("%d", cfg.ContextLength)
	}
	gpu := effectiveNumGPU(cfg, mode)
	gpuPart := "a"
	if gpu >= 0 {
		gpuPart = fmt.Sprintf("%d", gpu)
	}
	return derivedPrefix + sanitizeTag(base) + ":c" + ctxPart + "g" + gpuPart
}

// derivedParams builds the Modelfile parameters baked into the derived model.
func derivedParams(cfg store.ModelConfig, mode hw.DeviceMode) map[string]any {
	p := map[string]any{}
	if cfg.ContextLength > 0 {
		p["num_ctx"] = cfg.ContextLength
	}
	if g := effectiveNumGPU(cfg, mode); g >= 0 {
		p["num_gpu"] = g
	}
	return p
}

// derivedPrefixFor returns the tag prefix for all derived variants of a base.
func derivedPrefixFor(base string) string {
	return derivedPrefix + sanitizeTag(base) + ":"
}

// isDerived reports whether a tag is a tuicode-managed derived model.
func isDerived(tag string) bool {
	return strings.HasPrefix(tag, derivedPrefix)
}

// sanitizeTag makes a base tag safe for use inside a derived model name.
func sanitizeTag(tag string) string {
	r := strings.NewReplacer(":", "-", "/", "-")
	return r.Replace(tag)
}
