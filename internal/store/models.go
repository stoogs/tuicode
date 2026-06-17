package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ModelConfig is the per-model config stored at models/<alias>.json.
type ModelConfig struct {
	Alias          string  `json:"alias"`
	DisplayName    string  `json:"display_name"`
	ModelTag       string  `json:"model_tag"`
	ParamsBillions float64 `json:"params_billions,omitempty"`
	Quant          string  `json:"quant,omitempty"`
	ContextLength  int     `json:"context_length,omitempty"`
	// NumGPU is the number of layers to offload to the GPU. -1 = auto (let
	// Ollama decide), 0 = CPU-only, N = N layers. Baked into a derived model.
	NumGPU     int        `json:"num_gpu"`
	Residency  Residency  `json:"residency"`
	Parameters Parameters `json:"parameters"`
	// LastSession records the most recent OpenCode session launched on this
	// model, so the dashboard can offer to continue it. nil = none yet.
	LastSession *SessionRef `json:"last_session,omitempty"`
}

// SessionRef points at a resumable OpenCode session (continue with
// `opencode -s <id>`). Captured from `opencode session list` after a session.
type SessionRef struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Dir   string `json:"dir,omitempty"`
	// Updated is the session's last-updated time (unix millis, from OpenCode).
	// Used to pick the most-recent session across all models.
	Updated int64 `json:"updated,omitempty"`
}

// AliasFor derives a filesystem-safe alias from a model tag.
// "qwen3-coder:30b" → "qwen3-coder-30b".
func AliasFor(tag string) string {
	r := strings.NewReplacer(":", "-", "/", "-", " ", "-")
	return r.Replace(tag)
}

// DisplayNameFor produces a human-friendly display name from a tag.
// "qwen3-coder:30b" → "Qwen3 Coder 30B".
func DisplayNameFor(tag string) string {
	base := tag
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	words := strings.Fields(base)
	for i, w := range words {
		words[i] = titleWord(w)
	}
	name := strings.Join(words, " ")
	if i := strings.IndexByte(tag, ':'); i >= 0 {
		name += " " + strings.ToUpper(tag[i+1:])
	}
	return name
}

func titleWord(w string) string {
	if w == "" {
		return w
	}
	// Keep all-caps-with-digits tokens; capitalize first letter otherwise.
	runes := []rune(w)
	runes[0] = upperRune(runes[0])
	return string(runes)
}

func upperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}

// NumGPUAuto is the sentinel for "let Ollama decide GPU offload".
const NumGPUAuto = -1

// DefaultModelConfig builds a config for a tag using global defaults.
func DefaultModelConfig(tag string, defResidency Residency) ModelConfig {
	return ModelConfig{
		Alias:       AliasFor(tag),
		DisplayName: DisplayNameFor(tag),
		ModelTag:    tag,
		NumGPU:      NumGPUAuto,
		Residency:   defResidency,
		Parameters:  PresetCoding,
	}
}

// LoadModelConfig reads models/<alias>.json. Returns (cfg, false, nil) when the
// file doesn't exist so callers can fall back to defaults.
func (s *Store) LoadModelConfig(tag string) (ModelConfig, bool, error) {
	path := filepath.Join(s.modelsDir(), AliasFor(tag)+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ModelConfig{}, false, nil
	}
	if err != nil {
		return ModelConfig{}, false, err
	}
	// Pre-seed NumGPU so an absent "num_gpu" key (older config) stays "auto"
	// rather than unmarshalling to 0 (which would mean CPU-only).
	cfg := ModelConfig{NumGPU: NumGPUAuto}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ModelConfig{}, false, err
	}
	return cfg, true, nil
}

// SaveModelConfig writes models/<alias>.json.
func (s *Store) SaveModelConfig(cfg ModelConfig) error {
	if cfg.Alias == "" {
		cfg.Alias = AliasFor(cfg.ModelTag)
	}
	dir := s.modelsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, cfg.Alias+".json"), append(data, '\n'), 0o644)
}

// GetOrDefaultModelConfig returns the stored config or a default built from tag.
func (s *Store) GetOrDefaultModelConfig(tag string, defResidency Residency) (ModelConfig, error) {
	cfg, ok, err := s.LoadModelConfig(tag)
	if err != nil {
		return ModelConfig{}, err
	}
	if !ok {
		return DefaultModelConfig(tag, defResidency), nil
	}
	return cfg, nil
}

// Preset sampler settings.
var (
	PresetCoding        = Parameters{Temperature: 0.6, TopP: 0.95, TopK: 40}
	PresetBalanced      = Parameters{Temperature: 0.7, TopP: 0.9, TopK: 40}
	PresetCreative      = Parameters{Temperature: 0.9, TopP: 0.98, TopK: 50}
	PresetDeterministic = Parameters{Temperature: 0.2, TopP: 0.9, TopK: 20}
)

// Preset is a named sampler preset.
type Preset struct {
	Name   string
	Params Parameters
}

// Presets returns the built-in presets in display order.
func Presets() []Preset {
	return []Preset{
		{"Coding", PresetCoding},
		{"Balanced", PresetBalanced},
		{"Creative", PresetCreative},
		{"Deterministic", PresetDeterministic},
	}
}
